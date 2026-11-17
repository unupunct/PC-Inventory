package collect

import (
	"testing"

	"github.com/pcim/pc-inventory-manager/internal/agent/config"
)

// TestCollectorInventoryIsReal asserts the collector produces host data that
// comes from the machine, never from a fixture.
func TestCollectorInventoryIsReal(t *testing.T) {
	if testing.Short() {
		t.Skip("collects real hardware")
	}
	col := New(config.Default())
	inv := col.Inventory(t.Context())
	if inv.Host.Hostname == "" {
		t.Fatal("hostname must be collected")
	}
	if inv.OS.Arch == "" {
		t.Fatal("architecture must be collected")
	}
	if inv.CPU.LogicalCores <= 0 {
		t.Fatal("cpu cores must be collected")
	}
	if inv.Memory.TotalBytes == 0 {
		t.Fatal("total memory must be collected")
	}
	if inv.Hash == "" {
		t.Fatal("inventory hash must be set")
	}
	if len(inv.Volumes) == 0 {
		t.Fatal("volumes must be collected")
	}
	// Modules reported as supported must actually carry capacities.
	if inv.Memory.ModulesSupported {
		var total uint64
		for _, m := range inv.Memory.Modules {
			if m.CapacityBytes == 0 {
				t.Fatal("module without capacity despite ModulesSupported")
			}
			total += m.CapacityBytes
		}
		if total == 0 {
			t.Fatal("no module capacity collected")
		}
	}
}

func TestCollectorMetricsAreSane(t *testing.T) {
	if testing.Short() {
		t.Skip("collects real metrics")
	}
	col := New(config.Default())
	_ = col.Metrics(t.Context()) // prime the counters
	m := col.Metrics(t.Context())
	if m.CPU.UsagePercent < 0 || m.CPU.UsagePercent > 100 {
		t.Fatalf("cpu usage out of range: %v", m.CPU.UsagePercent)
	}
	if m.Memory.TotalBytes == 0 {
		t.Fatal("total memory missing from metrics")
	}
	if m.Memory.UsagePercent < 0 || m.Memory.UsagePercent > 100 {
		t.Fatalf("memory usage out of range: %v", m.Memory.UsagePercent)
	}
}


