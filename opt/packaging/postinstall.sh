#!/bin/sh
# Post-install hook for the proxiport / proxiportd deb and rpm packages.
#
# - Creates state directories with the right ownership.
# - Seeds /etc/proxiport/<svc>.conf from <svc>.example.conf if not present.
# - On first install of proxiportd, replaces placeholder secrets in the
#   seeded config with random values and writes the initial admin +
#   client-auth credentials to /var/lib/proxiport/initial-*-password.
# - Grants CAP_NET_BIND_SERVICE on /usr/bin/proxiportd so the proxiport
#   user can bind ports below 1024 without root.
# - Prints next-steps for whichever package was just installed.
#
# POSIX shell; safe to source under dash or bash.

set -e

install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
install -d -o proxiport -g proxiport -m 0750 /var/log/proxiport
install -d -o root -g root -m 0755 /etc/proxiport

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

# ----------------------------------------------------------------------
# helpers
# ----------------------------------------------------------------------

rand_hex() {
    # $1 = bytes of randomness; output hex on one line, no whitespace.
    head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
}

rand_b64() {
    # $1 = bytes of randomness; output base64 on one line, padding stripped.
    head -c "$1" /dev/urandom | base64 | tr -d '\n='
}

write_secret_file() {
    # write_secret_file <path> <content>
    # Mode 0640 root:proxiport so an admin can `sudo cat` it and the
    # proxiport daemon's group can read it if ever wired through.
    _path="$1"
    _content="$2"
    if [ ! -e "$_path" ]; then
        umask 027
        printf '%s\n' "$_content" > "$_path"
        chown root:proxiport "$_path"
        chmod 0640 "$_path"
    fi
}

seed_server_secrets() {
    # Replace placeholders in a freshly-copied proxiportd.conf with
    # random values; persist the admin and client-auth credentials.
    _conf="$1"

    _seed=$(rand_hex 32)
    _jwt=$(rand_b64 24)
    _admin_pw=$(rand_b64 16)
    _client_pw=$(rand_b64 18)

    sed -i \
        -e "s|key_seed = \"<YOUR_SEED>\"|key_seed = \"${_seed}\"|" \
        -e "s|jwt_secret = \"<YOUR_SECRET>\"|jwt_secret = \"${_jwt}\"|" \
        -e "s|auth = \"clientAuth1:1234\"|auth = \"client1:${_client_pw}\"|" \
        -e "s|auth = \"admin:foobaz\"|auth = \"admin:${_admin_pw}\"|" \
        "$_conf"

    write_secret_file /var/lib/proxiport/initial-admin-password "admin:${_admin_pw}"
    write_secret_file /var/lib/proxiport/initial-client-auth   "client1:${_client_pw}"
}

# ----------------------------------------------------------------------
# proxiportd (server)
# ----------------------------------------------------------------------

if [ -f /etc/proxiport/proxiportd.example.conf ] \
   && [ ! -f /etc/proxiport/proxiportd.conf ]; then
    cp /etc/proxiport/proxiportd.example.conf /etc/proxiport/proxiportd.conf
    chmod 0640 /etc/proxiport/proxiportd.conf
    chown root:proxiport /etc/proxiport/proxiportd.conf
    seed_server_secrets /etc/proxiport/proxiportd.conf
    # The package ships the SPA at /var/lib/proxiport/docroot, so enable
    # doc_root in the seeded config: any install that starts proxiportd
    # then serves the web UI, whether or not proxiport-setup is run.
    sed -i -E '/^\[api\]/,/^\[/ s|^[[:space:]]*#?[[:space:]]*doc_root[[:space:]]*=.*|  doc_root = "/var/lib/proxiport/docroot"|' \
        /etc/proxiport/proxiportd.conf
fi

# Own the shipped SPA tree so the proxiport daemon can read it.
if [ -d /var/lib/proxiport/docroot ]; then
    chown -R proxiport:proxiport /var/lib/proxiport/docroot
fi

if [ -x /usr/bin/proxiportd ] && command -v setcap >/dev/null 2>&1; then
    setcap CAP_NET_BIND_SERVICE=+eip /usr/bin/proxiportd 2>/dev/null || true
fi

# The server unit lists ssl-cert in SupplementaryGroups so the daemon can
# read certbot/manual TLS keys. systemd refuses to start a unit whose
# supplementary group does not resolve, and the group only exists where
# Debian's ssl-cert package created it — never on RHEL-family systems.
if [ -x /usr/bin/proxiportd ] && ! getent group ssl-cert >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system ssl-cert
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup --system ssl-cert
    fi
fi

# ----------------------------------------------------------------------
# proxiport (agent)
# ----------------------------------------------------------------------
# The agent's auth + fingerprint must come from the server side, so the
# seeded config is left with placeholder values; the operator fills it in.

if [ -f /etc/proxiport/proxiport.example.conf ] \
   && [ ! -f /etc/proxiport/proxiport.conf ]; then
    cp /etc/proxiport/proxiport.example.conf /etc/proxiport/proxiport.conf
    chmod 0640 /etc/proxiport/proxiport.conf
    chown root:proxiport /etc/proxiport/proxiport.conf
fi

# ----------------------------------------------------------------------
# next-steps message
# ----------------------------------------------------------------------

if [ -x /usr/bin/proxiportd ]; then
    cat <<'EOF'
ProxiPort server installed.

Initial credentials (read with `sudo cat`):
  /var/lib/proxiport/initial-admin-password   - SPA login (user:pass)
  /var/lib/proxiport/initial-client-auth      - first agent's credential
Both files are shredded automatically after your first admin login, so
read them and store them in a password manager now.

The server is NOT enabled yet. Before starting it:

  1. Edit /etc/proxiport/proxiportd.conf [api] to pick a public-listener
     profile (built-in ACME, manual cert, or reverse proxy). Out of the
     box the API only listens on 127.0.0.1.

  2. Open the chosen ports in any host / cloud firewall.
       Typical: 80 (chisel + ACME HTTP-01) + 443 (TLS API)

  3. systemctl enable --now proxiportd

See https://docs.proxiport.net/install/ for the full guide.
EOF
fi

if [ -x /usr/bin/proxiport ]; then
    cat <<'EOF'
ProxiPort agent installed.

Edit /etc/proxiport/proxiport.conf and set:
  - server      - the proxiportd address (e.g. proxiport.example.com:80)
  - auth        - the credential from the server's initial-client-auth file
  - fingerprint - the proxiportd host-key fingerprint (server SPA / log)

Then:  systemctl enable --now proxiport
EOF
fi

exit 0
