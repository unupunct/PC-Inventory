package collect

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

// ---------------------------------------------------------------------------
// macOS: system_profiler, ioreg, sysctl, pmset, networksetup
// ---------------------------------------------------------------------------

func collectOSDetailPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	inv.OS.Name = "macOS"
	if v := sysctl(ctx, "-n", "kern.osproductversion"); v != "" {
		inv.OS.Version = v
	}
	if v := sysctl(ctx, "-n", "kern.osversion"); v != "" {
		inv.OS.Build = v
	}
	if v := sysctl(ctx, "-n", "kern.osrelease"); v != "" {
		inv.OS.Kernel = "Darwin " + v
	}
	// Marketing name, e.g. "macOS 15.1.1 (Sequoia)" -> edition.
	if out := runCommand(ctx, []string{"sw_vers", "-productName"}); len(out) > 0 {
		inv.OS.Name = strings.TrimSpace(out[0])
	}
	// Hardware model identifier and Apple Silicon detection.
	if v := sysctl(ctx, "-n", "hw.model"); v != "" {
		inv.OS.HardwareModel = v
		inv.Host.Model = v
		inv.Motherboard.SystemProduct = v
	}
	apple := sysctl(ctx, "-n", "hw.optional.arm64") == "1"
	inv.OS.AppleSilicon = &apple
	if apple {
		inv.OS.Arch = "arm64"
	} else {
		inv.OS.Arch = sysctl(ctx, "-n", "hw.machine")
	}
	// Install date from the OS installation receipt.
	if out := runCommand(ctx, []string{"stat", "-f", "%SB", "/var/db/.AppleSetupDone"}); len(out) > 0 {
		if t, err := time.Parse("2006-01-02 15:04:05.000000000 -0700", strings.TrimSpace(out[0])); err == nil {
			inv.OS.InstallDate = &t
		}
	}
	// System serial number (platform expert device).
	ioreg := strings.Join(runCommand(ctx, []string{"ioreg", "-l"}), "\n")
	if m := regexp.MustCompile(`"IOPlatformSerialNumber"\s*=\s*"([^"]+)"`).FindStringSubmatch(ioreg); len(m) == 2 {
		inv.Host.SerialNumber = m[1]
		inv.Motherboard.SystemSerial = m[1]
	}
	// Model family from system_profiler hardware section (bounded output).
	sp := strings.Join(runCommand(ctx, []string{"system_profiler", "SPHardwareDataType"}), "\n")
	if m := regexp.MustCompile(`Model Name:\s*(.+)`).FindStringSubmatch(sp); len(m) == 2 {
		inv.Host.Model = strings.TrimSpace(m[1]) + " (" + inv.Host.Model + ")"
		inv.Motherboard.SystemProduct = strings.TrimSpace(m[1])
	}
	if m := regexp.MustCompile(`Chip:\s*(.+)`).FindStringSubmatch(sp); len(m) == 2 {
		inv.CPU.Brand = strings.TrimSpace(m[1])
		inv.CPU.Manufacturer = "Apple"
	}
	if m := regexp.MustCompile(`Total Number of Cores:\s*(\d+)`).FindStringSubmatch(sp); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil {
			inv.CPU.PhysicalCores = n
		}
	}
	if m := regexp.MustCompile(`Memory:\s*(.+)`).FindStringSubmatch(sp); len(m) == 2 {
		inv.Memory.TotalBytes = parseMacBytes(strings.TrimSpace(m[1]))
	}
	inv.Host.Manufacturer = "Apple"
	inv.OS.Timezone = sysctl(ctx, "-n", "machdep.timezone") // usually empty
	inv.OS.PackageManager = ""
	inv.OS.Systemd = nil
}

func sysctl(ctx context.Context, args ...string) string {
	out := runCommand(ctx, append([]string{"sysctl"}, args...))
	if len(out) == 0 {
		return ""
	}
	line := out[0]
	if i := strings.Index(line, ":"); i >= 0 && strings.HasPrefix(line, "kern") || strings.HasPrefix(line, "hw") || strings.HasPrefix(line, "machdep") {
		return strings.TrimSpace(line[i+1:])
	}
	return strings.TrimSpace(line)
}

func parseMacBytes(s string) uint64 {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{"KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "KiB": 1024, "MiB": 1 << 20, "GiB": 1 << 30}
	if m, ok := mult[strings.ToUpper(fields[1])]; ok {
		return uint64(v * m)
	}
	return 0
}

func collectMemoryModulesPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// Apple Silicon uses unified memory: no modules are exposed. Intel Macs
	// expose slots through system_profiler.
	if inv.OS.AppleSilicon != nil && *inv.OS.AppleSilicon {
		inv.Memory.ModulesSupported = false
		return
	}
	lines := runCommand(ctx, []string{"system_profiler", "SPMemoryDataType"})
	var cur *models.MemoryModule
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.Contains(t, ":") && (strings.Contains(t, "BANK") || strings.Contains(t, "DIMM")):
			if cur != nil {
				inv.Memory.Modules = append(inv.Memory.Modules, *cur)
			}
			cur = &models.MemoryModule{Slot: strings.TrimSuffix(t, ":")}
		case cur == nil:
			continue
		case strings.HasPrefix(t, "Size:"):
			cur.CapacityBytes = parseMacBytes(strings.TrimSpace(strings.TrimPrefix(t, "Size:")))
		case strings.HasPrefix(t, "Type:"):
			cur.Type = strings.TrimSpace(strings.TrimPrefix(t, "Type:"))
		case strings.HasPrefix(t, "Speed:"):
			if f := strings.Fields(strings.TrimPrefix(t, "Speed:")); len(f) > 0 {
				if n, err := strconv.Atoi(f[0]); err == nil {
					cur.SpeedMTs = &n
				}
			}
		case strings.HasPrefix(t, "Manufacturer:"):
			cur.Manufacturer = strings.TrimSpace(strings.TrimPrefix(t, "Manufacturer:"))
		case strings.HasPrefix(t, "Part Number:"):
			cur.PartNumber = strings.TrimSpace(strings.TrimPrefix(t, "Part Number:"))
		case strings.HasPrefix(t, "Serial Number:"):
			cur.SerialNumber = strings.TrimSpace(strings.TrimPrefix(t, "Serial Number:"))
		}
	}
	if cur != nil {
		inv.Memory.Modules = append(inv.Memory.Modules, *cur)
	}
	inv.Memory.ModulesSupported = len(inv.Memory.Modules) > 0
}

func collectGPUDetailPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	lines := runCommand(ctx, []string{"system_profiler", "SPDisplaysDataType"})
	var cur *models.GPU
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Chipset Model:") {
			if cur != nil {
				inv.GPUs = append(inv.GPUs, *cur)
			}
			cur = &models.GPU{Name: strings.TrimSpace(strings.TrimPrefix(t, "Chipset Model:"))}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(t, "Type:"):
			cur.Kind = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "Type:")))
		case strings.HasPrefix(t, "Vendor:"):
			cur.Manufacturer = strings.TrimSpace(strings.TrimPrefix(t, "Vendor:"))
		case strings.HasPrefix(t, "VRAM"):
			if i := strings.Index(t, ":"); i > 0 {
				cur.VRAMBytes = uintPtr(parseMacBytes(strings.TrimSpace(t[i+1:])))
			}
		case strings.HasPrefix(t, "Resolution:"):
			cur.Resolution = strings.TrimSpace(strings.TrimPrefix(t, "Resolution:"))
		case strings.HasPrefix(t, "Metal Support:"):
			cur.DriverVersion = strings.TrimSpace(strings.TrimPrefix(t, "Metal Support:"))
		}
	}
	if cur != nil {
		inv.GPUs = append(inv.GPUs, *cur)
	}
	for i := range inv.GPUs {
		if inv.GPUs[i].Manufacturer == "" {
			inv.GPUs[i].Manufacturer = "Apple"
		}
		if inv.GPUs[i].Kind == "" {
			inv.GPUs[i].Kind = "integrated"
		}
	}
}

func uintPtr(v uint64) *uint64 {
	if v == 0 {
		return nil
	}
	return &v
}

func collectMotherboardDetailPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	inv.Motherboard.Manufacturer = "Apple"
	lines := runCommand(ctx, []string{"system_profiler", "SPHardwareDataType"})
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "Model Identifier:"):
			inv.Motherboard.Product = strings.TrimSpace(strings.TrimPrefix(t, "Model Identifier:"))
		case strings.HasPrefix(t, "Boot ROM Version:"):
			inv.Motherboard.BIOSVersion = strings.TrimSpace(strings.TrimPrefix(t, "Boot ROM Version:"))
		case strings.HasPrefix(t, "System Firmware Version:"):
			if inv.Motherboard.BIOSVersion == "" {
				inv.Motherboard.BIOSVersion = strings.TrimSpace(strings.TrimPrefix(t, "System Firmware Version:"))
			}
		case strings.HasPrefix(t, "OS Loader Version:"):
			inv.Motherboard.BIOSDate = ""
		}
	}
	inv.Motherboard.SystemManufacturer = "Apple"
	if inv.Motherboard.SystemSerial == "" {
		inv.Motherboard.SystemSerial = inv.Host.SerialNumber
	}
}

func collectPhysicalDisksPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	lines := runCommand(ctx, []string{"system_profiler", "SPNVMeDataType", "SPSerialATADataType"})
	var cur *models.PhysicalDisk
	flush := func() {
		if cur != nil && cur.Name != "" {
			inv.PhysicalDisks = append(inv.PhysicalDisks, *cur)
		}
		cur = nil
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		indent := len(l) - len(strings.TrimLeft(l, " "))
		if indent <= 8 && strings.HasSuffix(t, ":") && !strings.Contains(t, "Data:") && !strings.Contains(t, "NVMExpress") && !strings.Contains(t, "SATA") {
			// A new device entry.
			flush()
			cur = &models.PhysicalDisk{Name: strings.TrimSuffix(t, ":"), Health: "unknown", HealthSupported: false}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(t, "Capacity:"):
			cur.CapacityBytes = parseMacBytes(strings.TrimSpace(strings.TrimPrefix(t, "Capacity:")))
		case strings.HasPrefix(t, "Model:"):
			cur.Model = strings.TrimSpace(strings.TrimPrefix(t, "Model:"))
		case strings.HasPrefix(t, "Revision:"):
			cur.FirmwareVersion = strings.TrimSpace(strings.TrimPrefix(t, "Revision:"))
		case strings.HasPrefix(t, "Serial Number:"):
			cur.SerialNumber = strings.TrimSpace(strings.TrimPrefix(t, "Serial Number:"))
		case strings.HasPrefix(t, "Medium Type:"):
			cur.MediaType = strings.TrimSpace(strings.TrimPrefix(t, "Medium Type:"))
		case strings.HasPrefix(t, "S.M.A.R.T. status:"):
			st := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "S.M.A.R.T. status:")))
			cur.HealthSupported = true
			if strings.Contains(st, "verified") {
				cur.Health = "ok"
			} else if st != "" {
				cur.Health = "failing"
				cur.HealthDetail = "S.M.A.R.T. status: " + st
			}
		case strings.HasPrefix(t, "Detachable Drive:"):
			cur.BusType = "USB"
		}
	}
	flush()
	for i := range inv.PhysicalDisks {
		d := &inv.PhysicalDisks[i]
		if d.MediaType == "" {
			d.MediaType = "SSD"
		}
		if d.BusType == "" {
			d.BusType = "NVMe"
		}
	}
}

func collectMonitorsPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	lines := runCommand(ctx, []string{"system_profiler", "SPDisplaysDataType"})
	var cur *models.Monitor
	flush := func() {
		if cur != nil && (cur.WidthPx > 0 || cur.Name != "") {
			inv.Monitors = append(inv.Monitors, *cur)
		}
		cur = nil
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "Resolution:") {
			if cur == nil {
				cur = &models.Monitor{Primary: len(inv.Monitors) == 0}
			}
			res := strings.TrimSpace(strings.TrimPrefix(t, "Resolution:"))
			if m := regexp.MustCompile(`(\d+)\s*x\s*(\d+)`).FindStringSubmatch(res); len(m) == 3 {
				cur.WidthPx, _ = strconv.Atoi(m[1])
				cur.HeightPx, _ = strconv.Atoi(m[2])
			}
		} else if strings.HasPrefix(t, "Display Type:") && cur != nil {
			cur.Name = strings.TrimSpace(strings.TrimPrefix(t, "Display Type:"))
		} else if strings.HasPrefix(t, "Mirror") {
			flush()
		}
	}
	flush()
}

func collectAudioPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	lines := runCommand(ctx, []string{"system_profiler", "SPAudioDataType"})
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasSuffix(t, ":") && strings.Contains(l, "        ") && !strings.Contains(t, "Audio") {
			name := strings.TrimSuffix(t, ":")
			if name != "" {
				inv.Audio = append(inv.Audio, models.AudioDevice{Name: name, Kind: "unknown"})
			}
		}
	}
}

func collectUSBPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	lines := runCommand(ctx, []string{"system_profiler", "SPUSBDataType"})
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.Contains(t, "Product ID:") {
			name := ""
			if i := strings.LastIndex(l, "Product ID:"); i >= 0 {
				_ = i
			}
			inv.USB = append(inv.USB, models.USBDevice{Name: name})
		}
		if strings.HasSuffix(t, ":") && !strings.Contains(t, "USB") && len(l)-len(strings.TrimLeft(l, " ")) == 6 {
			name := strings.TrimSuffix(t, ":")
			if name != "" && !strings.Contains(name, "USB") {
				inv.USB = append(inv.USB, models.USBDevice{Name: name})
			}
		}
	}
	// Deduplicate by name.
	seen := map[string]bool{}
	var out []models.USBDevice
	for _, u := range inv.USB {
		if u.Name == "" || seen[u.Name] {
			continue
		}
		seen[u.Name] = true
		out = append(out, u)
	}
	inv.USB = out
}

func collectBluetoothPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	lines := runCommand(ctx, []string{"system_profiler", "SPBluetoothDataType"})
	var cur *models.BluetoothAdapter
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.Contains(t, "Bluetooth Controller:") || strings.Contains(t, "Controller:") {
			cur = &models.BluetoothAdapter{Name: "Bluetooth Controller", Manufacturer: "Apple"}
			inv.Bluetooth = append(inv.Bluetooth, *cur)
		}
		if cur != nil && strings.HasPrefix(t, "Address:") {
			inv.Bluetooth[len(inv.Bluetooth)-1].Address = strings.TrimSpace(strings.TrimPrefix(t, "Address:"))
		}
	}
}

func collectPrintersPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	out := runCommand(ctx, []string{"lpstat", "-p", "-d"})
	defaultName := ""
	for _, l := range out {
		if strings.HasPrefix(l, "system default destination:") {
			defaultName = strings.TrimSpace(strings.TrimPrefix(l, "system default destination:"))
		}
		if strings.HasPrefix(l, "printer ") {
			fields := strings.Fields(l)
			if len(fields) >= 2 {
				inv.Printers = append(inv.Printers, models.Printer{Name: fields[1], IsDefault: fields[1] == defaultName})
			}
		}
	}
}

func collectSoftwarePlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// /Applications bundle inventory.
	out := runCommand(ctx, []string{"system_profiler", "SPApplicationsDataType"})
	var cur *models.Application
	flush := func() {
		if cur != nil && cur.Name != "" {
			inv.Software = append(inv.Software, *cur)
		}
		cur = nil
	}
	for _, l := range out {
		indent := len(l) - len(strings.TrimLeft(l, " "))
		t := strings.TrimSpace(l)
		if indent == 4 && strings.HasSuffix(t, ":") {
			flush()
			cur = &models.Application{Name: strings.TrimSuffix(t, ":")}
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(t, "Version:"):
			cur.Version = strings.TrimSpace(strings.TrimPrefix(t, "Version:"))
		case strings.HasPrefix(t, "Obtained from:"):
			cur.Publisher = strings.TrimSpace(strings.TrimPrefix(t, "Obtained from:"))
		case strings.HasPrefix(t, "Last Modified:"):
			if d := strings.TrimSpace(strings.TrimPrefix(t, "Last Modified:")); d != "" {
				if tm, err := time.Parse("1/2/06, 3:04 PM", d); err == nil {
					cur.InstallDate = &tm
				}
			}
		}
	}
	flush()
}

func collectAdapterDetailPlatform(ctx context.Context, na *models.NetAdapter, ifName string) {
	// networksetup maps service names to devices; keep it simple and only
	// report the Wi-Fi SSID when the interface is wireless.
	if na.Kind == "wifi" {
		out := runCommand(ctx, []string{"networksetup", "-getairportnetwork", ifName})
		for _, l := range out {
			if i := strings.Index(l, "Current Wi-Fi Network:"); i >= 0 {
				na.SSID = strings.TrimSpace(l[i+len("Current Wi-Fi Network:"):])
			}
		}
	}
}

func systemDNSPlatform(ctx context.Context) []string {
	var out []string
	for _, l := range runCommand(ctx, []string{"scutil", "--dns"}) {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "nameserver[") {
			if i := strings.Index(t, ":"); i > 0 {
				out = append(out, strings.TrimSpace(t[i+1:]))
			}
		}
	}
	return out
}

func batteryStats(ctx context.Context) ([]batteryInfo, error) {
	out := runCommand(ctx, []string{"pmset", "-g", "batt"})
	if len(out) == 0 {
		return nil, fmt.Errorf("pmset unavailable")
	}
	b := batteryInfo{Name: "Internal Battery"}
	joined := strings.Join(out, " ")
	if m := regexp.MustCompile(`(\d+)%`).FindStringSubmatch(joined); len(m) == 2 {
		if n, err := strconv.ParseFloat(m[1], 64); err == nil {
			b.Percent = &n
		}
	}
	switch {
	case strings.Contains(joined, "charging"):
		b.Status = "charging"
		t := true
		b.PluggedIn = &t
	case strings.Contains(joined, "charged") || strings.Contains(joined, "AC attached"):
		b.Status = "full"
		t := true
		b.PluggedIn = &t
	case strings.Contains(joined, "discharging"):
		b.Status = "discharging"
		f := false
		b.PluggedIn = &f
	default:
		b.Status = "unknown"
	}
	// Health: Maximum Capacity from ioreg AppleSmartBattery.
	ioreg := strings.Join(runCommand(ctx, []string{"ioreg", "-r", "-c", "AppleSmartBattery"}), "\n")
	if m := regexp.MustCompile(`"DesignCapacity"\s*=\s*(\d+)`).FindStringSubmatch(ioreg); len(m) == 2 {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > 0 {
			b.DesignMWh = &n
		}
	}
	if m := regexp.MustCompile(`"AppleRawMaxCapacity"\s*=\s*(\d+)`).FindStringSubmatch(ioreg); len(m) == 2 {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > 0 {
			b.FullMWh = &n
		}
	}
	if m := regexp.MustCompile(`"CycleCount"\s*=\s*(\d+)`).FindStringSubmatch(ioreg); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			b.Cycles = &n
		}
	}
	return []batteryInfo{b}, nil
}

func gatewayCommand() []string { return []string{"netstat", "-rn"} }

var _ = exec.Command
