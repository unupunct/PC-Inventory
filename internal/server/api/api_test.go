package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/auth"
	"github.com/pcim/pc-inventory-manager/internal/server/config"
	"github.com/pcim/pc-inventory-manager/internal/server/store"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

const testPassword = "Correct-Horse-1"

// newTestServer spins up a server on a temporary database with one admin.
func newTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := st.CreateAdmin(t.Context(), "admin", hash); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	cfg := config.Default()
	cfg.DataDir = dir
	cfg.DBPath = filepath.Join(dir, "test.db")
	cfg.BackupDir = filepath.Join(dir, "backups")
	s := &Server{
		Cfg: cfg, Store: st,
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hub:        NewHub(),
		StartedAt:  time.Now(),
		loginFails: map[string]loginFail{},
	}
	return s, s.Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func login(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{"username": "admin", "password": testPassword}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Token == "" {
		t.Fatal("no session token returned")
	}
	return out.Token
}

func TestLoginRejectsBadPassword(t *testing.T) {
	_, h := newTestServer(t)
	rec := doJSON(t, h, http.MethodPost, "/api/auth/login", map[string]string{"username": "admin", "password": "wrong"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestSessionRequired(t *testing.T) {
	_, h := newTestServer(t)
	for _, path := range []string{"/api/agents", "/api/dashboard", "/api/inventory", "/api/changes"} {
		if rec := doJSON(t, h, http.MethodGet, path, nil, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: want 401, got %d", path, rec.Code)
		}
	}
}

// TestEnrollmentApprovalFlow walks the real flow: pending -> approve -> token.
func TestEnrollmentApprovalFlow(t *testing.T) {
	_, h := newTestServer(t)
	machine := "machine-under-test"
	enroll := models.EnrollmentRequest{
		MachineID: machine, Hostname: "pc-01", Platform: "windows",
		OSName: "Windows 11", OSVersion: "24H2", Arch: "amd64",
		AgentVersion: version.Version, Protocol: version.Protocol,
	}
	rec := doJSON(t, h, http.MethodPost, "/api/agent/enroll", enroll, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first enroll: want 202, got %d %s", rec.Code, rec.Body.String())
	}
	var pending models.EnrollmentResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &pending)
	if pending.Status != "pending" || pending.AgentID == "" {
		t.Fatalf("unexpected enrollment response: %+v", pending)
	}

	tok := login(t, h)
	authHdr := map[string]string{"Authorization": "Bearer " + tok}
	if rec := doJSON(t, h, http.MethodPost, "/api/agents/"+pending.AgentID+"/approve", nil, authHdr); rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}

	// The approved agent collects its token on the next poll.
	rec = doJSON(t, h, http.MethodPost, "/api/agent/enroll", enroll, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll after approval: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var approved models.EnrollmentResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &approved)
	if approved.Status != "approved" || approved.AgentToken == "" {
		t.Fatalf("no token handed out: %+v", approved)
	}

	// The token authenticates agent endpoints.
	hdr := map[string]string{"Authorization": "Bearer " + approved.AgentToken, "X-Agent-Id": approved.AgentID}
	if rec := doJSON(t, h, http.MethodGet, "/api/agent/config", nil, hdr); rec.Code != http.StatusOK {
		t.Fatalf("agent config with token: %d %s", rec.Code, rec.Body.String())
	}
	// A wrong token is refused.
	bad := map[string]string{"Authorization": "Bearer not-a-real-token", "X-Agent-Id": approved.AgentID}
	if rec := doJSON(t, h, http.MethodGet, "/api/agent/config", nil, bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token: want 401, got %d", rec.Code)
	}
}

// TestGzipAgentBody proves agents may gzip their request bodies.
func TestGzipAgentBody(t *testing.T) {
	_, h := newTestServer(t)
	payload := models.EnrollmentRequest{MachineID: "gz-machine", Hostname: "gz", Platform: "linux", Protocol: version.Protocol}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_ = json.NewEncoder(zw).Encode(payload)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/agent/enroll", &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("gzip enroll: want 202, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestMetricsRelfectsAgentData checks that a reported sample is stored as sent.
func TestMetricsReflectAgentData(t *testing.T) {
	s, h := newTestServer(t)
	agentID, token := approveAgent(t, h, "metrics-machine")

	procs := 123
	req := models.MetricsRequest{
		AgentID: agentID,
		Metrics: models.Metrics{
			CollectedAt: time.Now().UTC(),
			CPU:         models.CPUMetrics{UsagePercent: 37.5},
			Memory:      models.MemoryMetrics{TotalBytes: 16 << 30, UsedBytes: 8 << 30, UsagePercent: 50},
			Processes:   &procs,
		},
	}
	hdr := map[string]string{"Authorization": "Bearer " + token, "X-Agent-Id": agentID}
	if rec := doJSON(t, h, http.MethodPost, "/api/agent/metrics", req, hdr); rec.Code != http.StatusOK {
		t.Fatalf("metrics: %d %s", rec.Code, rec.Body.String())
	}
	samples, _, err := s.Store.PerfSeries(t.Context(), agentID, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("perf series: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("want 1 sample, got %d", len(samples))
	}
	if samples[0].CPUPercent != 37.5 || samples[0].ProcessCount == nil || *samples[0].ProcessCount != 123 {
		t.Fatalf("stored sample differs from what the agent sent: %+v", samples[0])
	}
}

// approveAgent enrolls and approves one agent, returning its id and token.
func approveAgent(t *testing.T, h http.Handler, machine string) (string, string) {
	t.Helper()
	enroll := models.EnrollmentRequest{MachineID: machine, Hostname: machine, Platform: "windows", Protocol: version.Protocol}
	rec := doJSON(t, h, http.MethodPost, "/api/agent/enroll", enroll, nil)
	var pending models.EnrollmentResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &pending)
	tok := login(t, h)
	authHdr := map[string]string{"Authorization": "Bearer " + tok}
	doJSON(t, h, http.MethodPost, "/api/agents/"+pending.AgentID+"/approve", nil, authHdr)
	rec = doJSON(t, h, http.MethodPost, "/api/agent/enroll", enroll, nil)
	var approved models.EnrollmentResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &approved)
	if approved.AgentToken == "" {
		t.Fatalf("approval did not yield a token: %d %s", rec.Code, rec.Body.String())
	}
	return approved.AgentID, approved.AgentToken
}

var _ = os.TempDir
