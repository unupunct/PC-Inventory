package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/inventory"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
)

// Dashboard aggregates every statistic from real stored records.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := s.Store.ListAgents(ctx, "")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.enrichAgents(agents)
	rows, err := s.Store.ListInventoryRows(ctx, false)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	latest, _ := s.Store.LatestPerfSamples(ctx)
	changes, _ := s.Store.ListHardwareChanges(ctx, "", 15)
	alerts, _ := s.Store.ListAlerts(ctx, "active", "", "", 15)
	alertCounts, _ := s.Store.AlertCounts(ctx)
	events, _ := s.Store.ListAgentEvents(ctx, "", 25)
	reqStats, _ := s.Store.RequestStatsSummary(ctx)

	type stats struct {
		TotalComputers    int    `json:"totalComputers"`
		Online            int    `json:"online"`
		Offline           int    `json:"offline"`
		Pending           int    `json:"pending"`
		Disabled          int    `json:"disabled"`
		Revoked           int    `json:"revoked"`
		Windows           int    `json:"windows"`
		MacOS             int    `json:"macos"`
		Linux             int    `json:"linux"`
		TotalRAMBytes     uint64 `json:"totalRamBytes"`
		TotalCores        int    `json:"totalCores"`
		TotalStorageBytes uint64 `json:"totalStorageBytes"`
		FreeStorageBytes  uint64 `json:"freeStorageBytes"`
		NeedsAttention    int    `json:"needsAttention"`
		Laptops           int    `json:"laptops"`
		Desktops          int    `json:"desktops"`
	}
	var st stats
	approved := 0
	for _, a := range agents {
		switch a.Status {
		case store.StatusApproved:
			st.TotalComputers++
			approved++
			if a.Online {
				st.Online++
			} else {
				st.Offline++
			}
			switch a.Platform {
			case "windows":
				st.Windows++
			case "darwin":
				st.MacOS++
			case "linux":
				st.Linux++
			}
			if a.IsLaptop {
				st.Laptops++
			} else {
				st.Desktops++
			}
		case store.StatusPending:
			st.Pending++
		case store.StatusDisabled:
			st.Disabled++
		case store.StatusRevoked:
			st.Revoked++
		}
	}
	attention := map[string][]string{}
	for _, row := range rows {
		st.TotalRAMBytes += row.RAMBytes
		st.TotalCores += row.CPULogicalCores
		st.TotalStorageBytes += row.StorageTotal
		st.FreeStorageBytes += row.StorageFree
		if p, ok := latest[row.AgentID]; ok {
			if p.CPUPercent >= 85 {
				attention["highCpu"] = append(attention["highCpu"], row.AgentID)
			}
			if p.MemPercent >= 90 {
				attention["highRam"] = append(attention["highRam"], row.AgentID)
			}
		}
		if row.StorageTotal > 0 && float64(row.StorageFree)/float64(row.StorageTotal) < 0.10 {
			attention["lowDisk"] = append(attention["lowDisk"], row.AgentID)
		}
	}
	for _, a := range agents {
		if a.Status == store.StatusApproved && !a.Online {
			attention["offline"] = append(attention["offline"], a.ID)
		}
		if a.LastError != "" {
			attention["errors"] = append(attention["errors"], a.ID)
		}
	}
	seen := map[string]bool{}
	for _, ids := range attention {
		for _, id := range ids {
			seen[id] = true
		}
	}
	st.NeedsAttention = len(seen)

	// Chart data
	osDist := map[string]int{}
	cpuMfr := map[string]int{}
	gpuMfr := map[string]int{}
	ramDist := map[string]int{}
	mediaDist := map[string]int{}
	storageDist := map[string]int{}
	ramTypes := map[string]int{}
	cpuBrands := map[string]int{}
	for _, row := range rows {
		osKey := strings.TrimSpace(row.OSName)
		if osKey == "" {
			osKey = row.Platform
		}
		osDist[osKey]++
		cpuMfr[orUnknown(row.CPUManufacturer)]++
		for _, g := range splitSemi(row.GPUManufacturers) {
			gpuMfr[orUnknown(g)]++
		}
		ramDist[bucketRAM(row.RAMBytes)]++
		if row.RAMType != "" {
			ramTypes[row.RAMType]++
		}
		for _, m := range splitSemi(row.DiskMedia) {
			mediaDist[m]++
		}
		storageDist[bucketStorage(row.StorageTotal)]++
		cpuBrands[row.CPUBrand]++
	}
	onlineDist := map[string]int{"Online": st.Online, "Offline": st.Offline}

	// Hardware age from OS install date (proxy when purchase date unknown)
	ageDist := map[string]int{}
	for _, row := range rows {
		if row.InstallDate == nil || row.InstallDate.IsZero() {
			continue
		}
		age := time.Since(*row.InstallDate).Hours() / 24 / 365
		switch {
		case age < 1:
			ageDist["< 1 year"]++
		case age < 2:
			ageDist["1-2 years"]++
		case age < 3:
			ageDist["2-3 years"]++
		case age < 5:
			ageDist["3-5 years"]++
		default:
			ageDist["5+ years"]++
		}
	}

	// Live overview lists
	type liveItem struct {
		AgentID  string    `json:"agentId"`
		Hostname string    `json:"hostname"`
		Platform string    `json:"platform"`
		At       time.Time `json:"at"`
		Value    float64   `json:"value,omitempty"`
		Message  string    `json:"message,omitempty"`
	}
	var recentlyConnected, recentlyDisconnected []liveItem
	var highCPU, highRAM, lowDisk, commErrors []liveItem
	now := time.Now().UTC()
	for _, a := range agents {
		if a.Status != store.StatusApproved {
			continue
		}
		name := a.DisplayName
		if name == "" {
			name = a.Hostname
		}
		if a.LastHeartbeatAt != nil {
			it := liveItem{AgentID: a.ID, Hostname: name, Platform: a.Platform, At: *a.LastHeartbeatAt}
			if now.Sub(*a.LastHeartbeatAt) <= 5*time.Minute {
				recentlyConnected = append(recentlyConnected, it)
			} else {
				recentlyDisconnected = append(recentlyDisconnected, it)
			}
		}
		if a.LastError != "" {
			commErrors = append(commErrors, liveItem{AgentID: a.ID, Hostname: name, Platform: a.Platform, Message: a.LastError, At: a.UpdatedAt})
		}
		if p, ok := latest[a.ID]; ok {
			if p.CPUPercent >= 60 {
				highCPU = append(highCPU, liveItem{AgentID: a.ID, Hostname: name, Platform: a.Platform, At: p.At, Value: p.CPUPercent})
			}
			if p.MemPercent >= 80 {
				highRAM = append(highRAM, liveItem{AgentID: a.ID, Hostname: name, Platform: a.Platform, At: p.At, Value: p.MemPercent})
			}
		}
	}
	for _, row := range rows {
		if row.StorageTotal > 0 {
			free := float64(row.StorageFree) / float64(row.StorageTotal) * 100
			if free < 20 {
				lowDisk = append(lowDisk, liveItem{AgentID: row.AgentID, Hostname: row.Hostname, Platform: row.Platform, Value: 100 - free})
			}
		}
	}
	sort.Slice(recentlyConnected, func(i, j int) bool { return recentlyConnected[i].At.After(recentlyConnected[j].At) })
	sort.Slice(recentlyDisconnected, func(i, j int) bool { return recentlyDisconnected[i].At.After(recentlyDisconnected[j].At) })
	sort.Slice(highCPU, func(i, j int) bool { return highCPU[i].Value > highCPU[j].Value })
	sort.Slice(highRAM, func(i, j int) bool { return highRAM[i].Value > highRAM[j].Value })
	sort.Slice(lowDisk, func(i, j int) bool { return lowDisk[i].Value > lowDisk[j].Value })
	trim := func(v []liveItem, n int) []liveItem {
		if len(v) > n {
			return v[:n]
		}
		return v
	}

	// Perf summary across fleet (last sample per agent)
	var cpuSum, ramSum float64
	var cpuN, ramN int
	for _, p := range latest {
		cpuSum += p.CPUPercent
		ramSum += p.MemPercent
		cpuN++
		ramN++
	}
	avgCPU, avgRAM := 0.0, 0.0
	if cpuN > 0 {
		avgCPU = cpuSum / float64(cpuN)
	}
	if ramN > 0 {
		avgRAM = ramSum / float64(ramN)
	}

	writeJSON(w, 200, map[string]any{
		"stats":                st,
		"osDistribution":       toPairs(osDist),
		"cpuManufacturers":     toPairs(cpuMfr),
		"gpuManufacturers":     toPairs(gpuMfr),
		"ramDistribution":      orderedPairs(ramDist, []string{"< 8 GB", "8-16 GB", "16-32 GB", "32-64 GB", "64+ GB"}),
		"ramTypes":             toPairs(ramTypes),
		"mediaDistribution":    toPairs(mediaDist),
		"storageDistribution":  orderedPairs(storageDist, []string{"< 256 GB", "256-512 GB", "512 GB-1 TB", "1-2 TB", "2-4 TB", "4+ TB"}),
		"onlineDistribution":   toPairs(onlineDist),
		"ageDistribution":      toPairs(ageDist),
		"recentlyConnected":    trim(recentlyConnected, 8),
		"recentlyDisconnected": trim(recentlyDisconnected, 8),
		"highCpu":              trim(highCPU, 8),
		"highRam":              trim(highRAM, 8),
		"lowDisk":              trim(lowDisk, 8),
		"commErrors":           trim(commErrors, 8),
		"latestChanges":        changes,
		"activeAlerts":         alerts,
		"alertCounts":          alertCounts,
		"recentEvents":         events,
		"fleet": map[string]any{
			"avgCpuPercent":   avgCPU,
			"avgRamPercent":   avgRAM,
			"reportingAgents": cpuN,
		},
		"requestStats":        reqStats,
		"generatedAt":         now,
		"offlineThresholdSec": s.Cfg.OfflineThresholdSec,
	})
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "Unknown"
	}
	return s
}

func splitSemi(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func bucketRAM(b uint64) string {
	gb := float64(b) / (1024 * 1024 * 1024)
	switch {
	case gb < 8:
		return "< 8 GB"
	case gb < 16:
		return "8-16 GB"
	case gb < 32:
		return "16-32 GB"
	case gb < 64:
		return "32-64 GB"
	default:
		return "64+ GB"
	}
}

func bucketStorage(b uint64) string {
	gb := float64(b) / (1000 * 1000 * 1000)
	switch {
	case gb < 256:
		return "< 256 GB"
	case gb < 512:
		return "256-512 GB"
	case gb < 1024:
		return "512 GB-1 TB"
	case gb < 2048:
		return "1-2 TB"
	case gb < 4096:
		return "2-4 TB"
	default:
		return "4+ TB"
	}
}

type pair struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func toPairs(m map[string]int) []pair {
	out := make([]pair, 0, len(m))
	for k, v := range m {
		out = append(out, pair{Name: k, Value: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value == out[j].Value {
			return out[i].Name < out[j].Name
		}
		return out[i].Value > out[j].Value
	})
	return out
}

func orderedPairs(m map[string]int, order []string) []pair {
	out := make([]pair, 0, len(order))
	for _, k := range order {
		if v, ok := m[k]; ok && v > 0 {
			out = append(out, pair{Name: k, Value: v})
		}
	}
	for k, v := range m {
		found := false
		for _, o := range order {
			if o == k {
				found = true
				break
			}
		}
		if !found && v > 0 {
			out = append(out, pair{Name: k, Value: v})
		}
	}
	return out
}

// BackupNow writes a timestamped backup into the backup directory.
func (s *Server) BackupNow(ctx context.Context) (string, error) {
	dir := s.Cfg.BackupDir
	if v, ok := s.Store.GetSetting(ctx, "backup_dir"); ok && v != "" {
		dir = v
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	name := fmt.Sprintf("pcim-backup-%s.db", time.Now().Format("20060102-150405"))
	dest := filepath.Join(dir, name)
	if err := s.Store.Backup(ctx, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// settingsResponse is the admin-editable configuration.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	settings, err := s.Store.AllSettings(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	agentCfg := s.latestConfig(ctx, nil)
	rules, _ := s.Store.ListAlertRules(ctx)
	safe := map[string]string{}
	for k, v := range settings {
		if strings.HasPrefix(k, "agent_commands:") {
			continue
		}
		safe[k] = v
	}
	writeJSON(w, 200, map[string]any{
		"settings":    safe,
		"agentConfig": agentCfg,
		"alertRules":  rules,
		"serverInfo": map[string]any{
			"listenHost":          s.Cfg.ListenHost,
			"port":                s.Cfg.Port,
			"tls":                 s.Cfg.TLSEnabled(),
			"publicUrl":           s.Cfg.AdvertisedURL(),
			"dataDir":             s.Cfg.DataDir,
			"dbPath":              s.Cfg.DBPath,
			"backupDir":           s.Cfg.BackupDir,
			"offlineThresholdSec": s.Cfg.OfflineThresholdSec,
			"retentionDays":       s.Cfg.RetentionDays,
			"rawSampleHours":      s.Cfg.RawSampleHours,
			"allowedCidrs":        s.Cfg.AllowedCIDRs,
			"emailConfigured":     s.Email.Configured(),
			"telegramConfigured":  s.Telegram.Configured(),
			"version":             "1.0.0",
		},
	})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Settings    map[string]string `json:"settings"`
		AgentConfig *struct {
			HeartbeatIntervalSec  *int  `json:"heartbeatIntervalSec"`
			MetricsIntervalSec    *int  `json:"metricsIntervalSec"`
			InventoryIntervalSec  *int  `json:"inventoryIntervalSec"`
			CollectSoftware       *bool `json:"collectSoftware"`
			CollectMonitors       *bool `json:"collectMonitors"`
			CollectUSB            *bool `json:"collectUsb"`
			CollectPrinters       *bool `json:"collectPrinters"`
			CollectProcesses      *bool `json:"collectProcesses"`
			CollectPerCoreCPU     *bool `json:"collectPerCoreCpu"`
			CollectTemperatures   *bool `json:"collectTemperatures"`
			CollectInstalledApps  *bool `json:"collectInstalledApps"`
			CollectWindowsUpdates *bool `json:"collectWindowsUpdates"`
			CollectLoggedInUser   *bool `json:"collectLoggedInUser"`
		} `json:"agentConfig"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}
	ctx := r.Context()
	allowed := map[string]bool{
		"server_name": true, "enrollment_enabled": true, "auto_approve_agents": true,
		"backup_dir": true, "theme": true, "dashboard_refresh_sec": true,
		"low_disk_threshold": true, "default_agent_config": true, "notes": true,
		"metrics_token": true, "report_schedule": true, "report_recipients": true,
		"agent_auto_update": true, "report_schedule_enabled": true, "report_kinds": true,
		"report_format": true,
	}
	for k, v := range body.Settings {
		if !allowed[k] {
			continue
		}
		if err := s.Store.SetSetting(ctx, k, v); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	if body.AgentConfig != nil {
		cfg := s.latestConfig(ctx, nil)
		a := body.AgentConfig
		if a.HeartbeatIntervalSec != nil && *a.HeartbeatIntervalSec >= 10 && *a.HeartbeatIntervalSec <= 3600 {
			cfg.HeartbeatIntervalSec = *a.HeartbeatIntervalSec
		}
		if a.MetricsIntervalSec != nil && *a.MetricsIntervalSec >= 5 && *a.MetricsIntervalSec <= 3600 {
			cfg.MetricsIntervalSec = *a.MetricsIntervalSec
		}
		if a.InventoryIntervalSec != nil && *a.InventoryIntervalSec >= 300 {
			cfg.InventoryIntervalSec = *a.InventoryIntervalSec
		}
		if a.CollectSoftware != nil {
			cfg.CollectSoftware = *a.CollectSoftware
		}
		if a.CollectMonitors != nil {
			cfg.CollectMonitors = *a.CollectMonitors
		}
		if a.CollectUSB != nil {
			cfg.CollectUSB = *a.CollectUSB
		}
		if a.CollectPrinters != nil {
			cfg.CollectPrinters = *a.CollectPrinters
		}
		if a.CollectProcesses != nil {
			cfg.CollectProcesses = *a.CollectProcesses
		}
		if a.CollectPerCoreCPU != nil {
			cfg.CollectPerCoreCPU = *a.CollectPerCoreCPU
		}
		if a.CollectTemperatures != nil {
			cfg.CollectTemperatures = *a.CollectTemperatures
		}
		if a.CollectInstalledApps != nil {
			cfg.CollectInstalledApps = *a.CollectInstalledApps
		}
		if a.CollectWindowsUpdates != nil {
			cfg.CollectWindowsUpdates = *a.CollectWindowsUpdates
		}
		if a.CollectLoggedInUser != nil {
			cfg.CollectLoggedInUser = *a.CollectLoggedInUser
		}
		cfg.ConfigVersion++
		if err := s.Store.SetSetting(ctx, "default_agent_config", mustJSON(cfg)); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		s.Hub.Broadcast("settings", nil)
	}
	s.audit(r, "settings.update", "", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func mustJSON(v any) string {
	b, _ := jsonMarshal(v)
	return string(b)
}

// --- reports -------------------------------------------------------------

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "json"
	}
	ids := r.URL.Query().Get("agents")
	idFilter := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		if id = strings.TrimSpace(id); id != "" {
			idFilter[id] = true
		}
	}
	ctx := r.Context()
	rows, err := s.Store.ListInventoryRows(ctx, false)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(idFilter) > 0 {
		var filtered []*store.InventoryRow
		for _, row := range rows {
			if idFilter[row.AgentID] {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	s.enrichInventory(rows, nil)
	agents, _ := s.Store.ListAgents(ctx, "")
	agentByID := map[string]*store.Agent{}
	for _, a := range agents {
		agentByID[a.ID] = a
	}

	switch kind {
	case "inventory":
		rep := buildInventoryReport(ctx, s, rows)
		s.audit(r, "report.inventory", fmt.Sprint(len(rows)), format)
		writeReport(w, r, format, "hardware-inventory", rep)
	case "network":
		rep := buildNetworkReport(rows)
		s.audit(r, "report.network", fmt.Sprint(len(rows)), format)
		writeReport(w, r, format, "network-summary", rep)
	case "storage":
		rep := buildStorageReport(rows)
		s.audit(r, "report.storage", fmt.Sprint(len(rows)), format)
		writeReport(w, r, format, "storage", rep)
	case "distribution":
		rep := buildDistributionReport(rows)
		s.audit(r, "report.distribution", "", format)
		writeReport(w, r, format, "hardware-distribution", rep)
	case "changes":
		changes, _ := s.Store.ListHardwareChanges(ctx, "", 500)
		var filtered []store.HardwareChange
		for _, c := range changes {
			if len(idFilter) == 0 || idFilter[c.AgentID] {
				filtered = append(filtered, c)
			}
		}
		s.audit(r, "report.changes", fmt.Sprint(len(filtered)), format)
		writeReport(w, r, format, "hardware-changes", changesReport{GeneratedAt: time.Now().UTC(), Changes: filtered})
	case "performance":
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
			from = now.Add(-24 * time.Hour)
			rangeKey = "24h"
		}
		rep := buildPerformanceReport(ctx, s, rows, from, now, rangeKey)
		s.audit(r, "report.performance", rangeKey, format)
		writeReport(w, r, format, "performance", rep)
	default:
		writeErr(w, http.StatusNotFound, "unknown report type")
	}
}

// writeReport renders JSON, CSV or PDF.
func writeReport(w http.ResponseWriter, r *http.Request, format, name string, rep report) {
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+"-"+time.Now().Format("20060102-1504")+".csv"))
		_ = rep.WriteCSV(w)
	case "pdf":
		data, err := rep.PDF()
		if err != nil {
			writeErr(w, 500, "pdf generation failed: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name+"-"+time.Now().Format("20060102-1504")+".pdf"))
		_, _ = w.Write(data)
	case "html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(rep.HTML()))
	default:
		writeJSON(w, 200, rep)
	}
}

var _ = inventory.Hash
