// Package collect implements real hardware, OS and performance collection.
// Every value comes from the host operating system; values the platform does
// not expose are left empty and reported as unsupported.
package collect

import (
	"context"
	"fmt"
	"math"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/pcim/pc-inventory-manager/internal/agent/config"
	"github.com/pcim/pc-inventory-manager/internal/server/inventory"
	"github.com/pcim/pc-inventory-manager/internal/shared/models"
	"github.com/pcim/pc-inventory-manager/internal/shared/version"
)

// Collector gathers inventory and metrics on the current host.
type Collector struct {
	cfg config.Config

	mu          sync.Mutex
	lastNet     map[string]psnet.IOCountersStat
	lastNetAt   time.Time
	lastDiskIO  map[string]disk.IOCountersStat
	lastDiskAt  time.Time
	lastCPUTime float64
	lastCPUAt   time.Time
}

func New(cfg config.Config) *Collector {
	return &Collector{cfg: cfg}
}

// SetConfig updates collection preferences.
func (c *Collector) SetConfig(cfg config.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cfg = cfg
}

// Inventory collects a full hardware/OS snapshot.
func (c *Collector) Inventory(ctx context.Context) models.Inventory {
	c.mu.Lock()
	cfg := c.cfg
	c.mu.Unlock()

	inv := models.Inventory{CollectedAt: time.Now().UTC(), Host: models.Host{AgentVersion: version.Version}}
	errs := &inv.CollectionErrors
	addErr := func(cat string, err error) {
		if err != nil {
			*errs = append(*errs, models.CollectorError{Category: cat, Error: err.Error()})
		}
	}

	hostInfo, err := host.InfoWithContext(ctx)
	if err != nil {
		addErr("host", err)
	} else {
		inv.Host.Hostname = hostInfo.Hostname
		inv.Host.Platform = hostInfo.Platform
		inv.Host.PlatformFamily = hostInfo.PlatformFamily
		inv.Host.IsVirtual = strings.Contains(strings.ToLower(hostInfo.VirtualizationSystem), "vm") ||
			strings.Contains(strings.ToLower(hostInfo.VirtualizationRole), "guest") || hostInfo.VirtualizationSystem == "docker"
		if bt := time.Unix(int64(hostInfo.BootTime), 0); hostInfo.BootTime > 0 {
			inv.OS.BootTime = &bt
		}
		inv.OS.UptimeSeconds = hostInfo.Uptime
	}

	c.collectOS(ctx, &inv, addErr)
	c.collectCPU(ctx, &inv, addErr)
	c.collectMemory(ctx, &inv, addErr)
	c.collectGPUs(ctx, &inv, addErr)
	c.collectMotherboard(ctx, &inv, addErr)
	c.collectDisks(ctx, &inv, addErr)
	c.collectVolumes(ctx, &inv, addErr)
	c.collectNetwork(ctx, &inv, addErr)

	if cfg.CollectMonitors {
		collectMonitors(ctx, &inv, addErr)
	}
	collectBatteries(ctx, &inv, addErr)
	collectAudio(ctx, &inv, addErr)
	if cfg.CollectUSB {
		collectUSB(ctx, &inv, addErr)
	}
	collectBluetooth(ctx, &inv, addErr)
	collectOptical(ctx, &inv, addErr)
	if cfg.CollectPrinters {
		collectPrinters(ctx, &inv, addErr)
	}
	if cfg.CollectSoftware && cfg.CollectInstalledApps {
		collectSoftware(ctx, &inv, addErr)
	}

	if inv.OS.Arch == "" {
		inv.OS.Arch = runtime.GOARCH
	}
	inv.Hash = inventory.Hash(inv)
	return inv
}

func (c *Collector) collectOS(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	hostInfo, err := host.InfoWithContext(ctx)
	if err != nil {
		return
	}
	inv.OS.Name = hostInfo.Platform
	if hostInfo.Platform == "microsoft" || hostInfo.Platform == "windows" {
		inv.OS.Name = "Windows"
	}
	inv.OS.Version = hostInfo.PlatformVersion
	inv.OS.Kernel = hostInfo.KernelVersion
	inv.OS.Arch = hostInfo.KernelArch
	inv.Host.Platform = normalizePlatform(hostInfo.OS)
	// Edition/build and vendor details come from OS-specific collectors.
	collectOSDetail(ctx, inv, addErr)
	if tz, _ := time.Now().Zone(); tz != "" {
		inv.OS.Timezone = tz
	}
}

func normalizePlatform(os string) string {
	switch os {
	case "windows":
		return models.PlatformWindows
	case "darwin":
		return models.PlatformDarwin
	case "linux":
		return models.PlatformLinux
	}
	return models.PlatformUnknown
}

func (c *Collector) collectCPU(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		addErr("cpu", err)
	} else if len(infos) > 0 {
		// Group physical packages; report the primary package.
		sort.Slice(infos, func(i, j int) bool { return infos[i].CPU < infos[j].CPU })
		primary := infos[0]
		inv.CPU.Manufacturer = vendorFromID(primary.VendorID, primary.Flags)
		inv.CPU.Brand = strings.TrimSpace(primary.ModelName)
		inv.CPU.Family = primary.Family
		if primary.Mhz > 0 {
			mhz := math.Round(primary.Mhz*10) / 10
			inv.CPU.BaseFrequencyMHz = &mhz
		}
		if primary.CacheSize > 0 {
			cache := int(primary.CacheSize)
			inv.CPU.CacheKB = &cache
		}
		if len(primary.Flags) > 0 {
			// Keep a bounded, meaningful subset of capability flags.
			keep := []string{"sse4_1", "sse4_2", "avx", "avx2", "avx512f", "aes", "vmx", "svm", "hypervisor"}
			have := map[string]bool{}
			for _, f := range primary.Flags {
				have[f] = true
			}
			for _, k := range keep {
				if have[k] {
					inv.CPU.Capabilities = append(inv.CPU.Capabilities, k)
				}
			}
		}
		inv.CPU.Architecture = runtime.GOARCH
	}
	if counts, err := cpu.CountsWithContext(ctx, false); err == nil {
		inv.CPU.PhysicalCores = counts
	}
	if counts, err := cpu.CountsWithContext(ctx, true); err == nil {
		inv.CPU.LogicalCores = counts
	}
	if inv.CPU.LogicalCores == 0 {
		inv.CPU.LogicalCores = runtime.NumCPU()
	}
	if inv.CPU.PhysicalCores == 0 {
		inv.CPU.PhysicalCores = inv.CPU.LogicalCores
	}
	collectCPUDetail(ctx, inv, addErr)
}

func vendorFromID(id string, flags []string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "genuineintel", "intel":
		return "Intel"
	case "authenticamd", "amd":
		return "AMD"
	case "0x41", "arm":
		return "ARM"
	case "0x61":
		return "Apple"
	}
	if len(flags) > 0 {
		// Fall back to brand-string hints.
		return ""
	}
	return ""
}

func (c *Collector) collectMemory(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		addErr("memory", err)
		return
	}
	inv.Memory.TotalBytes = vm.Total
	inv.Memory.ModulesSupported = false
	collectMemoryModules(ctx, inv, addErr)
}

func (c *Collector) collectGPUs(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectGPUDetail(ctx, inv, addErr)
}

func (c *Collector) collectMotherboard(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectMotherboardDetail(ctx, inv, addErr)
}

func (c *Collector) collectDisks(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	collectPhysicalDisks(ctx, inv, addErr)
}

func (c *Collector) collectVolumes(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		addErr("volumes", err)
		return
	}
	seen := map[string]bool{}
	for _, p := range parts {
		if skipFilesystem(p.Fstype, p.Mountpoint) {
			continue
		}
		if seen[p.Mountpoint] {
			continue
		}
		seen[p.Mountpoint] = true
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		v := models.Volume{
			MountPoint: p.Mountpoint, Filesystem: p.Fstype, Device: p.Device,
			TotalBytes: usage.Total, UsedBytes: usage.Used, FreeBytes: usage.Free,
			UsagePercent: math.Round(usage.UsedPercent*10) / 10,
			IsRemovable: strings.Contains(strings.ToLower(strings.Join(p.Opts, ",")), "removable") || isRemovableMount(p.Mountpoint),
		}
		inv.Volumes = append(inv.Volumes, v)
	}
	sort.Slice(inv.Volumes, func(i, j int) bool { return inv.Volumes[i].MountPoint < inv.Volumes[j].MountPoint })
}

func skipFilesystem(fstype, mount string) bool {
	switch fstype {
	case "tmpfs", "devtmpfs", "devfs", "overlay", "squashfs", "proc", "sysfs", "cgroup", "cgroup2", "autofs", "map auto_home", "nullfs", "ramfs", "fusectl", "debugfs", "tracefs", "securityfs", "pstore", "bpf", "configfs", "mqueue", "hugetlbfs", "nsfs", "efivarfs":
		return true
	}
	if strings.HasPrefix(mount, "/snap/") || strings.HasPrefix(mount, "/sys/") || strings.HasPrefix(mount, "/proc/") || strings.HasPrefix(mount, "/dev/") {
		return true
	}
	if strings.HasPrefix(mount, "/run/") && fstype == "tmpfs" {
		return true
	}
	return false
}

func (c *Collector) collectNetwork(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	ifaces, err := psnet.InterfacesWithContext(ctx)
	if err != nil {
		addErr("network", err)
		return
	}
	gateways := defaultGateways(ctx)
	for _, ifc := range ifaces {
		na := models.NetAdapter{
			Name: ifc.Name, Description: ifc.Name, MAC: strings.ToUpper(ifc.HardwareAddr),
			IPv4: []string{}, IPv6: []string{}, DNS: []string{}, Status: "down",
		}
		for _, f := range ifc.Flags {
			if f == "up" {
				na.Status = "up"
			}
		}
		for _, a := range ifc.Addrs {
			ip, _, err := net.ParseCIDR(a.Addr)
			if err != nil {
				continue
			}
			if ip.IsLoopback() {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				na.IPv4 = append(na.IPv4, v4.String())
			} else {
				na.IPv6 = append(na.IPv6, ip.String())
			}
		}
		na.Kind = classifyInterface(ifc.Name, ifc.HardwareAddr)
		na.IsVirtual = isVirtualInterface(ifc.Name)
		if ifc.MTU > 0 {
			mtu := ifc.MTU
			na.MTU = &mtu
		}
		if gw, ok := gateways[ifc.Name]; ok {
			na.DefaultGateway = gw
		}
		collectAdapterDetail(ctx, &na, ifc.Name)
		inv.NetAdapters = append(inv.NetAdapters, na)
	}
	// DNS servers (system-wide)
	if dns := systemDNS(ctx); len(dns) > 0 {
		for i := range inv.NetAdapters {
			if inv.NetAdapters[i].Status == "up" && !inv.NetAdapters[i].IsVirtual {
				inv.NetAdapters[i].DNS = dns
			}
		}
	}
	// Sort: physical up interfaces first.
	sort.SliceStable(inv.NetAdapters, func(i, j int) bool {
		ai, aj := inv.NetAdapters[i], inv.NetAdapters[j]
		score := func(n models.NetAdapter) int {
			s := 0
			if n.Status == "up" {
				s += 4
			}
			if n.Kind == "ethernet" || n.Kind == "wifi" {
				s += 2
			}
			if n.IsVirtual {
				s--
			}
			if len(n.IPv4) > 0 {
				s++
			}
			return s
		}
		if si, sj := score(ai), score(aj); si != sj {
			return si > sj
		}
		return ai.Name < aj.Name
	})
}

const netFlagsUp = 0x1

func classifyInterface(name string, mac string) string {
	ln := strings.ToLower(name)
	switch {
	case strings.HasPrefix(ln, "lo") || len(mac) == 0 && (ln == "lo" || ln == "lo0"):
		return "loopback"
	case strings.Contains(ln, "wi-fi") || strings.Contains(ln, "wifi") || strings.Contains(ln, "wlan") || strings.HasPrefix(ln, "wl"):
		return "wifi"
	case strings.Contains(ln, "ethernet") || strings.HasPrefix(ln, "en") || strings.HasPrefix(ln, "eth") || strings.HasPrefix(ln, "lan"):
		return "ethernet"
	}
	if isVirtualInterface(name) {
		return "virtual"
	}
	if len(mac) > 0 {
		return "ethernet"
	}
	return "other"
}

func isVirtualInterface(name string) bool {
	ln := strings.ToLower(name)
	for _, p := range []string{"vethernet", "vmware", "virtualbox", "vbox", "hyper-v", "docker", "br-", "veth", "virbr", "tap", "tun", "wg", "zt", "tailscale", "utun", "bridge", "vnic", "ham", "awdl", "llw", "gif", "stf", "anpi", "p2p", "vboxnet", "npcap", "bluetooth"} {
		if strings.Contains(ln, p) {
			return true
		}
	}
	return false
}

// defaultGateways parses the OS routing table.
func defaultGateways(ctx context.Context) map[string]string {
	out := map[string]string{}
	lines := runCommand(ctx, gatewayCommand())
	for _, line := range lines {
		if gw, iface, ok := parseRouteLine(line); ok {
			out[iface] = gw
		}
	}
	return out
}

// Metrics collects a live performance sample.
func (c *Collector) Metrics(ctx context.Context) models.Metrics {
	c.mu.Lock()
	cfg := c.cfg
	c.mu.Unlock()

	m := models.Metrics{CollectedAt: time.Now().UTC(), IntervalSec: cfg.MetricsSec}
	if c.lastCPUAt.IsZero() {
		m.IntervalSec = 1
	}
	// CPU usage: gopsutil measures over the interval itself.
	if pcts, err := cpu.PercentWithContext(ctx, time.Second, false); err == nil && len(pcts) > 0 {
		m.CPU.UsagePercent = round1(pcts[0])
	}
	if cfg.CollectPerCoreCPU {
		perCore, _ := cpu.PercentWithContext(ctx, 0, true)
		if len(perCore) > 1 {
			for _, p := range perCore {
				m.CPU.PerCore = append(m.CPU.PerCore, round1(p))
			}
		}
	}
	if freqs, err := cpu.InfoWithContext(ctx); err == nil && len(freqs) > 0 && freqs[0].Mhz > 0 {
		f := round1(freqs[0].Mhz)
		m.CPU.FrequencyMHz = &f
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		m.Memory.TotalBytes = vm.Total
		m.Memory.UsedBytes = vm.Used
		m.Memory.AvailableBytes = vm.Available
		m.Memory.UsagePercent = round1(vm.UsedPercent)
	}
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.Memory.SwapTotalBytes = sw.Total
		m.Memory.SwapUsedBytes = sw.Used
	}
	if hi, err := host.InfoWithContext(ctx); err == nil {
		m.UptimeSeconds = hi.Uptime
	}
	if la, err := load.AvgWithContext(ctx); err == nil {
		m.Load = &models.LoadAvg{One: round2(la.Load1), Five: round2(la.Load5), Fifteen: round2(la.Load15)}
	}
	if cfg.CollectProcesses {
		if pids, err := process.PidsWithContext(ctx); err == nil {
			n := len(pids)
			m.Processes = &n
		}
	}

	// Disk throughput from counter deltas.
	c.mu.Lock()
	now := time.Now()
	curIO, _ := disk.IOCountersWithContext(ctx)
	if c.lastDiskIO != nil && !c.lastDiskAt.IsZero() {
		dt := now.Sub(c.lastDiskAt).Seconds()
		if dt > 0 {
			names := make([]string, 0, len(curIO))
			for name := range curIO {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				cur := curIO[name]
				prev, ok := c.lastDiskIO[name]
				if !ok {
					continue
				}
				dm := models.DiskMetrics{Name: name}
				dm.ReadBytesPerSec = round2(delta(cur.ReadBytes, prev.ReadBytes) / dt)
				dm.WriteBytesPerSec = round2(delta(cur.WriteBytes, prev.WriteBytes) / dt)
				if cur.IoTime > 0 || prev.IoTime > 0 {
					util := math.Min(100, delta(cur.IoTime, prev.IoTime)/dt)
					u := round1(util)
					dm.UtilizationPercent = &u
				}
				if cur.ReadCount > 0 || cur.WriteCount > 0 {
					iops := delta(cur.ReadCount+cur.WriteCount, prev.ReadCount+prev.WriteCount) / dt
					v := round1(iops)
					dm.IOPS = &v
				}
				m.Disks = append(m.Disks, dm)
			}
		}
	}
	c.lastDiskIO = curIO
	c.lastDiskAt = now

	// Network throughput from counter deltas.
	curNet, _ := psnet.IOCountersWithContext(ctx, true)
	if c.lastNet != nil && !c.lastNetAt.IsZero() {
		dt := now.Sub(c.lastNetAt).Seconds()
		if dt > 0 {
			byName := map[string]psnet.IOCountersStat{}
			for _, n := range curNet {
				byName[n.Name] = n
			}
			names := make([]string, 0, len(byName))
			for name := range byName {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				cur := byName[name]
				prev, ok := c.lastNet[name]
				if !ok {
					continue
				}
				nm := models.NetMetrics{Name: name, RxBytesTotal: cur.BytesRecv, TxBytesTotal: cur.BytesSent}
				nm.RxBytesPerSec = round2(delta(cur.BytesRecv, prev.BytesRecv) / dt)
				nm.TxBytesPerSec = round2(delta(cur.BytesSent, prev.BytesSent) / dt)
				m.Network = append(m.Network, nm)
			}
		}
	}
	c.lastNet = map[string]psnet.IOCountersStat{}
	for _, n := range curNet {
		c.lastNet[n.Name] = n
	}
	c.lastNetAt = now
	c.mu.Unlock()

	// Volumes
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		seen := map[string]bool{}
		for _, p := range parts {
			if skipFilesystem(p.Fstype, p.Mountpoint) || seen[p.Mountpoint] {
				continue
			}
			seen[p.Mountpoint] = true
			if u, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil {
				m.Volumes = append(m.Volumes, models.VolumeUsage{MountPoint: p.Mountpoint, TotalBytes: u.Total, FreeBytes: u.Free, UsagePercent: round1(u.UsedPercent)})
			}
		}
	}

	// Battery
	if bats, err := batteryStats(ctx); err == nil && len(bats) > 0 {
		m.Battery = &models.BatteryMetrics{Percent: bats[0].Percent, Status: bats[0].Status, PluggedIn: bats[0].PluggedIn}
	} else {
		m.Unsupported = append(m.Unsupported, "battery")
	}

	// Temperatures (real sensors only)
	if cfg.CollectTemperatures {
		if temps, err := sensors.SensorsTemperatures(); err == nil && len(temps) > 0 {
			for _, t := range temps {
				if t.Temperature <= 0 || t.Temperature > 150 {
					continue
				}
				m.Temperatures = append(m.Temperatures, models.Temperature{Sensor: t.SensorKey, Celsius: round1(t.Temperature), Source: "sensors"})
				if len(m.Temperatures) >= 12 {
					break
				}
			}
		}
		if len(m.Temperatures) == 0 {
			m.Unsupported = append(m.Unsupported, "temperature")
		}
	} else {
		m.Unsupported = append(m.Unsupported, "temperature")
	}
	if len(m.Disks) == 0 {
		m.Unsupported = append(m.Unsupported, "disk_io")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		m.Unsupported = append(m.Unsupported, "load_average")
	}
	return m
}

func delta(cur, prev uint64) float64 {
	if cur < prev { // counter reset
		return 0
	}
	return float64(cur - prev)
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
func round2(f float64) float64 { return math.Round(f*100) / 100 }

// Summary returns a compact human summary for logs.
func Summary(inv models.Inventory) string {
	return fmt.Sprintf("%s | %s %s | %s | %d cores/%d threads | %s RAM | %d disks | %d adapters",
		inv.Host.Hostname, inv.OS.Name, inv.OS.Version, inv.CPU.Brand, inv.CPU.PhysicalCores, inv.CPU.LogicalCores,
		humanBytes(inv.Memory.TotalBytes), len(inv.PhysicalDisks), len(inv.NetAdapters))
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
