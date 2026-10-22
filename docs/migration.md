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
   unit name change — but a few values need a look. `[api] doc_root` must
   point at ProxiPort's web UI (`/usr/share/proxiport/docroot` for a
   package install; an upstream value such as `/var/lib/rport/docroot`
   leaves the UI dark), and any `cert_file`, `key_file` or `auth_file`
   path that still names `/etc/rport` or `/var/lib/rport` must follow the
   files. The same goes for `[logging] log_file` and, if set,
   `[api] access_log_file`: an upstream value under `/var/log/rport` is
   read-only under the packaged systemd unit, which allows writes only to
   `/var/lib/proxiport` and `/var/log/proxiport`. `proxiportd` exits at
   startup if it cannot open its log file, so point them at
   `/var/log/proxiport/proxiportd.log` and
   `/var/log/proxiport/api-access.log`.
5. **Move the existing datastore into place.** The packaged unit only
   lets `proxiportd` write under `/var/lib/proxiport` and
   `/var/log/proxiport` (`ProtectSystem=strict`), so move the old
   directory there and hand it to the `proxiport` account (upstream ran
   as `rport`):
   ```sh
   sudo mv /var/lib/rport /var/lib/proxiport
   sudo chown -R proxiport:proxiport /var/lib/proxiport
   ```
   Make sure `[server] data_dir` in `/etc/proxiport/proxiportd.conf` is
   `/var/lib/proxiport`. If you must keep the data somewhere else, point
   `data_dir` at it and add that path with a systemd drop-in
   (`sudo systemctl edit proxiportd`, then under `[Service]` add
   `ReadWritePaths=/var/lib/rport`). Without the drop-in the server
   cannot write its databases there.
6. **Start the service.**
   ```sh
   sudo systemctl enable --now proxiportd
   sudo systemctl status proxiportd
   sudo tail -n 50 /var/log/proxiport/proxiportd.log
   ```
   Pending schema migrations run automatically on first start, and the
   server writes them to the file set by `[logging] log_file`, along
   with its host-key fingerprint. If the unit fails to start,
   `sudo journalctl -u proxiportd -n 50` shows why. That includes a
   config error or a `log_file` the `proxiport` account cannot write,
   such as one that still points at the old upstream log directory.

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
   sudo chown root:proxiport-agent /etc/proxiport/proxiport.conf
   sudo chmod 0640 /etc/proxiport/proxiport.conf
   ```
   The ProxiPort agent runs as its own account, `proxiport-agent`, so
   point `[client] data_dir` at a directory that account owns
   (`/var/lib/proxiport-agent` in the packages) rather than at the old
   `/var/lib/rport`.
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
  `auth_user_table`, `auth_group_table`, `auth_group_details_table`,
  `jwt_secret`, `doc_root`, `cert_file`, `key_file`, `access_log_file`,
  `user_login_wait`, `max_failed_login`, `ban_time`,
  `totp_enabled`, `totp_login_session_ttl`,
  `totp_account_name`. (`auth_group_details_table` is required for
  per-group permission editing in the database store; see
  [API authentication](api-authentication.md#database).)
- `[logging]`, `[database]`, `[caddy-integration]`, `[monitoring]`,
  `[notifications]`, `[pushover]`, `[smtp]` blocks — same option
  names as upstream. (There is no separate `[client-auth]` block —
  client-authentication options live under `[server]`: `auth`,
  `auth_file`, `auth_table`, `auth_multiuse_creds`,
  `equate_clientauthid_clientid`.)
- The schedule, command, script, library and user-auth tables: same
  schema, read as-is. The audit-log and vault tables are read as-is
  too, and ProxiPort extends them automatically on first start. The
  audit log gains `seq`, `mac` and `prev_mac` columns for its
  tamper-evidence chain, plus corrected indexes. The vault status table
  gains a `kdf` column and drops `dec_check`, and the vault is re-keyed
  to Argon2id on its next unlock. Because of these changes, rolling
  back to openrport needs the backup from step 2 (see
  [Rollback](#rollback)).

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
