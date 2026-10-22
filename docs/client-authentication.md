# Client authentication

The "client" in this page is the **agent** — the `proxiport` binary
running on a managed host. Each agent presents its credential as the
SSH password inside the chisel control channel it opens to
`proxiportd`, after checking the server's host key against its
configured `fingerprint`.
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

The server caches each credential it looks up for 60 minutes. A new ID
added to the file on disk is picked up by the next agent that connects.
A password changed or an entry removed by editing the file directly can
keep authenticating for up to an hour, until the cached entry expires or
the server is restarted. Changes made through the **Client Access** page
or the API take effect immediately.

If you would rather have an external tool own the file, set
`auth_write = false` and the API will reject every write attempt
(creating or deleting a credential) with HTTP 405 Method Not Allowed and
the error code `ERR_CODE_CLIENT_AUTH_RO`:

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
  server = "proxiport.example.com:80"
  auth   = "alpha-prod:ph2eePh1Bei4iezi"
  fingerprint = "SHA256:<server-host-key-fingerprint>"
```

The port must match proxiportd's `[server] address` (port 80 in the
example config). If
the agent listener sits behind a TLS reverse proxy on 443, write the
value with a scheme, `server = "https://proxiport.example.com"`, so the
agent connects over `wss://` rather than plain `ws://`.

The `fingerprint` value pins the server's host key. When it is set, the
agent refuses to talk to a server whose key does not match. `proxiportd`
logs two fingerprints on startup, `Fingerprint (SHA-256, pin this)` and
`Fingerprint (MD5, deprecated)`, and the SPA's Info page shows them too.
Pin the SHA-256 value (it starts with `SHA256:`), which the agent
matches in full. The legacy MD5 colon-hex form is still accepted but is
deprecated, and it is only prefix-matched, so a shortened MD5 pin
matches more than one key.

If `fingerprint` is left empty, the agent accepts whatever host key the
server presents and logs the fingerprint to pin. Set
`require_fingerprint = true` in `[client]` to make a missing pin a hard
error instead.

## Managing credentials via the API

The `/api/v1/clients-auth` endpoints list, fetch, create and delete
client credentials. Every route, the listing included, requires an
administrator account (a member of the `Administrators` user group);
other users and their API tokens are refused. Listing and fetching work
with any credential store. Create and delete also need `auth_file` or
`auth_table` to be active and `auth_write` to be true. The examples
below assume `admin` is an administrator:

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

# Delete (force=true also removes clients still bound to it)
curl -s -X DELETE -u admin:password \
  'https://proxiport.example.com/api/v1/clients-auth/delta-edge?force=true'
```

Without `?force=true`, a delete is refused with HTTP 409
(`ERR_CODE_CLIENT_AUTH_HAS_CLIENT`) while any client, connected or not,
is still bound to the credential.

The same operations are available on the **Client Access** page in the
SPA, which only administrators can use: add a credential with
**+ Add credential** and delete one from its row. Credentials cannot be
edited; to change a password, delete the credential and add it again.
The password is shown only when you create it and cannot be retrieved
later. If a pairing service is configured, the page then gives you Linux
and Windows installer one-liners to copy. If the pairing request fails,
it shows a `[client]` snippet to paste into the agent's config instead.

## Multi-use credentials and client identity

By default every agent that successfully authenticates presents its
own client ID. This is the `[client] id` setting in `proxiport.conf` if
one is configured. Otherwise it is the machine ID (`/etc/machine-id` on
Linux, the ComputerSystemProduct UUID on Windows), because
`use_system_id = true` is the default. If `use_system_id = false` and no
`id` is set, the server assigns a random ID for each connection, so the
ID (and the client's history) changes every time the agent restarts.
Two agents may share one client-auth credential as long as their IDs
differ.

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

1. Delete its credential from the **Client Access** page in the SPA
   (which applies force once you confirm the delete), or through the
   API with force:

    ```bash
    curl -s -X DELETE -u admin:password \
      "https://proxiport.example.com/api/v1/clients-auth/<id>?force=true"
    ```

    This closes the agent's live session, removes the client records
    bound to the credential, and deletes the credential.
2. The agent's reconnect attempts then fail, so it stays disconnected.
3. If the host is reachable, also stop `proxiport.service` and remove
   the stale binary + config so the credential is not re-issued by
   automation.

Removing the row by editing the JSON file or the database table by
hand does not drop a session that is already connected. With the file
store, a credential that was looked up recently can also keep
authenticating for up to 60 minutes (see [JSON credential file](#json-credential-file)).

Rotating one credential out of many does not affect other agents.

## Upgrading to 0.8.6 — the API served agent credentials

**Affects every version through 0.8.5, on all three credential
stores.**

An agent sends its whole configuration to the server when it connects,
and servers before 0.8.6 stored that configuration verbatim and served
it back through the REST API. The stored copy included the agent's own
`auth` credential and any proxy credential, so
`GET /api/v1/clients/<id>` returned an agent's connection password to
**any authenticated API user** — a read-only account included — for
every client that account could see, and
`GET /api/v1/clients?fields[clients]=client_configuration` returned it
for the whole visible fleet in one request. A reader could then connect
to the server as that agent.

0.8.6 stops the agent sending those fields at all, so nothing new is
stored, and the server deletes them from records written by older
versions the first time it starts. Backups taken before the upgrade
still contain them.

Treat every agent credential that existed on an affected server as
disclosed to every account that had API access, and rotate it:

1. Upgrade the server to 0.8.6 and restart it.
2. Issue a fresh credential per agent — see
   [managing credentials via the API](#managing-credentials-via-the-api).
3. Update each agent's `auth =` and restart it.
4. Delete the old credential rows.

The same applies to any credential written into an agent's `proxy`
setting, and to tunnel HTTP basic-auth passwords: `auth_password` was
returned by the same endpoints and is now redacted from the client
payload.

If you are also upgrading past the 0.8.5 advisory below, one rotation
covers both — do it once, after the upgrade to 0.8.6.

## Upgrading to 0.8.5 — reading a credential destroyed it

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

1. Upgrade the server to 0.8.5 or later and restart it.
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
- The client listener carries SSH inside the WebSocket, so the
  credential and everything after it are encrypted and the host key is
  checked against the agent's `fingerprint`. To wrap that port in TLS as well, put it behind a reverse
  proxy. The client listener has no TLS option of its own: the TLS keys
  under `[server]` (`tunnel_proxy_cert_file`, `tunnel_proxy_key_file`,
  `tunnel_enable_acme`, `tls_min`) apply only to the built-in tunnel
  proxy at `tunnel_host`, not to this port. See [HTTPS](https.md).
- Watch for repeated failed-auth log lines and ban offending IPs at
  the firewall. The server has two built-in throttles, and neither
  replaces fail2ban. `client_login_wait` (default 2 s) refuses further
  logins for a client auth ID for that long after a failed attempt; it
  is enforced in the SSH handshake per client auth ID, not per IP.
  `max_failed_login` / `ban_time` (defaults 5 / 3600 s) bans a source IP
  at the HTTP layer with status 423. It counts distinct failing
  credentials from that IP, so an agent that keeps retrying the same
  wrong credential does not ban itself.

See also: [operator runbook — rotating credentials](operator-runbook.md#rotating-credentials)
and [install — agent](install.md#agent).
