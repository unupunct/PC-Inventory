//go:build darwin

package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const plistPath = "/Library/LaunchDaemons/com.pcim.agent.plist"

// IsWindowsService is always false on macOS.
func IsWindowsService() bool { return false }

// RunService runs the agent in the foreground; launchd supervises it.
func RunService(run func(ctx context.Context) error, log *slog.Logger) error {
	return fmt.Errorf("launchd manages the process; run the agent in the foreground instead")
}

// Install writes the launchd plist and loads it.
func Install(exePath, configPath string) error {
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return err
	}
	logDir := "/Library/Logs/PCInventoryManager"
	_ = os.MkdirAll(logDir, 0o755)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.pcim.agent</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
    <string>--config</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardOutPath</key><string>%s/pcim-agent.out.log</string>
  <key>StandardErrorPath</key><string>%s/pcim-agent.err.log</string>
</dict>
</plist>
`, abs, configPath, logDir, logDir)
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write launchd plist (run with sudo): %w", err)
	}
	_, _ = exec.Command("launchctl", "unload", plistPath).CombinedOutput()
	if out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %v: %s", err, out)
	}
	return nil
}

// Uninstall unloads and removes the plist.
func Uninstall() error {
	_, _ = exec.Command("launchctl", "unload", "-w", plistPath).CombinedOutput()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Control sends a start/stop/restart command.
func Control(action string) error {
	target := "system/com.pcim.agent"
	var args []string
	switch action {
	case "start":
		args = []string{"load", "-w", plistPath}
	case "stop":
		args = []string{"unload", "-w", plistPath}
	case "restart":
		_, _ = exec.Command("launchctl", "unload", "-w", plistPath).CombinedOutput()
		args = []string{"load", "-w", plistPath}
	default:
		_ = target
		return fmt.Errorf("unknown action %q", action)
	}
	out, err := exec.Command("launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", action, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Query returns the current service state.
func Query() (string, error) {
	if _, err := os.Stat(plistPath); err != nil {
		return "not installed", nil
	}
	out, _ := exec.Command("launchctl", "list").Output()
	if strings.Contains(string(out), "com.pcim.agent") {
		return "running", nil
	}
	return "loaded (not running)", nil
}

// ConfigDir returns the configuration directory.
func ConfigDir() string { return "/usr/local/etc/pcim-agent" }
