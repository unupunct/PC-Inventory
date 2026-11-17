// Command pcim-agent is the PC Inventory Manager agent. It collects hardware
// inventory and performance metrics from the host and reports them to the
// central server over outbound HTTP(S).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	agentrt "github.com/pcim/pc-inventory-manager/internal/agent"
	"github.com/pcim/pc-inventory-manager/internal/agent/collect"
	"github.com/pcim/pc-inventory-manager/internal/agent/config"
	"github.com/pcim/pc-inventory-manager/internal/agent/service"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	if p := os.Getenv("PCIM_AGENT_CONFIG"); p != "" {
		return p
	}
	dir := service.ConfigDir()
	return filepath.Join(dir, "agent.json")
}

func run() error {
	args := os.Args[1:]
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("pcim-agent %s (protocol %d, %s/%s)\n", version.Version, version.Protocol, runtime.GOOS, runtime.GOARCH)
		return nil
	case "run", "start":
		return cmdRun(args)
	case "status":
		return cmdStatus(args)
	case "config":
		return cmdConfig(args)
	case "inventory":
		return cmdInventory(args)
	case "enroll":
		return cmdEnroll(args)
	case "service":
		return cmdService(args)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Print(`pcim-agent - PC Inventory Manager agent

Usage:
  pcim-agent run [--config PATH]          Run the agent in the foreground
  pcim-agent enroll --server ADDR [--code CODE]
                                          Register this computer with a server
  pcim-agent status [--config PATH]       Show agent status and connectivity
  pcim-agent inventory [--json]           Print this computer's inventory
  pcim-agent config [--config PATH] [--server ADDR] [--interval 30s]
                                          Show or change local settings
  pcim-agent service install|uninstall|start|stop|restart|status
                                          Manage the background service
  pcim-agent version                      Print the version

The agent only makes outbound connections to the server.
`)
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}

func loadAgent(args []string) (*agentrt.Agent, string, error) {
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to agent.json")
	logLevel := fs.String("log-level", "", "override log level (debug, info, warn, error)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return nil, *cfgPath, err
	}
	level := cfg.LogLevel
	if *logLevel != "" {
		level = *logLevel
	}
	log := newLogger(level)
	dataDir := filepath.Dir(*cfgPath)
	a, err := agentrt.New(cfg, *cfgPath, dataDir, log)
	if err != nil {
		return nil, *cfgPath, err
	}
	return a, *cfgPath, nil
}

func cmdRun(args []string) error {
	// Service mode (Windows SCM) has no flags of its own.
	isSvc := false
	for _, a := range args {
		if a == "--service" || a == "-service" {
			isSvc = true
		}
	}
	a, _, err := loadAgent(args)
	if err != nil {
		return err
	}
	if a.Cfg.ServerURL == "" {
		return fmt.Errorf("no server configured; run: pcim-agent enroll --server <address>")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if isSvc && service.IsWindowsService() {
		a.Log.Info("running as a Windows service")
		return service.RunService(func(ctx context.Context) error { return a.Run(ctx) }, a.Log)
	}

	err = a.Run(ctx)
	if err != nil && ctx.Err() == nil {
		return err
	}
	a.Log.Info("agent stopped")
	return nil
}

func cmdStatus(args []string) error {
	a, cfgPath, err := loadAgent(args)
	if err != nil {
		return err
	}
	st := a.Status()
	st["configPath"] = cfgPath
	svc, _ := service.Query()
	st["service"] = svc
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(st)
}

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to agent.json")
	server := fs.String("server", "", "server address (host, host:port or URL)")
	heartbeat := fs.Int("heartbeat", 0, "heartbeat interval in seconds")
	metrics := fs.Int("metrics", 0, "metrics interval in seconds")
	inventory := fs.Int("inventory", 0, "inventory interval in seconds")
	insecure := fs.Bool("insecure", false, "accept self-signed TLS certificates")
	logLevel := fs.String("log-level", "", "log level")
	show := fs.Bool("show", false, "print the current configuration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	changed := false
	if *server != "" {
		u, err := config.NormalizeServerURL(*server)
		if err != nil {
			return err
		}
		cfg.ServerURL = u
		// A new server means the old credentials no longer apply.
		cfg.AgentID, cfg.AgentToken = "", ""
		changed = true
	}
	if *heartbeat > 0 {
		cfg.HeartbeatSec, changed = *heartbeat, true
	}
	if *metrics > 0 {
		cfg.MetricsSec, changed = *metrics, true
	}
	if *inventory > 0 {
		cfg.InventorySec, changed = *inventory, true
	}
	if *insecure {
		cfg.InsecureTLS, changed = true, true
	}
	if *logLevel != "" {
		cfg.LogLevel, changed = *logLevel, true
	}
	if changed {
		if err := cfg.Save(*cfgPath); err != nil {
			return err
		}
		fmt.Println("configuration saved to", *cfgPath)
	}
	if !changed || *show {
		b, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(b))
	}
	return nil
}

func cmdInventory(args []string) error {
	fs := flag.NewFlagSet("inventory", flag.ContinueOnError)
	asJSON := fs.Bool("json", true, "output JSON")
	cfgPath := fs.String("config", defaultConfigPath(), "path to agent.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	col := collect.New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	inv := col.Inventory(ctx)
	if !*asJSON {
		fmt.Printf("%s (%s %s)\n", inv.Host.Hostname, inv.OS.Name, inv.OS.Version)
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(inv)
}

func cmdEnroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to agent.json")
	server := fs.String("server", "", "server address")
	code := fs.String("code", "", "optional enrollment code")
	displayName := fs.String("name", "", "display name for this computer")
	insecure := fs.Bool("insecure", false, "accept self-signed TLS certificates")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return fmt.Errorf("--server is required (e.g. --server 192.168.1.100:8080)")
	}
	u, err := config.NormalizeServerURL(*server)
	if err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	cfg.ServerURL = u
	cfg.InsecureTLS = *insecure
	if *displayName != "" {
		cfg.DisplayName = *displayName
	}
	if err := cfg.Save(*cfgPath); err != nil {
		return err
	}
	a, _, err := loadAgent([]string{"--config", *cfgPath})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := a.Enroll(ctx, *code); err != nil {
		return err
	}
	fmt.Println("enrolled; agent id:", a.AgentID())
	fmt.Println("configuration:", *cfgPath)
	return nil
}

func cmdService(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pcim-agent service install|uninstall|start|stop|restart|status")
	}
	action := args[0]
	switch action {
	case "status":
		st, err := service.Query()
		if err != nil {
			return err
		}
		fmt.Println(st)
		return nil
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cfgPath := defaultConfigPath()
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		cp := fs.String("config", cfgPath, "path to agent.json")
		_ = fs.Parse(args[1:])
		if err := service.Install(exe, *cp); err != nil {
			return err
		}
		fmt.Println("service installed and started")
		return nil
	case "uninstall":
		if err := service.Uninstall(); err != nil {
			return err
		}
		fmt.Println("service removed")
		return nil
	case "start", "stop", "restart":
		if err := service.Control(action); err != nil {
			return err
		}
		fmt.Println("service", action, "requested")
		return nil
	}
	return fmt.Errorf("unknown service action %q", action)
}
