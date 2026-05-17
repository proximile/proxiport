#!/bin/sh
# Pre-install hook for the proxiport / proxiportd deb and rpm packages.
# Creates the unprivileged `proxiport` system user and group used by
# both daemons. POSIX shell — runs under dpkg and rpm scriptlets.

set -e

if ! getent group proxiport >/dev/null 2>&1; then
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system proxiport
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup --system proxiport
    fi
fi

if ! getent passwd proxiport >/dev/null 2>&1; then
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --gid proxiport \
                --home-dir /var/lib/proxiport \
                --shell /usr/sbin/nologin \
                --comment "ProxiPort daemon user" \
                proxiport
    elif command -v adduser >/dev/null 2>&1; then
        adduser --system --ingroup proxiport \
                --home /var/lib/proxiport \
                --shell /usr/sbin/nologin \
                --gecos "ProxiPort daemon user" \
                proxiport
    fi
fi

exit 0
