package collect

import (
	"context"
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"github.com/pcim/pc-inventory-manager/internal/shared/models"
)

// ---------------------------------------------------------------------------
// Windows: PowerShell + CIM/WMI, registry, Get-PhysicalDisk, Get-CimInstance
// ---------------------------------------------------------------------------

func psRun(ctx context.Context, script string) (string, error) {
	full := "$ErrorActionPreference='SilentlyContinue';$ProgressPreference='SilentlyContinue';" + script
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", full)
	hideCmd(cmd)
	out, err := cmd.Output()
	return string(out), err
}

// psCSV runs a script that ends in ConvertTo-Csv and returns parsed rows.
func psCSV(ctx context.Context, selectCmd string) ([]map[string]string, error) {
	// A pipeline left bare at the end of the script leaks its CSV into stdout
	// before ConvertTo-Csv sees it; wrap it so only converted output is emitted.
	out, err := psRun(ctx, "@("+selectCmd+") | ConvertTo-Csv -NoTypeInformation")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(out)))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, fmt.Errorf("no data")
	}
	header := records[0]
	var rows []map[string]string
	for _, rec := range records[1:] {
		if len(rec) != len(header) {
			continue
		}
		m := map[string]string{}
		for i, h := range header {
			m[strings.TrimSpace(h)] = strings.TrimSpace(rec[i])
		}
		rows = append(rows, m)
	}
	return rows, nil
}

func regGet(ctx context.Context, key, name string) string {
	out, err := psRun(ctx, fmt.Sprintf(`(Get-ItemProperty -LiteralPath '%s' -Name '%s').'%s'`, key, name, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func collectOSDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	// Product name / edition / build from the registry (most reliable).
	inv.OS.Name = "Windows"
	if v := regGet(ctx, `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "ProductName"); v != "" {
		inv.OS.Name = v
	}
	if v := regGet(ctx, `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "EditionID"); v != "" {
		inv.OS.Edition = v
	}
	displayVersion := regGet(ctx, `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "DisplayVersion")
	currentBuild := regGet(ctx, `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "CurrentBuild")
	ubr := regGet(ctx, `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "UBR")
	if displayVersion != "" {
		inv.OS.Version = displayVersion
	}
	if currentBuild != "" {
		inv.OS.Build = currentBuild
		if ubr != "" {
			inv.OS.Build += "." + ubr
		}
	}
	// Install date (unix seconds in registry).
	if v := regGet(ctx, `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion`, "InstallDate"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			t := time.Unix(n, 0).UTC()
			inv.OS.InstallDate = &t
		}
	}
	if v := regGet(ctx, `HKLM:\SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, "TimeZoneKeyName"); v != "" {
		inv.OS.Timezone = v
	}
	// System model/manufacturer
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_ComputerSystem | Select-Object Manufacturer,Model,SystemType,Domain,PartOfDomain,UserName"); err == nil && len(rows) > 0 {
		r := rows[0]
		inv.Host.Manufacturer = r["Manufacturer"]
		inv.Host.Model = r["Model"]
		inv.Motherboard.SystemManufacturer = r["Manufacturer"]
		inv.Motherboard.SystemProduct = r["Model"]
		inv.Host.Domain = r["Domain"]
	}
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_ComputerSystemProduct | Select-Object IdentifyingNumber,Name,Vendor"); err == nil && len(rows) > 0 {
		inv.Host.SerialNumber = rows[0]["IdentifyingNumber"]
		inv.Motherboard.SystemSerial = rows[0]["IdentifyingNumber"]
	}
	// ChassisTypes is a uint16 array; flatten it to a plain integer so the CSV
	// column carries the code rather than the type name.
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_SystemEnclosure | Select-Object @{n='ChassisType';e={$_.ChassisTypes -join ';'}}"); err == nil && len(rows) > 0 {
		ct := strings.Trim(strings.TrimSpace(rows[0]["ChassisType"]), "{}\"")
		inv.Host.ChassisType = chassisName(ct)
	}
	// Activation state via the software licensing service (supported API).
	if out, err := psRun(ctx, "(Get-CimInstance SoftwareLicensingProduct -Filter \"PartialProductKey IS NOT NULL AND Name LIKE 'Windows%'\" | Select-Object -First 1).LicenseStatus"); err == nil {
		switch strings.TrimSpace(out) {
		case "1":
			inv.OS.ActivationState = "Licensed"
		case "0":
			inv.OS.ActivationState = "Unlicensed"
		case "2":
			inv.OS.ActivationState = "OOB Grace"
		case "3":
			inv.OS.ActivationState = "OOT Grace"
		case "5":
			inv.OS.ActivationState = "Notification"
		default:
			if strings.TrimSpace(out) != "" {
				inv.OS.ActivationState = "Status " + strings.TrimSpace(out)
			}
		}
	}
	// Windows Defender status (only where the cmdlet exists and is permitted).
	if rows, err := psCSV(ctx, "Get-MpComputerStatus | Select-Object AntivirusEnabled,RealTimeProtectionEnabled,AntivirusSignatureAge"); err == nil && len(rows) > 0 {
		r := rows[0]
		if v := r["AntivirusEnabled"]; v != "" {
			b := strings.EqualFold(v, "True")
			inv.OS.DefenderEnabled = &b
		}
		if v := r["AntivirusSignatureAge"]; v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				inv.OS.DefenderSignatureAgeDays = &n
			}
		}
	}
	// Recent hotfixes (bounded).
	if rows, err := psCSV(ctx, "Get-HotFix | Sort-Object InstalledOn -Descending | Select-Object -First 12 HotFixID,Description,InstalledOn"); err == nil {
		for _, r := range rows {
			upd := models.InstalledUpdate{ID: r["HotFixID"], Description: r["Description"]}
			if t, err := parseWinDate(r["InstalledOn"]); err == nil {
				upd.InstalledAt = &t
			}
			if upd.ID != "" {
				inv.OS.InstalledUpdates = append(inv.OS.InstalledUpdates, upd)
			}
		}
	}
}

func parseWinDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	for _, layout := range []string{"01/02/2006", "1/2/2006", "2006-01-02", "01/02/2006 15:04:05", "1/2/2006 3:04:05 PM"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparsed date %q", s)
}

func chassisName(code string) string {
	n, _ := strconv.Atoi(strings.TrimSpace(code))
	switch n {
	case 3, 4, 5, 6, 7, 15, 16:
		return "Desktop"
	case 8, 9, 10, 11, 12, 14, 18, 21, 30, 31, 32:
		return "Laptop"
	case 17:
		return "Server"
	case 23:
		return "Rack"
	case 13:
		return "All-in-One"
	case 35:
		return "Mini PC"
	}
	if code != "" && code != "0" {
		return "Chassis type " + code
	}
	return ""
}

func collectCPUDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_Processor | Select-Object Name,Manufacturer,NumberOfCores,NumberOfLogicalProcessors,MaxClockSpeed,CurrentClockSpeed,L2CacheSize,L3CacheSize,SocketDesignation,ProcessorId,Architecture"); err == nil && len(rows) > 0 {
		r := rows[0]
		if inv.CPU.Brand == "" {
			inv.CPU.Brand = r["Name"]
		}
		if inv.CPU.Manufacturer == "" {
			inv.CPU.Manufacturer = r["Manufacturer"]
		}
		if n, err := strconv.Atoi(r["NumberOfCores"]); err == nil && n > 0 {
			inv.CPU.PhysicalCores = n
		}
		if n, err := strconv.Atoi(r["NumberOfLogicalProcessors"]); err == nil && n > 0 {
			inv.CPU.LogicalCores = n
		}
		if n, err := strconv.Atoi(r["MaxClockSpeed"]); err == nil && n > 0 {
			f := float64(n)
			inv.CPU.MaxFrequencyMHz = &f
		}
		if inv.CPU.BaseFrequencyMHz == nil {
			if n, err := strconv.Atoi(r["CurrentClockSpeed"]); err == nil && n > 0 {
				f := float64(n)
				inv.CPU.BaseFrequencyMHz = &f
			}
		}
		if inv.CPU.Socket == "" {
			inv.CPU.Socket = r["SocketDesignation"]
		}
		if inv.CPU.CacheKB == nil {
			l2, _ := strconv.Atoi(r["L2CacheSize"])
			l3, _ := strconv.Atoi(r["L3CacheSize"])
			if l3 > 0 {
				c := l3
				inv.CPU.CacheKB = &c
			} else if l2 > 0 {
				c := l2
				inv.CPU.CacheKB = &c
			}
		}
	}
	if inv.CPU.Architecture == "" {
		inv.CPU.Architecture = "x86_64"
	}
}

func collectMemoryModules(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-CimInstance Win32_PhysicalMemory | Select-Object DeviceLocator,BankLabel,Manufacturer,PartNumber,SerialNumber,Capacity,Speed,ConfiguredClockSpeed,SMBIOSMemoryType,MemoryType,FormFactor")
	if err != nil {
		addErr("memory_modules", err)
		return
	}
	for _, r := range rows {
		cap, _ := strconv.ParseUint(r["Capacity"], 10, 64)
		m := models.MemoryModule{
			Slot: r["DeviceLocator"], Manufacturer: r["Manufacturer"], PartNumber: r["PartNumber"],
			SerialNumber: r["SerialNumber"], CapacityBytes: cap, Type: smbiosMemoryType(r["SMBIOSMemoryType"]),
			FormFactor: memoryFormFactor(r["FormFactor"]),
		}
		speed := r["ConfiguredClockSpeed"]
		if speed == "" || speed == "0" {
			speed = r["Speed"]
		}
		if n, err := strconv.Atoi(speed); err == nil && n > 0 {
			m.SpeedMTs = &n
		}
		if m.Type == "" {
			m.Type = legacyMemoryType(r["MemoryType"])
		}
		inv.Memory.Modules = append(inv.Memory.Modules, m)
	}
	if len(rows) > 0 {
		inv.Memory.ModulesSupported = true
	}
	// Slot count from the SMBIOS array.
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_PhysicalMemoryArray | Select-Object MemoryDevices"); err == nil && len(rows) > 0 {
		if n, err := strconv.Atoi(rows[0]["MemoryDevices"]); err == nil && n > 0 {
			inv.Memory.SlotCount = &n
			used := len(inv.Memory.Modules)
			inv.Memory.SlotsUsed = &used
		}
	}
}

func smbiosMemoryType(v string) string {
	n, _ := strconv.Atoi(v)
	switch n {
	case 20:
		return "DDR"
	case 21:
		return "DDR2"
	case 22:
		return "DDR2 FB-DIMM"
	case 24:
		return "DDR3"
	case 26:
		return "DDR4"
	case 27:
		return "LPDDR"
	case 28:
		return "LPDDR2"
	case 29:
		return "LPDDR3"
	case 30:
		return "LPDDR4"
	case 34:
		return "DDR5"
	case 35:
		return "LPDDR5"
	}
	return ""
}

func legacyMemoryType(v string) string {
	switch v {
	case "20":
		return "DDR"
	case "21":
		return "DDR2"
	case "24":
		return "DDR3"
	}
	return ""
}

func memoryFormFactor(v string) string {
	n, _ := strconv.Atoi(v)
	switch n {
	case 8:
		return "DIMM"
	case 12:
		return "SODIMM"
	case 13:
		return "SRIMM"
	}
	return ""
}

func collectGPUDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-CimInstance Win32_VideoController | Select-Object Name,AdapterCompatibility,AdapterRAM,DriverVersion,DriverDate,PNPDeviceID,VideoProcessor,CurrentHorizontalResolution,CurrentVerticalResolution,CurrentRefreshRate,Status")
	if err != nil {
		addErr("gpu", err)
		return
	}
	for _, r := range rows {
		name := r["Name"]
		if name == "" {
			continue
		}
		g := models.GPU{Name: name, Manufacturer: strings.TrimSpace(r["AdapterCompatibility"]), DriverVersion: r["DriverVersion"], PNPDeviceID: r["PNPDeviceID"], AdapterString: r["VideoProcessor"]}
		if ram, err := strconv.ParseUint(r["AdapterRAM"], 10, 64); err == nil && ram > 0 && ram < 4294967296 {
			// Win32_VideoController.AdapterRAM is a 32-bit field and overflows
			// above 4 GB, so only trust it below that bound.
			g.VRAMBytes = &ram
		}
		if r["DriverDate"] != "" {
			if t, err := parseWinDate(strings.Split(r["DriverDate"], " ")[0]); err == nil {
				g.DriverDate = t.Format("2006-01-02")
			}
		}
		if h, v := r["CurrentHorizontalResolution"], r["CurrentVerticalResolution"]; h != "" && h != "0" {
			g.Resolution = h + "x" + v
		}
		g.Kind = classifyGPU(name, r["AdapterCompatibility"], r["PNPDeviceID"])
		inv.GPUs = append(inv.GPUs, g)
	}
	// Refine VRAM with the registry HardwareInformation.qwMemorySize (64-bit).
	if qw := regGet(ctx, `HKLM:\SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}\0000`, "HardwareInformation.qwMemorySize"); qw != "" {
		if n, err := strconv.ParseUint(strings.TrimSpace(qw), 10, 64); err == nil && n > 0 && len(inv.GPUs) > 0 && (inv.GPUs[0].VRAMBytes == nil || *inv.GPUs[0].VRAMBytes < n) {
			inv.GPUs[0].VRAMBytes = &n
		}
	}
}

func classifyGPU(name, vendor, pnp string) string {
	ln := strings.ToLower(name + " " + vendor)
	integrated := []string{"intel(r) uhd", "intel(r) hd", "intel iris", "hd graphics", "radeon graphics", "vega 8", "vega 7", "vega 6", "vega 3", "radeon(tm) graphics", "apple m1", "apple m2", "apple m3", "apple m4", "microsoft basic display", "virtio", "vmware svga", "hyper-v video"}
	for _, k := range integrated {
		if strings.Contains(ln, k) {
			if strings.HasPrefix(k, "apple m") {
				return "integrated"
			}
			return "integrated"
		}
	}
	dedicated := []string{"nvidia", "geforce", "quadro", "rtx", "gtx", "radeon rx", "radeon pro", "arc a", "arc b", "intel arc"}
	for _, k := range dedicated {
		if strings.Contains(ln, k) {
			return "dedicated"
		}
	}
	return "unknown"
}

func collectMotherboardDetail(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_BaseBoard | Select-Object Manufacturer,Product,Version,SerialNumber"); err == nil && len(rows) > 0 {
		r := rows[0]
		inv.Motherboard.Manufacturer = r["Manufacturer"]
		inv.Motherboard.Product = r["Product"]
		inv.Motherboard.Version = r["Version"]
		inv.Motherboard.SerialNumber = r["SerialNumber"]
	}
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_BIOS | Select-Object Manufacturer,SMBIOSBIOSVersion,ReleaseDate,Version"); err == nil && len(rows) > 0 {
		r := rows[0]
		inv.Motherboard.BIOSVendor = r["Manufacturer"]
		inv.Motherboard.BIOSVersion = r["SMBIOSBIOSVersion"]
		if r["BIOSVersion"] != "" && inv.Motherboard.BIOSVersion == "" {
			inv.Motherboard.BIOSVersion = r["BIOSVersion"]
		}
		if d := strings.Split(r["ReleaseDate"], " ")[0]; d != "" {
			if t, err := parseWinDate(d); err == nil {
				inv.Motherboard.BIOSDate = t.Format("2006-01-02")
			}
		}
	}
}

func collectPhysicalDisks(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-PhysicalDisk | Select-Object DeviceId,FriendlyName,SerialNumber,MediaType,BusType,Size,HealthStatus,OperationalStatus,FirmwareVersion,Manufacturer,Model")
	if err != nil {
		// Fall back to Win32_DiskDrive (no health/SMART data).
		addErr("physical_disks", err)
		if rows2, err2 := psCSV(ctx, "Get-CimInstance Win32_DiskDrive | Select-Object DeviceID,Model,SerialNumber,Size,InterfaceType,MediaType,FirmwareRevision,Manufacturer,PNPDeviceID"); err2 == nil {
			for _, r := range rows2 {
				size, _ := strconv.ParseUint(r["Size"], 10, 64)
				d := models.PhysicalDisk{Name: r["DeviceID"], Model: r["Model"], SerialNumber: r["SerialNumber"], CapacityBytes: size,
					BusType: r["InterfaceType"], FirmwareVersion: r["FirmwareRevision"], Manufacturer: r["Manufacturer"], Health: "unknown"}
				d.MediaType = mediaFromWin32(r["MediaType"], r["PNPDeviceID"])
				inv.PhysicalDisks = append(inv.PhysicalDisks, d)
			}
		}
		return
	}
	for _, r := range rows {
		size, _ := strconv.ParseUint(r["Size"], 10, 64)
		d := models.PhysicalDisk{
			Name: r["DeviceId"], Model: r["FriendlyName"], SerialNumber: strings.TrimSpace(r["SerialNumber"]),
			CapacityBytes: size, MediaType: r["MediaType"], BusType: r["BusType"], FirmwareVersion: r["FirmwareVersion"],
			Manufacturer: r["Manufacturer"], Health: "unknown", HealthSupported: true,
		}
		switch strings.ToLower(r["HealthStatus"]) {
		case "healthy":
			d.Health = "ok"
		case "warning":
			d.Health = "warning"
		case "unhealthy":
			d.Health = "failing"
		}
		if d.MediaType == "" {
			d.MediaType = "Unspecified"
		}
		if strings.EqualFold(d.BusType, "NVMe") {
			d.MediaType = "NVMe"
		}
		d.HealthDetail = "HealthStatus=" + r["HealthStatus"] + " OperationalStatus=" + strings.Trim(r["OperationalStatus"], "{}")
		inv.PhysicalDisks = append(inv.PhysicalDisks, d)
	}
	// SMART predictive failure + temperature via the storage reliability counters.
	if rows, err := psCSV(ctx, "Get-PhysicalDisk | Select-Object DeviceId | Get-StorageReliabilityCounter -ErrorAction SilentlyContinue | Select-Object DeviceId,Temperature,TemperatureMax,PowerOnHours,Wear,ReadErrorsTotal"); err == nil {
		for _, r := range rows {
			for i := range inv.PhysicalDisks {
				if inv.PhysicalDisks[i].Name == r["DeviceId"] {
					if t, err := strconv.Atoi(r["Temperature"]); err == nil && t > 0 {
						inv.PhysicalDisks[i].TemperatureC = &t
					}
					if h, err := strconv.ParseInt(r["PowerOnHours"], 10, 64); err == nil && h > 0 {
						inv.PhysicalDisks[i].PowerOnHours = &h
					}
					if w := r["Wear"]; w != "" {
						inv.PhysicalDisks[i].HealthDetail += " Wear=" + w + "%"
					}
				}
			}
		}
	}
}

func mediaFromWin32(mediaType, pnp string) string {
	mt := strings.ToLower(mediaType)
	switch {
	case strings.Contains(mt, "ssd"), strings.Contains(mt, "solid state"):
		return "SSD"
	case strings.Contains(mt, "fixed hard disk"):
		if strings.Contains(strings.ToLower(pnp), "nvme") {
			return "NVMe"
		}
		return "HDD"
	case strings.Contains(mt, "removable"):
		return "Removable"
	}
	if strings.Contains(strings.ToLower(pnp), "nvme") {
		return "NVMe"
	}
	return "Unknown"
}

func collectMonitors(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-CimInstance -Namespace root\\wmi -ClassName WmiMonitorID | Select-Object InstanceName,ManufacturerName,UserFriendlyName,SerialNumberID,YearOfManufacture,Active")
	if err != nil {
		addErr("monitors", err)
		return
	}
	// Map device instance to connection type.
	conn := map[string]string{}
	if r2, err := psCSV(ctx, "Get-CimInstance -Namespace root\\wmi -ClassName WmiMonitorConnectionParams | Select-Object InstanceName,VideoOutputTechnology"); err == nil {
		for _, r := range r2 {
			conn[r["InstanceName"]] = videoOutputTech(r["VideoOutputTechnology"])
		}
	}
	for _, r := range rows {
		name := decodeWmiString(r["UserFriendlyName"])
		if name == "" && r["InstanceName"] == "" {
			continue
		}
		m := models.Monitor{
			Name: name, Manufacturer: decodeWmiString(r["ManufacturerName"]),
			SerialNumber: decodeWmiString(r["SerialNumberID"]), YearOfManufacture: r["YearOfManufacture"],
			Connection: conn[r["InstanceName"]],
		}
		inv.Monitors = append(inv.Monitors, m)
	}
	// Resolution and refresh rate per display (current mode).
	if rows, err := psCSV(ctx, "Get-CimInstance Win32_VideoController | Where-Object {$_.CurrentHorizontalResolution -gt 0} | Select-Object Name,CurrentHorizontalResolution,CurrentVerticalResolution,CurrentRefreshRate"); err == nil {
		for _, r := range rows {
			w, _ := strconv.Atoi(r["CurrentHorizontalResolution"])
			h, _ := strconv.Atoi(r["CurrentVerticalResolution"])
			hz, _ := strconv.Atoi(r["CurrentRefreshRate"])
			matched := false
			for i := range inv.Monitors {
				if inv.Monitors[i].WidthPx == 0 {
					inv.Monitors[i].WidthPx = w
					inv.Monitors[i].HeightPx = h
					if hz > 0 {
						inv.Monitors[i].RefreshHz = &hz
					}
					inv.Monitors[i].Primary = !matched
					matched = true
					break
				}
			}
			if !matched {
				inv.Monitors = append(inv.Monitors, models.Monitor{Name: r["Name"], WidthPx: w, HeightPx: h, RefreshHz: intPtr(hz), Primary: true})
			}
		}
	}
}

func intPtr(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

// decodeWmiString converts the uint16[] representation of WMI string
// properties (as rendered by PowerShell) into text.
func decodeWmiString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	trimmed := strings.Trim(s, "{} ")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ",")
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return strings.TrimSpace(s)
		}
		if n == 0 {
			continue
		}
		if n >= 32 && n < 127 {
			b.WriteByte(byte(n))
		}
	}
	return strings.TrimSpace(b.String())
}

func videoOutputTech(v string) string {
	n, _ := strconv.Atoi(v)
	switch n {
	case 0:
		return "VGA"
	case 4:
		return "DVI"
	case 5:
		return "HDMI"
	case 10:
		return "DisplayPort"
	case 11:
		return "DisplayPort"
	case 0x80000000:
		return "Internal"
	}
	return ""
}

func collectBatteries(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	bats, err := batteryStats(ctx)
	if err != nil || len(bats) == 0 {
		return
	}
	for _, b := range bats {
		inv.Batteries = append(inv.Batteries, models.Battery{Name: b.Name, Status: b.Status, Percent: b.Percent,
			DesignCapacityMWh: b.DesignMWh, FullCapacityMWh: b.FullMWh, CycleCount: b.Cycles})
	}
	// Static battery details (design capacity, cycles) from Win32_Battery / root\wmi.
	if rows, err := psCSV(ctx, "Get-CimInstance -Namespace root\\wmi -ClassName BatteryStaticData | Select-Object DesignedCapacity,CycleCount,DeviceName"); err == nil {
		for i, r := range rows {
			if i >= len(inv.Batteries) {
				break
			}
			if n, err := strconv.ParseInt(r["DesignedCapacity"], 10, 64); err == nil && n > 0 {
				inv.Batteries[i].DesignCapacityMWh = &n
			}
			if n, err := strconv.Atoi(r["CycleCount"]); err == nil && n > 0 {
				inv.Batteries[i].CycleCount = &n
			}
		}
	}
	if rows, err := psCSV(ctx, "Get-CimInstance -Namespace root\\wmi -ClassName BatteryFullChargedCapacity | Select-Object FullChargedCapacity"); err == nil {
		for i, r := range rows {
			if i >= len(inv.Batteries) {
				break
			}
			if n, err := strconv.ParseInt(r["FullChargedCapacity"], 10, 64); err == nil && n > 0 {
				inv.Batteries[i].FullCapacityMWh = &n
			}
		}
	}
	for i := range inv.Batteries {
		b := &inv.Batteries[i]
		if b.DesignCapacityMWh != nil && b.FullCapacityMWh != nil && *b.DesignCapacityMWh > 0 {
			h := float64(*b.FullCapacityMWh) / float64(*b.DesignCapacityMWh) * 100
			h = float64(int(h*10)) / 10
			b.HealthPercent = &h
		}
	}
}

func collectAudio(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-CimInstance Win32_SoundDevice | Select-Object Name,Manufacturer,Status")
	if err != nil {
		addErr("audio", err)
		return
	}
	for _, r := range rows {
		if r["Name"] == "" {
			continue
		}
		inv.Audio = append(inv.Audio, models.AudioDevice{Name: r["Name"], Manufacturer: r["Manufacturer"], Kind: "unknown"})
	}
}

func collectUSB(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-PnpDevice -PresentOnly | Where-Object {$_.InstanceId -like 'USB\\*'} | Select-Object FriendlyName,InstanceId,Manufacturer,Class,Status")
	if err != nil {
		addErr("usb", err)
		return
	}
	for _, r := range rows {
		name := r["FriendlyName"]
		if name == "" {
			continue
		}
		u := models.USBDevice{Name: name, Manufacturer: r["Manufacturer"], Class: r["Class"]}
		if vid, pid, serial, ok := parseUSBInstance(r["InstanceId"]); ok {
			u.VendorID = vid
			u.ProductID = pid
			u.SerialNumber = serial
		}
		inv.USB = append(inv.USB, u)
	}
}

var usbInstanceRe = regexp.MustCompile(`USB\\VID_([0-9A-Fa-f]{4})&PID_([0-9A-Fa-f]{4})(?:\\([^\\]+))?`)

func parseUSBInstance(id string) (vid, pid, serial string, ok bool) {
	m := usbInstanceRe.FindStringSubmatch(id)
	if len(m) < 3 {
		return "", "", "", false
	}
	serial = ""
	if len(m) > 3 {
		serial = m[3]
	}
	return strings.ToUpper(m[1]), strings.ToUpper(m[2]), serial, true
}

func collectBluetooth(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-PnpDevice -Class Bluetooth -PresentOnly | Select-Object FriendlyName,Manufacturer,InstanceId")
	if err != nil {
		return
	}
	for _, r := range rows {
		if r["FriendlyName"] == "" {
			continue
		}
		b := models.BluetoothAdapter{Name: r["FriendlyName"], Manufacturer: r["Manufacturer"]}
		if m := regexp.MustCompile(`([0-9A-Fa-f]{12})`).FindStringSubmatch(r["InstanceId"]); len(m) == 2 {
			hex := strings.ToUpper(m[1])
			var parts []string
			for i := 0; i+1 < len(hex); i += 2 {
				parts = append(parts, hex[i:i+2])
			}
			b.Address = strings.Join(parts, ":")
		}
		inv.Bluetooth = append(inv.Bluetooth, b)
	}
}

func collectOptical(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-CimInstance Win32_CDROMDrive | Select-Object Name,Drive,MediaType")
	if err != nil {
		return
	}
	for _, r := range rows {
		inv.Optical = append(inv.Optical, models.OpticalDrive{Name: r["Name"], Model: r["Drive"]})
	}
}

func collectPrinters(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	rows, err := psCSV(ctx, "Get-Printer | Select-Object Name,DriverName,PortName,Type")
	if err != nil {
		addErr("printers", err)
		return
	}
	defaultName := ""
	if out, err := psRun(ctx, "(Get-CimInstance Win32_Printer -Filter 'Default=TRUE').Name"); err == nil {
		defaultName = strings.TrimSpace(out)
	}
	for _, r := range rows {
		p := models.Printer{Name: r["Name"], Driver: r["DriverName"], Port: r["PortName"]}
		p.IsDefault = strings.EqualFold(p.Name, defaultName)
		port := strings.ToUpper(p.Port)
		p.IsNetwork = strings.HasPrefix(port, "IP_") || strings.HasPrefix(port, "TCP") || strings.HasPrefix(port, "WSD") || strings.HasPrefix(port, "\\\\")
		inv.Printers = append(inv.Printers, p)
	}
}

func collectSoftware(ctx context.Context, inv *models.Inventory, addErr func(string, error)) {
	seen := map[string]bool{}
	addApp := func(name, version, publisher, date, location, size string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name+"|"+version)] {
			return
		}
		seen[strings.ToLower(name+"|"+version)] = true
		a := models.Application{Name: name, Version: version, Publisher: publisher, InstallLocation: location}
		if t, err := parseWinDate(date); err == nil {
			a.InstallDate = &t
		}
		if n, err := strconv.ParseUint(size, 10, 64); err == nil && n > 0 {
			kb := n / 1024
			a.EstimatedSizeKB = &kb
		}
		inv.Software = append(inv.Software, a)
	}
	paths := []string{
		`HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*`,
		`HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*`,
		`HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*`,
	}
	for _, p := range paths {
		rows, err := psCSV(ctx, "Get-ItemProperty '"+p+"' | Where-Object {$_.DisplayName} | Select-Object DisplayName,DisplayVersion,Publisher,InstallDate,InstallLocation,EstimatedSize")
		if err != nil {
			continue
		}
		for _, r := range rows {
			addApp(r["DisplayName"], r["DisplayVersion"], r["Publisher"], r["InstallDate"], r["InstallLocation"], r["EstimatedSize"])
		}
	}
	sort.Slice(inv.Software, func(i, j int) bool { return strings.ToLower(inv.Software[i].Name) < strings.ToLower(inv.Software[j].Name) })
}

func collectLoggedInUser(ctx context.Context) string {
	out, err := psRun(ctx, "(Get-CimInstance Win32_ComputerSystem).UserName")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func gatewayCommand() []string {
	return []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Get-NetRoute -DestinationPrefix '0.0.0.0/0' | ForEach-Object { $_.NextHop + ' ' + $_.InterfaceAlias }"}
}

func parseRouteLine(line string) (gw, iface string, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	if ip := net.ParseIP(fields[0]); ip == nil || ip.To4() == nil {
		return "", "", false
	}
	return fields[0], strings.Join(fields[1:], " "), true
}

func systemDNS(ctx context.Context) []string {
	out, err := psRun(ctx, "(Get-DnsClientServerAddress -AddressFamily IPv4 | Where-Object {$_.ServerAddresses}).ServerAddresses -join ','")
	if err != nil {
		return nil
	}
	var out2 []string
	for _, s := range strings.Split(strings.TrimSpace(out), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out2 = append(out2, s)
		}
	}
	return out2
}

func collectAdapterDetail(ctx context.Context, na *models.NetAdapter, ifName string) {
	// Map the friendly name used by CIM to the gopsutil interface name.
	if na.Description == "" {
		na.Description = ifName
	}
	if na.Manufacturer == "" {
		out, err := psRun(ctx, fmt.Sprintf("(Get-NetAdapter -Name '%s').InterfaceDescription", escapePS(ifName)))
		if err == nil && strings.TrimSpace(out) != "" {
			na.Description = strings.TrimSpace(out)
			na.Manufacturer = adapterVendor(na.Description)
		}
	}
	if out, err := psRun(ctx, fmt.Sprintf("(Get-NetAdapter -Name '%s').LinkSpeed", escapePS(ifName))); err == nil {
		if mbps := parseLinkSpeed(strings.TrimSpace(out)); mbps > 0 {
			na.SpeedMbps = &mbps
		}
	}
	if out, err := psRun(ctx, fmt.Sprintf("(Get-NetAdapter -Name '%s').PhysicalMediaType", escapePS(ifName))); err == nil {
		pm := strings.ToLower(strings.TrimSpace(out))
		switch {
		case strings.Contains(pm, "802.11"):
			na.Kind = "wifi"
		case strings.Contains(pm, "802.3"):
			na.Kind = "ethernet"
		}
	}
	if na.Kind == "wifi" {
		if out, err := psRun(ctx, fmt.Sprintf("(netsh wlan show interfaces | Select-String '^\\s+SSID\\s+:' | Select-Object -First 1) -replace '.*:\\s*',''")); err == nil {
			na.SSID = strings.TrimSpace(out)
		}
	}
	if out, err := psRun(ctx, fmt.Sprintf("(Get-NetIPInterface -InterfaceAlias '%s' -AddressFamily IPv4).Dhcp", escapePS(ifName))); err == nil {
		v := strings.ToLower(strings.TrimSpace(out))
		if v == "enabled" || v == "disabled" {
			b := v == "enabled"
			na.DHCP = &b
		}
	}
}

func escapePS(s string) string { return strings.ReplaceAll(s, "'", "''") }

func adapterVendor(desc string) string {
	ld := strings.ToLower(desc)
	for _, v := range []struct{ key, name string }{
		{"intel", "Intel"}, {"realtek", "Realtek"}, {"broadcom", "Broadcom"}, {"qualcomm", "Qualcomm"},
		{"atheros", "Qualcomm Atheros"}, {"marvell", "Marvell"}, {"killer", "Rivet Networks"},
		{"vmware", "VMware"}, {"virtualbox", "Oracle"}, {"hyper-v", "Microsoft"}, {"microsoft", "Microsoft"},
		{"tp-link", "TP-Link"}, {"asix", "ASIX"}, {"mediatek", "MediaTek"},
	} {
		if strings.Contains(ld, v.key) {
			return v.name
		}
	}
	return ""
}

// parseLinkSpeed converts "1 Gbps", "100 Mbps", "2.5 Gbps" to Mbps.
func parseLinkSpeed(s string) int64 {
	ls := strings.ToLower(s)
	var val float64
	var unit string
	for i, r := range ls {
		if (r < '0' || r > '9') && r != '.' {
			if _, err := fmt.Sscanf(ls[:i], "%f", &val); err != nil {
				return 0
			}
			unit = strings.TrimSpace(ls[i:])
			break
		}
	}
	switch {
	case strings.HasPrefix(unit, "gbps"), strings.HasPrefix(unit, "g"):
		return int64(val * 1000)
	case strings.HasPrefix(unit, "mbps"), strings.HasPrefix(unit, "m"):
		return int64(val)
	case strings.HasPrefix(unit, "kbps"), strings.HasPrefix(unit, "k"):
		return int64(val / 1000)
	}
	return 0
}

func batteryStats(ctx context.Context) ([]batteryInfo, error) {
	rows, err := psCSV(ctx, "Get-CimInstance Win32_Battery | Select-Object Name,EstimatedChargeRemaining,BatteryStatus,DesignCapacity")
	if err != nil {
		return nil, err
	}
	var out []batteryInfo
	for _, r := range rows {
		b := batteryInfo{Name: r["Name"]}
		if v, err := strconv.ParseFloat(r["EstimatedChargeRemaining"], 64); err == nil {
			b.Percent = &v
		}
		switch r["BatteryStatus"] {
		case "2":
			b.Status = "charging"
			plugged := true
			b.PluggedIn = &plugged
		case "1":
			b.Status = "discharging"
			plugged := false
			b.PluggedIn = &plugged
		case "3":
			b.Status = "full"
			plugged := true
			b.PluggedIn = &plugged
		default:
			b.Status = "unknown"
		}
		out = append(out, b)
	}
	return out, nil
}

// platform-specific helper used by collectOSDetail for extra host facts.
func platformHostExtra(ctx context.Context, inv *models.Inventory) {
	if hi, err := host.InfoWithContext(ctx); err == nil && hi.VirtualizationSystem != "" {
		inv.Host.IsVirtual = hi.VirtualizationRole == "guest"
	}
}

var _ = os.Getenv
