# API authentication

The ProxiPort REST API supports three credential flows for human users
and one for programmatic clients. They share a backing user store: a
single inline credential, a JSON file, or a database table.

This page covers how the API authenticates an HTTP request, how to
enable a TOTP second factor, and how to delegate authentication to a
reverse proxy. For agent (chisel) authentication see
[client authentication](client-authentication.md).

## Authentication flows

### HTTP basic with username + password

Every endpoint accepts a `Basic` `Authorization` header. The server
checks the credentials on each request:

```bash
curl -s -u admin:password \
  https://proxiport.example.com/api/v1/clients | jq
```

When any second factor is enabled (`[api] totp_enabled = true` or a
`two_fa_token_delivery` method), basic auth with a password stops
working on every endpoint except `/login`. The workaround for
script integrations is the personal API token below.

### JWT bearer token

`GET /api/v1/login` with basic auth returns a short-lived JWT. The
default lifetime is 10 minutes; request a longer one with
`?token-lifetime=<seconds>`, up to 90 days (`7776000`). A larger value,
or one that is not an integer, is rejected with HTTP 400. The SvelteKit
SPA asks for 24 hours.

```bash
TOKEN=$(curl -s -u admin:password \
  'https://proxiport.example.com/api/v1/login?token-lifetime=3600' \
  | jq -r .data.token)

curl -s -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/clients | jq
```

The JWT is HMAC-signed with `[api] jwt_secret` from
[`proxiportd.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf).
**Pin a long random value before exposing the API** — if the secret is
left unset, the server generates a fresh one at every restart and every
existing session is invalidated.

!!! warning "Login tokens are not bearer tokens"
    When a second factor is enabled, `/login` returns an intermediate
    token that is only accepted by `POST /verify-2fa` (and, for a user
    who has not enrolled a TOTP secret yet, by `POST /me/totp-secret`,
    so they can enroll). The final bearer token comes back from
    `/verify-2fa` once the TOTP or delivered code is validated.

### Personal API token

Each user can mint named, scoped API tokens. They authenticate via HTTP
basic with the **username + token in place of the password**, so they
work even when a second factor is enabled:

The token is the `<prefix>_<secret>` string returned by
`POST /api/v1/me/tokens` — use it verbatim (the leading `prefix_` is
required; a bare secret without it does not authenticate):

```bash
curl -s -u admin:Ab3xK9pq_e83d40e4-e237-43d6-bb99-35972ded631b \
  https://proxiport.example.com/api/v1/clients | jq
```

Each token has a scope and an expiry date. If you leave out
`expires_at`, the token expires one year after it is created. There are
three scopes:

- `read+write` — any request.
- `read` — `GET` requests only, and not `/ws`, `/ws-ticket`,
  `/me/totp-secret`, or individual vault values under `/vault/{id}`
  (the vault listing is still readable).
- `clients-auth` — only the `/api/v1/clients-auth` routes that manage
  agent credentials. Only an administrator can mint one; anyone else
  gets `400`.

Mint `read` or `read+write` tokens from **Admin → API Tokens** in the
SPA sidebar (also **API tokens** in the user menu at the top right).
Mint any scope, including `clients-auth`, via `POST /api/v1/me/tokens`.
List tokens with `GET /api/v1/me/tokens`, and rename one or change its
expiry with `PUT /api/v1/me/tokens/{prefix}` (body: `name`,
`expires_at`). Revoke one with `DELETE /api/v1/me/tokens/{prefix}`; it
stops authenticating on the next request. The token is a random secret
stored as a bcrypt hash and checked against the database on every
request, so once its row is deleted it no longer matches.

## User stores

Exactly one user store is active at a time. Combining the three modes
is rejected at startup.

### Inline single user

The simplest setup: pin one credential in
[`proxiportd.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf):

```toml
[api]
  auth = "admin:supersecret"
```

This mode has no SPA-managed user list, no multi-user support, and no
TOTP. It is useful for an initial install or a single-operator
deployment. Move to the JSON file or the database before sharing the
server.

### JSON user file

Point `[api] auth_file` at a writable JSON file:

```toml
[api]
  auth_file = "/var/lib/proxiport/api-auth.json"
```

The file is a list of users with bcrypt-hashed passwords:

```json
[
  {
    "username": "alice",
    "password": "$2y$10$ezwCZekHE/qxMb4g9n6rU.XIIdCnHnOo.q2wqqA8LyYf3ihonenmu",
    "groups": ["Administrators"]
  },
  {
    "username": "bob",
    "password": "$2y$10$ezwCZekHE/qxMb4g9n6rU.XIIdCnHnOo.q2wqqA8LyYf3ihonenmu",
    "groups": ["operators"],
    "two_fa_send_to": "bob@example.com",
    "totp_secret": ""
  }
]
```

Generate bcrypt hashes with `htpasswd -nbB '' 'your-password' | cut -d: -f2`.
The file is read on start and on `kill -SIGUSR1 <pid>` — edit it and
reload, or use the SPA to manage users (which writes the file in place).

The server needs read+write access to the file when the SPA is the
source of truth, so `chown proxiport /var/lib/proxiport/api-auth.json`
after creation.

### Database

To integrate with an existing identity store or to manage thousands of
users efficiently, point `[api]` at a set of database tables and
configure `[database]` with the connection:

```toml
[database]
  db_type = "sqlite"
  db_name = "/var/lib/proxiport/database.sqlite3"

[api]
  auth_user_table = "users"
  auth_group_table = "groups"
  auth_group_details_table = "group_details"
```

The schema for SQLite:

```sql
CREATE TABLE users (
  username TEXT NOT NULL,
  password TEXT NOT NULL,
  password_expired BOOLEAN NOT NULL DEFAULT 0,
  two_fa_send_to TEXT,
  totp_secret TEXT
);
CREATE UNIQUE INDEX users_username ON users (username);

CREATE TABLE groups (
  username TEXT NOT NULL,
  "group" TEXT NOT NULL
);
CREATE UNIQUE INDEX groups_username_group ON groups (username, "group");

CREATE TABLE group_details (
  name TEXT NOT NULL,
  permissions TEXT DEFAULT '{}',
  tunnels_restricted TEXT DEFAULT '{}',
  commands_restricted TEXT DEFAULT '{}'
);
CREATE UNIQUE INDEX group_details_name ON group_details (name);
```

`two_fa_send_to` and `totp_secret` are needed only when the matching
second factor is enabled. Personal API tokens are stored by the server
itself, not in these tables.

The MySQL equivalents use `VARCHAR` and `InnoDB`; see
[`proxiportd.example.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf)
for the `[database]` connection options.

Seed the first user:

```sql
INSERT INTO users (username, password)
  VALUES ('admin', '$2y$05$zfvuP4PvjsNWTqRFLdswEeRzETE2KiZONJQyVn7T3ZV5qcYAlmNWO');
INSERT INTO groups (username, "group") VALUES ('admin', 'Administrators');
```

## Managing users in the SPA

**Admin → Users** and **Admin → User Groups** in the SPA sidebar
(`/settings/users` and `/settings/user-groups`) manage the user
store from the web UI, backed by the same `POST`/`PUT`/`DELETE /users`
and `PUT`/`DELETE /user-groups` endpoints. What you can do depends on
which store is active:

| Store | Add / edit / delete users | Edit group permissions |
|---|---|---|
| Inline single user | — | — |
| JSON user file | Yes | — (every user is a full admin) |
| Database | Yes | Yes |

On the inline single-user store both pages are read-only: the API
rejects user and group writes, and the SPA shows the current account
alongside a note explaining why. Switch to the JSON file or the database
(above) to manage more than one operator.

### Users

**Admin → Users** lists every account. *New user* and *Edit* set the
username, password, and group membership; *Delete* removes the account;
and — when TOTP authenticator 2FA is enabled — *Reset 2FA* clears a
user's enrolled TOTP secret so they re-enroll on next login (out-of-band
email/push 2FA has no per-user secret to reset). Tick a group to
assign it, or type a new name to create one on the spot. Passwords must
meet the server's minimum length, and *Require password change* forces a
reset at next login. Out-of-band 2FA (email/push) also needs a delivery
address per user.

### User groups and permissions

A user's permissions are the **union of the permissions of its groups** —
there are no per-user grants. Each group toggles eight areas: tunnels,
commands, scripts, scheduler, monitoring, auditlog, uploads, and vault.
The built-in **Administrators** group grants all eight and cannot be
edited.

**Admin → User Groups** shows each group's permissions. In the
database store the checkboxes are editable and *Save* writes them back.
*Delete* (`DELETE /user-groups/{group_name}`) removes the group's
permission set and takes the group off every user who had it. The
built-in Administrators row has neither button. In the JSON-file
store the checkboxes are read-only, because that store grants
every user the full permission set. Create a group by assigning its name
to a user on the **Users** page, then set what it grants here.

!!! note "Group-permission editing needs the database store"
    Fine-grained per-group permissions require the
    `auth_group_details_table`. The JSON file has no place to store them,
    so file-store deployments treat every user as an administrator.

## Two-factor authentication

ProxiPort supports two second-factor flows. Both need users in the
JSON file or the database. With the inline single-user `auth` setting,
`two_fa_token_delivery` is refused at startup. `totp_enabled = true` is
accepted, but the inline user cannot be updated, so it can never enroll
and every login stops at the TOTP step. Move to `auth_file` or
`auth_user_table` before you turn on TOTP.

### TOTP authenticator app

The recommended setup. Set `[api] totp_enabled = true` and restart.
Each user is prompted to enroll on next login — the SPA renders a QR
code; scan it with any RFC 6238 app (Aegis, Google Authenticator,
1Password, etc.). The server generates the secret and a PNG of the QR
code and returns both once, in the enrollment response (`secret` and
base64 `qr`); the SPA displays that image along with the secret for
manual entry. The secret is then kept in the user store (database or
`auth_file`) and is not shown again.

When users live in the **database** (`auth_user_table`) and a
`[key_provider]` is configured, the `totp_secret` column is encrypted
at rest under the server DEK (`enc:v1:…` on disk), and any existing
plaintext secret is re-encrypted on the next boot. Secrets stored via
the JSON `auth_file` are not covered by this — that file is read as-is.

```toml
[api]
  totp_enabled = true
  totp_login_session_ttl = "600s"
  totp_account_name = "ProxiPort"
```

Run multiple servers with distinct `totp_account_name` values so the
authenticator app can tell them apart.

To enroll programmatically:

```bash
# Step 1: get a login token (cannot be used as a bearer token).
LOGIN_TOKEN=$(curl -s -u alice:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

# Step 2: create a TOTP secret (returns secret + base64 PNG of QR).
curl -s -X POST \
  -H "Authorization: Bearer $LOGIN_TOKEN" \
  https://proxiport.example.com/api/v1/me/totp-secret

# Step 3: validate a code from the app to finish enrollment.
curl -s -X POST \
  https://proxiport.example.com/api/v1/verify-2fa \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $LOGIN_TOKEN" \
  --data-raw '{"username":"alice","token":"123456"}'
```

!!! warning "The secret is returned once"
    Step 2 is the only response that carries the shared secret and the
    QR image. `GET /api/v1/me/totp-secret` reports enrollment status
    only — `{"enrolled": true}` for an enrolled account, or `404` when
    no secret exists yet — so a leaked session or a read-scoped
    API token cannot lift the secret and keep minting codes after the
    session is revoked. Capture the secret at enrollment time, or reset
    the factor and enroll again.

Removing your own factor with `DELETE /api/v1/me/totp-secret` requires
the account password in the request body:

```bash
curl -s -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{"old_password":"password"}' \
  https://proxiport.example.com/api/v1/me/totp-secret
```

Changing your own `two_fa_send_to` delivery address takes the same
`old_password` step-up, for the same reason: both are one step away
from moving the second factor to an attacker's device.

Admins can reset a user's TOTP from **Users** in the SPA, or by
`DELETE /api/v1/users/<username>/totp-secret`.

### Out-of-band code delivery

For sites that prefer email or push, set `two_fa_token_delivery` to
`smtp`, `pushover`, a URL, or the path to an executable (URL delivery
also needs `[api] base_url` set, or the server refuses to start). The
user's
`two_fa_send_to` field carries the destination address:

```toml
[api]
  two_fa_token_delivery = "smtp"
  two_fa_token_ttl_seconds = 600
```

Configure `[smtp]` or `[pushover]` to match, or set `base_url` for URL
delivery. The flow mirrors TOTP:
`/login` returns a login token, the server sends a code, and the user
posts `{username, token}` to `/verify-2fa` to get the final JWT.

## Login throttling and second-factor limits

Failed authentication is rate-limited on two axes. Both are on by
default; neither needs configuring to be effective.

**Per-IP.** `[api] max_failed_login` consecutive failures from one
source address ban that address for `ban_time` seconds. A successful
authentication clears the counter for that address.

**Per-account.** Each failed login also puts the *source IP and
username pair* on a short deny list for `[api] user_login_wait` seconds
(default 2). Further attempts at that account from that address get
`429 Too Many Requests` until the wait expires, which slows online
guessing against a single account. The key includes the address, so a
failed login from one host never locks the account out for users on
other hosts. Spraying one password across many accounts from a single
host is caught by the per-IP limit above.

```toml
[api]
  user_login_wait = 2.0
  max_failed_login = 10
  ban_time = 600
```

Second factors carry their own limits, independent of the settings
above:

- A pending login accepts at most **five** wrong second-factor codes.
  On the fifth the pending session is discarded and the user starts
  over from `/login` with their password.
- A TOTP code is accepted **once**. The ±1-step skew keeps a code
  usable for about 90 seconds, so a code that was shoulder-surfed or
  phished within that window would otherwise still work a second time;
  the server records the step it accepted and refuses that step and any
  earlier one.
- Out-of-band codes are **not re-sent** while the outstanding one still
  has more than 30 seconds to live. Repeating `/login` returns the same
  delivery target instead of mailing or pushing another code, so the
  endpoint cannot be used to flood a user's inbox or phone.

Sessions are invalidated when the password behind them changes —
whether the user changes it themselves, an admin changes it from
**Users**, or it is changed during login. Anyone still holding a bearer
token minted under the old password is logged out.

!!! note "Changing a password at login with 2FA on"
    `/login` accepts a `new_password` alongside the current one. When a
    second factor is enabled the new password is held and applied only
    after the factor is validated, so knowledge of the current password
    alone cannot change it.

## Delegated authentication

The server can treat any request that arrives with a configured header
as pre-authenticated. The reverse proxy decides whether to allow the
request; ProxiPort takes the username from a header value and issues a
JWT against the matching user record.

```toml
[api]
  auth_header = "Authentication-IsAuthenticated"
  user_header = "Authentication-User"
  create_missing_users = true
  default_user_group = "operators"
```

`user_header` defaults to `Authentication-User` and must not be set to
an empty value. `create_missing_users` is off by default. When it is
on, a username the server has not seen before gets a new account in
`default_user_group`, which defaults to `Administrators` — set it on
purpose, or every user the proxy lets through becomes a full
administrator. Delegated authentication needs a JSON user file
(`auth_file`) or a database user table (`auth_user_table`); the server
refuses to start if `auth_header` is combined with the inline
single-user `auth`.

The pre-auth flow still goes through `/login`, so the proxy needs to
inject the header on that endpoint. Once the SPA holds a JWT, every
subsequent call uses the bearer token and the headers are ignored.

!!! warning "Lock down the trust boundary"
    Anyone who can set `auth_header` on a request to the API server can
    impersonate any user. Bind ProxiPort to localhost (or to a
    dedicated network interface only the reverse proxy can reach) and
    refuse the header at the public edge.

## Command-line user management

The `proxiportd user` subcommand writes directly to whichever store
`[api]` points at, bypassing the API. Use it for break-glass password
resets when no admin can log in:

```bash
sudo -u proxiport proxiportd user change -u alice -p \
  -c /etc/proxiport/proxiportd.conf
```

Run it as the `proxiport` system user, not root, or the JSON file's
permissions will end up unreadable by the daemon.

With the JSON file store, the running daemon keeps its own copy of the
user list, so it does not see the change until you reload it:
`kill -SIGUSR1 <pid>`, or restart the service (on Windows, restart is
the only option). With the database store, the change takes effect
immediately.

## Hardening checklist

- Pin `[api] jwt_secret` to a long random value, and store it
  encrypted — anyone who reads it in the clear can forge admin
  sessions. See
  [encrypting the config secrets](operator-runbook.md#encrypting-the-config-secrets).
- Switch off the inline single-user mode as soon as you have more than
  one operator.
- Enable `totp_enabled = true` if you can.
- Leave `max_failed_login` and `ban_time` at their defaults or
  tighter — see [login throttling](#login-throttling-and-second-factor-limits).
- Sit the API behind TLS — see [HTTPS](https.md).
- Restrict the username/password basic-auth flow at the reverse proxy
  if you only intend to allow bearer tokens.
- Review changes on the **Audit** page in the SPA. Most administrative
  changes are recorded there: users, groups, API tokens and TOTP,
  session revocation, client deletion, client ACLs, client credentials,
  client groups, tunnels, commands, scripts, the library, the vault, schedules and
  uploads. Logins, logouts and 2FA verification are not recorded, and
  neither are changes to stored tunnels or client attributes, or
  pairing requests. To review a user's active API sessions, use
  `GET /api/v1/users/{user_id}/sessions` (admin only).

See also: [operator runbook — rotating credentials](operator-runbook.md#rotating-credentials).
