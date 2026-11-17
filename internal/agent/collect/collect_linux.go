package collect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

// ---------------------------------------------------------------------------
// Linux: /sys, /proc, lsblk, dmidecode (optional), os-release
// ---------------------------------------------------------------------------

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func collectOSDetailPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	osr := readFile("/etc/os-release")
	kv := map[string]string{}
	for _, line := range strings.Split(osr, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = strings.Trim(v, `"`)
		}
	}
	if kv["NAME"] != "" {
		inv.OS.Name = kv["NAME"]
	}
	inv.OS.Edition = kv["VARIANT"] + prefixSpace(kv["VARIANT_ID"])
	if kv["VERSION_ID"] != "" {
		inv.OS.Version = kv["VERSION_ID"]
	}
	if v := readFile("/proc/sys/kernel/osrelease"); v != "" {
		inv.OS.Kernel = v
		inv.OS.Build = v
	}
	inv.OS.Arch = readFile("/proc/sys/kernel/arch")
	// Package manager detection
	for _, pm := range []struct{ path, name string }{
		{"/usr/bin/apt", "apt"}, {"/usr/bin/dnf", "dnf"}, {"/usr/bin/pacman", "pacman"},
		{"/usr/bin/zypper", "zypper"}, {"/usr/bin/apk", "apk"}, {"/usr/bin/emerge", "portage"},
		{"/usr/bin/xbps-install", "xbps"}, {"/usr/bin/snap", "snap"}, {"/usr/bin/flatpak", "flatpak"},
	} {
		if _, err := os.Stat(pm.path); err == nil {
			if inv.OS.PackageManager == "" {
				inv.OS.PackageManager = pm.name
			}
		}
	}
	systemd := false
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		systemd = true
	}
	inv.OS.Systemd = &systemd
	// Install date from the root filesystem creation time.
	if fi, err := os.Stat("/etc"); err == nil {
		// Not exact; the root filesystem birth time is a better proxy.
		_ = fi
	}
	if out := runCommand(ctx, []string{"stat", "-c", "%w", "/"}); len(out) > 0 {
		if t, err := time.Parse("2006-01-02 15:04:05.000000000 -0700", strings.TrimSpace(out[0])); err == nil {
			inv.OS.InstallDate = &t
		}
	}
	inv.Host.Model = readFile("/sys/devices/virtual/dmi/id/product_name")
	inv.Host.Manufacturer = readFile("/sys/devices/virtual/dmi/id/sys_vendor")
	inv.Host.SerialNumber = readFile("/sys/devices/virtual/dmi/id/product_serial")
	inv.Host.ChassisType = chassisTypeName(readFile("/sys/devices/virtual/dmi/id/chassis_type"))
	if tz, err := os.Readlink("/etc/localtime"); err == nil {
		inv.OS.Timezone = strings.TrimPrefix(tz, "/usr/share/zoneinfo/")
	}
}

func prefixSpace(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

func chassisTypeName(v string) string {
	n, _ := strconv.Atoi(v)
	switch n {
	case 3, 4, 5, 6, 7, 15, 16:
		return "Desktop"
	case 8, 9, 10, 11, 12, 14, 18, 21, 30, 31, 32:
		return "Laptop"
	case 17:
		return "Server"
	case 23:
		return "Rack"
	case 35:
		return "Mini PC"
	}
	return ""
}

func collectMemoryModulesPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// dmidecode requires root. Only report module details when it works.
	out := runCommand(ctx, []string{"dmidecode", "-t", "memory"})
	if len(out) == 0 {
		inv.Memory.ModulesSupported = false
		return
	}
	var cur *models.MemoryModule
	flush := func() {
		if cur != nil && cur.CapacityBytes > 0 {
			inv.Memory.Modules = append(inv.Memory.Modules, *cur)
		}
		cur = nil
	}
	for _, l := range out {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "Memory Device"):
			flush()
			cur = &models.MemoryModule{}
		case cur == nil:
			continue
		case strings.HasPrefix(t, "Locator:"):
			cur.Slot = strings.TrimSpace(strings.TrimPrefix(t, "Locator:"))
		case strings.HasPrefix(t, "Size:"):
			if f := strings.Fields(strings.TrimPrefix(t, "Size:")); len(f) >= 2 {
				if v, err := strconv.ParseUint(f[0], 10, 64); err == nil {
					cur.CapacityBytes = v * parseSizeUnit(f[1])
				}
			}
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
		case strings.HasPrefix(t, "Serial Number:"):
			cur.SerialNumber = strings.TrimSpace(strings.TrimPrefix(t, "Serial Number:"))
		case strings.HasPrefix(t, "Part Number:"):
			cur.PartNumber = strings.TrimSpace(strings.TrimPrefix(t, "Part Number:"))
		}
	}
	flush()
	inv.Memory.ModulesSupported = len(inv.Memory.Modules) > 0
}

func parseSizeUnit(u string) uint64 {
	switch strings.ToUpper(u) {
	case "KB":
		return 1024
	case "MB":
		return 1024 * 1024
	case "GB":
		return 1024 * 1024 * 1024
	case "TB":
		return 1024 * 1024 * 1024 * 1024
	}
	return 1
}

func collectGPUDetailPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// /sys/class/drm/card*/device is the canonical source; vendor files give
	// the name and driver.
	entries, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device")
	seen := map[string]bool{}
	for _, dev := range entries {
		vendor := readFile(filepath.Join(dev, "vendor"))
		device := readFile(filepath.Join(dev, "device"))
		name := gpuNameFromPCI(vendor, device)
		if name == "" {
			name = "GPU " + strings.TrimPrefix(filepath.Base(filepath.Dir(dev)), "card")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		g := models.GPU{Name: name, Manufacturer: pciVendorName(vendor)}
		// VRAM from sysfs (bytes) where exposed.
		if v := readFile(filepath.Join(dev, "mem_info_vram_total")); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil && n > 0 {
				g.VRAMBytes = &n
			}
		}
		// Driver version.
		if drv := readFile(filepath.Join(dev, "driver/module/version")); drv != "" {
			g.DriverVersion = drv
		}
		g.Kind = "unknown"
		ln := strings.ToLower(name)
		for _, k := range []string{"uhd", "hd graphics", "iris", "radeon vega", "radeon graphics", "integrated", "llvmpipe"} {
			if strings.Contains(ln, k) {
				g.Kind = "integrated"
			}
		}
		for _, k := range []string{"geforce", "radeon rx", "quadro", "rtx", "gtx", "arc a", "arc b"} {
			if strings.Contains(ln, k) {
				g.Kind = "dedicated"
			}
		}
		inv.GPUs = append(inv.GPUs, g)
	}
	if len(inv.GPUs) == 0 {
		// Fallback: lspci.
		for _, l := range runCommand(ctx, []string{"lspci"}) {
			if strings.Contains(l, "VGA compatible controller") || strings.Contains(l, "3D controller") || strings.Contains(l, "Display controller") {
				if i := strings.Index(l, ": "); i > 0 {
					name := strings.TrimSpace(l[i+2:])
					inv.GPUs = append(inv.GPUs, models.GPU{Name: name, Manufacturer: pciVendorFromText(name), Kind: "unknown"})
				}
			}
		}
	}
}

func pciVendorName(id string) string {
	switch strings.TrimPrefix(strings.ToLower(id), "0x") {
	case "10de":
		return "NVIDIA"
	case "1002":
		return "AMD"
	case "8086":
		return "Intel"
	case "1af4":
		return "Red Hat (virtio)"
	case "15ad":
		return "VMware"
	case "1b36":
		return "Red Hat (QXL)"
	case "1234":
		return "QEMU"
	}
	return ""
}

func pciVendorFromText(name string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.Contains(ln, "nvidia"):
		return "NVIDIA"
	case strings.Contains(ln, "amd"), strings.Contains(ln, "ati"), strings.Contains(ln, "radeon"):
		return "AMD"
	case strings.Contains(ln, "intel"):
		return "Intel"
	}
	return ""
}

var gpuNames = map[string]string{
	"8086:46a6": "Intel Iris Xe Graphics", "8086:9a49": "Intel Iris Xe Graphics", "8086:3e9b": "Intel UHD Graphics 630",
	"8086:4680": "Intel UHD Graphics 770", "8086:a780": "Intel UHD Graphics", "1002:1638": "AMD Radeon Graphics",
	"10de:2489": "NVIDIA GeForce RTX 3060 Ti", "10de:2504": "NVIDIA GeForce RTX 3060", "10de:2204": "NVIDIA GeForce RTX 3090",
}

func gpuNameFromPCI(vendor, device string) string {
	key := strings.ToLower(strings.TrimPrefix(vendor, "0x")) + ":" + strings.ToLower(strings.TrimPrefix(device, "0x"))
	if n, ok := gpuNames[key]; ok {
		return n
	}
	return ""
}

func collectMotherboardDetailPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	inv.Motherboard.Manufacturer = readFile("/sys/devices/virtual/dmi/id/board_vendor")
	inv.Motherboard.Product = readFile("/sys/devices/virtual/dmi/id/board_name")
	inv.Motherboard.Version = readFile("/sys/devices/virtual/dmi/id/board_version")
	inv.Motherboard.SerialNumber = readFile("/sys/devices/virtual/dmi/id/board_serial")
	inv.Motherboard.BIOSVendor = readFile("/sys/devices/virtual/dmi/id/bios_vendor")
	inv.Motherboard.BIOSVersion = readFile("/sys/devices/virtual/dmi/id/bios_version")
	inv.Motherboard.BIOSDate = readFile("/sys/devices/virtual/dmi/id/bios_date")
	inv.Motherboard.SystemManufacturer = inv.Host.Manufacturer
	inv.Motherboard.SystemProduct = inv.Host.Model
	inv.Motherboard.SystemSerial = inv.Host.SerialNumber
}

func collectPhysicalDisksPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// lsblk gives model/serial/size/rota (HDD=1). SMART data needs root.
	out := runCommand(ctx, []string{"lsblk", "-b", "-d", "-o", "NAME,MODEL,SERIAL,SIZE,ROTA,TYPE,TRAN", "-P", "-e", "7,11"})
	for _, line := range out {
		kv := parseLSBLK(line)
		name := kv["NAME"]
		if name == "" || kv["TYPE"] != "disk" {
			continue
		}
		size, _ := strconv.ParseUint(kv["SIZE"], 10, 64)
		d := models.PhysicalDisk{Name: "/dev/" + name, Model: strings.TrimSpace(kv["MODEL"]), SerialNumber: strings.TrimSpace(kv["SERIAL"]),
			CapacityBytes: size, Health: "unknown", BusType: kv["TRAN"]}
		if kv["ROTA"] == "1" {
			d.MediaType = "HDD"
		} else if strings.EqualFold(kv["TRAN"], "nvme") {
			d.MediaType = "NVMe"
		} else {
			d.MediaType = "SSD"
		}
		inv.PhysicalDisks = append(inv.PhysicalDisks, d)
	}
	// SMART health via smartctl when available and permitted.
	for i := range inv.PhysicalDisks {
		dev := inv.PhysicalDisks[i].Name
		res := runCommand(ctx, []string{"smartctl", "-H", "-A", "-j", dev})
		if len(res) == 0 {
			continue
		}
		joined := strings.Join(res, "\n")
		inv.PhysicalDisks[i].HealthSupported = true
		if strings.Contains(joined, `"passed":true`) || strings.Contains(joined, "PASSED") {
			inv.PhysicalDisks[i].Health = "ok"
		} else if strings.Contains(joined, `"passed":false`) || strings.Contains(joined, "FAILED") {
			inv.PhysicalDisks[i].Health = "failing"
		}
		if m := regexp.MustCompile(`"temperature":\{"current":(\d+)`).FindStringSubmatch(joined); len(m) == 2 {
			if t, err := strconv.Atoi(m[1]); err == nil {
				inv.PhysicalDisks[i].TemperatureC = &t
			}
		}
		if m := regexp.MustCompile(`"power_on_time":\{"hours":(\d+)`).FindStringSubmatch(joined); len(m) == 2 {
			if h, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				inv.PhysicalDisks[i].PowerOnHours = &h
			}
		}
	}
}

func parseLSBLK(line string) map[string]string {
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`([A-Z:]+)="([^"]*)"`).FindAllStringSubmatch(line, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func collectMonitorsPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// /sys/class/drm/card*-*/modes lists supported modes; the connected flag
	// shows which outputs have a display attached.
	entries, _ := filepath.Glob("/sys/class/drm/card*-*")
	for _, e := range entries {
		status := readFile(filepath.Join(e, "status"))
		if status != "connected" {
			continue
		}
		mon := models.Monitor{Name: filepath.Base(e), Primary: len(inv.Monitors) == 0}
		if modes := readFile(filepath.Join(e, "modes")); modes != "" {
			first := strings.Split(modes, "\n")[0]
			if m := regexp.MustCompile(`^(\d+)x(\d+)`).FindStringSubmatch(first); len(m) == 3 {
				mon.WidthPx, _ = strconv.Atoi(m[1])
				mon.HeightPx, _ = strconv.Atoi(m[2])
			}
		}
		// EDID parsing for manufacturer/model would need the binary blob;
		// only report the output name and resolution here.
		inv.Monitors = append(inv.Monitors, mon)
	}
}

func collectAudioPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	out := runCommand(ctx, []string{"aplay", "-l"})
	for _, l := range out {
		if strings.Contains(l, "card ") && strings.Contains(l, "device ") {
			name := l
			if i := strings.Index(l, ": "); i >= 0 {
				name = strings.TrimSpace(l[i+2:])
			}
			inv.Audio = append(inv.Audio, models.AudioDevice{Name: name, Kind: "output"})
		}
	}
	if len(inv.Audio) == 0 {
		for _, l := range runCommand(ctx, []string{"lspci"}) {
			if strings.Contains(strings.ToLower(l), "audio device") {
				if i := strings.Index(l, ": "); i > 0 {
					inv.Audio = append(inv.Audio, models.AudioDevice{Name: strings.TrimSpace(l[i+2:])})
				}
			}
		}
	}
}

func collectUSBPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	base := "/sys/bus/usb/devices"
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		dir := filepath.Join(base, e.Name())
		if !strings.HasPrefix(e.Name(), "usb") {
			continue
		}
		name := readFile(filepath.Join(dir, "product"))
		if name == "" {
			continue
		}
		u := models.USBDevice{Name: name, Manufacturer: readFile(filepath.Join(dir, "manufacturer")),
			VendorID: strings.ToUpper(readFile(filepath.Join(dir, "idVendor"))), ProductID: strings.ToUpper(readFile(filepath.Join(dir, "idProduct"))),
			SerialNumber: readFile(filepath.Join(dir, "serial"))}
		if b := readFile(filepath.Join(dir, "bDeviceClass")); b != "" {
			u.Class = b
		}
		inv.USB = append(inv.USB, u)
	}
	sort.Slice(inv.USB, func(i, j int) bool { return inv.USB[i].Name < inv.USB[j].Name })
}

func collectBluetoothPlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	base := "/sys/class/bluetooth"
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		addr := readFile(filepath.Join(base, e.Name(), "address"))
		name := readFile(filepath.Join(base, e.Name(), "name"))
		if name == "" {
			name = e.Name()
		}
		inv.Bluetooth = append(inv.Bluetooth, models.BluetoothAdapter{Name: name, Address: strings.ToUpper(addr)})
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
			f := strings.Fields(l)
			if len(f) >= 2 {
				inv.Printers = append(inv.Printers, models.Printer{Name: f[1], IsDefault: f[1] == defaultName})
			}
		}
	}
}

func collectSoftwarePlatform(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	var cmd []string
	switch {
	case fileExists("/var/lib/dpkg/status"):
		cmd = []string{"dpkg-query", "-W", "-f", "${Package}\t${Version}\t${Maintainer}\t${Installed-Size}\n"}
	case fileExists("/usr/bin/rpm"):
		cmd = []string{"rpm", "-qa", "--qf", "%{NAME}\t%{VERSION}-%{RELEASE}\t%{VENDOR}\t%{SIZE}\n"}
	case fileExists("/usr/bin/pacman"):
		cmd = []string{"pacman", "-Q"}
	case fileExists("/sbin/apk") || fileExists("/usr/sbin/apk"):
		cmd = []string{"apk", "info", "-v"}
	}
	if cmd == nil {
		addErr("software", fmt.Errorf("no supported package database found"))
		return
	}
	for _, l := range runCommand(ctx, cmd) {
		fields := strings.Split(l, "\t")
		if len(fields) < 2 {
			continue
		}
		a := models.Application{Name: fields[0], Version: fields[1]}
		if len(fields) > 2 {
			a.Publisher = fields[2]
		}
		if len(fields) > 3 {
			if n, err := strconv.ParseUint(fields[3], 10, 64); err == nil {
				a.EstimatedSizeKB = &n
			}
		}
		inv.Software = append(inv.Software, a)
	}
	// Also list snap/flatpak apps when present.
	if fileExists("/usr/bin/flatpak") {
		for _, l := range runCommand(ctx, []string{"flatpak", "list", "--columns=application,version,origin"}) {
			f := strings.Fields(l)
			if len(f) >= 2 {
				inv.Software = append(inv.Software, models.Application{Name: f[0], Version: f[1], Publisher: "flatpak"})
			}
		}
	}
	sort.Slice(inv.Software, func(i, j int) bool { return strings.ToLower(inv.Software[i].Name) < strings.ToLower(inv.Software[j].Name) })
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func collectAdapterDetailPlatform(ctx context.Context, na *models.NetAdapter, ifName string) {
	if na.MTU != nil {
		// Already set from gopsutil.
	}
	// Link speed from /sys/class/net/<if>/speed (Mbps).
	if v := readFile("/sys/class/net/" + ifName + "/speed"); v != "" && v != "-1" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			na.SpeedMbps = &n
		}
	}
	if na.Kind == "wifi" {
		// SSID without any credentials.
		out := runCommand(ctx, []string{"iwgetid", "-r", ifName})
		if len(out) > 0 {
			na.SSID = strings.TrimSpace(out[0])
		}
	}
}

func systemDNSPlatform(ctx context.Context) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range runCommand(ctx, []string{"cat", "/etc/resolv.conf"}) {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] == "nameserver" && !seen[f[1]] {
			seen[f[1]] = true
			out = append(out, f[1])
		}
	}
	return out
}

func batteryStats(ctx context.Context) ([]batteryInfo, error) {
	entries, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	if len(entries) == 0 {
		return nil, fmt.Errorf("no battery")
	}
	var out []batteryInfo
	for _, e := range entries {
		b := batteryInfo{Name: filepath.Base(e)}
		if v := readFile(filepath.Join(e, "capacity")); v != "" {
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				b.Percent = &n
			}
		}
		b.Status = readFile(filepath.Join(e, "status"))
		plugged := false
		switch b.Status {
		case "Charging", "Full":
			plugged = true
		}
		b.PluggedIn = &plugged
		// Energy values are in µWh; charge values in µAh need voltage scaling.
		if d := readFile(filepath.Join(e, "energy_full_design")); d != "" {
			if n, err := strconv.ParseInt(d, 10, 64); err == nil {
				b.DesignMWh = &n
			}
		}
		if f := readFile(filepath.Join(e, "energy_full")); f != "" {
			if n, err := strconv.ParseInt(f, 10, 64); err == nil {
				b.FullMWh = &n
			}
		}
		if b.DesignMWh == nil {
			// Fall back to charge counters scaled by voltage.
			volt := int64(0)
			if v := readFile(filepath.Join(e, "voltage_min_design")); v != "" {
				volt, _ = strconv.ParseInt(v, 10, 64)
			}
			if volt > 0 {
				if d := readFile(filepath.Join(e, "charge_full_design")); d != "" {
					if n, err := strconv.ParseInt(d, 10, 64); err == nil {
						mwh := n * volt / 1000000
						b.DesignMWh = &mwh
					}
				}
				if f := readFile(filepath.Join(e, "charge_full")); f != "" {
					if n, err := strconv.ParseInt(f, 10, 64); err == nil {
						mwh := n * volt / 1000000
						b.FullMWh = &mwh
					}
				}
			}
		}
		if c := readFile(filepath.Join(e, "cycle_count")); c != "" {
			if n, err := strconv.Atoi(c); err == nil {
				b.Cycles = &n
			}
		}
		out = append(out, b)
	}
	return out, nil
}

func gatewayCommand() []string { return []string{"ip", "route", "show", "default"} }
