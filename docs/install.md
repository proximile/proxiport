# Install

!!! tip "Just want to see it work?"
    The public demo at
    [`https://demo.proxiport.net/`](https://demo.proxiport.net/) lets
    you sign in (`demo` / `demo`) and explore an Inventory of three
    pre-registered agents without installing anything. State resets
    on the half-hour.

ProxiPort has two pieces: the **server** (`proxiportd`) and the **agent**
(`proxiport`). One server reaches many agents; the agent dials the server
over an outbound WebSocket, so it works behind NAT without inbound
firewall rules.

This page covers a single-server install on Linux and a single Linux
agent. macOS and Windows agents follow the same shape with the
platform-native service manager.

## Server

### Requirements

- Linux x86_64 or arm64 with systemd
- A public hostname is recommended so you can serve TLS. ProxiPort
  terminates TLS itself — point `[api] cert_file` + `key_file` at a
  PEM pair, or set `[api] enable_acme = true` to get a Let's Encrypt
  certificate from the embedded ACME client. A separate reverse proxy
  (nginx, Caddy, Traefik) is optional, useful when you already run one
  for other services. See [HTTPS](https.md) for all three setups.
- SQLite is the default datastore; MySQL is also supported.

### Install

Each release on the
[GitHub releases page](https://github.com/proximile/proxiport/releases)
ships the server for `linux/amd64` and `linux/arm64` in three formats:

- **Debian/Ubuntu `.deb`** — `proxiportd_<ver>_linux_<arch>.deb`
- **Fedora/RHEL/openSUSE `.rpm`** — `proxiportd_<ver>_linux_<arch>.rpm`
- **Tarball** — `proxiportd_<ver>_linux_<arch>.tar.gz`, for other
  distributions

Asset filenames carry the version; the snippets below resolve the
latest tag from the GitHub API before downloading. Substitute `arm64`
for `x86_64` on aarch64 hosts.

#### Debian / Ubuntu

```sh
VER=$(curl -fsS https://api.github.com/repos/proximile/proxiport/releases/latest \
        | grep -m1 '"tag_name"' | cut -d'"' -f4)
curl -LO "https://github.com/proximile/proxiport/releases/download/${VER}/proxiportd_${VER#v}_linux_x86_64.deb"
sudo dpkg -i "proxiportd_${VER#v}_linux_x86_64.deb"
sudo vi /etc/proxiport/proxiportd.conf
sudo systemctl enable --now proxiportd
```

The package creates the `proxiport` system user, installs the binary
at `/usr/bin/proxiportd`, ships the systemd unit at
`/lib/systemd/system/proxiportd.service`, and seeds
`/etc/proxiport/proxiportd.conf` from the example. State lives under
`/var/lib/proxiport`.

#### Fedora / RHEL / openSUSE

```sh
VER=$(curl -fsS https://api.github.com/repos/proximile/proxiport/releases/latest \
        | grep -m1 '"tag_name"' | cut -d'"' -f4)
sudo rpm -ivh "https://github.com/proximile/proxiport/releases/download/${VER}/proxiportd_${VER#v}_linux_x86_64.rpm"
sudo vi /etc/proxiport/proxiportd.conf
sudo systemctl enable --now proxiportd
```

Same layout as the `.deb` — `proxiport` user, `/usr/bin/proxiportd`,
unit at `/lib/systemd/system/proxiportd.service`, config at
`/etc/proxiport/proxiportd.conf`, state under `/var/lib/proxiport`.

#### Tarball (other distributions)

```sh
VER=$(curl -fsS https://api.github.com/repos/proximile/proxiport/releases/latest \
        | grep -m1 '"tag_name"' | cut -d'"' -f4)
curl -LO "https://github.com/proximile/proxiport/releases/download/${VER}/proxiportd_${VER#v}_linux_x86_64.tar.gz"
tar xzf "proxiportd_${VER#v}_linux_x86_64.tar.gz"

sudo install -m 0755 proxiportd /usr/bin/proxiportd
sudo install -d /etc/proxiport
sudo install -m 0644 proxiportd.example.conf /etc/proxiport/proxiportd.conf
sudo install -m 0644 proxiportd.service /lib/systemd/system/proxiportd.service

sudo useradd --system --home /var/lib/proxiport --shell /usr/sbin/nologin proxiport || true
sudo install -d -o proxiport -g proxiport -m 0750 /var/lib/proxiport

sudo systemctl daemon-reload
sudo vi /etc/proxiport/proxiportd.conf
sudo systemctl enable --now proxiportd
```

The systemd unit runs `proxiportd` as the unprivileged `proxiport`
user. `CAP_NET_BIND_SERVICE` is granted so the binary can bind ports
80 and 443 without root.

#### Building from source

If none of the published artefacts fit your platform — or you want a
container image, which we do not publish — build from source:

```sh
go install github.com/proximile/proxiport/cmd/proxiportd@latest
```

The server needs CGO (`CGO_ENABLED=1`) for the embedded SQLite. The
agent is pure Go.

### Configure

Edit `/etc/proxiport/proxiportd.conf`:

- `[api] auth = "admin:<password>"` — initial admin login (rotate
  before exposing the API).
- `[server] auth = "<client-auth-id>:<password>"` — credentials the agent
  uses to register.
- `[api] jwt_secret = "<long-random-string>"` — pin so users do not
  get logged out on every restart.
- `[server] key_seed = "<long-random-hex>"` — pin the server host key
  so the agent's `fingerprint` check stays stable across restarts.

Restart the unit so it picks up the new config (or `enable --now` it
if you skipped that step in the install block above):

```sh
sudo systemctl restart proxiportd
```

Enable TLS before exposing the server to anything other than
`127.0.0.1`. The fastest path is built-in ACME — set
`[api] enable_acme = true` and `[api] base_url = "https://<your-host>"`
in `proxiportd.conf` and restart. For manually-managed certificates,
point `[api] cert_file` and `[api] key_file` at a PEM pair. If you
prefer to front the server with an external reverse proxy (nginx,
Caddy, Traefik), see [HTTPS](https.md) — the same page covers the
WebSocket-upgrade and `X-Forwarded-For` settings the proxy needs.

Hit the SPA in a browser and log in with the admin credentials from
`proxiportd.conf`.

![ProxiPort login screen.](screenshots/00-login-screen.png)

Once the server is up, jump to the **Info** page — that's where the
host-key fingerprint and the list of Connect-URLs the agents should
use live.

![Server info — copy the fingerprint into each agent's
`proxiport.conf` to pin it.](screenshots/23-server-info-2fa-off.png)

## Agent

### Requirements

- Linux / macOS / Windows
- Outbound TCP to the server on whatever port the server's
  client-listener is published on — 443 in most production setups,
  whether that's served by `proxiportd` directly or by a reverse
  proxy in front of it.

### Install

The fastest way to bring up a new agent is the **pairing service** at
[`pairing.proxiport.net`](https://pairing.proxiport.net/): the operator
posts the agent's credentials, the service mints a one-shot pairing
code, the agent host runs

```sh
curl https://pairing.proxiport.net/<code> | sudo sh
```

and the installer drops a working binary plus `proxiport.conf` into
place. Source for the pairing service: <https://github.com/proximile/proxiport-pairing>.

The agent ships in the same three package formats as the server, plus
tarballs for a wider platform list: linux (amd64, arm64, i386, armv6,
armv7, mips/mipsle/mips64/mips64le hard- and softfloat, s390x), macOS
(amd64, arm64), Windows (amd64), and FreeBSD (amd64, arm64, armv6,
armv7, i386). `.deb` and `.rpm` are published for every Linux variant.

### Manual install — Debian / Ubuntu

```sh
VER=$(curl -fsS https://api.github.com/repos/proximile/proxiport/releases/latest \
        | grep -m1 '"tag_name"' | cut -d'"' -f4)
curl -LO "https://github.com/proximile/proxiport/releases/download/${VER}/proxiport_${VER#v}_linux_x86_64.deb"
sudo dpkg -i "proxiport_${VER#v}_linux_x86_64.deb"
sudo vi /etc/proxiport/proxiport.conf
sudo systemctl enable --now proxiport
```

### Manual install — Fedora / RHEL / openSUSE

```sh
VER=$(curl -fsS https://api.github.com/repos/proximile/proxiport/releases/latest \
        | grep -m1 '"tag_name"' | cut -d'"' -f4)
sudo rpm -ivh "https://github.com/proximile/proxiport/releases/download/${VER}/proxiport_${VER#v}_linux_x86_64.rpm"
sudo vi /etc/proxiport/proxiport.conf
sudo systemctl enable --now proxiport
```

### Manual install — tarball (other platforms)

```sh
VER=$(curl -fsS https://api.github.com/repos/proximile/proxiport/releases/latest \
        | grep -m1 '"tag_name"' | cut -d'"' -f4)
curl -LO "https://github.com/proximile/proxiport/releases/download/${VER}/proxiport_${VER#v}_linux_x86_64.tar.gz"
tar xzf "proxiport_${VER#v}_linux_x86_64.tar.gz"

sudo install -m 0755 proxiport /usr/bin/proxiport
sudo install -d /etc/proxiport
sudo install -m 0644 proxiport.example.conf /etc/proxiport/proxiport.conf
sudo install -m 0644 proxiport.service /lib/systemd/system/proxiport.service
sudo systemctl daemon-reload
```

#### Configure the agent

Edit `/etc/proxiport/proxiport.conf`:

```toml
[client]
server = "your-server.example.com:443"
auth = "<client-auth-id>:<password>"
fingerprint = "<server-host-key-fingerprint>"
```

The `fingerprint` value pins the server's host key so the agent
refuses to talk to an imposter. It is printed by `proxiportd` at
startup, and shown on the server **Info** page.

Start the service:

```sh
sudo systemctl enable --now proxiport     # or `restart` if it is already running
```

The agent connects, registers, and waits for tunnel-open requests from
the server. It reconnects automatically.

Refresh the SPA. The new agent shows up in the inventory.

![Inventory with the first agent online.](screenshots/01-inventory-dashboard.png)

## Migrating from rport or openrport

ProxiPort's config file format is intentionally compatible with the
upstream `rportd.conf` and `rport.conf`. Tags and option names are
unchanged where the underlying behaviour is unchanged.

To migrate:

1. Stop the upstream service (`rportd` or `openrportd`).
2. Install ProxiPort using the Debian/Ubuntu, Fedora/RHEL, or tarball
   path above.
3. Copy your existing `rportd.conf` to `/etc/proxiport/proxiportd.conf`.
4. Start `proxiportd`.
5. On each agent, replace the upstream binary, copy the config to
   `/etc/proxiport/proxiport.conf`, and start `proxiport`. Re-issue
   the `fingerprint` if the server's host key has changed.

The datastore schema is forwards-compatible from the openrport tree;
ProxiPort runs any pending migrations on first start.

## What does not change from upstream

- **Config file format** — TOML, structurally compatible with
  `rportd.conf` / `rport.conf`.
- **Tunnel transport defaults** — chisel over WebSocket. Serve it
  over TLS in production (built-in `cert_file`/`enable_acme` on the
  server, or an external reverse proxy — see [HTTPS](https.md)).
- **Datastore** — SQLite by default, MySQL supported.
- **REST API surface** — the same endpoints, the same response shapes.
  See the API reference.
