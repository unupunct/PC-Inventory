#!/usr/bin/env bash
# PC Inventory Manager - Linux installer
#
#   sudo ./install.sh            install server and agent service files
#   sudo ./install.sh --server   server only
#   sudo ./install.sh --agent    agent only
#
# Expects pcim-server and pcim-agent next to this script (or on PATH).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="$HERE"
INSTALL_SERVER=1
INSTALL_AGENT=1

for arg in "$@"; do
  case "$arg" in
    --server) INSTALL_AGENT=0 ;;
    --agent)  INSTALL_SERVER=0 ;;
    -h|--help)
      sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

if [ "$(id -u)" -ne 0 ]; then
  echo "error: run this installer with sudo" >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "error: systemd is required (systemctl not found)" >&2
  exit 1
fi

find_bin() { # name
  if [ -x "$BIN_SRC/$1" ]; then echo "$BIN_SRC/$1"
  elif command -v "$1" >/dev/null 2>&1; then command -v "$1"
  else return 1; fi
}

echo "PC Inventory Manager installer"
echo "------------------------------"

if [ "$INSTALL_SERVER" = "1" ]; then
  SERVER_BIN="$(find_bin pcim-server)" || { echo "error: pcim-server not found" >&2; exit 1; }
  echo "Installing server"
  install -m 0755 "$SERVER_BIN" /usr/local/bin/pcim-server
  if ! id pcim >/dev/null 2>&1; then
    useradd --system --home /var/lib/pcim --shell /usr/sbin/nologin pcim
    echo "  created system user 'pcim'"
  fi
  install -d -o pcim -g pcim /var/lib/pcim
  install -d /etc/pcim
  if [ ! -f /etc/pcim/server.env ]; then
    cat > /etc/pcim/server.env <<'ENV'
# PC Inventory Manager server configuration.
# Uncomment to accept agents from other computers on the network:
#PCIM_LISTEN_HOST=0.0.0.0
#PCIM_PORT=8080
#PCIM_TLS_SELF_SIGNED=false
#PCIM_RETENTION_DAYS=30
ENV
    echo "  wrote /etc/pcim/server.env"
  fi
  install -d /usr/local/share/pcim
  install -m 0644 "$HERE/pcim-server.service" /usr/local/share/pcim/pcim-server.service
  install -m 0644 /usr/local/share/pcim/pcim-server.service /etc/systemd/system/pcim-server.service
  systemctl daemon-reload
  systemctl enable --now pcim-server
  service_ok=0
  for _ in $(seq 1 20); do
    if systemctl is-active --quiet pcim-server; then service_ok=1; break; fi
    sleep 0.5
  done
  if [ "$service_ok" = "1" ]; then
    echo "  server running"
  else
    echo "  warning: pcim-server did not start; see: journalctl -u pcim-server -n 30" >&2
  fi
  if [ -f /var/lib/pcim/initial-admin-password.txt ]; then
    echo
    echo "  Initial administrator credentials:"
    sed 's/^/    /' /var/lib/pcim/initial-admin-password.txt
  elif [ -f /var/lib/pcim/data/initial-admin-password.txt ]; then
    echo
    echo "  Initial administrator credentials:"
    sed 's/^/    /' /var/lib/pcim/initial-admin-password.txt
  fi
fi

if [ "$INSTALL_AGENT" = "1" ]; then
  AGENT_BIN="$(find_bin pcim-agent)" || { echo "error: pcim-agent not found" >&2; exit 1; }
  echo "Installing agent"
  install -m 0755 "$AGENT_BIN" /usr/local/bin/pcim-agent
  install -d /etc/pcim-agent
  install -d /usr/local/share/pcim
  install -m 0644 "$HERE/pcim-agent.service" /usr/local/share/pcim/pcim-agent.service
  install -m 0644 /usr/local/share/pcim/pcim-agent.service /etc/systemd/system/pcim-agent.service
  systemctl daemon-reload
  if [ ! -f /etc/pcim-agent/agent.json ]; then
    echo
    echo "  The agent needs to know where the server is."
    printf "  Server address [http://127.0.0.1:8080]: "
    read -r SERVER_URL || SERVER_URL=""
    SERVER_URL="${SERVER_URL:-http://127.0.0.1:8080}"
    /usr/local/bin/pcim-agent config --config /etc/pcim-agent/agent.json --server "$SERVER_URL" || \
      echo "  warning: could not write /etc/pcim-agent/agent.json"
    echo "  Enrolling (approve this computer in the dashboard, then run:"
    echo "    sudo pcim-agent enroll --server $SERVER_URL"
    echo "  )"
  fi
  systemctl enable --now pcim-agent || true
  echo "  agent service enabled (it keeps retrying until the server approves it)"
fi

echo
echo "Done."
if [ "$INSTALL_SERVER" = "1" ]; then
  echo "  Dashboard: check the server log for the URL, or: systemctl status pcim-server"
  echo "  Accept LAN agents: set PCIM_LISTEN_HOST=0.0.0.0 in /etc/pcim/server.env, then"
  echo "    sudo systemctl restart pcim-server"
  echo "  Firewall: sudo ufw allow 8080/tcp   (only on your private network)"
fi
