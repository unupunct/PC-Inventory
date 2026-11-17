// Package agent contains the long-running agent loop: enrollment, heartbeat,
// metrics and inventory reporting, with local buffering when offline.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/agent/buffer"
	"github.com/pcim/pc-inventory-manager/internal/agent/client"
	"github.com/pcim/pc-inventory-manager/internal/agent/collect"
	"github.com/pcim/pc-inventory-manager/internal/agent/config"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

type Agent struct {
	Cfg      config.Config
	ConfigPath string
	DataDir  string
	Log      *slog.Logger

	client *client.Client
	buf    *buffer.Buffer
	col    *collect.Collector

	mu         sync.Mutex
	connected  bool
	lastError  string
	pendingCmd []models.AgentCommand
	logQueue   []models.LogEntry
	logMu      sync.Mutex

	stateMu      sync.Mutex
	lastInventory time.Time
	lastMetrics   time.Time
	nextInventory time.Time

	// Metrics are paced by a ticker so usage is measured over the interval.
	runStarted time.Time
}

func New(cfg config.Config, cfgPath, dataDir string, log *slog.Logger) (*Agent, error) {
	buf, err := buffer.New(filepathJoin(dataDir, "buffer"), 5000)
	if err != nil {
		return nil, err
	}
	return &Agent{
		Cfg: cfg, ConfigPath: cfgPath, DataDir: dataDir, Log: log,
		buf: buf, col: collect.New(cfg),
	}, nil
}

func filepathJoin(a, b string) string {
	if strings.HasSuffix(a, "/") || strings.HasSuffix(a, "\\") {
		return a + b
	}
	return a + string(os.PathSeparator) + b
}

// logf writes to the logger and queues the entry for the server.
func (a *Agent) logf(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	switch level {
	case "error":
		a.Log.Error(msg)
	case "warn":
		a.Log.Warn(msg)
	case "debug":
		a.Log.Debug(msg)
	default:
		a.Log.Info(msg)
	}
	a.logMu.Lock()
	a.logQueue = append(a.logQueue, models.LogEntry{Time: time.Now().UTC(), Level: level, Message: msg})
	if len(a.logQueue) > 200 {
		a.logQueue = a.logQueue[len(a.logQueue)-200:]
	}
	a.logMu.Unlock()
}

func (a *Agent) takeLogs() []models.LogEntry {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	out := a.logQueue
	a.logQueue = nil
	return out
}

// Run starts the agent loop until the context is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	if a.Cfg.ServerURL == "" {
		return errors.New("no server address configured (run: pcim-agent config --server <address>)")
	}
	a.col.SetConfig(a.Cfg)
	a.logf("info", "agent %s starting (version %s, machine %s)", hostnameOr(a.Cfg.Hostname), version.Version, a.Cfg.MachineID)

	// Enroll / resume.
	for {
		if err := a.ensureEnrolled(ctx); err == nil {
			break
		} else {
			a.logf("warn", "enrollment pending or failed: %v (retrying in 20s)", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Second):
		}
	}

	a.client = client.New(a.Cfg.ServerURL, a.Cfg.AgentID, a.Cfg.AgentToken, a.Cfg.InsecureTLS, "")
	a.connected = true
	a.logf("info", "connected to server %s as agent %s", a.Cfg.ServerURL, a.Cfg.AgentID)

	// Send an initial inventory on startup, then follow the schedule.
	go a.loopHeartbeat(ctx)
	go a.loopMetrics(ctx)
	go a.loopInventory(ctx)
	go a.loopFlush(ctx)
	<-ctx.Done()
	return nil
}

func hostnameOr(s string) string {
	if s != "" {
		return s
	}
	h, _ := os.Hostname()
	return h
}

// ensureEnrolled contacts the server and completes enrollment if needed.
func (a *Agent) ensureEnrolled(ctx context.Context) error {
	base, err := config.NormalizeServerURL(a.Cfg.ServerURL)
	if err != nil {
		return err
	}
	a.Cfg.ServerURL = base
	c := client.New(base, a.Cfg.AgentID, a.Cfg.AgentToken, a.Cfg.InsecureTLS, "")

	// Fast path: we already have credentials and they still work. Any failure
	// here (offline, timeout) is reported and retried by the caller.
	if a.Cfg.AgentID != "" && a.Cfg.AgentToken != "" {
		cfg, err := c.Config(ctx)
		if err == nil {
			a.applyServerConfig(cfg)
			a.client = c
			return nil
		}
		if !errors.Is(err, client.ErrUnauthorized) {
			return err
		}
		// A rejected token is recoverable: poll the enrollment endpoint, which
		// hands back a token if an administrator approved us in the meantime.
		a.logf("warn", "stored token rejected by server; asking for a new one")
		a.Cfg.AgentToken = ""
	}

	inv := a.col.Inventory(ctx)
	req := models.EnrollmentRequest{
		MachineID: a.Cfg.MachineID, Hostname: inv.Host.Hostname, Platform: inv.Host.Platform,
		OSName: inv.OS.Name, OSVersion: inv.OS.Version, Arch: inv.OS.Arch,
		AgentVersion: version.Version, Protocol: version.Protocol,
		PrimaryIP: primaryIPv4(inv), PrimaryMAC: primaryMAC(inv),
	}
	resp, status, err := c.Enroll(ctx, req, "")
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("unexpected enrollment response (status %d)", status)
	}
	switch resp.Status {
	case "approved":
		if resp.AgentToken == "" {
			return fmt.Errorf("%s", resp.Message)
		}
		a.Cfg.AgentID = resp.AgentID
		a.Cfg.AgentToken = resp.AgentToken
		a.Cfg.ServerName = resp.ServerName
		now := time.Now().UTC()
		a.Cfg.EnrolledAt = &now
		// The enrollment response carries the effective server-side config.
		if resp.Config != nil {
			a.applyServerConfig(configMap(resp.Config))
		}
		if err := a.Cfg.Save(a.ConfigPath); err != nil {
			a.logf("error", "cannot save credentials: %v", err)
		}
		a.logf("info", "enrollment approved; agent id %s", resp.AgentID)
		return nil
	case "pending":
		return fmt.Errorf("waiting for administrator approval (hostname %s)", req.Hostname)
	default:
		msg := resp.Message
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("%s", msg)
	}
}

func configMap(c *models.AgentConfig) map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func (a *Agent) applyServerConfig(m map[string]any) {
	if m == nil {
		return
	}
	a.mu.Lock()
	cfg := a.Cfg
	changed := cfg.ApplyServer(m)
	now := time.Now().UTC()
	cfg.LastConfigSync = &now
	a.Cfg = cfg
	a.mu.Unlock()
	a.col.SetConfig(cfg)
	if changed {
		_ = cfg.Save(a.ConfigPath)
		a.logf("info", "server configuration applied: heartbeat %ds, metrics %ds, inventory %ds",
			cfg.HeartbeatSec, cfg.MetricsSec, cfg.InventorySec)
	}
}

func (a *Agent) getCfg() config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Cfg
}

func (a *Agent) setConnected(ok bool, errMsg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.connected != ok {
		if ok {
			a.logf("info", "connection to server restored")
		} else {
			a.logf("warn", "connection to server lost: %s", errMsg)
		}
	}
	a.connected = ok
	a.lastError = errMsg
}

func (a *Agent) loopHeartbeat(ctx context.Context) {
	for {
		cfg := a.getCfg()
		a.sendHeartbeat(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(maxInt(cfg.HeartbeatSec, 5)) * time.Second):
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context) {
	cfg := a.getCfg()
	if a.client == nil {
		return
	}
	req := models.HeartbeatRequest{
		AgentID: cfg.AgentID, SentAt: time.Now().UTC(), AgentVersion: version.Version,
		PendingBuffered: a.buf.Len(), LastError: a.lastError,
	}
	if h, err := os.Hostname(); err == nil {
		req.Hostname = h
	}
	hctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := a.client.Heartbeat(hctx, req)
	if err != nil {
		a.setConnected(false, err.Error())
		return
	}
	a.setConnected(true, "")
	now := time.Now().UTC()
	a.mu.Lock()
	a.Cfg.LastSeenServer = &now
	a.mu.Unlock()
	if resp.Config != nil {
		a.applyServerConfig(configMap(resp.Config))
	}
	for _, cmd := range resp.Commands {
		a.handleCommand(cmd)
	}
	// Flush queued logs opportunistically.
	if logs := a.takeLogs(); len(logs) > 0 {
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := a.client.Logs(lctx, models.LogsRequest{AgentID: cfg.AgentID, Entries: logs}); err != nil {
			// Put them back for a later attempt.
			a.logMu.Lock()
			a.logQueue = append(logs, a.logQueue...)
			a.logMu.Unlock()
		}
		cancel()
	}
}

func (a *Agent) handleCommand(cmd models.AgentCommand) {
	switch cmd.Type {
	case "inventory_refresh":
		a.logf("info", "inventory refresh requested by server")
		go a.sendInventory(context.Background(), "manual")
	case "rotate_token":
		a.logf("info", "server requested token rotation")
		// The new token is delivered through the config endpoint.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if m, err := a.client.Config(ctx); err == nil {
			if t, ok := m["agentToken"].(string); ok && t != "" {
				a.mu.Lock()
				a.Cfg.AgentToken = t
				cfg := a.Cfg
				a.mu.Unlock()
				_ = cfg.Save(a.ConfigPath)
				a.client.Token = t
				a.logf("info", "rotated API token")
			}
		}
	default:
		a.logf("debug", "ignoring unknown command %q", cmd.Type)
	}
}

func (a *Agent) loopMetrics(ctx context.Context) {
	// Wait one interval first so the first sample has a real delta.
	cfg := a.getCfg()
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(maxInt(cfg.MetricsSec, 5)) * time.Second):
	}
	for {
		a.sendMetrics(ctx)
		cfg := a.getCfg()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(maxInt(cfg.MetricsSec, 5)) * time.Second):
		}
	}
}

func (a *Agent) sendMetrics(ctx context.Context) {
	cfg := a.getCfg()
	if a.client == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	m := a.col.Metrics(cctx)
	cancel()
	req := models.MetricsRequest{AgentID: cfg.AgentID, Metrics: m}
	rctx, rcancel := context.WithTimeout(ctx, 30*time.Second)
	defer rcancel()
	if err := a.client.Metrics(rctx, req); err != nil {
		a.setConnected(false, err.Error())
		if err := a.buf.Enqueue("metrics", req); err != nil {
			a.logf("error", "cannot buffer metrics: %v", err)
		}
		return
	}
	a.setConnected(true, "")
}

func (a *Agent) loopInventory(ctx context.Context) {
	// Initial inventory shortly after start.
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Second):
	}
	a.sendInventory(ctx, "startup")
	for {
		cfg := a.getCfg()
		wait := time.Duration(maxInt(cfg.InventorySec, 300)) * time.Second
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			a.sendInventory(ctx, "scheduled")
		}
	}
}

func (a *Agent) sendInventory(ctx context.Context, reason string) {
	cfg := a.getCfg()
	if a.client == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	inv := a.col.Inventory(cctx)
	cancel()
	req := models.InventoryRequest{AgentID: cfg.AgentID, Inventory: inv, Reason: reason}
	rctx, rcancel := context.WithTimeout(ctx, 120*time.Second)
	defer rcancel()
	resp, err := a.client.Inventory(rctx, req)
	if err != nil {
		a.setConnected(false, err.Error())
		// Always buffer inventory; it is small and the most valuable data.
		if berr := a.buf.Enqueue("inventory", req); berr != nil {
			a.logf("error", "cannot buffer inventory: %v", berr)
		}
		return
	}
	a.setConnected(true, "")
	a.stateMu.Lock()
	a.lastInventory = time.Now()
	a.stateMu.Unlock()
	if resp != nil && resp.Changed {
		a.logf("info", "inventory sent (%s): hardware changed", reason)
	} else {
		a.logf("info", "inventory sent (%s)", reason)
	}
}

// loopFlush retries buffered reports when connectivity returns.
func (a *Agent) loopFlush(ctx context.Context) {
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.flush(ctx)
		}
	}
}

func (a *Agent) flush(ctx context.Context) {
	if a.client == nil {
		return
	}
	items := a.buf.List()
	if len(items) == 0 {
		return
	}
	cfg := a.getCfg()
	sent := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return
		}
		var err error
		switch it.Kind {
		case "metrics":
			var req models.MetricsRequest
			if json.Unmarshal(it.Payload, &req) == nil {
				req.AgentID = cfg.AgentID
				fctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				err = a.client.Metrics(fctx, req)
				cancel()
			}
		case "inventory":
			var req models.InventoryRequest
			if json.Unmarshal(it.Payload, &req) == nil {
				req.AgentID = cfg.AgentID
				fctx, cancel := context.WithTimeout(ctx, 60*time.Second)
				_, err = a.client.Inventory(fctx, req)
				cancel()
			}
		}
		if err != nil {
			if errors.Is(err, client.ErrUnauthorized) {
				// Credentials invalid; stop flushing until re-enrollment.
				return
			}
			// Still offline: stop this round.
			return
		}
		_ = a.buf.Remove(it)
		sent++
	}
	if sent > 0 {
		a.logf("info", "flushed %d buffered report(s); %d remaining", sent, a.buf.Len())
	}
}

// Status returns a snapshot for the CLI.
func (a *Agent) Status() map[string]any {
	cfg := a.getCfg()
	a.mu.Lock()
	connected, lastErr := a.connected, a.lastError
	a.mu.Unlock()
	a.stateMu.Lock()
	lastInv := a.lastInventory
	a.stateMu.Unlock()
	return map[string]any{
		"server":       cfg.ServerURL,
		"agentId":      cfg.AgentID,
		"machineId":    cfg.MachineID,
		"hostname":     hostnameOr(cfg.Hostname),
		"platform":     runtime.GOOS + "/" + runtime.GOARCH,
		"version":      version.Version,
		"enrolled":     cfg.AgentToken != "",
		"connected":    connected,
		"lastError":    lastErr,
		"buffered":     a.buf.Len(),
		"lastInventory": lastInv,
		"intervals":    map[string]int{"heartbeatSec": cfg.HeartbeatSec, "metricsSec": cfg.MetricsSec, "inventorySec": cfg.InventorySec},
	}
}

func primaryIPv4(inv models.Inventory) string {
	for _, n := range inv.NetAdapters {
		if n.Kind == "loopback" || n.IsVirtual {
			continue
		}
		if n.Status == "up" && len(n.IPv4) > 0 {
			return n.IPv4[0]
		}
	}
	for _, n := range inv.NetAdapters {
		if len(n.IPv4) > 0 && n.Kind != "loopback" {
			return n.IPv4[0]
		}
	}
	return ""
}

func primaryMAC(inv models.Inventory) string {
	for _, n := range inv.NetAdapters {
		if n.Kind == "loopback" || n.IsVirtual {
			continue
		}
		if n.Status == "up" && n.MAC != "" {
			return n.MAC
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Enroll performs a one-shot enrollment (used by the CLI) and reports whether
// the server accepted this computer immediately.
func (a *Agent) Enroll(ctx context.Context, code string) error {
	if err := a.ensureEnrolled(ctx); err != nil {
		return err
	}
	return nil
}

// AgentID returns the enrolled agent id, if any.
func (a *Agent) AgentID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.Cfg.AgentID
}
