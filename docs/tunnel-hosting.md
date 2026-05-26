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

`proxiportd` launches and supervises a [Caddy](https://caddyserver.com/)
process. Caddy listens on the configured port (typically 443),
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

   You can also let the [built-in ACME](https.md#built-in-acme)
   manage the wildcard:

   ```toml
   [server]
     tunnel_host = "tunnels.proxiport.example.com"
     tunnel_enable_acme = true
   ```

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

When creating a tunnel from the SPA's per-client **Tunnels** tab, tick
one of:

- *Enable HTTP reverse proxy* (for HTTP/HTTPS targets);
- *Enable NoVNC (VNC via browser)*;
- *Enable RDP via browser*.

Any of those triggers subdomain allocation. The tunnel record now
carries a `tunnel_url` field, and the SPA's "access tunnel" button
points there instead of the random-port URL.

To inspect the live Caddy routes:

```bash
curl --unix-socket /var/lib/proxiport/caddy-admin.sock \
  -H "host:unix" \
  http://localhost/config/apps/http/servers/srv0/routes | jq
```

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

Restart `proxiportd`. The tunnel-create form now exposes the
"Enable NoVNC" option for tunnels targeting a VNC port.

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
`127.0.0.1`. Once running, tunnels created with "Enable RDP via
browser" expose a URL like
`https://proxiport.example.com:<port>/` that loads the Guacamole
client in the browser.

You can pass through Guacamole session parameters as query strings:

```
/?username=Administrator&width=1280&height=800&security=nla&keyboard=en-us-qwerty
```

For security reasons the password cannot be injected — the operator
types it into the Guacamole login form. Available `keyboard` values
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
2. The server saves the file to `{data_dir}/filepush/<uuid>`.
3. Each addressed agent opens an SFTP-over-SSH session inside its
   existing chisel channel, downloads the file to its own
   `{data_dir}/filepush/<uuid>`, verifies the MD5, applies any
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
| `client_id` / `group_id` | Repeatable. At least one of either is required. |
| `dest` (required) | Absolute destination path on the agent. |
| `force` | Overwrite if the file already exists. |
| `sync` | Compare MD5; overwrite only if changed. Apply mode/owner if requested. |
| `mode` | Unix mode bits, e.g. `0644`. Default `0764`. |
| `user`, `group` | Unix owner / group. Requires sudo on the agent for non-default owners. |

### File-size limit

Server side, `[api] max_filepush_size` caps the single-file size in
bytes (default 10 MiB). Increase it under `[api]` if you need to push
larger payloads. The general `max_request_bytes` cap does **not**
apply to this endpoint.

### Restricted destinations

By default the agent refuses to write into directories listed in
`[file-reception] protected`:

```toml
[file-reception]
  enabled = true
  protected = ['/bin', '/sbin', '/boot', '/usr/bin', '/usr/sbin', '/dev', '/lib*', '/run']
  # Windows defaults:
  # protected = ['C:\Windows\', 'C:\ProgramData']
```

These are glob patterns matched against both the target directory and
the target file path. To disable reception entirely, set `enabled = false`.

### Privileged writes

The agent runs as an unprivileged user. To let it chown a pushed file
or write into a root-owned directory, add narrow sudoers rules:

```text
# /etc/sudoers.d/proxiport-filereception
proxiport ALL=NOPASSWD: /usr/bin/chown * /var/lib/proxiport/filepush/*_proxiport_filepush
proxiport ALL=NOPASSWD: /usr/bin/mv     /var/lib/proxiport/filepush/*_proxiport_filepush *
```

Limit the wildcards as much as your operational shape allows.

### Live progress

`WS /ws/uploads` streams per-file results as they happen. Subscribers
get JSON frames like:

```json
{
  "client_id": "alpha-prod",
  "uuid": "482ae29e-d372-4d21-8cb4-58d75482b7e1",
  "filepath": "/etc/hosts.allow",
  "size": 17118,
  "message": "file successfully copied to destination",
  "status": "success"
}
```

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
- **Tunnel URL returns a Caddy 404.** The subdomain has not been
  registered with Caddy yet — either the tunnel hasn't finished
  creating, or `proxiportd` and Caddy lost sync. Check
  `journalctl -u proxiportd -n 100 --no-pager` for ACME errors.
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
