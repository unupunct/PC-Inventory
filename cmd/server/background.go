package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/alerts"
	"github.com/pcim/pc-inventory-manager/internal/server/api"
)

func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

// runBackground starts periodic maintenance and evaluation jobs.
func runBackground(ctx context.Context, srv *api.Server, engine *alerts.Engine, log *slog.Logger) {
	// Alert evaluation every 15 seconds (offline detection, thresholds).
	go every(ctx, 15*time.Second, func() {
		if err := engine.Evaluate(ctx); err != nil {
			log.Warn("alert evaluation failed", "err", err)
		}
	})
	// Rollups + retention every 10 minutes.
	go every(ctx, 10*time.Minute, func() {
		st := srv.Store
		rawCutoff := time.Now().UTC().Add(-time.Duration(srv.Cfg.RawSampleHours) * time.Hour)
		n, err := st.RollupOlderThan(ctx, rawCutoff)
		if err != nil {
			log.Warn("rollup failed", "err", err)
			return
		}
		if n > 0 {
			log.Debug("rolled up samples", "rows", n)
		}
		retention := time.Duration(srv.Cfg.RetentionDays) * 24 * time.Hour
		rawDel, rollDel, err := st.DeletePerfOlderThan(ctx, rawCutoff.Add(-retention), time.Now().UTC().Add(-retention))
		if err != nil {
			log.Warn("retention cleanup failed", "err", err)
		}
		_ = st.PurgeExpiredSessions(ctx)
		// Purge agent logs older than 14 days and request stats older than 2 days.
		_, _ = st.DB.ExecContext(ctx, `DELETE FROM agent_logs WHERE at < ?`, time.Now().UTC().Add(-14*24*time.Hour).Format("2006-01-02T15:04:05.000Z07:00"))
		_, _ = st.DB.ExecContext(ctx, `DELETE FROM request_stats WHERE at < ?`, time.Now().UTC().Add(-48*time.Hour).Format("2006-01-02T15:04:05.000Z07:00"))
		if rawDel+rollDel > 0 {
			log.Debug("retention cleanup", "raw", rawDel, "rollups", rollDel)
		}
	})
	// Automatic daily backup at startup + every 24h.
	go func() {
		time.Sleep(30 * time.Second)
		backupIfDue(ctx, srv, log)
		every(ctx, 6*time.Hour, func() { backupIfDue(ctx, srv, log) })
	}()
	// Link discovered devices to agents every minute.
	go every(ctx, time.Minute, func() {
		_ = srv.Store.LinkDiscoveredToAgents(ctx)
	})
}

func backupIfDue(ctx context.Context, srv *api.Server, log *slog.Logger) {
	dir := srv.Cfg.BackupDir
	if v, ok := srv.Store.GetSetting(ctx, "backup_dir"); ok && v != "" {
		dir = v
	}
	entries, err := os.ReadDir(dir)
	if err == nil {
		newest := time.Time{}
		for _, e := range entries {
			if fi, err := e.Info(); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		if time.Since(newest) < 20*time.Hour {
			return
		}
	}
	path, err := srv.BackupNow(ctx)
	if err != nil {
		log.Warn("automatic backup failed", "err", err)
		_ = srv.Store.AddServerEvent(ctx, "warn", "backup", "Automatic backup failed: "+err.Error())
		return
	}
	log.Info("automatic backup written", "path", path)
	// Keep the newest 14 backups.
	if entries, err := os.ReadDir(filepath.Dir(path)); err == nil && len(entries) > 14 {
		type fi struct {
			name string
			mod  time.Time
		}
		var list []fi
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				list = append(list, fi{e.Name(), info.ModTime()})
			}
		}
		for i := 0; i < len(list)-14; i++ {
			_ = os.Remove(filepath.Join(filepath.Dir(path), list[i].name))
		}
	}
}

func every(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}
