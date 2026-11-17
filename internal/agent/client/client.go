// Package client is the agent's HTTP client for the central server.
package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

type Client struct {
	BaseURL    string
	AgentID    string
	Token      string
	HTTP       *http.Client
	ServerCACert string
}

// New creates a client with sensible LAN timeouts.
func New(baseURL, agentID, token string, insecure bool, caFile string) *Client {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure}
	if caFile != "" {
		if pem, err := os.ReadFile(caFile); err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				tlsCfg.RootCAs = pool
			}
		}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		AgentID: agentID,
		Token:   token,
		ServerCACert: caFile,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     tlsCfg,
				MaxIdleConns:        4,
				IdleConnTimeout:     90 * time.Second,
				DisableCompression:  false,
				DialContext: (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			},
		},
	}
}

type HTTPError struct {
	Status  int
	Body    string
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("server returned %d: %s", e.Status, strings.TrimSpace(e.Message))
}

var ErrUnauthorized = errors.New("unauthorized")

func (c *Client) do(ctx context.Context, method, path string, body any, out any, auth bool, extraHeaders map[string]string) error {
	var rdr io.Reader
	if body != nil {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if err := json.NewEncoder(gz).Encode(body); err != nil {
			return err
		}
		if err := gz.Close(); err != nil {
			return err
		}
		rdr = &buf
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")
	}
	req.Header.Set("User-Agent", "pcim-agent/1.0")
	req.Header.Set("Accept", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("X-Agent-Id", c.AgentID)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		msg := e.Error
		if msg == "" {
			msg = e.Message
		}
		if msg == "" {
			msg = string(data)
		}
		return &HTTPError{Status: resp.StatusCode, Body: string(data), Message: msg}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// Ping tests connectivity without credentials.
func (c *Client) Ping(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodGet, "/api/agent/ping", nil, &out, false, nil)
	return out, err
}

// Enroll requests enrollment. secret is only set when re-enrolling.
func (c *Client) Enroll(ctx context.Context, req models.EnrollmentRequest, secret string) (*models.EnrollmentResponse, int, error) {
	var out models.EnrollmentResponse
	var resp struct {
		models.EnrollmentResponse
		Error string `json:"error"`
	}
	hdrs := map[string]string{}
	if secret != "" {
		hdrs["X-Enroll-Secret"] = secret
	}
	err := c.do(ctx, http.MethodPost, "/api/agent/enroll", req, &resp, false, hdrs)
	if err != nil {
		var he *HTTPError
		if errors.As(err, &he) {
			if resp.Status != "" {
				return &resp.EnrollmentResponse, he.Status, nil
			}
			return nil, he.Status, fmt.Errorf("%s", he.Message)
		}
		return nil, 0, err
	}
	out = resp.EnrollmentResponse
	return &out, http.StatusOK, nil
}

func (c *Client) Heartbeat(ctx context.Context, req models.HeartbeatRequest) (*models.HeartbeatResponse, error) {
	var out models.HeartbeatResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/heartbeat", req, &out, true, nil)
	return &out, err
}

func (c *Client) Metrics(ctx context.Context, req models.MetricsRequest) error {
	return c.do(ctx, http.MethodPost, "/api/agent/metrics", req, nil, true, nil)
}

func (c *Client) Inventory(ctx context.Context, req models.InventoryRequest) (*models.IngestResponse, error) {
	var out models.IngestResponse
	err := c.do(ctx, http.MethodPost, "/api/agent/inventory", req, &out, true, nil)
	return &out, err
}

func (c *Client) Logs(ctx context.Context, req models.LogsRequest) error {
	return c.do(ctx, http.MethodPost, "/api/agent/logs", req, nil, true, nil)
}

func (c *Client) Config(ctx context.Context) (map[string]any, error) {
	var out struct {
		Config        map[string]any `json:"config"`
		ConfigVersion int            `json:"configVersion"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/agent/config", nil, &out, true, nil); err != nil {
		return nil, err
	}
	return out.Config, nil
}

// ---------------------------------------------------------------------------
// Auto-update
// ---------------------------------------------------------------------------

// UpdateInfo describes a published agent release.
type UpdateInfo struct {
	Version     string    `json:"version"`
	Platform    string    `json:"platform"`
	Arch        string    `json:"arch"`
	Filename    string    `json:"filename"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"sizeBytes"`
	PublishedAt time.Time `json:"publishedAt"`
}

// CheckUpdate asks the server whether a newer agent build exists for this
// platform. Returns nil when updates are disabled or the agent is current.
func (c *Client) CheckUpdate(ctx context.Context, platform, arch, current string) (*UpdateInfo, string, error) {
	var out struct {
		Enabled bool        `json:"enabled"`
		Update  *UpdateInfo `json:"update"`
		Message string      `json:"message"`
	}
	path := fmt.Sprintf("/api/agent/update?platform=%s&arch=%s&current=%s", platform, arch, current)
	if err := c.do(ctx, http.MethodGet, path, nil, &out, true, nil); err != nil {
		return nil, "", err
	}
	if !out.Enabled || out.Update == nil {
		return nil, out.Message, nil
	}
	return out.Update, out.Message, nil
}

// DownloadUpdate streams the announced update to dst.
func (c *Client) DownloadUpdate(ctx context.Context, platform, arch string, dst io.Writer) error {
	path := fmt.Sprintf("/api/agent/update/download?platform=%s&arch=%s", platform, arch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-Agent-Id", c.AgentID)
	req.Header.Set("User-Agent", "pcim-agent/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{Status: resp.StatusCode, Body: string(data), Message: string(data)}
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}
