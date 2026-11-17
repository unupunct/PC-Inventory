# Troubleshooting

Start every investigation with the agent's own view:

```bash
pcim-agent status      # enrolled? connected? last error? buffered reports?
pcim-agent inventory   # what the agent would send, straight from this machine
```

## The computer never appears in the dashboard

**`enroll` says `waiting for administrator approval`** — this is normal on first install. Sign in to the dashboard, open **Enrollments**, approve the computer, then run `pcim-agent enroll` again (or just `pcim-agent run`, which keeps polling). If auto-approve is enabled in Settings, the agent should be approved immediately.

**`error: server address is required`** — `--server` was not given. Example: `pcim-agent enroll --server 192.168.1.100:8080`.

**`connection refused`** —

1. Is the server running? `curl http://<server>:8080/api/system` from the agent machine.
2. Is it bound to the network? The default `127.0.0.1` only accepts local connections. Start it with `PCIM_LISTEN_HOST=0.0.0.0`.
3. Is the firewall open? `New-NetFirewallRule -DisplayName "PCIM" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Private` (Windows) or `sudo ufw allow 8080/tcp` (Linux).
4. Are both machines on the same subnet, with no guest-network isolation?

**`context deadline exceeded` / timeouts** — usually a firewall dropping packets silently (as opposed to refusing them). Check the server machine's firewall and any router isolation setting.

**`x509: certificate signed by unknown authority`** — the server uses a self-signed certificate. Either install that certificate in the agent machine's trust store, or enroll with `--insecure` (trusted LAN only). `--insecure` is stored in `agent.json` and survives restarts.

**`protocol version mismatch`** — server and agent are from incompatible releases. Upgrade both to the same version.

## The computer shows as offline

An agent is offline after `OFFLINE_THRESHOLD_SEC` (default 90 seconds) without a heartbeat.

- `pcim-agent status` shows `"connected": false` with a `lastError` — read that error, it is the real cause.
- The agent was disabled, revoked or rejected in the dashboard: `"agent is disabled"` / `"invalid agent token"`. Re-enable or approve it.
- The service is not running: `pcim-agent service status`; on Linux `systemctl status pcim-agent`; on macOS `launchctl list | grep pcim`.
- The machine was asleep or shut down — offline is expected then, and history stays intact.
- Clock skew: a laptop waking from sleep with a wrong clock is rejected as `invalid agent token` only if the token is genuinely wrong; timestamps five minutes in the future are clamped server-side.

## Hardware data is missing or empty

The agent only reports what the OS exposes; nothing is invented. Common cases:

| Field | Why it can be empty |
|---|---|
| Memory modules (Windows) | `Win32_PhysicalMemory` needs no elevation, but virtual machines often report no slots. On Linux, module details require `dmidecode`, which needs root — run the service as root or accept `modulesSupported: false`. |
| GPU (Windows) | Only present devices with a driver appear. Headless VMs report none. |
| Physical disk health | `Get-PhysicalDisk`/SMART needs Windows 8+ and usually elevation for full detail; on Linux `smartctl` must be installed and permitted. Health is `unknown` rather than `ok` when unreadable. |
| Monitors | Only monitors that answer EDID are listed; remote sessions show none. |
| Battery | Desktops have none — `battery` is omitted, not zero. |
| Temperatures | Many desktop boards expose no ACPI thermal zone. Listed under `unsupported` when the platform says it cannot provide it. |
| Installed software (Linux) | Needs `dpkg-query`, `rpm` or `pacman` on PATH; a minimal container may have none. |

If a field is present but zero, that is a bug — report it with the output of `pcim-agent inventory`.

## "No inventory received yet" for a computer that is online

Metrics arrived but no inventory. Inventory is sent 3 seconds after start-up and then every `inventorySec` (6 h), so:

- Wait a minute after starting the agent, or
- Click **Refresh inventory** in the dashboard (or `POST /api/agents/{id}/refresh-inventory`). The agent picks it up on its next heartbeat, within 30 seconds.

Collection on a busy machine with hundreds of installed applications can take 30–90 seconds; the report is sent only once collection completes.

## Performance charts are empty

- Samples arrive every `metricsSec` (default 30 s). The first chart point appears after roughly one interval.
- `GET /api/agents/{id}/performance?range=1h` should return samples; if it does, the data is there and the issue is in the browser.
- After `RAW_SAMPLE_HOURS` (48 h) raw samples roll up into hourly averages, so a narrow range older than two days can look coarse.
- History older than `RETENTION_DAYS` is deleted by the retention job.

## Dashboard problems

**Login fails with a correct password** — five failed attempts lock that username+IP for 5 minutes. Restarting the server clears the counter.

**Locked out permanently** — reset the administrator password:

```bash
./pcim-server -reset-admin-password
```

or delete `data/initial-admin-password.txt`'s predecessor by creating a new admin through the flag, then restart. Data is untouched.

**Live updates stop** — the `LIVE` indicator in the header turns red when the SSE connection drops; the browser reconnects automatically. A reverse proxy must not buffer `text/event-stream`; disable buffering for `/api/stream`.

**Page loads but is blank** — the dashboard is embedded in the server binary. If you replaced only part of a release, rebuild with `go build ./cmd/server`.

## Hardware changes are noisy or missing

- Only stable fields are fingerprinted. Uptime, IP addresses and free-space values are excluded, so they never trigger a change.
- Installing a GPU driver, adding RAM or swapping a disk produces an entry within one inventory cycle.
- Changes are only detected when a new inventory arrives — use **Refresh inventory** to check immediately.

## Alerts do not arrive

- Rules must be enabled (Settings → Alerts) and their thresholds exceeded.
- Each rule has a cooldown; a condition that stays true does not re-notify until it expires.
- Email requires all of `SMTP_HOST`, `SMTP_FROM` and `ALERT_EMAIL_TO`; Telegram requires both `TELEGRAM_TOKEN` and `TELEGRAM_CHAT_ID`.
- Use **Test notification** in Settings to exercise the channel independently of any rule.

## Server problems

**`bind: Only one usage of each socket address`** — another instance is already listening on that port. On Windows:

```powershell
Get-Process pcim-server | Stop-Process
netstat -ano | findstr :8080
```

**Database grows quickly** — lower `RETENTION_DAYS`, raise `METRICS_INTERVAL_SEC`, or reduce the number of agents. Verdict for a 10-computer home network: roughly 20–40 MB per month.

**Server will not start after an upgrade** — the schema migrates automatically, but a downgrade is not supported. Restore the backup you took before upgrading.

## Collecting diagnostics to share

```bash
pcim-agent status            > agent-status.json
pcim-agent inventory --json  > inventory.json
pcim-agent version           >> agent-status.json
```

On the server, the log output (stdout, or `LOG_FILE`) shows enrollment decisions, ingest errors and background job results. Redact IP addresses and hostnames you do not want to share.
