//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serverServiceName = "pcim-server"
	serverDisplayName = "PC Inventory Manager Server"
	serverDescription = "Self-hosted inventory and monitoring server. Serves the dashboard and receives reports from agents."
)

// isServiceMode reports whether the process was launched by the SCM.
func isServiceMode(args []string) bool {
	for _, a := range args {
		if a == "--service" || a == "-service" {
			return true
		}
	}
	inService, err := svc.IsWindowsService()
	return err == nil && inService
}

type serverServiceHandler struct {
	run func(ctx context.Context) error
	log *slog.Logger
}

func (h *serverServiceHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	elog, _ := eventlog.Open(serverServiceName)
	defer func() {
		if elog != nil {
			elog.Close()
		}
	}()
	if elog != nil {
		elog.Info(1, "server service starting")
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
					elog.Info(1, "server service stopping")
				}
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				return false, 0
			}
		case err := <-errCh:
			if err != nil {
				if elog != nil {
					elog.Error(1, "server stopped with error: "+err.Error())
				}
				h.log.Error("server stopped with error", "err", err)
				return true, 1
			}
			return false, 0
		}
	}
}

// runAsService runs the server under the service control manager.
func runAsService(run func(ctx context.Context) error, log *slog.Logger) error {
	return svc.Run(serverServiceName, &serverServiceHandler{run: run, log: log})
}

// serverDataDir is the machine-wide data directory for the service.
func serverDataDir() string {
	if p := os.Getenv("ProgramData"); p != "" {
		return filepath.Join(p, "PCInventoryManager", "Server")
	}
	return `C:\ProgramData\PCInventoryManager\Server`
}

// serverConfigPath is the machine-wide env file for the service.
func serverConfigPath() string {
	return filepath.Join(serverDataDir(), "server.env")
}

func installServerService(exePath, envPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator): %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serverServiceName); err == nil {
		s.Close()
		return fmt.Errorf("service %q is already installed", serverServiceName)
	}
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return err
	}
	args := []string{"--service"}
	if envPath != "" {
		args = append(args, "--env", envPath)
	}
	s, err := m.CreateService(serverServiceName, abs, mgr.Config{
		DisplayName:  serverDisplayName,
		Description:  serverDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, args...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10_000_000_000},
	}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	_ = eventlog.InstallAsEventCreate(serverServiceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err := s.Start(); err != nil {
		return fmt.Errorf("service installed but failed to start: %w", err)
	}
	return nil
}

func uninstallServerService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serverServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", serverServiceName)
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	_ = eventlog.Remove(serverServiceName)
	return nil
}

// serverServiceState reports the state of the Windows service.
func serverServiceState() string {
	m, err := mgr.Connect()
	if err != nil {
		return "unknown"
	}
	defer m.Disconnect()
	s, err := m.OpenService(serverServiceName)
	if err != nil {
		return "not installed"
	}
	defer s.Close()
	st, err := s.Query()
	if err != nil {
		return "unknown"
	}
	switch st.State {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "starting"
	case svc.StopPending:
		return "stopping"
	case svc.Running:
		return "running"
	}
	return "unknown"
}

// serverServiceCommand implements `pcim-server service <action>`.
func serverServiceCommand(action, envPath string) error {
	switch action {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(serverDataDir(), 0o750); err != nil {
			return err
		}
		if envPath != "" {
			if _, err := os.Stat(envPath); err != nil {
				sample := "# PC Inventory Manager server configuration.\n" +
					"# 127.0.0.1 = this computer only, 0.0.0.0 = accept agents on the LAN.\n" +
					"PCIM_LISTEN_HOST=0.0.0.0\nPCIM_PORT=8080\n"
				if werr := os.WriteFile(envPath, []byte(sample), 0o640); werr != nil {
					fmt.Fprintf(os.Stderr, "warning: could not write %s: %v\n", envPath, werr)
				} else {
					fmt.Printf("wrote %s\n", envPath)
				}
			}
		}
		if err := installServerService(exe, envPath); err != nil {
			return err
		}
		fmt.Println("service installed and started")
		fmt.Printf("data directory: %s\n", serverDataDir())
		fmt.Printf("configuration:  %s\n", envPath)
		return nil
	case "uninstall":
		if err := uninstallServerService(); err != nil {
			return err
		}
		fmt.Println("service removed")
		return nil
	case "start", "stop":
		return controlServerService(action)
	case "status":
		fmt.Println(serverServiceState())
		return nil
	case "restart", "":
		if action == "" {
			return fmt.Errorf("usage: pcim-server service install|uninstall|start|stop|restart|status")
		}
		if err := controlServerService("stop"); err != nil {
			return err
		}
		return controlServerService("start")
	}
	return fmt.Errorf("usage: pcim-server service install|uninstall|start|stop|restart|status")
}

func controlServerService(action string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as administrator): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serverServiceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed", serverServiceName)
	}
	defer s.Close()
	if action == "start" {
		if err := s.Start(); err != nil && !strings.Contains(err.Error(), "already running") {
			return fmt.Errorf("start service: %w", err)
		}
		return nil
	}
	status, err := s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	for status.State != svc.Stopped {
		time.Sleep(500 * time.Millisecond)
		status, err = s.Query()
		if err != nil {
			return err
		}
	}
	return nil
}

var _ io.Writer = os.Stdout
