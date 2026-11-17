package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Scan struct {
	ID          int64      `json:"id"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Target      string     `json:"target"`
	Method      string     `json:"method"`
	HostsFound  int        `json:"hostsFound"`
	HostsScanned int       `json:"hostsScanned"`
	Status      string     `json:"status"`
	Message     string     `json:"message,omitempty"`
	StartedBy   string     `json:"startedBy,omitempty"`
}

type DiscoveredDevice struct {
	ID         int64      `json:"id"`
	ScanID     *int64     `json:"scanId,omitempty"`
	IP         string     `json:"ip"`
	MAC        string     `json:"mac,omitempty"`
	Hostname   string     `json:"hostname,omitempty"`
	Vendor     string     `json:"vendor,omitempty"`
	OpenPorts  string     `json:"openPorts,omitempty"`
	Source     string     `json:"source"`
	AgentPresent bool     `json:"agentPresent"`
	AgentID    string     `json:"agentId,omitempty"`
	AgentHostname string  `json:"agentHostname,omitempty"`
	AgentStatus string    `json:"agentStatus,omitempty"`
	FirstSeen  time.Time  `json:"firstSeen"`
	LastSeen   time.Time  `json:"lastSeen"`
}

func (s *Store) CreateScan(ctx context.Context, target, method, by string) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO discovery_scans (started_at,target,method,status,started_by) VALUES (?,?,?,'running',?)`, nowStr(), target, method, by)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishScan(ctx context.Context, id int64, status string, scanned, found int, message string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE discovery_scans SET finished_at=?, status=?, hosts_scanned=?, hosts_found=?, message=? WHERE id=?`, nowStr(), status, scanned, found, message, id)
	return err
}

func (s *Store) ListScans(ctx context.Context, limit int) ([]Scan, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,started_at,finished_at,target,method,hosts_found,hosts_scanned,status,COALESCE(message,''),COALESCE(started_by,'') FROM discovery_scans ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scan
	for rows.Next() {
		var sc Scan
		var started string
		var finished sql.NullString
		if err := rows.Scan(&sc.ID, &started, &finished, &sc.Target, &sc.Method, &sc.HostsFound, &sc.HostsScanned, &sc.Status, &sc.Message, &sc.StartedBy); err != nil {
			return nil, err
		}
		sc.StartedAt = parseTS(started)
		if finished.Valid {
			t := parseTS(finished.String)
			sc.FinishedAt = &t
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Store) UpsertDiscoveredDevice(ctx context.Context, d DiscoveredDevice) error {
	now := nowStr()
	_, err := s.DB.ExecContext(ctx, `INSERT INTO discovered_devices (scan_id,ip,mac,hostname,vendor,open_ports,source,agent_present,agent_id,first_seen,last_seen)
	 VALUES (?,?,?,?,?,?,?,?,?,?,?)
	 ON CONFLICT(ip,mac) DO UPDATE SET scan_id=excluded.scan_id, hostname=CASE WHEN excluded.hostname<>'' THEN excluded.hostname ELSE discovered_devices.hostname END,
	  vendor=CASE WHEN excluded.vendor<>'' THEN excluded.vendor ELSE discovered_devices.vendor END,
	  open_ports=CASE WHEN excluded.open_ports<>'' THEN excluded.open_ports ELSE discovered_devices.open_ports END,
	  last_seen=excluded.last_seen`, d.ScanID, d.IP, d.MAC, d.Hostname, d.Vendor, d.OpenPorts, d.Source, boolInt(d.AgentPresent), nullIfEmpty(d.AgentID), now, now)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) ListDiscoveredDevices(ctx context.Context) ([]DiscoveredDevice, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,d.scan_id,d.ip,COALESCE(d.mac,''),COALESCE(d.hostname,''),COALESCE(d.vendor,''),COALESCE(d.open_ports,''),d.source,d.agent_present,COALESCE(d.agent_id,''),COALESCE(a.hostname,''),COALESCE(a.status,''),d.first_seen,d.last_seen
	 FROM discovered_devices d LEFT JOIN agents a ON a.id=d.agent_id ORDER BY d.last_seen DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiscoveredDevice
	for rows.Next() {
		var d DiscoveredDevice
		var scanID sql.NullInt64
		var present int
		var first, last string
		if err := rows.Scan(&d.ID, &scanID, &d.IP, &d.MAC, &d.Hostname, &d.Vendor, &d.OpenPorts, &d.Source, &present, &d.AgentID, &d.AgentHostname, &d.AgentStatus, &first, &last); err != nil {
			return nil, err
		}
		if scanID.Valid {
			v := scanID.Int64
			d.ScanID = &v
		}
		d.AgentPresent = present != 0
		d.FirstSeen = parseTS(first)
		d.LastSeen = parseTS(last)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDiscoveredDevice(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM discovered_devices WHERE id=?`, id)
	return err
}

// MarkAgentPresentOnIP flags discovered devices that match a known agent IP.
func (s *Store) LinkDiscoveredToAgents(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, COALESCE(primary_ip,''), COALESCE(last_ip,''), COALESCE(primary_mac,'') FROM agents WHERE status IN ('approved','pending')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type rec struct{ id, ip1, ip2, mac string }
	var recs []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.ip1, &r.ip2, &r.mac); err != nil {
			return err
		}
		recs = append(recs, r)
	}
	for _, r := range recs {
		for _, ip := range []string{r.ip1, r.ip2} {
			if ip == "" {
				continue
			}
			if _, err := s.DB.ExecContext(ctx, `UPDATE discovered_devices SET agent_present=1, agent_id=? WHERE ip=?`, r.id, ip); err != nil {
				return err
			}
		}
	}
	return nil
}

var ErrScanNotFound = errors.New("scan not found")

func (s *Store) GetScan(ctx context.Context, id int64) (*Scan, error) {
	scans, err := s.ListScans(ctx, 1000)
	if err != nil {
		return nil, err
	}
	for i := range scans {
		if scans[i].ID == id {
			return &scans[i], nil
		}
	}
	return nil, ErrScanNotFound
}
