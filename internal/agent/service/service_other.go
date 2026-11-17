//go:build !windows && !linux && !darwin

package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

func IsWindowsService() bool { return false }

func RunService(run func(ctx context.Context) error, log *slog.Logger) error {
	return fmt.Errorf("service mode not supported on this platform")
}

func Install(exePath, configPath string) error { return ErrNotSupported }
func Uninstall() error                         { return ErrNotSupported }
func Control(action string) error              { return ErrNotSupported }
func Query() (string, error)                   { return "unsupported", nil }
func ConfigDir() string                        { return os.TempDir() }
