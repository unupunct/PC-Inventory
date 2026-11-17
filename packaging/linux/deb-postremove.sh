#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi
if [ "$1" = "purge" ]; then
  echo "Purge: /var/lib/pcim (database and history) is kept; remove it manually if you want a clean slate."
fi
exit 0
