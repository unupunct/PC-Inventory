# Server setup

## Configuration sources

Order of precedence (later wins):

1. Built-in defaults
2. `config.env` in the working directory (`KEY=value` lines, `#` comments)
3. Environment variables

All keys accept an optional `PCIM_` prefix; `PORT` and `PCIM_PORT` are equivalent.

## Settings

| Key | Default | Description |
|---|---|---|
| `LISTEN_HOST` | `127.0.0.1` | Bind address. `0.0.0.0` to accept LAN agents, `::` for IPv6. |
| `PORT` | `8080` | HTTP(S) port |
| `PUBLIC_URL` | – | URL agents should use, when behind a proxy or port forward |
| `DATA_DIR` | `./data` | Directory for database, backups and the initial password file |
| `DB_PATH` | `<DATA_DIR>/pcim.db` | SQLite database file |
| `BACKUP_DIR` | `<DATA_DIR>/backups` | Where scheduled backups are written |
| `TLS_CERT` / `TLS_KEY` | – | PEM certificate and key; enables HTTPS |
| `TLS_SELF_SIGNED` | `false` | Generate a self-signed certificate on first start |
| `ADMIN_USER` | `admin` | Bootstrap administrator name |
| `ADMIN_PASSWORD` | generated | Bootstrap administrator password |
| `SESSION_TTL_HOURS` | `168` | Dashboard session lifetime (7 days) |
| `OFFLINE_THRESHOLD_SEC` | `90` | An agent is offline after this long without a heartbeat |
| `HEARTBEAT_INTERVAL_SEC` | `30` | Default heartbeat interval given to agents |
| `METRICS_INTERVAL_SEC` | `30` | Default metrics interval |
| `INVENTORY_INTERVAL_SEC` | `21600` | Default full inventory interval (6 h) |
| `RETENTION_DAYS` | `30` | Performance history kept (raw samples roll up after 48 h) |
| `RAW_SAMPLE_HOURS` | `48` | How long raw samples are kept before hourly rollup |
| `ALLOWED_CIDRS` | – | Comma-separated allow list, e.g. `192.168.1.0/24`. Empty = any client |
| `SMTP_HOST`/`SMTP_PORT`/`SMTP_USER`/`SMTP_PASSWORD`/`SMTP_FROM`/`SMTP_TLS` | – | Email alerts |
| `ALERT_EMAIL_TO` | – | Comma-separated recipients |
| `TELEGRAM_TOKEN` / `TELEGRAM_CHAT_ID` | – | Telegram alerts |
| `TRUST_PROXY` | `false` | Trust `X-Forwarded-For` for client IPs |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FILE` | – | Also write logs to this file |

Server-side agent settings (intervals, collection toggles) are also editable in the dashboard and override each agent's local file on its next heartbeat.

## Backups

A backup is a consistent copy of the SQLite database produced by the server (safe while it runs). Backups are written to `BACKUP_DIR` on the schedule configured in Settings, and you can force one with Settings → Backup now (or `POST /api/backup`).

Restore: stop the server, replace `pcim.db` with the backup file, start the server.

## TLS

Two options:

```bash
PCIM_TLS_CERT=/etc/ssl/pcim.crt PCIM_TLS_KEY=/etc/ssl/pcim.key ./pcim-server
```

or let the server generate a self-signed certificate:

```bash
PCIM_TLS_SELF_SIGNED=true PCIM_LISTEN_HOST=0.0.0.0 ./pcim-server
```

With a self-signed certificate, agents must be enrolled with `--insecure` (or the certificate installed in the OS trust store). Prefer a certificate from your own CA or Let's Encrypt when the server has a DNS name.

## Data model

- `agents` — one row per computer, with status, token hashes, last-seen timestamps
- `inventory_snapshots` — full inventory JSON per report, plus a fingerprint hash
- `inventory_current` — the latest inventory per computer, used by the dashboard
- `hardware_changes` — diffs between consecutive snapshots
- `performance_samples` / rollups — CPU, memory, disk, network and volume usage over time
- `alerts` — rule evaluations and acknowledgements
- `audit` — administrative actions
- `discovery` — LAN scan results and manually added devices

The database uses WAL mode; do not copy `pcim.db` while the server is running — use a backup instead.

## Running headless

The server has no GUI dependency. Under systemd, launchd, `nssm` or as a Windows scheduled task at boot, it serves the dashboard and API for other computers on the network.
