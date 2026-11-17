// Wire types for the parts of the API the dashboard uses.
// Kept in sync with internal/shared/models and internal/server/store.

export type AgentStatus = "pending" | "approved" | "rejected" | "disabled" | "revoked";

export interface Agent {
  id: string;
  machineId: string;
  hostname: string;
  displayName?: string;
  platform: string;
  osName?: string;
  osVersion?: string;
  arch?: string;
  agentVersion?: string;
  status: AgentStatus;
  isLaptop?: boolean;
  location?: string;
  groupId?: number | null;
  notes?: string;
  primaryIp?: string;
  lastHeartbeatAt?: string;
  lastMetricsAt?: string;
  lastInventoryAt?: string;
  lastError?: string;
  pendingBuffered?: number;
  enrolledAt?: string;
  approvedAt?: string;
  online: boolean;
  onlineState: "online" | "offline" | "never";
  configVersion?: number;
}

export interface Me {
  username: string;
  serverName?: string;
  version?: string;
}

export interface Pair {
  name: string;
  value: number;
}

export interface LiveItem {
  agentId: string;
  hostname: string;
  platform: string;
  at: string;
  value?: number;
  message?: string;
}

export interface HardwareChange {
  id: number;
  agentId: string;
  hostname?: string;
  detectedAt: string;
  category: string;
  severity: string;
  summary: string;
  details?: string;
}

export interface AgentEvent {
  id: number;
  agentId: string;
  hostname?: string;
  at: string;
  kind: string;
  message: string;
  severity?: string;
}

export interface Alert {
  id: number;
  agentId?: string;
  hostname?: string;
  ruleKey: string;
  severity: "info" | "warning" | "critical";
  status: "active" | "acknowledged" | "resolved";
  message: string;
  value?: number;
  createdAt: string;
  updatedAt?: string;
}

export interface AlertRule {
  key: string;
  name: string;
  description?: string;
  enabled: boolean;
  severity: string;
  threshold: number;
  durationSec: number;
  cooldownSec: number;
}

export interface Dashboard {
  stats: {
    totalComputers: number;
    online: number;
    offline: number;
    pending: number;
    disabled: number;
    revoked: number;
    windows: number;
    macos: number;
    linux: number;
    totalRamBytes: number;
    totalCores: number;
    totalStorageBytes: number;
    freeStorageBytes: number;
    needsAttention: number;
    laptops: number;
    desktops: number;
  };
  osDistribution: Pair[];
  cpuManufacturers: Pair[];
  gpuManufacturers: Pair[];
  ramDistribution: Pair[];
  ramTypes: Pair[];
  mediaDistribution: Pair[];
  storageDistribution: Pair[];
  onlineDistribution: Pair[];
  ageDistribution: Pair[];
  recentlyConnected: LiveItem[];
  recentlyDisconnected: LiveItem[];
  highCpu: LiveItem[];
  highRam: LiveItem[];
  lowDisk: LiveItem[];
  commErrors: LiveItem[];
  latestChanges: HardwareChange[];
  activeAlerts: Alert[];
  alertCounts: Record<string, number>;
  recentEvents: AgentEvent[];
  fleet: { avgCpuPercent: number; avgRamPercent: number; reportingAgents: number };
}

export interface InventoryRow {
  agentId: string;
  hostname: string;
  displayName: string;
  platform: string;
  osName: string;
  osEdition: string;
  osVersion: string;
  arch: string;
  uptimeSeconds: number;
  cpuManufacturer: string;
  cpuBrand: string;
  cpuPhysicalCores: number;
  cpuLogicalCores: number;
  ramBytes: number;
  ramType: string;
  gpuNames: string;
  gpuManufacturers: string;
  storageTotalBytes: number;
  storageFreeBytes: number;
  diskCount: number;
  diskMedia: string;
  volumeCount: number;
  motherboard: string;
  systemModel: string;
  systemManufacturer: string;
  primaryIp: string;
  primaryMac: string;
  ips: string;
  monitorCount: number;
  softwareCount: number;
  adapterCount: number;
  snapshotId: number;
  lastInventoryAt?: string;
  lastHeartbeatAt?: string;
  status: string;
  groupName: string;
  location: string;
  isLaptop: boolean;
  agentVersion: string;
  enrolledAt: string;
  installDate?: string;
  online: boolean;
}

export interface PerfSample {
  at: string;
  cpuPercent: number;
  memUsedBytes: number;
  memTotalBytes: number;
  memPercent: number;
  swapUsedBytes?: number;
  diskReadBps?: number;
  diskWriteBps?: number;
  netRxBps?: number;
  netTxBps?: number;
  load1?: number;
}

export interface PerfResponse {
  range: string;
  bucket: string;
  from: string;
  to: string;
  samples: PerfSample[];
}

export interface MemoryModule {
  slot: string;
  sizeBytes: number;
  speedMHz?: number;
  type?: string;
  manufacturer?: string;
  partNumber?: string;
  serial?: string;
}

export interface Inventory {
  collectedAt: string;
  hash: string;
  host: {
    hostname: string;
    platform: string;
    platformFamily?: string;
    domain?: string;
    manufacturer?: string;
    model?: string;
    serial?: string;
    uuid?: string;
    chassisType?: string;
    isVirtual?: boolean;
    agentVersion?: string;
  };
  os: {
    name: string;
    edition?: string;
    version?: string;
    build?: string;
    kernel?: string;
    arch: string;
    installDate?: string;
    bootTime?: string;
    uptimeSeconds: number;
    timezone?: string;
    activationState?: string;
    defenderEnabled?: boolean;
    packageManager?: string;
    hardwareModel?: string;
    appleSilicon?: boolean;
    installedUpdates?: { id: string; description?: string; installedAt?: string }[];
  };
  cpu: {
    manufacturer?: string;
    brand: string;
    family?: string;
    architecture: string;
    socket?: string;
    physicalCores: number;
    logicalCores: number;
    baseFrequencyMHz?: number;
    maxFrequencyMHz?: number;
    cacheKB?: number;
    capabilities?: string[];
  };
  memory: {
    totalBytes: number;
    modulesSupported: boolean;
    modules?: MemoryModule[];
  };
  gpus?: { name: string; vendor?: string; vramBytes?: number; driverVersion?: string; resolution?: string }[];
  motherboard?: { manufacturer?: string; product?: string; version?: string; serial?: string };
  bios?: { vendor?: string; version?: string; releaseDate?: string };
  physicalDisks?: {
    model: string;
    serial?: string;
    sizeBytes: number;
    mediaType?: string;
    busType?: string;
    health?: string;
    temperatureC?: number;
    powerOnHours?: number;
  }[];
  volumes?: {
    mountPoint: string;
    label?: string;
    fileSystem?: string;
    totalBytes: number;
    freeBytes: number;
    usedBytes?: number;
  }[];
  networkAdapters?: {
    name: string;
    mac?: string;
    ipv4?: string[];
    ipv6?: string[];
    speedMbps?: number;
    isWireless?: boolean;
    gateway?: string;
  }[];
  monitors?: { model?: string; manufacturer?: string; serial?: string; width?: number; height?: number }[];
  batteries?: {
    name: string;
    chargePercent?: number;
    designCapacitymWh?: number;
    fullCapacitymWh?: number;
    cycleCount?: number;
    health?: string;
  }[];
  audioDevices?: { name: string; manufacturer?: string }[];
  usbDevices?: { name: string; vendorId?: string; productId?: string; manufacturer?: string }[];
  bluetoothAdapters?: { name: string; mac?: string; vendor?: string }[];
  opticalDrives?: { name: string; model?: string }[];
  printers?: { name: string; driver?: string; port?: string; isDefault?: boolean }[];
  software?: { name: string; version?: string; publisher?: string; installDate?: string }[];
  collectionErrors?: { category: string; error: string }[];
}

export interface SoftwareChange {
  name: string;
  publisher?: string;
  change: "added" | "removed" | "upgraded" | "downgraded";
  fromVersion?: string;
  toVersion?: string;
}

export interface SoftwareDiff {
  agentId: string;
  fromSnapshot: number;
  toSnapshot: number;
  fromCollectedAt: string;
  toCollectedAt: string;
  added: number;
  removed: number;
  upgraded: number;
  downgraded: number;
  changes: SoftwareChange[] | null;
  available: boolean;
  message?: string;
}

export interface Snapshot {
  id: number;
  collectedAt: string;
  receivedAt?: string;
  hash: string;
  reason?: string;
}

export interface EnrollmentRequest {
  id: number;
  agentId: string;
  machineId: string;
  hostname: string;
  platform: string;
  osName?: string;
  osVersion?: string;
  arch?: string;
  agentVersion?: string;
  remoteAddr?: string;
  requestedAt: string;
  decision: string;
}

export interface AgentConfig {
  heartbeatIntervalSec: number;
  metricsIntervalSec: number;
  inventoryIntervalSec: number;
  collectSoftware: boolean;
  collectMonitors: boolean;
  collectUsb: boolean;
  collectPrinters: boolean;
  collectProcesses: boolean;
  collectPerCoreCpu: boolean;
  collectTemperatures: boolean;
  collectInstalledApps: boolean;
  collectWindowsUpdates: boolean;
  collectLoggedInUser: boolean;
  configVersion: number;
}

export interface SettingsResponse {
  settings: Record<string, string>;
  agentConfig: Record<string, unknown>;
  alertRules: AlertRule[];
  serverInfo: Record<string, unknown>;
}

export interface Scan {
  id: number;
  startedAt: string;
  finishedAt?: string;
  target: string;
  method?: string;
  hostsFound: number;
  hostsScanned: number;
  status: string;
  message?: string;
  startedBy?: string;
}

export interface DiscoveredDevice {
  id: number;
  scanId?: number;
  ip: string;
  mac?: string;
  hostname?: string;
  vendor?: string;
  openPorts?: string;
  source: string;
  agentPresent: boolean;
  agentId?: string;
  agentHostname?: string;
  agentStatus?: string;
  firstSeen: string;
  lastSeen: string;
}
