#!/bin/sh
set -e
if ! getent group pcim >/dev/null 2>&1; then
  addgroup --system pcim >/dev/null 2>&1 || true
fi
if ! getent passwd pcim >/dev/null 2>&1; then
  adduser --system --group --home /var/lib/pcim --no-create-home \
    --shell /usr/sbin/nologin pcim >/dev/null 2>&1 || true
fi
exit 0
