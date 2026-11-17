package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/auth"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// enrichAgents computes online state from the offline threshold.
func (s *Server) enrichAgents(agents []*store.Agent) {
	threshold := time.Duration(s.Cfg.OfflineThresholdSec) * time.Second
	now := time.Now().UTC()
	for _, a := range agents {
		switch {
		case a.LastHeartbeatAt == nil:
			a.OnlineState = "never"
			a.Online = false
		case now.Sub(*a.LastHeartbeatAt) <= threshold:
			a.OnlineState = "online"
			a.Online = true
		default:
			a.OnlineState = "offline"
			a.Online = false
		}
	}
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	agents, err := s.Store.ListAgents(r.Context(), status)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.enrichAgents(agents)
	writeJSON(w, 200, map[string]any{"agents": agents, "offlineThresholdSec": s.Cfg.OfflineThresholdSec})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	s.enrichAgents([]*store.Agent{a})
	out := map[string]any{"agent": a, "offlineThresholdSec": s.Cfg.OfflineThresholdSec}
	if row, err := s.Store.GetInventoryRow(r.Context(), a.ID); err == nil {
		out["summary"] = row
	}
	if latest, err := s.Store.LatestPerfSamples(r.Context()); err == nil {
		if p, ok := latest[a.ID]; ok {
			out["latestMetrics"] = p
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	var body struct {
		DisplayName *string             `json:"displayName"`
		Location    *string             `json:"location"`
		GroupID     *int64              `json:"groupId"`
		Notes       *string             `json:"notes"`
		Config      *models.AgentConfig `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}
	displayName, location, notes := a.DisplayName, a.Location, a.Notes
	groupID := a.GroupID
	if body.DisplayName != nil {
		displayName = strings.TrimSpace(*body.DisplayName)
		if len(displayName) > 100 {
			writeErr(w, 400, "display name too long")
			return
		}
	}
	if body.Location != nil {
		location = strings.TrimSpace(*body.Location)
	}
	if body.Notes != nil {
		notes = *body.Notes
	}
	if body.GroupID != nil {
		if *body.GroupID == 0 {
			groupID = nil
		} else {
			groupID = body.GroupID
		}
	}
	if err := s.Store.UpdateAgentAdmin(r.Context(), a.ID, displayName, location, groupID, notes); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if body.Config != nil {
		cfg := *body.Config
		if a.Config != nil {
			cfg.ConfigVersion = a.Config.ConfigVersion + 1
		}
		if err := s.Store.SetAgentConfig(r.Context(), a.ID, cfg); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	s.audit(r, "agent.update", a.ID, "")
	s.Hub.Broadcast("agents", nil)
	s.handleGetAgent(w, r)
}

func (s *Server) handleApproveAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	ctx := r.Context()
	if a.Status == store.StatusApproved {
		writeErr(w, 400, "agent is already approved")
		return
	}
	// Issue a fresh token that the agent picks up on its next enrollment poll.
	token, err := auth.NewToken()
	if err != nil {
		writeErr(w, 500, "token generation failed")
		return
	}
	sd := sessionFrom(ctx)
	by := "admin"
	if sd != nil {
		by = sd.Username
	}
	if err := s.Store.SetAgentStatus(ctx, a.ID, store.StatusApproved, by, ""); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// Hold the token until the agent collects it on its next enrollment poll.
	if err := s.Store.SetPendingToken(ctx, a.ID, token); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddCredential(ctx, a.ID, store.HashToken(token), auth.TokenPrefix(token), "approved by "+by)
	_ = s.Store.DecideEnrollment(ctx, a.ID, "approved", by, "")
	_ = s.Store.AddAgentEvent(ctx, a.ID, "approve", "info", "Agent approved by "+by, "")
	s.audit(r, "agent.approve", a.ID, "")
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, map[string]any{"ok": true, "tokenPrefix": auth.TokenPrefix(token), "note": "agent will receive its token on its next enrollment poll"})
}

func (s *Server) handleRejectAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	ctx := r.Context()
	sd := sessionFrom(ctx)
	by := "admin"
	if sd != nil {
		by = sd.Username
	}
	_ = s.Store.SetAgentStatus(ctx, a.ID, store.StatusRejected, by, "rejected by "+by)
	_ = s.Store.DecideEnrollment(ctx, a.ID, "rejected", by, "")
	_ = s.Store.RevokeCredentials(ctx, a.ID)
	_ = s.Store.AddAgentEvent(ctx, a.ID, "reject", "warning", "Agent rejected by "+by, "")
	s.audit(r, "agent.reject", a.ID, "")
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDisableAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	ctx := r.Context()
	if err := s.Store.SetAgentStatus(ctx, a.ID, store.StatusDisabled, sessionUser(ctx), "disabled"); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddAgentEvent(ctx, a.ID, "disable", "warning", "Agent disabled", "")
	s.audit(r, "agent.disable", a.ID, "")
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleEnableAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	ctx := r.Context()
	if a.Status != store.StatusDisabled {
		writeErr(w, 400, "agent is not disabled")
		return
	}
	if err := s.Store.SetAgentStatus(ctx, a.ID, store.StatusApproved, sessionUser(ctx), ""); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddAgentEvent(ctx, a.ID, "enable", "info", "Agent re-enabled", "")
	s.audit(r, "agent.enable", a.ID, "")
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRevokeAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	ctx := r.Context()
	if err := s.Store.SetAgentStatus(ctx, a.ID, store.StatusRevoked, sessionUser(ctx), "revoked"); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.RevokeCredentials(ctx, a.ID)
	_ = s.Store.ResolveAlertsForAgent(ctx, a.ID)
	_ = s.Store.AddAgentEvent(ctx, a.ID, "revoke", "critical", "Agent revoked; credentials invalidated", "")
	s.audit(r, "agent.revoke", a.ID, "")
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	if _, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM agents WHERE id=?`, a.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "agent.delete", a.ID, a.Hostname)
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleAdminRotateToken(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		writeErr(w, 500, "token generation failed")
		return
	}
	ctx := r.Context()
	if err := s.Store.SetAgentTokens(ctx, a.ID, store.HashToken(token), auth.TokenPrefix(token), true); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddCredential(ctx, a.ID, store.HashToken(token), auth.TokenPrefix(token), "admin rotation")
	_ = s.Store.AddAgentEvent(ctx, a.ID, "config", "info", "API token rotated by administrator", "")
	s.audit(r, "agent.rotate_token", a.ID, "")
	// The agent receives the new token through the authenticated config poll
	// it performs on its next heartbeat.
	_ = s.queueCommand(ctx, a.ID, "rotate_token")
	writeJSON(w, 200, map[string]any{"ok": true, "note": "the agent will adopt the new token on its next heartbeat"})
}

func (s *Server) handleRefreshInventory(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	if a.Status != store.StatusApproved {
		writeErr(w, 400, "agent is not approved")
		return
	}
	if err := s.queueCommand(r.Context(), a.ID, "inventory_refresh"); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddAgentEvent(r.Context(), a.ID, "config", "info", "Inventory refresh requested", "")
	s.audit(r, "agent.refresh_inventory", a.ID, "")
	writeJSON(w, 200, map[string]any{"ok": true, "note": "the agent will send a fresh inventory on its next heartbeat"})
}

func (s *Server) handleGetAgentLogs(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	logs, err := s.Store.ListAgentLogs(r.Context(), a.ID, queryInt(r, "limit", 300))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"logs": logs})
}

func (s *Server) handleGetAgentEvents(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	events, err := s.Store.ListAgentEvents(r.Context(), a.ID, queryInt(r, "limit", 200))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	changes, _ := s.Store.ListHardwareChanges(r.Context(), a.ID, 200)
	writeJSON(w, 200, map[string]any{"events": events, "changes": changes})
}

func (s *Server) handleGetAgentInventory(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	snap, err := s.Store.LatestSnapshot(r.Context(), a.ID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "no inventory received yet")
		return
	}
	writeJSON(w, 200, map[string]any{"inventory": snap.Inventory, "snapshotId": snap.ID, "collectedAt": snap.CollectedAt, "receivedAt": snap.ReceivedAt, "hash": snap.Hash})
}

func (s *Server) handleGetAgentPerformance(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	rangeKey := r.URL.Query().Get("range")
	var from time.Time
	now := time.Now().UTC()
	switch rangeKey {
	case "6h":
		from = now.Add(-6 * time.Hour)
	case "24h":
		from = now.Add(-24 * time.Hour)
	case "7d":
		from = now.Add(-7 * 24 * time.Hour)
	case "30d":
		from = now.Add(-30 * 24 * time.Hour)
	default:
		from = now.Add(-1 * time.Hour)
		rangeKey = "1h"
	}
	series, bucket, err := s.Store.PerfSeries(r.Context(), a.ID, from, now)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if series == nil {
		series = []store.PerfSample{}
	}
	writeJSON(w, 200, map[string]any{"range": rangeKey, "bucket": bucket, "from": from, "to": now, "samples": series})
}

func (s *Server) handleGetSnapshots(w http.ResponseWriter, r *http.Request) {
	a := s.agentOr404(w, r)
	if a == nil {
		return
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,collected_at,received_at,hash,COALESCE(reason,'') FROM inventory_snapshots WHERE agent_id=? ORDER BY collected_at DESC LIMIT 60`, a.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer rows.Close()
	type snap struct {
		ID          int64     `json:"id"`
		CollectedAt time.Time `json:"collectedAt"`
		ReceivedAt  time.Time `json:"receivedAt"`
		Hash        string    `json:"hash"`
		Reason      string    `json:"reason"`
	}
	var out []snap
	for rows.Next() {
		var sn snap
		var c, rc string
		if err := rows.Scan(&sn.ID, &c, &rc, &sn.Hash, &sn.Reason); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		sn.CollectedAt = parseTime(c)
		sn.ReceivedAt = parseTime(rc)
		out = append(out, sn)
	}
	writeJSON(w, 200, map[string]any{"snapshots": out})
}

func parseTime(s string) time.Time {
	for _, l := range []string{"2006-01-02T15:04:05.000Z07:00", time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func (s *Server) handleListEnrollments(w http.ResponseWriter, r *http.Request) {
	decision := r.URL.Query().Get("decision")
	reqs, err := s.Store.ListEnrollmentRequests(r.Context(), decision)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"requests": reqs})
}

func (s *Server) handleInventoryList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.ListInventoryRows(r.Context(), false)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	latest, _ := s.Store.LatestPerfSamples(r.Context())
	s.enrichInventory(rows, latest)
	writeJSON(w, 200, map[string]any{"computers": rows})
}

func (s *Server) enrichInventory(rows []*store.InventoryRow, latest map[string]store.PerfSample) {
	threshold := time.Duration(s.Cfg.OfflineThresholdSec) * time.Second
	now := time.Now().UTC()
	for _, r := range rows {
		r.Online = r.LastHeartbeatAt != nil && now.Sub(*r.LastHeartbeatAt) <= threshold
	}
}

func (s *Server) handleListChanges(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agentId")
	changes, err := s.Store.ListHardwareChanges(r.Context(), agentID, queryInt(r, "limit", 200))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"changes": changes})
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListAlerts(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("severity"), r.URL.Query().Get("agentId"), queryInt(r, "limit", 200))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	counts, _ := s.Store.AlertCounts(r.Context())
	writeJSON(w, 200, map[string]any{"alerts": list, "counts": counts})
}

func (s *Server) handleListAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Store.ListAlertRules(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Server) handleUpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var body store.AlertRule
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}
	body.Key = key
	if body.CooldownSec < 0 {
		body.CooldownSec = 0
	}
	if body.Severity != "info" && body.Severity != "warning" && body.Severity != "critical" {
		body.Severity = "warning"
	}
	if err := s.Store.UpdateAlertRule(r.Context(), body); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "alert.rule.update", key, "")
	rules, _ := s.Store.ListAlertRules(r.Context())
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Server) handleAckAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var alertID int64
	fmt.Sscanf(id, "%d", &alertID)
	if err := s.Store.AckAlert(r.Context(), alertID, sessionUser(r.Context())); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "alert.ack", id, "")
	s.Hub.Broadcast("alerts", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleResolveAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var alertID int64
	fmt.Sscanf(id, "%d", &alertID)
	if err := s.Store.ResolveAlert(r.Context(), alertID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "alert.resolve", id, "")
	s.Hub.Broadcast("alerts", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func sessionUser(ctx interface{ Value(any) any }) string {
	if sd, ok := ctx.Value(sessionKey{}).(*sessionData); ok && sd != nil {
		return sd.Username
	}
	return "admin"
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.Store.ListGroups(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"groups": groups})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeErr(w, 400, "name is required")
		return
	}
	id, err := s.Store.CreateGroup(r.Context(), strings.TrimSpace(body.Name), body.Description)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.audit(r, "group.create", fmt.Sprint(id), body.Name)
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var gid int64
	fmt.Sscanf(id, "%d", &gid)
	if err := s.Store.DeleteGroup(r.Context(), gid); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "group.delete", id, "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	results := map[string]any{}
	agents, _ := s.Store.ListAgents(r.Context(), "")
	s.enrichAgents(agents)
	var ag []*store.Agent
	var apps []map[string]string
	for _, a := range agents {
		hay := strings.ToLower(strings.Join([]string{a.Hostname, a.DisplayName, a.OSName, a.PrimaryIP, a.PrimaryMAC, a.Location, a.GroupName, a.AgentVersion}, " "))
		if q == "" || strings.Contains(hay, q) {
			ag = append(ag, a)
		}
	}
	if q != "" {
		rows, _ := s.Store.ListInventoryRows(r.Context(), false)
		for _, row := range rows {
			snap, err := s.Store.LatestSnapshot(r.Context(), row.AgentID)
			if err != nil {
				continue
			}
			for _, app := range snap.Inventory.Software {
				if strings.Contains(strings.ToLower(app.Name), q) {
					apps = append(apps, map[string]string{"agentId": row.AgentID, "hostname": row.Hostname, "name": app.Name, "version": app.Version, "publisher": app.Publisher})
					if len(apps) >= 50 {
						break
					}
				}
			}
		}
	}
	results["agents"] = ag
	results["software"] = apps
	writeJSON(w, 200, results)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	events, err := s.Store.ListAudit(r.Context(), queryInt(r, "limit", 200))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
	}
	_ = decodeJSON(r, &body)
	ctx := r.Context()
	switch body.Channel {
	case "email":
		if err := s.Email.Test(ctx); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	case "telegram":
		if err := s.Telegram.Test(ctx); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	default:
		writeErr(w, 400, "unknown channel")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, _ := s.Store.CountAgentsByStatus(ctx)
	dbStats, _ := s.Store.Stats(ctx)
	reqStats, _ := s.Store.RequestStatsSummary(ctx)
	serverEvents, _ := s.Store.ListServerEvents(ctx, 20)
	agents, _ := s.Store.ListAgents(ctx, "")
	s.enrichAgents(agents)
	online := 0
	for _, a := range agents {
		if a.Online && a.Status == store.StatusApproved {
			online++
		}
	}
	writeJSON(w, 200, map[string]any{
		"serverName":          s.serverName(ctx),
		"version":             "1.0.0",
		"uptimeSec":           int(time.Since(s.StartedAt).Seconds()),
		"startedAt":           s.StartedAt,
		"dbPath":              s.Cfg.DBPath,
		"dbSizeBytes":         s.Store.SizeBytes(),
		"dbStats":             dbStats,
		"agentsByStatus":      counts,
		"agentsOnline":        online,
		"sseClients":          s.Hub.Count(),
		"requests":            reqStats,
		"totalRequests":       s.reqCount.Load(),
		"recentServerEvents":  serverEvents,
		"listen":              fmt.Sprintf("%s:%d", s.Cfg.ListenHost, s.Cfg.Port),
		"tls":                 s.Cfg.TLSEnabled(),
		"advertisedUrl":       s.Cfg.AdvertisedURL(),
		"offlineThresholdSec": s.Cfg.OfflineThresholdSec,
	})
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	path, err := s.BackupNow(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "db.backup", path, "")
	writeJSON(w, 200, map[string]any{"ok": true, "path": path})
}

func sortChanges(c []store.HardwareChange) {
	sort.Slice(c, func(i, j int) bool { return c[i].DetectedAt.After(c[j].DetectedAt) })
}
