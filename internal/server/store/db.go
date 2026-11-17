// Package store implements the SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database.
type Store struct {
	DB   *sql.DB
	Path string
}

// Open opens (creating if needed) the database at path and applies migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	dsn := "file:" + filepath.ToSlash(path) + "?" + q.Encode()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// modernc/sqlite is safe with concurrency, but a single writer avoids
	// SQLITE_BUSY storms with several agents reporting at once.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)
	s := &Store{DB: db, Path: path}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// SizeBytes returns the total size of the database including WAL.
func (s *Store) SizeBytes() int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(s.Path + suffix); err == nil {
			total += fi.Size()
		}
	}
	return total
}

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = []migration{
	{1, "initial", schemaV1},
	{2, "agent_groups_and_audit", schemaV2},
	{3, "discovery", schemaV3},
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var current int
	row := s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`)
	if err := row.Scan(&current); err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version,name,applied_at) VALUES (?,?,?)`, m.version, m.name, nowStr()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// timeLayout is the canonical timestamp format used in SQLite TEXT columns.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

func nowStr() string { return time.Now().UTC().Format(timeLayout) }

func tsStr(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, l := range []string{timeLayout, time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func nullStr(ns sql.NullString) string { return strings.TrimSpace(ns.String) }

const schemaV1 = `
CREATE TABLE IF NOT EXISTS server_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admins (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_login_at TEXT
);

CREATE TABLE IF NOT EXISTS admin_sessions (
  token_hash TEXT PRIMARY KEY,
  admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  user_agent TEXT
);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_exp ON admin_sessions(expires_at);

CREATE TABLE IF NOT EXISTS agent_groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  created_at TEXT NOT NULL
);

-- An agent is the installed instance on a machine. Status values:
-- pending | approved | rejected | disabled | revoked
CREATE TABLE IF NOT EXISTS agents (
  id TEXT PRIMARY KEY,                 -- server-issued agent id (uuid)
  machine_id TEXT NOT NULL UNIQUE,     -- stable per-install id chosen by the agent
  hostname TEXT NOT NULL,
  display_name TEXT,
  platform TEXT NOT NULL,
  os_name TEXT,
  os_version TEXT,
  arch TEXT,
  agent_version TEXT,
  status TEXT NOT NULL,
  group_id INTEGER REFERENCES agent_groups(id) ON DELETE SET NULL,
  location TEXT,
  notes TEXT,
  primary_ip TEXT,
  primary_mac TEXT,
  enrollment_token_hash TEXT,          -- hash of the pending enrollment secret
  token_hash TEXT,                     -- hash of the active API token
  token_prefix TEXT,                   -- first 8 chars for display
  token_rotated_at TEXT,
  previous_token_hash TEXT,            -- accepted briefly after rotation
  previous_token_expires_at TEXT,
  config_json TEXT,                    -- server-side AgentConfig override
  enrolled_at TEXT NOT NULL,
  approved_at TEXT,
  approved_by TEXT,
  last_heartbeat_at TEXT,
  last_metrics_at TEXT,
  last_inventory_at TEXT,
  last_ip TEXT,
  last_error TEXT,
  pending_buffered INTEGER NOT NULL DEFAULT 0,
  status_note TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status);
CREATE INDEX IF NOT EXISTS idx_agents_machine ON agents(machine_id);
CREATE INDEX IF NOT EXISTS idx_agents_group ON agents(group_id);

CREATE TABLE IF NOT EXISTS agent_credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,
  prefix TEXT,
  created_at TEXT NOT NULL,
  expires_at TEXT,
  revoked_at TEXT,
  reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_credentials_agent ON agent_credentials(agent_id);

CREATE TABLE IF NOT EXISTS enrollment_requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  machine_id TEXT NOT NULL,
  hostname TEXT NOT NULL,
  platform TEXT,
  os_name TEXT,
  os_version TEXT,
  arch TEXT,
  agent_version TEXT,
  remote_addr TEXT,
  requested_at TEXT NOT NULL,
  decided_at TEXT,
  decision TEXT NOT NULL DEFAULT 'pending', -- pending | approved | rejected
  decided_by TEXT,
  note TEXT
);
CREATE INDEX IF NOT EXISTS idx_enrollment_decision ON enrollment_requests(decision);

-- Current inventory snapshot (one row per agent) plus the history table.
CREATE TABLE IF NOT EXISTS inventory_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  hash TEXT NOT NULL,
  collected_at TEXT NOT NULL,
  received_at TEXT NOT NULL,
  reason TEXT,
  inventory_json TEXT NOT NULL,
  schema_version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_snapshots_agent ON inventory_snapshots(agent_id, collected_at DESC);

-- Denormalised columns for fast table queries; rebuilt from the JSON on ingest.
CREATE TABLE IF NOT EXISTS inventory_current (
  agent_id TEXT PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
  snapshot_id INTEGER NOT NULL REFERENCES inventory_snapshots(id) ON DELETE CASCADE,
  hostname TEXT,
  platform TEXT,
  os_name TEXT,
  os_edition TEXT,
  os_version TEXT,
  os_build TEXT,
  arch TEXT,
  uptime_seconds INTEGER,
  boot_time TEXT,
  install_date TEXT,
  cpu_manufacturer TEXT,
  cpu_brand TEXT,
  cpu_physical_cores INTEGER,
  cpu_logical_cores INTEGER,
  ram_bytes INTEGER,
  ram_type TEXT,
  ram_module_count INTEGER,
  gpu_names TEXT,       -- '; ' joined
  gpu_manufacturers TEXT,
  gpu_vram_bytes INTEGER,
  storage_total_bytes INTEGER,
  storage_free_bytes INTEGER,
  disk_count INTEGER,
  disk_media TEXT,      -- '; ' joined distinct media types
  volume_count INTEGER,
  motherboard TEXT,
  system_model TEXT,
  system_manufacturer TEXT,
  primary_ip TEXT,
  primary_mac TEXT,
  macs TEXT,            -- '; ' joined
  ips TEXT,             -- '; ' joined
  monitor_count INTEGER,
  software_count INTEGER,
  adapter_count INTEGER,
  hardware_summary TEXT
);

CREATE TABLE IF NOT EXISTS hardware_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  detected_at TEXT NOT NULL,
  category TEXT NOT NULL,   -- cpu | ram | gpu | storage | network | os | hostname | software | other
  severity TEXT NOT NULL,   -- info | warning | critical
  summary TEXT NOT NULL,
  details_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_hwchanges_agent ON hardware_changes(agent_id, detected_at DESC);
CREATE INDEX IF NOT EXISTS idx_hwchanges_time ON hardware_changes(detected_at DESC);

-- Performance samples: raw rows, then aggregated hourly/daily buckets.
CREATE TABLE IF NOT EXISTS performance_samples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  collected_at TEXT NOT NULL,
  cpu_percent REAL,
  mem_used_bytes INTEGER,
  mem_total_bytes INTEGER,
  mem_percent REAL,
  swap_used_bytes INTEGER,
  disk_read_bps REAL,
  disk_write_bps REAL,
  net_rx_bps REAL,
  net_tx_bps REAL,
  process_count INTEGER,
  load1 REAL,
  battery_percent REAL,
  max_disk_percent REAL,
  max_volume_usage REAL,
  details_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_perf_agent_time ON performance_samples(agent_id, collected_at);
CREATE INDEX IF NOT EXISTS idx_perf_time ON performance_samples(collected_at);

CREATE TABLE IF NOT EXISTS performance_rollups (
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  bucket TEXT NOT NULL,        -- hour | day
  bucket_start TEXT NOT NULL,
  samples INTEGER NOT NULL,
  cpu_avg REAL, cpu_max REAL,
  mem_avg REAL, mem_max REAL,
  disk_read_avg REAL, disk_write_avg REAL,
  net_rx_avg REAL, net_tx_avg REAL,
  net_rx_max REAL, net_tx_max REAL,
  disk_util_avg REAL,
  PRIMARY KEY (agent_id, bucket, bucket_start)
);

CREATE TABLE IF NOT EXISTS agent_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  at TEXT NOT NULL,
  kind TEXT NOT NULL,          -- online | offline | enroll | approve | reject | disable | enable | revoke | ip_change | agent_upgrade | inventory | error | config
  severity TEXT NOT NULL DEFAULT 'info',
  message TEXT NOT NULL,
  details_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_agent_events_agent ON agent_events(agent_id, at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_events_time ON agent_events(at DESC);

CREATE TABLE IF NOT EXISTS agent_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  at TEXT NOT NULL,
  level TEXT NOT NULL,
  message TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_logs_agent ON agent_logs(agent_id, at DESC);

CREATE TABLE IF NOT EXISTS alert_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  key TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  threshold REAL,
  cooldown_sec INTEGER NOT NULL DEFAULT 3600,
  severity TEXT NOT NULL DEFAULT 'warning',
  notify_email INTEGER NOT NULL DEFAULT 0,
  notify_telegram INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS alerts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  rule_key TEXT NOT NULL,
  agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
  opened_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT,
  severity TEXT NOT NULL,
  status TEXT NOT NULL,     -- open | acknowledged | resolved
  title TEXT NOT NULL,
  message TEXT NOT NULL,
  acknowledged_at TEXT,
  acknowledged_by TEXT,
  last_notified_at TEXT,
  details_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status, opened_at DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_agent ON alerts(agent_id, rule_key);

CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT,
  remote_addr TEXT,
  details_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_events(at DESC);

CREATE TABLE IF NOT EXISTS server_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  level TEXT NOT NULL,
  source TEXT NOT NULL,
  message TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_server_events_time ON server_events(at DESC);

CREATE TABLE IF NOT EXISTS request_stats (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL,
  path TEXT NOT NULL,
  method TEXT NOT NULL,
  status INTEGER NOT NULL,
  duration_ms REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_request_stats_time ON request_stats(at DESC);

INSERT OR IGNORE INTO alert_rules (key,name,enabled,threshold,cooldown_sec,severity,updated_at) VALUES
 ('agent_offline','Agent offline',1,90,1800,'critical',datetime('now')),
 ('agent_online','Agent reconnected',1,NULL,0,'info',datetime('now')),
 ('disk_low','Low disk space',1,10,21600,'warning',datetime('now')),
 ('cpu_high','Sustained high CPU',1,90,3600,'warning',datetime('now')),
 ('ram_high','Sustained high RAM',1,90,3600,'warning',datetime('now')),
 ('hardware_change','Hardware change detected',1,NULL,0,'info',datetime('now')),
 ('agent_outdated','Agent version outdated',1,NULL,86400,'info',datetime('now')),
 ('agent_silent','Agent stopped reporting',1,900,3600,'critical',datetime('now')),
 ('disk_health','Disk health problem',1,NULL,86400,'critical',datetime('now'));
`

const schemaV2 = `
ALTER TABLE agents ADD COLUMN is_laptop INTEGER NOT NULL DEFAULT 0;
ALTER TABLE agents ADD COLUMN pending_plain_token TEXT;
CREATE TABLE IF NOT EXISTS saved_groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'custom', -- custom | os | manufacturer | location
  filter_json TEXT,
  created_at TEXT NOT NULL
);
`

const schemaV3 = `
CREATE TABLE IF NOT EXISTS discovery_scans (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  target TEXT NOT NULL,
  method TEXT NOT NULL,
  hosts_found INTEGER NOT NULL DEFAULT 0,
  hosts_scanned INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  message TEXT,
  started_by TEXT
);
CREATE TABLE IF NOT EXISTS discovered_devices (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  scan_id INTEGER REFERENCES discovery_scans(id) ON DELETE SET NULL,
  ip TEXT NOT NULL,
  mac TEXT,
  hostname TEXT,
  vendor TEXT,
  open_ports TEXT,
  source TEXT NOT NULL DEFAULT 'scan', -- scan | manual
  agent_present INTEGER NOT NULL DEFAULT 0,
  agent_id TEXT,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  UNIQUE(ip, mac)
);
CREATE INDEX IF NOT EXISTS idx_discovered_ip ON discovered_devices(ip);
`
