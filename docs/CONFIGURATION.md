# Configuration reference

Two configuration surfaces: server settings (environment variables) and agent settings (`agent.json`). Server-controlled agent settings win over the agent's local file.

## Server

All variables are optional. Each accepts an optional `PCIM_` prefix (`PORT` ≡ `PCIM_PORT`). They can also be written to a `config.env` file in the server's working directory as `KEY=value` lines.

### Network

| Variable | Default | Description |
|---|---|---|
| `LISTEN_HOST` | `127.0.0.1` | Bind address. `0.0.0.0` = all interfaces, `::` = IPv6. |
| `PORT` | `8080` | Listen port |
| `PUBLIC_URL` | derived | Address advertised to agents, e.g. behind NAT or a reverse proxy |
| `TRUST_PROXY` | `false` | Trust `X-Forwarded-For` / `X-Real-IP` from a local reverse proxy |
| `ALLOWED_CIDRS` | – | Comma-separated client ranges, e.g. `192.168.1.0/24,10.0.0.0/8`. Empty accepts all. |

### Storage

| Variable | Default | Description |
|---|---|---|
| `DATA_DIR` | `./data` | Database, backups, bootstrap password |
| `DB_PATH` | `<DATA_DIR>/pcim.db` | SQLite file (WAL mode) |
| `BACKUP_DIR` | `<DATA_DIR>/backups` | Backup destination |
| `RETENTION_DAYS` | `30` | How long performance history is kept |
| `RAW_SAMPLE_HOURS` | `48` | Raw samples kept before hourly rollup |

### Security

| Variable | Default | Description |
|---|---|---|
| `ADMIN_USER` | `admin` | Bootstrap administrator (only used when no admin exists) |
| `ADMIN_PASSWORD` | random | Bootstrap password (also settable via `-reset-admin-password`) |
| `SESSION_TTL_HOURS` | `168` | Dashboard session lifetime |
| `TLS_CERT` / `TLS_KEY` | – | PEM files; enables HTTPS |
| `TLS_SELF_SIGNED` | `false` | Generate a self-signed certificate on first start |

### Agent defaults

Sent to agents and applied at their next heartbeat. Also editable per server in the dashboard.

| Variable | Default | Description |
|---|---|---|
| `HEARTBEAT_INTERVAL_SEC` | `30` | Allowed range 10–3600 |
| `METRICS_INTERVAL_SEC` | `30` | Allowed range 5–3600 |
| `INVENTORY_INTERVAL_SEC` | `21600` | Allowed range 300–604800 (5 min – 7 days) |
| `OFFLINE_THRESHOLD_SEC` | `90` | Time without a heartbeat before a computer is offline |

### Notifications

| Variable | Description |
|---|---|
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Email delivery |
| `SMTP_TLS` | `starttls`, `tls` or `none` |
| `ALERT_EMAIL_TO` | Comma-separated recipients |
| `TELEGRAM_TOKEN`, `TELEGRAM_CHAT_ID` | Telegram delivery via bot API |

### Logging

| Variable | Default | Description |
|---|---|---|
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FILE` | – | Also write to this file (rotation is left to the OS/log shipper) |

## Agent

`agent.json` location: `C:\ProgramData\PCInventoryManager\Agent\agent.json` (Windows), `/etc/pcim-agent/agent.json` (Linux), `/usr/local/etc/pcim-agent/agent.json` (macOS). Override with `--config` or `PCIM_AGENT_CONFIG`.

| Key | Default | Description |
|---|---|---|
| `serverUrl` | – | Server base URL, normalized by `enroll` |
| `serverName` | – | Reported by the server, informational |
| `agentId` | – | Assigned at enrollment |
| `machineId` | derived | Stable machine identity: `MachineGuid` (Windows), `IOPlatformUUID` (macOS), `/etc/machine-id` (Linux) |
| `agentToken` | – | Secret token issued at approval |
| `hostname` | system | Reported host name |
| `displayName` | – | Friendly name shown in the dashboard |
| `heartbeatSec` | `30` | Heartbeat interval |
| `metricsSec` | `30` | Performance sample interval |
| `inventorySec` | `21600` | Full inventory interval (6 hours) |
| `collectSoftware` | `true` | Installed applications |
| `collectInstalledApps` | `true` | Alias toggle used with `collectSoftware` |
| `collectMonitors` | `true` | Attached displays |
| `collectUsb` | `true` | USB devices |
| `collectPrinters` | `true` | Printers |
| `collectProcesses` | `true` | Process count in metrics |
| `collectPerCoreCpu` | `true` | Per-core CPU percentages |
| `collectTemperatures` | `true` | Temperature sensors where available |
| `collectWindowsUpdates` | `true` | Update/hotfix history (Windows) |
| `collectLoggedInUser` | `false` | Currently logged-in user name — off by default for privacy |
| `configVersion` | `0` | Incremented when the server changes configuration |
| `insecureTls` | `false` | Skip certificate verification (self-signed LAN servers only) |
| `logLevel` | `info` | `debug`, `info`, `warn`, `error` |

Changing the server address through `pcim-agent config --server` clears the stored `agentId` and `agentToken`, because a token is only valid for the server that issued it.

### Changing settings

```bash
pcim-agent config --heartbeat 60 --metrics 60 --inventory 43200
pcim-agent config --show
```

Prefer the dashboard for fleets: settings there apply to the default configuration for all agents, and per-computer overrides are kept in `config_json` on the agent record.

## Runtime behaviour worth knowing

- **Offline buffer**: when a report cannot be delivered, it is written to a local queue (default cap 5000 items). Metrics overflow first; inventory reports are kept preferentially. The queue flushes oldest-first every 45 seconds when the server is reachable.
- **Retries**: enrollment retries every 20 seconds indefinitely; heartbeats, metrics and inventory simply continue on their own schedule, so recovery needs no manual action.
- **Token rotation**: a rotated token keeps working for 10 minutes so a report in flight is not lost.
- **Clock skew**: metric timestamps more than 5 minutes in the future are replaced with server time.
- **Inventory fingerprint**: a hash over stable hardware fields only, so fluctuating counters never register as hardware changes.

## Example configurations

Small home lab (3–5 machines, low overhead):

```bash
PCIM_LISTEN_HOST=0.0.0.0 \
PCIM_PORT=8080 \
PCIM_METRICS_INTERVAL_SEC=60 \
PCIM_INVENTORY_INTERVAL_SEC=43200 \
PCIM_RETENTION_DAYS=90 \
./pcim-server
```

Office server behind a reverse proxy with HTTPS:

```bash
PCIM_LISTEN_HOST=127.0.0.1 \
PCIM_PORT=8080 \
PCIM_PUBLIC_URL=https://inventory.example.lan \
PCIM_TRUST_PROXY=true \
PCIM_ALLOWED_CIDRS=192.168.10.0/24 \
./pcim-server
```

Agent on a laptop that travels:

```bash
pcim-agent config --server https://inventory.example.lan --metrics 120 --inventory 43200
```
