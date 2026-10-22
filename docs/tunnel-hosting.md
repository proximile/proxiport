# Tunnel hosting

A ProxiPort tunnel is a server-side listener that forwards traffic to
a TCP port on an agent. Everything in this document is about how
operators **publish** those listeners — on what port, on what
hostname, with what protocol on top — so users can reach them in a
browser or an SSH client.

ProxiPort can present a tunnel as:

- a **raw TCP port** on the server itself (the default, fine for SSH,
  databases, anything that already has its own client);
- a **subdomain on a wildcard hostname** routed by an embedded Caddy
  instance (HTTPS-on-443 access to internal web UIs, RDP-via-browser,
  VNC-via-browser);
- a **NoVNC-wrapped VNC tunnel** that renders a remote VNC server
  inside an HTML5 browser tab;
- an **RDP-via-browser tunnel** that proxies through an Apache
  Guacamole `guacd` daemon;
- a **target for inbound file pushes** that uses the same chisel
  channel to drop files onto the agent host.

This page covers all five. For tunnel ACLs, timeouts, and the basic
SPA flow, see [operator runbook — tunnels](operator-runbook.md#tunnels).

## Tunnels on random subdomains

The default tunnel scheme allocates a random port from
`[server] used_ports` (default `20000-30000`). That works fine until
you have to dial through a firewall that blocks outbound traffic to
non-default ports.

The subdomain hosting feature lets you publish each HTTP-based tunnel
on `https://<random>.tunnels.example.com:443` alongside the random
port. Same tunnel, two access paths. The browser uses 443, which gets
through most strict egress filters.

### Architecture

`proxiportd` launches a [Caddy](https://caddyserver.com/) process as a
child and stops it on shutdown. It does not restart Caddy itself: if
Caddy exits with an error, `proxiportd` exits too, and the packaged
systemd unit (`Restart=always`) restarts `proxiportd`, which launches
Caddy again. Caddy listens on the configured port (typically 443),
terminates TLS, and routes the random subdomain to the right
tunnel's internal port. All Caddy configuration is generated and
pushed by `proxiportd` over a Unix socket — no manual `Caddyfile`.

### Prerequisites

1. **Caddy installed locally.** Either via the package manager or as
   a static binary in `/usr/local/bin/caddy`. A Caddy in a separate
   container is not supported — `proxiportd` needs to spawn and
   manage the process directly.

   ```bash
   sudo curl -L "https://caddyserver.com/api/download?os=linux&arch=amd64" \
     -o /usr/local/bin/caddy
   sudo chmod +x /usr/local/bin/caddy
   sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/caddy
   ```

   `setcap` is what lets the unprivileged `proxiport` user start a
   listener on 443. Without it, Caddy will fail to bind.

   If Caddy is installed via the package manager, the unit usually
   auto-starts on boot. Disable it (`sudo systemctl disable caddy`)
   so the package's Caddy does not fight `proxiportd`'s child Caddy.

2. **Wildcard DNS record.** Create `*.tunnels.example.com` pointing
   at the server's public IP. If you would rather not use a subdomain
   prefix, a wildcard at the apex (`*.example.com`) works too.

3. **Wildcard TLS certificate.** A Let's Encrypt DNS-01 wildcard:

   ```bash
   sudo certbot certonly --manual --preferred-challenges dns \
     -d '*.tunnels.example.com'
   ```

   Make the cert readable by the `proxiport` user:

   ```bash
   sudo find /etc/letsencrypt/{archive,live} -type d -exec chmod o+rx {} \;
   sudo find /etc/letsencrypt/{archive,live} -type f -exec chmod o+r  {} \;
   ```

   Caddy always serves the wildcard certificate from the `cert_file` /
   `key_file` paths under `[caddy-integration]`. `proxiportd` checks at
   startup that both files exist and load. The
   [built-in ACME](https.md#built-in-acme) cannot issue a wildcard, so
   get one with a DNS-01 client as shown above and set up renewal for
   it. (`[server] tunnel_enable_acme` is a separate option. It gets a
   certificate for the single `tunnel_host` name, used by the tunnel
   proxy on the random tunnel ports, not by Caddy.)

### Server configuration

Two deployment shapes, "split port" and "shared port".

**Split port** keeps the API/SPA on one TCP port (say 3000) and lets
Caddy run on its own port (typically 443) just for tunnels.
[`proxiportd.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf):

```toml
[caddy-integration]
  caddy            = "/usr/local/bin/caddy"
  address          = "0.0.0.0:443"
  subdomain_prefix = "tunnels.proxiport.example.com"
  cert_file        = "/etc/letsencrypt/live/tunnels.proxiport.example.com/fullchain.pem"
  key_file         = "/etc/letsencrypt/live/tunnels.proxiport.example.com/privkey.pem"
```

The `subdomain_prefix` is the bare domain — **do not include the
leading `*.`**.

The `[caddy-integration] address` port must be different from
`[api] address`.

**Shared port** runs the API and the tunnel subdomains on one TCP
port (443). Caddy fronts everything; the API runs as a plain-HTTP
backend behind it:

```toml
[api]
  address = "127.0.0.1:3000"
  # cert_file and key_file MUST be commented out — Caddy terminates TLS.

[caddy-integration]
  caddy            = "/usr/local/bin/caddy"
  address          = "0.0.0.0:443"
  subdomain_prefix = "tunnels.proxiport.example.com"
  cert_file        = "/etc/letsencrypt/live/tunnels.proxiport.example.com/fullchain.pem"
  key_file         = "/etc/letsencrypt/live/tunnels.proxiport.example.com/privkey.pem"

  api_hostname = "proxiport-api.example.com"
  api_cert_file = "/etc/letsencrypt/live/proxiport-api.example.com/fullchain.pem"
  api_key_file  = "/etc/letsencrypt/live/proxiport-api.example.com/privkey.pem"
  api_port = "3000"
```

`api_port` must equal the port in `[api] address`. Caddy will SNI-route
requests for `api_hostname` to the API backend; everything else under
the wildcard goes to a tunnel.

### Using it

When creating a tunnel from the SPA's per-client **Tunnels** tab,
choose an **HTTP** or **HTTPS** service (directly, or as the target of
a forwarding tunnel). Then tick *Terminate TLS on the server using the
server's certificate*. This sends `http_proxy=true` with the create
request. With `[caddy-integration]` enabled, the server allocates a
random subdomain under `subdomain_prefix` and returns the resulting URL
in the tunnel's `tunnel_url` field.

For now, the SPA's Active tunnels table shows only the random-port
address (`<server>:<port>`, with a **Copy** button). To get the
subdomain URL, read `tunnel_url` from the create response or from the
client's tunnel list in the API (`GET /api/v1/clients/{id}`).

To inspect the live Caddy routes:

```bash
curl --unix-socket /var/lib/proxiport/caddy-admin.sock \
  -H "host:unix" \
  http://localhost/config/apps/http/servers/srv0/routes | jq
```

### HTTPS targets and certificate verification

When the reverse proxy fronts an **HTTPS** target it terminates the
browser's TLS and opens a second TLS connection to the target. That
second connection's certificate **is verified** — against the system
trust store, or against a private CA bundle if you set one:

```toml
[server]
  # PEM bundle of CA certificate(s) to trust for tunnel targets.
  # Empty = use the system trust store.
  tunnel_proxy_target_ca_file = "/var/lib/proxiport/target-ca.pem"
```

Verification uses the target's real hostname for SNI: the tunnel's
`host_header` when you set one, otherwise the tunnel's remote host. If a
particular target presents a self-signed certificate you cannot add to a
CA bundle — or one whose SAN does not match the hostname, such as a LAN
appliance whose certificate carries no SAN at all — opt that one tunnel
out with the `skip_tls_verify=true` query parameter when creating it. It
applies only to that tunnel. In the web UI the same option is a **"Do not
verify the target's TLS certificate"** checkbox that appears under
*Terminate TLS on the server* once the target scheme is `https`.

A private CA bundle does not help a certificate that has no SAN: modern
TLS verification ignores the legacy Common Name and matches only against
the SAN, so a SAN-less certificate is rejected however it is trusted.
Either reissue the target's certificate with the hostname in its SAN, or
use `skip_tls_verify` for that tunnel.

If you would rather the operator's own browser validate the target
certificate end to end, use a plain (non-proxied) tunnel instead: the
bytes pass through untouched and your client completes the real TLS
handshake with the target.

### Protecting a tunnel with HTTP basic auth

A tunnel served through the reverse proxy (`http_proxy=true`) can require
HTTP basic auth. Send the credential in a **JSON request body**:

```bash
curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"auth_user":"operator","auth_password":"<password>"}' \
  "https://proxiport.example.com/api/v1/clients/<id>/tunnels?remote=0.0.0.0:8080&scheme=https&http_proxy=1&acl=203.0.113.0/24"
```

`auth_user` and `auth_password` are still accepted as query parameters for
compatibility, but a credential in a URL is recorded by every access log,
proxy cache and browser history between the caller and the API. The body
form takes precedence when both are supplied.

The password is stored so the proxy can check incoming requests, but it is
redacted from the client API payload and from the audit log.

### Plaintext HTTP tunnels are blocked by default

A tunnel whose scheme is `http` with **no** TLS in front of it carries the
operator's browser ⇆ server traffic in the clear. Anyone able to observe the
network path between the ProxiPort server and the browser — other hosts on the
LAN, an upstream router, a hostile Wi-Fi access point, a transit provider — can
read the request URLs, headers, session cookies and page contents. Because a
management UI is exactly the kind of traffic worth protecting, ProxiPort
**refuses to open such a tunnel by default**.

A tunnel is considered safe, and needs no override, when either:

- the **server terminates TLS** with its own certificate — tick *Terminate TLS
  on the server* in the SPA, or pass `http_proxy=true` (see the reverse proxy
  above); or
- **TLS is carried end to end by the target** — use the `https` scheme, and the
  encrypted bytes pass through the tunnel untouched.

If you genuinely need plaintext `http` — for instance on a trusted, isolated
private network — you must opt in explicitly:

- **In the SPA:** tick *Allow insecure `http://` without TLS* on the New Tunnel
  form. The box appears only for an `http` tunnel with server TLS disabled, and
  the tunnel cannot be created until it is ticked.
- **Via the API:** add `allow_insecure_http=true` to the
  `PUT /clients/{client_id}/tunnels` query. Without it the request is rejected
  with `400` and a message explaining the exposure.

This is the same deliberate, one-tick opt-in the UI already requires before
opening a tunnel to an unrestricted IP range — an easy-to-hit safe default, with
a clear escape hatch when you know what you are doing.

## NoVNC proxy

For agents on hosts that expose a VNC server, ProxiPort can bridge to
[noVNC](https://github.com/novnc/noVNC) so the operator opens a
browser tab and sees the remote desktop without installing a VNC
client.

### Prerequisites

- noVNC JavaScript bundle on the server's filesystem.
- TLS enabled on the tunnel proxy (`tunnel_proxy_cert_file` and
  `tunnel_proxy_key_file`). NoVNC depends on the same TLS-fronted
  reverse proxy that the subdomain feature uses.

Install noVNC:

```bash
sudo curl -L \
  https://github.com/novnc/noVNC/archive/refs/tags/v1.3.0.zip \
  -o /tmp/novnc.zip
sudo unzip -d /var/lib/proxiport /tmp/novnc.zip
sudo rm -f /tmp/novnc.zip
```

Point ProxiPort at the extracted directory:

```toml
[server]
  novnc_root = "/var/lib/proxiport/noVNC-1.3.0"
  tunnel_proxy_cert_file = "/var/lib/proxiport/server.crt"
  tunnel_proxy_key_file  = "/var/lib/proxiport/server.key"
```

Restart `proxiportd`. You create a noVNC tunnel through the REST API.
The web UI's tunnel form has no noVNC option, and it shows the
TLS-proxy checkbox only for HTTP and HTTPS tunnels. Ask for the VNC
scheme with the internal proxy enabled:

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  "https://proxiport.example.com/api/v1/clients/<client-id>/tunnels?remote=127.0.0.1:5900&scheme=vnc&http_proxy=true&acl=203.0.113.7"
```

The response gives the tunnel's proxy URL. Open it in a browser to get
the noVNC console. If `novnc_root` is unset, the noVNC app files are
answered with 404, so the console does not load, and the server logs
`novnc_root is not configured`.

## RDP proxy via guacd

For RDP access from a browser, ProxiPort proxies the chisel tunnel
through an Apache Guacamole `guacd` daemon on the same host:

```toml
[server]
  guacd_address = "127.0.0.1:4822"
  tunnel_proxy_cert_file = "/var/lib/proxiport/server.crt"
  tunnel_proxy_key_file  = "/var/lib/proxiport/server.key"
```

You do not need a full Guacamole web stack — only `guacd`. Run it in
a container:

```bash
docker run -d --name guacd --net=host --restart unless-stopped \
  lscr.io/linuxserver/guacd
```

`--net=host` is required so `guacd` can reach the tunnel listener on
`127.0.0.1`.

`proxiportd` test-connects to `guacd_address` when it starts and
refuses to start (`try guacd connection: ...`) if nothing is listening
there. Start guacd first, then (re)start `proxiportd`, and if both
start at boot, make sure guacd comes up before `proxiportd`.

Once `guacd` is running, an RDP tunnel created with the tunnel proxy
enabled (`scheme=rdp&http_proxy=true`) exposes a URL like
`https://proxiport.example.com:<port>/` that loads the Guacamole
client in the browser. The web UI's tunnel form does not offer this
option yet (its TLS-termination checkbox appears only for HTTP and
HTTPS services), so create the tunnel through the API:

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  "https://proxiport.example.com/api/v1/clients/<client-id>/tunnels?scheme=rdp&remote=3389&http_proxy=true&acl=203.0.113.7"
```

The server rejects `http_proxy=true` ("creation of tunnel proxy not
enabled") unless the tunnel proxy is enabled, either with
`tunnel_proxy_cert_file` and `tunnel_proxy_key_file` or with
`tunnel_enable_acme`.

You can pass through Guacamole session parameters as query strings:

```
/?username=Administrator&width=1280&height=800&security=nla&keyboard=en-us-qwerty
```

The landing page does not accept a `password` query parameter; the
operator types the password into the login form, which posts it to
`/createToken`. Available `keyboard` values
are the standard Guacamole layout codes (e.g. `en-us-qwerty`,
`de-de-qwertz`, `fr-fr-azerty`).

## File reception

A separate but related capability: pushing a file from the server's
disk to one or many agent hosts over the same chisel channel that
carries tunnel traffic.

### Flow

1. The operator uploads the file to the server via the multipart
   `POST /api/v1/files` endpoint, specifying target client IDs or
   group IDs, the destination path, and optional mode/owner/group.
2. The server saves the file to
   `{data_dir}/filepush/<uuid>_proxiport_filepush`.
3. Each addressed agent opens an SFTP-over-SSH session inside its
   existing chisel channel, downloads the file to its own
   `{data_dir}/filepush/<uuid>_proxiport_filepush` (same file name as
   on the server), verifies the MD5, applies any
   chmod/chown, and moves the file to the destination.
4. The server tracks per-agent success/failure and surfaces it on the
   API and on a WebSocket subscriber stream.

A minimal upload:

```bash
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -F 'upload=@./hosts.allow' \
  -F 'client_id=alpha-prod' \
  -F 'client_id=bravo-staging' \
  -F 'dest=/etc/hosts.allow' \
  -F 'force=true' \
  -F 'mode=0644' \
  https://proxiport.example.com/api/v1/files
```

Useful form fields:

| Field | Notes |
| --- | --- |
| `upload` (required) | The file body. |
| `client_id` / `group_id` | Repeatable. Picks the target agents directly or by group. Give these or `tags`, not both. |
| `tags` | Optional alternative to `client_id`/`group_id`: one JSON value such as `{"tags":["linux","web"],"operator":"AND"}`. It cannot be combined with them, and the tag list must not be empty. |
| `id` | Optional upload ID. A UUID is generated when it is omitted. It names the staged `<id>_proxiport_filepush` file and is reported as `uuid`. |
| `dest` (required) | Absolute destination path on the agent. Relative paths are rejected — see below. |
| `force` | Overwrite if the file already exists. |
| `sync` | Compare MD5; overwrite only if changed. Apply mode/owner if requested. |
| `mode` | Unix mode bits, e.g. `0644`. Without it the agent creates the file with `0764` less its umask (`0744` under the usual `022`). Directories the agent has to create for the destination get `0755`. |
| `user`, `group` | Unix owner / group, by name or numeric id. An agent running as root sets them directly; any other agent needs passwordless `sudo` for `chown`. If the mode or owner cannot be applied, the push fails and the destination is left as it was. |

### File-size limit

Server side, `[api] max_filepush_size` caps the single-file size in
bytes (default 10 MiB). Increase it under `[api]` if you need to push
larger payloads. The general `max_request_bytes` cap does **not**
apply to this endpoint.

### The destination must be absolute

`dest` is required to be an absolute path: `/etc/motd` on a Unix agent,
`C:\ProgramData\app\config.ini` or `\\fileserver\share\file.txt` on Windows. A
relative path such as `etc/motd` is refused by the server and, independently, by
the agent.

This matters for more than tidiness. Every entry in the server's refusal list and
in `[file-reception] protected` is written as an absolute path, so a relative
destination matches none of them — while the agent still resolves it, against the
agent process's working directory. Under the shipped systemd unit that directory
is `/`, so a relative `etc/cron.d/job` would have arrived at `/etc/cron.d/job`
with every protection bypassed. Requiring an absolute path is what makes the
lists below mean what they say.

A Windows path that is rooted but has no drive (`\Windows\...`) is also refused:
it resolves against whichever drive is current, which is the same surprise in a
different form.

### Protected destinations

`[file-reception] protected` is the agent's own refusal list, enforced on the
agent because it is the agent's filesystem at stake. By default the agent
refuses to write anywhere a pushed file could become code that runs as root:

- executable search paths and pseudo-filesystems (`/bin/**`, `/sbin/**`,
  `/usr/bin/**`, `/usr/sbin/**`, `/usr/local/bin/**`, `/usr/local/sbin/**`,
  `/lib*/**`, `/usr/lib*/**`, `/boot/**`, `/dev/**`, `/run/**`, `/proc/**`,
  `/sys/**`)
- anything that runs on a schedule (`/etc/crontab`, `/etc/cron.d/**`,
  `/etc/cron.{hourly,daily,weekly,monthly}/**`, `/var/spool/cron/**`)
- anything that runs at boot or defines a service (the systemd unit
  directories, `/etc/init.d/**`, `/etc/init/**`, `/etc/rc.local`,
  `/etc/rc*.d/**`, and on macOS the launchd directories and
  `/etc/periodic/**`)
- shell and loader initialisation (`/etc/profile`, `/etc/profile.d/**`,
  `/etc/bash.bashrc`, `/etc/bashrc`, `/etc/environment`, `/etc/ld.so.*`)
- accounts and authorization (`/etc/passwd`, `/etc/shadow`, `/etc/group`,
  `/etc/gshadow`, `/etc/sudoers`, `/etc/sudoers.d/**`, `/etc/pam.d/**`,
  `/etc/security/**`, `/etc/ssh/**`)
- SSH keys — `/root/**`, `/home/*/.ssh/**`, `/Users/*/.ssh/**` — since an
  `authorized_keys` file is a login
- package-manager hook directories (apt, yum, dnf, apk, pacman), which run as
  root on the next update, and network and DHCP hook directories
- the agent's own configuration (`/etc/proxiport/**`)

On Windows it covers `C:\Windows`, `C:\ProgramData`, both `Program Files`
trees and the per-user Startup folder. The full list is in the agent source
(`client/upload_nix.go`, `client/upload_win.go`).

A pattern ending in `/**` protects that directory and everything below it. Any
other pattern is a shell-style glob that matches a single path element, tested
against both the target file path and its directory, so `/etc/systemd/*`
covers `/etc/systemd/system` but not
`/etc/systemd/system/foo.service.d/override.conf` — use the `/**` form for a
directory tree. A `**` anywhere else in a pattern does not cross path
separators.

Setting `protected` *replaces* the default rather than adding to it, so an
override must carry the built-in entries you still want:

```toml
[file-reception]
  enabled = true
  protected = ['/bin/**', '/sbin/**', '/usr/bin/**', '/usr/sbin/**', '/etc/systemd/**', '/srv/deploy/**']
```

To disable reception entirely, set `enabled = false`. The server refuses an
obviously dangerous destination too, before transferring the file, but that
check is a convenience — the agent's list is the enforcement.

!!! warning "File reception crosses the command permission boundary"

    A push carries a mode and an owner, and the agent will `chown` as root, so
    an operator who can write to one of these locations has code execution on
    the host without needing the `commands` or `scripts` permission. Treat the
    `uploads` permission as equivalent to command execution unless you have
    narrowed `protected` to an allowed directory of your own.

### Privileged writes

The agent runs as an unprivileged user. To let it chown a pushed file
or write into a root-owned directory, add narrow sudoers rules:

```text
# /etc/sudoers.d/proxiport-filereception
proxiport-agent ALL=NOPASSWD: /usr/bin/chown * /var/lib/proxiport-agent/filepush/*_proxiport_filepush
proxiport-agent ALL=NOPASSWD: /usr/bin/mv     /var/lib/proxiport-agent/filepush/*_proxiport_filepush *
```

The rules name the agent's own account (`proxiport-agent` since 0.10.0;
`proxiport` is the server's) and the agent's data directory. A rule that
still names `proxiport` grants the server, not the agent.

Limit the wildcards as much as your operational shape allows.

A failed `chown` or `chmod` does not fail the push. If the sudoers rule
is missing or does not match, the agent still moves the file into
place, owned by the agent's own account with the agent's mode, and
reports `status: "success"`. The failure appears only as a
`chown of ... failed` or `chmod failed` note in the result's `message`
(on `WS /ws/uploads` and in the audit log). Only a failed copy or move
fails the push. When ownership matters, check `message`, not only
`status`.

### Live progress

`WS /ws/uploads` streams per-file results as they happen. The socket
does not accept a Bearer token. First call `GET /api/v1/ws-ticket` with
your normal Bearer token, then open
`/api/v1/ws/uploads?ticket=<data.ticket>` within 30 seconds. Each
ticket works only once. Clients that can set request headers can use
HTTP basic auth on the upgrade instead. Otherwise the upgrade is
answered `401 ticket required`. Subscribers get JSON frames like:

```json
{
  "client_id": "alpha-prod",
  "uuid": "482ae29e-d372-4d21-8cb4-58d75482b7e1",
  "filepath": "/etc/hosts.allow",
  "size": 17118,
  "message": "file successfully copied to destination /etc/hosts.allow",
  "status": "success"
}
```

If setting the mode or owner fails but the file is still placed, a
`chmod failed: ...` or `chown of ... failed: ...` note comes first in
the message, joined with a period.

The test UI at `/api/v1/test/uploads/ui` (enable with
`enable_ws_test_endpoints = true` under `[api]`) is the easiest way
to see this in action.

For long-term auditing, the result is also written to `auditlog.db`
and surfaces on the **Audit** page.

## Troubleshooting

- **Caddy fails to bind 443.** Re-run
  `sudo setcap 'cap_net_bind_service=+ep' $(which caddy)` after every
  Caddy upgrade. The capability is on the binary and is lost when the
  binary is replaced.
- **Tunnel URL returns a Caddy 404.** Caddy has no route for that
  subdomain. Either the tunnel was not created as a proxied tunnel
  (`http_proxy=true`, so it has no `tunnel_url`), or Caddy lost its
  routes, for example after a restart. List the live routes with the
  admin-socket `curl` shown above, and check
  `journalctl -u proxiportd -n 100 --no-pager` for
  `failed to create downstream caddy proxy` or other caddy errors.
  Caddy serves the static wildcard certificate from
  `[caddy-integration] cert_file`/`key_file`; no ACME runs for tunnel
  subdomains.
- **NoVNC loads but the canvas is blank.** The agent could not reach
  the VNC port on the target host. The tunnel itself is fine; the
  agent-side TCP connect is failing.
- **File push reports "permission denied" on rename.** The agent
  user can't write to `dest`. Either change the destination or add a
  sudoers rule and retry with the correct owner.

See also: [HTTPS](https.md) for the underlying TLS setup,
[operator runbook — tunnels](operator-runbook.md#tunnels) for ACLs and
the per-tunnel SPA flow, and [architecture](architecture.md) for the
chisel transport details.
