package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/store"
)

func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid request")
		return
	}
	target := strings.TrimSpace(body.Target)
	method := "icmp+tcp"
	if target == "" {
		target = discoveryLocalSubnet()
		if target == "" {
			writeErr(w, 400, "no local subnet detected; specify an IP or CIDR range")
			return
		}
	}
	ips, err := s.Disc.ParseTargets(target)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ctx := r.Context()
	scanID, err := s.Store.CreateScan(ctx, target, method, sessionUser(ctx))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.audit(r, "discovery.scan", target, "")
	// Run asynchronously so the UI can poll progress.
	go func(scanID int64, ips []string, target string) {
		bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var lastProgress time.Time
		devices, err := s.Disc.Scan(bg, ips, func(scanned, found int) {
			if time.Since(lastProgress) < 2*time.Second {
				return
			}
			lastProgress = time.Now()
			_ = s.Store.FinishScan(bg, scanID, "running", scanned, found, "")
		})
		if err != nil {
			_ = s.Store.FinishScan(bg, scanID, "failed", len(ips), 0, err.Error())
			return
		}
		for _, d := range devices {
			_ = s.Store.UpsertDiscoveredDevice(bg, store.DiscoveredDevice{
				ScanID: &scanID, IP: d.IP, MAC: d.MAC, Hostname: d.Hostname, OpenPorts: d.OpenPorts, Source: "scan",
			})
		}
		_ = s.Store.LinkDiscoveredToAgents(bg)
		_ = s.Store.FinishScan(bg, scanID, "completed", len(ips), len(devices), "")
		s.Hub.Broadcast("discovery", map[string]any{"scanId": scanID, "found": len(devices)})
	}(scanID, ips, target)
	writeJSON(w, http.StatusAccepted, map[string]any{"scanId": scanID, "target": target, "hosts": len(ips)})
}

func (s *Server) handleListScans(w http.ResponseWriter, r *http.Request) {
	scans, err := s.Store.ListScans(r.Context(), 30)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"scans": scans, "localSubnet": discoveryLocalSubnet(), "agentPort": s.Cfg.Port})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.Store.ListDiscoveredDevices(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"devices": devices})
}

func (s *Server) handleAddDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IP       string `json:"ip"`
		MAC      string `json:"mac"`
		Hostname string `json:"hostname"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.IP) == "" {
		writeErr(w, 400, "ip is required")
		return
	}
	if err := s.Store.UpsertDiscoveredDevice(r.Context(), store.DiscoveredDevice{IP: strings.TrimSpace(body.IP), MAC: strings.ToUpper(strings.TrimSpace(body.MAC)), Hostname: body.Hostname, Source: "manual"}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_ = s.Store.LinkDiscoveredToAgents(r.Context())
	s.audit(r, "discovery.add", body.IP, "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var did int64
	for _, c := range id {
		did = did*10 + int64(c-'0')
	}
	if err := s.Store.DeleteDiscoveredDevice(r.Context(), did); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
