# Command execution

ProxiPort can dispatch a command to one or many connected agents, stream
the output back, and record the result. The command travels through the
existing chisel control channel — no tunnel is opened, and no extra
port on the agent host is exposed.

For multi-line bodies and stored, reusable invocations, use
[scripts](scripts.md) instead. The two share the same allow/deny
filter on the agent.

## How it works

1. The operator (SPA, REST API, or a custom client) `POST`s a command
   body to `/api/v1/clients/<id>/commands` (single host) or
   `/api/v1/commands` (multi-host).
2. The server checks the user's `commands` permission and the per-client
   ACL, then forwards the body to the agent.
3. The agent matches the command against its
   [`[remote-commands]` allow/deny lists](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
   and, if accepted, spawns the process under the OS account that runs
   `proxiport`.
4. The agent streams stdout/stderr back over the chisel channel; the
   server stores the result in `jobs.db` and pushes it to any open
   WebSocket subscribers.

The exit code, started/finished timestamps, PID, and the executing
user are all preserved on the job record.

## Single-host execution

```bash
CLIENT=alpha-prod
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

JOB=$(curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{"command":"uname -a","timeout_sec":10}' \
  "https://proxiport.example.com/api/v1/clients/$CLIENT/commands" \
  | jq -r .data.jid)

# Fetch the result once it completes
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://proxiport.example.com/api/v1/clients/$CLIENT/commands/$JOB" | jq
```

A representative response:

```json
{
  "data": {
    "jid": "f72b69fd-f418-40c3-ab62-4ce2c2022c58",
    "status": "successful",
    "client_id": "alpha-prod",
    "command": "uname -a",
    "cwd": "/home/proxiport",
    "is_sudo": false,
    "interpreter": "/bin/sh",
    "pid": 908526,
    "started_at": "2026-05-17T15:30:12.934Z",
    "finished_at": "2026-05-17T15:30:12.937Z",
    "created_by": "admin",
    "timeout_sec": 10,
    "result": {
      "stdout": "Linux alpha 6.8.0-111-generic ...\n",
      "stderr": ""
    }
  }
}
```

`timeout_sec` is the agent's supervision window. If the process exceeds
it, the job is marked `unknown` but the process is not killed — that is
intentional, because killing a long-running migration mid-step is
usually worse than letting it finish unsupervised.

### Job body fields

| Field | Type | Notes |
| --- | --- | --- |
| `command` | string (required) | The command line to execute. Must satisfy the agent's allow/deny filter. |
| `timeout_sec` | int | Agent supervision timeout, default 60. |
| `cwd` | string | Working directory on the agent. |
| `is_sudo` | bool | Prefix with `sudo -n` on Unix. Requires an NOPASSWD sudoers entry — see below. |
| `interpreter` | string | Override the default shell. Useful for `powershell` on Windows or a `[interpreter-aliases]` entry. |

## Multi-host execution

`POST /api/v1/commands` fan-outs a single command body to any combination
of client IDs and client-group IDs:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{
    "command": "/usr/bin/uptime",
    "client_ids": ["alpha-prod", "bravo-staging"],
    "group_ids": ["edge-fleet"],
    "execute_concurrently": true,
    "abort_on_error": false,
    "timeout_sec": 30
  }' \
  https://proxiport.example.com/api/v1/commands
```

The response carries one parent `jid` and one child job per target
host. Poll `/api/v1/commands/<jid>` to get the rolled-up status, or
subscribe to the WebSocket endpoint for live stdout.

Execution-flow flags:

`execute_concurrently`
: `false` by default — agents run in series, useful for migrations
  where one host must finish before the next starts. `true` fans the
  command out in parallel.

`abort_on_error`
: `true` by default in sequential mode — the first failure stops the
  rest. Ignored in concurrent mode. Set to `false` to keep going
  regardless.

For group IDs, define the group first via the
[client-groups API](client-groups-permissions.md).

## Authorising commands on the agent

Allowing arbitrary command execution from the API is not the default.
Each agent's
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
has a `[remote-commands]` block with an allow/deny filter:

```toml
[remote-commands]
  enabled = true
  allow = [
    '^/usr/bin/.*',
    '^/usr/local/bin/.*',
    '^C:\\Windows\\System32\\.*'
  ]
  deny = ['(\||<|>|;|,|\n|&)']
  order = ['allow', 'deny']
  send_back_limit = 4194304
```

The filter is applied to the **full command line as sent**. Two
practical consequences:

- The operator must invoke commands by absolute path. `uname -a` is
  not in `/usr/bin/.*` unless sent as `/usr/bin/uname -a`.
- Shell metacharacters are rejected by the default `deny` regex —
  pipes, redirects, `;`, `&&`, etc. Wrap multi-step work in a
  [script](scripts.md) instead.

### Order semantics

`order = ['allow', 'deny']` (the default):

1. The command must match at least one `allow` regex.
2. If it also matches a `deny` regex, it is rejected.
3. Otherwise it is allowed.

`order = ['deny', 'allow']`:

1. If the command matches a `deny` regex, it is rejected **unless**
   it also matches an `allow` regex.
2. Anything that matches no regex at all is allowed.

The first order is "default deny", the second is "default allow".
Use the first one unless you have a specific reason.

### Examples

Allow anything under the standard binary dirs and any `sudo -n`
invocation:

```toml
allow = [
  '^/usr/bin/.*',
  '^/usr/local/bin/.*',
  '^sudo -n .*'
]
```

Allow a single specific script:

```toml
allow = ['^/opt/site/maintenance.sh( |$)']
```

Allow PowerShell scripts under a fixed directory on Windows:

```toml
allow = [
  '^C:\\Windows\\System32\\.*',
  '^C:\\Users\\Administrator\\scripts\\.*\\.bat'
]
```

## Sudo / privileged commands

The agent runs as an unprivileged user on Linux and as the local
service account on Windows. To allow `is_sudo: true` invocations on
Linux, add an NOPASSWD sudoers rule scoped to exactly the commands
the agent should be able to elevate:

```text
# /etc/sudoers.d/proxiport
proxiport ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart nginx
proxiport ALL=(ALL) NOPASSWD: /usr/local/bin/maintenance.sh
```

Avoid a blanket `proxiport ALL=(ALL) NOPASSWD: ALL` — at that point
the allow/deny filter is the only thing standing between the API and
full root.

## Output size limits

The default `send_back_limit` is 4 MiB per stream. Anything beyond that
is truncated and the job result carries a `truncated` flag. Raise it
in `[remote-commands]` if you need full output, or write the output to
a file on the agent and pull it back via a separate path.

## API surface

| Endpoint | Purpose |
| --- | --- |
| `POST /api/v1/clients/<id>/commands` | Single-host command |
| `GET  /api/v1/clients/<id>/commands/<jid>` | Single-host result |
| `POST /api/v1/commands` | Multi-host fan-out |
| `GET  /api/v1/commands/<jid>` | Multi-host result |
| `GET  /api/v1/library/commands` | Stored command library |
| `WS   /ws/commands` | Live stdout/stderr stream |

See also: [scripts](scripts.md),
[client groups and permissions](client-groups-permissions.md),
[operator runbook — commands and scripts](operator-runbook.md#commands-and-scripts).
