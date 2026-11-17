package agent

// Self-update. The agent asks the server on its own schedule whether a newer
// build is published, downloads it over the authenticated channel it already
// uses, verifies the SHA256 the server advertised, replaces its own executable
// and exits so the service manager (SCM, systemd or launchd) starts the new
// build. If anything about the update looks wrong it is discarded and the agent
// keeps running the build it already has.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/agent/client"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

// checkForUpdate asks the server for a newer build and applies it when one is
// published and auto-update is enabled in the agent's configuration.
func (a *Agent) checkForUpdate(ctx context.Context) {
	if !a.Cfg.AutoUpdate {
		return
	}
	platform, arch := runtime.GOOS, runtime.GOARCH
	up, msg, err := a.client.CheckUpdate(ctx, platform, arch, version.Version)
	if err != nil {
		a.logf("warn", "update check failed: %v", err)
		return
	}
	if up == nil {
		if msg != "" {
			a.logf("debug", "no update: %s", msg)
		}
		return
	}
	if up.Version == version.Version {
		return
	}
	a.logf("info", "update available: %s -> %s (%d bytes)", version.Version, up.Version, up.SizeBytes)
	if err := a.applyUpdate(ctx, up); err != nil {
		a.logf("error", "update failed: %v", err)
		return
	}
	a.logf("info", "updated to %s; restarting so the service manager starts the new build", up.Version)
	// Give the log a moment to flush, then leave so the supervisor restarts us.
	time.AfterFunc(700*time.Millisecond, func() { os.Exit(0) })
}

func (a *Agent) applyUpdate(ctx context.Context, up *client.UpdateInfo) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	staging := exe + ".update"
	f, err := os.OpenFile(staging, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", staging, err)
	}
	hasher := sha256.New()
	n, dlErr := io.Copy(io.MultiWriter(f, hasher), &updateStream{a: a, ctx: ctx, platform: up.Platform, arch: up.Arch})
	if err := f.Close(); err != nil && dlErr == nil {
		dlErr = err
	}
	if dlErr != nil {
		_ = os.Remove(staging)
		return fmt.Errorf("download failed: %w", dlErr)
	}
	if up.SizeBytes > 0 && n != up.SizeBytes {
		_ = os.Remove(staging)
		return fmt.Errorf("size mismatch: got %d bytes, expected %d", n, up.SizeBytes)
	}
	if sum := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(sum, up.SHA256) {
		_ = os.Remove(staging)
		return fmt.Errorf("checksum mismatch: got %s, expected %s", sum, up.SHA256)
	}
	if err := swapExecutable(exe, staging); err != nil {
		_ = os.Remove(staging)
		return err
	}
	return nil
}

// updateStream downloads the binary into a pipe so io.Copy can hash it while it
// is written to disk.
type updateStream struct {
	a        *Agent
	ctx      context.Context
	platform string
	arch     string
	pr       *io.PipeReader
}

func (s *updateStream) Read(p []byte) (int, error) {
	if s.pr == nil {
		pr, pw := io.Pipe()
		s.pr = pr
		go func() {
			err := s.a.client.DownloadUpdate(s.ctx, s.platform, s.arch, pw)
			_ = pw.CloseWithError(err)
		}()
	}
	return s.pr.Read(p)
}
