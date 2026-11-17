//go:build windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// swapExecutable replaces the running binary on Windows. A running .exe cannot
// be overwritten, so the current file is renamed aside, the new file is put in
// its place, and a short-lived hidden helper deletes the old one after exit.
func swapExecutable(exe, staging string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("cannot move current binary aside: %w", err)
	}
	if err := os.Rename(staging, exe); err != nil {
		// Put the old one back so the service can still start.
		_ = os.Rename(old, exe)
		return fmt.Errorf("cannot install new binary: %w", err)
	}
	// Delete the old binary once this process has exited.
	script := fmt.Sprintf(`for ($i = 0; $i -lt 60; $i++) { Start-Sleep -Milliseconds 500; try { Remove-Item -LiteralPath "%s" -Force -ErrorAction Stop; break } catch {} }`, old)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		// Not fatal: a leftover .old file is harmless and replaced next time.
		return nil
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
