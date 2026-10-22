# What changed from openrport

ProxiPort imports the openrport Go server and agent tree at the
MIT-licensed state of the fork (see [origin](origin.md) for the full
lineage). This page is the concrete, behaviour-oriented diff for
anyone migrating from rport or openrport.

## Removed

### The `plus/` plugin scaffolding

openrport's source tree contained a directory called `plus/` whose
sole function was to load a separately-distributed proprietary plugin
binary (the "Plus" plugin) that gated several enterprise features.
The Plus binary itself was never in any public source repository — it
was distributed by the previous upstream maintainer under a
proprietary licence, behind a paywall.

ProxiPort deletes `plus/` entirely along with every `IsPlusEnabled`
capability gate. We do not ship the proprietary binary, do not load
it, and do not reimplement Plus features as a licence-gated commercial
add-on. Features that previously lived behind the gate (OAuth/OIDC,
RBAC, alerting, …) will be reimplemented in the open under AGPL.

User-visible effect: API endpoints that returned a Plus status
(`GET /api/v1/plus/status`, etc.) are gone; the corresponding settings in
`proxiportd.conf` are accepted but no longer have any effect; the SPA
does not show Plus-only configuration pages.

### The proprietary Vue/Nuxt frontend

Upstream rport/openrport shipped a web frontend, but only as a
prebuilt JavaScript bundle. The source for that frontend was never
released as FOSS. Because the source was never available, we could
not have forked it, and we made no attempt to.

The frontend in `frontend/` is original work: a SvelteKit SPA written
from scratch in TypeScript against the existing REST API. It is
AGPL-licensed along with the rest of the tree.

![ProxiPort login screen.](screenshots/00-login-screen.png)

![Inventory page after a few agents have connected. It combines what
the upstream SPA split across its "Clients" and "Inventory" pages,
rebuilt against the same REST endpoints.](screenshots/01-inventory-dashboard.png)

User-visible effect: the operator-facing web UI looks different.
Functional surface area is broadly the same — clients, tunnels,
commands, scripts, schedules, monitoring, library, vault, users,
client-auth, client-groups, API tokens, audit log — but the rendering
is now Tailwind/Svelte rather than Vue/Nuxt, and some pages are still
being reimplemented. Most notably, the top-level **Scripts** page
(running one script on many clients at once) is not implemented yet.
The per-client Scripts tab works; to run on several clients, use the
**Commands** runner and pick an interpreter. Document authoring, the
Documentation page and some profile preferences are also still
placeholders (see [open issues] for the running list).

[open issues]: https://github.com/proximile/proxiport/issues

### The `tacoscript` interpreter

Upstream let you set `interpreter: "tacoscript"` on a command or a
script, which made the agent hand the file to a `tacoscript` binary —
a separate third-party tool that neither rport nor ProxiPort ever
installed for you.

ProxiPort removes it in **v0.2.0**. It was a menu item that could not
work: both places the SPA offered it were *command* forms, and the
server rejected tacoscript for commands outright, so choosing it
returned `400`. `tacoscript` is no longer an accepted `interpreter`
value and is gone from the API schema and the SPA.

If you actually run tacoscript, nothing stops you: the server does not
validate the interpreter of a *script*, so an alias or an absolute path
(`/usr/local/bin/tacoscript`) still works. The only change is that
ProxiPort no longer writes the script with a `.yml` extension for you.

### Some upstream API quirks

We have fixed a small number of inherited bugs where the upstream API
behaviour was clearly unintended. These changes are intentional
deviations from upstream, and the SPA is the only first-party
consumer that depended on them:

- `GET /api/v1/schedules` used to return a double envelope
  `{"data":{"data":[…],"meta":{…}}}` because the handler re-wrapped
  the manager's already-wrapped payload. ProxiPort returns the
  single-envelope `{"data":[…],"meta":{…}}` like every other list
  endpoint. The SPA accepts either shape, so older third-party clients
  written against the buggy response are not broken.

## Added

Configuration and behaviour that rport / openrport never had. All of
it is opt-in: an upstream config that leaves these sections out
behaves as before.

- **End-to-end encrypted forwarding** (`[e2e]` in `proxiport.conf`).
  Tunnel traffic can be encrypted between the endpoints so the server
  only relays ciphertext. See
  [end-to-end encrypted forwarding](e2e-forwarding.md).
- **Tor / VPN egress for the agent** (`transports` in `[client]`). An
  ordered list of SOCKS proxies and/or `direct` that the agent tries
  when it dials the server, with `transport_dial_timeout` per attempt.
  See [anonymous transport](anonymous-transport.md).
- **Per-client tunnel defaults** (`[tunnels]` in `proxiport.conf`).
  Default options for the permanent tunnels in `remotes`, which any
  single entry can override. See the comments in
  [`proxiport.example.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf).
- **At-rest encryption of server secrets** (`[key_provider]` in
  `proxiportd.conf`). Vault values, the `totp_secret` of
  database-backed users and the secrets in `proxiportd.conf` itself can
  be stored as ciphertext under a key held only in memory. See
  [encrypting the config secrets](operator-runbook.md#encrypting-the-config-secrets).
- **A tamper-evident audit log.** With a key provider configured,
  audit entries carry a keyed HMAC chained to the previous entry, so a
  removed or altered entry can be detected. See
  [tamper-evidence](operator-runbook.md#tamper-evidence).

## Kept as-is

The pieces that gave rport / openrport their value are kept exactly:

- the chisel-based SSH-over-WebSocket tunnel transport,
- the REST API surface (`/api/v1/*`),
- the agent control-channel protocol,
- the `proxiportd.conf` / `proxiport.conf` TOML config formats
  (option names and tags are structurally compatible with upstream's
  `rportd.conf` / `rport.conf` — see [migration](migration.md) for
  details),
- the optional `[database]` connection (SQLite or MySQL/MariaDB) for
  the agent-credential and API user/group tables; all other server
  state is kept in SQLite files under `data_dir` (see
  [architecture](architecture.md#datastore)),
- the BDD test harness shape,
- the systemd unit conventions (unit layout, `ExecStart … -c <conf>`).
  The units now run under new accounts, `proxiport` for the server and
  `proxiport-agent` for the agent; see [migration](migration.md) for
  the chown steps.

## Renamed

| Upstream | ProxiPort |
| --- | --- |
| `rportd` / `openrportd` | `proxiportd` |
| `rport` (agent) / `openrport` (agent) | `proxiport` |
| `/etc/rport/` | `/etc/proxiport/` |
| `/var/lib/rport/` (server) | `/var/lib/proxiport/` |
| `/var/lib/rport/` (agent) | `/var/lib/proxiport-agent/` |
| systemd unit `rportd.service` | `proxiportd.service` |
| systemd unit `rport.service` (agent) | `proxiport.service` |
| `rport` system account (server) | `proxiport` |
| `rport` system account (agent) | `proxiport-agent` (a separate account from the server's) |
| `rportd.conf` | `proxiportd.conf` |
| `rport.conf` | `proxiport.conf` |

## Planned (not yet shipped)

These are tracked, not promised. Filed here so contributors can
see the direction of travel before any code lands.

- **OIDC / OAuth login.** First-class identity-provider login as
  an open-source replacement for the upstream Plus plugin's SSO.
  See [API authentication](api-authentication.md) for the
  current local-user model the OIDC flow will sit alongside.
- **Extended group permissions (RBAC).** Per-group policy file
  (YAML) layered on top of the existing
  [client groups and permissions](client-groups-permissions.md)
  model, so operators can express "this group can run scripts on
  hosts tagged `prod` but not `infra`" without code changes.
- **Alerting / notification dispatch.** SMTP, Slack, and generic
  webhook sinks for the [monitoring](monitoring.md) rule engine,
  replacing the proprietary alerting that used to ship with the
  upstream Plus plugin.
- **Sandboxed noVNC demo.** The public ProxiPort demo will gain
  a pre-opened noVNC tunnel terminating at a network-jailed
  Firefox container that can reach only a baked-in offline
  Wikipedia mirror — so visitors can exercise the proxy feature
  end-to-end without ProxiPort itself operating an unrestricted
  web kiosk.

## Licence change

The combined work changes from MIT (upstream openrport / rport) to
**AGPL-3.0-or-later** (ProxiPort). The inherited MIT code stays under
MIT — see [LICENSE-MIT](https://github.com/proximile/proxiport/blob/main/LICENSE-MIT)
for the verbatim original notice and [origin](origin.md) for the
rationale.

If you contribute to ProxiPort, your contribution is licensed under
AGPL-3.0-or-later (inbound = outbound). See [`CONTRIBUTING.md`](https://github.com/proximile/proxiport/blob/main/CONTRIBUTING.md).
