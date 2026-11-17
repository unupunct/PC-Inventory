//go:build !windows

package collect

import (
	"context"
	"fmt"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

func collectOSDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectOSDetailPlatform(ctx, inv, addErr)
}

func collectCPUDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {}

func collectMemoryModules(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectMemoryModulesPlatform(ctx, inv, addErr)
}

func collectGPUDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectGPUDetailPlatform(ctx, inv, addErr)
}

func collectMotherboardDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectMotherboardDetailPlatform(ctx, inv, addErr)
}

func collectPhysicalDisks(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectPhysicalDisksPlatform(ctx, inv, addErr)
}

func collectMonitors(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectMonitorsPlatform(ctx, inv, addErr)
}

func collectBatteries(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	bats, err := batteryStats(ctx)
	if err != nil || len(bats) == 0 {
		return
	}
	for _, b := range bats {
		inv.Batteries = append(inv.Batteries, models.Battery{Name: b.Name, Status: b.Status, Percent: b.Percent,
			DesignCapacityMWh: b.DesignMWh, FullCapacityMWh: b.FullMWh, CycleCount: b.Cycles, HealthPercent: healthPct(b)})
	}
}

func healthPct(b batteryInfo) *float64 {
	if b.DesignMWh == nil || b.FullMWh == nil || *b.DesignMWh <= 0 {
		return nil
	}
	h := float64(*b.FullMWh) / float64(*b.DesignMWh) * 100
	h = float64(int(h*10)) / 10
	return &h
}

func collectAudio(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectAudioPlatform(ctx, inv, addErr)
}

func collectUSB(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectUSBPlatform(ctx, inv, addErr)
}

func collectBluetooth(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectBluetoothPlatform(ctx, inv, addErr)
}

func collectOptical(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {}

func collectPrinters(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectPrintersPlatform(ctx, inv, addErr)
}

func collectSoftware(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectSoftwarePlatform(ctx, inv, addErr)
}

func collectLoggedInUser(ctx context.Context) string { return "" }

func collectAdapterDetail(ctx context.Context, na *models.NetAdapter, ifName string) {
	collectAdapterDetailPlatform(ctx, na, ifName)
}

func parseRouteLine(line string) (gw, iface string, ok bool) { return "", "", false }

func systemDNS(ctx context.Context) []string { return systemDNSPlatform(ctx) }

var _ = fmt.Sprintf
