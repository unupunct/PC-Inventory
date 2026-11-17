package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pcim/pc-inventory-manager/internal/server/auth"
	"github.com/pcim/pc-inventory-manager/internal/server/inventory"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

type agentCtxKey struct{}

func agentFrom(ctx context.Context) *store.Agent {
	a, _ := ctx.Value(agentCtxKey{}).(*store.Agent)
	return a
}

// agentAuth validates the bearer token of an approved agent.
func (s *Server) agentAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		}
		if token == "" {
			token = r.Header.Get("X-Agent-Token")
		}
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "missing agent token")
			return
		}
		// Agents identify themselves with their agent id; fall back to a
		// lookup by token hash when the header is absent.
		var rec *store.TokenRecord
		if agentID := r.Header.Get("X-Agent-Id"); agentID != "" {
			rec, _ = s.Store.GetTokenRecord(r.Context(), agentID)
		}
		if rec == nil {
			if id := s.Store.AgentIDByToken(r.Context(), store.HashToken(token)); id != "" {
				rec, _ = s.Store.GetTokenRecord(r.Context(), id)
			}
		}
		if rec == nil {
			writeErr(w, http.StatusUnauthorized, "unknown agent")
			return
		}
		if rec.Status != store.StatusApproved {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "agent is " + rec.Status, "status": rec.Status})
			return
		}
		hash := store.HashToken(token)
		ok := rec.TokenHash != "" && auth.ConstantTimeEqual(hash, rec.TokenHash)
		if !ok && rec.PrevHash != "" && rec.PrevExpires != nil && time.Now().UTC().Before(*rec.PrevExpires) {
			ok = auth.ConstantTimeEqual(hash, rec.PrevHash)
		}
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid agent token")
			return
		}
		agent, err := s.Store.GetAgent(r.Context(), rec.AgentID)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unknown agent")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), agentCtxKey{}, agent)))
	}
}

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req models.EnrollmentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, 400, "invalid request: "+err.Error())
		return
	}
	req.MachineID = strings.TrimSpace(req.MachineID)
	if req.MachineID == "" || len(req.MachineID) > 128 {
		writeErr(w, 400, "machineId is required")
		return
	}
	if req.Protocol != 0 && req.Protocol != version.Protocol {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "protocol version mismatch", "serverProtocol": itoa(version.Protocol), "status": "rejected"})
		return
	}
	ctx := r.Context()
	remote := clientIP(r, s.Cfg.TrustProxy)

	agent, err := s.Store.GetAgentByMachineID(ctx, req.MachineID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeErr(w, 500, err.Error())
		return
	}

	enrollmentEnabled := s.settingBool(ctx, "enrollment_enabled", true)

	if agent == nil {
		if !enrollmentEnabled {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "enrollment is disabled on this server", "status": "rejected"})
			return
		}
		token, err := auth.NewToken()
		if err != nil {
			writeErr(w, 500, "token generation failed")
			return
		}
		autoApprove := s.settingBool(ctx, "auto_approve_agents", false)
		status := store.StatusPending
		if autoApprove {
			status = store.StatusApproved
		}
		agent, err = s.Store.CreateAgent(ctx, store.NewAgent{
			ID: uuid.NewString(), MachineID: req.MachineID, Hostname: req.Hostname, Platform: req.Platform,
			OSName: req.OSName, OSVersion: req.OSVersion, Arch: req.Arch, AgentVersion: req.AgentVersion,
			Status: status, PrimaryIP: req.PrimaryIP, PrimaryMAC: req.PrimaryMAC, EnrollTokenHash: store.HashToken(token),
			PlainToken: token,
		})
		if err != nil {
			writeErr(w, 500, "could not create agent: "+err.Error())
			return
		}
		if _, err := s.Store.CreateEnrollmentRequest(ctx, store.EnrollmentRequestRow{
			AgentID: agent.ID, MachineID: req.MachineID, Hostname: req.Hostname, Platform: req.Platform, OSName: req.OSName,
			OSVersion: req.OSVersion, Arch: req.Arch, AgentVersion: req.AgentVersion, RemoteAddr: remote,
		}); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_ = s.Store.AddAgentEvent(ctx, agent.ID, "enroll", "info", "Agent requested enrollment from "+remote, "")
		_ = s.Store.AddAudit(ctx, "agent", "enroll.request", agent.ID, remote, `{"hostname":`+quote(req.Hostname)+`}`)
		if autoApprove {
			if err := s.Store.SetAgentStatus(ctx, agent.ID, store.StatusApproved, "auto-approve", "Automatically approved by server setting"); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			_ = s.Store.DecideEnrollment(ctx, agent.ID, "approved", "auto-approve", "")
			_ = s.Store.SetAgentTokens(ctx, agent.ID, store.HashToken(token), auth.TokenPrefix(token), false)
			_ = s.Store.AddCredential(ctx, agent.ID, store.HashToken(token), auth.TokenPrefix(token), "initial enrollment")
			cfg := s.latestConfig(ctx, agent)
			s.Hub.Broadcast("agents", nil)
			writeJSON(w, 200, models.EnrollmentResponse{Status: "approved", AgentID: agent.ID, AgentToken: token, ServerName: s.serverName(ctx), Config: &cfg,
				Message: "approved automatically"})
			return
		}
		s.Hub.Broadcast("enrollment", nil)
		writeJSON(w, http.StatusAccepted, models.EnrollmentResponse{Status: "pending", AgentID: agent.ID, ServerName: s.serverName(ctx),
			Message: "enrollment request recorded; waiting for administrator approval"})
		return
	}

	// Existing agent
	switch agent.Status {
	case store.StatusPending:
		_ = s.Store.UpdateAgentMeta(ctx, agent.ID, req.Hostname, req.OSName, req.OSVersion, req.Arch, req.AgentVersion, req.PrimaryIP, req.PrimaryMAC)
		writeJSON(w, http.StatusAccepted, models.EnrollmentResponse{Status: "pending", AgentID: agent.ID, ServerName: s.serverName(ctx),
			Message: "waiting for administrator approval"})
		return
	case store.StatusRejected:
		writeJSON(w, http.StatusForbidden, models.EnrollmentResponse{Status: "rejected", Message: "enrollment was rejected by the administrator"})
		return
	case store.StatusRevoked:
		writeJSON(w, http.StatusForbidden, models.EnrollmentResponse{Status: "revoked", Message: "this agent was revoked; re-enrollment requires administrator action"})
		return
	case store.StatusDisabled:
		writeJSON(w, http.StatusForbidden, models.EnrollmentResponse{Status: "disabled", Message: "this agent is disabled"})
		return
	}

	// Approved agent polling for its enrollment token. The token minted when the
	// request was created (or when an administrator approved it) is held
	// server-side until the agent collects it, so an approval that lands
	// between two polls is not lost.
	var pendingToken, enrollHash string
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT COALESCE(pending_plain_token,''), COALESCE(enrollment_token_hash,'') FROM agents WHERE id=?`, agent.ID).
		Scan(&pendingToken, &enrollHash); err == nil && pendingToken != "" {
		// Move the pending token into the active slot and consume it.
		_, _ = s.Store.DB.ExecContext(ctx, `UPDATE agents SET pending_plain_token='' WHERE id=?`, agent.ID)
		if err := s.Store.SetAgentTokens(ctx, agent.ID, store.HashToken(pendingToken), auth.TokenPrefix(pendingToken), false); err == nil {
			cfg := s.latestConfig(ctx, agent)
			_ = s.Store.AddAgentEvent(ctx, agent.ID, "enroll", "info", "Agent collected its enrollment token", "")
			_ = s.Store.AddAudit(ctx, "agent", "enroll.complete", agent.ID, remote, "")
			s.Hub.Broadcast("agents", nil)
			writeJSON(w, 200, models.EnrollmentResponse{Status: "approved", AgentID: agent.ID, AgentToken: pendingToken,
				ServerName: s.serverName(ctx), Config: &cfg, Message: "enrollment approved"})
			return
		}
	}

	// Otherwise this is a re-enrollment after a lost token: require the
	// enrollment secret, or a fresh approval, to avoid token theft.
	var body struct {
		models.EnrollmentRequest
		EnrollSecret string `json:"enrollSecret"`
	}
	// req was already decoded; we cannot re-read body, so rely on header.
	secret := r.Header.Get("X-Enroll-Secret")
	_ = body
	var storedHash string
	_ = s.Store.DB.QueryRowContext(ctx, `SELECT COALESCE(enrollment_token_hash,'') FROM agents WHERE id=?`, agent.ID).Scan(&storedHash)
	if storedHash == "" || !auth.ConstantTimeEqual(store.HashToken(secret), storedHash) {
		writeJSON(w, http.StatusConflict, models.EnrollmentResponse{Status: "approved", AgentID: agent.ID, ServerName: s.serverName(ctx),
			Message: "agent already enrolled; ask an administrator to rotate its token"})
		return
	}
	token, err := auth.NewToken()
	if err != nil {
		writeErr(w, 500, "token generation failed")
		return
	}
	if err := s.Store.SetAgentTokens(ctx, agent.ID, store.HashToken(token), auth.TokenPrefix(token), false); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddCredential(ctx, agent.ID, store.HashToken(token), auth.TokenPrefix(token), "re-enrollment")
	cfg := s.latestConfig(ctx, agent)
	writeJSON(w, 200, models.EnrollmentResponse{Status: "approved", AgentID: agent.ID, AgentToken: token, ServerName: s.serverName(ctx), Config: &cfg})
}

func (s *Server) settingBool(ctx context.Context, key string, def bool) bool {
	v, ok := s.Store.GetSetting(ctx, key)
	if !ok || v == "" {
		return def
	}
	return v == "1" || v == "true"
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	agent := agentFrom(r.Context())
	var req models.HeartbeatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, 400, "invalid request: "+err.Error())
		return
	}
	ctx := r.Context()
	remote := clientIP(r, s.Cfg.TrustProxy)
	if err := s.Store.TouchHeartbeat(ctx, agent.ID, remote, req.PendingBuffered, req.LastError, req.UptimeSeconds); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if req.AgentVersion != "" && req.AgentVersion != agent.AgentVersion {
		_ = s.Store.AddAgentEvent(ctx, agent.ID, "agent_upgrade", "info", "Agent version "+agent.AgentVersion+" -> "+req.AgentVersion, "")
		_ = s.Store.UpdateAgentMeta(ctx, agent.ID, coalesce(req.Hostname, agent.Hostname), agent.OSName, agent.OSVersion, agent.Arch, req.AgentVersion, agent.PrimaryIP, agent.PrimaryMAC)
	}
	cfg := s.latestConfig(ctx, agent)
	resp := models.HeartbeatResponse{OK: true, ServerTime: time.Now().UTC(), Config: &cfg, ConfigVersion: cfg.ConfigVersion}

	// Pending commands queued in settings key "agent_commands:<id>"
	if cmds := s.takeCommands(ctx, agent.ID); len(cmds) > 0 {
		resp.Commands = cmds
	}
	// Alert engine picks up offline/online transitions on its own tick; also
	// do an immediate check so reconnection alerts appear promptly.
	s.Hub.Broadcast("agents", nil)
	writeJSON(w, 200, resp)
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) handleAgentMetrics(w http.ResponseWriter, r *http.Request) {
	agent := agentFrom(r.Context())
	var req models.MetricsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, 400, "invalid request: "+err.Error())
		return
	}
	m := req.Metrics
	if m.CollectedAt.IsZero() {
		m.CollectedAt = time.Now().UTC()
	}
	// Reject wildly wrong timestamps (clock skew), keep the server's view sane.
	if m.CollectedAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		m.CollectedAt = time.Now().UTC()
	}
	ctx := r.Context()
	var maxDisk, maxVol *float64
	for i := range m.Disks {
		if m.Disks[i].UtilizationPercent != nil && (maxDisk == nil || *m.Disks[i].UtilizationPercent > *maxDisk) {
			v := *m.Disks[i].UtilizationPercent
			maxDisk = &v
		}
	}
	for i := range m.Volumes {
		if maxVol == nil || m.Volumes[i].UsagePercent > *maxVol {
			v := m.Volumes[i].UsagePercent
			maxVol = &v
		}
	}
	var rx, tx float64
	for _, n := range m.Network {
		if n.Name == "lo" || strings.Contains(strings.ToLower(n.Name), "loopback") {
			continue
		}
		rx += n.RxBytesPerSec
		tx += n.TxBytesPerSec
	}
	details, _ := json.Marshal(map[string]any{"disks": m.Disks, "network": m.Network, "volumes": m.Volumes, "temperatures": m.Temperatures, "perCore": m.CPU.PerCore, "unsupported": m.Unsupported})
	sample := store.PerfSample{
		AgentID: agent.ID, At: m.CollectedAt, CPUPercent: m.CPU.UsagePercent,
		MemUsed: m.Memory.UsedBytes, MemTotal: m.Memory.TotalBytes, MemPercent: m.Memory.UsagePercent, SwapUsed: m.Memory.SwapUsedBytes,
		DiskReadBps: sumDisk(m, true), DiskWriteBps: sumDisk(m, false), NetRxBps: rx, NetTxBps: tx,
		ProcessCount: m.Processes, BatteryPercent: batteryPct(m.Battery), MaxDiskPercent: maxDisk, MaxVolumeUsage: maxVol,
	}
	if m.Load != nil {
		v := m.Load.One
		sample.Load1 = &v
	}
	if err := s.Store.InsertPerfSample(ctx, sample, string(details)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.TouchMetrics(ctx, agent.ID)
	s.Hub.Broadcast("metrics", map[string]any{"agentId": agent.ID, "sample": sample})
	writeJSON(w, 200, models.IngestResponse{OK: true})
}

// batteryPct is nil-safe: desktops report no battery at all.
func batteryPct(b *models.BatteryMetrics) *float64 {
	if b == nil || b.Percent == nil {
		return nil
	}
	v := *b.Percent
	return &v
}

func sumDisk(m models.Metrics, read bool) float64 {
	var t float64
	for _, d := range m.Disks {
		if read {
			t += d.ReadBytesPerSec
		} else {
			t += d.WriteBytesPerSec
		}
	}
	return t
}

func (s *Server) handleInventory(w http.ResponseWriter, r *http.Request) {
	agent := agentFrom(r.Context())
	var req models.InventoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, 400, "invalid request: "+err.Error())
		return
	}
	ctx := r.Context()
	inv := req.Inventory
	if inv.CollectedAt.IsZero() {
		inv.CollectedAt = time.Now().UTC()
	}
	if inv.Hash == "" {
		inv.Hash = inventory.Hash(inv)
	}

	var prev *models.Inventory
	if prevSnap, err := s.Store.LatestSnapshot(ctx, agent.ID); err == nil {
		p := prevSnap.Inventory
		prev = &p
	}
	snapID, changed, err := s.Store.SaveInventory(ctx, agent.ID, inv, req.Reason)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// Update agent laptop flag and metadata from inventory.
	isLaptop := len(inv.Batteries) > 0
	_ = s.Store.SetLaptop(ctx, agent.ID, isLaptop)
	if inv.Host.Hostname != "" {
		_ = s.Store.UpdateAgentMeta(ctx, agent.ID, inv.Host.Hostname, inv.OS.Name, inv.OS.Version, inv.OS.Arch, inv.Host.AgentVersion, "", "")
	}
	if prev != nil {
		// Detect IP change events.
		if oldIP, newIP := primaryIP(*prev), primaryIP(inv); oldIP != "" && newIP != "" && oldIP != newIP {
			_ = s.Store.AddAgentEvent(ctx, agent.ID, "ip_change", "info", "IP address changed from "+oldIP+" to "+newIP, "")
		}
		if changed {
			changes := inventory.Diff(*prev, inv)
			for _, c := range changes {
				if err := s.Store.AddHardwareChange(ctx, agent.ID, c.Category, c.Severity, c.Summary, c.Details); err != nil {
					s.Log.Warn("store hardware change", "err", err)
				}
				if c.Category != "software" {
					_, _, _ = s.Alerts.Raise(ctx, store.RuleHardwareChange, &agent.ID, c.Severity, "Hardware change: "+inv.Host.Hostname, c.Summary, c.Details)
				}
			}
			if len(changes) > 0 {
				_ = s.Store.AddAgentEvent(ctx, agent.ID, "inventory", "info", "Hardware inventory changed ("+itoa(len(changes))+" changes)", "")
			}
			s.Hub.Broadcast("changes", nil)
		}
	} else {
		_ = s.Store.AddAgentEvent(ctx, agent.ID, "inventory", "info", "First inventory received", "")
	}
	b, _ := json.Marshal(inv)
	s.Alerts.EvaluateInventory(ctx, agent.ID, inv.Host.Hostname, b)
	s.Hub.Broadcast("inventory", map[string]any{"agentId": agent.ID})
	writeJSON(w, 200, models.IngestResponse{OK: true, Changed: changed, Snapshot: snapID})
}

func primaryIP(inv models.Inventory) string {
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

func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	agent := agentFrom(r.Context())
	var req models.LogsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if len(req.Entries) > 500 {
		req.Entries = req.Entries[len(req.Entries)-500:]
	}
	if err := s.Store.AddAgentLogs(r.Context(), agent.ID, req.Entries); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	agent := agentFrom(r.Context())
	cfg := s.latestConfig(r.Context(), agent)
	writeJSON(w, 200, map[string]any{"config": cfg, "configVersion": cfg.ConfigVersion})
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	agent := agentFrom(r.Context())
	token, err := auth.NewToken()
	if err != nil {
		writeErr(w, 500, "token generation failed")
		return
	}
	ctx := r.Context()
	if err := s.Store.SetAgentTokens(ctx, agent.ID, store.HashToken(token), auth.TokenPrefix(token), true); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.AddCredential(ctx, agent.ID, store.HashToken(token), auth.TokenPrefix(token), "self-rotation")
	_ = s.Store.AddAgentEvent(ctx, agent.ID, "config", "info", "Agent rotated its API token", "")
	writeJSON(w, 200, map[string]string{"agentToken": token, "agentId": agent.ID})
}

func (s *Server) takeCommands(ctx context.Context, agentID string) []models.AgentCommand {
	key := "agent_commands:" + agentID
	raw, ok := s.Store.GetSetting(ctx, key)
	if !ok || raw == "" {
		return nil
	}
	_ = s.Store.SetSetting(ctx, key, "")
	var cmds []models.AgentCommand
	_ = json.Unmarshal([]byte(raw), &cmds)
	return cmds
}

// queueCommand appends a restricted command for the agent to pick up on its
// next heartbeat.
func (s *Server) queueCommand(ctx context.Context, agentID, cmdType string) error {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	key := "agent_commands:" + agentID
	raw, _ := s.Store.GetSetting(ctx, key)
	var cmds []models.AgentCommand
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cmds)
	}
	cmds = append(cmds, models.AgentCommand{Type: cmdType, ID: hex.EncodeToString(b)})
	js, _ := json.Marshal(cmds)
	return s.Store.SetSetting(ctx, key, string(js))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// listenAddr normalises the listen host/port.
func listenAddr(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return net.JoinHostPort(host, itoa(port))
	}
	return host + ":" + itoa(port)
}
