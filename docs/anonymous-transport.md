# Anonymous transport (Tor / VPN)

The agent makes one outbound connection to the server and carries
everything — the control channel, every tunnel, every command result —
over it. Anything that carries that single TCP connection therefore
carries the whole agent, so routing it through Tor or a VPN needs no
special protocol support: it needs a proxy and a policy.

The `transports` key is that policy. It is an ordered list of egress
paths. The agent tries them strictly in order and keeps the first that
connects.

## When to use it

- The agent runs on a host whose address should not be visible to the
  network between it and the server.
- The server is published as a Tor onion service and has no clearnet
  address at all.
- The agent sits behind a VPN and must not fall back to the local
  connection when the VPN drops — or must, but visibly.

If none of these apply, leave it unset. An agent with no `transports`
behaves exactly as it always has.

## The list is the policy

There is no `allow_fallback` switch, and nothing is ever appended to the
list for you. The agent dials the open internet if and only if you wrote
the word `direct` into the list yourself.

```toml
[client]
  # Tor, falling back to the clear internet if the tor daemon is down.
  transports = ["socks5h://127.0.0.1:9050", "direct"]
```

```toml
[client]
  # A VPN, then Tor, and never the clear internet. If both are down the
  # agent retries them forever and does not connect.
  transports = ["socks5h://gluetun:1080", "socks5h://127.0.0.1:9050"]
```

Removing `direct` is how you fail closed. An agent that cannot reach its
server is a recoverable operational problem; an agent that quietly
revealed the address you were hiding is not.

Accepted entries:

| Entry | Meaning |
| --- | --- |
| `direct` | Dial the server with no proxy. Must be the last entry. |
| `socks5h://host:port` | SOCKS5. The proxy resolves the hostname, which is what makes `.onion` work. |
| `socks://`, `socks5://` | Accepted spellings of the same thing. |
| `http://host:port` | HTTP CONNECT. |

Credentials may be embedded (`socks5h://user:pass@host:1080`). They are
masked in every log line and are never sent to the server.

Anything else — an unknown scheme, a typo, `https://` — stops the agent
at startup with an error naming the entry and its position. An unusable
entry is never skipped, because skipping it would silently shorten the
policy you wrote.

## Order of attempts

Entries are tried transport-major: **every** server over the first
transport before **any** server over the second.

```
transports      = ["socks5h://127.0.0.1:9050", "direct"]
server          = "primary.example.com:80"
fallback_servers = ["secondary.example.com:80"]
```

gives the order: primary over Tor, secondary over Tor, primary direct,
secondary direct. Server redundancy never buys itself with anonymity — a
dead primary alone cannot drop you to the clear internet while the
secondary is still reachable over Tor.

`transport_dial_timeout` (default `45s`) bounds each attempt. A whole
sweep therefore costs at most
`len(transports) × (1 + len(fallback_servers)) × transport_dial_timeout`;
keep that inside your watchdog window if you run one.

After a sweep fails the agent backs off exactly as before (5s to 10m)
and sweeps again. `server_switchback_interval` (default `2m`) retries
the *preferred* combination — first transport, main server — while
connected to any other, so a transient Tor or VPN outage heals itself.
Set it to `0` to stay put.

### What advances the chain, and what does not

Only a failure of the path advances to the next entry: a refused
connection, a SOCKS or CONNECT failure, a timeout, a broken handshake.

A **credential rejection does not.** The agent uses one credential for
every server and every transport, so a rejection at one entry is a
rejection at all of them, and walking the list could only end in dialing
the clear internet because `auth` has a typo in it. The sweep stops and
the agent backs off instead.

## Running over Tor

Run a `tor` daemon — the system package or a sidecar container — and
point the agent at its SOCKS port:

```toml
[client]
  server      = "yourserveraddress.onion:80"
  transports  = ["socks5h://127.0.0.1:9050"]
  fingerprint = "SHA256:..."
```

Two constraints are enforced for you:

- **A `.onion` server is never dialed directly.** Handing a `.onion`
  name to the host resolver would disclose it to whoever runs that
  resolver, so such a candidate is skipped rather than attempted.
- **A `.onion` server requires a pinned `fingerprint`.** No CA issues
  certificates for `.onion` names, so the connection is `ws://` with no
  TLS and the SSH host-key pin is the *only* thing authenticating the
  server. The agent refuses to start without it.

Publish the onion service with a standard `torrc` mapping in front of
the server's existing listener — the server needs no configuration
change and no code change:

```
HiddenServiceDir /var/lib/tor/proxiport/
HiddenServicePort 80 127.0.0.1:8080
```

Add the onion to the server's `url` list so it is advertised alongside
the clearnet address.

## Running over a VPN

There are two shapes, and they are not equivalent.

**A proxy the VPN exposes** (for example gluetun with its SOCKS5 or
HTTP proxy enabled) is a normal chain entry and can fail over:

```toml
transports = ["socks5h://gluetun:1080", "socks5h://127.0.0.1:9050"]
```

**A network-namespace VPN** — running the agent inside the VPN
container's namespace, or behind a host WireGuard interface — is
invisible to the agent. Every dial it makes is already inside the
tunnel, so the correct configuration is simply `direct` (or no
`transports` at all). This is a robust and common deployment, but the
agent cannot fail over *out of* it, because the namespace is fixed
before the process starts. If you need failover, use a proxy the VPN
exposes.

Note that gluetun's Shadowsocks listener is not a SOCKS5 proxy despite
the name — it speaks the Shadowsocks protocol and this agent cannot use
it. Use gluetun's SOCKS5 or HTTP proxy.

## What this does and does not cover

The chain covers **the agent's connection to the server.** It is not a
whole-host anonymiser, and the following are outside it:

- **Package-manager refreshes.** `updates_interval` (default 4h) shells
  out to `apt-get update`, `dnf check-update`, `zypper refresh` or the
  Windows Update client. These are subprocesses; no setting in this file
  can route them. **Set `updates_interval = 0`** on an agent that must
  not originate direct traffic.
- **`ip_api_url`.** The external-IP lookup builds its own connection and
  cannot traverse the chain. The agent refuses to start with both set.
- **Server-issued commands and scripts.** If `[remote-commands]` or
  `[remote-scripts]` is enabled, the server can run programs on the
  host, and those programs make their own network calls. The server is
  inside your trust boundary; the chain hides the agent from the network
  in between, not from the server.
- **Tunnel traffic to local targets.** A tunnel to `127.0.0.1:5432` or a
  LAN host is dialed on the local stack, deliberately — reaching those
  is what the agent is for.

## Hardening checklist

- [ ] `direct` omitted from `transports` unless a clear-internet fallback
      is genuinely acceptable.
- [ ] `fingerprint` pinned, and `require_fingerprint = true`.
- [ ] `ip_api_url` unset.
- [ ] `updates_interval = 0`.
- [ ] `[remote-commands]` and `[remote-scripts]` disabled unless needed.
- [ ] Alerting on the literal string `ANONYMITY DOWNGRADE:` in the agent
      log. That line is emitted on **every** connection that uses
      `direct` after an earlier transport failed — a successful clearnet
      connection on an agent configured for Tor is itself the incident.
- [ ] `transport_dial_timeout × entries × servers` inside your watchdog
      window.

See also: [IP-address determination](ip-address-determination.md) for
the external-IP lookup this feature disables, and
[client authentication](client-authentication.md) for the fingerprint
pin that a `.onion` endpoint depends on.
