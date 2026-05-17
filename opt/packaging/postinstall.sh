#!/bin/sh
# Post-install hook for the proxiport / proxiportd deb and rpm packages.
# Sets up state directories, reloads systemd, and prints a hint for
# enabling the service. POSIX shell.

set -e

install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
install -d -o proxiport -g proxiport -m 0750 /var/log/proxiport
install -d -o root -g root -m 0755 /etc/proxiport

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

# Best-effort: if the example config landed but the operator-edited
# real config doesn't exist yet, copy the example so the service can
# at least start in dry-run mode after the operator edits it.
for svc in proxiport proxiportd; do
    example="/etc/proxiport/${svc}.example.conf"
    real="/etc/proxiport/${svc}.conf"
    if [ -f "$example" ] && [ ! -f "$real" ]; then
        cp "$example" "$real"
        chmod 0640 "$real"
        chown root:proxiport "$real"
    fi
done

cat <<'EOF'
ProxiPort installed.

Next steps:
  1. Edit /etc/proxiport/proxiportd.conf (server) or
     /etc/proxiport/proxiport.conf (agent).
  2. systemctl enable --now proxiportd     # for the server
     systemctl enable --now proxiport      # for the agent

See https://docs.proxiport.net/install/ for the full guide.
EOF

exit 0
