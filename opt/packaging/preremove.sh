#!/bin/sh
# Pre-remove hook for the proxiport / proxiportd deb and rpm packages.
# Stops and disables the systemd units if they exist.

set -e

if command -v systemctl >/dev/null 2>&1; then
    for svc in proxiportd proxiport; do
        if systemctl is-enabled "$svc.service" >/dev/null 2>&1; then
            systemctl disable --now "$svc.service" >/dev/null 2>&1 || true
        fi
    done
fi

exit 0
