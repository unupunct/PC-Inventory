# API documentation

Base URL: `http://<server>:8080`. All request and response bodies are JSON (`Content-Type: application/json`).

## Authentication

Two independent schemes:

**Agent token** — used by agents on `/api/agent/*`:

```
Authorization: Bearer <agent token>
X-Agent-Id: <agent id>
```

Agent request bodies may be gzip-compressed (`Content-Encoding: gzip`).

**Admin session** — used by the dashboard on everything else:

```
Authorization: Bearer <session token>
```

or the `pcim_session` cookie set by the login endpoint. Sessions expire after `SESSION_TTL_HOURS` (default 7 days).

Errors are returned as `{"error": "message"}` with a matching HTTP status: `400` bad request, `401` not authenticated, `403` forbidden/disabled, `404` not found, `409` conflict, `429` throttled, `500` server error.

## Health

### `GET /api/system` — public

```json
{"name":"PC Inventory Manager","version":"1.0.0","protocol":1,"serverName":"PC Inventory Manager (host)","advertisedUrl":"http://192.168.1.100:8080","uptimeSec":512}
```

### `GET /api/health` — session required

Storage sizes, database health and background-job status.

## Agent endpoints

### `POST /api/agent/ping`

Unauthenticated reachability probe. Returns server version and protocol so an agent can verify it reached the right service.

### `POST /api/agent/enroll`

The only unauthenticated endpoint that changes state. Call it repeatedly; each call returns the current decision.

```json
{
  "machineId": "27c4c2dda984e5ac744f947c2c8a2b83",
  "hostname": "pc-01",
  "platform": "windows",
  "osName": "Windows 11 Pro",
  "osVersion": "24H2",
  "arch": "amd64",
  "agentVersion": "1.0.0",
  "protocol": 1,
  "primaryIp": "192.168.1.23",
  "primaryMac": "3C:52:82:71:29:2B"
}
```

| Status | Body | Meaning |
|---|---|---|
| `202` | `{"status":"pending","agentId":"…"}` | Recorded, waiting for approval |
| `200` | `{"status":"approved","agentId":"…","agentToken":"…","config":{…}}` | Approved; **store the token** |
| `403` | `{"status":"rejected"}` / `"revoked"` / `"disabled"` | Not allowed to report |
| `409` | `{"status":"approved","message":"…"}` | Already enrolled and the token was already collected; an admin must rotate it |

`machineId` must be stable across agent restarts — it is how a reinstalled agent re-attaches to its existing record.

### `POST /api/agent/heartbeat`

```json
{"agentId":"…","sentAt":"2026-11-16T10:00:00Z","agentVersion":"1.0.0","pendingBuffered":0,"lastError":""}
```

Response carries the command queue and any configuration change:

```json
{"ok":true,"config":{"heartbeatIntervalSec":30,"metricsIntervalSec":30,"inventoryIntervalSec":21600,"collectUsb":true,"configVersion":3},"commands":[{"type":"inventory_refresh"}]}
```

Commands are limited to `inventory_refresh`, `rotate_token` and configuration updates.

### `POST /api/agent/metrics`

```json
{
  "agentId":"…",
  "metrics":{
    "collectedAt":"2026-11-16T10:00:30Z",
    "cpu":{"usagePercent":37.5,"perCore":[12.0,44.5],"frequencyMHz":3400},
    "memory":{"totalBytes":34272202752,"usedBytes":20867223552,"usagePercent":60.9,"swapUsedBytes":933806080},
    "disks":[{"name":"Disk 0","readBytesPerSec":1048576,"writeBytesPerSec":524288,"utilizationPercent":12.5}],
    "network":[{"name":"Ethernet","rxBytesPerSec":2048,"txBytesPerSec":1024}],
    "volumes":[{"mount":"C:","totalBytes":500107862016,"usedBytes":243269632000,"usagePercent":48.6}],
    "processCount":350,
    "battery":{"percent":87,"status":"discharging"},
    "temperatures":[{"sensor":"Core 0","celsius":54}],
    "unsupported":["gpu_temperature"]
  }
}
```

Any metric the platform cannot read is either omitted or listed in `unsupported` — never sent as zero.

### `POST /api/agent/inventory`

```json
{"agentId":"…","reason":"scheduled","inventory":{ /* models.Inventory */ }}
```

Response: `{"ok":true,"changed":true,"changeCount":2}`. `changed` is true when the hardware fingerprint differs from the previous report; the server then stores a diff in `hardware_changes`.

The inventory object contains `host`, `os`, `cpu`, `memory`, `gpus`, `motherboard`, `physicalDisks`, `volumes`, `monitors`, `batteries`, `audio`, `usb`, `bluetooth`, `optical`, `printers`, `software`, `updates`, `netAdapters`, `security`, `collectedAt` and a `hash` fingerprint computed by the agent.

### `POST /api/agent/logs`

```json
{"agentId":"…","entries":[{"time":"2026-11-16T10:00:00Z","level":"info","message":"inventory sent"}]}
```

### `GET /api/agent/config`

Returns `{"config":{…},"configVersion":N}` — the effective configuration including any server-side overrides.

### `POST /api/agent/rotate-token`

The agent requests a new token. Response contains the new `agentToken`; the old one stays valid for 10 minutes.

## Authentication endpoints

| Method | Path | Body | Result |
|---|---|---|---|
| `POST` | `/api/auth/login` | `{"username","password"}` | `{"ok":true,"token":"…","expiresAt":"…"}` + cookie |
| `POST` | `/api/auth/logout` | – | Clears the session |
| `GET` | `/api/auth/me` | – | `{"username","adminId","serverName"}` |
| `POST` | `/api/auth/password` | `{"currentPassword","newPassword"}` | Changes the password and invalidates other sessions |

Five failed logins for the same username and IP address block further attempts for 5 minutes.

## Computers

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/agents?status=` | List; `status` = `approved`, `pending`, `rejected`, `disabled`, `revoked` |
| `GET` | `/api/agents/{id}` | Single computer with computed `online` flag |
| `PATCH` | `/api/agents/{id}` | `{"displayName","location","notes","groupId"}` |
| `POST` | `/api/agents/{id}/approve` | Approve a pending agent and issue its token |
| `POST` | `/api/agents/{id}/reject` | Reject a pending agent |
| `POST` | `/api/agents/{id}/disable` / `enable` | Stop/restart reporting without deleting history |
| `POST` | `/api/agents/{id}/revoke` | Invalidate the token immediately |
| `POST` | `/api/agents/{id}/rotate-token` | Queue a token rotation; the agent adopts it on its next heartbeat |
| `POST` | `/api/agents/{id}/refresh-inventory` | Queue a fresh inventory report |
| `DELETE` | `/api/agents/{id}` | Remove the computer and all its history |
| `GET` | `/api/agents/{id}/inventory` | Latest snapshot: `{"inventory":{…},"collectedAt","receivedAt","hash"}` |
| `GET` | `/api/agents/{id}/snapshots?limit=` | Snapshot history (hashes, timestamps) |
| `GET` | `/api/agents/{id}/performance?range=` | Time series; `range` = `1h`, `6h`, `24h`, `7d`, `30d` |
| `GET` | `/api/agents/{id}/logs?limit=` | Agent-reported log lines |
| `GET` | `/api/agents/{id}/events?limit=` | Server-side lifecycle events |

## Enrollments

- `GET /api/enrollments?decision=pending` — enrollment requests awaiting a decision.

## Fleet data

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/dashboard` | Aggregate cards, distributions, high-CPU/high-RAM, recent changes, alerts and events |
| `GET` | `/api/inventory` | One row per computer with flattened hardware summary |
| `GET` | `/api/changes?agentId=&limit=` | Hardware change log |
| `GET` | `/api/search?q=` | Search computers, inventory fields and software |
| `GET` | `/api/groups` · `POST /api/groups` · `DELETE /api/groups/{id}` | Computer groups |

## Alerts

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/alerts?status=&severity=&agentId=&limit=` | `status` = `active`, `acknowledged`, `resolved` |
| `GET` | `/api/alerts/rules` | Rule list with thresholds and cooldowns |
| `PUT` | `/api/alerts/rules/{key}` | Enable/disable a rule, change thresholds |
| `POST` | `/api/alerts/{id}/ack` · `/resolve` | Acknowledge or resolve |
| `POST` | `/api/alerts/test-notification` | Send a test email/Telegram message |

## Reports

`GET /api/reports/{kind}?format=csv|pdf|html|json&agents=id1,id2&range=`

| `kind` | Content |
|---|---|
| `inventory` | Full hardware inventory, one row per computer |
| `network` | IPs, MACs, gateways, DNS per computer |
| `storage` | Physical disks, volumes, free space |
| `distribution` | Counts by OS, CPU, RAM size, disk media, GPU |
| `changes` | Hardware changes over time |
| `performance` | CPU/memory/disk averages per computer for `range` (`6h`, `24h`, `7d`, `30d`) |

`json` returns the report object itself, whose JSON tags are the CSV column headers. Filenames look like `hardware-inventory-20261116-1015.csv`.

## Settings and maintenance

| Method | Path | Notes |
|---|---|---|
| `GET` | `/api/settings` | All server settings, alert rules and default agent configuration |
| `PUT` | `/api/settings` | Partial update of the same |
| `GET` | `/api/audit?limit=` | Audit trail |
| `POST` | `/api/backup` | Write a database backup now |
| `GET` | `/api/discovery/scans` · `POST /api/discovery/scan` | LAN scan history / start a scan |
| `GET` | `/api/discovery/devices` · `POST` · `DELETE /{id}` | Discovered and manually added devices |

## Live updates

`GET /api/stream` — Server-Sent Events. Each event has a `data` payload of `{}` or a small object; refetch the relevant endpoint when it arrives.

| Event | Fires when |
|---|---|
| `agents` | A computer is enrolled, approved, disabled, revoked or deleted |
| `metrics` | A performance sample arrives (`data` includes `{agentId, sample}`) |
| `inventory` | A new inventory snapshot is stored |
| `changes` | Hardware changes are detected |
| `alerts` | An alert opens, is acknowledged or resolved |
| `enrollment` | A new enrollment request arrives |
| `settings` | Server settings changed |

```js
const es = new EventSource("/api/stream");
es.addEventListener("metrics", e => console.log(JSON.parse(e.data)));
```

## Example: a minimal agent report loop

```bash
TOKEN=...; ID=...
curl -s -X POST http://server:8080/api/agent/heartbeat \
  -H "Authorization: Bearer $TOKEN" -H "X-Agent-Id: $ID" \
  -H 'Content-Type: application/json' \
  -d '{"agentId":"'"$ID"'","sentAt":"2026-11-16T10:00:00Z","agentVersion":"1.0.0"}'
```
