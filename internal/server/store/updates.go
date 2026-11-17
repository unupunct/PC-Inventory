package store

// Agent auto-update. The server never pushes a binary at an agent on its own:
// an update is published in the release folder, the agent downloads it from
// the server over the same authenticated connection it already uses, checks
// the SHA256 the server advertised, and only then replaces itself. Updates can
// be disabled per server, and the agent refuses to update if its version is
// already current.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AgentUpdate is the release information handed to agents.
type AgentUpdate struct {
	Version     string    `json:"version"`
	Platform    string    `json:"platform"`
	Arch        string    `json:"arch"`
	Filename    string    `json:"filename"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"sizeBytes"`
	PublishedAt time.Time `json:"publishedAt"`
}

const agentUpdateSettingKey = "agent_update"

// AgentUpdatesEnabled reports whether the server offers agent updates.
func (s *Store) AgentUpdatesEnabled(ctx context.Context) bool {
	v, ok := s.GetSetting(ctx, "agent_auto_update")
	return ok && strings.EqualFold(v, "true")
}

// publishedPlatformDir is where release binaries are staged:
// <data>/updates/<platform>/<arch>/<file>
func (s *Store) publishedPlatformDir(root, platform, arch string) string {
	return filepath.Join(root, "updates", platform, arch)
}

// ScanAgentUpdates indexes whatever release binaries were placed under
// <data>/updates and records them in the agent_update table.
func (s *Store) ScanAgentUpdates(ctx context.Context, dataDir string) (int, error) {
	root := filepath.Join(dataDir, "updates")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return 0, nil
	}
	count := 0
	platforms, _ := os.ReadDir(root)
	for _, p := range platforms {
		if !p.IsDir() {
			continue
		}
		arches, _ := os.ReadDir(filepath.Join(root, p.Name()))
		for _, a := range arches {
			if !a.IsDir() {
				continue
			}
			dir := filepath.Join(root, p.Name(), a.Name())
			files, _ := os.ReadDir(dir)
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				st, err := f.Info()
				if err != nil || st.Size() < 1<<20 {
					continue
				}
				// Expect <version>/<name> or a name carrying the version,
				// e.g. pcim-agent-1.1.0-windows-amd64.exe
				up, err := describeBinary(filepath.Join(dir, f.Name()), st)
				if err != nil {
					continue
				}
				if err := s.upsertAgentUpdate(ctx, up); err != nil {
					continue
				}
				count++
			}
		}
	}
	return count, nil
}

func describeBinary(path string, st os.FileInfo) (AgentUpdate, error) {
	f, err := os.Open(path)
	if err != nil {
		return AgentUpdate{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return AgentUpdate{}, err
	}
	name := sanitizeFilename(filepath.Base(path))
	version := versionFromFilename(name)
	if version == "" {
		return AgentUpdate{}, errors.New("no version in filename")
	}
	platform := ""
	switch {
	case strings.Contains(name, "windows"):
		platform = "windows"
	case strings.Contains(name, "linux"):
		platform = "linux"
	case strings.Contains(name, "darwin"), strings.Contains(name, "macos"):
		platform = "darwin"
	}
	arch := ""
	for _, a := range []string{"amd64", "arm64", "armv7", "386"} {
		if strings.Contains(name, a) || strings.Contains(name, map[string]string{"amd64": "x86_64", "386": "i386", "arm64": "aarch64"}[a]) {
			arch = a
			break
		}
	}
	if platform == "" || arch == "" || strings.Contains(name, "server") {
		return AgentUpdate{}, errors.New("not an agent binary")
	}
	return AgentUpdate{
		Version:     version,
		Platform:    platform,
		Arch:        arch,
		Filename:    name,
		SHA256:      hex.EncodeToString(h.Sum(nil)),
		SizeBytes:   st.Size(),
		PublishedAt: st.ModTime().UTC(),
	}, nil
}

func sanitizeFilename(n string) string {
	n = strings.ReplaceAll(n, "\\", "")
	return filepath.Base(n)
}

// versionFromFilename pulls a semver-looking version out of a filename such as
// pcim-agent-1.1.0-windows-amd64.exe. Returns "" when there is none.
func versionFromFilename(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			continue
		}
		if i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
			continue // inside a number already
		}
		// A version starts at a digit that begins a segment.
		j, dots := i, 0
		for j < len(name) {
			c := name[j]
			if c >= '0' && c <= '9' {
				j++
				continue
			}
			if c == '.' && dots < 2 && j+1 < len(name) && name[j+1] >= '0' && name[j+1] <= '9' {
				dots++
				j++
				continue
			}
			break
		}
		if dots >= 1 {
			return name[i:j]
		}
		i = j
	}
	return ""
}

func allDigits(parts []string) bool {
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func (s *Store) upsertAgentUpdate(ctx context.Context, up AgentUpdate) error {
	b, err := json.Marshal(up)
	if err != nil {
		return err
	}
	key := "agent_update:" + up.Platform + ":" + up.Arch
	return s.SetSetting(ctx, key, string(b))
}

// AgentUpdateFor returns the published update for a platform/arch pair.
func (s *Store) AgentUpdateFor(ctx context.Context, platform, arch string) (*AgentUpdate, error) {
	raw, ok := s.GetSetting(ctx, "agent_update:"+platform+":"+arch)
	if !ok {
		return nil, ErrNotFound
	}
	var up AgentUpdate
	if err := json.Unmarshal([]byte(raw), &up); err != nil {
		return nil, err
	}
	return &up, nil
}

// UpdateBinaryPath resolves the on-disk path of a published update.
func UpdateBinaryPath(dataDir string, up *AgentUpdate) string {
	return filepath.Join(dataDir, "updates", up.Platform, up.Arch, up.Filename)
}

// ListAgentUpdates returns every published update.
func (s *Store) ListAgentUpdates(ctx context.Context) ([]AgentUpdate, error) {
	settings, err := s.AllSettings(ctx)
	if err != nil {
		return nil, err
	}
	var out []AgentUpdate
	seen := map[string]bool{}
	for k, v := range settings {
		if !strings.HasPrefix(k, "agent_update:") {
			continue
		}
		var up AgentUpdate
		if err := json.Unmarshal([]byte(v), &up); err != nil {
			continue
		}
		id := up.Platform + "/" + up.Arch + "/" + up.Version
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, up)
	}
	return out, nil
}

// defaultUpdateTarget is the running platform/arch, used for logging.
func defaultUpdateTarget() (string, string) { return runtime.GOOS, runtime.GOARCH }

var _ = fmt.Sprint
var _ = defaultUpdateTarget
var _ = time.Now
