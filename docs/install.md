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
- A public hostname (recommended) and a TLS-terminating reverse proxy
  (nginx, Caddy, or Traefik) in front of the server. ProxiPort itself
  serves plain HTTP/WebSocket on a configurable port; TLS lives at the
  reverse proxy.
- SQLite is the default datastore; MySQL is also supported.

### Install

ProxiPort is currently distributed as a **tarball** from the project's
[GitHub releases page](https://github.com/proximile/proxiport/releases).
Each release ships a static `proxiportd` binary, a sample
`proxiportd.conf`, and a systemd unit file, for `linux/amd64` and
`linux/arm64`.

```sh
# Replace VERSION and ARCH (x86_64 or aarch64) for your target.
curl -LO https://github.com/proximile/proxiport/releases/latest/download/proxiportd_Linux_x86_64.tar.gz
tar xzf proxiportd_Linux_x86_64.tar.gz
sudo install -m 0755 proxiportd /usr/local/bin/
sudo mkdir -p /etc/proxiport /var/lib/proxiport
sudo cp proxiportd.example.conf /etc/proxiport/proxiportd.conf
sudo cp proxiportd.service /etc/systemd/system/
```

We do not yet maintain distro-native packages (`.deb`, `.rpm`),
container images, or any other registry-published artifacts. If you need
one of those, build it from source — `go install
github.com/proximile/proxiport/cmd/proxiportd@latest` produces a working
binary against `main`.

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

Start and enable the unit:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now proxiportd
```

Put a reverse proxy in front. The TLS terminator must support WebSocket
upgrade on the client-listener port and forward `X-Forwarded-For`.

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
- Outbound TCP to the server (whatever your reverse proxy listens on,
  443 in most production setups)

### Install

Same shape as the server — tarball from the GitHub releases page,
extract, copy binary + config + service unit. Replace `proxiportd` with
`proxiport` in the URL above.

The fastest way to bring up a new agent is the **pairing service** at
[`pairing.proxiport.net`](https://pairing.proxiport.net/): the operator
posts the agent's credentials, the service mints a one-shot pairing
code, the agent host runs

```sh
curl https://pairing.proxiport.net/<code> | sudo sh
```

and the installer drops a working binary plus `proxiport.conf` into
place. Source for the pairing service: <https://github.com/proximile/proxiport-pairing>.

### Manual config

If you prefer to install the agent yourself, drop the tarball's
`proxiport` binary into `/usr/local/bin/` and write
`/etc/proxiport/proxiport.conf`:

```toml
[client]
server = "your-server.example.com:443"
auth = "<client-auth-id>:<password>"
fingerprint = "<server-host-key-fingerprint>"
```

The `fingerprint` value pins the server's host key so the agent refuses
to talk to an imposter. It's printed by `proxiportd` at startup.

Start the service:

```sh
sudo systemctl enable --now proxiport
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
2. Install the ProxiPort server tarball.
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
- **Tunnel transport defaults** — chisel over plain WebSocket; put a
  TLS-terminating reverse proxy in front for production.
- **Datastore** — SQLite by default, MySQL supported.
- **REST API surface** — the same endpoints, the same response shapes.
  See the API reference.
