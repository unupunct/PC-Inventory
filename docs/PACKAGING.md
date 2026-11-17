# Packaging and distribution

## Artifacts

`scripts/build.sh` (bash, Linux/macOS/Git-Bash) produces every file below in `dist/`:

| File | Platform | What it is |
|---|---|---|
| `pcim-1.0.0-windows-setup.exe` | Windows 10/11, Server 2016+ (x64) | Inno Setup installer: server and/or agent, services, firewall rule, uninstaller |
| `pcim-1.0.0-windows.zip` | Windows x64 | Portable: `pcim-server.exe`, `pcim-agent.exe`, README and docs |
| `pcim-1.0.0-linux-amd64.tar.gz` | Linux x86-64 | Binaries, docs, `install.sh`, systemd units |
| `pcim-1.0.0-linux-arm64.tar.gz` | Linux ARM64 (Raspberry Pi, ARM servers) | Same as above |
| `pcim-agent-macos-arm64`, `pcim-agent-macos-amd64` | macOS 12+ | Agent binary (Intel and Apple Silicon) |

`scripts/build-windows.ps1` does the same on a Windows machine and then compiles the installer
with Inno Setup. The macOS agent ships as a bare binary; run it from a launchd unit as described
in [MACOS_AGENT](MACOS_AGENT.md).

## Windows installer

Built from `packaging/windows/setup.iss` with Inno Setup 6
(`winget install JRSoftware.InnoSetup`):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build-windows.ps1
# or directly:
& "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe" /DAppVersion=1.0.0 packaging\windows\setup.iss
```

What the installer does:

1. Copies `pcim-server.exe` / `pcim-agent.exe` to `C:\Program Files\PC Inventory Manager`.
2. Asks for the HTTP port (default 8080) and, for the agent, the server address.
3. Writes `C:\ProgramData\PCInventoryManager\Server\server.env` with
   `PCIM_LISTEN_HOST=0.0.0.0` and the chosen port, so agents on the LAN can connect.
4. Registers the **pcim-server** service (`service install`), starts it, and optionally adds an
   inbound firewall rule for the chosen port, **private network profile only**.
5. For the agent: enrolls, then registers the **pcim-agent** service.
6. Adds Start-menu entries and, optionally, a desktop shortcut to the dashboard.

Silent install (e.g. for scripted rollout):

```powershell
pcim-1.0.0-windows-setup.exe /VERYSILENT /SUPPRESSMSGBOXES /NORESTART `
  /COMPONENTS="server,agent" /TASKS="server_service,firewall,enroll"
```

`/LOG=<file>` writes an install log. Uninstalling stops and removes both services, deletes the
firewall rule, and keeps `C:\ProgramData\PCInventoryManager` (database and history) — delete that
folder yourself if you want a clean slate.

### Service commands

```powershell
pcim-server.exe service install|start|stop|restart|status|uninstall
pcim-agent.exe  service install|start|stop|restart|status|uninstall
```

Both services are set to start automatically and restart 10 seconds after a failure. Full log:
`C:\ProgramData\PCInventoryManager\Server\server.log` and the Application event log (source
`pcim-server`).

Running `service install` requires an elevated prompt; the installer already requests it.

## Linux packages

Building `.deb`/`.rpm` needs `nfpm`, which this build environment does not have. The tarballs are
complete and install cleanly instead:

```bash
tar -xzf pcim-1.0.0-linux-amd64.tar.gz
cd pcim-linux-amd64
sudo ./install.sh              # server + agent
sudo ./install.sh --server     # server only
sudo ./install.sh --agent      # agent only
```

`install.sh` is idempotent and does not overwrite an existing `/etc/pcim/server.env` or
`/etc/pcim-agent/agent.json`. Files it writes:

| Path | Purpose |
|---|---|
| `/usr/local/bin/pcim-server`, `/usr/local/bin/pcim-agent` | Binaries |
| `/etc/pcim/server.env` | Server configuration (systemd `EnvironmentFile`) |
| `/etc/pcim-agent/agent.json` | Agent configuration |
| `/var/lib/pcim` | Server database and backups (owner `pcim`) |
| `/etc/systemd/system/pcim-server.service`, `pcim-agent.service` | Units, enabled and started |

The server unit runs as the unprivileged `pcim` user with `ProtectSystem=strict`; the agent runs as
root because reading DMI/SMART data needs it.

To build `.deb`/`.rpm` later:

```bash
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
nfpm package -f packaging/nfpm.yaml
```

## Verification done for 1.0.0

- Both Windows binaries start, serve the dashboard, accept an agent, and export CSV and PDF.
- Silent install into a scratch directory and full uninstall were exercised (files, `[Code]`
  sections and uninstaller), including the port page and `server.env` generation.
- Cross-compilation to `linux/amd64`, `linux/arm64` and `darwin/arm64` succeeds; the Linux and
  macOS binaries have **not** yet been run on real hardware. The service registration
  (`service install`) itself still needs one manual test on a Windows machine with an elevated
  prompt — it was written and reviewed but not executed in this environment.
- `test/sample-agent` is a development helper that sends a clearly labelled synthetic inventory
  (hostname `sample-vm-*`, `collectionErrors` set). It is not part of any release artifact and
  must not be used to populate a production dashboard.

## Versioning

`internal/shared/version/version.go` holds `Version` and `Protocol`. Both are stamped into the
binaries with `-ldflags -X`. Bump `Version` for a release and `Protocol` only when the agent/server
wire format changes incompatibly; the server rejects agents with a different protocol, so upgrading
means upgrading both sides.
