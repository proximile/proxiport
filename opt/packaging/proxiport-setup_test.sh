#!/usr/bin/env bash
#======================================================================
# Regression tests for proxiport-setup.sh.
#
# Guards the classes of automation hazard that bit scripted / CI / rootless
# runs:
#   - confirm() must never loop forever on a non-interactive (EOF) stdin.
#   - the initial passwords must not be echoed to stdout on a non-TTY run.
#   - monitoring retention must be written to the key the server actually
#     reads ([monitoring] data_storage_duration), not a dead one.
#
# Runs with plain bash — no root, no install, no network. Sources the script
# in PROXIPORT_SETUP_LIB_ONLY mode to get its functions without its main flow.
#======================================================================
set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/proxiport-setup.sh"

fail=0
ok()   { printf '  ok   - %s\n' "$1"; }
bad()  { printf '  FAIL - %s\n' "$1"; fail=1; }

#----------------------------------------------------------------------
# 0. Syntax must parse.
#----------------------------------------------------------------------
if bash -n "$SCRIPT"; then ok "script parses (bash -n)"; else bad "bash -n failed"; fi

# shellcheck is advisory here (not always installed); run it when present.
if command -v shellcheck >/dev/null 2>&1; then
    if shellcheck -S error "$SCRIPT"; then ok "shellcheck (severity=error) clean"
    else bad "shellcheck reported errors"; fi
else
    printf '  skip - shellcheck not installed\n'
fi

#----------------------------------------------------------------------
# A1. confirm() must not hang on a non-interactive stdin.
#     Source the helpers, force a non-terminal stdin, and bound the call
#     with `timeout` so a regression to the old infinite loop FAILS here
#     instead of hanging CI.
#----------------------------------------------------------------------
# shellcheck disable=SC1090
run_confirm() { # $1 = ASSUME_YES value ; stdin is closed (</dev/null)
    # Source first (which resets ASSUME_YES to its default), THEN override, so
    # the test controls the value the way a parsed --assume-yes flag would.
    PROXIPORT_SETUP_LIB_ONLY=1 \
        timeout 10 bash -c '
            source "$1"
            ASSUME_YES="$2"
            confirm "proceed?"
        ' _ "$SCRIPT" "$1" </dev/null >/dev/null 2>&1
}

run_confirm 0
rc=$?
if [ "$rc" -eq 124 ]; then
    bad "A1: confirm() HUNG on non-interactive stdin (timed out)"
elif [ "$rc" -ne 0 ]; then
    ok "A1: confirm() fails fast on non-interactive stdin without --assume-yes"
else
    bad "A1: confirm() returned success without --assume-yes on a non-TTY"
fi

run_confirm 1
rc=$?
if [ "$rc" -eq 0 ]; then
    ok "A1: confirm() honors ASSUME_YES on non-interactive stdin"
elif [ "$rc" -eq 124 ]; then
    bad "A1: confirm() HUNG even with ASSUME_YES (timed out)"
else
    bad "A1: confirm() did not return success with ASSUME_YES (rc=$rc)"
fi

#----------------------------------------------------------------------
# A2. Passwords must not be echoed to stdout on a non-TTY run.
#     Assert the summary block is gated on is_terminal / SHOW_SECRETS.
#----------------------------------------------------------------------
if grep -Eq 'if is_terminal \|\| \[ "\$SHOW_SECRETS" -eq 1 \]' "$SCRIPT"; then
    ok "A2: stdout password block is gated on TTY / --show-secrets"
else
    bad "A2: could not find the TTY/--show-secrets gate around the password output"
fi
if grep -Eq -- '--show-secrets' "$SCRIPT"; then
    ok "A2: --show-secrets opt-in flag present"
else
    bad "A2: --show-secrets flag missing"
fi

#----------------------------------------------------------------------
# A3. Monitoring retention must target the key the server reads, and the
#     dead [server] data_storage_days write must be gone. Also prove the
#     real set_in_section() correctly rewrites a commented [monitoring] key.
#----------------------------------------------------------------------
if grep -Eq 'set_in_section "monitoring" "data_storage_duration"' "$SCRIPT"; then
    ok "A3: writes [monitoring] data_storage_duration"
else
    bad "A3: does not write [monitoring] data_storage_duration"
fi
if grep -Eq 'set_in_section "server" +"data_storage_days"' "$SCRIPT"; then
    bad "A3: still writes the dead [server] data_storage_days key"
else
    ok "A3: no longer writes the dead [server] data_storage_days key"
fi

# Functional: extract the real set_in_section() and run it against a temp
# conf shaped like proxiportd.example.conf (commented key under [monitoring]).
tmp_conf="$(mktemp)"
trap 'rm -f "$tmp_conf"' EXIT
cat >"$tmp_conf" <<'CONF'
[server]
  address = "0.0.0.0:80"

[monitoring]
  #data_storage_duration = "7d"
CONF

# shellcheck disable=SC1090
eval "$(sed -n '/^set_in_section() {/,/^}/p' "$SCRIPT")"
CONFIG_FILE="$tmp_conf"
set_in_section "monitoring" "data_storage_duration" "\"7d\""

if grep -Eq '^\s*data_storage_duration = "7d"' "$tmp_conf" \
   && ! grep -Eq '^\s*#\s*data_storage_duration' "$tmp_conf"; then
    ok "A3: set_in_section uncomments+sets the [monitoring] retention key"
else
    bad "A3: set_in_section did not set the [monitoring] retention key"
    cat "$tmp_conf"
fi

echo
if [ "$fail" -eq 0 ]; then
    echo "proxiport-setup_test: PASS"
else
    echo "proxiport-setup_test: FAIL"
fi
exit "$fail"
