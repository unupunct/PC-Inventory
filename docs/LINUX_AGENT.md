# Linux agent

Supports any systemd distribution: Ubuntu, Debian, Fedora, RHEL, Rocky, AlmaLinux, openSUSE, Arch, and others. The binary is static (`CGO_ENABLED=0`), so it runs on the matching architecture without extra libraries.

> Status: the Linux collector is implemented and compiles, but has not yet been validated on real hardware. Treat the first run as a test and report anything missing — see [TROUBLESHOOTING](TROUBLESHOOTING.md).

## Install

```bash
sudo install -m 0755 pcim-agent-linux-amd64 /usr/local/bin/pcim-agent
sudo /usr/local/bin/pcim-agent enroll --server 192.168.1.100:8080
sudo /usr/local/bin/pcim-agent service install
```

This writes `/etc/systemd/system/pcim-agent.service`, enables it and starts it:

```ini
[Service]
ExecStart=/usr/local/bin/pcim-agent run --config /etc/pcim-agent/agent.json
Restart=always
RestartSec=10
User=root
```

Approve the machine in the dashboard (Enrollments), then run `enroll` again if the first attempt reported `waiting for administrator approval`.

## Verify

```bash
pcim-agent status
systemctl status pcim-agent
journalctl -u pcim-agent -f
```

## Optional packages for richer data

Installing these improves detail; the agent works without them and reports what it cannot read as `unknown` or omits it entirely.

| Package | Adds |
|---|---|
| `dmidecode` | Memory module details (slot, size, speed, part number) |
| `smartmontools` | Disk health, SMART temperature, power-on hours |
| `lm-sensors` | CPU/board temperature sensors |
| `pciutils` | GPU identification fallback via `lspci` |
| `alsa-utils` | Audio device names via `aplay` |

```bash
sudo apt install dmidecode smartmontools lm-sensors pciutils alsa-utils
```

`dmidecode` and full SMART data need root, which the service has.

## What is collected on Linux

| Category | Source |
|---|---|
| Distribution, version, kernel, architecture | `/etc/os-release`, `/proc/sys/kernel/*` |
| Package manager and container/systemd detection | filesystem probes, `/run/systemd/system` |
| Host manufacturer, model, serial, chassis type | `/sys/devices/virtual/dmi/id/*` |
| CPU model, cores, threads, frequency | `/proc/cpuinfo` via gopsutil |
| Memory modules | `dmidecode -t memory` (root) |
| GPU, VRAM, driver version | `/sys/class/drm/card*/device` and `lspci` |
| Motherboard and BIOS | `/sys/devices/virtual/dmi/id/board_*`, `bios_*` |
| Disks: model, serial, capacity, rotation, bus | `lsblk -P -b -d` (rotational flag decides SSD/HDD/NVMe) |
| Disk health | `smartctl -H -A -j` when installed |
| Monitors and resolution | `/sys/class/drm/card*-*/status` and `modes` |
| Battery | `/sys/class/power_supply/BAT*` (capacity, status, design/full charge, cycles) |
| USB devices | `/sys/bus/usb/devices/*` |
| Bluetooth adapters | `/sys/class/bluetooth/*` |
| Printers | `lpstat` |
| Installed software | `dpkg-query`, `rpm -qa`, `pacman -Q`, `apk info`, plus `flatpak list` |
| Network | gopsutil, `/sys/class/net/*/speed`, `ip route`, `/etc/resolv.conf` |
| Wi-Fi SSID (never the key) | `iwgetid` |

## Containers

Inside a Docker or LXC container the agent only sees the container's view: the host's disks, modules and firmware are not visible. Run the agent on the host, not in a container, for real hardware inventory.

On a VM, expect virtual hardware: `QEMU`/`VMware`/`VirtualBox` model names, virtual disks, no battery and possibly no GPU.

## Uninstall

```bash
sudo systemctl disable --now pcim-agent
sudo /usr/local/bin/pcim-agent service uninstall
sudo rm -rf /etc/pcim-agent
```

Then delete the computer in the dashboard.
