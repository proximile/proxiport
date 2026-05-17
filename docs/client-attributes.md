# Client attributes file format

ProxiPort lets you label each agent with two kinds of attribute:

- **Tags** — single-dimension labels, e.g. `["linux", "edge", "vm"]`.
- **Labels** — 2-dimensional key/value pairs, e.g.
  `{"country": "France", "city": "Lille", "datacenter": "OVH"}`.

Both are reported back to the server, indexable via the
[client groups](client-groups-permissions.md) `params` matcher, and
filterable through `/api/v1/clients?filter[tags]=...` /
`?filter[labels]=...`. Use them to slice the fleet by environment,
geography, role, or any other organisational dimension.

There are three places attributes can live. They are mutually
exclusive — when more than one is configured, the rules below decide
which wins.

## The three places attributes live

### 1. Tags inline in `proxiport.conf`

The simplest setup. Under `[client]` in
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf):

```toml
[client]
  tags = ["linux", "edge", "vm"]
```

Only tags are supported in this form. There is no `labels` key inside
`proxiport.conf` itself.

The values are read once at agent start. To change them, edit the
file and restart the agent.

### 2. A separate attributes file

The format ProxiPort recommends for anything beyond a couple of
unchanging tags. Point `attributes_file_path` at a JSON, YAML, or
TOML file:

```toml
[client]
  attributes_file_path = "/var/lib/proxiport/client_attributes.json"
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

YAML example:

```yaml
tags:
  - linux
  - edge
  - vm
labels:
  country: France
  city: Lille
  datacenter: OVH
```

TOML example:

```toml
tags = ["linux", "edge", "vm"]

[labels]
country = "France"
city = "Lille"
datacenter = "OVH"
```

The file extension determines the parser — `.json`, `.yaml`, `.yml`,
or `.toml`. On Windows, point the path at the location of your
choice, e.g.
`C:\Program Files\proxiport\client_attributes.json`.

This file is read **only at agent start**. Edit the file and restart
the agent for changes to take effect — unless you are managing it
through the API (see below), in which case the agent re-reads after
each successful API update.

### 3. Through the API or the SPA

Once `attributes_file_path` is set on an agent, the
`PUT /api/v1/clients/<id>/attributes` endpoint accepts the entire
new attribute document and pushes it to the agent, which rewrites
its local file. The SPA exposes the same operation from the
**Client detail → Attributes** panel.

Three preconditions:

- `attributes_file_path` must be set and point to a writable file.
- The file must be writable by the agent's OS user.
- The agent must be **currently connected**. Attributes are
  persisted on the agent only — the server has no offline buffer.
  An update against a disconnected agent is rejected.

Partial updates are not supported. The PUT request must include the
entire `{tags, labels}` document; whatever you send replaces what
was there.

```bash
TOKEN=$(curl -s -u admin:password \
  http://proxiport.example.com:3000/api/v1/login | jq -r .data.token)

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
  http://proxiport.example.com:3000/api/v1/clients/alpha-prod/attributes
```

## Precedence

If `attributes_file_path` is set, the attributes file wins. The
inline `[client] tags` value is ignored. To go back to the inline
form, comment out `attributes_file_path` and restart the agent.

The API only manages the file form — it cannot edit `[client] tags`.

## Filtering by attribute

The clients list endpoint accepts filters on both shapes:

```bash
# All agents tagged "server"
curl -s -H "Authorization: Bearer $TOKEN" \
  'http://proxiport.example.com:3000/api/v1/clients?filter[tags]=server' | jq

# All agents whose city label equals "Lille"
curl -s -H "Authorization: Bearer $TOKEN" \
  'http://proxiport.example.com:3000/api/v1/clients?filter[labels]=city:%20Lille' | jq
```

Wildcards are supported with `*`:

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  'http://proxiport.example.com:3000/api/v1/clients?filter[tags]=ser*' | jq

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://proxiport.example.com:3000/api/v1/clients?filter[labels]=*:%20Lille' | jq
```

URL-encode the space as `%20`. The filter is case-insensitive, like
the [client-groups matcher](client-groups-permissions.md#matching-modes).

The same selectors work as `params` keys in a client-group
definition: `tag` is the inline-and-attribute-file tag, and `labels`
becomes filterable via the same wildcard syntax.

## Choosing tags vs. labels

Use **tags** when the dimension is binary: the host is or is not in
that category. `linux`, `windows`, `vm`, `bare-metal`, `prod`,
`staging`. The matcher is set-membership.

Use **labels** when the value carries information of its own:
`country=France`, `datacenter=OVH`, `kernel=6.8`. The matcher is
key/value with wildcard glob; the SPA can group, sort, or colour by
the value.

A reasonable shape: tags for "is this in the set?" filters,
labels for "what's the value?" annotations. Either works for a small
fleet; both pay off as the fleet grows.

## File layout reference

The attributes file is a single JSON/YAML/TOML object with two
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
[monitoring](monitoring.md) for slicing the metrics view by attribute,
and the agent's
[`proxiport.example.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
for every other `[client]` setting.
