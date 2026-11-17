#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl enable pcim-server >/dev/null 2>&1 || true
  systemctl restart pcim-server >/dev/null 2>&1 || systemctl start pcim-server >/dev/null 2>&1 || true
fi
echo "PC Inventory Manager installed. Server: systemctl status pcim-server"
exit 0
