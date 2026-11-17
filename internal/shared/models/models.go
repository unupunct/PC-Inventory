// Package models holds the wire and storage data model shared by the server
// and the agent. All inventory and metric values are produced by real
// platform collectors; optional fields are pointers/omitempty so that
// "not available on this platform" is never confused with a real zero value.
package models

import "time"

// Platform identifies the agent operating system family.
const (
	PlatformWindows = "windows"
	PlatformDarwin  = "darwin"
	PlatformLinux   = "linux"
	PlatformUnknown = "unknown"
)

// ---------------------------------------------------------------------------
// Hardware inventory
// ---------------------------------------------------------------------------

type Inventory struct {
	CollectedAt time.Time `json:"collectedAt"`
	// Hash is a stable fingerprint of the hardware-relevant parts of the
	// inventory; the server compares hashes to detect hardware changes and
	// only stores a new snapshot when it differs.
	Hash         string        `json:"hash"`
	Host         Host          `json:"host"`
	OS           OperatingSystem `json:"os"`
	CPU          CPU           `json:"cpu"`
	Memory       Memory        `json:"memory"`
	GPUs         []GPU         `json:"gpus"`
	Motherboard  Motherboard   `json:"motherboard"`
	PhysicalDisks []PhysicalDisk `json:"physicalDisks"`
	Volumes      []Volume      `json:"volumes"`
	NetAdapters  []NetAdapter  `json:"networkAdapters"`
	Monitors     []Monitor     `json:"monitors"`
	Batteries    []Battery     `json:"batteries"`
	Audio        []AudioDevice `json:"audioDevices"`
	USB          []USBDevice   `json:"usbDevices"`
	Bluetooth    []BluetoothAdapter `json:"bluetoothAdapters"`
	Optical      []OpticalDrive `json:"opticalDrives"`
	Printers     []Printer     `json:"printers"`
	Software     []Application `json:"software"`
	// CollectionErrors lists collectors that failed, with the reason. The UI
	// surfaces these instead of pretending the data is complete.
	CollectionErrors []CollectorError `json:"collectionErrors"`
}

type CollectorError struct {
	Category string `json:"category"`
	Error    string `json:"error"`
}

type Host struct {
	Hostname      string `json:"hostname"`
	Platform      string `json:"platform"`
	PlatformFamily string `json:"platformFamily"`
	Domain        string `json:"domain,omitempty"`
	Manufacturer  string `json:"manufacturer,omitempty"`
	Model         string `json:"model,omitempty"`
	SerialNumber  string `json:"serialNumber,omitempty"`
	ChassisType   string `json:"chassisType,omitempty"`
	IsVirtual     bool   `json:"isVirtual"`
	AgentVersion  string `json:"agentVersion"`
}

type OperatingSystem struct {
	Name          string     `json:"name"`
	Edition       string     `json:"edition,omitempty"`
	Version       string     `json:"version,omitempty"`
	Build         string     `json:"build,omitempty"`
	Kernel        string     `json:"kernel,omitempty"`
	Arch          string     `json:"arch"`
	InstallDate   *time.Time `json:"installDate,omitempty"`
	BootTime      *time.Time `json:"bootTime,omitempty"`
	UptimeSeconds uint64     `json:"uptimeSeconds"`
	// Timezone is the IANA name where available, else the raw OS zone.
	Timezone string `json:"timezone,omitempty"`
	// Windows-specific
	ProductKey   string `json:"-"` // never collected
	ActivationState string `json:"activationState,omitempty"`
	DefenderEnabled *bool  `json:"defenderEnabled,omitempty"`
	DefenderSignatureAgeDays *int `json:"defenderSignatureAgeDays,omitempty"`
	InstalledUpdates []InstalledUpdate `json:"installedUpdates,omitempty"`
	// Linux-specific
	PackageManager string `json:"packageManager,omitempty"`
	Systemd        *bool  `json:"systemd,omitempty"`
	// macOS-specific
	HardwareModel string `json:"hardwareModel,omitempty"`
	AppleSilicon  *bool  `json:"appleSilicon,omitempty"`
}

type InstalledUpdate struct {
	ID          string     `json:"id"`
	Description string     `json:"description,omitempty"`
	InstalledAt *time.Time `json:"installedAt,omitempty"`
}

type CPU struct {
	Manufacturer    string   `json:"manufacturer,omitempty"`
	Brand           string   `json:"brand"`
	Family          string   `json:"family,omitempty"`
	Architecture    string   `json:"architecture"`
	Socket          string   `json:"socket,omitempty"`
	PhysicalCores   int      `json:"physicalCores"`
	LogicalCores    int      `json:"logicalCores"`
	BaseFrequencyMHz *float64 `json:"baseFrequencyMHz,omitempty"`
	MaxFrequencyMHz  *float64 `json:"maxFrequencyMHz,omitempty"`
	CacheKB         *int     `json:"cacheKB,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

type Memory struct {
	TotalBytes     uint64          `json:"totalBytes"`
	Modules        []MemoryModule  `json:"modules"`
	// ModulesSupported reports whether the platform exposes module details.
	ModulesSupported bool          `json:"modulesSupported"`
	SlotCount      *int            `json:"slotCount,omitempty"`
	SlotsUsed      *int            `json:"slotsUsed,omitempty"`
}

type MemoryModule struct {
	Slot           string `json:"slot,omitempty"`
	Manufacturer   string `json:"manufacturer,omitempty"`
	PartNumber     string `json:"partNumber,omitempty"`
	SerialNumber   string `json:"serialNumber,omitempty"`
	CapacityBytes  uint64 `json:"capacityBytes"`
	Type           string `json:"type,omitempty"` // DDR3/DDR4/DDR5...
	SpeedMTs       *int   `json:"speedMTs,omitempty"`
	FormFactor     string `json:"formFactor,omitempty"`
}

type GPU struct {
	Name           string `json:"name"`
	Manufacturer   string `json:"manufacturer,omitempty"`
	VRAMBytes      *uint64 `json:"vramBytes,omitempty"`
	DriverVersion  string `json:"driverVersion,omitempty"`
	DriverDate     string `json:"driverDate,omitempty"`
	Kind           string `json:"kind,omitempty"` // integrated | dedicated | unknown
	PNPDeviceID    string `json:"pnpDeviceId,omitempty"`
	AdapterString  string `json:"adapterString,omitempty"`
	Resolution     string `json:"resolution,omitempty"`
}

type Motherboard struct {
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Version      string `json:"version,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	BIOSVendor   string `json:"biosVendor,omitempty"`
	BIOSVersion  string `json:"biosVersion,omitempty"`
	BIOSDate     string `json:"biosDate,omitempty"`
	SystemManufacturer string `json:"systemManufacturer,omitempty"`
	SystemProduct      string `json:"systemProduct,omitempty"`
	SystemSerial       string `json:"systemSerial,omitempty"`
}

type PhysicalDisk struct {
	Name         string  `json:"name"`
	Manufacturer string  `json:"manufacturer,omitempty"`
	Model        string  `json:"model,omitempty"`
	SerialNumber string  `json:"serialNumber,omitempty"`
	CapacityBytes uint64 `json:"capacityBytes"`
	MediaType    string  `json:"mediaType,omitempty"` // SSD | HDD | NVMe | Unknown
	BusType      string  `json:"busType,omitempty"`   // SATA | NVMe | USB | ...
	FirmwareVersion string `json:"firmwareVersion,omitempty"`
	// Health: ok | warning | failing | unknown
	Health       string  `json:"health"`
	HealthDetail string  `json:"healthDetail,omitempty"`
	TemperatureC *int    `json:"temperatureC,omitempty"`
	// HealthSupported is false when the platform exposes no SMART data.
	HealthSupported bool  `json:"healthSupported"`
	PowerOnHours    *int64 `json:"powerOnHours,omitempty"`
}

type Volume struct {
	MountPoint   string  `json:"mountPoint"`
	Label        string  `json:"label,omitempty"`
	Filesystem   string  `json:"filesystem,omitempty"`
	TotalBytes   uint64  `json:"totalBytes"`
	UsedBytes    uint64  `json:"usedBytes"`
	FreeBytes    uint64  `json:"freeBytes"`
	UsagePercent float64 `json:"usagePercent"`
	IsRemovable  bool    `json:"isRemovable"`
	Device       string  `json:"device,omitempty"`
}

type NetAdapter struct {
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	Manufacturer  string   `json:"manufacturer,omitempty"`
	MAC           string   `json:"mac,omitempty"`
	IPv4          []string `json:"ipv4"`
	IPv6          []string `json:"ipv6"`
	SpeedMbps     *int64   `json:"speedMbps,omitempty"`
	MTU           *int     `json:"mtu,omitempty"`
	Status        string   `json:"status"`     // up | down | unknown
	Kind          string   `json:"kind"`       // ethernet | wifi | virtual | loopback | other
	DefaultGateway string  `json:"defaultGateway,omitempty"`
	DNS           []string `json:"dns"`
	IsVirtual     bool     `json:"isVirtual"`
	DHCP          *bool    `json:"dhcp,omitempty"`
	// Wi-Fi: SSID only. Never credentials.
	SSID          string   `json:"ssid,omitempty"`
}

type Monitor struct {
	Name          string `json:"name,omitempty"`
	Manufacturer  string `json:"manufacturer,omitempty"`
	Model         string `json:"model,omitempty"`
	SerialNumber  string `json:"serialNumber,omitempty"`
	YearOfManufacture string `json:"yearOfManufacture,omitempty"`
	WidthPx       int    `json:"widthPx,omitempty"`
	HeightPx      int    `json:"heightPx,omitempty"`
	RefreshHz     *int   `json:"refreshHz,omitempty"`
	Primary       bool   `json:"primary"`
	Connection    string `json:"connection,omitempty"`
}

type Battery struct {
	Name               string   `json:"name,omitempty"`
	Status             string   `json:"status,omitempty"` // charging | discharging | full | unknown
	Percent            *float64 `json:"percent,omitempty"`
	DesignCapacityMWh  *int64   `json:"designCapacityMWh,omitempty"`
	FullCapacityMWh    *int64   `json:"fullCapacityMWh,omitempty"`
	HealthPercent      *float64 `json:"healthPercent,omitempty"`
	SecondsRemaining   *int64   `json:"secondsRemaining,omitempty"`
	CycleCount         *int     `json:"cycleCount,omitempty"`
}

type AudioDevice struct {
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Kind         string `json:"kind,omitempty"` // output | input | unknown
}

type USBDevice struct {
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	VendorID     string `json:"vendorId,omitempty"`
	ProductID    string `json:"productId,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	Class        string `json:"class,omitempty"`
}

type BluetoothAdapter struct {
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Address      string `json:"address,omitempty"`
}

type OpticalDrive struct {
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

type Printer struct {
	Name     string `json:"name"`
	Driver   string `json:"driver,omitempty"`
	Port     string `json:"port,omitempty"`
	IsDefault bool  `json:"isDefault"`
	IsNetwork bool  `json:"isNetwork"`
}

type Application struct {
	Name             string     `json:"name"`
	Version          string     `json:"version,omitempty"`
	Publisher        string     `json:"publisher,omitempty"`
	InstallDate      *time.Time `json:"installDate,omitempty"`
	InstallLocation  string     `json:"installLocation,omitempty"`
	EstimatedSizeKB  *uint64    `json:"estimatedSizeKb,omitempty"`
}

// ---------------------------------------------------------------------------
// Live performance metrics
// ---------------------------------------------------------------------------

type Metrics struct {
	CollectedAt time.Time `json:"collectedAt"`
	IntervalSec int       `json:"intervalSec"`
	UptimeSeconds uint64  `json:"uptimeSeconds"`
	CPU         CPUMetrics    `json:"cpu"`
	Memory      MemoryMetrics `json:"memory"`
	Disks       []DiskMetrics `json:"disks"`
	Network     []NetMetrics  `json:"network"`
	Volumes     []VolumeUsage `json:"volumes"`
	Processes   *int          `json:"processCount,omitempty"`
	Load        *LoadAvg      `json:"load,omitempty"`
	Battery     *BatteryMetrics `json:"battery,omitempty"`
	Temperatures []Temperature `json:"temperatures,omitempty"`
	// Unsupported names metrics the platform cannot provide, so the UI can
	// label them instead of showing zeros.
	Unsupported []string `json:"unsupported,omitempty"`
}

type CPUMetrics struct {
	UsagePercent float64   `json:"usagePercent"`
	PerCore      []float64 `json:"perCore,omitempty"`
	FrequencyMHz *float64  `json:"frequencyMHz,omitempty"`
}

type MemoryMetrics struct {
	TotalBytes     uint64  `json:"totalBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsagePercent   float64 `json:"usagePercent"`
	SwapTotalBytes uint64  `json:"swapTotalBytes"`
	SwapUsedBytes  uint64  `json:"swapUsedBytes"`
}

type DiskMetrics struct {
	Name            string  `json:"name"`
	ReadBytesPerSec float64 `json:"readBytesPerSec"`
	WriteBytesPerSec float64 `json:"writeBytesPerSec"`
	UtilizationPercent *float64 `json:"utilizationPercent,omitempty"`
	IOPS            *float64 `json:"iops,omitempty"`
}

type NetMetrics struct {
	Name           string  `json:"name"`
	RxBytesPerSec  float64 `json:"rxBytesPerSec"`
	TxBytesPerSec  float64 `json:"txBytesPerSec"`
	RxBytesTotal   uint64  `json:"rxBytesTotal"`
	TxBytesTotal   uint64  `json:"txBytesTotal"`
}

type VolumeUsage struct {
	MountPoint string  `json:"mountPoint"`
	TotalBytes uint64  `json:"totalBytes"`
	FreeBytes  uint64  `json:"freeBytes"`
	UsagePercent float64 `json:"usagePercent"`
}

type LoadAvg struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

type BatteryMetrics struct {
	Percent *float64 `json:"percent,omitempty"`
	Status  string   `json:"status,omitempty"`
	PluggedIn *bool  `json:"pluggedIn,omitempty"`
}

type Temperature struct {
	Sensor  string  `json:"sensor"`
	Celsius float64 `json:"celsius"`
	Source  string  `json:"source,omitempty"`
}

// ---------------------------------------------------------------------------
// Agent <-> server envelopes
// ---------------------------------------------------------------------------

type EnrollmentRequest struct {
	MachineID    string `json:"machineId"`
	Hostname     string `json:"hostname"`
	Platform     string `json:"platform"`
	OSName       string `json:"osName"`
	OSVersion    string `json:"osVersion"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agentVersion"`
	Protocol     int    `json:"protocol"`
	// PrimaryMAC / PrimaryIP are informational hints shown to the admin.
	PrimaryMAC string `json:"primaryMac,omitempty"`
	PrimaryIP  string `json:"primaryIp,omitempty"`
}

type EnrollmentResponse struct {
	Status string `json:"status"` // pending | approved | rejected
	AgentID   string `json:"agentId,omitempty"`
	AgentToken string `json:"agentToken,omitempty"`
	Message   string `json:"message,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	// AgentConfig is sent once approved so the agent picks up server-side
	// collection intervals immediately.
	Config *AgentConfig `json:"config,omitempty"`
}

// AgentConfig is the server-controlled runtime configuration for an agent.
type AgentConfig struct {
	HeartbeatIntervalSec  int  `json:"heartbeatIntervalSec"`
	MetricsIntervalSec    int  `json:"metricsIntervalSec"`
	InventoryIntervalSec  int  `json:"inventoryIntervalSec"`
	CollectSoftware       bool `json:"collectSoftware"`
	CollectMonitors       bool `json:"collectMonitors"`
	CollectUSB            bool `json:"collectUsb"`
	CollectPrinters       bool `json:"collectPrinters"`
	CollectProcesses      bool `json:"collectProcesses"`
	CollectPerCoreCPU     bool `json:"collectPerCoreCpu"`
	CollectTemperatures   bool `json:"collectTemperatures"`
	CollectInstalledApps  bool `json:"collectInstalledApps"`
	CollectWindowsUpdates bool `json:"collectWindowsUpdates"`
	CollectLoggedInUser   bool `json:"collectLoggedInUser"`
	ConfigVersion         int  `json:"configVersion"`
}

func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		HeartbeatIntervalSec:  30,
		MetricsIntervalSec:    30,
		InventoryIntervalSec:  6 * 3600,
		CollectSoftware:       true,
		CollectMonitors:       true,
		CollectUSB:            true,
		CollectPrinters:       true,
		CollectProcesses:      true,
		CollectPerCoreCPU:     true,
		CollectTemperatures:   true,
		CollectInstalledApps:  true,
		CollectWindowsUpdates: true,
		CollectLoggedInUser:   false,
		ConfigVersion:         1,
	}
}

type HeartbeatRequest struct {
	AgentID      string    `json:"agentId"`
	SentAt       time.Time `json:"sentAt"`
	UptimeSeconds uint64   `json:"uptimeSeconds"`
	AgentVersion string    `json:"agentVersion"`
	PrimaryIP    string    `json:"primaryIp,omitempty"`
	PrimaryMAC   string    `json:"primaryMac,omitempty"`
	Hostname     string    `json:"hostname,omitempty"`
	PendingBuffered int    `json:"pendingBuffered"`
	LastError    string    `json:"lastError,omitempty"`
}

type HeartbeatResponse struct {
	OK            bool         `json:"ok"`
	ServerTime    time.Time    `json:"serverTime"`
	Config        *AgentConfig `json:"config,omitempty"`
	ConfigVersion int          `json:"configVersion"`
	// Commands: restricted, non-arbitrary actions only.
	Commands []AgentCommand `json:"commands,omitempty"`
}

type AgentCommand struct {
	Type string `json:"type"` // inventory_refresh | rotate_token | update_config
	ID   string `json:"id"`
}

type MetricsRequest struct {
	AgentID string  `json:"agentId"`
	Metrics Metrics `json:"metrics"`
}

type InventoryRequest struct {
	AgentID   string    `json:"agentId"`
	Inventory Inventory `json:"inventory"`
	// Reason: scheduled | startup | manual | hardware_change
	Reason string `json:"reason,omitempty"`
}

type IngestResponse struct {
	OK       bool   `json:"ok"`
	Changed  bool   `json:"changed"`
	Snapshot int64  `json:"snapshotId,omitempty"`
	Message  string `json:"message,omitempty"`
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type LogsRequest struct {
	AgentID string     `json:"agentId"`
	Entries []LogEntry `json:"entries"`
}
