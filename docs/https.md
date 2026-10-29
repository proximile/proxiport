# Securing proxiportd with HTTPS

`proxiportd` terminates TLS itself on the API/SPA listener. The agent
(chisel) listener speaks plain WebSocket: the SSH session inside it
carries its own encryption and the agent checks the server's host key
against its configured `fingerprint`, so TLS on that port is a reverse
proxy's job, not the server's. Three
deployment shapes are supported for the API:

1. **Built-in ACME** — ProxiPort obtains and renews a Let's Encrypt
   certificate itself. One config line on a single-purpose host; no
   external tooling.
2. **Manually-managed certificates** — supply your own
   `cert_file`/`key_file` and renew them on your own schedule (or
   wire `certbot` to do it).
3. **External reverse proxy in front** — nginx, Caddy, or Traefik
   handles TLS; ProxiPort listens on plain HTTP/WebSocket bound to
   localhost. Useful when you already run a TLS edge for other
   services on the same host.

!!! warning "Never run the API on plain HTTP across the network"
    Credentials, JWTs, command output, and file pushes all cross the
    API channel in clear. Even on a trusted intranet, terminate TLS
    somewhere. The only safe plain-HTTP binding is `127.0.0.1` with a
    TLS proxy on the public side.

## Choosing between the three

| Setup | When it fits |
| --- | --- |
| Built-in ACME | Single-purpose host, no other web service on the box, the API on port 443 and reachable from the internet (Let's Encrypt validates with TLS-ALPN-01 on that listener). Easiest production setup. |
| Manual cert files | Behind an internal CA, an air-gapped environment, or any setup where certbot cannot reach Let's Encrypt directly. |
| External reverse proxy | You already run nginx/Caddy; you want one TLS surface across many services; you want HTTP/2 / HTTP/3, request logging, rate limiting, or path routing in one place. |

## External reverse proxy in front

Bind ProxiPort to localhost and let the proxy handle TLS. Two pieces
need to be reachable through the proxy:

- the **API/SPA** listener (`[api] address`), serving HTTP+WS, and
- the **agent (chisel)** listener (`[server] address`), serving the
  WebSocket upgrade that carries the SSH-over-WS tunnel transport.

The two listeners are separate servers: agents connect only to the
`[server] address` listener, and the API/SPA is served only on
`[api] address`. The proxy has to route each to its own backend. The
simplest way is two hostnames:

```caddy
proxiport.example.com {
    reverse_proxy 127.0.0.1:3000   # API + SPA
}

agents.proxiport.example.com {
    reverse_proxy 127.0.0.1:80     # agent (chisel) WebSocket
}
```

Caddy proxies WebSocket upgrades by default, so neither block needs
extra directives — neither for the agent transport on the second block
nor for the SPA's `/api/v1/ws/*` command, script and upload streams on
the first.

The matching `proxiportd.conf`:

```toml
[server]
  address = "127.0.0.1:80"
  url     = "https://agents.proxiport.example.com"

[api]
  address = "127.0.0.1:3000"
  # leave cert_file and key_file commented out
```

To serve agents and the UI on one hostname instead, send the whole site
to the agent listener (`reverse_proxy 127.0.0.1:80`) and set
`proxy = "http://127.0.0.1:3000"` under `[server]`. The agent listener
answers agent upgrades itself and forwards every other request,
including the SPA's WebSocket streams, to the API.
Every request on that listener, proxied ones included, is limited to
`[server] max_request_bytes_client` (512 KB by default), so raise it if
you upload larger files through the web UI in this setup.

Agents then dial the proxy, not the loopback listener, and must give
the scheme explicitly: `[client] server = "https://agents.proxiport.example.com"`
(the port defaults to 443). A value without a scheme, like the
`host:80` form in the example agent config, is treated as plain
`ws://`, so the agent never completes the handshake with the TLS port.
`[server] url` is the connect address the server advertises; setting
it to the proxied address means agent configs generated through
pairing point at the proxy as well.

For nginx, the key directives are `proxy_set_header Upgrade`,
`proxy_set_header Connection "upgrade"`, and a long `proxy_read_timeout`
on the WebSocket location.

Behind a proxy, also set `[api] trusted_proxies` to the address the
proxy connects from and configure the proxy to forward
`X-Forwarded-For`. Both halves are needed: the server ignores the
header from any peer not on that list, and the list is empty by
default — so without it the audit log records `127.0.0.1` for every
operator action and the `Only my current IP address` tunnel ACL preset
writes an ACL for the loopback. See
[IP-address determination](ip-address-determination.md).

## Built-in ACME

ProxiPort embeds an automatic certificate manager. It will request,
install, and renew a Let's Encrypt certificate without any external
tooling.

Enable it in
[`proxiportd.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf)
under `[api]`:

```toml
[api]
  address = "0.0.0.0:443"
  base_url = "https://proxiport.example.com:443"
  enable_acme = true
```

`base_url` is mandatory — its hostname must resolve in public DNS to
the address the ACME challenge will reach. The certificate is issued
for that hostname.

!!! note "Validation uses TLS-ALPN-01 on port 443 by default"
    With `[api] address` on port 443, Let's Encrypt validates over
    TLS-ALPN-01 on the API listener itself, and nothing else needs to
    be open. If the API cannot be reached on 443 from the internet,
    enable the HTTP-01 fallback with `[server] acme_http_port = 80`,
    which starts a separate plain-HTTP challenge listener. Let's Encrypt
    only connects to ports 443 (TLS-ALPN-01) and 80 (HTTP-01) — that's a
    Let's Encrypt protocol restriction, not a ProxiPort one. The
    packaged config puts the agent listener on port 80
    (`[server] address = "0.0.0.0:80"`), so move the agent listener to
    another port first; otherwise the challenge listener cannot bind
    and issuance fails.

Certificates are stored in `{data_dir}/acme/`, which defaults to
`/var/lib/proxiport/acme/`.

The built-in ACME client can also issue a certificate for the built-in
tunnel proxy. Set the proxy's hostname and turn ACME on for it:

```toml
[server]
  tunnel_host = "tunnels.proxiport.example.com"
  tunnel_enable_acme = true
```

This gets a certificate for that one hostname only. The built-in client
validates over TLS-ALPN-01 or HTTP-01, so it cannot issue the wildcard
certificate that the [Caddy-integrated tunnel subdomains](tunnel-hosting.md)
need. Get that wildcard with a DNS-01 client such as certbot, then
point `[caddy-integration] cert_file` / `key_file` at it.

If a certificate is not issued, check the server's standard error
(under systemd, `journalctl -u proxiportd`). Certificate errors appear
there as `http: TLS handshake error from …: acme/autocert: …` lines,
whatever `log_level` is set to. Also confirm that the `base_url`
hostname (and `tunnel_host`, if used) resolves in public DNS to this
server and that port 443 is reachable from the internet (or port 80,
when `acme_http_port` is set).

## Manual cert files

If you cannot use built-in ACME, point ProxiPort at a key and
certificate on disk. Both files must be readable by the `proxiport`
system user and the key must not be passphrase-protected:

```toml
[api]
  address   = "0.0.0.0:443"
  base_url  = "https://proxiport.example.com"
  cert_file = "/etc/proxiport/tls/fullchain.pem"
  key_file  = "/etc/proxiport/tls/privkey.pem"
```

Restart `proxiportd` after writing the config. Verify with:

```bash
curl -sv -u admin:password -o /dev/null https://proxiport.example.com/api/v1/status
```

The verbose output shows the certificate the server presented
(subject, issuer, expiry) followed by `HTTP/1.1 200` (or `HTTP/2 200`).
Don't use `curl -I`: it sends HEAD, which the API does not serve, so
you get an error status even when TLS is set up correctly.

### Using certbot

`certbot` works fine standalone. The trick is permissions: by default
the renewed files live under `/etc/letsencrypt/archive/` with mode
`700 root:root`, which the unprivileged `proxiport` user cannot read.

```bash
DOMAIN=proxiport.example.com
sudo apt install certbot
sudo certbot certonly --standalone -d "$DOMAIN" -n --agree-tos -m ops@example.com

# Make the Let's Encrypt tree readable by the proxiport user
sudo chgrp -R proxiport /etc/letsencrypt/{archive,live}
sudo find /etc/letsencrypt/{archive,live} -type d -exec chmod g+rx {} \;
sudo find /etc/letsencrypt/{archive,live} -type f -exec chmod g+r {} \;
```

Then point `cert_file` and `key_file` at the `/etc/letsencrypt/live/<DOMAIN>/`
symlinks — they always resolve to the current cert:

```toml
[api]
  cert_file = "/etc/letsencrypt/live/proxiport.example.com/fullchain.pem"
  key_file  = "/etc/letsencrypt/live/proxiport.example.com/privkey.pem"
```

### Auto-renewal

`certbot` ships a systemd timer (`/lib/systemd/system/certbot.timer`)
that runs `certbot -q renew` twice a day. The renewed files keep the
same paths, but **`proxiportd` does not pick them up automatically** —
the certificate is read once at start. Wire a `deploy-hook` to restart
the service after a successful renewal:

```bash
sudo mkdir -p /etc/letsencrypt/renewal-hooks/deploy
sudo tee /etc/letsencrypt/renewal-hooks/deploy/restart-proxiportd.sh <<'EOF'
#!/bin/sh
systemctl restart proxiportd
EOF
sudo chmod +x /etc/letsencrypt/renewal-hooks/deploy/restart-proxiportd.sh
```

If you front ProxiPort with a reverse proxy, the proxy reloads its own
certificate — ProxiPort does not need to know about the renewal.

## TLS minimum version

`[api]` accepts a `tls_min` setting. The default is TLS 1.3. To accept
TLS 1.2 for legacy clients:

```toml
[api]
  tls_min = "1.2"
```

Anything older than 1.2 is hard-rejected. ProxiPort does not implement
SSLv3 / TLS 1.0 / TLS 1.1 fallbacks and will not.

## Verifying the setup

```bash
# Cert chain and expiry
echo | openssl s_client -connect proxiport.example.com:443 -servername proxiport.example.com 2>/dev/null \
  | openssl x509 -noout -subject -issuer -dates

# API reachability
curl -sf -u admin:password https://proxiport.example.com/api/v1/status | jq

# Agent connection from a managed host
sudo systemctl restart proxiport
sudo tail -n 50 /var/log/proxiport-agent/proxiport.log
```

The agent writes its log to the file set by `[logging] log_file` in
`/etc/proxiport/proxiport.conf`. Packaged and paired installs use
`/var/log/proxiport-agent/proxiport.log`, or `/var/log/<account>/proxiport.log`
when the pairing installer ran the agent under another account.
`journalctl -u proxiport`
shows the agent's own output only when `log_file` is unset. A healthy
agent log shows `Connected to <server> (connection request answered in
<time>)` within about ten seconds of start: the agent waits a random
delay of up to 10 s, which it logs, before it sends its first connection
request. The host-key check happens before that line.

See also: [install](install.md), [operator runbook](operator-runbook.md),
[tunnel hosting](tunnel-hosting.md) for the related Caddy + wildcard
certificate setup.
