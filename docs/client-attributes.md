# Client attributes file format

ProxiPort lets you label each agent with two kinds of attribute:

- **Tags** — single-dimension labels, e.g. `["linux", "edge", "vm"]`.
- **Labels** — 2-dimensional key/value pairs, e.g.
  `{"country": "France", "city": "Lille", "datacenter": "OVH"}`.

Both are reported back to the server and can be filtered through
`/api/v1/clients?filter[tags]=...` / `?filter[labels]=...`. Tags can
also select agents into a [client group](client-groups-permissions.md)
through its `tag` param; labels cannot. Use them to slice the fleet by
environment, geography, role, or any other organisational dimension.

There are three places attributes can live. They are mutually
exclusive — when more than one is configured, the rules below decide
which wins.

## The three places attributes live

### 1. Tags and labels inline in `proxiport.conf`

The simplest setup. Under `[client]` in
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf):

```toml
[client]
  tags = ["linux", "edge", "vm"]

[client.labels]
  country = "France"
  city = "Lille"
```

Labels go in a `[client.labels]` table of string keys and string
values.

The values are read once at agent start. To change them, edit the
file and restart the agent.

### 2. A separate attributes file

The format ProxiPort recommends for anything beyond a couple of
unchanging tags. Point `attributes_file_path` at a JSON file:

```toml
[client]
  attributes_file_path = "/var/lib/proxiport-agent/client_attributes.json"
```

JSON example:

```json
{
  "tags": ["linux", "edge", "vm"],
  "labels": {
    "country": "France",
    "city": "Lille",
    "datacenter": "OVH"
  }
}
```

The file must be JSON, whatever its extension. If it cannot be read
or parsed, the agent prints `error reading attributes_file` and
`ignoring attributes_file`, then starts with the inline values from
`proxiport.conf`. On Windows, point the path at the location of your
choice, e.g.
`C:\Program Files\proxiport\client_attributes.json`.

This file is read **only at agent start**. Edit the file and restart
the agent for changes to take effect — unless you are managing it
through the API (see below), in which case the agent re-reads after
each successful API update.

### 3. Through the API

Once `attributes_file_path` is set on an agent, the
`PUT /api/v1/clients/<id>/attributes` endpoint accepts the entire
new attribute document and pushes it to the agent, which rewrites
its local file. `GET /api/v1/clients/<id>/attributes` returns the
server's current copy of the agent's `{tags, labels}`.

The SPA shows the current attributes, read-only, under the client's
**Meta-Data** tab (Inventory → client → Meta-Data; the vault must be
unlocked). Editing is through the API only.

Three preconditions:

- `attributes_file_path` must be set and point to a writable file.
- The file must be writable by the agent's OS user.
- The agent must be **currently connected**. Attributes are
  persisted on the agent only — the server has no offline buffer.
  An update against a disconnected agent is rejected.

If the agent is not connected, or is paused, both GET and PUT return
`404`. If the agent receives the update but cannot apply it (for
example `attributes file path not set` or `can't write attributes to
file`), PUT returns `409 Conflict` with the agent's error message. A
successful update returns `200`.

Partial updates are not supported. The PUT request must include the
entire `{tags, labels}` document; whatever you send replaces what
was there.

```bash
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

curl -s -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{
    "tags": ["linux","edge","vm"],
    "labels": {
      "country": "France",
      "city": "Lille",
      "datacenter": "OVH"
    }
  }' \
  https://proxiport.example.com/api/v1/clients/alpha-prod/attributes
```

## Precedence

If `attributes_file_path` is set and the file reads and decodes
successfully, its contents win: the inline `[client] tags` and
`[client.labels]` values are both replaced, even if the file leaves
one of them out. If the file cannot be read or decoded (it is
missing, has bad permissions, or is not valid JSON), the agent prints
`error reading attributes_file ...` and `ignoring attributes_file` to
stderr at startup and keeps the inline values. These messages go to
stderr, not the agent log file. To go back to the inline form,
comment out `attributes_file_path` and restart the agent.

The API only manages the file form. It cannot edit `[client] tags`
or `[client.labels]`.

## Filtering by attribute

The clients list endpoint accepts filters on both shapes:

```bash
# All agents tagged "server"
curl -s -H "Authorization: Bearer $TOKEN" \
  'https://proxiport.example.com/api/v1/clients?filter[tags]=server' | jq

# All agents whose city label equals "Lille"
curl -s -H "Authorization: Bearer $TOKEN" \
  'https://proxiport.example.com/api/v1/clients?filter[labels]=city:%20Lille' | jq
```

Wildcards are supported with `*`:

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  'https://proxiport.example.com/api/v1/clients?filter[tags]=ser*' | jq

curl -s -H "Authorization: Bearer $TOKEN" \
  'https://proxiport.example.com/api/v1/clients?filter[labels]=*:%20Lille' | jq
```

URL-encode the space as `%20`. The filter is case-insensitive, like
the [client-groups matcher](client-groups-permissions.md#matching-modes).

In a client-group definition only tags are usable, through the `tag`
param key (it matches both inline tags and attribute-file tags, with
the same wildcard syntax). Client-group `params` has no labels key,
and a group definition with a `labels` entry is rejected with HTTP 400
(`Invalid JSON data.`), so labels can be used only to
filter the `/clients` list.

## Choosing tags vs. labels

Use **tags** when the dimension is binary: the host is or is not in
that category. `linux`, `windows`, `vm`, `bare-metal`, `prod`,
`staging`. The matcher is set-membership.

Use **labels** when the value carries information of its own:
`country=France`, `datacenter=OVH`, `kernel=6.8`. The matcher is
key/value with wildcard glob.

A reasonable shape: tags for "is this in the set?" filters,
labels for "what's the value?" annotations. Either works for a small
fleet; both pay off as the fleet grows.

## File layout reference

The attributes file is a single JSON object with two
optional top-level keys, in any combination:

| Key | Type | Notes |
| --- | --- | --- |
| `tags` | list of strings | Case-preserved; matched case-insensitively. |
| `labels` | object of string→string | Both keys and values are case-preserved; matched case-insensitively. |

Both keys may be omitted (a file with neither is legal but pointless).
Extra top-level keys are ignored — safe to add a `comment` field for
operator notes without breaking parsing.

## Operational notes

- **Restart on file edit.** When you edit the file directly, the
  agent picks it up only on restart. API-driven updates are picked
  up immediately because the agent writes the file from its own
  process.
- **No server-side store.** Attributes live on the agent. A
  disconnected agent's attributes are unknown to the server until it
  reconnects. The server caches the last-known value for display,
  but updates must wait for the agent to come back.
- **Permissions.** The file must be writable by the agent's OS user
  for API updates to work, but readable by any other tooling you
  expect to consume it (e.g. a separate config-management runner).
- **Versioning.** If you check in the attributes file alongside the
  agent's configuration management, the API may overwrite it from
  the SPA. Pick one source of truth.

See also: [client groups and permissions](client-groups-permissions.md)
for how tags and labels map to access-control decisions,
[monitoring](monitoring.md) for the per-agent metrics each agent reports,
and the agent's
[`proxiport.example.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
for every other `[client]` setting.
