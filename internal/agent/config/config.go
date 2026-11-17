package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Config is the on-disk agent configuration (agent.json).
type Config struct {
	ServerURL       string `json:"serverUrl"`
	ServerName      string `json:"serverName,omitempty"`
	AgentID         string `json:"agentId,omitempty"`
	MachineID       string `json:"machineId"`
	AgentToken      string `json:"agentToken,omitempty"`
	Hostname        string `json:"hostname,omitempty"`
	DisplayName     string `json:"displayName,omitempty"`
	HeartbeatSec    int    `json:"heartbeatSec"`
	MetricsSec      int    `json:"metricsSec"`
	InventorySec    int    `json:"inventorySec"`
	CollectSoftware bool   `json:"collectSoftware"`
	CollectMonitors bool   `json:"collectMonitors"`
	CollectUSB      bool   `json:"collectUsb"`
	CollectPrinters bool   `json:"collectPrinters"`
	CollectProcesses bool  `json:"collectProcesses"`
	CollectPerCoreCPU bool `json:"collectPerCoreCpu"`
	CollectTemperatures bool `json:"collectTemperatures"`
	CollectInstalledApps bool `json:"collectInstalledApps"`
	CollectWindowsUpdates bool `json:"collectWindowsUpdates"`
	CollectLoggedInUser bool `json:"collectLoggedInUser"`
	ConfigVersion   int    `json:"configVersion"`
	InsecureTLS     bool   `json:"insecureTls"`
	LogLevel        string `json:"logLevel"`
	EnrolledAt      *time.Time `json:"enrolledAt,omitempty"`
	LastConfigSync  *time.Time `json:"lastConfigSync,omitempty"`
	LastSeenServer  *time.Time `json:"lastSeenServer,omitempty"`
}

func Default() Config {
	return Config{
		HeartbeatSec:          30,
		MetricsSec:            30,
		InventorySec:          6 * 3600,
		CollectSoftware:       true,
		CollectMonitors:       true,
		CollectUSB:            true,
		CollectPrinters:       true,
		CollectProcesses:      true,
		CollectPerCoreCPU:     true,
		CollectTemperatures:   true,
		CollectInstalledApps:  true,
		CollectWindowsUpdates: true,
		CollectLoggedInUser:   false,
		ConfigVersion:         0,
		LogLevel:              "info",
	}
}

// NormalizeServerURL accepts "192.168.1.100", "192.168.1.100:8080" or a full
// URL and returns a base URL.
func NormalizeServerURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("server address is required")
	}
	if !strings.Contains(s, "://") {
		if _, _, err := net.SplitHostPort(s); err != nil {
			s += ":8080"
		}
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q (use http or https)", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("server address has no host")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}

// MachineID derives a stable identifier from OS-level identity so a reinstall
// of the agent (or a lost config) re-enrols the same computer.
func MachineID(hostname string) string {
	seeds := []string{"hostname=" + hostname}
	switch runtime.GOOS {
	case "windows":
		if v := regValue(`HKLM\SOFTWARE\Microsoft\Cryptography`, "MachineGuid"); v != "" {
			seeds = append(seeds, "machineguid="+v)
		}
	case "darwin":
		if v := runCmd("ioreg", "-rd1", "-c", "IOPlatformExpertDevice"); v != "" {
			if id := extractQuoted(v, "IOPlatformUUID"); id != "" {
				seeds = append(seeds, "platformuuid="+id)
			}
		}
	case "linux":
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil {
				seeds = append(seeds, "machine-id="+strings.TrimSpace(string(b)))
				break
			}
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(seeds, "|")))
	return hex.EncodeToString(sum[:16])
}

// Load reads the config file, creating it with defaults if absent.
func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return cfg, err
		}
		host, _ := os.Hostname()
		cfg.Hostname = host
		cfg.MachineID = MachineID(host)
		return cfg, nil
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.MachineID == "" {
		host, _ := os.Hostname()
		cfg.Hostname = host
		cfg.MachineID = MachineID(host)
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	return cfg, nil
}

// Save writes the config atomically.
func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	// Keep a backup of the previous version in case of corruption.
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		_ = os.WriteFile(path+".bak", old, 0o600)
	}
	return os.Rename(tmp, path)
}

// EffectiveConfig applies server-controlled values onto the local config.
func (c *Config) ApplyServer(v map[string]any) bool {
	changed := false
	setInt := func(key string, dst *int, min, max int) {
		if vv, ok := v[key]; ok {
			if f, ok := vv.(float64); ok {
				n := int(f)
				if n >= min && n <= max && n != *dst {
					*dst = n
					changed = true
				}
			}
		}
	}
	setBool := func(key string, dst *bool) {
		if vv, ok := v[key]; ok {
			if b, ok := vv.(bool); ok && b != *dst {
				*dst = b
				changed = true
			}
		}
	}
	setInt("heartbeatIntervalSec", &c.HeartbeatSec, 10, 3600)
	setInt("metricsIntervalSec", &c.MetricsSec, 5, 3600)
	setInt("inventoryIntervalSec", &c.InventorySec, 300, 7*24*3600)
	setBool("collectSoftware", &c.CollectSoftware)
	setBool("collectMonitors", &c.CollectMonitors)
	setBool("collectUsb", &c.CollectUSB)
	setBool("collectPrinters", &c.CollectPrinters)
	setBool("collectProcesses", &c.CollectProcesses)
	setBool("collectPerCoreCpu", &c.CollectPerCoreCPU)
	setBool("collectTemperatures", &c.CollectTemperatures)
	setBool("collectInstalledApps", &c.CollectInstalledApps)
	setBool("collectWindowsUpdates", &c.CollectWindowsUpdates)
	setBool("collectLoggedInUser", &c.CollectLoggedInUser)
	if vv, ok := v["configVersion"].(float64); ok && int(vv) != c.ConfigVersion {
		c.ConfigVersion = int(vv)
		changed = true
	}
	return changed
}
