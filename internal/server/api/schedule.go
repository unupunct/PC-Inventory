package api

// Scheduled report delivery: builds the same PDF/CSV reports the Reports page
// offers and emails them on a cron-ish schedule. The scheduler runs inside the
// server process; if SMTP is not configured nothing is sent and the reason is
// recorded in the server event log instead of failing silently.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/alerts"
)

const lastReportRunKey = "last_scheduled_report_at"

// buildReportBytes renders a report kind to CSV or PDF.
func (s *Server) buildReportBytes(ctx context.Context, kind, format string) ([]byte, string, error) {
	rows, err := s.Store.ListInventoryRows(ctx, false)
	if err != nil {
		return nil, "", err
	}
	s.enrichInventory(rows, nil)

	var rep report
	var name string
	switch kind {
	case "inventory":
		rep, name = buildInventoryReport(ctx, s, rows), "hardware-inventory"
	case "network":
		rep, name = buildNetworkReport(rows), "network-summary"
	case "storage":
		rep, name = buildStorageReport(rows), "storage"
	case "distribution":
		rep, name = buildDistributionReport(rows), "hardware-distribution"
	case "changes":
		changes, _ := s.Store.ListHardwareChanges(ctx, "", 500)
		rep, name = changesReport{GeneratedAt: time.Now().UTC(), Changes: changes}, "hardware-changes"
	default:
		return nil, "", fmt.Errorf("unknown report kind %q", kind)
	}

	day := time.Now().Format("2006-01-02")
	if format == "csv" {
		var b strings.Builder
		if err := rep.WriteCSV(&b); err != nil {
			return nil, "", err
		}
		return []byte(b.String()), fmt.Sprintf("%s-%s.csv", name, day), nil
	}
	pdf, err := rep.PDF()
	if err != nil {
		return nil, "", err
	}
	return pdf, fmt.Sprintf("%s-%s.pdf", name, day), nil
}

// StartReportScheduler runs the scheduled email report in the background.
func (s *Server) StartReportScheduler(ctx context.Context) {
	go func() {
		// Check every minute; the schedule itself is a daily clock time.
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.maybeSendScheduledReport(ctx)
			}
		}
	}()
}

func (s *Server) maybeSendScheduledReport(ctx context.Context) {
	settings, err := s.Store.AllSettings(ctx)
	if err != nil {
		return
	}
	enabled := strings.EqualFold(settings["report_schedule_enabled"], "true")
	if !enabled {
		return
	}
	at := strings.TrimSpace(settings["report_schedule"])
	if at == "" {
		at = "07:00"
	}
	hhmm := time.Now().Format("15:04")
	if hhmm != at {
		return
	}
	// Only once per day.
	if last := settings[lastReportRunKey]; last != "" {
		if t, err := time.Parse(time.RFC3339, last); err == nil && time.Since(t) < 20*time.Hour {
			return
		}
	}
	if !s.Email.Configured() {
		_ = s.Store.SetSetting(ctx, lastReportRunKey, time.Now().UTC().Format(time.RFC3339))
		_ = s.Store.AddServerEvent(ctx, "warn", "reports", "Scheduled report skipped: SMTP is not configured")
		return
	}

	kinds := []string{"inventory"}
	if k := strings.TrimSpace(settings["report_kinds"]); k != "" {
		kinds = nil
		for _, part := range strings.Split(k, ",") {
			if part = strings.TrimSpace(part); part != "" {
				kinds = append(kinds, part)
			}
		}
	}
	format := strings.TrimSpace(settings["report_format"])
	if format == "" {
		format = "pdf"
	}
	var rcpts []string
	for _, r := range strings.Split(settings["report_recipients"], ",") {
		if r = strings.TrimSpace(r); r != "" {
			rcpts = append(rcpts, r)
		}
	}
	if len(rcpts) == 0 {
		rcpts = []string{s.Cfg.AlertEmailTo}
	}

	var atts []alerts.Attachment
	for _, kind := range kinds {
		data, filename, err := s.buildReportBytes(ctx, kind, format)
		if err != nil {
			_ = s.Store.AddServerEvent(ctx, "error", "reports", fmt.Sprintf("Scheduled %s report failed: %v", kind, err))
			continue
		}
		ct := "application/pdf"
		if format == "csv" {
			ct = "text/csv"
		}
		atts = append(atts, alerts.Attachment{Filename: filename, ContentType: ct, Data: data})
	}
	_ = s.Store.SetSetting(ctx, lastReportRunKey, time.Now().UTC().Format(time.RFC3339))
	if len(atts) == 0 {
		return
	}
	body := fmt.Sprintf("Scheduled PC Inventory Manager report\r\n\r\nGenerated %s\r\nComputers covered: see the attached report.\r\n",
		time.Now().Format(time.RFC1123))
	if err := s.Email.SendMailWithAttachments(ctx, rcpts, "PC Inventory Manager scheduled report", body, atts); err != nil {
		_ = s.Store.AddServerEvent(ctx, "error", "reports", "Scheduled report email failed: "+err.Error())
		return
	}
	_ = s.Store.AddServerEvent(ctx, "info", "reports", fmt.Sprintf("Scheduled report emailed to %s", strings.Join(rcpts, ", ")))
}

// recordReportSettingsDefaults writes the keys the Settings page shows when
// they are missing, so the UI always has something to display.
func (s *Server) RecordReportSettingsDefaults(ctx context.Context) {
	defaults := map[string]string{
		"report_schedule_enabled": "false",
		"report_schedule":         "07:00",
		"report_kinds":            "inventory",
		"report_format":           "pdf",
		"report_recipients":       "",
	}
	existing, err := s.Store.AllSettings(ctx)
	if err != nil {
		return
	}
	for k, v := range defaults {
		if _, ok := existing[k]; !ok {
			_ = s.Store.SetSetting(ctx, k, v)
		}
	}
}
