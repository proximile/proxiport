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

# Everything under /var/lib/proxiport and /var/log/proxiport is writable by the
# daemon that owns it, and this hook runs as root on every upgrade. chown and
# chmod follow symlinks, `-f` and `-e` follow symlinks, and a redirect creates
# whatever a dangling link points at. So a daemon that has been compromised
# could turn any of those into "hand /etc/shadow to my uid" by planting a link
# and waiting for the next upgrade. Nothing below operates twice on a path a
# daemon could have replaced: a file is either opened once and then handled
# through the open descriptor, or first moved (rename never follows links)
# into a root-only staging directory on the same filesystem, inspected there,
# fixed there, and moved to its destination.

warn() {
    echo "proxiport: WARNING - $*" >&2
}

root_only_dir() {
    # root_only_dir <dir>
    #
    # True when <dir> is a directory that root owns and nobody else can
    # rename entries in: no group or other write bit, unless the sticky bit
    # makes a writable directory safe, as it does /tmp. (Ubuntu's rsyslog
    # package makes /var/log group-writable for `syslog`, which is why this
    # is checked rather than assumed.)
    if [ ! -d "$1" ] || [ -L "$1" ]; then
        return 1
    fi
    if [ "$(stat -c %u "$1")" != 0 ]; then
        return 1
    fi
    case "$(stat -c %A "$1")" in
        *t|*T) return 0 ;;
        d????w????|d???????w?) return 1 ;;
    esac
    return 0
}

stage_dir() {
    # stage_dir <daemon-dir>
    #
    # Prints a fresh root-only directory on the same filesystem as
    # <daemon-dir>, so that mv is a rename rather than a copy, in a parent
    # where nothing but root can rename it away between its creation and its
    # use. Tries the daemon directory's own parent, then /var/lib. Prints
    # nothing when there is no such place (<daemon-dir> is its own mount).
    for _parent in "$(dirname "$1")" /var/lib; do
        root_only_dir "$_parent" || continue
        if [ "$(stat -c %d "$_parent")" != "$(stat -c %d "$1")" ]; then
            continue
        fi
        _dir=$(mktemp -d "$_parent/.proxiport-postinst.XXXXXX") || continue
        if [ -L "$_dir" ] || [ "$(stat -c '%u %a' "$_dir")" != "0 700" ]; then
            rm -rf "$_dir"
            continue
        fi
        printf '%s\n' "$_dir"
        return 0
    done
    return 1
}

sweep_stale_stage_dirs() {
    # A hook killed between creating its staging directory and its EXIT trap
    # leaves a root-only directory behind. An empty one is removed; one that
    # still holds a file is reported, never emptied, since that file is a
    # daemon's that was mid-move. Only parents root_only_dir accepts are
    # looked at, so nothing anyone else planted is ever touched.
    for _parent in /var/lib /var/log; do
        root_only_dir "$_parent" || continue
        for _d in "$_parent"/.proxiport-postinst.*; do
            if [ -d "$_d" ] && [ ! -L "$_d" ]; then
                rmdir "$_d" 2>/dev/null \
                    || warn "$_d is left over from an interrupted run and is not empty; look inside and remove it by hand"
            fi
        done
    done
}

_keep_stage=0

put_back() {
    # put_back <staged> <original-path> <why>
    #
    # Returns a file from staging to where it came from and reports why. If
    # even that fails the file stays in staging, which the EXIT trap then
    # leaves in place rather than deleting.
    if mv -T "$1" "$2"; then
        warn "$3"
    else
        _keep_stage=1
        warn "$3; it could not be put back either and is at $1"
    fi
}

claim_file() {
    # claim_file <src> <stage> <owner:group> <mode> <dst>
    #
    # Hands the plain file at <src> to <owner:group>, with <mode> unless it is
    # empty, and leaves it at <dst>. Anything that is not a plain file with a
    # single name -- a symlink, a directory, a hard link to a file elsewhere --
    # is put back untouched and reported, since the chown would land on
    # whatever it points at.
    _src="$1"; _stage="$2"; _own="$3"; _mode="$4"; _dst="$5"
    _tmp="$_stage/$(basename "$_src")"
    _ino=$(stat -c %i "$_src") || return 1
    mv -T "$_src" "$_tmp" || return 1
    if [ "$(stat -c %i "$_tmp")" != "$_ino" ]; then
        # mv copied instead of renaming: a bind mount boundary shares the
        # device number but not the namespace. Nothing checked below would
        # be about the original, and a hard link would have been flattened.
        put_back "$_tmp" "$_src" "$_src sits across a mount boundary; left as it is"
        return 1
    fi
    if [ -L "$_tmp" ] || [ ! -f "$_tmp" ] || [ "$(stat -c %h "$_tmp")" != 1 ]; then
        put_back "$_tmp" "$_src" "$_src is not a plain file; left as it is"
        return 1
    fi
    chown "$_own" "$_tmp"
    if [ -n "$_mode" ]; then
        chmod "$_mode" "$_tmp"
    fi
    if ! mv -T "$_tmp" "$_dst"; then
        put_back "$_tmp" "$_src" "could not move $_src to $_dst; left as it is"
        return 1
    fi
}

hand_over_file() {
    # hand_over_file <path> <owner:group> <mode>
    #
    # chown + chmod for a plain file in a directory the daemon can write,
    # without trusting the path twice: the file is opened once and both
    # changes go through the open descriptor, so a link or a rename that
    # appears after the open cannot redirect them. The open inode must be the
    # one the directory entry names (so the entry was not a symlink), a
    # regular file, with a single name (so not a hard link to something
    # else). Needs no staging directory, so it works on any mount.
    (
        exec 3<> "$1" 2>/dev/null || exit 1
        _entry=$(stat -c '%d:%i' "$1") || exit 1
        case "$(stat -L -c '%d:%i %h %F' /dev/fd/3)" in
            "$_entry 1 regular file"|"$_entry 1 regular empty file") ;;
            *) exit 1 ;;
        esac
        chown "$2" /dev/fd/3 && chmod "$3" /dev/fd/3
    )
}

sweep_stale_stage_dirs
_stage_lib=$(stage_dir /var/lib/proxiport) || _stage_lib=""
_stage_log=$(stage_dir /var/log/proxiport) || _stage_log=""
# shellcheck disable=SC2329  # invoked by the EXIT trap below
remove_stage_dirs() {
    if [ "$_keep_stage" = 1 ]; then
        return 0
    fi
    for _d in "$_stage_lib" "$_stage_log"; do
        if [ -n "$_d" ]; then
            rm -rf "$_d" || true
        fi
    done
}
trap remove_stage_dirs EXIT

rand_hex() {
    # $1 = bytes of randomness; output hex on one line, no whitespace.
    head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
}

rand_b64() {
    # $1 = bytes of randomness; output base64 on one line, padding stripped.
    head -c "$1" /dev/urandom | base64 | tr -d '\n='
}

write_secret_file() {
    # write_secret_file <path> <content> <stage>
    #
    # Mode 0600 owned by the SERVER's account. root can still `sudo cat` it,
    # and the server can now shred it: the documented guarantee is that these
    # files disappear at first admin login, and shredFile opens the file
    # O_WRONLY as its first step. At 0640 root:proxiport the daemon could not
    # open it for writing, so the shred returned EACCES into a Debug log line
    # and the cleartext admin password stayed on disk indefinitely -- readable
    # by exactly the uid the daemon runs as, which is what made a data_dir
    # backup or a file-push job enough to recover it.
    #
    # Never written through the destination path: a dangling link there would
    # make root create the link's target. The file is built in the staging
    # directory and renamed into place; a link that appears at the path in
    # the meantime is replaced by the rename, not followed. Whatever goes
    # wrong, the same values are in the seeded config, so this never fails
    # the install.
    _path="$1"
    _content="$2"
    _stage="$3"
    if [ -L "$_path" ] || [ -e "$_path" ]; then
        if [ -L "$_path" ] || [ ! -f "$_path" ]; then
            warn "$_path exists and is not a plain file; the credential was not written there (it is in /etc/proxiport/proxiportd.conf)"
        else
            warn "$_path already exists and was left alone; the credential for this install is in /etc/proxiport/proxiportd.conf"
        fi
        return 0
    fi
    if [ -n "$_stage" ]; then
        _tmp="$_stage/$(basename "$_path")"
        (umask 077; printf '%s\n' "$_content" > "$_tmp")
        chown proxiport:proxiport "$_tmp"
        chmod 0600 "$_tmp"
        if ! mv -T "$_tmp" "$_path"; then
            rm -f "$_tmp"
            warn "could not write $_path; the credential is in /etc/proxiport/proxiportd.conf"
        fi
    else
        # The data directory is its own mount, so there is no staging
        # directory a rename can reach. O_EXCL (noclobber) fails on a link
        # instead of following it, and the umask already gives 0600.
        if (umask 077; set -C; printf '%s\n' "$_content" > "$_path") 2>/dev/null; then
            chown -h proxiport:proxiport "$_path"
        else
            warn "could not write $_path; the credential is in /etc/proxiport/proxiportd.conf"
        fi
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

    write_secret_file /var/lib/proxiport/initial-admin-password "admin:${_admin_pw}"   "$_stage_lib"
    write_secret_file /var/lib/proxiport/initial-client-auth   "client1:${_client_pw}" "$_stage_lib"
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
    # The package ships the SPA at /usr/share/proxiport/docroot, so enable
    # doc_root in the seeded config: any install that starts proxiportd
    # then serves the web UI, whether or not proxiport-setup is run.
    sed -i -E '/^\[api\]/,/^\[/ s|^[[:space:]]*#?[[:space:]]*doc_root[[:space:]]*=.*|  doc_root = "/usr/share/proxiport/docroot"|' \
        /etc/proxiport/proxiportd.conf
fi

# The shipped SPA tree is root's, under /usr/share where only root can write,
# and the daemon only reads it. Up to 0.11.x it was unpacked into
# /var/lib/proxiport, a tree the daemon owns and could therefore rearrange
# before the package manager, as root, unpacked the next release through it.
# Repoint ONLY the shipped default; a doc_root the operator chose is theirs.
if [ -x /usr/bin/proxiportd ] && [ -f /etc/proxiport/proxiportd.conf ]; then
    sed -i -E '/^\[api\]/,/^\[/ s|^([[:space:]]*)doc_root[[:space:]]*=[[:space:]]*"/var/lib/proxiport/docroot"[[:space:]]*$|\1doc_root = "/usr/share/proxiport/docroot"|' \
        /etc/proxiport/proxiportd.conf
    if [ -d /var/lib/proxiport/docroot ] && [ ! -L /var/lib/proxiport/docroot ]; then
        echo "proxiport: the web UI is now served from /usr/share/proxiport/docroot; whatever is left under /var/lib/proxiport/docroot is no longer used and can be removed" >&2
    fi
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
    # and is deliberately left behind. Both ends of the move are directories a
    # daemon can write, so the file goes through claim_file: a link at either
    # end is reported, never followed, and the mode is kept as it was.
    _src=/var/lib/proxiport/state.json
    _dst=/var/lib/proxiport-agent/state.json
    if [ -L "$_src" ] || { [ -e "$_src" ] && [ -L "$_dst" ]; }; then
        warn "$_src or $_dst is a link; the agent's state was not moved to $_dst"
    elif [ -f "$_src" ] && [ ! -e "$_dst" ]; then
        if [ -n "$_stage_lib" ]; then
            claim_file "$_src" "$_stage_lib" proxiport-agent:proxiport-agent "" "$_dst" || true
        else
            warn "$_src not moved to $_dst (separate filesystem); move it by hand"
        fi
    fi

    # And its log, so the history is not orphaned under the server's account.
    _src=/var/log/proxiport/proxiport.log
    _dst=/var/log/proxiport-agent/proxiport.log
    if [ -L "$_src" ] || { [ -e "$_src" ] && [ -L "$_dst" ]; }; then
        warn "$_src or $_dst is a link; the agent's log was not moved to $_dst"
    elif [ -f "$_src" ] && [ ! -e "$_dst" ]; then
        if [ -n "$_stage_log" ]; then
            claim_file "$_src" "$_stage_log" proxiport-agent:proxiport-agent "" "$_dst" || true
        else
            warn "$_src not moved to $_dst (separate filesystem); move it by hand"
        fi
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
#
# Only a plain file that is not already the daemon's is touched, and only
# through hand_over_file, so a link planted at either name is left where it is
# and reported.
if [ -x /usr/bin/proxiportd ]; then
    for _cred in /var/lib/proxiport/initial-admin-password \
                 /var/lib/proxiport/initial-client-auth; do
        if [ -L "$_cred" ]; then
            warn "$_cred is a link; left as it is"
            continue
        fi
        if [ ! -e "$_cred" ]; then
            continue
        fi
        if [ ! -f "$_cred" ]; then
            warn "$_cred is not a plain file; left as it is"
            continue
        fi
        if [ "$(stat -c '%U:%G %a' "$_cred")" = "proxiport:proxiport 600" ]; then
            continue
        fi
        if ! hand_over_file "$_cred" proxiport:proxiport 0600; then
            warn "$_cred could not be handed to the proxiport account; left as it is -- run: chown proxiport:proxiport $_cred && chmod 0600 $_cred"
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
