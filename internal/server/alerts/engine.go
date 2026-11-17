// Package alerts implements the alert engine: evaluation of rules against
// live data, cooldowns, recovery, and notification delivery.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/config"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

// Notifier sends an alert somewhere outside the app.
type Notifier interface {
	Name() string
	Send(ctx context.Context, a store.Alert, rule store.AlertRule) error
}

// Hub broadcasts events to SSE clients.
type Broadcaster interface {
	Broadcast(event string, payload any)
}

type Engine struct {
	st     *store.Store
	cfg    config.Config
	log    *slog.Logger
	hub    Broadcaster
	notifs []Notifier

	mu          sync.Mutex
	lastNotify  map[string]time.Time
	prevOffline map[string]bool
}

func New(st *store.Store, cfg config.Config, log *slog.Logger, hub Broadcaster, notifs ...Notifier) *Engine {
	return &Engine{st: st, cfg: cfg, log: log, hub: hub, notifs: notifs, lastNotify: map[string]time.Time{}, prevOffline: map[string]bool{}}
}

func (e *Engine) broadcast() {
	if e.hub != nil {
		e.hub.Broadcast("alerts", nil)
	}
}

// Raise opens (or refreshes) an alert for a rule/agent pair, respecting the
// cooldown for notifications. Returns the alert id.
func (e *Engine) Raise(ctx context.Context, ruleKey string, agentID *string, severity, title, message, details string) (int64, bool, error) {
	rule, err := e.st.GetAlertRule(ctx, ruleKey)
	if err != nil {
		return 0, false, err
	}
	if !rule.Enabled {
		return 0, false, nil
	}
	if rule.Severity != "" && ruleKey != store.RuleAgentOnline {
		severity = rule.Severity
	}
	existing, err := e.st.FindOpenAlert(ctx, ruleKey, agentID)
	if err != nil && err != store.ErrNotFound {
		return 0, false, err
	}
	var id int64
	notify := false
	if err == store.ErrNotFound {
		id, err = e.st.OpenAlert(ctx, ruleKey, agentID, severity, title, message, details)
		if err != nil {
			return 0, false, err
		}
		notify = true
		e.broadcast()
	} else {
		id = existing.ID
		_ = e.st.UpdateAlertMessage(ctx, id, message)
		if rule.CooldownSec > 0 && existing.LastNotified != nil && time.Since(*existing.LastNotified) < time.Duration(rule.CooldownSec)*time.Second {
			notify = false
		} else if existing.LastNotified == nil {
			notify = true
		}
	}
	if notify {
		alert, err := e.getAlert(ctx, id)
		if err == nil {
			e.deliver(ctx, *alert, *rule)
			_ = e.st.MarkAlertNotified(ctx, id)
		}
	}
	return id, notify, nil
}

func (e *Engine) getAlert(ctx context.Context, id int64) (*store.Alert, error) {
	list, err := e.st.ListAlerts(ctx, "", "", "", 1000)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, store.ErrNotFound
}

// Resolve resolves the newest open alert for a rule/agent pair.
func (e *Engine) Resolve(ctx context.Context, ruleKey string, agentID *string) error {
	a, err := e.st.FindOpenAlert(ctx, ruleKey, agentID)
	if err == store.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err := e.st.ResolveAlert(ctx, a.ID); err != nil {
		return err
	}
	e.broadcast()
	return nil
}

func (e *Engine) deliver(ctx context.Context, a store.Alert, rule store.AlertRule) {
	for _, n := range e.notifs {
		switch {
		case n.Name() == "email" && !rule.NotifyEmail:
			continue
		case n.Name() == "telegram" && !rule.NotifyTelegram:
			continue
		}
		nctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := n.Send(nctx, a, rule); err != nil {
			e.log.Warn("alert notification failed", "notifier", n.Name(), "err", err)
			_ = e.st.AddServerEvent(ctx, "warn", "alerts", fmt.Sprintf("%s notification failed: %v", n.Name(), err))
		}
		cancel()
	}
}

// Evaluate runs all rules against current state. Called periodically.
func (e *Engine) Evaluate(ctx context.Context) error {
	agents, err := e.st.ListAgents(ctx, "")
	if err != nil {
		return err
	}
	rules, err := e.st.ListAlertRules(ctx)
	if err != nil {
		return err
	}
	ruleByKey := map[string]store.AlertRule{}
	for _, r := range rules {
		ruleByKey[r.Key] = r
	}
	latest, _ := e.st.LatestPerfSamples(ctx)
	invRows, _ := e.st.ListInventoryRows(ctx, true)
	invByAgent := map[string]*store.InventoryRow{}
	for _, r := range invRows {
		invByAgent[r.AgentID] = r
	}
	now := time.Now().UTC()

	for _, a := range agents {
		id := a.ID
		name := a.DisplayName
		if name == "" {
			name = a.Hostname
		}
		offlineSec := e.cfg.OfflineThresholdSec
		if r, ok := ruleByKey[store.RuleAgentOffline]; ok && r.Threshold != nil && *r.Threshold > 0 {
			offlineSec = int(*r.Threshold)
		}
		if a.Status != store.StatusApproved {
			// Resolve connectivity alerts for non-approved agents.
			_ = e.Resolve(ctx, store.RuleAgentOffline, &id)
			_ = e.Resolve(ctx, store.RuleAgentSilent, &id)
			continue
		}
		var since time.Duration
		if a.LastHeartbeatAt != nil {
			since = now.Sub(*a.LastHeartbeatAt)
		} else {
			since = now.Sub(a.EnrolledAt)
		}
		offline := since > time.Duration(offlineSec)*time.Second
		if offline {
			msg := fmt.Sprintf("No heartbeat for %s (threshold %ds).", humanDur(since), offlineSec)
			if _, _, err := e.Raise(ctx, store.RuleAgentOffline, &id, "critical", "Agent offline: "+name, msg, ""); err != nil {
				e.log.Warn("raise offline alert", "err", err)
			}
		} else {
			if e.prevOffline[id] {
				// Recovery
				_ = e.st.AddAgentEvent(ctx, id, "online", "info", "Agent reconnected", "")
				if _, _, err := e.Raise(ctx, store.RuleAgentOnline, &id, "info", "Agent reconnected: "+name, "Heartbeats resumed.", ""); err != nil {
					e.log.Warn("raise online alert", "err", err)
				}
				// Immediately resolve the recovery alert so it is informational only
				go func(agentID string) {
					time.Sleep(2 * time.Second)
					_ = e.Resolve(context.Background(), store.RuleAgentOnline, &agentID)
				}(id)
			}
			_ = e.Resolve(ctx, store.RuleAgentOffline, &id)
		}
		e.prevOffline[id] = offline

		if r, ok := ruleByKey[store.RuleAgentSilent]; ok && r.Threshold != nil && *r.Threshold > 0 {
			last := a.EnrolledAt
			if a.LastHeartbeatAt != nil && a.LastHeartbeatAt.After(last) {
				last = *a.LastHeartbeatAt
			}
			if a.LastMetricsAt != nil && a.LastMetricsAt.After(last) {
				last = *a.LastMetricsAt
			}
			if now.Sub(last) > time.Duration(*r.Threshold)*time.Second {
				_, _, _ = e.Raise(ctx, store.RuleAgentSilent, &id, "critical", "Agent stopped reporting: "+name, fmt.Sprintf("No report for %s.", humanDur(now.Sub(last))), "")
			} else {
				_ = e.Resolve(ctx, store.RuleAgentSilent, &id)
			}
		}

		if p, ok := latest[id]; ok {
			if r, ok := ruleByKey[store.RuleCPUHigh]; ok && r.Threshold != nil {
				if p.CPUPercent >= *r.Threshold {
					_, _, _ = e.Raise(ctx, store.RuleCPUHigh, &id, "warning", "High CPU usage: "+name, fmt.Sprintf("CPU at %.0f%% (threshold %.0f%%).", p.CPUPercent, *r.Threshold), "")
				} else {
					_ = e.Resolve(ctx, store.RuleCPUHigh, &id)
				}
			}
			if r, ok := ruleByKey[store.RuleRAMHigh]; ok && r.Threshold != nil {
				if p.MemPercent >= *r.Threshold {
					_, _, _ = e.Raise(ctx, store.RuleRAMHigh, &id, "warning", "High memory usage: "+name, fmt.Sprintf("Memory at %.0f%% (threshold %.0f%%).", p.MemPercent, *r.Threshold), "")
				} else {
					_ = e.Resolve(ctx, store.RuleRAMHigh, &id)
				}
			}
		}

		if row, ok := invByAgent[id]; ok {
			if r, ok := ruleByKey[store.RuleDiskLow]; ok && r.Threshold != nil {
				if row.StorageTotal > 0 {
					freePct := float64(row.StorageFree) / float64(row.StorageTotal) * 100
					if freePct < *r.Threshold {
						_, _, _ = e.Raise(ctx, store.RuleDiskLow, &id, "warning", "Low disk space: "+name,
							fmt.Sprintf("%.0f%% free across volumes (threshold %.0f%%).", freePct, *r.Threshold), "")
					} else {
						_ = e.Resolve(ctx, store.RuleDiskLow, &id)
					}
				}
			}
		}

		// Outdated agent
		if r, ok := ruleByKey[store.RuleAgentOutdated]; ok && r.Enabled {
			if a.AgentVersion != "" && compareVersions(a.AgentVersion, version.Version) < 0 {
				_, _, _ = e.Raise(ctx, store.RuleAgentOutdated, &id, "info", "Outdated agent: "+name,
					fmt.Sprintf("Agent %s is older than server %s.", a.AgentVersion, version.Version), "")
			} else {
				_ = e.Resolve(ctx, store.RuleAgentOutdated, &id)
			}
		}
	}
	return nil
}

// EvaluateInventoryChecks inspects the latest inventory for disk health issues.
func (e *Engine) EvaluateInventory(ctx context.Context, agentID, hostname string, invJSON []byte) {
	var inv struct {
		PhysicalDisks []struct {
			Name, Model, Health, HealthDetail string
		} `json:"physicalDisks"`
	}
	if err := json.Unmarshal(invJSON, &inv); err != nil {
		return
	}
	var bad []string
	for _, d := range inv.PhysicalDisks {
		if d.Health == "failing" || d.Health == "warning" {
			bad = append(bad, fmt.Sprintf("%s %s: %s", d.Name, d.Model, d.Health))
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		_, _, _ = e.Raise(ctx, store.RuleDiskHealth, &agentID, "critical", "Disk health warning: "+hostname, strings.Join(bad, "; "), "")
	} else {
		_ = e.Resolve(ctx, store.RuleDiskHealth, &agentID)
	}
}

func humanDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

func compareVersions(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
