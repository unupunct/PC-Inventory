# macOS agent

Supports macOS 12 and newer on both Intel and Apple Silicon. The binary is universal per architecture; pick the one matching the Mac (`pcim-agent-macos-arm64` for Apple Silicon, `pcim-agent-macos-amd64` for Intel) or run either under Rosetta.

> Status: the macOS collector is implemented and compiles, but has not yet been validated on real Mac hardware. Treat the first run as a test and report anything missing — see [TROUBLESHOOTING](TROUBLESHOOTING.md).

## Install

```bash
sudo mkdir -p /usr/local/bin
sudo cp pcim-agent-macos-arm64 /usr/local/bin/pcim-agent
sudo chmod +x /usr/local/bin/pcim-agent

sudo /usr/local/bin/pcim-agent enroll --server 192.168.1.100:8080
sudo /usr/local/bin/pcim-agent service install
```

The service is a launchd daemon (`com.pcim.agent`) that starts at boot and restarts if it exits. Configuration lives in `/usr/local/etc/pcim-agent/agent.json`; logs go to `/Library/Logs/PCInventoryManager/`.

Approve the Mac in the dashboard (Enrollments), then run `enroll` again if the first attempt reported `waiting for administrator approval`.

## Verify

```bash
pcim-agent status
sudo launchctl list | grep pcim
sudo tail -f /Library/Logs/PCInventoryManager/pcim-agent.out.log
```

## What is collected on macOS

| Category | Source |
|---|---|
| Model identifier, chip, cores, memory | `sysctl`, `system_profiler SPHardwareDataType` |
| Serial number, platform UUID | `ioreg -c IOPlatformExpertDevice` |
| OS name, version, build, kernel | `sw_vers`, `sysctl` |
| Apple Silicon detection | `sysctl hw.optional.arm64` (reported explicitly, not inferred) |
| Memory modules | `system_profiler SPMemoryDataType` on Intel; Apple Silicon uses unified memory and reports `modulesSupported: false` |
| GPU / Metal support | `system_profiler SPDisplaysDataType` |
| Disks, media type, SMART verified status | `system_profiler SPNVMeDataType` / `SPSerialATADataType` |
| Monitors and resolutions | `system_profiler SPDisplaysDataType` |
| Battery charge, cycles, design/full capacity | `pmset -g batt`, `ioreg -c AppleSmartBattery` |
| Applications | `system_profiler SPApplicationsDataType` |
| Printers | `lpstat` |
| USB and Bluetooth devices | `system_profiler SPUSBDataType` / `SPBluetoothDataType` |
| DNS servers | `scutil --dns` |
| Wi-Fi SSID (no password, ever) | `networksetup -getairportnetwork` |

## Permissions

No Full Disk Access or any other privacy permission is required: every source above is readable by a normal user. Do **not** grant the agent access to Documents, Desktop or any other personal folder — it does not need them.

## Uninstall

```bash
sudo /usr/local/bin/pcim-agent service uninstall
sudo rm -rf /usr/local/etc/pcim-agent /Library/Logs/PCInventoryManager
```

Then delete the computer in the dashboard.

## Notes

- `pcim-agent run` works in a terminal without root for testing; the service runs as root to read all hardware details.
- On laptops, the agent keeps reporting on battery; it is deliberately not throttled.
- If the Mac is asleep, it is offline in the dashboard, which is expected — history resumes when it wakes.
