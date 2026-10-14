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

# Up to 0.11.0 the server package unpacked the web UI into
# /var/lib/proxiport/docroot, a tree the proxiport account owns. The package
# manager erases those obsolete paths during the upgrade (dpkg while it
# unpacks, rpm after %post) and resolves a directory symlink the daemon may
# have placed at or under that path while doing so: on rpm 4.16 a link
# pointing at the new docroot made it erase the files it had just installed.
# Drop every link the daemon account owns there first. Removing a link never
# follows it, and nothing the package still ships lives under this path. Only
# links owned by that account are touched: a link the operator made as root
# (a tarball-installed server pointing docroot at its own tree) is theirs,
# and the daemon can only have planted links it owns. The agent package
# shares this script; when the package manager says which package is being
# installed (dpkg does, rpm does not), the agent package skips this.
_pkg="${DPKG_MAINTSCRIPT_PACKAGE:-${RPM_PACKAGE_NAME:-proxiportd}}"
if [ "$_pkg" != proxiportd ]; then
    :
elif [ -L /var/lib/proxiport/docroot ]; then
    if [ "$(stat -c %U /var/lib/proxiport/docroot 2>/dev/null)" = proxiport ]; then
        rm -f /var/lib/proxiport/docroot
    fi
elif [ -d /var/lib/proxiport/docroot ]; then
    find /var/lib/proxiport/docroot -mindepth 1 -type l -user proxiport -delete || {
        echo "proxiport: could not remove a link under /var/lib/proxiport/docroot; refusing to continue the upgrade" >&2
        exit 1
    }
fi

exit 0
