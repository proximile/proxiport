# IP-address determination

The server records two IP addresses per agent:

- the **transport IP** that the server observes the chisel WebSocket
  connection coming from, and
- one or both of the agent's **external IPv4 / IPv6** addresses as
  reported by the agent itself.

The transport IP is always known — it's the TCP source the kernel sees
on the agent listener. The external IP is optional and is collected
by the agent calling an HTTP IP-discovery API. This page covers when
each value is right, why they can disagree, and how to configure the
external-IP discovery.

## Why both values exist

Three common deployments produce three different "the IP" answers:

1. **Direct internet exposure.** The server is on the public internet,
   the agent dials it directly. The transport IP equals the agent's
   public IP. They always agree.
2. **Reverse proxy in front of the server.** The proxy terminates
   TLS; from the server's perspective every agent appears to come
   from `127.0.0.1`. The transport IP is useless for identifying
   the agent's network.
3. **Agent behind a CGNAT or a corporate egress proxy.** The server
   sees the gateway IP, not the agent's. Even if the server reads
   `X-Forwarded-For`, that header only carries the closest hop the
   proxy can see, not the original interface IP on the agent host.

The external-IP discovery exists for cases 2 and 3. The agent makes a
small outbound HTTPS request to an IP-discovery API, the API echoes
back the source IP it observed, and the agent reports that to the
server. Because the agent's outbound HTTPS connection traverses the
same NAT boundary as a real user request, the IP it gets back is the
one inventory tools and ACLs usually want.

## Enabling the discovery

Off by default, for privacy. The discovery makes one outbound HTTPS
call per refresh interval to a server you do not control. Decide
whether that is acceptable for your deployment before turning it on.

In
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
under `[client]`:

```toml
[client]
  ip_api_url   = "https://ifconfig.me/ip"
  ip_refresh_min = 30
```

`ip_refresh_min` is the refresh interval in minutes. The default is
30; lower values produce more accurate state at the cost of more
outbound calls.

## Supported IP-discovery APIs

The agent expects either of two response shapes:

1. A plain-text body containing the IP and nothing else (`ifconfig.me`,
   `api.ipify.org`).
2. A JSON object that includes an `ip` field:

   ```json
   {"ip": "203.0.113.42"}
   ```

   Any extra fields are ignored. Examples in this shape include
   `https://api.my-ip.io/ip.json`, `https://api.myip.com`, and
   `https://api.seeip.org/jsonip`.

Verify the URL works over both IPv4 and IPv6 before deploying it.
ProxiPort makes one IPv4 call and one IPv6 call per refresh — if the
endpoint only responds to one family, the other column stays empty:

```bash
curl -4 -s https://ifconfig.me/ip; echo
curl -6 -s https://ifconfig.me/ip; echo
```

ProxiPort does not operate a first-party IP-discovery service.

## Running your own IP API

A small PHP file on a webserver of yours is enough:

```php
<?php
header('Content-Type: application/json; charset=utf-8');
echo json_encode(['ip' => $_SERVER['REMOTE_ADDR']]);
```

To gate access with HTTP basic auth, embed the credential in the URL:

```toml
[client]
  ip_api_url = "https://probe:longpassword@ip.example.com/"
```

Running your own endpoint avoids two failure modes of the public
services: throttling (a public service can rate-limit or block your
agents in bulk) and disappearance (the service can go down or stop
being free). It also keeps the discovery traffic inside your network
boundary.

## What the server does with the values

`GET /api/v1/clients` returns both kinds of address on each client
record:

- `address` — the transport-level IP, including the port the server
  saw the connection from.
- `ipv4` and `ipv6` — arrays containing any of the agent's own
  reported external addresses (when discovery is enabled), plus the
  addresses the agent enumerated from its local interfaces.

The SPA shows the discovered IPs on the per-client detail page. The
"Only my current IP address" preset on the tunnel-create form reads
the **request's** `X-Forwarded-For` (i.e. the operator's IP, not the
agent's), so it is unaffected by the agent's discovery setting.

## Forwarded-for handling on the server

If you sit ProxiPort behind a reverse proxy, configure the proxy to
forward `X-Forwarded-For`. The server uses that header to record the
visitor IP for audit-log entries and for the tunnel ACL preset.

```nginx
proxy_set_header X-Real-IP        $remote_addr;
proxy_set_header X-Forwarded-For  $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
```

A Caddy reverse proxy adds `X-Forwarded-For` by default.

`X-Forwarded-For` from outside the trusted network must be stripped
at the edge — otherwise a client can spoof its source IP for audit
log entries. Most reverse proxies do this by default; double-check
yours.

## Hardening checklist

- Decide whether the privacy tradeoff of outbound IP discovery is
  acceptable. If not, leave it off and rely on the transport IP.
- If you enable it, prefer a small endpoint you operate, ideally on
  HTTPS with basic auth, to keep the metadata trail inside your
  network.
- Always set the refresh interval to a value that won't get you
  throttled — 30 minutes is the default and is rarely the bottleneck.
- Configure the reverse proxy to forward `X-Forwarded-For` so the
  audit log records the real visitor IP, not the proxy.

See also: [client attributes](client-attributes.md) for tagging agents
by location/role/datacenter, and
[operator runbook — tunnels](operator-runbook.md#tunnels) for the
single-IP ACL preset that reads `X-Forwarded-For`.
