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

When the TOTP second factor is enabled, basic auth with a password
stops working on every endpoint except `/login`. The workaround for
script integrations is the personal API token below.

### JWT bearer token

`POST /api/v1/login` with basic auth returns a short-lived JWT. The
default lifetime is 10 minutes; request a longer one with
`?token-lifetime=<seconds>`. The SvelteKit SPA asks for 24 hours.

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
    When TOTP is enabled, `/login` returns an intermediate token that
    is only accepted by `/verify-2fa`. The final bearer token comes
    back from `/verify-2fa` once the TOTP code is validated.

### Personal API token

Each user can mint named, scoped API tokens. They authenticate via HTTP
basic with the **username + token in place of the password**, so they
work even when TOTP is enabled on the account:

```bash
curl -s -u admin:e83d40e4-e237-43d6-bb99-35972ded631b \
  https://proxiport.example.com/api/v1/clients | jq
```

Tokens carry an expiry date and a scope (`read`, `read+write`). Mint
them from **Settings → API Tokens** in the SPA, or via
`POST /api/v1/me/token`. Revoke them by deleting the row — the
underlying JWT becomes unverifiable immediately.

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
  token TEXT DEFAULT NULL,
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

The MySQL equivalents use `VARCHAR` and `InnoDB`; see
[`proxiportd.example.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf)
for the `[database]` connection options.

Seed the first user:

```sql
INSERT INTO users (username, password)
  VALUES ('admin', '$2y$05$zfvuP4PvjsNWTqRFLdswEeRzETE2KiZONJQyVn7T3ZV5qcYAlmNWO');
INSERT INTO groups (username, "group") VALUES ('admin', 'Administrators');
```

## Two-factor authentication

ProxiPort supports two second-factor flows. Both require the JSON file
or the database — the inline single-user mode cannot enable 2FA.

### TOTP authenticator app

The recommended setup. Set `[api] totp_enabled = true` and restart.
Each user is prompted to enroll on next login — the SPA renders a QR
code; scan it with any RFC 6238 app (Aegis, Google Authenticator,
1Password, etc.). The secret stays in the database; the QR is
rendered client-side.

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

Admins can reset a user's TOTP from **Users** in the SPA, or by
`DELETE /api/v1/users/<username>/totp-secret`.

### Out-of-band code delivery

For sites that prefer email or push, set `two_fa_token_delivery` to
`smtp`, `pushover`, a URL, or the path to an executable. The user's
`two_fa_send_to` field carries the destination address:

```toml
[api]
  two_fa_token_delivery = "smtp"
  two_fa_token_ttl_seconds = 600
```

Configure `[smtp]` or `[pushover]` to match. The flow mirrors TOTP:
`/login` returns a login token, the server sends a code, and the user
posts `{username, token}` to `/verify-2fa` to get the final JWT.

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

## Hardening checklist

- Pin `[api] jwt_secret` to a long random value, and store it
  encrypted — anyone who reads it in the clear can forge admin
  sessions. See
  [encrypting the config secrets](operator-runbook.md#encrypting-the-config-secrets).
- Switch off the inline single-user mode as soon as you have more than
  one operator.
- Enable `totp_enabled = true` if you can.
- Sit the API behind TLS — see [HTTPS](https.md).
- Restrict the username/password basic-auth flow at the reverse proxy
  if you only intend to allow bearer tokens.
- Audit access from **Audit log** in the SPA; every state-changing
  call is recorded.

See also: [operator runbook — rotating credentials](operator-runbook.md#rotating-credentials).
