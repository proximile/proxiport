# Operator runbook

Short reference for keeping a ProxiPort server healthy. Not a full
admin guide — see [install](install.md) and [migration](migration.md)
for the first-run setup.

## Service control

```sh
# Server
sudo systemctl status proxiportd
sudo systemctl restart proxiportd
sudo journalctl -u proxiportd -f

# Agent
sudo systemctl status proxiport
sudo systemctl restart proxiport
sudo journalctl -u proxiport -f
```

## Paths

| Purpose | Path |
| --- | --- |
| Server config | `/etc/proxiport/proxiportd.conf` |
| Server data | `/var/lib/proxiport/` |
| Server SPA files | `/var/lib/proxiport/docroot/` |
| Server logs (default) | `/var/log/proxiport/proxiportd.log` (and journald) |
| Server PID file | `/run/proxiportd.pid` |
| Agent config | `/etc/proxiport/proxiport.conf` |
| Agent log | journald (`-u proxiport`) |

## Backups

The server keeps everything that matters in two places:

- **SQLite databases** under `/var/lib/proxiport/`. The set as of
  this writing: `clients.db`, `monitoring.db`, `library.db`,
  `auditlog.db`, `api_sessions.db`, `api_token.db`,
  `client_groups.db`, `jobs.db`, `notifications.db`, and the vault
  store at `vault.sqlite.db`. Each is created on demand the first
  time the corresponding feature is touched, so an early-life
  install may not have all of them on disk. Back the directory up
  with a stop-the-world snapshot or with the SQLite `.backup`
  command for hot backups.
- **`/etc/proxiport/proxiportd.conf`** — the config with the
  pinned `key_seed`, `jwt_secret`, and admin credentials.

User auth lives either in a JSON file (`[api] auth_file = "..."`)
or in a table inside the main database (`[api] auth_user_table =
"users"` plus a configured `[database]` connection), so back those
up alongside the SQLite files as appropriate.

Tarball both directories for the simplest backup. Restore by
stopping the service, replacing the directories, and starting again.

If you use MySQL instead of SQLite (`[database] db_type = "mysql"`
in proxiportd.conf), back up the database with your usual MySQL
tooling.

## Rotating credentials

- **Admin password.** Edit `/etc/proxiport/proxiportd.conf`,
  change the `[api] auth = "admin:<new-password>"` line, restart
  the service. Or, if running a multi-user setup with `auth_file`,
  edit that JSON file or the corresponding DB row.
- **JWT secret.** `[api] jwt_secret = "<long-random-string>"` —
  rotating this invalidates every issued session immediately. All
  users will be redirected to `/auth`.
- **`key_seed`.** Rotating the seed changes the server's SSH host
  key. Every connected agent will fail the fingerprint check
  until its `proxiport.conf` is updated. Avoid rotating unless the
  seed is compromised; coordinate with all agent operators.
- **client-auth credentials.** Used by agents to register. Change
  via `[server] auth = "<id>:<password>"` (single credential), or
  via the JSON file pointed at by `[server] auth_file`, or via the
  table named in `[server] auth_table`. Push the new credentials
  to each agent and restart it.

## Capacity and limits

- **Connected agents per server.** SQLite handles a few hundred
  agents comfortably on modest hardware. For more, switch to
  MySQL.
- **Tunnel ports.** `[server] used_ports` controls the pool of
  ports the server may allocate for tunnels. Default `20000-30000`.
  Expand if you run out.
- **Per-user session lifetime.** `?token-lifetime=<seconds>` on
  `/login`; defaults to 10 minutes; the SPA asks for 24 hours.

## Updating

1. Stop the service.
2. Replace the binary in `/usr/local/bin/proxiportd` (or wherever
   your package manager put it).
3. Start the service. Schema migrations, if any, run on first
   start.
4. Tail the logs to confirm.

For the agent, the same shape applies on each managed host.

## Common pitfalls

- **`429 too many requests` immediately after a 401.** ProxiPort
  inherits openrport's `BanList`: every failed auth attempt buys a
  2-second deny on the keyed username. If the SPA fires three
  parallel API calls after a session expires, the first 401s and
  the rest 429. The SPA fixes this client-side by short-circuiting
  on a missing token; if you are writing your own client, do the
  same.
- **Agent refuses to connect with "fingerprint mismatch".** Either
  the server's `key_seed` changed or the agent's
  `fingerprint = "..."` value is stale. Compare the fingerprint
  printed in the server log at startup against the agent config.
- **SPA shows the vault as locked even after entering the
  passphrase.** Vault passphrases are *not* persisted server-side
  — they live in memory for the life of the process. A server
  restart re-locks the vault. Enter the passphrase again via
  `Settings → Vault`.

## Where to get help

- File a bug or feature request on the GitHub repository.
- Private vulnerability reports go through GitHub Security
  Advisories — see [`SECURITY.md`](https://github.com/proximile/proxiport/blob/main/SECURITY.md).
