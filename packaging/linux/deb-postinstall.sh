#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
  systemctl enable pcim-server >/dev/null 2>&1 || true
  # On upgrade: restart so configuration changes take effect.
  systemctl restart pcim-server >/dev/null 2>&1 || systemctl start pcim-server >/dev/null 2>&1 || true
fi
echo "PC Inventory Manager installed."
echo "  Server:  systemctl status pcim-server   (config: /etc/pcim/server.env)"
echo "  Agent:   pcim-agent enroll --server http://<server>:8080, then"
echo "           systemctl enable --now pcim-agent"
if [ -f /var/lib/pcim/initial-admin-password.txt ]; then
  echo "  Initial administrator credentials:"
  sed 's/^/    /' /var/lib/pcim/initial-admin-password.txt
fi
exit 0
