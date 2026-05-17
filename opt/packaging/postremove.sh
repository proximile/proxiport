#!/bin/sh
# Post-remove hook for the proxiport / proxiportd deb and rpm packages.
# Reloads systemd so the disappeared units do not show up as masked.
# Leaves /var/lib/proxiport and /etc/proxiport in place — operators
# remove those manually after a deliberate purge.

set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
