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

# Distinguish a first install from an upgrade. dpkg calls this hook as
# `configure <old-version>`, so $2 is empty only on a first install; rpm calls
# it with $1 = 1 on an install and 2 or more on an upgrade. An upgrade must
# restart whatever was already running and skip the first-install banner.
is_upgrade=0
case "${1:-}" in
    configure)
        if [ -n "${2:-}" ]; then
            is_upgrade=1
        fi
        ;;
    ''|*[!0-9]*)
        # Not an rpm-style instance count; treat as a first install.
        ;;
    *)
        if [ "$1" -ge 2 ]; then
            is_upgrade=1
        fi
        ;;
esac

# The server's state and logs. Owned by `proxiport`, which is the server's
# account and nothing else's -- see preinstall.sh for why.
install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
install -d -o proxiport -g proxiport -m 0750 /var/log/proxiport
install -d -o root -g root -m 0755 /etc/proxiport

# The agent's own state and logs. Separate directories, because separate
# accounts with a shared directory is not a separation.
if [ -x /usr/bin/proxiport ]; then
    install -d -o proxiport-agent -g proxiport-agent -m 0750 /var/lib/proxiport-agent
    install -d -o proxiport-agent -g proxiport-agent -m 0750 /var/log/proxiport-agent
fi

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
    #
    # Mode 0600 owned by the SERVER's account. root can still `sudo cat` it,
    # and the server can now shred it: the documented guarantee is that these
    # files disappear at first admin login, and shredFile opens the file
    # O_WRONLY as its first step. At 0640 root:proxiport the daemon could not
    # open it for writing, so the shred returned EACCES into a Debug log line
    # and the cleartext admin password stayed on disk indefinitely -- readable
    # by exactly the uid the daemon runs as, which is what made a data_dir
    # backup or a file-push job enough to recover it.
    _path="$1"
    _content="$2"
    if [ ! -e "$_path" ]; then
        umask 077
        printf '%s\n' "$_content" > "$_path"
        chown proxiport:proxiport "$_path"
        chmod 0600 "$_path"
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
    chown root:proxiport-agent /etc/proxiport/proxiport.conf
fi

# ----------------------------------------------------------------------
# upgrade: restart what was running, and skip the first-install banner
# ----------------------------------------------------------------------

# ----------------------------------------------------------------------
# migration: the agent and the server no longer share an account
# ----------------------------------------------------------------------
# Up to 0.9.x both daemons ran as `proxiport` and wrote to the same
# directories. `proxiport` is now the server's account alone and the agent runs
# as `proxiport-agent`, so anything the agent needs has to follow it. Every
# host that has ever installed this package needs this, which is why it runs
# unconditionally rather than only on upgrade -- it is a no-op on a host that
# is already right.

if [ -x /usr/bin/proxiport ]; then
    # The agent must be able to read its own config.
    if [ -f /etc/proxiport/proxiport.conf ]; then
        chown root:proxiport-agent /etc/proxiport/proxiport.conf
        chmod 0640 /etc/proxiport/proxiport.conf
    fi

    # An existing config still names the old shared paths, and the agent can no
    # longer write them. Repoint ONLY the shipped defaults; a path the operator
    # chose is theirs and is reported instead of rewritten.
    if [ -f /etc/proxiport/proxiport.conf ]; then
        sed -i \
            -e 's|^\([[:space:]]*\)log_file[[:space:]]*=[[:space:]]*"/var/log/proxiport/proxiport.log"|\1log_file = "/var/log/proxiport-agent/proxiport.log"|' \
            -e 's|^\([[:space:]]*\)data_dir[[:space:]]*=[[:space:]]*"/var/lib/proxiport"|\1data_dir = "/var/lib/proxiport-agent"|' \
            /etc/proxiport/proxiport.conf

        if grep -qE '^[[:space:]]*(log_file|data_dir)[[:space:]]*=[[:space:]]*"/var/(log|lib)/proxiport[/"]' \
                /etc/proxiport/proxiport.conf; then
            cat >&2 <<'WARN'
proxiport: WARNING - /etc/proxiport/proxiport.conf still points log_file or
  data_dir at /var/log/proxiport or /var/lib/proxiport. Those now belong to the
  ProxiPort SERVER's account and the agent cannot write them. Move them under
  /var/log/proxiport-agent and /var/lib/proxiport-agent, or chown your chosen
  paths to proxiport-agent, before restarting the agent.
WARN
        fi
    fi

    # Carry the agent's own state across. Its scripts directory is transient
    # and is deliberately left behind.
    if [ -f /var/lib/proxiport/state.json ] && [ ! -e /var/lib/proxiport-agent/state.json ]; then
        mv /var/lib/proxiport/state.json /var/lib/proxiport-agent/state.json
        chown proxiport-agent:proxiport-agent /var/lib/proxiport-agent/state.json
    fi

    # And its log, so the history is not orphaned under the server's account.
    if [ -f /var/log/proxiport/proxiport.log ] && [ ! -e /var/log/proxiport-agent/proxiport.log ]; then
        mv /var/log/proxiport/proxiport.log /var/log/proxiport-agent/proxiport.log
        chown proxiport-agent:proxiport-agent /var/log/proxiport-agent/proxiport.log
    fi

    # Sudoers rules follow the agent's uid, and the agent's uid has changed.
    if [ -d /etc/sudoers.d ] \
       && grep -rlE '^[[:space:]]*proxiport[[:space:]]' /etc/sudoers.d/ >/dev/null 2>&1; then
        cat >&2 <<'WARN'
proxiport: WARNING - a sudoers rule under /etc/sudoers.d/ grants privileges to
  the user `proxiport`. The agent now runs as `proxiport-agent`, so that rule
  no longer applies to it and privileged commands will fail. Update the rule to
  name proxiport-agent. (Leaving it as it is grants the rule to the ProxiPort
  SERVER instead, which is not what it was written for -- remove it if you do
  not want that.)
WARN
    fi
fi

# The server's own artifacts stay exactly where they are, under exactly the
# account they were already under. Nothing about /var/lib/proxiport moves,
# which is the point: a partially-failed chown -R over the databases, the vault
# and the ACME key cache is a far worse failure than any of the above.
#
# The one exception is the pair of installer credential files. They were
# written 0640 root:proxiport, which the daemon cannot open for writing, so
# the shred it performs at first admin login returned EACCES and the cleartext
# admin password stayed on disk -- on every install that has ever run. Hand
# them to the account that is supposed to destroy them.
if [ -x /usr/bin/proxiportd ]; then
    for _cred in /var/lib/proxiport/initial-admin-password \
                 /var/lib/proxiport/initial-client-auth; do
        if [ -f "$_cred" ]; then
            chown proxiport:proxiport "$_cred"
            chmod 0600 "$_cred"
        fi
    done
fi

if [ "$is_upgrade" = 1 ]; then
    if command -v systemctl >/dev/null 2>&1; then
        for svc in proxiportd proxiport; do
            if [ -x "/usr/bin/$svc" ]; then
                # try-restart is a no-op for a unit that is not running, so an
                # intentionally stopped service stays stopped across upgrades.
                systemctl try-restart "$svc.service" >/dev/null 2>&1 || true
            fi
        done
    fi
    exit 0
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

The agent runs as `proxiport-agent`, which is NOT the ProxiPort server's
account. If you grant the agent sudo rights, name proxiport-agent in
/etc/sudoers.d/ -- a rule naming `proxiport` now applies to the server.
EOF
fi

exit 0
