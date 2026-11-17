# PC Inventory Manager

Self-hosted IT inventory and monitoring for a home or small-office LAN.

- **Server** (Go, single binary): REST API, SQLite storage, embedded web dashboard, SSE live updates, hardware-change detection, performance history, alerts, reports (CSV/PDF), backups.
- **Agent** (Go, single binary): Windows, macOS and Linux. Collects real hardware, OS, network and performance data from the host and reports it to the server over **outbound** HTTP(S) only.

> Status: Phase 1 is working end to end on Windows — server, agent enrollment/approval, heartbeats, metrics, inventory, change detection, dashboard and reports. The macOS and Linux collectors are implemented but have not yet been run on real hardware.

## Quick start (Windows)

```powershell
# 1. Start the server (it creates data\pcim.db and an initial admin password)
.\bin\pcim-server.exe
#    Dashboard: http://127.0.0.1:8080
#    Initial admin password: data\initial-admin-password.txt

# 2. On the machine to monitor, enroll the agent
.\bin\pcim-agent.exe enroll --server 192.168.1.100:8080

# 3. Approve it in the dashboard (Enrollments), then run it
.\bin\pcim-agent.exe run
```

The first `enroll` prints `waiting for administrator approval`. Approve the computer in the dashboard, then run `enroll` again (or just `run` — the agent keeps polling until it is approved and then collects its token). `run` sends inventory at start-up, metrics and heartbeats every 30 s, and buffers reports on disk whenever the server is unreachable.

## Building from source

Requires Go 1.24+ and Node 20+ (Node only for rebuilding the dashboard).

```bash
go build -o bin/pcim-server.exe ./cmd/server
go build -o bin/pcim-agent.exe  ./cmd/agent
```

Cross-compiling the agent:

```bash
GOOS=darwin GOARCH=arm64 go build -o bin/pcim-agent-macos-arm64 ./cmd/agent
GOOS=darwin GOARCH=amd64 go build -o bin/pcim-agent-macos-amd64 ./cmd/agent
GOOS=linux  GOARCH=amd64 go build -o bin/pcim-agent-linux-amd64 ./cmd/agent
```

The dashboard is a self-contained HTML file embedded in the server (`internal/server/webui/dist/index.html`); no Node build step is needed for the current version.

## Configuration

Server — environment variables (all optional):

| Variable | Default | Meaning |
|---|---|---|
| `PCIM_LISTEN_HOST` | `127.0.0.1` | Bind address. Use `0.0.0.0` to accept LAN agents. |
| `PCIM_PORT` | `8080` | Listen port |
| `PCIM_DATA_DIR` | `./data` | Data directory |
| `PCIM_DB_PATH` | `<data>/pcim.db` | SQLite database |
| `PCIM_ADMIN_PASSWORD` | generated | Initial admin password |
| `PCIM_OFFLINE_THRESHOLD_SEC` | `90` | Mark an agent offline after this long without a heartbeat |
| `PCIM_RETENTION_DAYS` | `30` | Performance history retention |
| `PCIM_TLS_CERT` / `PCIM_TLS_KEY` | – | Serve HTTPS |
| `PCIM_TLS_SELF_SIGNED` | `false` | Generate a self-signed certificate |

Agent — `agent.json` (default `C:\ProgramData\PCInventoryManager\Agent\agent.json` on Windows, `/etc/pcim-agent/agent.json` on Linux, `/usr/local/etc/pcim-agent/agent.json` on macOS):

| Key | Default | Meaning |
|---|---|---|
| `serverUrl` | – | Server base URL |
| `heartbeatSec` | 30 | Heartbeat interval |
| `metricsSec` | 30 | Performance sample interval |
| `inventorySec` | 21600 | Full inventory interval (6 h) |
| `collect*` | true | Toggles for software, monitors, USB, printers, processes, temperatures |

Intervals are server-controlled: values set in the dashboard override the local file on the next heartbeat.

## Agent commands

```
pcim-agent run       [--config PATH] [--log-level debug]
pcim-agent enroll    --server ADDR [--name NAME] [--insecure]
pcim-agent status    [--config PATH]
pcim-agent inventory [--json]
pcim-agent config    --server ADDR | --heartbeat N | --metrics N | --inventory N | --show
pcim-agent service   install|uninstall|start|stop|restart|status
pcim-agent version
```

`service install` registers the Windows service (`pcim-agent`), a systemd unit on Linux or a launchd daemon on macOS, with automatic restart.

## Security model

- Agents are approved by an administrator before they can report. Each agent gets its own random token; tokens can be rotated or revoked per agent, and every action is audited.
- Only outbound connections from agents; the server never connects to a computer.
- The server binds to loopback by default — it is not reachable from the network until you set `PCIM_LISTEN_HOST`.
- The agent never collects passwords, browser history, personal files or Wi-Fi credentials.
- Remote actions are restricted to: inventory refresh, token rotation, configuration update.

## Documentation

- [INSTALLATION](docs/INSTALLATION.md) — server and agent install on each OS
- [SERVER_SETUP](docs/SERVER_SETUP.md) — configuration, TLS, backups, retention
- [WINDOWS_AGENT](docs/WINDOWS_AGENT.md) · [MACOS_AGENT](docs/MACOS_AGENT.md) · [LINUX_AGENT](docs/LINUX_AGENT.md)
- [CONFIGURATION](docs/CONFIGURATION.md) — every setting
- [API_DOCUMENTATION](docs/API_DOCUMENTATION.md) — REST endpoints
- [SECURITY](docs/SECURITY.md) — threat model and hardening
- [TROUBLESHOOTING](docs/TROUBLESHOOTING.md)
- [DEVELOPMENT](docs/DEVELOPMENT.md)

## What the agent reports

Hardware: CPU (model, cores, frequencies), memory modules (size, type, speed, slot), GPUs, motherboard/BIOS, physical disks (model, serial, capacity, media type, health, SMART temperature), volumes, monitors, batteries (design/full capacity, cycles, health), audio, USB, Bluetooth, printers, optical drives.
Software: installed applications, Windows updates/hotfixes, Defender status.
System: OS build, install date, uptime, timezone, activation, network adapters (IP, MAC, gateway, DNS, link speed), logged-in user.

Anything a platform cannot provide is reported as *unsupported* rather than as zero.
