#!/bin/sh
set -e
getent group pcim >/dev/null 2>&1 || groupadd -r pcim
getent passwd pcim >/dev/null 2>&1 || useradd -r -g pcim -d /var/lib/pcim -s /sbin/nologin -M pcim
exit 0
