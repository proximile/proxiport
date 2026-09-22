#!/usr/bin/env bash
#
# Asserts the boundary the agent/server account split exists to create, against
# the real preinstall.sh and postinstall.sh, on a fresh install and on an
# upgrade from the shared-account layout every deployed host currently has.
#
# The property under test is one sentence: the account the AGENT runs as must
# not be able to read the server's secrets or write its state. The agent
# executes operator-supplied commands as its own uid, so while the two shared
# `proxiport` an operator holding only the `commands` permission for one
# managed host could read jwt_secret (forge an admin session for the whole
# fleet) and key_seed (impersonate the server to every agent).
#
# Needs root and a throwaway filesystem, so it runs itself inside a container
# unless it is already root. It never skips: a check that quietly does not run
# reads exactly like a check that passed.

set -euo pipefail

IMAGE="${ACCOUNT_SPLIT_TEST_IMAGE:-debian:bookworm}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

failures=0

fail() { printf '  FAIL  %s\n' "$*"; failures=$((failures + 1)); }
pass() { printf '  ok    %s\n' "$*"; }

# ---------------------------------------------------------------------------
# Re-exec into a container unless we are already root in a disposable place.
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
    if ! command -v docker >/dev/null 2>&1; then
        echo "account_split_test: needs root or docker; refusing to skip." >&2
        echo "  Run it as root in a throwaway container, or install docker." >&2
        exit 1
    fi
    echo "account_split_test: re-running as root inside $IMAGE"
    exec docker run --rm -v "$REPO_ROOT":/src:ro -w /src "$IMAGE" \
        bash /src/opt/packaging/account_split_test.sh
fi

SRC="${REPO_ROOT}"

# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

lay_payload() {
    install -d /etc/proxiport /usr/bin /lib/systemd/system
    cp "$SRC/proxiportd.example.conf" /etc/proxiport/proxiportd.example.conf
    cp "$SRC/proxiport.example.conf"  /etc/proxiport/proxiport.example.conf
    cp "$SRC/opt/systemd/proxiportd.service" /lib/systemd/system/proxiportd.service
    cp "$SRC/opt/systemd/proxiport.service"  /lib/systemd/system/proxiport.service
    # The scriptlets only test -x these.
    printf '#!/bin/sh\nexit 0\n' > /usr/bin/proxiportd
    printf '#!/bin/sh\nexit 0\n' > /usr/bin/proxiport
    chmod 0755 /usr/bin/proxiportd /usr/bin/proxiport
    install -d /var/lib/proxiport/docroot
    printf 'spa\n' > /var/lib/proxiport/docroot/index.html
}

# seed_previous_release recreates what a 0.9.x install left behind: one shared
# account, one shared data directory, one shared log directory.
seed_previous_release() {
    groupadd --system proxiport
    useradd --system --gid proxiport --home-dir /var/lib/proxiport \
            --shell /usr/sbin/nologin --comment "ProxiPort daemon user" proxiport
    lay_payload
    install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
    install -d -o proxiport -g proxiport -m 0750 /var/log/proxiport
    for conf in proxiportd proxiport; do
        cp "/etc/proxiport/${conf}.example.conf" "/etc/proxiport/${conf}.conf"
        chmod 0640 "/etc/proxiport/${conf}.conf"
        chown root:proxiport "/etc/proxiport/${conf}.conf"
    done
    # The config is what actually holds the secrets.
    sed -i 's|key_seed = "<YOUR_SEED>"|key_seed = "SEEDSEEDSEED"|' /etc/proxiport/proxiportd.conf
    for f in jobs.db auditlog.db clients.db user-auth.db vault.db; do
        printf 'db\n' > "/var/lib/proxiport/$f"
        chown proxiport:proxiport "/var/lib/proxiport/$f"
        chmod 0600 "/var/lib/proxiport/$f"
    done
    printf 'admin:oldpassword\n' > /var/lib/proxiport/initial-admin-password
    chown root:proxiport /var/lib/proxiport/initial-admin-password
    chmod 0640 /var/lib/proxiport/initial-admin-password
    printf '{"watchdog":true}\n' > /var/lib/proxiport/state.json
    chown proxiport:proxiport /var/lib/proxiport/state.json
    printf 'agent log\n' > /var/log/proxiport/proxiport.log
    chown proxiport:proxiport /var/log/proxiport/proxiport.log
    chown -R proxiport:proxiport /var/lib/proxiport/docroot
    # The agent's config still names the shared paths, as an upgraded host's does.
    sed -i 's|^\(\s*\)log_file = "/var/log/proxiport-agent/proxiport.log"|\1log_file = "/var/log/proxiport/proxiport.log"|' \
        /etc/proxiport/proxiport.conf
}

reset_host() {
    rm -rf /etc/proxiport /var/lib/proxiport /var/lib/proxiport-agent \
           /var/log/proxiport /var/log/proxiport-agent /lib/systemd/system/proxiport*.service
    for u in proxiport proxiport-agent; do
        getent passwd "$u" >/dev/null 2>&1 && userdel "$u" 2>/dev/null || true
        getent group  "$u" >/dev/null 2>&1 && groupdel "$u" 2>/dev/null || true
    done
}

# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------

as_user_can_read() { su -s /bin/sh "$1" -c "cat '$2' >/dev/null 2>&1"; }
as_user_can_write_dir() {
    su -s /bin/sh "$1" -c "touch '$2/.probe' 2>/dev/null" && rm -f "$2/.probe"
}

unit_user() { sed -n 's/^User=//p' "$1" | head -1; }

assert_boundary() {
    local scenario="$1"
    local agent server
    agent="$(unit_user /lib/systemd/system/proxiport.service)"
    server="$(unit_user /lib/systemd/system/proxiportd.service)"

    printf '\n=== %s ===\n' "$scenario"

    if [ "$agent" = "$server" ]; then
        fail "the agent and the server run as the same account ($agent)"
        return
    fi
    pass "agent runs as $agent, server as $server"

    # The whole point: nothing of the server's is reachable from the agent's uid.
    local secret
    for secret in /etc/proxiport/proxiportd.conf \
                  /var/lib/proxiport/jobs.db \
                  /var/lib/proxiport/vault.db \
                  /var/lib/proxiport/initial-admin-password; do
        [ -e "$secret" ] || continue
        if as_user_can_read "$agent" "$secret"; then
            fail "$agent can read $secret"
        else
            pass "$agent cannot read $secret"
        fi
    done

    if as_user_can_write_dir "$agent" /var/lib/proxiport; then
        fail "$agent can write the server's data directory"
    else
        pass "$agent cannot write the server's data directory"
    fi

    # ... and each daemon can still do its own job.
    if as_user_can_write_dir "$agent" /var/lib/proxiport-agent \
       && as_user_can_write_dir "$agent" /var/log/proxiport-agent; then
        pass "$agent can write its own state and log directories"
    else
        fail "$agent cannot write its own state or log directory"
    fi

    if as_user_can_read "$agent" /etc/proxiport/proxiport.conf; then
        pass "$agent can read its own config"
    else
        fail "$agent cannot read its own config"
    fi

    if as_user_can_write_dir "$server" /var/lib/proxiport \
       && as_user_can_write_dir "$server" /var/log/proxiport; then
        pass "$server can write its own state and log directories"
    else
        fail "$server cannot write its own state or log directory"
    fi

    # H11: the server has to be able to destroy the installer credentials it is
    # documented to destroy. Opening for writing is shredFile's first step.
    local cred=/var/lib/proxiport/initial-admin-password
    if [ -f "$cred" ]; then
        if su -s /bin/sh "$server" -c "exec 3>>'$cred'" 2>/dev/null; then
            pass "$server can overwrite the installer credential file"
        else
            fail "$server cannot open $cred for writing, so the shred will not overwrite it"
        fi
    fi

    # The agent's configured paths have to be ones it can actually use.
    if grep -qE '^\s*log_file\s*=\s*"/var/log/proxiport/' /etc/proxiport/proxiport.conf; then
        fail "the agent's config still points log_file at the server's log directory"
    else
        pass "the agent's config points log_file somewhere the agent can write"
    fi
}

# ---------------------------------------------------------------------------
# Scenarios
# ---------------------------------------------------------------------------

reset_host
lay_payload
sh "$SRC/opt/packaging/preinstall.sh"
sh "$SRC/opt/packaging/postinstall.sh" >/dev/null 2>&1
assert_boundary "fresh install"

reset_host
seed_previous_release
sh "$SRC/opt/packaging/preinstall.sh"
sh "$SRC/opt/packaging/postinstall.sh" configure 0.9.0 >/dev/null 2>&1
assert_boundary "upgrade from the shared-account layout"

# The upgrade has to carry the agent's own state across, not strand it under
# the server's account.
if [ -f /var/lib/proxiport-agent/state.json ]; then
    pass "the agent's state.json moved to its new data directory"
else
    fail "the agent's state.json was left behind under the server's account"
fi

printf '\n'
if [ "$failures" -ne 0 ]; then
    printf '%d assertion(s) failed\n' "$failures"
    exit 1
fi
printf 'account split: all assertions passed\n'
