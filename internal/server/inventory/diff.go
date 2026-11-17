// Package inventory implements server-side inventory processing: change
// detection between snapshots and the hardware fingerprint hash.
package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

// Hash produces a stable fingerprint of hardware-relevant inventory fields.
// Volatile values (uptime, IPs, free space, software list) are excluded so a
// routine report never looks like a hardware change.
func Hash(inv models.Inventory) string {
	var b strings.Builder
	p := func(parts ...string) {
		for _, s := range parts {
			b.WriteString(strings.TrimSpace(s))
			b.WriteByte('\x1f')
		}
		b.WriteByte('\x1e')
	}
	p(inv.Host.Hostname, inv.Host.Manufacturer, inv.Host.Model, inv.Host.SerialNumber)
	p(inv.OS.Name, inv.OS.Edition, inv.OS.Version, inv.OS.Build, inv.OS.Arch)
	p(inv.CPU.Manufacturer, inv.CPU.Brand, inv.CPU.Architecture, fmt.Sprint(inv.CPU.PhysicalCores), fmt.Sprint(inv.CPU.LogicalCores))
	p(fmt.Sprint(inv.Memory.TotalBytes))
	mods := make([]string, 0, len(inv.Memory.Modules))
	for _, m := range inv.Memory.Modules {
		mods = append(mods, fmt.Sprintf("%s|%s|%s|%d|%s|%v", m.Slot, m.Manufacturer, m.PartNumber, m.CapacityBytes, m.Type, derefInt(m.SpeedMTs)))
	}
	sort.Strings(mods)
	p(mods...)
	gpus := make([]string, 0, len(inv.GPUs))
	for _, g := range inv.GPUs {
		gpus = append(gpus, fmt.Sprintf("%s|%s|%v|%s", g.Name, g.Manufacturer, derefU64(g.VRAMBytes), g.DriverVersion))
	}
	sort.Strings(gpus)
	p(gpus...)
	p(inv.Motherboard.Manufacturer, inv.Motherboard.Product, inv.Motherboard.BIOSVersion, inv.Motherboard.BIOSDate)
	disks := make([]string, 0, len(inv.PhysicalDisks))
	for _, d := range inv.PhysicalDisks {
		disks = append(disks, fmt.Sprintf("%s|%s|%s|%d|%s|%s", d.Name, d.Model, d.SerialNumber, d.CapacityBytes, d.MediaType, d.BusType))
	}
	sort.Strings(disks)
	p(disks...)
	vols := make([]string, 0, len(inv.Volumes))
	for _, v := range inv.Volumes {
		vols = append(vols, fmt.Sprintf("%s|%d|%s", v.MountPoint, v.TotalBytes, v.Filesystem))
	}
	sort.Strings(vols)
	p(vols...)
	nets := make([]string, 0, len(inv.NetAdapters))
	for _, n := range inv.NetAdapters {
		if n.Kind == "loopback" {
			continue
		}
		nets = append(nets, fmt.Sprintf("%s|%s|%s", n.Name, n.MAC, n.Kind))
	}
	sort.Strings(nets)
	p(nets...)
	mons := make([]string, 0, len(inv.Monitors))
	for _, m := range inv.Monitors {
		mons = append(mons, fmt.Sprintf("%s|%s|%s|%d|%d", m.Name, m.Manufacturer, m.Model, m.WidthPx, m.HeightPx))
	}
	sort.Strings(mons)
	p(mons...)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func derefInt(p *int) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}

func derefU64(p *uint64) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}

// Change is a single detected difference between two snapshots.
type Change struct {
	Category string `json:"category"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Details  string `json:"details,omitempty"`
}

// Diff compares two inventories and returns human-readable changes.
func Diff(old, new models.Inventory) []Change {
	var out []Change
	add := func(cat, sev, summary, details string) {
		out = append(out, Change{Category: cat, Severity: sev, Summary: summary, Details: details})
	}

	if old.CPU.Brand != new.CPU.Brand && new.CPU.Brand != "" {
		add("cpu", "warning", fmt.Sprintf("CPU changed from %q to %q", old.CPU.Brand, new.CPU.Brand), detail("old", old.CPU.Brand, "new", new.CPU.Brand))
	}
	if old.CPU.PhysicalCores != new.CPU.PhysicalCores && old.CPU.PhysicalCores != 0 && new.CPU.PhysicalCores != 0 {
		add("cpu", "info", fmt.Sprintf("CPU physical cores changed from %d to %d", old.CPU.PhysicalCores, new.CPU.PhysicalCores), "")
	}
	if old.CPU.LogicalCores != new.CPU.LogicalCores && old.CPU.LogicalCores != 0 && new.CPU.LogicalCores != 0 {
		add("cpu", "info", fmt.Sprintf("Logical processors changed from %d to %d", old.CPU.LogicalCores, new.CPU.LogicalCores), "")
	}

	oldRAM := totalRAM(old)
	newRAM := totalRAM(new)
	if oldRAM != newRAM && oldRAM != 0 && newRAM != 0 {
		sev := "info"
		verb := "changed"
		if newRAM > oldRAM {
			verb = "increased"
		} else {
			verb = "decreased"
			sev = "warning"
		}
		add("ram", sev, fmt.Sprintf("RAM %s from %s to %s", verb, humanBytes(oldRAM), humanBytes(newRAM)), detail("oldBytes", fmt.Sprint(oldRAM), "newBytes", fmt.Sprint(newRAM)))
	}
	if len(old.Memory.Modules) != len(new.Memory.Modules) && len(new.Memory.Modules) > 0 && len(old.Memory.Modules) > 0 {
		add("ram", "info", fmt.Sprintf("Memory module count changed from %d to %d", len(old.Memory.Modules), len(new.Memory.Modules)), "")
	}
	oldType, newType := firstRAMType(old), firstRAMType(new)
	if oldType != "" && newType != "" && oldType != newType {
		add("ram", "info", fmt.Sprintf("Memory type changed from %s to %s", oldType, newType), "")
	}

	// GPU set comparison
	oldGPUs := gpuSet(old)
	newGPUs := gpuSet(new)
	for name := range newGPUs {
		if !oldGPUs[name] {
			add("gpu", "warning", "GPU added: "+name, "")
		}
	}
	for name := range oldGPUs {
		if !newGPUs[name] {
			add("gpu", "warning", "GPU removed: "+name, "")
		}
	}

	// Disks by identity (serial, else model+capacity)
	oldDisks := diskMap(old)
	newDisks := diskMap(new)
	for k, d := range newDisks {
		od, ok := oldDisks[k]
		if !ok {
			add("storage", "info", fmt.Sprintf("New storage device detected: %s (%s)", diskLabel(d), humanBytes(d.CapacityBytes)), "")
			continue
		}
		if od.CapacityBytes != d.CapacityBytes {
			add("storage", "info", fmt.Sprintf("Storage device %s capacity changed from %s to %s", diskLabel(d), humanBytes(od.CapacityBytes), humanBytes(d.CapacityBytes)), "")
		}
		if od.Health != d.Health && d.Health != "" && d.Health != "unknown" && d.Health != od.Health {
			add("storage", "warning", fmt.Sprintf("Storage device %s health changed from %s to %s", diskLabel(d), od.Health, d.Health), "")
		}
	}
	for k, od := range oldDisks {
		if _, ok := newDisks[k]; !ok {
			add("storage", "warning", fmt.Sprintf("Storage device removed: %s (%s)", diskLabel(od), humanBytes(od.CapacityBytes)), "")
		}
	}
	// Volumes added/removed
	oldVols, newVols := volumeSet(old), volumeSet(new)
	for k := range newVols {
		if !oldVols[k] {
			add("storage", "info", "Volume added: "+k, "")
		}
	}
	for k := range oldVols {
		if !newVols[k] {
			add("storage", "warning", "Volume removed: "+k, "")
		}
	}

	// Network adapters
	oldNets, newNets := nicMap(old), nicMap(new)
	for mac, name := range newNets {
		if _, ok := oldNets[mac]; !ok && mac != "" {
			add("network", "info", fmt.Sprintf("Network adapter added: %s (%s)", name, mac), "")
		}
	}
	for mac, name := range oldNets {
		if _, ok := newNets[mac]; !ok && mac != "" {
			add("network", "warning", fmt.Sprintf("Network adapter removed: %s (%s)", name, mac), "")
		}
	}

	if old.OS.Version != new.OS.Version || old.OS.Build != new.OS.Build {
		if new.OS.Version != "" || new.OS.Build != "" {
			add("os", "info", fmt.Sprintf("Operating system changed from %s to %s", osLabel(old.OS), osLabel(new.OS)), "")
		}
	}
	if old.Host.Hostname != new.Host.Hostname && new.Host.Hostname != "" && old.Host.Hostname != "" {
		add("hostname", "info", fmt.Sprintf("Hostname changed from %s to %s", old.Host.Hostname, new.Host.Hostname), "")
	}
	oldMons, newMons := monitorSet(old), monitorSet(new)
	for k := range newMons {
		if !oldMons[k] {
			add("display", "info", "Monitor added or changed: "+k, "")
		}
	}
	for k := range oldMons {
		if !newMons[k] {
			add("display", "info", "Monitor removed or changed: "+k, "")
		}
	}
	if len(old.Software) > 0 || len(new.Software) > 0 {
		added, removed := softwareDiff(old.Software, new.Software)
		if added > 0 || removed > 0 {
			add("software", "info", fmt.Sprintf("Software inventory changed: %d added, %d removed", added, removed), "")
		}
	}
	return out
}

func detail(kv ...string) string {
	parts := make([]string, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, fmt.Sprintf("%q:%q", kv[i], kv[i+1]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func totalRAM(inv models.Inventory) uint64 {
	var t uint64
	for _, m := range inv.Memory.Modules {
		t += m.CapacityBytes
	}
	if t == 0 {
		t = inv.Memory.TotalBytes
	}
	return t
}

func firstRAMType(inv models.Inventory) string {
	for _, m := range inv.Memory.Modules {
		if m.Type != "" {
			return m.Type
		}
	}
	return ""
}

func gpuSet(inv models.Inventory) map[string]bool {
	m := map[string]bool{}
	for _, g := range inv.GPUs {
		if g.Name == "" {
			continue
		}
		m[g.Name] = true
	}
	return m
}

func diskKey(d models.PhysicalDisk) string {
	if d.SerialNumber != "" {
		return "sn:" + d.SerialNumber
	}
	return fmt.Sprintf("mn:%s|%s|%d", d.Model, d.Name, d.CapacityBytes)
}

func diskMap(inv models.Inventory) map[string]models.PhysicalDisk {
	m := map[string]models.PhysicalDisk{}
	for _, d := range inv.PhysicalDisks {
		m[diskKey(d)] = d
	}
	return m
}

func diskLabel(d models.PhysicalDisk) string {
	l := strings.TrimSpace(d.Manufacturer + " " + d.Model)
	if l == "" {
		l = d.Name
	}
	return strings.TrimSpace(l)
}

func volumeSet(inv models.Inventory) map[string]bool {
	m := map[string]bool{}
	for _, v := range inv.Volumes {
		m[v.MountPoint] = true
	}
	return m
}

func nicMap(inv models.Inventory) map[string]string {
	m := map[string]string{}
	for _, n := range inv.NetAdapters {
		if n.Kind == "loopback" {
			continue
		}
		m[strings.ToUpper(n.MAC)] = n.Name
	}
	return m
}

func monitorSet(inv models.Inventory) map[string]bool {
	m := map[string]bool{}
	for _, mo := range inv.Monitors {
		label := strings.TrimSpace(mo.Manufacturer + " " + mo.Model)
		if label == "" {
			label = mo.Name
		}
		if label != "" {
			m[fmt.Sprintf("%s %dx%d", label, mo.WidthPx, mo.HeightPx)] = true
		}
	}
	return m
}

func softwareDiff(old, new []models.Application) (added, removed int) {
	om := map[string]string{}
	for _, a := range old {
		om[strings.ToLower(a.Name)] = a.Version
	}
	nm := map[string]string{}
	for _, a := range new {
		nm[strings.ToLower(a.Name)] = a.Version
	}
	for k := range nm {
		if _, ok := om[k]; !ok {
			added++
		}
	}
	for k := range om {
		if _, ok := nm[k]; !ok {
			removed++
		}
	}
	return
}

func osLabel(os models.OperatingSystem) string {
	s := os.Name
	if os.Edition != "" {
		s += " " + os.Edition
	}
	if os.Version != "" {
		s += " " + os.Version
	}
	if os.Build != "" {
		s += " (build " + os.Build + ")"
	}
	return strings.TrimSpace(s)
}

// HumanBytes formats a byte count in binary units.
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
