#!/usr/bin/env bash
#======================================================================
# proxiport-setup — interactive finisher for the proxiportd package.
#
# Acts on an already-installed proxiportd .deb / .rpm: takes a FQDN,
# picks a TLS path (built-in ACME for public FQDNs on port 443,
# operator-supplied cert files otherwise), opens host + UFW / firewalld
# rules, creates a SQLite users / groups / group_details / clients_auth
# schema with a bcrypt-hashed admin password, enables TOTP-2FA by
# default, and starts the service.
#
# Modeled on the upstream openrport installer (get.openrport.io); the
# CLI flags match where it makes sense to match.
#
#   sudo proxiport-setup --fqdn proxiport.example.com
#   sudo proxiport-setup --fqdn x --email y --no-2fa
#   sudo proxiport-setup --uninstall
#======================================================================

set -e

CONFIG_FILE="/etc/proxiport/proxiportd.conf"
EXAMPLE_FILE="/etc/proxiport/proxiportd.example.conf"
DB_FILE="/var/lib/proxiport/user-auth.db"
SUMMARY_FILE="/root/proxiport-installation.txt"
BINARY="/usr/bin/proxiportd"

API_PORT=443
CLIENT_PORT=80
TUNNEL_PORT_RANGE="20000-30000"
TWO_FA="totp"            # totp | none — email-2FA needs an MTA we don't ship
FQDN=""
EMAIL=""
PUBLIC_FQDN=0
SKIP_NAT=0
CLIENT_URL=""
CERT_FILE=""
KEY_FILE=""
USE_ACME=0

#======================================================================
# Logging
#======================================================================

if [ -t 1 ] && command -v tput >/dev/null 2>&1; then
    BOLD=$(tput bold); RESET=$(tput sgr0)
    GREEN=$(tput setaf 2); YELLOW=$(tput setaf 3); RED=$(tput setaf 1)
else
    BOLD=""; RESET=""; GREEN=""; YELLOW=""; RED=""
fi

throw_info()    { echo "${GREEN}[ok]${RESET}    $*"; }
throw_warn()    { echo "${YELLOW}[warn]${RESET}  $*"; }
throw_error()   { echo "${RED}[error]${RESET} $*" >&2; }
throw_fatal()   { throw_error "$*"; exit 1; }
throw_debug()   { [ -n "$PROXIPORT_DEBUG" ] && echo "[debug] $*"; return 0; }

is_available()  { command -v "$1" >/dev/null 2>&1; }
is_terminal()   { [ -t 0 ]; }

confirm() {
    local prompt="${1:-Proceed?}"
    while true; do
        printf "%s (y/n) " "$prompt"
        read -r ans
        case "$ans" in
            [Yy]*) return 0 ;;
            [Nn]*) return 1 ;;
        esac
    done
}

#======================================================================
# Help / usage
#======================================================================

print_help() {
    cat <<'EOF'
proxiport-setup — finish the proxiportd .deb / .rpm install.

Usage:
  proxiport-setup [flags]

Flags:
  -d, --fqdn FQDN          public hostname (required, no default)
  -e, --email EMAIL        operator email (used for TOTP account label only;
                           not registered with Let's Encrypt)
  -a, --api-port PORT      port the TLS API/SPA binds       (default 443)
  -c, --client-port PORT   port the agent (chisel) listener (default 80)
  -p, --port-range RANGE   tunnel port range  (default 20000-30000)
  -i, --client-url URL     override the URL agents are told to dial
  -o, --totp               TOTP-2FA on (default)
  -n, --no-2fa             disable 2FA (not recommended)
      --cert-file PATH     manual cert PEM; turns off built-in ACME
      --key-file PATH      matching key PEM
  -s, --skip-nat           skip the public-IP / NAT sanity check
  -u, --uninstall          remove proxiportd and all state
  -h, --help               this message
  -v, --version            print version

If --fqdn is omitted and stdin is a terminal, you'll be prompted.
EOF
}

#======================================================================
# Uninstall
#======================================================================

do_uninstall() {
    if ! confirm "This removes proxiportd, its config, its state, and the proxiport user. Continue?"; then
        throw_info "Aborted."
        exit 0
    fi

    throw_info "Stopping proxiportd ..."
    systemctl disable --now proxiportd >/dev/null 2>&1 || true
    pkill -9 proxiportd >/dev/null 2>&1 || true

    if is_available dpkg && dpkg -l proxiportd >/dev/null 2>&1; then
        throw_info "Purging proxiportd .deb ..."
        DEBIAN_FRONTEND=noninteractive apt-get -y purge proxiportd >/dev/null 2>&1 \
            || dpkg --purge proxiportd
    elif is_available rpm && rpm -q proxiportd >/dev/null 2>&1; then
        throw_info "Removing proxiportd .rpm ..."
        if is_available dnf; then dnf -y remove proxiportd
        elif is_available yum; then yum -y remove proxiportd
        else rpm -e proxiportd; fi
    fi

    for d in /etc/proxiport /var/lib/proxiport /var/log/proxiport; do
        if [ -e "$d" ]; then rm -rf "$d"; throw_debug "removed $d"; fi
    done

    if id proxiport >/dev/null 2>&1; then
        if is_available deluser; then deluser proxiport >/dev/null 2>&1 || true
        elif is_available userdel; then userdel -r -f proxiport >/dev/null 2>&1 || true
        fi
        if is_available groupdel; then groupdel -f proxiport >/dev/null 2>&1 || true; fi
    fi

    throw_info "Uninstalled."
}

#======================================================================
# Argument parsing
#======================================================================

# getopt is GNU on Debian / RHEL — features we use are portable across
# both. Long-only flags use the --long form.
PARSED=$(getopt \
    -o vhd:e:a:c:p:i:onsu \
    --long version,help,fqdn:,email:,api-port:,client-port:,port-range:,client-url:,totp,no-2fa,cert-file:,key-file:,skip-nat,uninstall \
    -n proxiport-setup -- "$@") || { print_help; exit 2; }
eval set -- "$PARSED"
while true; do
    case "$1" in
        -h|--help)          print_help; exit 0 ;;
        -v|--version)       echo "proxiport-setup 0.1.4"; exit 0 ;;
        -d|--fqdn)          FQDN="$2"; shift 2 ;;
        -e|--email)         EMAIL="$2"; shift 2 ;;
        -a|--api-port)      API_PORT="$2"; shift 2 ;;
        -c|--client-port)   CLIENT_PORT="$2"; shift 2 ;;
        -p|--port-range)    TUNNEL_PORT_RANGE="$2"; shift 2 ;;
        -i|--client-url)    CLIENT_URL="$2"; shift 2 ;;
        -o|--totp)          TWO_FA="totp"; shift ;;
        -n|--no-2fa)        TWO_FA="none"; shift ;;
        --cert-file)        CERT_FILE="$2"; shift 2 ;;
        --key-file)         KEY_FILE="$2"; shift 2 ;;
        -s|--skip-nat)      SKIP_NAT=1; shift ;;
        -u|--uninstall)     do_uninstall; exit 0 ;;
        --) shift; break ;;
        *) throw_fatal "argument parsing error" ;;
    esac
done

#======================================================================
# Prereq checks
#======================================================================

if [ "$(id -u)" -ne 0 ]; then
    throw_fatal "Run as root (sudo proxiport-setup ...)."
fi

if [ ! -x "$BINARY" ]; then
    throw_fatal "$BINARY not found. Install the proxiportd .deb / .rpm first."
fi

if [ ! -f "$CONFIG_FILE" ]; then
    if [ -f "$EXAMPLE_FILE" ]; then
        throw_warn "$CONFIG_FILE missing; seeding from $EXAMPLE_FILE."
        cp "$EXAMPLE_FILE" "$CONFIG_FILE"
        chmod 0640 "$CONFIG_FILE"
        chown root:proxiport "$CONFIG_FILE"
    else
        throw_fatal "Neither $CONFIG_FILE nor $EXAMPLE_FILE exists."
    fi
fi

if [ -f "$DB_FILE" ]; then
    if ! confirm "$DB_FILE already exists — re-running will overwrite it (admin user will be reset). Continue?"; then
        throw_info "Aborted."
        exit 0
    fi
fi

if { [ -n "$CERT_FILE" ] && [ -z "$KEY_FILE" ]; } \
   || { [ -z "$CERT_FILE" ] && [ -n "$KEY_FILE" ]; }; then
    throw_fatal "--cert-file and --key-file must be supplied together."
fi

#======================================================================
# Dependencies
#======================================================================

ensure_deps() {
    local need_install=()
    is_available htpasswd      || need_install+=("htpasswd")
    is_available sqlite3       || need_install+=("sqlite3")
    is_available openssl       || need_install+=("openssl")
    is_available dig           || need_install+=("dig")
    is_available setcap        || need_install+=("setcap")
    [ ${#need_install[@]} -eq 0 ] && return 0

    throw_info "Installing missing dependencies: ${need_install[*]}"
    if is_available apt-get; then
        local pkgs=()
        for tool in "${need_install[@]}"; do
            case "$tool" in
                htpasswd) pkgs+=(apache2-utils) ;;
                sqlite3)  pkgs+=(sqlite3) ;;
                openssl)  pkgs+=(openssl) ;;
                dig)      pkgs+=(dnsutils) ;;
                setcap)   pkgs+=(libcap2-bin) ;;
            esac
        done
        DEBIAN_FRONTEND=noninteractive apt-get -y update >/dev/null
        DEBIAN_FRONTEND=noninteractive apt-get -y --no-install-recommends install "${pkgs[@]}"
    elif is_available dnf || is_available yum; then
        local pm; pm=$(is_available dnf && echo dnf || echo yum)
        local pkgs=()
        for tool in "${need_install[@]}"; do
            case "$tool" in
                htpasswd) pkgs+=(httpd-tools) ;;
                sqlite3)  pkgs+=(sqlite) ;;
                openssl)  pkgs+=(openssl) ;;
                dig)      pkgs+=(bind-utils) ;;
                setcap)   pkgs+=(libcap) ;;
            esac
        done
        "$pm" -y install "${pkgs[@]}"
    else
        throw_fatal "Install these manually: ${need_install[*]}"
    fi
}

ensure_deps

#======================================================================
# FQDN handling
#======================================================================

ask_for_fqdn() {
    if ! is_terminal; then
        throw_fatal "--fqdn is required when stdin is not a terminal."
    fi
    while [ -z "$FQDN" ]; do
        printf "Public hostname for this server (FQDN): "
        read -r FQDN
        if [ -n "$FQDN" ] && confirm "Use \"$FQDN\"?"; then
            break
        fi
        FQDN=""
    done
}

# Resolves the FQDN's public A record. Prefers an external resolver so a
# local /etc/hosts entry can't shadow the real record: cloud images map the
# box's own hostname to 127.0.1.1, and dig against the system stub resolver
# (systemd-resolved) honours /etc/hosts, so a correct public DNS record would
# otherwise look like it points at loopback. Falls back to the system resolver
# only if no public resolver answers. The anchored grep keeps pure-IPv4 lines,
# dropping any CNAME target dig +short prints ahead of the address.
resolve_public_a() {
    local fqdn="$1" resolver ip
    for resolver in @1.1.1.1 @8.8.8.8 ''; do
        ip=$(dig +short +time=3 +tries=1 $resolver "$fqdn" A 2>/dev/null \
                 | grep -Eo '^[0-9]+(\.[0-9]+){3}$' | head -n1)
        [ -n "$ip" ] && { printf '%s\n' "$ip"; return 0; }
    done
    return 1
}

# Resolves to true (0) if FQDN A points at one of *this* host's routable IPs.
fqdn_points_here() {
    local fqdn_ip my_ip
    fqdn_ip=$(resolve_public_a "$1") || return 1
    for my_ip in $(ip -4 -o addr show scope global | awk '{print $4}' | cut -d/ -f1) \
                  $(curl -fsS --max-time 4 https://api.ipify.org 2>/dev/null); do
        [ "$fqdn_ip" = "$my_ip" ] && return 0
    done
    return 1
}

[ -z "$FQDN" ] && ask_for_fqdn

if [ "$SKIP_NAT" -eq 0 ]; then
    if fqdn_points_here "$FQDN"; then
        PUBLIC_FQDN=1
        throw_info "$FQDN resolves to this host."
    else
        throw_warn "$FQDN does not resolve to one of this host's public IPs."
        throw_warn "Built-in ACME will not work until DNS is correct."
        throw_warn "Re-run with --skip-nat to proceed anyway, or fix DNS and retry."
        if ! confirm "Continue anyway?"; then exit 1; fi
    fi
else
    PUBLIC_FQDN=1
fi

[ -z "$CLIENT_URL" ] && CLIENT_URL="http://${FQDN}:${CLIENT_PORT}"

# Pick the TLS path
if [ -n "$CERT_FILE" ]; then
    USE_ACME=0
    [ -r "$CERT_FILE" ] || throw_fatal "Can't read --cert-file $CERT_FILE"
    [ -r "$KEY_FILE"  ] || throw_fatal "Can't read --key-file  $KEY_FILE"
elif [ "$API_PORT" -eq 443 ] && [ "$PUBLIC_FQDN" -eq 1 ]; then
    USE_ACME=1
else
    throw_fatal "API port is not 443 or FQDN is not public; supply --cert-file/--key-file."
fi

#======================================================================
# Stop the service before editing config + DB
#======================================================================

systemctl stop proxiportd >/dev/null 2>&1 || true

#======================================================================
# Firewall
#======================================================================

if is_available ufw && ufw status 2>/dev/null | grep -q "Status: active"; then
    throw_info "Adding UFW rules"
    ufw allow "${API_PORT}/tcp"    >/dev/null
    ufw allow "${CLIENT_PORT}/tcp" >/dev/null
    ufw allow "$(echo "$TUNNEL_PORT_RANGE" | tr - :)/tcp" >/dev/null
fi
if is_available firewall-cmd && firewall-cmd --state >/dev/null 2>&1; then
    throw_info "Adding firewalld rules"
    firewall-cmd --permanent --add-port="${API_PORT}/tcp"    >/dev/null
    firewall-cmd --permanent --add-port="${CLIENT_PORT}/tcp" >/dev/null
    firewall-cmd --permanent --add-port="${TUNNEL_PORT_RANGE}/tcp" >/dev/null
    firewall-cmd --reload >/dev/null
fi

#======================================================================
# setcap (re-apply in case package was upgraded)
#======================================================================

setcap CAP_NET_BIND_SERVICE=+eip "$BINARY" 2>/dev/null || \
    throw_warn "setcap on $BINARY failed; binding ports < 1024 will need root."

#======================================================================
# Generate secrets (regen even if v0.1.3 postinstall already did,
# so an operator re-running setup gets a clean slate)
#======================================================================

KEY_SEED=$(openssl rand -hex 32)
JWT_SECRET=$(openssl rand -base64 24 | tr -d '=')
ADMIN_PASSWD=$(openssl rand -base64 16 | tr -d '/+=' | cut -c1-18)
CLIENT_PASSWD=$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-22)
ADMIN_HASH=$(htpasswd -nbB -C 10 admin "$ADMIN_PASSWD" | cut -d: -f2)

#======================================================================
# Edit proxiportd.conf
#
# Match openrport's sed-based mutation: switch on auth_*_table, switch
# off the single-string `auth = "..."` placeholders, set listener
# addresses + base_url + cert paths or enable_acme, set tunnel range.
#======================================================================

# helper: set `key = val` inside a specific TOML section, replacing the
# (possibly commented-out) existing line.
set_in_section() {
    local section="$1" key="$2" val="$3"
    val=${val//|/\\|}
    sed -i -E "/^\[${section}\]/,/^\[/ {
        s|^[[:space:]]*#?[[:space:]]*${key}[[:space:]]*=.*|  ${key} = ${val}|
    }" "$CONFIG_FILE"
}

# helper: uncomment a bare key inside a section (value already correct).
uncomment_in_section() {
    local section="$1" key="$2"
    sed -i -E "/^\[${section}\]/,/^\[/ {
        s|^[[:space:]]*#[[:space:]]*${key}[[:space:]]*=|  ${key} =|
    }" "$CONFIG_FILE"
}

# helper: comment an active key inside a section.
comment_in_section() {
    local section="$1" key="$2"
    sed -i -E "/^\[${section}\]/,/^\[/ {
        s|^([[:space:]]*)(${key}[[:space:]]*=[[:space:]]*\".*\")|\1# \2|
    }" "$CONFIG_FILE"
}

# [server] block
set_in_section "server" "address"            "\"0.0.0.0:${CLIENT_PORT}\""
set_in_section "server" "url"                "[\"${CLIENT_URL}\"]"
set_in_section "server" "key_seed"           "\"${KEY_SEED}\""
set_in_section "server" "used_ports"         "['${TUNNEL_PORT_RANGE}']"
set_in_section "server" "keep_lost_clients"  "\"168h\""
set_in_section "server" "data_storage_days"  "7"
comment_in_section   "server" "auth"
uncomment_in_section "server" "auth_table"

# Now the [api] block. We have to be careful — sed -E with a top-level
# address pattern would match both [server] address and [api] address.
# Restrict the [api] mutations to the section using a range.
sed -i -E "/^\[api\]/,/^\[/ {
    s|^[[:space:]]*#?[[:space:]]*address[[:space:]]*=.*|  address = \"0.0.0.0:${API_PORT}\"|
    s|^[[:space:]]*#?[[:space:]]*base_url[[:space:]]*=.*|  base_url = \"https://${FQDN}\"|
    s|^([[:space:]]*)auth[[:space:]]*=[[:space:]]*\".*\"|\1# auth =|
    s|^[[:space:]]*#[[:space:]]*auth_user_table[[:space:]]*=|  auth_user_table =|
    s|^[[:space:]]*#[[:space:]]*auth_group_table[[:space:]]*=|  auth_group_table =|
    s|^[[:space:]]*#[[:space:]]*auth_group_details_table[[:space:]]*=|  auth_group_details_table =|
    s|^[[:space:]]*#?[[:space:]]*jwt_secret[[:space:]]*=.*|  jwt_secret = \"${JWT_SECRET}\"|
    s|^[[:space:]]*#[[:space:]]*totp_account_name[[:space:]]*=.*|  totp_account_name = \"${FQDN}\"|
}" "$CONFIG_FILE"

if [ "$USE_ACME" -eq 1 ]; then
    sed -i -E "/^\[api\]/,/^\[/ {
        s|^[[:space:]]*#?[[:space:]]*enable_acme[[:space:]]*=.*|  enable_acme = true|
        s|^[[:space:]]*cert_file[[:space:]]*=|  # cert_file =|
        s|^[[:space:]]*key_file[[:space:]]*=|  # key_file =|
    }" "$CONFIG_FILE"
else
    sed -i -E "/^\[api\]/,/^\[/ {
        s|^[[:space:]]*#?[[:space:]]*cert_file[[:space:]]*=.*|  cert_file = \"${CERT_FILE}\"|
        s|^[[:space:]]*#?[[:space:]]*key_file[[:space:]]*=.*|  key_file = \"${KEY_FILE}\"|
        s|^[[:space:]]*#?[[:space:]]*enable_acme[[:space:]]*=.*|  enable_acme = false|
    }" "$CONFIG_FILE"
fi

# 2FA — only TOTP supported in v0.1.4 (email-2FA requires an MTA we
# don't ship). Email-delivery script wiring stays open for operators
# who want to bring their own.
if [ "$TWO_FA" = "totp" ]; then
    sed -i -E "/^\[api\]/,/^\[/ {
        s|^[[:space:]]*#?[[:space:]]*totp_enabled[[:space:]]*=.*|  totp_enabled = true|
        s|^[[:space:]]*#[[:space:]]*totp_login_session_ttl|  totp_login_session_ttl|
    }" "$CONFIG_FILE"
else
    sed -i -E "/^\[api\]/,/^\[/ {
        s|^[[:space:]]*#?[[:space:]]*totp_enabled[[:space:]]*=.*|  totp_enabled = false|
    }" "$CONFIG_FILE"
fi

# [database] uncomment sqlite + path
sed -i -E "/^\[database\]/,/^\[/ {
    s|^[[:space:]]*#?[[:space:]]*db_type[[:space:]]*=.*|  db_type = \"sqlite\"|
    s|^[[:space:]]*#?[[:space:]]*db_name[[:space:]]*=.*|  db_name = \"${DB_FILE}\"|
}" "$CONFIG_FILE"

#======================================================================
# Database
#======================================================================

throw_info "Creating user database at $DB_FILE"
test -e "$DB_FILE" && rm -f "$DB_FILE"
touch "$DB_FILE"
chown proxiport:proxiport "$DB_FILE"
chmod 0640 "$DB_FILE"

# Schema mirrors the upstream openrport schema verbatim — same tables,
# same indexes, same column types.
sqlite3 "$DB_FILE" <<EOF
CREATE TABLE "users" (
  "username"         TEXT(150) NOT NULL,
  "password"         TEXT(255) NOT NULL,
  "password_expired" BOOLEAN   NOT NULL CHECK (password_expired IN (0, 1)) DEFAULT 0,
  "token"            TEXT(36)  DEFAULT NULL,
  "two_fa_send_to"   TEXT(150) DEFAULT '',
  "totp_secret"      TEXT      DEFAULT ''
);
CREATE UNIQUE INDEX "main"."username" ON "users" ("username" ASC);

CREATE TABLE "groups" (
  "username" TEXT(150) NOT NULL,
  "group"    TEXT(150) NOT NULL
);
CREATE UNIQUE INDEX "main"."username_group" ON "groups" ("username" ASC, "group" ASC);

CREATE TABLE "group_details" (
  "name"        TEXT(150) NOT NULL,
  "permissions" TEXT      DEFAULT '{}'
);
CREATE UNIQUE INDEX "main"."name" ON "group_details" ("name" ASC);

INSERT INTO users  VALUES ('admin', '${ADMIN_HASH}', 0, NULL, '${EMAIL}', '');
INSERT INTO groups VALUES ('admin', 'Administrators');

CREATE TABLE "clients_auth" (
  "id"       varchar(100) PRIMARY KEY,
  "password" varchar(100) NOT NULL
);
INSERT INTO clients_auth VALUES ('client1', '${CLIENT_PASSWD}');
EOF

# Persist the operator-visible credentials. The .deb postinstall also
# writes initial-admin-password; overwrite with the new value.
umask 027
printf 'admin:%s\n'    "$ADMIN_PASSWD"   > /var/lib/proxiport/initial-admin-password
printf 'client1:%s\n'  "$CLIENT_PASSWD"  > /var/lib/proxiport/initial-client-auth
chown root:proxiport /var/lib/proxiport/initial-admin-password /var/lib/proxiport/initial-client-auth
chmod 0640           /var/lib/proxiport/initial-admin-password /var/lib/proxiport/initial-client-auth

#======================================================================
# Start the service
#======================================================================

systemctl daemon-reload
throw_info "Starting proxiportd ..."
systemctl enable --now proxiportd

sleep 3
if ! systemctl is-active --quiet proxiportd; then
    throw_error "proxiportd failed to start. Last 30 log lines:"
    journalctl -u proxiportd -n 30 --no-pager
    throw_fatal "Service not running."
fi

#======================================================================
# Summary
#======================================================================

if [ "$TWO_FA" = "totp" ]; then
    TWO_FA_MSG="On first login, the SPA will prompt you to enroll a TOTP authenticator."
else
    TWO_FA_MSG="Two-factor authentication is disabled. Enable it in proxiportd.conf and re-login to enroll."
fi

mkdir -p "$(dirname "$SUMMARY_FILE")"
umask 077
cat > "$SUMMARY_FILE" <<EOF
proxiportd setup $(date -Iseconds)

Admin URL:    https://${FQDN}:${API_PORT}
Admin user:   admin
Admin pass:   ${ADMIN_PASSWD}

Agent dial:   ${FQDN}:${CLIENT_PORT}
Agent auth:   client1:${CLIENT_PASSWD}

${TWO_FA_MSG}

Config:       ${CONFIG_FILE}
Database:     ${DB_FILE}
EOF

cat <<EOF

------------------------------------------------------------------------
${BOLD}proxiport is up.${RESET}

  Point a browser at  ${BOLD}https://${FQDN}:${API_PORT}${RESET}
  Sign in as          ${BOLD}admin${RESET} / ${BOLD}${ADMIN_PASSWD}${RESET}

  Agent dial address  ${FQDN}:${CLIENT_PORT}
  First agent auth    client1:${CLIENT_PASSWD}

  ${TWO_FA_MSG}

  Credentials and config paths also saved to ${SUMMARY_FILE}
------------------------------------------------------------------------
EOF
