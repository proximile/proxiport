# Client groups and permissions

ProxiPort has two related but distinct grouping models:

- **Client groups** label sets of *agents* (the managed hosts) so
  that operators can run commands, push files, or grant access
  against a meaningful set rather than enumerating client IDs every
  time.
- **User groups** label sets of *operators* (humans + API tokens)
  and carry the permissions that gate which API endpoints — and
  therefore which SPA sections — each operator can use.

These compose: a user has effective rights on an agent only when at
least one of their **user-group function permissions** is allowed
and at least one of their **client permissions** covers that agent.

For the user side of the auth story, see
[API authentication](api-authentication.md). For the agent-side
credentials, see [client authentication](client-authentication.md).

## Client groups

A client group is identified by an `id` string and is defined by a
set of `params`. Every connected agent is evaluated against the
`params` of every group; matching agents are listed in the group's
`client_ids` field at read time.

### Matching modes

Each `params` value can be either an exact-match list (case-insensitive)
or a list with wildcard glob patterns (also case-insensitive):

```json
{
  "params": {
    "client_id": ["alpha-prod", "bravo-staging"]
  }
}
```

```json
{
  "params": {
    "os_family": ["linux*", "*win*"]
  }
}
```

When multiple parameters are given, an agent is in the group only if
**all** of them match. Within one parameter that takes a list of
values on the agent side (`tags`, `ipv4`, `ipv6`, etc.), it is enough
for **one** value to match. Roughly: AND across keys, OR within each
key.

The explicit AND/OR form is also accepted on multi-value parameters:

```json
{
  "params": {
    "tags": { "and": ["Linux", "Datacenter-3"] }
  }
}
```

Means the agent must carry both tags. `or` works the same way and
matches if any one tag is present.

### Available parameter keys

- `client_id`
- `name`
- `os`, `os_arch`, `os_family`, `os_kernel`
- `hostname`
- `ipv4`, `ipv6`
- `tag` (single-dimension labels)
- `version`
- `address`
- `client_auth_id`

For 2-dimensional labels (e.g. `country=France`), use
[client attributes](client-attributes.md).

### Managing groups via the API

```bash
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

# Create
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{
    "id": "linux-edge",
    "description": "Linux hosts on the edge fleet",
    "params": {
      "os_family": ["linux*"],
      "tag": ["edge*"]
    }
  }' \
  https://proxiport.example.com/api/v1/client-groups

# Read
curl -s -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/client-groups/linux-edge | jq

# Update (full replacement; partial PATCH is not supported)
curl -s -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{ "id": "linux-edge", "description": "...", "params": { ... } }' \
  https://proxiport.example.com/api/v1/client-groups/linux-edge

# Delete
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/client-groups/linux-edge
```

The same CRUD is wired into the **Client Groups** page in the SPA.

### Where groups show up

- The **Commands** page accepts `group_ids` for fan-out — see
  [command execution](command-execution.md#multi-host-execution).
- The **Scripts** page accepts `group_ids` similarly.
- The file-push endpoint accepts repeated `group_id` form fields —
  see [tunnel hosting → file reception](tunnel-hosting.md#file-reception).
- The audit-log query builder lets you filter by group ID.
- User-group permissions can grant access to a client group as a
  unit (see below), so adding a host to the right group automatically
  gives the matching operators access.

## User groups and function permissions

Every API endpoint that does something interesting is gated by a
named **function permission**. A user-group record carries the set of
permissions it grants; a user is in zero or more groups; the
effective permission set is the **union** of their groups' grants.

There is no negative-permission mechanism. If `group-A` grants
`tunnels` and the user is also in `group-B`, you cannot use `group-B`
to revoke `tunnels`. Build the group set so that each grant is
intentional.

### The function permissions

- `tunnels` — open, close, list tunnels.
- `scripts` — run, store, list scripts.
- `commands` — run, store, list commands.
- `vault` — read, write, list vault items.
- `scheduler` — create, run, list schedules.
- `monitoring` — read monitoring metrics.
- `uploads` — push files to agents.
- `auditlog` — read the audit log.

The `Administrators` user group bypasses every check — its members
have every function and every client.

### Managing user groups

Permissions are stored in the `group_details` table (or its JSON-file
equivalent under `auth_file`). The
`PUT /api/v1/user-groups/<name>` endpoint takes a permission map:

```bash
curl -s -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{
    "name": "ops-readonly",
    "permissions": {
      "tunnels": false,
      "scripts": false,
      "commands": false,
      "vault": false,
      "scheduler": false,
      "monitoring": true,
      "uploads": false,
      "auditlog": true
    }
  }' \
  https://proxiport.example.com/api/v1/user-groups/ops-readonly
```

The same matrix is editable on the **User Groups** page in the SPA.

## Client permissions

A function permission grants the user the *ability* to call an
endpoint — but the endpoint also needs at least one agent the user
is allowed to address.

Two ways to grant per-agent access:

### Per-client ACL

The client record carries an ACL list (`details.acl` on the response,
managed via `POST /api/v1/clients/<id>/acl`). Set it to a list of
user-group names. Members of those groups can target that specific
agent.

Useful when one server hosts several customers, and access boundaries
match the tenant rather than any organisational category.

### Client-group permissions

Each client group can be associated with a list of user groups that
have access to all agents matching the group definition. Grant once,
extend automatically as new agents match the group's `params`. This
is the right shape for fleet-style operation.

Like function permissions, client permissions are **additive only**.
Granting access to client group A cannot revoke access to client
group B; if B is a subset of A, the user has access to B by virtue
of having access to A.

## Putting it together: a worked example

You operate two product lines, `apollo` and `bravo`, with separate
on-call teams. Both teams need to run scripts on their own hosts and
read monitoring data. The platform team needs full access.

1. **Tag the agents.** Each agent's
   [`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
   gets a `tags` entry under `[client]` (or use the
   [attributes file](client-attributes.md) if you'd rather manage tags
   from the API). For example, `tags = ["apollo", "linux"]`.

2. **Define two client groups.**

   ```json
   { "id": "apollo-fleet", "params": { "tag": ["apollo"] } }
   { "id": "bravo-fleet",  "params": { "tag": ["bravo"]  } }
   ```

3. **Define two user groups.**

   ```json
   {
     "name": "apollo-oncall",
     "permissions": {
       "scripts": true, "commands": true,
       "monitoring": true, "auditlog": true
     }
   }
   {
     "name": "bravo-oncall",
     "permissions": {
       "scripts": true, "commands": true,
       "monitoring": true, "auditlog": true
     }
   }
   ```

4. **Grant client-group access.** On `apollo-fleet`, allow user group
   `apollo-oncall`. On `bravo-fleet`, allow user group `bravo-oncall`.

5. **Add the operators.** Apollo on-call humans go into the
   `apollo-oncall` user group. Bravo on-call humans into
   `bravo-oncall`. Platform engineers go into `Administrators` and
   bypass the whole model.

Adding a new apollo host needs only step 1 — the tag binding causes
it to roll up into `apollo-fleet`, which the on-call group already
has access to. No per-host ACL maintenance.

See also: [client attributes](client-attributes.md) for the more
expressive 2-dimensional labelling model and
[operator runbook — user and group admin](operator-runbook.md#user-and-group-admin)
for the SPA workflow.
