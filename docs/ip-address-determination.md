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

Off by default, for privacy. Each refresh interval the discovery
makes up to two outbound requests to a server you do not control: one
over IPv4, then one over IPv6. They use whatever scheme `ip_api_url`
names; a plain `http://` URL is accepted and sends the request
unencrypted. Decide whether that is acceptable for your deployment
before turning it on.

!!! warning "This call does not use the agent's proxy"

    The lookup builds its own HTTP connection and deliberately dials
    IPv4 and IPv6 separately, so it does **not** go through `proxy` or
    `transports`. On an agent routed over Tor or a VPN it would announce
    the host's real address — which is the one thing that setup exists
    to hide. With `proxy` set the agent ignores `ip_api_url` and logs a
    warning; with `transports` set it refuses to start. See
    [Anonymous transport](anonymous-transport.md).

In
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
under `[client]`:

```toml
[client]
  ip_api_url   = "https://api64.ipify.org?format=json"
  ip_refresh_min = 30
```

`ip_refresh_min` is the refresh interval in minutes. The default is
30; lower values produce more accurate state at the cost of more
outbound calls.

## Supported IP-discovery APIs

The agent accepts only one response shape: a JSON object with an `ip`
string field:

```json
{"ip": "203.0.113.42"}
```

Any extra fields are ignored. A plain-text body holding just the
address is rejected: the lookup fails, the agent logs
`Failed to determine IP addresses`, and no addresses are reported.
Services that return plain text by default (such as
`https://ifconfig.me/ip`, or `api.ipify.org` without `?format=json`)
must be used in their JSON form, for example
`https://api64.ipify.org?format=json`. Other examples:
`https://api.my-ip.io/ip.json`, `https://api.myip.com`,
`https://api.seeip.org/jsonip`.

Verify the URL works over both IPv4 and IPv6 before deploying it. On
each refresh ProxiPort makes one IPv4 call and then one IPv6 call, and
both must succeed. If either call fails (the endpoint does not answer
over that family, or the host has no connectivity for it), the agent
logs `Failed to determine IP addresses` and reports nothing for that
refresh, not even the address that worked. A host with no IPv6
connectivity therefore never reports an external IP. Both commands
below must print a JSON object with an `ip` field:

```bash
curl -4 -s 'https://api64.ipify.org?format=json'; echo
curl -6 -s 'https://api64.ipify.org?format=json'; echo
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

- `address` — the transport-level IP the server saw the connection
  come from (host only; the port is not stored).
- `ipv4` and `ipv6` — arrays of the addresses the agent found on its
  own (non-loopback) network interfaces, as sent when it connects.
  Discovered external addresses never appear here.
- `ext_ip_addresses` — the result of external-IP discovery, for
  example
  `{"ipv4": "203.0.113.7", "ipv6": "2001:db8::7", "error": "", "updated_at": "..."}`.
  Here `ipv4` and `ipv6` are single strings, not arrays. The value is
  `null` when discovery is disabled or has not reported yet. A failed
  lookup is logged on the agent and not sent, so the value keeps the
  last successful result. To choose it with the
  `fields` query option, use the name `ip_addresses`.

The SPA's per-client detail page shows the transport address and the
interface addresses in its Network card. It does not show the
discovered external addresses yet; read them from the API
(`ext_ip_addresses`).

The "Only my current IP address" preset on the tunnel-create form uses
the address the server saw the **request** come from (`GET /me/ip`).
That is the operator's IP, not the agent's, so the agent's discovery
setting does not affect it. Behind a reverse proxy this address comes
from `X-Forwarded-For` only when the proxy is listed in
`[api] trusted_proxies` (see below). Otherwise it is the socket peer.

## Forwarded-for handling on the server

This takes **two** settings, one on each side. Forwarding the header
from the proxy is not enough on its own: the server ignores
`X-Forwarded-For` unless the socket peer is listed in
`[api] trusted_proxies`, and that list is **empty by default**.

Without it the audit log records the proxy's address — usually
`127.0.0.1` — for every operator action, and `GET /me/ip` returns the
same, so the "Only my current IP address" tunnel ACL preset writes an
ACL for the loopback rather than for the operator. Neither failure is
reported: the header is simply not read.

**1. Tell the server which peers may speak for someone else**, in
`proxiportd.conf`:

```toml
[api]
  ## CIDRs or addresses of the reverse proxies in front of this server.
  ## Empty (the default) means X-Forwarded-For is never trusted.
  trusted_proxies = ['127.0.0.1/32', '::1/128']
```

Use the address the proxy connects *from*, which for a proxy on the
same host is the loopback the API is bound to. Keep this list as small
as it can be: anything in it can claim to be any client.

**2. Configure the proxy to send the header.** For nginx:

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

With built-in TLS (no proxy in front) leave `trusted_proxies` unset.
The connection's remote address is the operator's address, and an
empty list is what stops a client from claiming otherwise.

To check it is working, make any state-changing API call through the
proxy and read the `remote_ip` on the resulting audit-log row: it
should be your address, not the proxy's.

## Hardening checklist

- Decide whether the privacy tradeoff of outbound IP discovery is
  acceptable. If not, leave it off and rely on the transport IP.
- If you enable it, prefer a small endpoint you operate, ideally on
  HTTPS with basic auth, to keep the metadata trail inside your
  network.
- Always set the refresh interval to a value that won't get you
  throttled — 30 minutes is the default and is rarely the bottleneck.
- If you front the server with a reverse proxy, do both halves: set
  `[api] trusted_proxies` to the proxy's address **and** configure the
  proxy to forward `X-Forwarded-For`. Either one alone leaves the audit
  log recording the proxy instead of the operator. With built-in TLS
  the connection remote address is used directly, no header
  configuration is needed, and `trusted_proxies` should stay empty.

See also: [client attributes](client-attributes.md) for tagging agents
by location/role/datacenter, and
[operator runbook — tunnels](operator-runbook.md#tunnels) for the
single-IP ACL preset that uses the operator's request address.
