# Securing proxiportd with HTTPS

`proxiportd` terminates TLS itself on both the API/SPA listener and
the agent (chisel) listener. Three deployment shapes are supported:

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
| Built-in ACME | Single-purpose host, no other web service on the box, port 80 reachable from the internet for HTTP-01 challenges. Easiest production setup. |
| Manual cert files | Behind an internal CA, an air-gapped environment, or any setup where certbot cannot reach Let's Encrypt directly. |
| External reverse proxy | You already run nginx/Caddy; you want one TLS surface across many services; you want HTTP/2 / HTTP/3, request logging, rate limiting, or path routing in one place. |

## External reverse proxy in front

Bind ProxiPort to localhost and let the proxy handle TLS. Two pieces
need to be reachable through the proxy:

- the **API/SPA** listener (`[api] address`), serving HTTP+WS, and
- the **agent (chisel)** listener (`[server] address`), serving the
  WebSocket upgrade that carries the SSH-over-WS tunnel transport.

A minimal Caddy config that fronts both, sharing the same hostname on
port 443:

```caddy
proxiport.example.com {
    reverse_proxy 127.0.0.1:3000
}
```

Caddy proxies WebSocket upgrades by default; no extra directives
needed for `/ws/*` paths. To split agents onto a separate hostname or
port, add a second site block pointing at `127.0.0.1:80`.

The matching `proxiportd.conf`:

```toml
[server]
  address = "127.0.0.1:80"

[api]
  address = "127.0.0.1:3000"
  # leave cert_file and key_file commented out
```

For nginx, the key directives are `proxy_set_header Upgrade`,
`proxy_set_header Connection "upgrade"`, and a long `proxy_read_timeout`
on the WebSocket location.

If your proxy strips the original client IP, configure it to forward
`X-Forwarded-For` — the `Only my current IP address` tunnel ACL preset
reads that header. See [IP-address determination](ip-address-determination.md).

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

!!! note "Port 80 is required for HTTP-01"
    Let's Encrypt validates ownership over port 80 or 443. If the API
    is on a non-default port, expose port 80 to ProxiPort as well via
    `[server] acme_http_port = 80`. Other ports do not work — that's
    a Let's Encrypt protocol restriction, not a ProxiPort one.

Certificates are stored in `{data_dir}/acme/`, which defaults to
`/var/lib/proxiport/acme/`.

If you also use the [Caddy-integrated tunnel subdomains](tunnel-hosting.md),
the same ACME machinery can issue the wildcard certificate for the
subdomain prefix:

```toml
[server]
  tunnel_host = "tunnels.proxiport.example.com"
  tunnel_enable_acme = true
```

Set `log_level = "debug"` under `[logging]` if a request fails — the
ACME client logs the underlying challenge response there.

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
curl -Iv -u admin:password https://proxiport.example.com/api/v1/status
```

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

Both `[api]` and `[server]` accept a `tls_min` setting. The default is
TLS 1.3. To accept TLS 1.2 for legacy clients:

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
sudo journalctl -u proxiport -n 50 --no-pager
```

A healthy agent log shows `Connected (...) ssh-fingerprint OK` within
a few seconds.

See also: [install](install.md), [operator runbook](operator-runbook.md),
[tunnel hosting](tunnel-hosting.md) for the related Caddy + wildcard
certificate setup.
