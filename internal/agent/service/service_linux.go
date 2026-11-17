//go:build linux

package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"log/slog"
	"path/filepath"
	"strings"
)

const unitPath = "/etc/systemd/system/pcim-agent.service"

// IsWindowsService is always false on Linux.
func IsWindowsService() bool { return false }

// RunService runs the agent in the foreground; systemd supervises it.
func RunService(run func(ctx context.Context) error, log *slog.Logger) error {
	return fmt.Errorf("systemd manages the process; run the agent in the foreground instead")
}

// Install writes the systemd unit and enables it.
func Install(exePath, configPath string) error {
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return err
	}
	unit := fmt.Sprintf(`[Unit]
Description=%s
Documentation=https://github.com/pcim/pc-inventory-manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s run --config %s
Restart=always
RestartSec=10
User=root
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=read-only
PrivateTmp=true

[Install]
WantedBy=multi-user.target
`, Description, abs, configPath)
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write unit file (run as root): %w", err)
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %v: %s", err, out)
	}
	if out, err := exec.Command("systemctl", "enable", "--now", Name).CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl enable: %v: %s", err, out)
	}
	return nil
}

// Uninstall stops, disables and removes the unit.
func Uninstall() error {
	_, _ = exec.Command("systemctl", "disable", "--now", Name).CombinedOutput()
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, _ = exec.Command("systemctl", "daemon-reload").CombinedOutput()
	return nil
}

// Control sends a start/stop/restart command.
func Control(action string) error {
	var args []string
	switch action {
	case "start":
		args = []string{"start", Name}
	case "stop":
		args = []string{"stop", Name}
	case "restart":
		args = []string{"restart", Name}
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Query returns the current service state.
func Query() (string, error) {
	out, err := exec.Command("systemctl", "is-active", Name).Output()
	if err != nil {
		if _, statErr := os.Stat(unitPath); statErr != nil {
			return "not installed", nil
		}
		return strings.TrimSpace(string(out)), nil
	}
	return strings.TrimSpace(string(out)), nil
}

// ConfigDir returns the configuration directory.
func ConfigDir() string { return "/etc/pcim-agent" }
