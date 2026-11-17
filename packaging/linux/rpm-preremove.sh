#!/bin/sh
set -e
if command -v systemctl >/dev/null 2>&1; then
  systemctl stop pcim-server >/dev/null 2>&1 || true
  systemctl stop pcim-agent >/dev/null 2>&1 || true
  systemctl disable pcim-server >/dev/null 2>&1 || true
  systemctl disable pcim-agent >/dev/null 2>&1 || true
fi
exit 0
