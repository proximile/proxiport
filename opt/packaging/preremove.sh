#!/bin/sh
# Pre-remove hook for the proxiport / proxiportd deb and rpm packages.
# Stops and disables the systemd units when the package is being removed.
#
# Runs on upgrades too, where it must do nothing: dpkg calls prerm as
# `upgrade <new-version>` and rpm calls preun with the number of instances
# that will remain (1 or more during an upgrade, 0 on the final erase).
# Disabling unconditionally left the daemon stopped and disabled after every
# package upgrade -- the unit is only re-enabled by hand, so an unattended
# upgrade silently took the server offline.

set -e

case "${1:-}" in
    upgrade|failed-upgrade|deconfigure)
        # dpkg: an upgrade is in progress; postinst restarts the services.
        exit 0
        ;;
    ''|*[!0-9]*)
        # Not an rpm-style count (includes dpkg's `remove`): fall through.
        ;;
    *)
        # rpm: >0 instances remain after this transaction, so it is an upgrade.
        if [ "$1" -gt 0 ]; then
            exit 0
        fi
        ;;
esac

if command -v systemctl >/dev/null 2>&1; then
    for svc in proxiportd proxiport; do
        if systemctl is-enabled "$svc.service" >/dev/null 2>&1; then
            systemctl disable --now "$svc.service" >/dev/null 2>&1 || true
        fi
    done
fi

exit 0
