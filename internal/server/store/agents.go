package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

var ErrNotFound = errors.New("not found")

// Agent status values.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	StatusDisabled = "disabled"
	StatusRevoked  = "revoked"
)

type Agent struct {
	ID               string     `json:"id"`
	MachineID        string     `json:"machineId"`
	Hostname         string     `json:"hostname"`
	DisplayName      string     `json:"displayName"`
	Platform         string     `json:"platform"`
	OSName           string     `json:"osName"`
	OSVersion        string     `json:"osVersion"`
	Arch             string     `json:"arch"`
	AgentVersion     string     `json:"agentVersion"`
	Status           string     `json:"status"`
	GroupID          *int64     `json:"groupId,omitempty"`
	GroupName        string     `json:"groupName,omitempty"`
	Location         string     `json:"location,omitempty"`
	Notes            string     `json:"notes,omitempty"`
	PrimaryIP        string     `json:"primaryIp,omitempty"`
	PrimaryMAC       string     `json:"primaryMac,omitempty"`
	LastIP           string     `json:"lastIp,omitempty"`
	TokenPrefix      string     `json:"tokenPrefix,omitempty"`
	TokenRotatedAt   *time.Time `json:"tokenRotatedAt,omitempty"`
	EnrolledAt       time.Time  `json:"enrolledAt"`
	ApprovedAt       *time.Time `json:"approvedAt,omitempty"`
	ApprovedBy       string     `json:"approvedBy,omitempty"`
	LastHeartbeatAt  *time.Time `json:"lastHeartbeatAt,omitempty"`
	LastMetricsAt    *time.Time `json:"lastMetricsAt,omitempty"`
	LastInventoryAt  *time.Time `json:"lastInventoryAt,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	PendingBuffered  int        `json:"pendingBuffered"`
	StatusNote       string     `json:"statusNote,omitempty"`
	IsLaptop         bool       `json:"isLaptop"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`

	Config *models.AgentConfig `json:"config,omitempty"`

	// Computed fields (not stored).
	Online       bool   `json:"online"`
	OnlineState  string `json:"onlineState"` // online | offline | never
}

const agentColumns = `id,machine_id,hostname,COALESCE(display_name,''),platform,COALESCE(os_name,''),COALESCE(os_version,''),COALESCE(arch,''),COALESCE(agent_version,''),status,group_id,COALESCE(location,''),COALESCE(notes,''),COALESCE(primary_ip,''),COALESCE(primary_mac,''),COALESCE(last_ip,''),COALESCE(token_prefix,''),token_rotated_at,enrolled_at,approved_at,COALESCE(approved_by,''),last_heartbeat_at,last_metrics_at,last_inventory_at,COALESCE(last_error,''),pending_buffered,COALESCE(status_note,''),is_laptop,created_at,updated_at,config_json`

func scanAgent(sc interface{ Scan(...any) error }) (*Agent, error) {
	var a Agent
	var groupID sql.NullInt64
	var tokenRot, approved, hb, met, inv sql.NullString
	var enrolled, created, updated string
	var cfg sql.NullString
	var isLaptop int
	err := sc.Scan(&a.ID, &a.MachineID, &a.Hostname, &a.DisplayName, &a.Platform, &a.OSName, &a.OSVersion, &a.Arch, &a.AgentVersion, &a.Status,
		&groupID, &a.Location, &a.Notes, &a.PrimaryIP, &a.PrimaryMAC, &a.LastIP, &a.TokenPrefix, &tokenRot, &enrolled, &approved, &a.ApprovedBy,
		&hb, &met, &inv, &a.LastError, &a.PendingBuffered, &a.StatusNote, &isLaptop, &created, &updated, &cfg)
	if err != nil {
		return nil, err
	}
	if groupID.Valid {
		v := groupID.Int64
		a.GroupID = &v
	}
	if tokenRot.Valid {
		t := parseTS(tokenRot.String)
		a.TokenRotatedAt = &t
	}
	if approved.Valid {
		t := parseTS(approved.String)
		a.ApprovedAt = &t
	}
	if hb.Valid {
		t := parseTS(hb.String)
		a.LastHeartbeatAt = &t
	}
	if met.Valid {
		t := parseTS(met.String)
		a.LastMetricsAt = &t
	}
	if inv.Valid {
		t := parseTS(inv.String)
		a.LastInventoryAt = &t
	}
	a.EnrolledAt = parseTS(enrolled)
	a.CreatedAt = parseTS(created)
	a.UpdatedAt = parseTS(updated)
	a.IsLaptop = isLaptop != 0
	if cfg.Valid && cfg.String != "" {
		var c models.AgentConfig
		if json.Unmarshal([]byte(cfg.String), &c) == nil {
			a.Config = &c
		}
	}
	return &a, nil
}

func (s *Store) ListAgents(ctx context.Context, status string) ([]*Agent, error) {
	q := `SELECT ` + agentColumns + `, COALESCE((SELECT name FROM agent_groups g WHERE g.id = agents.group_id),'') FROM agents`
	var args []any
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, status)
	}
	q += ` ORDER BY hostname COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Agent
	for rows.Next() {
		var a Agent
		// scan inline to include group name column
		var groupID sql.NullInt64
		var tokenRot, approved, hb, met, inv, cfg sql.NullString
		var enrolled, created, updated string
		var isLaptop int
		if err := rows.Scan(&a.ID, &a.MachineID, &a.Hostname, &a.DisplayName, &a.Platform, &a.OSName, &a.OSVersion, &a.Arch, &a.AgentVersion, &a.Status,
			&groupID, &a.Location, &a.Notes, &a.PrimaryIP, &a.PrimaryMAC, &a.LastIP, &a.TokenPrefix, &tokenRot, &enrolled, &approved, &a.ApprovedBy,
			&hb, &met, &inv, &a.LastError, &a.PendingBuffered, &a.StatusNote, &isLaptop, &created, &updated, &cfg, &a.GroupName); err != nil {
			return nil, err
		}
		if groupID.Valid {
			v := groupID.Int64
			a.GroupID = &v
		}
		if tokenRot.Valid {
			t := parseTS(tokenRot.String)
			a.TokenRotatedAt = &t
		}
		if approved.Valid {
			t := parseTS(approved.String)
			a.ApprovedAt = &t
		}
		if hb.Valid {
			t := parseTS(hb.String)
			a.LastHeartbeatAt = &t
		}
		if met.Valid {
			t := parseTS(met.String)
			a.LastMetricsAt = &t
		}
		if inv.Valid {
			t := parseTS(inv.String)
			a.LastInventoryAt = &t
		}
		a.EnrolledAt = parseTS(enrolled)
		a.CreatedAt = parseTS(created)
		a.UpdatedAt = parseTS(updated)
		a.IsLaptop = isLaptop != 0
		if cfg.Valid && cfg.String != "" {
			var c models.AgentConfig
			if json.Unmarshal([]byte(cfg.String), &c) == nil {
				a.Config = &c
			}
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (s *Store) GetAgent(ctx context.Context, id string) (*Agent, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT name FROM agent_groups g WHERE g.id = agents.group_id),'') FROM agents WHERE id=?`, id).Scan(&a.GroupName)
	return a, nil
}

func (s *Store) GetAgentByMachineID(ctx context.Context, machineID string) (*Agent, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+agentColumns+` FROM agents WHERE machine_id = ?`, machineID)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

type NewAgent struct {
	ID          string
	MachineID   string
	Hostname    string
	Platform    string
	OSName      string
	OSVersion   string
	Arch        string
	AgentVersion string
	Status      string
	PrimaryIP   string
	PrimaryMAC  string
	EnrollTokenHash string
	// PlainToken is held server-side until the agent polls for it, so that an
	// approval arriving between two enrollment polls is not lost.
	PlainToken string
}

func (s *Store) CreateAgent(ctx context.Context, n NewAgent) (*Agent, error) {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO agents
		(id,machine_id,hostname,platform,os_name,os_version,arch,agent_version,status,primary_ip,primary_mac,enrollment_token_hash,pending_plain_token,enrolled_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ID, n.MachineID, n.Hostname, n.Platform, n.OSName, n.OSVersion, n.Arch, n.AgentVersion, n.Status, n.PrimaryIP, n.PrimaryMAC, n.EnrollTokenHash, n.PlainToken, now, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetAgent(ctx, n.ID)
}

func (s *Store) UpdateAgentMeta(ctx context.Context, id, hostname, osName, osVersion, arch, agentVersion, primaryIP, primaryMAC string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET hostname=?, os_name=?, os_version=?, arch=?, agent_version=?, primary_ip=?, primary_mac=?, updated_at=? WHERE id=?`,
		hostname, osName, osVersion, arch, agentVersion, primaryIP, primaryMAC, nowStr(), id)
	return err
}

// SetPendingToken stores the token an approved agent will collect on its next
// enrollment poll.
func (s *Store) SetPendingToken(ctx context.Context, id, plain string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET pending_plain_token=?, updated_at=? WHERE id=?`, plain, nowStr(), id)
	return err
}

func (s *Store) SetAgentStatus(ctx context.Context, id, status, by, note string) error {
	now := nowStr()
	var err error
	if status == StatusApproved {
		_, err = s.DB.ExecContext(ctx, `UPDATE agents SET status=?, approved_at=COALESCE(approved_at,?), approved_by=?, status_note=?, updated_at=? WHERE id=?`, status, now, by, note, now, id)
	} else {
		_, err = s.DB.ExecContext(ctx, `UPDATE agents SET status=?, status_note=?, updated_at=? WHERE id=?`, status, note, now, id)
	}
	return err
}

// SetAgentTokens stores the active token hash (and optional previous one).
func (s *Store) SetAgentTokens(ctx context.Context, id, tokenHash, prefix string, keepPrevious bool) error {
	now := nowStr()
	if keepPrevious {
		_, err := s.DB.ExecContext(ctx, `UPDATE agents SET previous_token_hash=token_hash, previous_token_expires_at=?, token_hash=?, token_prefix=?, token_rotated_at=?, enrollment_token_hash=NULL, updated_at=? WHERE id=?`,
			tsStr(time.Now().UTC().Add(10*time.Minute)), tokenHash, prefix, now, now, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET token_hash=?, token_prefix=?, token_rotated_at=?, enrollment_token_hash=NULL, updated_at=? WHERE id=?`, tokenHash, prefix, now, now, id)
	return err
}

type TokenRecord struct {
	AgentID     string
	Status      string
	TokenHash   string
	PrevHash    string
	PrevExpires *time.Time
}

// GetTokenRecord loads the authentication state for one agent id.
func (s *Store) GetTokenRecord(ctx context.Context, agentID string) (*TokenRecord, error) {
	var r TokenRecord
	var prev, prevExp sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id,status,COALESCE(token_hash,''),COALESCE(previous_token_hash,''),previous_token_expires_at FROM agents WHERE id=?`, agentID).
		Scan(&r.AgentID, &r.Status, &r.TokenHash, &prev, &prevExp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if prev.Valid {
		r.PrevHash = prev.String
	}
	if prevExp.Valid {
		if t, err := time.Parse(time.RFC3339Nano, prevExp.String); err == nil {
			r.PrevExpires = &t
		}
	}
	return &r, nil
}

// AgentIDByToken returns the agent id owning an active or just-rotated token.
func (s *Store) AgentIDByToken(ctx context.Context, tokenHash string) string {
	var id string
	_ = s.DB.QueryRowContext(ctx, `SELECT id FROM agents WHERE token_hash=? OR previous_token_hash=? LIMIT 1`, tokenHash, tokenHash).Scan(&id)
	return id
}

func (s *Store) GetTokenRecordByMachineID(ctx context.Context, machineID string) (*TokenRecord, error) {
	var r TokenRecord
	var prev, prevExp sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id,status,COALESCE(token_hash,''),COALESCE(previous_token_hash,''),previous_token_expires_at FROM agents WHERE machine_id=?`, machineID).
		Scan(&r.AgentID, &r.Status, &r.TokenHash, &prev, &prevExp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.PrevHash = prev.String
	if prevExp.Valid {
		t := parseTS(prevExp.String)
		r.PrevExpires = &t
	}
	return &r, nil
}

func (s *Store) AddCredential(ctx context.Context, agentID, tokenHash, prefix, reason string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO agent_credentials (agent_id,token_hash,prefix,created_at,reason) VALUES (?,?,?,?,?)`, agentID, tokenHash, prefix, nowStr(), reason)
	return err
}

func (s *Store) RevokeCredentials(ctx context.Context, agentID string) error {
	now := nowStr()
	if _, err := s.DB.ExecContext(ctx, `UPDATE agent_credentials SET revoked_at=? WHERE agent_id=? AND revoked_at IS NULL`, now, agentID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET token_hash=NULL, previous_token_hash=NULL, enrollment_token_hash=NULL, updated_at=? WHERE id=?`, now, agentID)
	return err
}

func (s *Store) SetAgentConfig(ctx context.Context, id string, cfg models.AgentConfig) error {
	b, _ := json.Marshal(cfg)
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET config_json=?, updated_at=? WHERE id=?`, string(b), nowStr(), id)
	return err
}

func (s *Store) UpdateAgentAdmin(ctx context.Context, id, displayName, location string, groupID *int64, notes string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET display_name=?, location=?, group_id=?, notes=?, updated_at=? WHERE id=?`, displayName, location, groupID, notes, nowStr(), id)
	return err
}

func (s *Store) TouchHeartbeat(ctx context.Context, id, ip string, buffered int, lastErr string, uptimeSec uint64) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET last_heartbeat_at=?, last_ip=?, pending_buffered=?, last_error=?, updated_at=? WHERE id=?`, now, ip, buffered, lastErr, now, id)
	return err
}

func (s *Store) TouchMetrics(ctx context.Context, id string) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET last_metrics_at=? WHERE id=?`, now, id)
	return err
}

func (s *Store) TouchInventory(ctx context.Context, id string) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET last_inventory_at=? WHERE id=?`, now, id)
	return err
}

func (s *Store) SetLaptop(ctx context.Context, id string, laptop bool) error {
	v := 0
	if laptop {
		v = 1
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE agents SET is_laptop=? WHERE id=?`, v, id)
	return err
}

// --- enrollment requests -------------------------------------------------

type EnrollmentRequestRow struct {
	ID          int64      `json:"id"`
	AgentID     string     `json:"agentId"`
	MachineID   string     `json:"machineId"`
	Hostname    string     `json:"hostname"`
	Platform    string     `json:"platform"`
	OSName      string     `json:"osName"`
	OSVersion   string     `json:"osVersion"`
	Arch        string     `json:"arch"`
	AgentVersion string    `json:"agentVersion"`
	RemoteAddr  string     `json:"remoteAddr"`
	RequestedAt time.Time  `json:"requestedAt"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
	Decision    string     `json:"decision"`
	DecidedBy   string     `json:"decidedBy,omitempty"`
	Note        string     `json:"note,omitempty"`
}

func (s *Store) CreateEnrollmentRequest(ctx context.Context, r EnrollmentRequestRow) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO enrollment_requests (agent_id,machine_id,hostname,platform,os_name,os_version,arch,agent_version,remote_addr,requested_at,decision) VALUES (?,?,?,?,?,?,?,?,?,?, 'pending')`,
		r.AgentID, r.MachineID, r.Hostname, r.Platform, r.OSName, r.OSVersion, r.Arch, r.AgentVersion, r.RemoteAddr, nowStr())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DecideEnrollment(ctx context.Context, agentID, decision, by, note string) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE enrollment_requests SET decision=?, decided_at=?, decided_by=?, note=? WHERE agent_id=? AND decision='pending'`, decision, now, by, note, agentID)
	return err
}

func (s *Store) ListEnrollmentRequests(ctx context.Context, decision string) ([]EnrollmentRequestRow, error) {
	q := `SELECT id,agent_id,machine_id,hostname,COALESCE(platform,''),COALESCE(os_name,''),COALESCE(os_version,''),COALESCE(arch,''),COALESCE(agent_version,''),COALESCE(remote_addr,''),requested_at,decided_at,decision,COALESCE(decided_by,''),COALESCE(note,'') FROM enrollment_requests`
	var args []any
	if decision != "" {
		q += ` WHERE decision=?`
		args = append(args, decision)
	}
	q += ` ORDER BY requested_at DESC LIMIT 500`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnrollmentRequestRow
	for rows.Next() {
		var r EnrollmentRequestRow
		var reqAt string
		var decAt sql.NullString
		if err := rows.Scan(&r.ID, &r.AgentID, &r.MachineID, &r.Hostname, &r.Platform, &r.OSName, &r.OSVersion, &r.Arch, &r.AgentVersion, &r.RemoteAddr, &reqAt, &decAt, &r.Decision, &r.DecidedBy, &r.Note); err != nil {
			return nil, err
		}
		r.RequestedAt = parseTS(reqAt)
		if decAt.Valid {
			t := parseTS(decAt.String)
			r.DecidedAt = &t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- events / logs / audit ----------------------------------------------

func (s *Store) AddAgentEvent(ctx context.Context, agentID, kind, severity, message, details string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO agent_events (agent_id,at,kind,severity,message,details_json) VALUES (?,?,?,?,?,?)`, agentID, nowStr(), kind, severity, message, details)
	return err
}

type AgentEvent struct {
	ID       int64     `json:"id"`
	AgentID  string    `json:"agentId"`
	At       time.Time `json:"at"`
	Kind     string    `json:"kind"`
	Severity string    `json:"severity"`
	Message  string    `json:"message"`
	Details  string    `json:"details,omitempty"`
	Hostname string    `json:"hostname,omitempty"`
}

func (s *Store) ListAgentEvents(ctx context.Context, agentID string, limit int) ([]AgentEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT e.id,e.agent_id,e.at,e.kind,e.severity,e.message,COALESCE(e.details_json,''),COALESCE(a.hostname,'') FROM agent_events e LEFT JOIN agents a ON a.id=e.agent_id`
	var args []any
	if agentID != "" {
		q += ` WHERE e.agent_id=?`
		args = append(args, agentID)
	}
	q += ` ORDER BY e.at DESC, e.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentEvent
	for rows.Next() {
		var e AgentEvent
		var at string
		if err := rows.Scan(&e.ID, &e.AgentID, &at, &e.Kind, &e.Severity, &e.Message, &e.Details, &e.Hostname); err != nil {
			return nil, err
		}
		e.At = parseTS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AddAgentLogs(ctx context.Context, agentID string, entries []models.LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO agent_logs (agent_id,at,level,message) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if _, err := st.ExecContext(ctx, agentID, tsStr(e.Time), e.Level, e.Message); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type AgentLog struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

func (s *Store) ListAgentLogs(ctx context.Context, agentID string, limit int) ([]AgentLog, error) {
	if limit <= 0 || limit > 2000 {
		limit = 300
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT at,level,message FROM agent_logs WHERE agent_id=? ORDER BY at DESC, id DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentLog
	for rows.Next() {
		var l AgentLog
		var at string
		if err := rows.Scan(&at, &l.Level, &l.Message); err != nil {
			return nil, err
		}
		l.At = parseTS(at)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) AddAudit(ctx context.Context, actor, action, target, remote, details string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO audit_events (at,actor,action,target,remote_addr,details_json) VALUES (?,?,?,?,?,?)`, nowStr(), actor, action, target, remote, details)
	return err
}

type AuditEvent struct {
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Target string    `json:"target"`
	Remote string    `json:"remote,omitempty"`
	Details string   `json:"details,omitempty"`
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT at,actor,action,COALESCE(target,''),COALESCE(remote_addr,''),COALESCE(details_json,'') FROM audit_events ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var at string
		if err := rows.Scan(&at, &e.Actor, &e.Action, &e.Target, &e.Remote, &e.Details); err != nil {
			return nil, err
		}
		e.At = parseTS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AddServerEvent(ctx context.Context, level, source, message string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO server_events (at,level,source,message) VALUES (?,?,?,?)`, nowStr(), level, source, message)
	return err
}

type ServerEvent struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level"`
	Source  string    `json:"source"`
	Message string    `json:"message"`
}

func (s *Store) ListServerEvents(ctx context.Context, limit int) ([]ServerEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT at,level,source,message FROM server_events ORDER BY at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ServerEvent
	for rows.Next() {
		var e ServerEvent
		var at string
		if err := rows.Scan(&at, &e.Level, &e.Source, &e.Message); err != nil {
			return nil, err
		}
		e.At = parseTS(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- settings ------------------------------------------------------------

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key=?`, key).Scan(&v)
	if err != nil {
		return "", false
	}
	return v, true
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO server_settings (key,value,updated_at) VALUES (?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, key, value, nowStr())
	return err
}

func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key,value FROM server_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// --- request stats -------------------------------------------------------

func (s *Store) RecordRequest(ctx context.Context, path, method string, status int, durMs float64) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO request_stats (at,path,method,status,duration_ms) VALUES (?,?,?,?,?)`, nowStr(), path, method, status, durMs)
}

type RequestStats struct {
	Total        int64   `json:"total"`
	LastMinute   int64   `json:"lastMinute"`
	LastHour     int64   `json:"lastHour"`
	ErrorsLastHour int64 `json:"errorsLastHour"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
	TopPaths     []PathStat `json:"topPaths"`
}

type PathStat struct {
	Path  string `json:"path"`
	Count int64  `json:"count"`
}

func (s *Store) RequestStatsSummary(ctx context.Context) (RequestStats, error) {
	var rs RequestStats
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_stats`).Scan(&rs.Total)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_stats WHERE at > ?`, tsStr(time.Now().UTC().Add(-time.Minute))).Scan(&rs.LastMinute)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_stats WHERE at > ?`, tsStr(time.Now().UTC().Add(-time.Hour))).Scan(&rs.LastHour)
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_stats WHERE at > ? AND status >= 400`, tsStr(time.Now().UTC().Add(-time.Hour))).Scan(&rs.ErrorsLastHour)
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(AVG(duration_ms),0) FROM request_stats WHERE at > ?`, tsStr(time.Now().UTC().Add(-time.Hour))).Scan(&rs.AvgLatencyMs)
	rows, err := s.DB.QueryContext(ctx, `SELECT path, COUNT(*) c FROM request_stats WHERE at > ? GROUP BY path ORDER BY c DESC LIMIT 8`, tsStr(time.Now().UTC().Add(-time.Hour)))
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p PathStat
			if rows.Scan(&p.Path, &p.Count) == nil {
				rs.TopPaths = append(rs.TopPaths, p)
			}
		}
	}
	return rs, nil
}

// --- groups --------------------------------------------------------------

type Group struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Description string `json:"description,omitempty"`
	AgentCount int64 `json:"agentCount"`
}

func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT g.id,g.name,COALESCE(g.description,''),(SELECT COUNT(*) FROM agents a WHERE a.group_id=g.id) FROM agent_groups g ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.AgentCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, name, description string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO agent_groups (name,description,created_at) VALUES (?,?,?)`, name, description, nowStr())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM agent_groups WHERE id=?`, id)
	return err
}

// CountAgentsByStatus returns a map of status -> count.
func (s *Store) CountAgentsByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT status, COUNT(*) FROM agents GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]int64{}
	for rows.Next() {
		var st string
		var c int64
		if err := rows.Scan(&st, &c); err != nil {
			return nil, err
		}
		m[st] = c
	}
	return m, rows.Err()
}
