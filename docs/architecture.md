# Architecture

ProxiPort is a three-tier system: an **agent** running on each managed
host, a **server** running somewhere the agent can reach, and a **web
UI** served by the server for human admins. There is no required cloud
component and no required third-party service.

```
+---------+      WebSocket (typically 443     +-----------+
| agent   |       behind a reverse proxy)     | server    |
+---------+   <----------------------------   |           |
              chisel control channel          |           |
                                              |           |
+---------+      HTTPS (typically 443         |           |
| admin   |       behind the same proxy)      |  REST API |
| browser |   <----------------------------   |  + SPA    |
+---------+      SvelteKit SPA + REST         +-----------+
                                                    |
                                                    | SQLite or
                                                    | MySQL
                                                    v
                                              +-----------+
                                              | datastore |
                                              +-----------+
```

## Server

A single Go binary, `proxiportd`. Responsibilities:

- terminate the chisel control channel from agents on
  `[server] address` (`0.0.0.0:8080` out of the box)
- serve the REST API and the SvelteKit SPA on `[api] address`
  (`0.0.0.0:3000` in the example config)
- persist clients, users, tunnels, scripts, commands, schedules, and
  audit records to SQLite (default) or MySQL
- enforce auth (HTTP-Basic for agents on the tunnel port; JWT for the
  API; optional TOTP second factor)
- multiplex tunnel sessions per connected agent

The server is the only piece that holds state. Agents are otherwise
stateless beyond their own configuration.

![Server info page — version, host-key fingerprint, pairing URL, the
list of Connect-URLs the agents and admins use, and a snapshot of
the auth posture.](screenshots/23-server-info-2fa-off.png)

## Agent

A single Go binary, `proxiport` (the agent). Responsibilities:

- open and maintain a chisel WebSocket control channel to the server
- present an identity (client ID + auth credentials) to the server
- accept tunnel requests from the server and open the corresponding
  local TCP listeners or forward to remote local services
- execute commands and scripts dispatched by the server (when enabled
  in the agent config) and stream their output back

Agents do not call out to the public internet beyond their server
connection. They do not require an inbound port.

![Client detail page — identity, OS / kernel / hardware inventory,
recent heartbeat, and the tabs that group everything you can do
against this agent.](screenshots/02-client-detail-alpha.png)

![Monitoring tab — CPU and memory series sampled from the agent and
held in `monitoring.db`. Defaults to short retention; tune with
`[monitoring]` in `proxiportd.conf`.](screenshots/03-monitoring-cpu-mem.png)

## Tunnel transport

ProxiPort uses the [chisel](https://github.com/jpillora/chisel)
SSH-over-WebSocket transport, inherited from the upstream fork. Each
chisel session is one TLS-or-not-TLS WebSocket carrying SSH framing,
inside which tunnel sub-channels are multiplexed.

For production deployments put the server behind a reverse proxy that
terminates TLS on both `[server] address` (chisel control channel) and
`[api] address` (REST + SPA). The same proxy can host them on a single
port via SNI / path routing.

![Tunnel create form — pick the remote target on the agent, the
public scheme, and the ACL gate. The server allocates the public
port from `[server] used_ports` unless you pin
one.](screenshots/04-tunnel-create-form.png)

![Active tunnel — the ACL is enforced at the server's listener
before any byte reaches the agent. `Only my current IP address` is
a one-click preset.](screenshots/05-tunnel-acl-active.png)

![Global active-tunnel view across all clients. The same listing
backs `GET /api/v1/tunnels`.](screenshots/06-tunnels-global-active.png)

## Datastore

SQLite is the default and is the right choice for small to medium
deployments (single server, up to a few hundred connected agents). For
larger deployments or for HA setups, MySQL is supported via the existing
upstream `db.MySQL` driver path.

The vault — an encrypted KV store for documents and per-client
secrets — uses a separate SQLite file with passphrase-derived
encryption. The passphrase is supplied at vault unlock time and is not
persisted server-side.

![Vault unlock prompt. The passphrase is held in process memory only
— a server restart re-locks the vault and every operator has to
unlock again.](screenshots/17-vault-unlock.png)

## Frontend

The web UI is a SvelteKit single-page app served as static assets from
the server's `doc_root`. It talks to the same REST API the agents'
control plane uses. There is no separate Node runtime in production.

## Authentication

Three layers:

1. **HTTP-Basic** on the chisel agent port. Agents authenticate to the
   server before the control channel comes up. Credentials are
   `[server] auth = "<client-auth-id>:<password>"` (or an `auth_file`
   / `auth_table` if you prefer).
2. **JWT bearer** on the REST API. Login is HTTP-Basic to `/login`,
   which returns a short-lived JWT (default 10 minutes, configurable
   via `?token-lifetime=`). The SPA requests a 24-hour lifetime by
   default.
3. **TOTP second factor** is optional; when enabled, `/login` returns a
   short-lived intermediate token, and the SPA POSTs the TOTP code to
   `/verify-2fa` to obtain the final JWT.

![Login with 2FA enabled — the SPA holds the intermediate token in
memory, prompts for the TOTP, and only exchanges for the final JWT
on a correct code.](screenshots/33-login-2fa-totp.png)

All authentication endpoints feed a per-username ban list with a
2-second penalty for failed attempts, which is what produces the
`429 too many requests` response under brute force or session-expiry
cascades.
