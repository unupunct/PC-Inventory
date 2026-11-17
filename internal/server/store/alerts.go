package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Alert rule keys.
const (
	RuleAgentOffline   = "agent_offline"
	RuleAgentOnline    = "agent_online"
	RuleDiskLow        = "disk_low"
	RuleCPUHigh        = "cpu_high"
	RuleRAMHigh        = "ram_high"
	RuleHardwareChange = "hardware_change"
	RuleAgentOutdated  = "agent_outdated"
	RuleAgentSilent    = "agent_silent"
	RuleDiskHealth     = "disk_health"
)

type AlertRule struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	Threshold      *float64 `json:"threshold,omitempty"`
	CooldownSec    int      `json:"cooldownSec"`
	Severity       string   `json:"severity"`
	NotifyEmail    bool     `json:"notifyEmail"`
	NotifyTelegram bool     `json:"notifyTelegram"`
	Description    string   `json:"description,omitempty"`
}

var ruleDescriptions = map[string]string{
	RuleAgentOffline:   "Raised when an approved agent misses heartbeats for longer than the offline threshold (seconds).",
	RuleAgentOnline:    "Raised when an agent that was offline reports again.",
	RuleDiskLow:        "Raised when a volume's free space falls below the threshold (percent).",
	RuleCPUHigh:        "Raised when average CPU stays above the threshold (percent) over recent samples.",
	RuleRAMHigh:        "Raised when memory usage stays above the threshold (percent) over recent samples.",
	RuleHardwareChange: "Raised when an inventory report shows changed hardware.",
	RuleAgentOutdated:  "Raised when an agent runs an older version than the server.",
	RuleAgentSilent:    "Raised when no report of any kind has been received for the threshold (seconds).",
	RuleDiskHealth:     "Raised when a physical disk reports a failing or warning SMART state.",
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key,name,enabled,threshold,cooldown_sec,severity,notify_email,notify_telegram FROM alert_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertRule
	for rows.Next() {
		var r AlertRule
		var enabled, ne, nt int
		var th sql.NullFloat64
		if err := rows.Scan(&r.Key, &r.Name, &enabled, &th, &r.CooldownSec, &r.Severity, &ne, &nt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		r.NotifyEmail = ne != 0
		r.NotifyTelegram = nt != 0
		if th.Valid {
			v := th.Float64
			r.Threshold = &v
		}
		r.Description = ruleDescriptions[r.Key]
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetAlertRule(ctx context.Context, key string) (*AlertRule, error) {
	rules, err := s.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].Key == key {
			return &rules[i], nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) UpdateAlertRule(ctx context.Context, r AlertRule) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE alert_rules SET enabled=?, threshold=?, cooldown_sec=?, severity=?, notify_email=?, notify_telegram=?, updated_at=? WHERE key=?`,
		boolInt(r.Enabled), r.Threshold, r.CooldownSec, r.Severity, boolInt(r.NotifyEmail), boolInt(r.NotifyTelegram), nowStr(), r.Key)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type Alert struct {
	ID           int64      `json:"id"`
	RuleKey      string     `json:"ruleKey"`
	AgentID      *string    `json:"agentId,omitempty"`
	Hostname     string     `json:"hostname,omitempty"`
	OpenedAt     time.Time  `json:"openedAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	ResolvedAt   *time.Time `json:"resolvedAt,omitempty"`
	Severity     string     `json:"severity"`
	Status       string     `json:"status"`
	Title        string     `json:"title"`
	Message      string     `json:"message"`
	AckedAt      *time.Time `json:"acknowledgedAt,omitempty"`
	AckedBy      string     `json:"acknowledgedBy,omitempty"`
	LastNotified *time.Time `json:"lastNotifiedAt,omitempty"`
	Details      string     `json:"details,omitempty"`
}

func (s *Store) OpenAlert(ctx context.Context, ruleKey string, agentID *string, severity, title, message, details string) (int64, error) {
	now := nowStr()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO alerts (rule_key,agent_id,opened_at,updated_at,severity,status,title,message,details_json) VALUES (?,?,?,?,?, 'open',?,?,?)`,
		ruleKey, agentID, now, now, severity, title, message, details)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FindOpenAlert returns the newest open alert for a rule/agent pair.
func (s *Store) FindOpenAlert(ctx context.Context, ruleKey string, agentID *string) (*Alert, error) {
	var a Alert
	var agent sql.NullString
	var opened, updated, resolved, acked, notified sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id,rule_key,agent_id,opened_at,updated_at,resolved_at,severity,status,title,message,acknowledged_at,COALESCE(acknowledged_by,''),last_notified_at,COALESCE(details_json,'')
	  FROM alerts WHERE rule_key=? AND COALESCE(agent_id,'')=COALESCE(?,'') AND status != 'resolved' ORDER BY id DESC LIMIT 1`, ruleKey, agentID).
		Scan(&a.ID, &a.RuleKey, &agent, &opened, &updated, &resolved, &a.Severity, &a.Status, &a.Title, &a.Message, &acked, &a.AckedBy, &notified, &a.Details)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if agent.Valid {
		v := agent.String
		a.AgentID = &v
	}
	a.OpenedAt = parseTS(opened.String)
	a.UpdatedAt = parseTS(updated.String)
	if resolved.Valid {
		t := parseTS(resolved.String)
		a.ResolvedAt = &t
	}
	if acked.Valid {
		t := parseTS(acked.String)
		a.AckedAt = &t
	}
	if notified.Valid {
		t := parseTS(notified.String)
		a.LastNotified = &t
	}
	return &a, nil
}

func (s *Store) UpdateAlertMessage(ctx context.Context, id int64, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE alerts SET message=?, updated_at=? WHERE id=?`, message, nowStr(), id)
	return err
}

func (s *Store) MarkAlertNotified(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE alerts SET last_notified_at=? WHERE id=?`, nowStr(), id)
	return err
}

func (s *Store) ResolveAlert(ctx context.Context, id int64) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE alerts SET status='resolved', resolved_at=?, updated_at=? WHERE id=? AND status != 'resolved'`, now, now, id)
	return err
}

func (s *Store) AckAlert(ctx context.Context, id int64, by string) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE alerts SET status=CASE WHEN status='resolved' THEN 'resolved' ELSE 'acknowledged' END, acknowledged_at=?, acknowledged_by=?, updated_at=? WHERE id=?`, now, by, now, id)
	return err
}

func (s *Store) ListAlerts(ctx context.Context, status, severity, agentID string, limit int) ([]Alert, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT al.id,al.rule_key,al.agent_id,COALESCE(a.hostname,''),al.opened_at,al.updated_at,al.resolved_at,al.severity,al.status,al.title,al.message,al.acknowledged_at,COALESCE(al.acknowledged_by,''),al.last_notified_at,COALESCE(al.details_json,'')
	 FROM alerts al LEFT JOIN agents a ON a.id=al.agent_id WHERE 1=1`
	var args []any
	if status != "" {
		if status == "active" {
			q += ` AND al.status != 'resolved'`
		} else {
			q += ` AND al.status=?`
			args = append(args, status)
		}
	}
	if severity != "" {
		q += ` AND al.severity=?`
		args = append(args, severity)
	}
	if agentID != "" {
		q += ` AND al.agent_id=?`
		args = append(args, agentID)
	}
	q += ` ORDER BY al.opened_at DESC, al.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var agent, resolved, acked, notified sql.NullString
		var opened, updated string
		if err := rows.Scan(&a.ID, &a.RuleKey, &agent, &a.Hostname, &opened, &updated, &resolved, &a.Severity, &a.Status, &a.Title, &a.Message, &acked, &a.AckedBy, &notified, &a.Details); err != nil {
			return nil, err
		}
		if agent.Valid {
			v := agent.String
			a.AgentID = &v
		}
		a.OpenedAt = parseTS(opened)
		a.UpdatedAt = parseTS(updated)
		if resolved.Valid {
			t := parseTS(resolved.String)
			a.ResolvedAt = &t
		}
		if acked.Valid {
			t := parseTS(acked.String)
			a.AckedAt = &t
		}
		if notified.Valid {
			t := parseTS(notified.String)
			a.LastNotified = &t
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AlertCounts(ctx context.Context) (map[string]int64, error) {
	m := map[string]int64{"open": 0, "acknowledged": 0, "critical": 0, "warning": 0, "info": 0, "resolved": 0}
	rows, err := s.DB.QueryContext(ctx, `SELECT status, severity, COUNT(*) FROM alerts GROUP BY status, severity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var st, sev string
		var c int64
		if err := rows.Scan(&st, &sev, &c); err != nil {
			return nil, err
		}
		if st != "resolved" {
			m["open"] += c
			m[sev] += c
			if st == "acknowledged" {
				m["acknowledged"] += c
			}
		} else {
			m["resolved"] += c
		}
	}
	return m, rows.Err()
}

// ResolveAlertsForAgent resolves all open alerts of an agent (used on revoke/delete).
func (s *Store) ResolveAlertsForAgent(ctx context.Context, agentID string) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `UPDATE alerts SET status='resolved', resolved_at=?, updated_at=? WHERE agent_id=? AND status != 'resolved'`, now, now, agentID)
	return err
}

// ResolveStaleAlerts resolves open alerts whose agent is now healthy again,
// based on a predicate evaluated by the caller.
func (s *Store) OpenAlerts(ctx context.Context) ([]Alert, error) {
	return s.ListAlerts(ctx, "active", "", "", 1000)
}

func (s *Store) TimeNow() time.Time { return time.Now().UTC() }
