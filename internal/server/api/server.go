package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/alerts"
	"github.com/pcim/pc-inventory-manager/internal/server/config"
	"github.com/pcim/pc-inventory-manager/internal/server/discovery"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/server/webui"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

// Server is the HTTP server holding all dependencies.
type Server struct {
	Cfg      config.Config
	Store    *store.Store
	Log      *slog.Logger
	Hub      *Hub
	Alerts   *alerts.Engine
	Email    *alerts.EmailNotifier
	Telegram *alerts.TelegramNotifier
	Disc     *discovery.Scanner

	StartedAt time.Time
	BuildInfo string

	// counters
	reqCount    atomic.Int64
	activeConns atomic.Int64

	// admin throttle: username+ip -> failed attempts
	loginMu    sync.Mutex
	loginFails map[string]loginFail // created lazily by recordLoginFailure
}

type loginFail struct {
	count int
	until time.Time
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// --- Agent API (token authenticated, no session) ---
	mux.HandleFunc("POST /api/agent/enroll", s.handleEnroll)
	mux.HandleFunc("POST /api/agent/heartbeat", s.agentAuth(s.handleHeartbeat))
	mux.HandleFunc("POST /api/agent/metrics", s.agentAuth(s.handleMetrics))
	mux.HandleFunc("POST /api/agent/inventory", s.agentAuth(s.handleInventory))
	mux.HandleFunc("POST /api/agent/logs", s.agentAuth(s.handleAgentLogs))
	mux.HandleFunc("GET /api/agent/config", s.agentAuth(s.handleAgentConfig))
	mux.HandleFunc("POST /api/agent/rotate-token", s.agentAuth(s.handleRotateToken))
	mux.HandleFunc("GET /api/agent/ping", s.handleAgentPing)

	// --- Admin API (session authenticated) ---
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.sessionAuth(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", s.sessionAuth(s.handleMe))
	mux.HandleFunc("POST /api/auth/password", s.sessionAuth(s.handleChangePassword))

	mux.HandleFunc("GET /api/dashboard", s.sessionAuth(s.handleDashboard))
	mux.HandleFunc("GET /api/stream", s.sessionAuth(s.handleStream))
	mux.HandleFunc("GET /api/agents", s.sessionAuth(s.handleListAgents))
	mux.HandleFunc("GET /api/agents/{id}", s.sessionAuth(s.handleGetAgent))
	mux.HandleFunc("PATCH /api/agents/{id}", s.sessionAuth(s.handleUpdateAgent))
	mux.HandleFunc("POST /api/agents/{id}/approve", s.sessionAuth(s.handleApproveAgent))
	mux.HandleFunc("POST /api/agents/{id}/reject", s.sessionAuth(s.handleRejectAgent))
	mux.HandleFunc("POST /api/agents/{id}/disable", s.sessionAuth(s.handleDisableAgent))
	mux.HandleFunc("POST /api/agents/{id}/enable", s.sessionAuth(s.handleEnableAgent))
	mux.HandleFunc("POST /api/agents/{id}/revoke", s.sessionAuth(s.handleRevokeAgent))
	mux.HandleFunc("DELETE /api/agents/{id}", s.sessionAuth(s.handleDeleteAgent))
	mux.HandleFunc("POST /api/agents/{id}/rotate-token", s.sessionAuth(s.handleAdminRotateToken))
	mux.HandleFunc("POST /api/agents/{id}/refresh-inventory", s.sessionAuth(s.handleRefreshInventory))
	mux.HandleFunc("GET /api/agents/{id}/logs", s.sessionAuth(s.handleGetAgentLogs))
	mux.HandleFunc("GET /api/agents/{id}/events", s.sessionAuth(s.handleGetAgentEvents))
	mux.HandleFunc("GET /api/agents/{id}/inventory", s.sessionAuth(s.handleGetAgentInventory))
	mux.HandleFunc("GET /api/agents/{id}/performance", s.sessionAuth(s.handleGetAgentPerformance))
	mux.HandleFunc("GET /api/agents/{id}/snapshots", s.sessionAuth(s.handleGetSnapshots))
	mux.HandleFunc("GET /api/enrollments", s.sessionAuth(s.handleListEnrollments))
	mux.HandleFunc("GET /api/inventory", s.sessionAuth(s.handleInventoryList))
	mux.HandleFunc("GET /api/changes", s.sessionAuth(s.handleListChanges))
	mux.HandleFunc("GET /api/alerts", s.sessionAuth(s.handleListAlerts))
	mux.HandleFunc("GET /api/alerts/rules", s.sessionAuth(s.handleListAlertRules))
	mux.HandleFunc("PUT /api/alerts/rules/{key}", s.sessionAuth(s.handleUpdateAlertRule))
	mux.HandleFunc("POST /api/alerts/{id}/ack", s.sessionAuth(s.handleAckAlert))
	mux.HandleFunc("POST /api/alerts/{id}/resolve", s.sessionAuth(s.handleResolveAlert))
	mux.HandleFunc("POST /api/alerts/test-notification", s.sessionAuth(s.handleTestNotification))
	mux.HandleFunc("GET /api/groups", s.sessionAuth(s.handleListGroups))
	mux.HandleFunc("POST /api/groups", s.sessionAuth(s.handleCreateGroup))
	mux.HandleFunc("DELETE /api/groups/{id}", s.sessionAuth(s.handleDeleteGroup))
	mux.HandleFunc("GET /api/settings", s.sessionAuth(s.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", s.sessionAuth(s.handleUpdateSettings))
	mux.HandleFunc("GET /api/health", s.sessionAuth(s.handleHealth))
	mux.HandleFunc("GET /api/audit", s.sessionAuth(s.handleAudit))
	mux.HandleFunc("POST /api/backup", s.sessionAuth(s.handleBackup))
	mux.HandleFunc("GET /api/reports/{kind}", s.sessionAuth(s.handleReport))
	mux.HandleFunc("GET /api/search", s.sessionAuth(s.handleSearch))
	mux.HandleFunc("POST /api/discovery/scan", s.sessionAuth(s.handleStartScan))
	mux.HandleFunc("GET /api/discovery/scans", s.sessionAuth(s.handleListScans))
	mux.HandleFunc("GET /api/discovery/devices", s.sessionAuth(s.handleListDevices))
	mux.HandleFunc("POST /api/discovery/devices", s.sessionAuth(s.handleAddDevice))
	mux.HandleFunc("DELETE /api/discovery/devices/{id}", s.sessionAuth(s.handleDeleteDevice))
	mux.HandleFunc("GET /api/system", s.handleSystemInfo)

	// --- Static web UI ---
	mux.Handle("/", webui.Handler())

	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.reqCount.Add(1)
		s.activeConns.Add(1)
		defer s.activeConns.Add(-1)

		// Access control: optional LAN CIDR restriction.
		if len(s.Cfg.AllowedCIDRs) > 0 && !s.ipAllowed(clientIP(r, s.Cfg.TrustProxy)) {
			writeErr(w, http.StatusForbidden, "your address is not permitted by the server's access policy")
			return
		}

		// Basic security headers.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-XSS-Protection", "0")

		rec := &statusRecorder{ResponseWriter: w, status: 200}
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			r.Body = http.MaxBytesReader(w, r.Body, s.Cfg.MaxBodyBytes)
		}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)
		// Skip noisy/static paths in stats.
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/stream" {
			go s.Store.RecordRequest(context.Background(), normalizePath(r.URL.Path), r.Method, rec.status, float64(dur.Microseconds())/1000)
		}
	})
}

func normalizePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if len(part) >= 32 || (len(part) > 0 && part[0] >= '0' && part[0] <= '9' && len(part) > 6) {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) ipAllowed(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, c := range s.Cfg.AllowedCIDRs {
		if strings.Contains(c, "/") {
			_, n, err := net.ParseCIDR(c)
			if err == nil && n.Contains(parsed) {
				return true
			}
		} else if net.ParseIP(c) != nil && net.ParseIP(c).Equal(parsed) {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- JSON helpers --------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, dst any) error {
	var body io.Reader = r.Body
	// Agents gzip their request bodies; decompress transparently.
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "gzip") {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			return fmt.Errorf("invalid gzip body: %w", err)
		}
		defer zr.Close()
		body = zr
	}
	dec := json.NewDecoder(io.LimitReader(body, 64<<20))
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("empty request body")
		}
		return err
	}
	return nil
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func queryBool(r *http.Request, key string) bool {
	v := r.URL.Query().Get(key)
	return v == "1" || v == "true" || v == "yes"
}

// --- system info ---------------------------------------------------------

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":          "PC Inventory Manager",
		"version":       version.Version,
		"protocol":      version.Protocol,
		"serverName":    s.serverName(r.Context()),
		"advertisedUrl": s.Cfg.AdvertisedURL(),
		"uptimeSec":     int(time.Since(s.StartedAt).Seconds()),
	})
}

func (s *Server) serverName(ctx context.Context) string {
	if v, ok := s.Store.GetSetting(ctx, "server_name"); ok && v != "" {
		return v
	}
	return "PC Inventory Manager"
}

// handleAgentPing lets agents test connectivity without a token.
func (s *Server) handleAgentPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "serverTime": time.Now().UTC(), "version": version.Version, "protocol": version.Protocol, "serverName": s.serverName(r.Context())})
}

// latestConfig returns the effective agent config: server defaults merged with
// settings and per-agent overrides.
func (s *Server) latestConfig(ctx context.Context, agent *store.Agent) models.AgentConfig {
	cfg := models.DefaultAgentConfig()
	if v, ok := s.Store.GetSetting(ctx, "default_agent_config"); ok && v != "" {
		_ = json.Unmarshal([]byte(v), &cfg)
	}
	if agent != nil && agent.Config != nil {
		cfg = *agent.Config
	}
	return cfg
}
