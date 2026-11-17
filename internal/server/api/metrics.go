package api

// Prometheus exposition endpoint. Every value here is read from the same
// tables the dashboard reads: no metric is synthesised, and a metric with no
// sample behind it is simply not emitted (never reported as zero).

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var b strings.Builder

	line := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}

	// --- server self metrics ---
	line("# HELP pcim_server_info Build information for this server.")
	line("# TYPE pcim_server_info gauge")
	line(`pcim_server_info{version=%q,protocol=%q} 1`, version.Version, fmt.Sprint(version.Protocol))

	line("# HELP pcim_server_uptime_seconds Seconds since this server process started.")
	line("# TYPE pcim_server_uptime_seconds gauge")
	line("pcim_server_uptime_seconds %s", fmtFloat(time.Since(s.StartedAt).Seconds()))

	line("# HELP pcim_agents Number of agents by status.")
	line("# TYPE pcim_agents gauge")
	counts, err := s.Store.CountAgentsByStatus(ctx)
	if err == nil {
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			line(`pcim_agents{status=%q} %d`, k, counts[k])
		}
	}

	// --- per-agent metrics ---
	agents, err := s.Store.ListAgents(ctx, "")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	labels := func(a *store.Agent) string {
		return fmt.Sprintf(`agent_id=%q,hostname=%q,platform=%q`, a.ID, a.Hostname, a.Platform)
	}

	line("# HELP pcim_agent_online 1 when the agent's last heartbeat is inside the offline threshold.")
	line("# TYPE pcim_agent_online gauge")
	for _, a := range agents {
		if a.Status != "approved" {
			continue
		}
		v := 0
		if a.Online {
			v = 1
		}
		line("pcim_agent_online{%s} %d", labels(a), v)
	}

	line("# HELP pcim_agent_last_heartbeat_timestamp_seconds Unix time of the last heartbeat.")
	line("# TYPE pcim_agent_last_heartbeat_timestamp_seconds gauge")
	for _, a := range agents {
		if a.LastHeartbeatAt == nil {
			continue
		}
		line("pcim_agent_last_heartbeat_timestamp_seconds{%s} %d", labels(a), a.LastHeartbeatAt.Unix())
	}

	line("# HELP pcim_agent_last_metrics_timestamp_seconds Unix time of the last metrics report.")
	line("# TYPE pcim_agent_last_metrics_timestamp_seconds gauge")
	for _, a := range agents {
		if a.LastMetricsAt == nil {
			continue
		}
		line("pcim_agent_last_metrics_timestamp_seconds{%s} %d", labels(a), a.LastMetricsAt.Unix())
	}

	line("# HELP pcim_agent_last_inventory_timestamp_seconds Unix time of the last inventory report.")
	line("# TYPE pcim_agent_last_inventory_timestamp_seconds gauge")
	for _, a := range agents {
		if a.LastInventoryAt == nil {
			continue
		}
		line("pcim_agent_last_inventory_timestamp_seconds{%s} %d", labels(a), a.LastInventoryAt.Unix())
	}

	// Latest performance sample per agent, from the samples table.
	type sample struct {
		cpu, memPct, memUsed, memTotal float64
		diskRead, diskWrite           float64
		netRx, netTx                  float64
	}
	latest := map[string]sample{}
	rows, err := s.Store.DB.QueryContext(ctx, `
		SELECT agent_id,cpu_percent,mem_percent,mem_used_bytes,mem_total_bytes,
		       disk_read_bps,disk_write_bps,net_rx_bps,net_tx_bps
		FROM performance_samples p
		WHERE id IN (SELECT MAX(id) FROM performance_samples GROUP BY agent_id)`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var sm sample
			if err := rows.Scan(&id, &sm.cpu, &sm.memPct, &sm.memUsed, &sm.memTotal, &sm.diskRead, &sm.diskWrite, &sm.netRx, &sm.netTx); err == nil {
				latest[id] = sm
			}
		}
	}

	if len(latest) > 0 {
		line("# HELP pcim_agent_cpu_percent Latest CPU usage reported by the agent.")
		line("# TYPE pcim_agent_cpu_percent gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_cpu_percent{%s} %s", labels(a), fmtFloat(sm.cpu))
			}
		}
		line("# HELP pcim_agent_memory_used_bytes Latest memory in use.")
		line("# TYPE pcim_agent_memory_used_bytes gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_memory_used_bytes{%s} %s", labels(a), fmtFloat(sm.memUsed))
			}
		}
		line("# HELP pcim_agent_memory_total_bytes Total memory reported by the agent.")
		line("# TYPE pcim_agent_memory_total_bytes gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_memory_total_bytes{%s} %s", labels(a), fmtFloat(sm.memTotal))
			}
		}
		line("# HELP pcim_agent_disk_read_bytes_per_second Latest disk read rate.")
		line("# TYPE pcim_agent_disk_read_bytes_per_second gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_disk_read_bytes_per_second{%s} %s", labels(a), fmtFloat(sm.diskRead))
			}
		}
		line("# HELP pcim_agent_disk_write_bytes_per_second Latest disk write rate.")
		line("# TYPE pcim_agent_disk_write_bytes_per_second gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_disk_write_bytes_per_second{%s} %s", labels(a), fmtFloat(sm.diskWrite))
			}
		}
		line("# HELP pcim_agent_network_receive_bytes_per_second Latest receive rate.")
		line("# TYPE pcim_agent_network_receive_bytes_per_second gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_network_receive_bytes_per_second{%s} %s", labels(a), fmtFloat(sm.netRx))
			}
		}
		line("# HELP pcim_agent_network_transmit_bytes_per_second Latest transmit rate.")
		line("# TYPE pcim_agent_network_transmit_bytes_per_second gauge")
		for _, a := range agents {
			if sm, ok := latest[a.ID]; ok {
				line("pcim_agent_network_transmit_bytes_per_second{%s} %s", labels(a), fmtFloat(sm.netTx))
			}
		}
	}

	// --- inventory-derived metrics ---
	line("# HELP pcim_agent_memory_installed_bytes Memory installed, from the latest inventory.")
	line("# TYPE pcim_agent_memory_installed_bytes gauge")
	line("# HELP pcim_agent_storage_total_bytes Total storage, from the latest inventory.")
	line("# TYPE pcim_agent_storage_total_bytes gauge")
	line("# HELP pcim_agent_storage_free_bytes Free storage, from the latest inventory.")
	line("# TYPE pcim_agent_storage_free_bytes gauge")
	line("# HELP pcim_agent_software_count Installed applications counted in the latest inventory.")
	line("# TYPE pcim_agent_software_count gauge")
	invRows, err := s.Store.DB.QueryContext(ctx, `
		SELECT a.id,a.hostname,a.platform,c.ram_bytes,c.storage_total_bytes,c.storage_free_bytes,c.software_count
		FROM agents a JOIN inventory_current c ON c.agent_id=a.id`)
	if err == nil {
		defer invRows.Close()
		for invRows.Next() {
			var id, host, plat string
			var ram, stTotal, stFree, sw int64
			if err := invRows.Scan(&id, &host, &plat, &ram, &stTotal, &stFree, &sw); err != nil {
				continue
			}
			l := fmt.Sprintf(`agent_id=%q,hostname=%q,platform=%q`, id, host, plat)
			line("pcim_agent_memory_installed_bytes{%s} %d", l, ram)
			line("pcim_agent_storage_total_bytes{%s} %d", l, stTotal)
			line("pcim_agent_storage_free_bytes{%s} %d", l, stFree)
			line("pcim_agent_software_count{%s} %d", l, sw)
		}
	}

	// --- alerts ---
	line("# HELP pcim_alerts Number of alerts by status and severity.")
	line("# TYPE pcim_alerts gauge")
	alertRows, err := s.Store.DB.QueryContext(ctx, `SELECT status,severity,COUNT(*) FROM alerts GROUP BY status,severity`)
	if err == nil {
		defer alertRows.Close()
		for alertRows.Next() {
			var st, sev string
			var n int64
			if err := alertRows.Scan(&st, &sev, &n); err == nil {
				line(`pcim_alerts{status=%q,severity=%q} %d`, st, sev, n)
			}
		}
	}

	// --- hardware changes ---
	line("# HELP pcim_hardware_changes_total Hardware changes detected since the server started recording them.")
	line("# TYPE pcim_hardware_changes_total counter")
	var changeCount int64
	if err := s.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM hardware_changes`).Scan(&changeCount); err == nil {
		line("pcim_hardware_changes_total %d", changeCount)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

func fmtFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}
