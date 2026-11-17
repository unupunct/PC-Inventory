//go:build windows

package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

// IsWindowsService reports whether we were started by the service manager.
func IsWindowsService() bool {
	inService, err := svc.IsWindowsService()
	return err == nil && inService
}

type handler struct {
	run func(ctx context.Context) error
	log *slog.Logger
}

func (h *handler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	elog, _ := eventlog.Open(Name)
	defer func() {
		if elog != nil {
			elog.Close()
		}
	}()
	if elog != nil {
		elog.Info(1, "agent service starting")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- h.run(ctx) }()
	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				if elog != nil {
					elog.Info(1, "agent service stopping")
				}
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				return false, 0
			}
		case err := <-errCh:
			if err != nil {
				if elog != nil {
					elog.Error(1, "agent stopped with error: "+err.Error())
				}
				h.log.Error("agent stopped with error", "err", err)
				return true, 1
			}
			return false, 0
		}
	}
}

// RunService runs the agent inside the Windows service control manager.
func RunService(run func(ctx context.Context) error, log *slog.Logger) error {
	return svc.Run(Name, &handler{run: run, log: log})
}

// Install registers and starts the service pointing at exePath.
func Install(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator): %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(Name); err == nil {
		s.Close()
		return fmt.Errorf("service %q is already installed", Name)
	}
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return err
	}
	s, err := m.CreateService(Name, abs, mgr.Config{
		DisplayName:  DisplayName,
		Description:  Description,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, "--service", "--config", configPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	// Automatic restart on crash: restart 5s, 10s, 30s (capped).
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	if err := eventlog.InstallAsEventCreate(Name, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		// Non-fatal: logging still goes to file.
		_ = err
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("service installed but failed to start: %w", err)
	}
	return nil
}

// Uninstall stops and removes the service.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed", Name)
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	// Wait up to 20 seconds for a clean stop.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	_ = eventlog.Remove(Name)
	return nil
}

// Control sends a start/stop/restart command to the service.
func Control(action string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q is not installed", Name)
	}
	defer s.Close()
	switch action {
	case "start":
		return s.Start()
	case "stop":
		_, err = s.Control(svc.Stop)
		return err
	case "restart":
		_, _ = s.Control(svc.Stop)
		time.Sleep(2 * time.Second)
		return s.Start()
	}
	return fmt.Errorf("unknown action %q", action)
}

// Query returns the current service state.
func Query() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "", err
	}
	defer m.Disconnect()
	s, err := m.OpenService(Name)
	if err != nil {
		return "not installed", nil
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return "", err
	}
	switch st.State {
	case svc.Stopped:
		return "stopped", nil
	case svc.StartPending:
		return "starting", nil
	case svc.StopPending:
		return "stopping", nil
	case svc.Running:
		return "running", nil
	case svc.Paused:
		return "paused", nil
	}
	return "unknown", nil
}

// ConfigDir returns the machine-wide configuration directory.
func ConfigDir() string {
	if p := os.Getenv("ProgramData"); p != "" {
		return filepath.Join(p, "PCInventoryManager", "Agent")
	}
	return `C:\ProgramData\PCInventoryManager\Agent`
}

var _ = strings.TrimSpace
