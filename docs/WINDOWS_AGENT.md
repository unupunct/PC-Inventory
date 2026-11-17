# Windows agent

Supports Windows 10, Windows 11 and Windows Server 2016 or newer. The agent is a single 64-bit executable with no installer framework and no runtime to install.

## Install

Save `pcim-agent.exe` somewhere permanent, for example `C:\Program Files\PCInventoryManager\`, and open an **Administrator** PowerShell:

```powershell
cd "C:\Program Files\PCInventoryManager"
.\pcim-agent.exe enroll --server 192.168.1.100:8080
.\pcim-agent.exe service install
```

The service is registered as `pcim-agent`, starts automatically at boot, and is configured to restart after a crash (5 s, then 10 s, then 30 s, reset after a day without failure).

Without Administrator rights the agent still runs, but only in the foreground: `pcim-agent run`. A foreground agent stops when the window closes or the user signs out.

## Verify

```powershell
.\pcim-agent.exe status
.\pcim-agent.exe service status
Get-EventLog -LogName Application -Source pcim-agent -Newest 10
```

`status` returns JSON including `connected`, `lastError`, `buffered` and the effective intervals.

## Configuration

`C:\ProgramData\PCInventoryManager\Agent\agent.json` — written by `enroll`, editable by `config`, and readable by an administrator. Typical file:

```json
{
  "serverUrl": "http://192.168.1.100:8080",
  "machineId": "27c4c2dda984e5ac744f947c2c8a2b83",
  "agentToken": "…",
  "heartbeatSec": 30,
  "metricsSec": 30,
  "inventorySec": 21600,
  "collectSoftware": true,
  "collectMonitors": true,
  "collectUsb": true,
  "collectPrinters": true,
  "collectTemperatures": true,
  "insecureTls": false,
  "logLevel": "info"
}
```

Values set from the dashboard override these on the next heartbeat. Logs and buffered reports live beside it in `buffer\` and `logs\`.

## What is collected on Windows

| Category | Source |
|---|---|
| Host name, manufacturer, model, serial, chassis | `Win32_ComputerSystem`, `Win32_ComputerSystemProduct`, `Win32_SystemEnclosure` (CIM) |
| OS name, edition, build, install date, uptime, timezone | Registry + `Win32_OperatingSystem` |
| Activation state | Software Licensing Product |
| CPU model, cores, threads, clock, cache, socket | `Win32_Processor` |
| Memory modules: slot, size, speed, type, part/serial | `Win32_PhysicalMemory`, `Win32_PhysicalMemoryArray` |
| GPU, VRAM, driver version | `Win32_VideoController` |
| Motherboard and BIOS | `Win32_BaseBoard`, `Win32_BIOS` |
| Physical disks: model, serial, capacity, media, health, temperature, power-on hours | `Get-PhysicalDisk`, `Get-StorageReliabilityCounter` (falls back to `Win32_DiskDrive`) |
| Volumes and free space | gopsutil + `Win32_LogicalDisk` |
| Monitors: model, serial, resolution | `WmiMonitorID`, `WmiMonitorConnectionParams` |
| Batteries: charge, design/full capacity, cycles, health | `Win32_Battery`, `BatteryStaticData`, `BatteryFullChargedCapacity` |
| Audio, USB, Bluetooth, optical, printers | `Win32_SoundDevice`, PnP devices, `Win32_CDROMDrive`, `Get-Printer` |
| Installed applications | Registry uninstall keys (32- and 64-bit) |
| Windows updates | `Get-HotFix`, Windows Update history |
| Defender status | `Get-MpComputerStatus` |
| Network adapters | gopsutil plus `netstat -rn` and DNS client configuration |

Collection runs through PowerShell with `-NoProfile -NonInteractive`, so a user profile, execution policy or console window cannot interfere. No elevated permissions are required for inventory; some disk health detail is richer when escalated.

Performance is sampled with gopsutil, not with WMI queries, so a 30-second interval costs almost nothing.

## Firewall and network

The agent only makes **outbound** connections. No inbound firewall rule is needed on any monitored computer. If the machine sits behind a proxy, the agent does not support one at present — point it directly at the server.

## Uninstall

```powershell
cd "C:\Program Files\PCInventoryManager"
.\pcim-agent.exe service uninstall
Remove-Item -Recurse "C:\ProgramData\PCInventoryManager"
```

Then delete the computer in the dashboard.

## Notes for Windows Server

On Server Core the same binary works; PowerShell 5.1 is present by default. `Get-PhysicalDisk` needs Windows Server 2012 or newer. Some server SKUs expose no battery, monitor or Bluetooth data — those sections are simply empty, and never reported as zero.
