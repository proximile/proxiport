# Client authentication

The "client" in this page is the **agent** — the `proxiport` binary
running on a managed host. Each agent presents an HTTP basic
credential to `proxiportd` when it opens its chisel control channel.
The server checks the credential against one of three stores: a single
inline credential, a JSON file, or a database table.

For human user authentication (the REST API and the SPA), see
[API authentication](api-authentication.md).

## Credential stores

Exactly one store is active. Combining them is rejected at startup.

### Inline single credential

The simplest setup: pin one pair in the `[server]` section of
[`proxiportd.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf):

```toml
[server]
  auth = "clientAuth1:a-strong-password"
```

!!! warning "Not recommended past the first install"
    A single shared credential means every agent uses the same
    password. Rotating it requires updating every agent at once, and
    revoking one compromised host means revoking them all. Move to
    the JSON file or the database before adding the second agent.

In this mode the credential is read-only: `POST` and `DELETE` on
`/api/v1/clients-auth` return `405`, and the **Client Access** page in
the SPA offers no create or delete. The listing itself still works —
the page loads, and `GET /api/v1/clients-auth` returns the credential's
ID. Only the write operations are disabled.

### JSON credential file

Point `[server] auth_file` at a JSON map of `client-auth-id → password`:

```toml
[server]
  auth_file = "/var/lib/proxiport/client-auth.json"
```

```json
{
  "alpha-prod": "ph2eePh1Bei4iezi",
  "bravo-staging": "iN4eithoG2ohgheo",
  "ops-laptop": "Eech9ieshaeYohth"
}
```

This is the mode the SvelteKit SPA expects. The **Client Access** page
reads and writes this file in place, so the file must be writable by
the daemon's system user:

```bash
sudo chown proxiport /var/lib/proxiport/client-auth.json
sudo chmod 0640 /var/lib/proxiport/client-auth.json
```

Deleting a credential from the **Client Access** page also removes any
clients still bound to it — including disconnected ones — so the
credential and its bound agents are cleared together once you confirm.
A bound agent cannot reconnect afterward; add a fresh credential to pair
a replacement.

On disk-only changes, send the daemon a SIGUSR1 to re-read the file:

```bash
sudo systemctl kill -s SIGUSR1 proxiportd
```

If you would rather have an external tool own the file, set
`auth_write = false` and the API will return HTTP 403 on every
write attempt:

```toml
[server]
  auth_file = "/var/lib/proxiport/client-auth.json"
  auth_write = false
```

### Database table

To share the credential store with an external provisioning system,
point `[server] auth_table` at a table in the `[database]` connection.
The same table schema works for SQLite and MySQL/MariaDB:

```sql
CREATE TABLE clients_auth (
  id       VARCHAR(100) PRIMARY KEY,
  password VARCHAR(100) NOT NULL
);
```

```toml
[database]
  db_type = "sqlite"
  db_name = "/var/lib/proxiport/database.sqlite3"

[server]
  auth_table = "clients_auth"
```

`auth_write` applies here too — set it to `false` to make the API
read-only for an externally-managed table.

## Empty credentials are rejected

An empty client-auth ID or password is refused in both directions:

- The server refuses to **store** one. Creating or updating a
  credential with a blank ID or a blank password fails with an error
  rather than writing the blank, and the at-rest hash upgrade skips a
  credential it would otherwise have to hash from nothing.
- The server refuses to **accept** one. An agent presenting a
  zero-length password is rejected during the handshake before the
  stored credential is consulted, and a stored credential that is
  somehow empty matches nothing.

Both halves matter because the credential comparison is constant-time,
and a constant-time comparison of two empty strings succeeds. A blank
on either side is treated as a failure explicitly rather than left to
the comparison to decide.


## How an agent presents its credential

The agent reads its credential from one of three places, in this
precedence order (highest first):

1. The `--auth` command-line flag.
2. The `PROXIPORT_AUTH` environment variable. `RPORT_AUTH` is accepted as
   a lower-precedence alias for compatibility with the upstream name.
3. The `[client] auth` setting in
   [`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf).

The environment variable and config setting both take the
`client_id:password` form; the flag takes the same value. The
fingerprint has the same three sources — `--fingerprint`,
`PROXIPORT_FINGERPRINT` (alias `RPORT_FINGERPRINT`), then
`[client] fingerprint` — in the same order.

The agent logs which state it resolved on startup: `Client credential
loaded for client id "<id>"` when a credential is present (the password
is never logged), or a clear `no client credential configured` error
when none of the three sources supplied one.

The on-disk form in `proxiport.conf`:

```toml
[client]
  server = "proxiport.example.com:443"
  auth   = "alpha-prod:ph2eePh1Bei4iezi"
  fingerprint = "<server-host-key-fingerprint>"
```

The `fingerprint` value pins the server's host key. The agent refuses
to talk to a server whose key does not match. The fingerprint is
printed by `proxiportd` on startup and shown on the Info page in the
SPA.

## Managing credentials via the API

When `auth_file` or `auth_table` is active and `auth_write` is true,
the `/api/v1/clients-auth` endpoints accept full CRUD:

```bash
# List
curl -s -u admin:password \
  https://proxiport.example.com/api/v1/clients-auth | jq

# Create
curl -s -X POST \
  -u admin:password \
  -H 'Content-Type: application/json' \
  --data-raw '{"id":"delta-edge","password":"foB3ainai9ouQu0o"}' \
  https://proxiport.example.com/api/v1/clients-auth

# Delete
curl -s -X DELETE -u admin:password \
  https://proxiport.example.com/api/v1/clients-auth/delta-edge
```

The same operations are wired into the **Client Access** page in the
SPA — create, edit, delete client-auth IDs, and copy them into each
agent's config.

## Multi-use credentials and client identity

By default every agent that successfully authenticates picks its own
client ID from `proxiport.conf` (the `[client] id` setting, or a UUID
auto-generated on first start). Two agents may share one client-auth
credential as long as their IDs differ.

To force a one-to-one relationship between credentials and clients,
disable `auth_multiuse_creds`:

```toml
[server]
  auth_multiuse_creds = false
```

With multi-use off, a second agent that tries to register with a
credential already in use is rejected. Set `equate_clientauthid_clientid = true`
alongside it to also stop the agent needing its own `id` value — the
client-auth ID becomes the client ID, which simplifies fleet
provisioning at the cost of losing the credential/identity split.

## Disconnecting a compromised agent

1. Delete its credential row from the JSON file or the database
   (Client Access page in the SPA, or `DELETE /api/v1/clients-auth/<id>`).
2. The next reconnect attempt fails — the agent stays disconnected.
3. If the host is reachable, also stop `proxiport.service` and remove
   the stale binary + config so the credential is not re-issued by
   automation.

Rotating one credential out of many does not affect other agents.

## Upgrading to 0.8.5 — rotate every agent credential

**Affects 0.2.0 through 0.8.4 on the inline and `auth_file` stores.**
Reading a credential through the API blanked the stored copy, and the
blank then authenticated any agent that presented an empty password.

Reading `GET /api/v1/clients-auth` (which the **Client Access** page
issues when it loads) or `GET /api/v1/clients-auth/<id>` wrote an empty
password over the server's own copy of the credential. Two consequences
followed:

- **Agents stopped connecting.** Every reconnect failed against the
  blanked credential, so the fleet drained away with no error that
  pointed at the cause.
- **Anyone could connect.** A client-auth ID with a blank password
  accepted an empty password from an unauthenticated caller, admitting
  them as an agent for that ID. With
  `equate_clientauthid_clientid = true` they took over the client ID
  outright.

On `auth_file` the damage persisted: the successful empty login
triggered the at-rest hash upgrade, which wrote the hash of the empty
password back to the file over the real credential.

The `auth_table` (database) store is **not affected** — it builds a
fresh credential for each query, so nothing the API did could reach the
stored row.

If you ran an affected version with the inline or `auth_file` store,
treat every agent credential as having been reachable without
authentication:

1. Upgrade the server to 0.8.5 and restart it.
2. Issue a new password for every client-auth ID, keeping the IDs so
   the client rows survive (see
   [operator runbook — rotating credentials](operator-runbook.md#rotating-credentials)).
3. Update each agent's `[client] auth` and restart it.
4. If your `auth_file` contains an entry whose password is empty or is
   the bcrypt hash of an empty string, that credential was blanked in
   place — replace it rather than trying to recover it.

Nothing in the server records which credentials were read, so there is
no way to narrow this to a subset after the fact. Rotate all of them.


## Hardening checklist

- Use the JSON file or database mode. Avoid the inline credential
  beyond the initial install.
- Issue one credential per agent. Long random passwords (32+ chars).
- Pin `[server] key_seed` so the host-key fingerprint stays stable
  across server restarts; otherwise every agent fails the next
  fingerprint check. The seed *is* the host identity — store it
  encrypted so a copy of the config cannot be used to impersonate the
  server. See
  [encrypting the config secrets](operator-runbook.md#encrypting-the-config-secrets).
- Serve `proxiportd`'s client-listener over TLS — either built-in
  (`cert_file`/`key_file` or `enable_acme` under `[server]`) or
  behind a reverse proxy. See [HTTPS](https.md).
- Watch for repeated failed-auth log lines and ban offending IPs at
  the firewall. The server has a built-in `client_login_wait` and
  `max_failed_login` / `ban_time` pair but it is per-IP at the HTTP
  layer, not a substitute for fail2ban.

See also: [operator runbook — rotating credentials](operator-runbook.md#rotating-credentials)
and [install — agent](install.md#agent).
