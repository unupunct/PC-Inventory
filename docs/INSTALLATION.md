# Installation

Two binaries, no runtime dependencies: `pcim-server` and `pcim-agent`. Both are statically linked Go programs.

## 1. Server

### Windows

```powershell
mkdir C:\PCInventoryManager
copy pcim-server.exe C:\PCInventoryManager\
cd C:\PCInventoryManager
.\pcim-server.exe
```

The server prints its URL and writes the initial admin password to `data\initial-admin-password.txt`. Open `http://127.0.0.1:8080`, sign in as `admin`, and change the password (Settings → Account).

To accept agents from other computers on the LAN, bind to the network:

```powershell
$env:PCIM_LISTEN_HOST = "0.0.0.0"
.\pcim-server.exe
```

Then allow the port through the firewall:

```powershell
New-NetFirewallRule -DisplayName "PC Inventory Manager" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Private
```

### Linux

```bash
sudo useradd --system --home /var/lib/pcim --shell /usr/sbin/nologin pcim
sudo install -d -o pcim -g pcim /var/lib/pcim
sudo install -m 0755 pcim-server /usr/local/bin/pcim-server
sudo tee /etc/systemd/system/pcim-server.service >/dev/null <<'EOF'
[Unit]
Description=PC Inventory Manager Server
After=network-online.target

[Service]
User=pcim
Environment=PCIM_DATA_DIR=/var/lib/pcim
Environment=PCIM_LISTEN_HOST=0.0.0.0
ExecStart=/usr/local/bin/pcim-server
Restart=always

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl enable --now pcim-server
sudo cat /var/lib/pcim/data/initial-admin-password.txt
```

### macOS

```bash
mkdir -p ~/PCInventoryManager && cd ~/PCInventoryManager
./pcim-server
```

For LAN access run with `PCIM_LISTEN_HOST=0.0.0.0 ./pcim-server` and allow the binary through the firewall prompt.

## 2. Agent

Install the agent on every computer you want to inventory, including the server host.

```bash
pcim-agent enroll --server <server-address>
pcim-agent service install
```

`<server-address>` is `host`, `host:port` or a full URL, e.g. `192.168.1.100:8080` or `https://inventory.lan`.

The first `enroll` reports `waiting for administrator approval`. Approve the computer in the dashboard (Enrollments), then run `pcim-agent enroll` again — or simply `pcim-agent run`, which keeps polling and finishes enrollment on its own once approved.

### Windows agent

```powershell
cd C:\ProgramData\PCInventoryManager
.\pcim-agent.exe enroll --server 192.168.1.100:8080
.\pcim-agent.exe service install      # run as Administrator
```

The service starts automatically at boot and restarts after a crash. Configuration lives in `C:\ProgramData\PCInventoryManager\Agent\agent.json`.

### Linux agent

```bash
sudo ./pcim-agent enroll --server 192.168.1.100:8080
sudo ./pcim-agent service install     # systemd unit pcim-agent.service
```

Configuration lives in `/etc/pcim-agent/agent.json`.

### macOS agent

```bash
sudo ./pcim-agent enroll --server 192.168.1.100:8080
sudo ./pcim-agent service install     # launchd daemon com.pcim.agent
```

Configuration lives in `/usr/local/etc/pcim-agent/agent.json`.

## 3. Verify

```bash
pcim-agent status        # enrolled, connected, last error, buffered reports
pcim-agent inventory     # the exact JSON the agent will send
```

Within a minute of approval the computer appears on the dashboard with real hardware data, and performance samples start filling in.

## Upgrading

Stop the service, replace the binary, start it again. The database schema migrates automatically on server start; the agent keeps its configuration and token across upgrades.

## Uninstalling

```bash
pcim-agent service uninstall
```

Then delete the configuration directory and, on the server, the computer record. Server data is entirely contained in the data directory (`pcim.db` plus `backups/`); deleting it removes all history.
