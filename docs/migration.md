# Migrating from rport or openrport

ProxiPort is config-format-compatible with upstream rport and
openrport. The migration is a stop / replace / restart, not a
schema rebuild.

## Server

1. **Stop the upstream service.**
   ```sh
   sudo systemctl stop rportd   # or: openrportd
   ```
2. **Back up the existing state.** The datastore lives in
   `/var/lib/rport/` (or `/var/lib/openrport/`):
   ```sh
   sudo tar -czf rport-backup-$(date +%F).tar.gz \
     /etc/rport /var/lib/rport
   ```
3. **Install ProxiPort server packages.** See
   [install](install.md).
4. **Copy the config across.**
   ```sh
   sudo cp /etc/rport/rportd.conf /etc/proxiport/proxiportd.conf
   ```
   The option names are the same. Only the file path and the systemd
   unit name change.
5. **Point ProxiPort at the existing datastore.** Edit
   `/etc/proxiport/proxiportd.conf` so the `[server] data_dir`
   matches your old `/var/lib/rport` location, or move the
   directory:
   ```sh
   sudo mv /var/lib/rport /var/lib/proxiport
   ```
6. **Start the service.**
   ```sh
   sudo systemctl enable --now proxiportd
   sudo journalctl -u proxiportd -n 50
   ```
   Pending schema migrations run automatically on first start.

   Existing agents reconnect against the same client-auth credentials
   and the same host-key fingerprint, so the inventory comes back
   populated without any per-agent touch:

   ![Inventory after the server-side migration — every agent that
   was already paired reconnects on its own.](screenshots/01-inventory-dashboard.png)
7. **Disable the old unit so it does not race ProxiPort on reboot.**
   ```sh
   sudo systemctl disable rportd
   ```

## Agents

Each agent needs the binary swapped and the config relocated. The
control protocol is unchanged — agents register against the same
client-auth credentials.

1. **Stop the upstream agent.**
   ```sh
   sudo systemctl stop rport   # or: openrport
   ```
2. **Install the ProxiPort agent package** (see
   [install](install.md)).
3. **Move the config.**
   ```sh
   sudo cp /etc/rport/rport.conf /etc/proxiport/proxiport.conf
   ```
4. **Re-issue the host-key fingerprint if the server's host key
   changed.** If you reused the same `key_seed` in
   `proxiportd.conf`, the fingerprint is unchanged — keep
   `fingerprint = "..."` as it was. If the server now has a different
   key, copy the new fingerprint from
   `/var/log/proxiport/proxiportd.log` on first start.
5. **Start.**
   ```sh
   sudo systemctl enable --now proxiport
   ```

## What is config-compatible

- `[server]` block options: `address`, `key_seed`, `auth_file`,
  `data_dir`, `keep_lost_clients`, `tunnel_*`, `used_ports`,
  `excluded_ports`.
- `[api]` block options: `address`, `auth`, `auth_file`,
  `auth_user_table`, `auth_group_table`, `jwt_secret`,
  `doc_root`, `cert_file`, `key_file`, `access_log_file`,
  `user_login_wait`, `max_failed_login`, `ban_time`,
  `totp_enabled`, `totp_login_session_ttl`,
  `totp_account_name`.
- `[logging]`, `[database]`, `[caddy-integration]`, `[monitoring]`,
  `[notifications]`, `[pushover]`, `[smtp]` blocks — same option
  names as upstream. (There is no separate `[client-auth]` block —
  client-authentication options live under `[server]`: `auth`,
  `auth_file`, `auth_table`, `auth_multiuse_creds`,
  `equate_clientauthid_clientid`.)
- The schedule, command, script, library, audit-log, vault, and
  user-auth tables — same schema, no migration needed.

## What is intentionally not migrated

- **Anything `plus/` produced.** OAuth/OIDC sessions issued via the
  upstream Plus plugin will not work after migration. Affected users
  will need to log in via local password + (optional) TOTP, which is
  the AGPL-supported path.
- **Alerting state.** The upstream alerting feature was Plus-gated.
  Alert definitions in the database are ignored. They will start
  working again when alerting is reimplemented in the open.
- **The Vue/Nuxt frontend.** Users do not need to migrate anything —
  the SvelteKit SPA is served by the new server out of the box.

## Rollback

If the migration fails and you need to roll back:

```sh
sudo systemctl stop proxiportd
sudo systemctl enable --now rportd
```

The datastore is forwards-compatible from openrport, so rolling back
in the other direction (after ProxiPort ran migrations on the schema)
may not work cleanly. Restore from the backup taken in step 2
above.
