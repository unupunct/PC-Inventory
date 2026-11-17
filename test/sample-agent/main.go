// Command pcim-sample-agent is a development aid for testing the server
// without access to the real platform.
//
// It sends a clearly-labelled synthetic inventory (hostname prefix "sample-",
// collectionErrors entry "synthetic"), so anything it produces is obviously
// not a real computer. It is never shipped in a release bundle and must not be
// used to populate a production dashboard.
//
// Usage:
//
//	pcim-sample-agent -server http://127.0.0.1:8080 [-metrics 5s] [-inventory 60s]
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

const sampleName = "sample-vm"

func main() {
	server := flag.String("server", "http://127.0.0.1:8080", "server base URL")
	metricsEvery := flag.Duration("metrics", 5*time.Second, "metrics interval")
	inventoryEvery := flag.Duration("inventory", 60*time.Second, "inventory interval")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &sample{base: *server, id: "", token: ""}
	if err := a.enroll(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "enroll:", err)
		os.Exit(1)
	}
	fmt.Printf("enrolled as agent %s\n", a.id)

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := a.sendHeartbeat(ctx); err != nil {
					fmt.Fprintln(os.Stderr, "heartbeat:", err)
				}
			}
		}
	}()

	go func() {
		t := time.NewTicker(*metricsEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := a.sendMetrics(ctx); err != nil {
					fmt.Fprintln(os.Stderr, "metrics:", err)
				}
			}
		}
	}()

	time.Sleep(2 * time.Second)
	if err := a.sendInventory(ctx, "startup"); err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
	}
	t := time.NewTicker(*inventoryEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := a.sendInventory(ctx, "scheduled"); err != nil {
				fmt.Fprintln(os.Stderr, "inventory:", err)
			}
		}
	}
}

type sample struct {
	base, id, token string
}

func (s *sample) machineID() string {
	h, _ := os.Hostname()
	return fmt.Sprintf("%x", hashString("sample:"+h+runtime.GOOS))
}

func (s *sample) enroll(ctx context.Context) error {
	body := models.EnrollmentRequest{
		MachineID:    s.machineID(),
		Hostname:     sampleName + "-" + runtime.GOOS,
		Platform:     runtime.GOOS,
		OSName:       "Synthetic " + runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: version.Version,
		Protocol:     version.Protocol,
	}
	for attempt := 0; attempt < 120; attempt++ {
		var out models.EnrollmentResponse
		code, err := s.post(ctx, "/api/agent/enroll", body, &out, false)
		if err != nil {
			return err
		}
		switch code {
		case http.StatusOK:
			s.id, s.token = out.AgentID, out.AgentToken
			return nil
		case http.StatusAccepted:
			if attempt == 0 {
				fmt.Println("waiting for approval in the dashboard (Enrollments) ...")
			}
		default:
			return fmt.Errorf("enroll returned %d: %s", code, out.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("still not approved")
}

func (s *sample) sendHeartbeat(ctx context.Context) error {
	_, err := s.postRaw(ctx, "/api/agent/heartbeat", models.HeartbeatRequest{
		AgentID: s.id, SentAt: time.Now().UTC(), AgentVersion: version.Version,
	})
	return err
}

func (s *sample) sendMetrics(ctx context.Context) error {
	now := time.Now().UTC()
	var used uint64 = 1288490188 // 1.2 GiB
	m := models.Metrics{
		CollectedAt:   now,
		IntervalSec:   5,
		UptimeSeconds: uint64(rand.Intn(100000)),
		CPU:           models.CPUMetrics{UsagePercent: 5 + rand.Float64()*20},
		Memory: models.MemoryMetrics{
			TotalBytes: 4 << 30, UsedBytes: used, AvailableBytes: 4<<30 - used,
			UsagePercent: 30 + rand.Float64()*10,
		},
		Load:        &models.LoadAvg{One: 0.4 + rand.Float64(), Five: 0.5 + rand.Float64(), Fifteen: 0.6 + rand.Float64()},
		Unsupported: []string{"synthetic sample data, not a real computer"},
	}
	_, err := s.postRaw(ctx, "/api/agent/metrics", models.MetricsRequest{AgentID: s.id, Metrics: m})
	return err
}

func (s *sample) sendInventory(ctx context.Context, reason string) error {
	now := time.Now().UTC()
	inv := models.Inventory{
		CollectedAt: now,
		Hash:        fmt.Sprintf("%x", hashString(sampleName+reason+now.Format("2006-01-02T15"))),
		Host: models.Host{
			Hostname:     sampleName + "-" + runtime.GOOS,
			Platform:     runtime.GOOS,
			Manufacturer: "Synthetic (test only)",
			Model:        "Sample VM",
			ChassisType:  "Virtual",
			AgentVersion: version.Version,
		},
		OS: models.OperatingSystem{
			Name: "Synthetic " + runtime.GOOS, Version: "0", Build: "test", Arch: runtime.GOARCH,
		},
		CPU: models.CPU{
			Brand: "Synthetic CPU", Architecture: runtime.GOARCH, PhysicalCores: 2, LogicalCores: 4,
		},
		Memory: models.Memory{TotalBytes: 4 << 30, ModulesSupported: false},
		CollectionErrors: []models.CollectorError{
			{Category: "all", Error: "synthetic sample data generated by pcim-sample-agent; not real hardware"},
		},
	}
	_, err := s.postRaw(ctx, "/api/agent/inventory", models.InventoryRequest{AgentID: s.id, Inventory: inv, Reason: reason})
	return err
}

func (s *sample) post(ctx context.Context, path string, body, out any, auth bool) (int, error) {
	return s.do(ctx, path, body, out, auth)
}

func (s *sample) postRaw(ctx context.Context, path string, body any) (int, error) {
	return s.do(ctx, path, body, nil, true)
}

func (s *sample) do(ctx context.Context, path string, body, out any, auth bool) (int, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("X-Agent-Id", s.id)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

func hashString(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
