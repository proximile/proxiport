# Vault

The vault is ProxiPort's encrypted key-value store for documents and
per-client secrets. It is a separate SQLite database
(`{data_dir}/vault.sqlite.db` by default) sealed with a passphrase
that lives only in the server process's memory — never on disk. If a
`[key_provider]` is configured, every stored value is additionally
envelope-encrypted at rest under a server-held key — see
[At-rest encryption](#at-rest-encryption-optional).

A server restart **always re-locks the vault**, regardless of what is
stored inside. An operator with the passphrase has to unlock it again
through the SPA or the API before the values become readable.

## Lifecycle

1. **Initialize.** Once, the very first time the vault is used.
   Creates the database file and stores key-derivation parameters.
2. **Unlock.** Every time the server starts. The administrator
   provides the passphrase; the server derives a key, validates it
   against the stored marker, and keeps the derived key in memory.
3. **Read/write.** Any user with the `vault` function permission can
   list, fetch, store, update, and delete entries while the vault
   is unlocked.
4. **Lock.** Optional manual seal. Wipes the key from memory. The
   stored data stays on disk.

The administrator who runs unlock does not gain any per-entry
visibility beyond what the user-group permissions would otherwise
allow — locking is a global control, not a per-entry one.

## Admin API

The endpoints under `/api/v1/vault-admin` require Administrators group
membership.

### Initialize

```bash
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{"password":"a-vault-passphrase"}' \
  https://proxiport.example.com/api/v1/vault-admin/init
```

The passphrase must be between 4 and 32 bytes. Anything outside that
range is rejected.

### Status

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/vault-admin | jq
```

```json
{
  "data": {
    "init":   "setup-completed",
    "status": "unlocked"
  }
}
```

`init` is `setup-completed` or `uninitialized`. `status` is `unlocked`
or `locked`.

### Unlock and lock

```bash
# Unlock
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{"password":"a-vault-passphrase"}' \
  https://proxiport.example.com/api/v1/vault-admin/sesame

# Lock (DELETE on the same path)
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/vault-admin/sesame
```

If the wrong passphrase is supplied to unlock, the request is
rejected and the vault stays locked. If the passphrase is **lost**,
the data is unrecoverable — the passphrase is the only key that can
recover the contents; there is no backup escrow and no support
recovery path. (The optional [key provider](#at-rest-encryption-optional)
adds a second at-rest layer, but it is not a passphrase-recovery
mechanism — a configured server still requires the passphrase to unlock.)

## User API

Once the vault is unlocked, any user with the `vault` function
permission (see [client groups and permissions](client-groups-permissions.md))
can interact with `/api/v1/vault`.

### List

`GET /api/v1/vault` returns every entry's metadata — id, client_id,
created_by, created_at, key — but not the decrypted value:

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/vault | jq
```

Supports `sort=` (prefix with `-` for descending) and
`filter[<field>]=` on `id`, `client_id`, `created_by`, `created_at`,
and `key`. Pass `-g` to `curl` if you use the bracketed filter syntax
on the URL.

### Read

`GET /api/v1/vault/<id>` returns the full record including the
decrypted `value`:

```json
{
  "data": {
    "id": 1,
    "client_id": "alpha-prod",
    "required_group": "",
    "key": "deploy-key",
    "value": "ssh-ed25519 AAAA…",
    "type": "secret",
    "created_at": "2026-05-17T09:46:07+00:00",
    "updated_at": "2026-05-17T09:46:07+00:00",
    "created_by": "admin",
    "updated_by": "admin"
  }
}
```

### Create

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{
    "client_id": "alpha-prod",
    "required_group": "",
    "key": "deploy-key",
    "value": "ssh-ed25519 AAAA…",
    "type": "secret"
  }' \
  https://proxiport.example.com/api/v1/vault
```

### Update and delete

```bash
# Full replacement; partial PATCH is not supported
curl -s -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{ "client_id":"alpha-prod","required_group":"","key":"deploy-key","value":"...","type":"secret" }' \
  https://proxiport.example.com/api/v1/vault/1

# Delete
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/vault/1
```

### Field reference

`client_id`
: Optional. Ties a document to a specific agent. Empty (or `0`)
  means the entry is global and shows up on every client's
  Documentation tab.

`required_group`
: Optional. If set, only users in this user-group can read, update,
  or delete the entry. Use it for compartmentalised secrets in a
  multi-team server.

`key`
: Required short identifier. Unique within a `client_id` scope.

`value`
: Required. The encrypted body of the document. Every other column
  is stored in clear.

`type`
: Required. One of `text`, `secret`, `markdown`, `string`. The SPA
  uses the type to decide how to render the body — `secret` is
  masked behind a reveal toggle, `markdown` is rendered, etc.

## At-rest encryption (optional)

By default the vault contents are protected only by the passphrase
described above. If a `[key_provider]` is configured (see the `[key_provider]` section
of `proxiportd.example.conf`), the server adds a **second at-rest layer**: a
data-encryption key (DEK) held only in RAM wraps the vault's
verification marker and every stored value, so on disk they become
`enc:v1:…` ciphertext on top of the passphrase-derived encryption. An
existing vault is wrapped in place the next time the server boots.

This closes the offline-guessing path: with the DEK the attacker does
not have, a stolen copy of `vault.sqlite.db` cannot be used to test
passphrase guesses at all. The DEK is **not** a passphrase-recovery
mechanism — unlock still requires the passphrase — and a `type = file`
key stored on the same disk as the database gives no protection against
whole-disk theft.

## Backups

`vault.sqlite.db` belongs in the same backup schedule as the other
SQLite files (see
[operator runbook — backups](operator-runbook.md#backups)). The
encrypted file is safe to copy — the cipher text is useless without
the passphrase (and, if a `[key_provider]` is configured, without the
DEK as well). Back up the DEK/key file separately, and never inside the
same `data_dir` tarball as the database.

**Restoring a vault backup requires re-entering the passphrase on the
restored server.** The marker the new server checks against is part
of the database, so the same passphrase used at init time will
unlock the restored copy. If the backup was written with a
`[key_provider]` enabled, the restored server must also present the
**same DEK** (`key_file`/`env_var`); without it the vault fails closed
and cannot be read.

### Clear-text backups

Because losing the passphrase loses the data, consider also keeping
clear-text dumps of the vault outside the server. The dumps belong
in a safe (encrypted disk, password manager, hardware-backed store)
— never alongside the unencrypted database file.

A minimal export script:

```bash
USER=admin
TOKEN=e83d40e4-e237-43d6-bb99-35972ded631b
URL=https://proxiport.example.com/api/v1/vault
FOLDER=./vault-backup

mkdir -p "${FOLDER}"
IDS=$(curl -s -u "${USER}:${TOKEN}" "${URL}" | jq .data[].id)
for ID in $IDS; do
  curl -s -u "${USER}:${TOKEN}" "${URL}/${ID}" -o "${FOLDER}/${ID}.json"
done

tar czf vault-backup.tar.gz "${FOLDER}"

# Securely delete the per-entry files; keep only the tarball.
find "${FOLDER}" -type f -exec shred {} \;
rm -rf "${FOLDER}"
```

Encrypt `vault-backup.tar.gz` before storing it anywhere external.

## Operational notes

- **The vault is locked after every restart.** This is the most
  common operator surprise. Bookmark the unlock URL in the SPA, or
  script the unlock with a credential pulled from your password
  manager.
- **A wrong passphrase only fails — it does not damage the data.**
  Brute-forcing the passphrase is the only attack path against the
  data on disk — and a configured `[key_provider]` closes even that
  for a disk thief who lacks the DEK. Use a strong passphrase.
- **`required_group` is checked at every read.** Removing a user
  from the required group immediately revokes their access to the
  entries that name it.
- **The vault does not auto-lock on idle.** If you want a periodic
  lock, run a cron that POSTs `DELETE /api/v1/vault-admin/sesame`
  on the schedule you want.

## Hardening checklist

- Pick a passphrase that survives a brute-force attempt against the
  on-disk file — 32 random ASCII characters is a reasonable floor.
- Store the passphrase in a password manager, not in a shell history
  or a script committed to version control.
- Schedule regular clear-text backups and encrypt them at rest.
- Use `required_group` to restrict per-team secrets even from other
  vault users.

See also: [operator runbook — backups](operator-runbook.md#backups),
[architecture — datastore](architecture.md#datastore),
[client groups and permissions](client-groups-permissions.md) for the
user-group model the `vault` function permission and `required_group`
fields rely on.
