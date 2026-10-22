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
set of `params`. Every known agent, connected or disconnected, is
evaluated against the `params` of every group. A disconnected agent
stays known until `keep_disconnected_clients` expires. Matching agents
are listed in the group's `client_ids` field at read time, limited to
the agents the requesting user has access to.

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
values on the agent side (`tag`, `ipv4`, `ipv6`, etc.), it is enough
for **one** value to match. Roughly: AND across keys, OR within each
key.

The explicit `and`/`or` object form is accepted on `tag` only; every
other key takes a plain list of values, matched as OR:

```json
{
  "params": {
    "tag": { "and": ["Linux", "Datacenter-3"] }
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
- `connection_state` (`connected` or `disconnected`)

For 2-dimensional labels (e.g. `country=France`), use
[client attributes](client-attributes.md).

### Managing groups via the API

Creating, updating and deleting client groups requires membership of
the `Administrators` user group; other users get an error on those
calls. Any authenticated user can read groups, but each group's
`client_ids` lists only the clients that user may access, and for a
user who is not an administrator the list call
(`GET /api/v1/client-groups`) also leaves out groups that contain none
of their clients.

A group `id` must be non-empty, at most 30 characters, and use only
`A-Z`, `a-z`, `0-9`, `_`, `-` and `*`. On `PUT`, the body's `id` must
equal the id in the URL, or the request is rejected with 400. `POST`
returns `201 Created`, while `PUT` and `DELETE` return
`204 No Content`; none of the three returns a body.

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
    },
    "allowed_user_groups": ["ops"]
  }' \
  https://proxiport.example.com/api/v1/client-groups

# Read
curl -s -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/client-groups/linux-edge | jq

# Update (full replacement; partial PATCH is not supported)
curl -s -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{ "id": "linux-edge", "description": "...", "params": { ... }, "allowed_user_groups": ["ops"] }' \
  https://proxiport.example.com/api/v1/client-groups/linux-edge

# Delete
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/client-groups/linux-edge
```

The **Client Groups** page in the SPA lists each group with its
description, client count and allowed user groups. It is read-only:
create, update and delete groups through the `/api/v1/client-groups`
API shown above.

### Where groups show up

- The multi-client command endpoint (`POST /api/v1/commands`) and the
  **Schedules** page accept `group_ids` for fan-out; the **Commands**
  page in the SPA targets clients individually. See
  [command execution](command-execution.md#multi-host-execution).
- The multi-host script endpoint, `POST /api/v1/scripts`, accepts
  `group_ids` the same way — see
  [scripts](scripts.md#multi-host-execution). The SPA's **Scripts**
  page does not have multi-target selection yet.
- The file-push endpoint accepts repeated `group_id` form fields —
  see [tunnel hosting → file reception](tunnel-hosting.md#file-reception).
- Creating, updating and deleting a group is recorded in the audit
  log under application `client.group`, with the group ID as
  `affected_id`. The audit log has no filter that scopes entries to a
  group's members; to filter by host, use `client_id` or
  `client_hostname`.
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
- `scheduler` — create, run, list schedules. A schedule runs a
  command or a script, so it also needs the matching `commands` or
  `scripts` permission (see below).
- `monitoring` — read monitoring data (graph metrics, metrics,
  processes, mountpoints) and trigger an OS-updates status refresh on
  an agent (`POST /api/v1/clients/<id>/updates-status`). The refresh
  trigger is available even when monitoring is disabled.
- `uploads` — push files to agents.
- `auditlog` — read the audit log.

The `Administrators` user group bypasses every check — its members
have every function and every client.

### Scheduling is not a way around `commands` and `scripts`

A schedule is a command or a script with a cron expression attached,
so it is gated by the same permission the equivalent direct call is:

| Schedule `type` | Permissions needed |
|---|---|
| `command` | `scheduler` **and** `commands` |
| `script`  | `scheduler` **and** `scripts` |

The check applies both when the schedule is created or repointed and
again each time the cron fires, so revoking `commands` or `scripts`
from a group stops the schedules its members have already stored —
you do not have to hunt down and delete each one. The account and its
client access are untouched, which makes this the right way to
withdraw execution from an operator while an investigation runs.

> **Upgrading from 0.9.x or earlier:** before 0.10.0, `scheduler`
> alone was enough to create and run a schedule of either type, which
> made it a superset of `commands` and `scripts`. If you have a group
> that grants `scheduler` without them, its members' schedules stop
> running once you upgrade to 0.10.0 or later. Add the permission the
> schedule's type needs, or delete the schedule. `Administrators` is
> unaffected, and so is any deployment whose auth backend has no group permissions at all (`auth`,
> `auth_file`): there is nothing there to check.

### Managing user groups

Permissions are stored in the database table named by
`[api] auth_group_details_table` (`group_details` in the example
config). Neither the JSON user file (`auth_file`) nor the single-user
`auth` mode can hold them: those stores grant every user every
permission, and reading, updating or deleting a single user group
returns `400` there (see [API authentication](api-authentication.md)). On the database store,
the `PUT /api/v1/user-groups/<name>` endpoint takes a permission map:

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

On the database store the same matrix is editable on the
**User Groups** page in the SPA.

## Client permissions

A function permission grants the user the *ability* to call an
endpoint — but the endpoint also needs at least one agent the user
is allowed to address.

Two ways to grant per-agent access:

### Per-client ACL

The client record carries an ACL: the `allowed_user_groups` field on
the client response (request it with `fields[clients]=allowed_user_groups`
if you limit the fields). An `Administrators` member sets it with
`POST /api/v1/clients/<id>/acl` and a body of
`{"allowed_user_groups": ["ops-readonly", "customer-a"]}`. Every name
must be an existing user group, or the request is rejected; a
successful call returns `204 No Content`. Members of the listed groups
can target that specific agent.

Useful when one server hosts several customers, and access boundaries
match the tenant rather than any organisational category.

### Client-group permissions

Each client group carries an `allowed_user_groups` array of
user-group names; members of those groups have access to every agent
matching the group's `params`. Set it in the same `POST`/`PUT` body as
`params`. `PUT` replaces the whole document, so include
`allowed_user_groups` on every update, or it is cleared. Grant once,
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
   This is the `allowed_user_groups` field of each client group (you
   can also set it when you create the group in step 2):

   ```json
   { "id": "apollo-fleet", "params": { "tag": ["apollo"] }, "allowed_user_groups": ["apollo-oncall"] }
   { "id": "bravo-fleet",  "params": { "tag": ["bravo"]  }, "allowed_user_groups": ["bravo-oncall"] }
   ```

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
