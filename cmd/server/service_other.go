//go:build !windows

package main

import (
	"context"
	"fmt"
	"log/slog"
)

// On Linux and macOS the server runs in the foreground under systemd or
// launchd, which provides restart supervision.

func isServiceMode(args []string) bool { return false }

func runAsService(run func(ctx context.Context) error, log *slog.Logger) error { return nil }

func serverDataDir() string { return "" }

func serverConfigPath() string { return "" }

func installServerService(exePath, envPath string) error { return nil }

func uninstallServerService() error { return nil }

func serverServiceState() string { return "unsupervised" }

func serverServiceCommand(action, envPath string) error {
	return fmt.Errorf("service management is only available on Windows; on this platform run the server under systemd or launchd (see docs/INSTALLATION.md)")
}
