# Development

## Layout

```
cmd/
  server/          server main, flags, bootstrap, background jobs
  agent/           agent CLI (run, enroll, status, inventory, config, service)
internal/
  server/
    api/           HTTP handlers, SSE hub, PDF writer, reports, tests
    auth/          argon2id hashing, token generation
    config/        environment/config-file loading
    discovery/     ARP + ICMP + TCP LAN scanning
    inventory/     fingerprinting and hardware diffing
    store/         SQLite schema, migrations and queries
    webui/         embedded dashboard (dist/)
    alerts/        rule engine and notifiers
  agent/
    agent.go       the runtime loop (enroll, heartbeat, metrics, inventory, flush)
    buffer/        on-disk queue for undelivered reports
    client/        HTTP client (gzip JSON)
    collect/       collectors: collect.go + per-OS implementations
    config/        agent.json load/save, machine identity
    service/       Windows SCM, systemd, launchd
  shared/
    models/        wire types shared by server and agent
    version/       version and protocol constants
docs/              this documentation
test/              integration helpers
```

## Build

```bash
go build -o bin/pcim-server ./cmd/server
go build -o bin/pcim-agent  ./cmd/agent
```

Cross-compilation works with no cgo (SQLite is `modernc.org/sqlite`, pure Go):

```bash
GOOS=windows GOARCH=amd64 go build -o bin/pcim-agent.exe      ./cmd/agent
GOOS=darwin  GOARCH=arm64 go build -o bin/pcim-agent-mac-arm  ./cmd/agent
GOOS=linux   GOARCH=amd64 go build -o bin/pcim-agent-linux    ./cmd/agent
```

## Test

```bash
go test ./...            # full suite
go test ./... -short     # skips tests that read real hardware
go vet ./...
```

What the tests cover:

- `internal/server/api` — login and throttling, session enforcement, the full enroll → approve → token flow, gzip request bodies, and a metrics test that asserts the stored sample equals what the agent sent.
- `internal/agent/buffer` — ordering, the retention cap dropping metrics before inventory, clear.
- `internal/agent/collect` — real hardware assertions (hostname, cores, memory, volumes, hash) and metric bounds. These read the machine they run on, which is why they are skipped by `-short`.

## Adding a collector

Collectors use a platform-dispatch pattern:

1. `collect.go` calls a platform-neutral function such as `collectGPUDetail(ctx, &inv, addErr)`.
2. Each platform file (`collect_windows.go`, `collect_linux.go`, `collect_darwin.go`, `collect_other.go`) defines that function, tagged with `//go:build`.
3. `collect_other.go` provides no-op stubs so every target still builds.

Rules for new fields:

- Never invent a value. If the platform cannot provide it, leave the field empty and append the name to `Metrics.Unsupported` / set a `*Supported` flag.
- Distinguish *unavailable* (nil / omitted) from *zero* (a real measured zero).
- Add the field to `models.Inventory` with a JSON tag, then to the fingerprint in `internal/server/inventory/diff.go` if it should count as a hardware change, and to the CSV columns of the matching report.

Run `pcim-agent inventory` on the target platform to see the real output before touching the server side.

## Schema changes

Migrations live in `internal/server/store/db.go` as numbered steps appended to the migration list. Add a new step; never edit an existing one, and never assume the database is empty — `ALTER TABLE ... ADD COLUMN` is applied to installed databases on next start. SQLite has no `DROP COLUMN` on all supported versions, so add columns rather than renaming them.

## Dashboard

The current dashboard is a single self-contained HTML file in `internal/server/webui/dist/index.html`, embedded with `go:embed` and served with SPA fallback. It talks to the same REST and SSE API any other client would.

A React/TypeScript front end is the intended next step; when it lands, build it into `internal/server/webui/dist` and the server picks it up with no code change. Keep the served app API-compatible with the endpoints in [API_DOCUMENTATION](API_DOCUMENTATION.md).

## Conventions

- `writeJSON` / `writeErr` for all responses; no ad-hoc `fmt.Fprintf` to the response writer.
- Every remote action goes through the restricted `AgentCommand` set — no command execution paths.
- Timestamps are stored as UTC strings (`RFC3339` in JSON, `timeLayout` in SQLite) and converted to local time only in the browser.
- The agent never blocks its heartbeat on collection: inventory runs on its own goroutine so a slow `system_profiler` cannot make the computer look offline.

## Manual end-to-end check

```bash
./bin/pcim-server &                                  # writes data/initial-admin-password.txt
./bin/pcim-agent enroll --server 127.0.0.1:8080      # -> waiting for approval
# approve in the dashboard, then:
./bin/pcim-agent run
./bin/pcim-agent status
curl -s localhost:8080/api/system
```

To simulate an outage, stop the server, watch `pendingBuffered` grow in `pcim-agent status`, then start the server again and confirm the log line `flushed N buffered report(s)` and that history has no gaps beyond the outage itself.
