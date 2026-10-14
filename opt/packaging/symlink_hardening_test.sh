#!/usr/bin/env bash
#
# Asserts that the maintainer scripts never follow a link planted in a
# directory a daemon can write, against the real preinstall.sh and
# postinstall.sh.
#
# /var/lib/proxiport and /var/log/proxiport belong to the server's account, and
# postinstall.sh runs as root on every upgrade. A chown, chmod, mv or redirect
# on a path in there follows whatever that path has become in the meantime, so
# code execution as the daemon's uid used to turn into "give /etc/shadow to my
# uid" by planting a link and waiting for the next package upgrade. The
# property under test: with links planted at every path the hook touches, an
# upgrade leaves every link target exactly as it was, a first install never
# creates a link's target, a genuine legacy file is still fixed up, and every
# refusal is reported rather than silent. The same is checked with the daemon
# directories mounted as their own filesystems, where the hook cannot stage a
# rename and has to work differently.
#
# Needs root and a throwaway filesystem, so it runs itself inside containers
# unless it is already root. It never skips: a check that quietly does not run
# reads exactly like a check that passed.

set -euo pipefail

IMAGE="${SYMLINK_HARDENING_TEST_IMAGE:-debian:bookworm}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCENARIO="${SYMLINK_HARDENING_SCENARIO:-same-fs}"

failures=0

fail() { printf '  FAIL  %s\n' "$*"; failures=$((failures + 1)); }
pass() { printf '  ok    %s\n' "$*"; }

# ---------------------------------------------------------------------------
# Re-exec into containers unless we are already root in a disposable place.
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
    if ! command -v docker >/dev/null 2>&1; then
        echo "symlink_hardening_test: needs root or docker; refusing to skip." >&2
        echo "  Run it as root in a throwaway container, or install docker." >&2
        exit 1
    fi
    echo "symlink_hardening_test: re-running as root inside $IMAGE"
    docker run --rm -e SYMLINK_HARDENING_IN_CONTAINER=1 \
        -v "$REPO_ROOT":/src:ro -w /src "$IMAGE" \
        bash /src/opt/packaging/symlink_hardening_test.sh
    echo "symlink_hardening_test: re-running with the daemon directories as their own mounts"
    exec docker run --rm -e SYMLINK_HARDENING_IN_CONTAINER=1 \
        -e SYMLINK_HARDENING_SCENARIO=separate-mount \
        --tmpfs /var/lib/proxiport:mode=750 --tmpfs /var/log/proxiport:mode=750 \
        -v "$REPO_ROOT":/src:ro -w /src "$IMAGE" \
        bash /src/opt/packaging/symlink_hardening_test.sh
fi

# Root on a real host would have its /etc/proxiport, its daemon directories
# and its proxiport accounts reset below. Only run where that is disposable.
if [ -z "${SYMLINK_HARDENING_IN_CONTAINER:-}" ] && [ ! -e /.dockerenv ]; then
    echo "symlink_hardening_test: refusing to run as root outside a container:" >&2
    echo "  it resets /etc/proxiport, /var/lib/proxiport, /var/log/proxiport and the proxiport accounts." >&2
    exit 1
fi

SRC="${REPO_ROOT}"
HOOK_ERR=/tmp/symlink_hardening_hook.stderr

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
    # The package ships the SPA root-owned and world-readable, under /usr/share.
    install -d -m 0755 /usr/share/proxiport/docroot
    printf 'spa\n' > /usr/share/proxiport/docroot/index.html
    chmod 0644 /usr/share/proxiport/docroot/index.html
}

clear_dir() {
    # Empties a directory, or removes it outright when it is not a mount
    # point: the separate-mount run cannot remove its mount points.
    if mountpoint -q "$1" 2>/dev/null; then
        find "$1" -mindepth 1 -delete
    else
        rm -rf "$1"
    fi
}

reset_host() {
    for d in /etc/proxiport /var/lib/proxiport /var/lib/proxiport-agent \
             /var/log/proxiport /var/log/proxiport-agent /usr/share/proxiport; do
        clear_dir "$d"
    done
    rm -rf /lib/systemd/system/proxiport*.service /etc/proxiport-planted
    rm -rf /var/lib/.proxiport-postinst.* /var/log/.proxiport-postinst.*
    chmod 0755 /var/log
    for u in proxiport proxiport-agent; do
        getent passwd "$u" >/dev/null 2>&1 && userdel "$u" 2>/dev/null || true
        getent group  "$u" >/dev/null 2>&1 && groupdel "$u" 2>/dev/null || true
    done
}

# What each account can do: everything inside its own directories.
as_server() { su -s /bin/sh proxiport -c "$1"; }
as_agent()  { su -s /bin/sh proxiport-agent -c "$1"; }

# Owner, group, mode and link count, without following links.
ident() { stat -c '%U:%G %a %h' "$1"; }

# The hook, with its stdout dropped and its stderr kept for the assertions.
run_hook() { sh "$SRC/opt/packaging/postinstall.sh" "$@" >/dev/null 2>"$HOOK_ERR"; }

check_unchanged() {
    local path="$1" before="$2"
    if [ "$(ident "$path")" = "$before" ]; then
        pass "$path unchanged ($before)"
    else
        fail "$path changed hands: $before -> $(ident "$path")"
    fi
}

check_warned() {
    local pattern="$1" what="$2"
    if grep -qE "$pattern" "$HOOK_ERR"; then
        pass "the hook reported $what"
    else
        fail "the hook did not report $what (stderr: $(tr '\n' '|' < "$HOOK_ERR"))"
    fi
}

check_no_leftover_staging() {
    if ls -d /var/lib/.proxiport-postinst.* /var/log/.proxiport-postinst.* >/dev/null 2>&1; then
        fail "the hook left a staging directory behind"
    else
        pass "no staging directory left behind"
    fi
}

check_doc_root() {
    local want="$1"
    if grep -qE "^[[:space:]]*doc_root[[:space:]]*=[[:space:]]*\"$want\"" /etc/proxiport/proxiportd.conf; then
        pass "doc_root is $want"
    else
        fail "doc_root is not $want: $(grep -E 'doc_root' /etc/proxiport/proxiportd.conf | tr '\n' '|')"
    fi
}

# ---------------------------------------------------------------------------
# Scenario 1: an upgrade with a link planted at every path the hook touches
# ---------------------------------------------------------------------------

scenario_links_on_upgrade() {
    printf '\n=== upgrade with links planted by the server account ===\n'
    reset_host
    lay_payload
    sh "$SRC/opt/packaging/preinstall.sh"
    run_hook

    # An upgraded host still has the SPA where the old package put it, in the
    # daemon's own tree, and its config still points there.
    install -d -o proxiport -g proxiport -m 0755 /var/lib/proxiport/docroot
    sed -i 's|doc_root = "/usr/share/proxiport/docroot"|doc_root = "/var/lib/proxiport/docroot"|' /etc/proxiport/proxiportd.conf
    check_doc_root /var/lib/proxiport/docroot

    shadow_before="$(ident /etc/shadow)"
    passwd_before="$(ident /etc/passwd)"
    group_before="$(ident /etc/group)"

    # The credential files are chowned/chmodded on every upgrade; the agent's
    # state and log are moved and chowned. Point each at a root-owned file.
    as_server 'rm -f /var/lib/proxiport/initial-admin-password
               ln -s /etc/shadow /var/lib/proxiport/initial-admin-password
               ln -s /etc/passwd /var/lib/proxiport/state.json
               ln -s /etc/group  /var/log/proxiport/proxiport.log'

    # A genuine legacy credential file (0640 root:proxiport, as pre-0.10
    # installs wrote it) sits next to the links and must still be handed to
    # the daemon.
    chown root:proxiport /var/lib/proxiport/initial-client-auth
    chmod 0640 /var/lib/proxiport/initial-client-auth
    client_auth_before="$(cat /var/lib/proxiport/initial-client-auth)"

    if run_hook configure 0.10.0; then
        pass "the hook completes the upgrade with links planted"
    else
        fail "the hook failed the upgrade with links planted"
    fi

    check_unchanged /etc/shadow "$shadow_before"
    check_unchanged /etc/passwd "$passwd_before"
    check_unchanged /etc/group  "$group_before"

    for p in /var/lib/proxiport-agent/state.json /var/log/proxiport-agent/proxiport.log; do
        if [ -L "$p" ]; then
            fail "$p is the planted link, carried over and chowned"
        elif [ -e "$p" ]; then
            fail "$p exists although the source was a link"
        else
            pass "$p is not a carried-over link"
        fi
    done
    check_warned 'initial-admin-password is a link' "the link at the credential path"
    check_warned 'state.json .* is a link' "the link at the agent's state path"
    check_warned 'proxiport.log .* is a link' "the link at the agent's log path"

    if [ "$(ident /var/lib/proxiport/initial-client-auth)" = "proxiport:proxiport 600 1" ] \
       && [ "$(cat /var/lib/proxiport/initial-client-auth)" = "$client_auth_before" ]; then
        pass "the legacy credential file was handed to the daemon intact"
    else
        fail "the legacy credential file was not fixed up: $(ident /var/lib/proxiport/initial-client-auth)"
    fi

    check_doc_root /usr/share/proxiport/docroot
    if as_server 'cat /usr/share/proxiport/docroot/index.html >/dev/null'; then
        pass "the daemon can read the shipped SPA without the hook chowning it"
    else
        fail "the daemon cannot read the shipped SPA"
    fi
    if [ "$(ident /usr/share/proxiport/docroot)" = "root:root 755 2" ]; then
        pass "the shipped SPA tree is still root's"
    else
        fail "the shipped SPA tree changed hands: $(ident /usr/share/proxiport/docroot)"
    fi

    check_no_leftover_staging
}

# ---------------------------------------------------------------------------
# Scenario 2: a first install over a data directory holding a dangling link
# ---------------------------------------------------------------------------
# A redirect creates whatever a dangling link points at. The data directory
# may pre-exist on a first install (a remove without purge), so the credential
# files must be created without ever opening the destination path.

scenario_first_install_dangling_link() {
    printf '\n=== first install over a data directory with a dangling link ===\n'
    reset_host
    lay_payload
    sh "$SRC/opt/packaging/preinstall.sh"
    install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
    as_server 'ln -s /etc/proxiport-planted /var/lib/proxiport/initial-admin-password'

    if run_hook; then
        pass "the hook completes the first install with a dangling link planted"
    else
        fail "the hook failed the first install with a dangling link planted"
    fi

    if [ -e /etc/proxiport-planted ] || [ -L /etc/proxiport-planted ]; then
        fail "root created the link's target /etc/proxiport-planted ($(ident /etc/proxiport-planted))"
    else
        pass "the link's target was not created"
    fi

    if [ -L /var/lib/proxiport/initial-admin-password ]; then
        pass "the planted link was left alone"
    else
        fail "the planted link was replaced or followed"
    fi
    check_warned 'initial-admin-password exists and is not a plain file' "the credential it could not write"

    if [ -f /var/lib/proxiport/initial-client-auth ] && [ ! -L /var/lib/proxiport/initial-client-auth ] \
       && [ "$(ident /var/lib/proxiport/initial-client-auth)" = "proxiport:proxiport 600 1" ]; then
        pass "the other credential file was still written 0600 proxiport:proxiport"
    else
        fail "the other credential file is wrong or missing"
    fi

    check_doc_root /usr/share/proxiport/docroot
    check_no_leftover_staging
}

# ---------------------------------------------------------------------------
# Scenario 3: a hard link, a directory in the way, and a writable /var/log
# ---------------------------------------------------------------------------
# A hard link shares the target's inode, so a chown on it is a chown on the
# target; the hook must refuse any file with more than one name. A directory
# at the agent's state path is in the way of the move and must stop it. And
# Ubuntu's rsyslog makes /var/log group-writable, which rules it out as the
# place for a staging directory: the hook has to stage elsewhere and still
# move the log.

scenario_hard_link_dir_and_writable_log() {
    printf '\n=== upgrade with a hard link, a directory in the way, and a group-writable /var/log ===\n'
    reset_host
    lay_payload
    sh "$SRC/opt/packaging/preinstall.sh"
    run_hook

    # A doc_root the operator chose is theirs.
    sed -i 's|doc_root = "/usr/share/proxiport/docroot"|doc_root = "/srv/custom-ui"|' /etc/proxiport/proxiportd.conf

    # Planted as root: it stands in for fs.protected_hardlinks=0, which is a
    # host-wide sysctl and cannot be set inside the container.
    rm -f /var/lib/proxiport/initial-admin-password
    ln /etc/shadow /var/lib/proxiport/initial-admin-password
    shadow_before="$(ident /etc/shadow)"

    as_server 'printf "{}\n" > /var/lib/proxiport/state.json
               printf "agent log\n" > /var/log/proxiport/proxiport.log'
    # su's umask differs between images (0022 on Debian, 0002 on Ubuntu), so
    # the mode the hook must preserve is whatever the fixture got.
    log_mode_before="$(stat -c '%a' /var/log/proxiport/proxiport.log)"
    as_agent 'mkdir /var/lib/proxiport-agent/state.json'
    chmod g+w /var/log

    if run_hook configure 0.10.0; then
        pass "the hook completes the upgrade"
    else
        fail "the hook failed the upgrade"
    fi
    chmod 0755 /var/log

    check_unchanged /etc/shadow "$shadow_before"
    if [ "$(ident /var/lib/proxiport/initial-admin-password)" = "$shadow_before" ]; then
        pass "the hard link to /etc/shadow was not chowned"
    else
        fail "the hard link to /etc/shadow changed hands: $(ident /var/lib/proxiport/initial-admin-password)"
    fi
    check_warned 'initial-admin-password could not be handed' "the hard link it refused"

    if [ -f /var/lib/proxiport/state.json ] && [ -d /var/lib/proxiport-agent/state.json ]; then
        pass "the agent's state stayed put with a directory at its destination"
    else
        fail "the agent's state or the directory in its way is gone"
    fi

    if [ -f /var/log/proxiport-agent/proxiport.log ] && [ ! -L /var/log/proxiport-agent/proxiport.log ] \
       && [ "$(ident /var/log/proxiport-agent/proxiport.log)" = "proxiport-agent:proxiport-agent $log_mode_before 1" ]; then
        pass "the agent's log was moved although /var/log was group-writable"
    else
        fail "the agent's log was not moved: $(ls -la /var/log/proxiport /var/log/proxiport-agent 2>&1 | tr '\n' '|')"
    fi

    check_doc_root /srv/custom-ui
    check_no_leftover_staging

    # The log moved either way; what matters is where it was staged. Borrow
    # the hook's own helper and show that a group-writable /var/log is
    # refused in favour of /var/lib.
    eval "$(sed -n '/^root_only_dir()/,/^}/p; /^stage_dir()/,/^}/p' "$SRC/opt/packaging/postinstall.sh")"
    chmod g+w /var/log
    staged="$(stage_dir /var/log/proxiport || true)"
    chmod 0755 /var/log
    case "$staged" in
        /var/lib/.proxiport-postinst.*) pass "a group-writable /var/log is refused for staging; /var/lib is used instead" ;;
        *) fail "staging went to '$staged' with /var/log group-writable" ;;
    esac
    [ -n "$staged" ] && rmdir "$staged"

    # The descriptor-based handover must refuse what is not a plain file, and
    # must not hang on a FIFO (a read-only open of one blocks for a writer).
    eval "$(sed -n '/^hand_over_file()/,/^}/p' "$SRC/opt/packaging/postinstall.sh")"
    rm -rf /tmp/sh-dir /tmp/sh-fifo /tmp/sh-plain
    mkdir /tmp/sh-dir
    mkfifo /tmp/sh-fifo
    printf 'x\n' > /tmp/sh-plain
    if hand_over_file /tmp/sh-dir proxiport:proxiport 0600 2>/dev/null; then
        fail "hand_over_file accepted a directory"
    else
        pass "hand_over_file refuses a directory"
    fi
    if timeout 10 sh -c "$(sed -n '/^hand_over_file()/,/^}/p' "$SRC/opt/packaging/postinstall.sh"); hand_over_file /tmp/sh-fifo proxiport:proxiport 0600" 2>/dev/null; then
        fail "hand_over_file accepted a FIFO"
    elif [ "$(ident /tmp/sh-fifo)" = "root:root 644 1" ]; then
        pass "hand_over_file refuses a FIFO without blocking"
    else
        fail "hand_over_file changed the FIFO: $(ident /tmp/sh-fifo)"
    fi
    if hand_over_file /tmp/sh-plain proxiport:proxiport 0600 && [ "$(ident /tmp/sh-plain)" = "proxiport:proxiport 600 1" ]; then
        pass "hand_over_file hands over a plain file"
    else
        fail "hand_over_file did not hand over a plain file: $(ident /tmp/sh-plain)"
    fi
    rm -rf /tmp/sh-dir /tmp/sh-fifo /tmp/sh-plain
}

# ---------------------------------------------------------------------------
# Scenario 4: the daemon directories are their own mounts
# ---------------------------------------------------------------------------
# No staging directory can be made on the same filesystem, so the credential
# files are created with O_EXCL and fixed up through an open descriptor, and
# the moves across directories are reported instead of attempted.

scenario_separate_mounts() {
    printf '\n=== first install and upgrade with the daemon directories as their own mounts ===\n'
    if ! mountpoint -q /var/lib/proxiport || ! mountpoint -q /var/log/proxiport; then
        fail "/var/lib/proxiport and /var/log/proxiport are not mount points; this scenario needs --tmpfs for both"
        return
    fi
    reset_host
    lay_payload
    sh "$SRC/opt/packaging/preinstall.sh"
    install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport
    as_server 'ln -s /etc/proxiport-planted /var/lib/proxiport/initial-admin-password'

    if run_hook; then
        pass "the hook completes the first install"
    else
        fail "the hook failed the first install"
    fi
    if [ -e /etc/proxiport-planted ] || [ -L /etc/proxiport-planted ]; then
        fail "root created the link's target /etc/proxiport-planted"
    else
        pass "the link's target was not created"
    fi
    if [ -f /var/lib/proxiport/initial-client-auth ] && [ ! -L /var/lib/proxiport/initial-client-auth ] \
       && [ "$(ident /var/lib/proxiport/initial-client-auth)" = "proxiport:proxiport 600 1" ]; then
        pass "the credential file was written 0600 proxiport:proxiport without a staging directory"
    else
        fail "the credential file is wrong or missing: $(ls -la /var/lib/proxiport | tr '\n' '|')"
    fi
    check_no_leftover_staging

    # Now an upgrade: a genuine legacy credential, the agent's real state, a
    # link at the log path.
    group_before="$(ident /etc/group)"
    chown root:proxiport /var/lib/proxiport/initial-client-auth
    chmod 0640 /var/lib/proxiport/initial-client-auth
    client_auth_before="$(cat /var/lib/proxiport/initial-client-auth)"
    as_server 'printf "{}\n" > /var/lib/proxiport/state.json
               ln -s /etc/group /var/log/proxiport/proxiport.log'

    if run_hook configure 0.10.0; then
        pass "the hook completes the upgrade"
    else
        fail "the hook failed the upgrade"
    fi
    check_unchanged /etc/group "$group_before"
    if [ "$(ident /var/lib/proxiport/initial-client-auth)" = "proxiport:proxiport 600 1" ] \
       && [ "$(cat /var/lib/proxiport/initial-client-auth)" = "$client_auth_before" ]; then
        pass "the legacy credential file was handed to the daemon on its own mount"
    else
        fail "the legacy credential file was not fixed up on its own mount: $(ident /var/lib/proxiport/initial-client-auth)"
    fi
    if [ -f /var/lib/proxiport/state.json ] && [ ! -e /var/lib/proxiport-agent/state.json ]; then
        pass "the agent's state stayed put across the mount boundary"
    else
        fail "the agent's state was moved or lost across the mount boundary"
    fi
    check_warned 'state.json not moved .*separate filesystem' "the move it could not make"
    check_warned 'proxiport.log .* is a link' "the link at the agent's log path"
    check_no_leftover_staging
}

# ---------------------------------------------------------------------------
# Scenario 5: the old docroot holds links when the next package is unpacked,
#             and doc_root is spelled in ways the repoint must handle
# ---------------------------------------------------------------------------
# The package manager erases the obsolete 0.11.0 docroot paths during the
# upgrade and follows a directory link while doing so (rpm 4.16 erased the
# freshly installed SPA through a link to it). preinstall must drop every link
# there first, without touching anything else, and must never follow one.

scenario_preinstall_drops_links_in_old_docroot() {
    printf '\n=== preinstall drops the daemon'"'"'s links in the old docroot; doc_root spellings ===\n'
    reset_host
    lay_payload
    sh "$SRC/opt/packaging/preinstall.sh"
    run_hook

    # The old tree, daemon-owned, with the daemon's links in and at it.
    install -d -o proxiport -g proxiport -m 0755 /var/lib/proxiport/docroot
    as_server 'printf "old\n" > /var/lib/proxiport/docroot/index.html
               ln -s /etc /var/lib/proxiport/docroot/_app
               mkdir /var/lib/proxiport/docroot/assets
               ln -s /etc/shadow /var/lib/proxiport/docroot/assets/app.css'
    etc_before="$(ident /etc)"
    shadow_before="$(ident /etc/shadow)"

    if DPKG_MAINTSCRIPT_PACKAGE=proxiportd sh "$SRC/opt/packaging/preinstall.sh" upgrade 0.11.0; then
        pass "preinstall completes with links under the old docroot"
    else
        fail "preinstall failed with links under the old docroot"
    fi
    if [ ! -L /var/lib/proxiport/docroot/_app ] && [ ! -e /var/lib/proxiport/docroot/_app ] \
       && [ ! -L /var/lib/proxiport/docroot/assets/app.css ]; then
        pass "the links under the old docroot are gone"
    else
        fail "a link under the old docroot survived: $(ls -la /var/lib/proxiport/docroot /var/lib/proxiport/docroot/assets | tr '\n' '|')"
    fi
    if [ -f /var/lib/proxiport/docroot/index.html ] && [ -d /var/lib/proxiport/docroot/assets ]; then
        pass "the old docroot's regular entries were left for the package manager"
    else
        fail "preinstall removed more than links from the old docroot"
    fi
    check_unchanged /etc "$etc_before"
    check_unchanged /etc/shadow "$shadow_before"

    # Now the whole old docroot is a link to the new one. The agent package
    # runs the same script and must leave it alone (on a tarball-server host
    # it may be the operator's); the server package removes it.
    rm -rf /var/lib/proxiport/docroot
    as_server 'ln -s /usr/share/proxiport/docroot /var/lib/proxiport/docroot'
    if RPM_PACKAGE_NAME=proxiport sh "$SRC/opt/packaging/preinstall.sh" 2 \
       && [ -L /var/lib/proxiport/docroot ]; then
        pass "the agent package leaves a link at the old docroot alone"
    else
        fail "the agent package's preinstall touched the old docroot link"
    fi
    if sh "$SRC/opt/packaging/preinstall.sh" 2 \
       && [ ! -L /var/lib/proxiport/docroot ] && [ ! -e /var/lib/proxiport/docroot ] \
       && [ -f /usr/share/proxiport/docroot/index.html ]; then
        pass "a daemon-owned link at the old docroot is removed and its target untouched (rpm: no package name in the environment)"
    else
        fail "the link at the old docroot was not removed, or its target was: $(ls -la /var/lib/proxiport /usr/share/proxiport/docroot | tr '\n' '|')"
    fi
    # A link the operator made as root is theirs, whichever package runs this.
    ln -s /opt/spa /var/lib/proxiport/docroot
    if sh "$SRC/opt/packaging/preinstall.sh" 2 && [ -L /var/lib/proxiport/docroot ]; then
        pass "a root-owned link at the old docroot is left alone"
    else
        fail "preinstall removed the operator's root-owned link at the old docroot"
    fi
    rm -f /var/lib/proxiport/docroot

    # doc_root spellings: a quoted, slashed, commented default is still the
    # default and is repointed, keeping the comment ...
    sed -i "s|doc_root = \"/usr/share/proxiport/docroot\"|doc_root = '/var/lib/proxiport/docroot/' # web UI|" /etc/proxiport/proxiportd.conf
    run_hook configure 0.11.0
    if grep -qE '^[[:space:]]*doc_root = "/usr/share/proxiport/docroot" # web UI$' /etc/proxiport/proxiportd.conf; then
        pass "a single-quoted, slashed, commented default doc_root was repointed"
    else
        fail "the spelled-differently default was not repointed: $(grep doc_root /etc/proxiport/proxiportd.conf | tr '\n' '|')"
    fi
    # ... while a value the package cannot rewrite is reported, not ignored.
    sed -i 's|doc_root = "/usr/share/proxiport/docroot" # web UI|doc_root = "/var/lib/proxiport/docroot" # custom|' /etc/proxiport/proxiportd.conf
    sed -i 's|^\[api\]|[ api ]|' /etc/proxiport/proxiportd.conf
    run_hook configure 0.11.0
    check_doc_root /usr/share/proxiport/docroot
    sed -i 's|^\[ api \]|[api]|' /etc/proxiport/proxiportd.conf
    printf 'api.doc_root = "/var/lib/proxiport/docroot"\n' >> /etc/proxiport/proxiportd.conf
    run_hook configure 0.11.0
    check_warned 'still points doc_root at /var/lib/proxiport/docroot' "a doc_root it could not repoint"

    # A host whose server came from a tarball has /usr/bin/proxiportd but no
    # shipped SPA; the agent package's postinstall must not repoint its doc_root.
    sed -i 's|doc_root = "/usr/share/proxiport/docroot"|doc_root = "/var/lib/proxiport/docroot"|' /etc/proxiport/proxiportd.conf
    mv /usr/share/proxiport/docroot/index.html /tmp/index.html.aside
    run_hook configure 0.11.0
    mv /tmp/index.html.aside /usr/share/proxiport/docroot/index.html
    check_doc_root /var/lib/proxiport/docroot
    if ! grep -q 'still points doc_root' "$HOOK_ERR"; then
        pass "no doc_root warning on a host without the shipped SPA"
    else
        fail "the hook warned about doc_root on a host without the shipped SPA"
    fi
    sed -i 's|doc_root = "/var/lib/proxiport/docroot"|doc_root = "/usr/share/proxiport/docroot"|' /etc/proxiport/proxiportd.conf

    # A config that is a symlink stays one.
    mv /etc/proxiport/proxiportd.conf /etc/proxiport/real.conf
    ln -s real.conf /etc/proxiport/proxiportd.conf
    sed -i 's|doc_root = "/usr/share/proxiport/docroot"|doc_root = "/var/lib/proxiport/docroot"|' /etc/proxiport/real.conf
    run_hook configure 0.11.0
    if [ -L /etc/proxiport/proxiportd.conf ] && grep -q 'doc_root = "/usr/share/proxiport/docroot"' /etc/proxiport/real.conf; then
        pass "a symlinked config was edited through the link and is still a link"
    else
        fail "the symlinked config was replaced or not edited: $(ls -la /etc/proxiport | tr '\n' '|') $(grep doc_root /etc/proxiport/real.conf | tr '\n' '|')"
    fi
    rm -f /etc/proxiport/proxiportd.conf; mv /etc/proxiport/real.conf /etc/proxiport/proxiportd.conf
    check_no_leftover_staging
}

case "$SCENARIO" in
    same-fs)
        scenario_links_on_upgrade
        scenario_first_install_dangling_link
        scenario_hard_link_dir_and_writable_log
        scenario_preinstall_drops_links_in_old_docroot
        ;;
    separate-mount)
        scenario_separate_mounts
        ;;
    *)
        echo "symlink_hardening_test: unknown scenario '$SCENARIO'" >&2
        exit 1
        ;;
esac

printf '\n'
if [ "$failures" -ne 0 ]; then
    printf '%d assertion(s) failed\n' "$failures"
    exit 1
fi
printf 'symlink hardening (%s): all assertions passed\n' "$SCENARIO"
