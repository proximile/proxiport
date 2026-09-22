#!/bin/sh
# Pre-install hook for the proxiport / proxiportd deb and rpm packages.
# Creates the unprivileged `proxiport` system user and group used by
# both daemons. POSIX shell — runs under dpkg and rpm scriptlets.

set -e

# ensure_account <name> <home> <comment>
ensure_account() {
    _name="$1"
    _home="$2"
    _comment="$3"

    if ! getent group "$_name" >/dev/null 2>&1; then
        if command -v groupadd >/dev/null 2>&1; then
            groupadd --system "$_name"
        elif command -v addgroup >/dev/null 2>&1; then
            addgroup --system "$_name"
        fi
    fi

    if ! getent passwd "$_name" >/dev/null 2>&1; then
        if command -v useradd >/dev/null 2>&1; then
            useradd --system --gid "$_name" \
                    --home-dir "$_home" \
                    --shell /usr/sbin/nologin \
                    --comment "$_comment" \
                    "$_name"
        elif command -v adduser >/dev/null 2>&1; then
            adduser --system --ingroup "$_name" \
                    --home "$_home" \
                    --shell /usr/sbin/nologin \
                    --gecos "$_comment" \
                    "$_name"
        fi
    fi
}

# The server's account. It owns the config with key_seed and jwt_secret in it,
# the databases, the vault and the ACME key cache.
ensure_account proxiport /var/lib/proxiport "ProxiPort server user"

# The agent's account, which is deliberately NOT the server's.
#
# The agent executes operator-supplied commands as its own uid. While both ran
# as `proxiport`, an operator holding only the `commands` permission for one
# managed host -- or anyone who compromised the agent process -- could read
# /etc/proxiport/proxiportd.conf and recover jwt_secret (forge an admin session
# for the whole fleet) and key_seed (impersonate the server to every agent),
# and had read/write on every database under /var/lib/proxiport.
ensure_account proxiport-agent /var/lib/proxiport-agent "ProxiPort agent user"

exit 0
