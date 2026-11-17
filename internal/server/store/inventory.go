package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

// Snapshot is a stored inventory snapshot.
type Snapshot struct {
	ID          int64
	AgentID     string
	Hash        string
	CollectedAt time.Time
	ReceivedAt  time.Time
	Reason      string
	Inventory   models.Inventory
}

// SaveInventory stores a snapshot and refreshes the denormalised current row.
// Returns the snapshot id and whether the hardware hash changed.
func (s *Store) SaveInventory(ctx context.Context, agentID string, inv models.Inventory, reason string) (int64, bool, error) {
	b, err := json.Marshal(inv)
	if err != nil {
		return 0, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()

	var prevHash string
	var prevID int64
	err = tx.QueryRowContext(ctx, `SELECT id,hash FROM inventory_snapshots WHERE agent_id=? ORDER BY collected_at DESC, id DESC LIMIT 1`, agentID).Scan(&prevID, &prevHash)
	hasPrev := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	changed := !hasPrev || prevHash != inv.Hash
	now := nowStr()
	res, err := tx.ExecContext(ctx, `INSERT INTO inventory_snapshots (agent_id,hash,collected_at,received_at,reason,inventory_json) VALUES (?,?,?,?,?,?)`,
		agentID, inv.Hash, tsStr(inv.CollectedAt), now, reason, string(b))
	if err != nil {
		return 0, false, err
	}
	snapID, _ := res.LastInsertId()
	if err := upsertCurrent(ctx, tx, agentID, snapID, inv); err != nil {
		return 0, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agents SET last_inventory_at=?, updated_at=? WHERE id=?`, now, now, agentID); err != nil {
		return 0, false, err
	}
	// Keep a bounded history of snapshots (per agent) to avoid unbounded growth.
	var count int64
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_snapshots WHERE agent_id=?`, agentID).Scan(&count)
	if count > 60 {
		_, _ = tx.ExecContext(ctx, `DELETE FROM inventory_snapshots WHERE agent_id=? AND id NOT IN (SELECT id FROM inventory_snapshots WHERE agent_id=? ORDER BY collected_at DESC LIMIT 60)`, agentID, agentID)
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return snapID, changed, nil
}

func joinStrings(ss []string) string {
	out := ""
	for i, s := range ss {
		if s == "" {
			continue
		}
		if out != "" {
			out += "; "
		}
		out += s
		_ = i
	}
	return out
}

func upsertCurrent(ctx context.Context, tx *sql.Tx, agentID string, snapID int64, inv models.Inventory) error {
	var ramType string
	var ramBytes uint64
	for _, m := range inv.Memory.Modules {
		ramBytes += m.CapacityBytes
		if ramType == "" {
			ramType = m.Type
		}
	}
	if ramBytes == 0 {
		ramBytes = inv.Memory.TotalBytes
	}
	var gpuNames, gpuMfrs []string
	var vram uint64
	for _, g := range inv.GPUs {
		gpuNames = append(gpuNames, g.Name)
		if g.Manufacturer != "" {
			gpuMfrs = append(gpuMfrs, g.Manufacturer)
		}
		if g.VRAMBytes != nil && *g.VRAMBytes > vram {
			vram = *g.VRAMBytes
		}
	}
	var storageTotal, storageFree uint64
	var mediaTypes []string
	seenMedia := map[string]bool{}
	for _, d := range inv.PhysicalDisks {
		storageTotal += d.CapacityBytes
		if d.MediaType != "" && !seenMedia[d.MediaType] {
			seenMedia[d.MediaType] = true
			mediaTypes = append(mediaTypes, d.MediaType)
		}
	}
	for _, v := range inv.Volumes {
		storageFree += v.FreeBytes
	}
	if storageTotal == 0 {
		for _, v := range inv.Volumes {
			storageTotal += v.TotalBytes
		}
	}
	var macs, ips []string
	primaryIP, primaryMAC := "", ""
	for _, n := range inv.NetAdapters {
		if n.MAC != "" {
			macs = append(macs, n.MAC)
		}
		ips = append(ips, n.IPv4...)
		if primaryIP == "" && n.Status == "up" && n.Kind != "loopback" && len(n.IPv4) > 0 && n.Kind != "virtual" {
			primaryIP = n.IPv4[0]
			primaryMAC = n.MAC
		}
	}
	if primaryIP == "" && len(ips) > 0 {
		primaryIP = ips[0]
	}
	summary := inv.CPU.Brand
	var bootTime any
	if inv.OS.BootTime != nil {
		bootTime = tsStr(*inv.OS.BootTime)
	}
	var installDate any
	if inv.OS.InstallDate != nil {
		installDate = tsStr(*inv.OS.InstallDate)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO inventory_current
	(agent_id,snapshot_id,hostname,platform,os_name,os_edition,os_version,os_build,arch,uptime_seconds,boot_time,install_date,
	 cpu_manufacturer,cpu_brand,cpu_physical_cores,cpu_logical_cores,ram_bytes,ram_type,ram_module_count,gpu_names,gpu_manufacturers,gpu_vram_bytes,
	 storage_total_bytes,storage_free_bytes,disk_count,disk_media,volume_count,motherboard,system_model,system_manufacturer,primary_ip,primary_mac,macs,ips,
	 monitor_count,software_count,adapter_count,hardware_summary)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(agent_id) DO UPDATE SET snapshot_id=excluded.snapshot_id, hostname=excluded.hostname, platform=excluded.platform, os_name=excluded.os_name,
	 os_edition=excluded.os_edition, os_version=excluded.os_version, os_build=excluded.os_build, arch=excluded.arch, uptime_seconds=excluded.uptime_seconds,
	 boot_time=excluded.boot_time, install_date=excluded.install_date, cpu_manufacturer=excluded.cpu_manufacturer, cpu_brand=excluded.cpu_brand,
	 cpu_physical_cores=excluded.cpu_physical_cores, cpu_logical_cores=excluded.cpu_logical_cores, ram_bytes=excluded.ram_bytes, ram_type=excluded.ram_type,
	 ram_module_count=excluded.ram_module_count, gpu_names=excluded.gpu_names, gpu_manufacturers=excluded.gpu_manufacturers, gpu_vram_bytes=excluded.gpu_vram_bytes,
	 storage_total_bytes=excluded.storage_total_bytes, storage_free_bytes=excluded.storage_free_bytes, disk_count=excluded.disk_count, disk_media=excluded.disk_media,
	 volume_count=excluded.volume_count, motherboard=excluded.motherboard, system_model=excluded.system_model, system_manufacturer=excluded.system_manufacturer,
	 primary_ip=excluded.primary_ip, primary_mac=excluded.primary_mac, macs=excluded.macs, ips=excluded.ips, monitor_count=excluded.monitor_count,
	 software_count=excluded.software_count, adapter_count=excluded.adapter_count, hardware_summary=excluded.hardware_summary`,
		agentID, snapID, inv.Host.Hostname, inv.Host.Platform, inv.OS.Name, inv.OS.Edition, inv.OS.Version, inv.OS.Build, inv.OS.Arch, inv.OS.UptimeSeconds, bootTime, installDate,
		inv.CPU.Manufacturer, inv.CPU.Brand, inv.CPU.PhysicalCores, inv.CPU.LogicalCores, ramBytes, ramType, len(inv.Memory.Modules), joinStrings(gpuNames), joinStrings(gpuMfrs), vram,
		storageTotal, storageFree, len(inv.PhysicalDisks), joinStrings(mediaTypes), len(inv.Volumes),
		joinStrings([]string{inv.Motherboard.Manufacturer, inv.Motherboard.Product}), inv.Motherboard.SystemProduct, inv.Motherboard.SystemManufacturer,
		primaryIP, primaryMAC, joinStrings(macs), joinStrings(ips), len(inv.Monitors), len(inv.Software), len(inv.NetAdapters), summary)
	return err
}

// LatestSnapshot returns the most recent inventory for an agent.
func (s *Store) LatestSnapshot(ctx context.Context, agentID string) (*Snapshot, error) {
	var sn Snapshot
	var collected, received, js string
	err := s.DB.QueryRowContext(ctx, `SELECT id,agent_id,hash,collected_at,received_at,COALESCE(reason,''),inventory_json FROM inventory_snapshots WHERE agent_id=? ORDER BY collected_at DESC, id DESC LIMIT 1`, agentID).
		Scan(&sn.ID, &sn.AgentID, &sn.Hash, &collected, &received, &sn.Reason, &js)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sn.CollectedAt = parseTS(collected)
	sn.ReceivedAt = parseTS(received)
	if err := json.Unmarshal([]byte(js), &sn.Inventory); err != nil {
		return nil, err
	}
	return &sn, nil
}

// PreviousSnapshot returns the snapshot before the latest one.
func (s *Store) PreviousSnapshot(ctx context.Context, agentID string) (*Snapshot, error) {
	var sn Snapshot
	var collected, received, js string
	err := s.DB.QueryRowContext(ctx, `SELECT id,agent_id,hash,collected_at,received_at,COALESCE(reason,''),inventory_json FROM inventory_snapshots WHERE agent_id=? ORDER BY collected_at DESC, id DESC LIMIT 1 OFFSET 1`, agentID).
		Scan(&sn.ID, &sn.AgentID, &sn.Hash, &collected, &received, &sn.Reason, &js)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sn.CollectedAt = parseTS(collected)
	sn.ReceivedAt = parseTS(received)
	if err := json.Unmarshal([]byte(js), &sn.Inventory); err != nil {
		return nil, err
	}
	return &sn, nil
}

// InventoryRow is the flattened inventory used by list views and dashboards.
type InventoryRow struct {
	AgentID          string     `json:"agentId"`
	Hostname         string     `json:"hostname"`
	DisplayName      string     `json:"displayName"`
	Platform         string     `json:"platform"`
	OSName           string     `json:"osName"`
	OSEdition        string     `json:"osEdition"`
	OSVersion        string     `json:"osVersion"`
	Arch             string     `json:"arch"`
	UptimeSeconds    uint64     `json:"uptimeSeconds"`
	CPUManufacturer  string     `json:"cpuManufacturer"`
	CPUBrand         string     `json:"cpuBrand"`
	CPUPhysicalCores int        `json:"cpuPhysicalCores"`
	CPULogicalCores  int        `json:"cpuLogicalCores"`
	RAMBytes         uint64     `json:"ramBytes"`
	RAMType          string     `json:"ramType"`
	GPUNames         string     `json:"gpuNames"`
	GPUManufacturers string     `json:"gpuManufacturers"`
	StorageTotal     uint64     `json:"storageTotalBytes"`
	StorageFree      uint64     `json:"storageFreeBytes"`
	DiskCount        int        `json:"diskCount"`
	DiskMedia        string     `json:"diskMedia"`
	VolumeCount      int        `json:"volumeCount"`
	Motherboard      string     `json:"motherboard"`
	SystemModel      string     `json:"systemModel"`
	SystemManufacturer string   `json:"systemManufacturer"`
	PrimaryIP        string     `json:"primaryIp"`
	PrimaryMAC       string     `json:"primaryMac"`
	IPs              string     `json:"ips"`
	MonitorCount     int        `json:"monitorCount"`
	SoftwareCount    int        `json:"softwareCount"`
	AdapterCount     int        `json:"adapterCount"`
	SnapshotID       int64      `json:"snapshotId"`
	LastInventoryAt  *time.Time `json:"lastInventoryAt,omitempty"`
	LastHeartbeatAt  *time.Time `json:"lastHeartbeatAt,omitempty"`
	Status           string     `json:"status"`
	GroupName        string     `json:"groupName"`
	Location         string     `json:"location"`
	IsLaptop         bool       `json:"isLaptop"`
	AgentVersion     string     `json:"agentVersion"`
	EnrolledAt       time.Time  `json:"enrolledAt"`
	InstallDate      *time.Time `json:"installDate,omitempty"`
	Online           bool       `json:"online"`
}

const invSelect = `SELECT c.agent_id,COALESCE(c.hostname,''),COALESCE(a.display_name,''),COALESCE(c.platform,''),COALESCE(c.os_name,''),COALESCE(c.os_edition,''),COALESCE(c.os_version,''),COALESCE(c.arch,''),COALESCE(c.uptime_seconds,0),
 COALESCE(c.cpu_manufacturer,''),COALESCE(c.cpu_brand,''),COALESCE(c.cpu_physical_cores,0),COALESCE(c.cpu_logical_cores,0),COALESCE(c.ram_bytes,0),COALESCE(c.ram_type,''),
 COALESCE(c.gpu_names,''),COALESCE(c.gpu_manufacturers,''),COALESCE(c.storage_total_bytes,0),COALESCE(c.storage_free_bytes,0),COALESCE(c.disk_count,0),COALESCE(c.disk_media,''),
 COALESCE(c.volume_count,0),COALESCE(c.motherboard,''),COALESCE(c.system_model,''),COALESCE(c.system_manufacturer,''),COALESCE(c.primary_ip,''),COALESCE(c.primary_mac,''),COALESCE(c.ips,''),
 COALESCE(c.monitor_count,0),COALESCE(c.software_count,0),COALESCE(c.adapter_count,0),c.snapshot_id,a.last_inventory_at,a.last_heartbeat_at,a.status,COALESCE(g.name,''),COALESCE(a.location,''),a.is_laptop,COALESCE(a.agent_version,''),a.enrolled_at,c.install_date
 FROM inventory_current c JOIN agents a ON a.id=c.agent_id LEFT JOIN agent_groups g ON g.id=a.group_id`

func scanInvRow(sc interface{ Scan(...any) error }) (*InventoryRow, error) {
	var r InventoryRow
	var lastInv, lastHb, install sql.NullString
	var enrolled string
	var laptop int
	err := sc.Scan(&r.AgentID, &r.Hostname, &r.DisplayName, &r.Platform, &r.OSName, &r.OSEdition, &r.OSVersion, &r.Arch, &r.UptimeSeconds,
		&r.CPUManufacturer, &r.CPUBrand, &r.CPUPhysicalCores, &r.CPULogicalCores, &r.RAMBytes, &r.RAMType, &r.GPUNames, &r.GPUManufacturers,
		&r.StorageTotal, &r.StorageFree, &r.DiskCount, &r.DiskMedia, &r.VolumeCount, &r.Motherboard, &r.SystemModel, &r.SystemManufacturer,
		&r.PrimaryIP, &r.PrimaryMAC, &r.IPs, &r.MonitorCount, &r.SoftwareCount, &r.AdapterCount, &r.SnapshotID, &lastInv, &lastHb, &r.Status, &r.GroupName, &r.Location, &laptop, &r.AgentVersion, &enrolled, &install)
	if err != nil {
		return nil, err
	}
	if lastInv.Valid {
		t := parseTS(lastInv.String)
		r.LastInventoryAt = &t
	}
	if lastHb.Valid {
		t := parseTS(lastHb.String)
		r.LastHeartbeatAt = &t
	}
	if install.Valid {
		t := parseTS(install.String)
		r.InstallDate = &t
	}
	r.IsLaptop = laptop != 0
	r.EnrolledAt = parseTS(enrolled)
	return &r, nil
}

// ListInventoryRows returns flattened inventory rows for approved agents.
func (s *Store) ListInventoryRows(ctx context.Context, includeNonApproved bool) ([]*InventoryRow, error) {
	q := invSelect
	if !includeNonApproved {
		q += ` WHERE a.status='approved'`
	}
	q += ` ORDER BY COALESCE(NULLIF(a.display_name,''),c.hostname) COLLATE NOCASE`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*InventoryRow
	for rows.Next() {
		r, err := scanInvRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetInventoryRow(ctx context.Context, agentID string) (*InventoryRow, error) {
	r, err := scanInvRow(s.DB.QueryRowContext(ctx, invSelect+` WHERE c.agent_id=?`, agentID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// --- performance samples -------------------------------------------------

// PerfSample is one raw performance row.
type PerfSample struct {
	AgentID    string    `json:"agentId"`
	At         time.Time `json:"at"`
	CPUPercent float64   `json:"cpuPercent"`
	MemUsed    uint64    `json:"memUsedBytes"`
	MemTotal   uint64    `json:"memTotalBytes"`
	MemPercent float64   `json:"memPercent"`
	SwapUsed   uint64    `json:"swapUsedBytes"`
	DiskReadBps  float64 `json:"diskReadBps"`
	DiskWriteBps float64 `json:"diskWriteBps"`
	NetRxBps     float64 `json:"netRxBps"`
	NetTxBps     float64 `json:"netTxBps"`
	ProcessCount *int    `json:"processCount,omitempty"`
	Load1        *float64 `json:"load1,omitempty"`
	BatteryPercent *float64 `json:"batteryPercent,omitempty"`
	MaxDiskPercent *float64 `json:"maxDiskPercent,omitempty"`
	MaxVolumeUsage *float64 `json:"maxVolumeUsage,omitempty"`
}

func (s *Store) InsertPerfSample(ctx context.Context, p PerfSample, details string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO performance_samples
	(agent_id,collected_at,cpu_percent,mem_used_bytes,mem_total_bytes,mem_percent,swap_used_bytes,disk_read_bps,disk_write_bps,net_rx_bps,net_tx_bps,process_count,load1,battery_percent,max_disk_percent,max_volume_usage,details_json)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.AgentID, tsStr(p.At), p.CPUPercent, p.MemUsed, p.MemTotal, p.MemPercent, p.SwapUsed, p.DiskReadBps, p.DiskWriteBps, p.NetRxBps, p.NetTxBps,
		p.ProcessCount, p.Load1, p.BatteryPercent, p.MaxDiskPercent, p.MaxVolumeUsage, details)
	return err
}

// LatestPerfSamples returns the newest sample per agent (map agentID -> sample).
func (s *Store) LatestPerfSamples(ctx context.Context) (map[string]PerfSample, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.agent_id,p.collected_at,p.cpu_percent,p.mem_used_bytes,p.mem_total_bytes,p.mem_percent,p.swap_used_bytes,p.disk_read_bps,p.disk_write_bps,p.net_rx_bps,p.net_tx_bps,p.process_count,p.load1,p.battery_percent,p.max_disk_percent,p.max_volume_usage
	 FROM performance_samples p JOIN (SELECT agent_id, MAX(id) mid FROM performance_samples GROUP BY agent_id) m ON m.mid=p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PerfSample{}
	for rows.Next() {
		var p PerfSample
		var at string
		var procs sql.NullInt64
		var load, batt, disk, vol sql.NullFloat64
		if err := rows.Scan(&p.AgentID, &at, &p.CPUPercent, &p.MemUsed, &p.MemTotal, &p.MemPercent, &p.SwapUsed, &p.DiskReadBps, &p.DiskWriteBps, &p.NetRxBps, &p.NetTxBps, &procs, &load, &batt, &disk, &vol); err != nil {
			return nil, err
		}
		p.At = parseTS(at)
		if procs.Valid {
			v := int(procs.Int64)
			p.ProcessCount = &v
		}
		if load.Valid {
			v := load.Float64
			p.Load1 = &v
		}
		if batt.Valid {
			v := batt.Float64
			p.BatteryPercent = &v
		}
		if disk.Valid {
			v := disk.Float64
			p.MaxDiskPercent = &v
		}
		if vol.Valid {
			v := vol.Float64
			p.MaxVolumeUsage = &v
		}
		out[p.AgentID] = p
	}
	return out, rows.Err()
}

// PerfSeries returns samples for an agent within a time range, choosing raw
// rows for short ranges and hourly/daily rollups for long ones.
func (s *Store) PerfSeries(ctx context.Context, agentID string, from, to time.Time) ([]PerfSample, string, error) {
	dur := to.Sub(from)
	if dur <= 12*time.Hour {
		rows, err := s.DB.QueryContext(ctx, `SELECT collected_at,cpu_percent,mem_used_bytes,mem_total_bytes,mem_percent,swap_used_bytes,disk_read_bps,disk_write_bps,net_rx_bps,net_tx_bps,process_count,load1,battery_percent,max_disk_percent,max_volume_usage
		 FROM performance_samples WHERE agent_id=? AND collected_at>=? AND collected_at<=? ORDER BY collected_at`, agentID, tsStr(from), tsStr(to))
		if err != nil {
			return nil, "", err
		}
		defer rows.Close()
		var out []PerfSample
		for rows.Next() {
			var p PerfSample
			var at string
			var procs sql.NullInt64
			var load, batt, disk, vol sql.NullFloat64
			if err := rows.Scan(&at, &p.CPUPercent, &p.MemUsed, &p.MemTotal, &p.MemPercent, &p.SwapUsed, &p.DiskReadBps, &p.DiskWriteBps, &p.NetRxBps, &p.NetTxBps, &procs, &load, &batt, &disk, &vol); err != nil {
				return nil, "", err
			}
			p.AgentID = agentID
			p.At = parseTS(at)
			if procs.Valid {
				v := int(procs.Int64)
				p.ProcessCount = &v
			}
			if load.Valid {
				v := load.Float64
				p.Load1 = &v
			}
			if batt.Valid {
				v := batt.Float64
				p.BatteryPercent = &v
			}
			if disk.Valid {
				v := disk.Float64
				p.MaxDiskPercent = &v
			}
			if vol.Valid {
				v := vol.Float64
				p.MaxVolumeUsage = &v
			}
			out = append(out, p)
		}
		return out, "raw", rows.Err()
	}
	bucket := "hour"
	if dur > 3*24*time.Hour {
		bucket = "day"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT bucket_start,samples,cpu_avg,cpu_max,mem_avg,mem_max,disk_read_avg,disk_write_avg,net_rx_avg,net_tx_avg,net_rx_max,net_tx_max,disk_util_avg
	 FROM performance_rollups WHERE agent_id=? AND bucket=? AND bucket_start>=? AND bucket_start<=? ORDER BY bucket_start`, agentID, bucket, tsStr(from), tsStr(to))
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []PerfSample
	for rows.Next() {
		var start string
		var samples int64
		var cpuAvg, cpuMax, memAvg, memMax, drAvg, dwAvg, rxAvg, txAvg, rxMax, txMax, duAvg sql.NullFloat64
		if err := rows.Scan(&start, &samples, &cpuAvg, &cpuMax, &memAvg, &memMax, &drAvg, &dwAvg, &rxAvg, &txAvg, &rxMax, &txMax, &duAvg); err != nil {
			return nil, "", err
		}
		p := PerfSample{AgentID: agentID, At: parseTS(start), CPUPercent: cpuAvg.Float64, MemPercent: memAvg.Float64, DiskReadBps: drAvg.Float64, DiskWriteBps: dwAvg.Float64, NetRxBps: rxAvg.Float64, NetTxBps: txAvg.Float64}
		if duAvg.Valid {
			v := duAvg.Float64
			p.MaxDiskPercent = &v
		}
		out = append(out, p)
	}
	return out, bucket, rows.Err()
}

// RollupOlderThan aggregates raw samples older than cutoff into hourly buckets
// and daily buckets for older data.
func (s *Store) RollupOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO performance_rollups (agent_id,bucket,bucket_start,samples,cpu_avg,cpu_max,mem_avg,mem_max,disk_read_avg,disk_write_avg,net_rx_avg,net_tx_avg,net_rx_max,net_tx_max,disk_util_avg)
	 SELECT agent_id,'hour',substr(collected_at,1,13)||':00:00.000Z',COUNT(*),AVG(cpu_percent),MAX(cpu_percent),AVG(mem_percent),MAX(mem_percent),AVG(disk_read_bps),AVG(disk_write_bps),AVG(net_rx_bps),AVG(net_tx_bps),MAX(net_rx_bps),MAX(net_tx_bps),AVG(max_disk_percent)
	 FROM performance_samples WHERE collected_at < ?
	 GROUP BY agent_id, substr(collected_at,1,13)
	 ON CONFLICT(agent_id,bucket,bucket_start) DO UPDATE SET samples=excluded.samples, cpu_avg=excluded.cpu_avg, cpu_max=excluded.cpu_max, mem_avg=excluded.mem_avg,
	  mem_max=excluded.mem_max, disk_read_avg=excluded.disk_read_avg, disk_write_avg=excluded.disk_write_avg, net_rx_avg=excluded.net_rx_avg, net_tx_avg=excluded.net_tx_avg,
	  net_rx_max=excluded.net_rx_max, net_tx_max=excluded.net_tx_max, disk_util_avg=excluded.disk_util_avg`, tsStr(cutoff))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `DELETE FROM performance_samples WHERE collected_at < ?`, tsStr(cutoff)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// DeletePerfOlderThan removes raw and rollup data beyond the retention window.
func (s *Store) DeletePerfOlderThan(ctx context.Context, rawCutoff, rollupCutoff time.Time) (int64, int64, error) {
	r1, err := s.DB.ExecContext(ctx, `DELETE FROM performance_samples WHERE collected_at < ?`, tsStr(rawCutoff))
	if err != nil {
		return 0, 0, err
	}
	r2, err := s.DB.ExecContext(ctx, `DELETE FROM performance_rollups WHERE bucket_start < ?`, tsStr(rollupCutoff))
	if err != nil {
		return 0, 0, err
	}
	n1, _ := r1.RowsAffected()
	n2, _ := r2.RowsAffected()
	return n1, n2, nil
}

// --- hardware changes ----------------------------------------------------

type HardwareChange struct {
	ID         int64     `json:"id"`
	AgentID    string    `json:"agentId"`
	Hostname   string    `json:"hostname,omitempty"`
	DetectedAt time.Time `json:"detectedAt"`
	Category   string    `json:"category"`
	Severity   string    `json:"severity"`
	Summary    string    `json:"summary"`
	Details    string    `json:"details,omitempty"`
}

func (s *Store) AddHardwareChange(ctx context.Context, agentID, category, severity, summary, details string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO hardware_changes (agent_id,detected_at,category,severity,summary,details_json) VALUES (?,?,?,?,?,?)`, agentID, nowStr(), category, severity, summary, details)
	return err
}

func (s *Store) ListHardwareChanges(ctx context.Context, agentID string, limit int) ([]HardwareChange, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT h.id,h.agent_id,COALESCE(a.hostname,''),h.detected_at,h.category,h.severity,h.summary,COALESCE(h.details_json,'') FROM hardware_changes h LEFT JOIN agents a ON a.id=h.agent_id`
	var args []any
	if agentID != "" {
		q += ` WHERE h.agent_id=?`
		args = append(args, agentID)
	}
	q += ` ORDER BY h.detected_at DESC, h.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HardwareChange
	for rows.Next() {
		var c HardwareChange
		var at string
		if err := rows.Scan(&c.ID, &c.AgentID, &c.Hostname, &at, &c.Category, &c.Severity, &c.Summary, &c.Details); err != nil {
			return nil, err
		}
		c.DetectedAt = parseTS(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- backup --------------------------------------------------------------

// Backup writes a consistent copy of the database to dest using VACUUM INTO.
func (s *Store) Backup(ctx context.Context, dest string) error {
	_, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, dest)
	return err
}

func (s *Store) Stats(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	for _, t := range []string{"agents", "inventory_snapshots", "performance_samples", "performance_rollups", "hardware_changes", "alerts", "agent_events", "audit_events"} {
		var c int64
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t).Scan(&c); err != nil {
			return nil, err
		}
		out[t] = c
	}
	return out, nil
}
