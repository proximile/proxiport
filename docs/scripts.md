# Scripts

Scripts are the multi-line cousin of [commands](command-execution.md).
Where a command sends a single line for the agent's shell to evaluate,
a script sends a body of code plus an interpreter — the agent writes
the body to a tempfile, runs the interpreter against it, and reaps
the process.

This lets you ship a bash heredoc, a Python script, or a PowerShell
function to one or many agents and stream the output back, without
keeping the script on disk on every host.

## How it works

1. The operator `POST`s the **base64-encoded** script body to
   `/api/v1/clients/<id>/scripts` along with an interpreter and the
   usual command options.
2. The server forwards the body to the agent via the chisel channel.
3. The agent decodes the body, writes it to a random tempfile under
   `{data_dir}/scripts/`, and invokes the interpreter against the file.
4. stdout/stderr stream back over the same channel.
5. The agent deletes the tempfile when it stops supervising the job,
   whatever the exit code. That happens when the process exits or when
   `timeout_sec` elapses, whichever comes first. A script still running
   at the timeout is not killed: the job is reported with status
   `unknown`, and the process keeps running after its tempfile is gone.

The end-to-end model is the same as command execution; the only
differences are that scripts can hold multiple lines and that the
agent's `[remote-scripts]` switch must also be enabled (in addition
to `[remote-commands]`).

## Enabling script execution on the agent

```toml
[remote-commands]
  enabled = true

[remote-scripts]
  enabled = true
```

Script execution requires command execution. If
`[remote-scripts] enabled = true` while
`[remote-commands] enabled = false`, the agent refuses to start and
logs `config validation failed: remote scripts execution requires
remote commands to be enabled`. Enable both switches, or leave
`[remote-scripts]` disabled.

!!! warning "The `allow`/`deny` filter does not apply to scripts"

    A script body and its interpreter are **not** matched against
    `[remote-commands] allow` or `deny`. The filter applies to the
    command path only. A script job runs the interpreter the operator
    chose, with the body the operator sent, and the only agent-side
    control over that is `[remote-scripts] enabled`.

    So narrowing `allow` does not narrow what scripts can do. If an
    operator should not be able to run arbitrary code on a host, do not
    give them the `scripts` permission, and leave
    `[remote-scripts] enabled = false` on that agent.

    Earlier releases of this page said the opposite. It was wrong: the
    agent has never applied the filter to script jobs.

## The scripts directory

The agent writes each job to `{data_dir}/scripts/` and then runs it.
The path is **not** configurable — it is `data_dir` with `scripts`
appended — and two properties of that directory decide whether jobs
run at all. Ordinary commands are written there as well as scripts, so
the directory has to be right even if you never run scripts.

**It must have mode exactly `0700` and be owned by the agent's own
account.** Anything written there is executed as the agent, so a
scripts directory other accounts can reach is a way to run code as
that account. Every job checks this before it runs, and while the mode
is not exactly `0700` (stricter modes like `0500` fail too) or the
owner is a different account, every command and script job fails
with:

```
scripts directory /var/lib/proxiport-agent/scripts must be read-writable
only by proxiport-agent[997]. Change directory mode from 0755 to 0700.
Your setup is insecure
```

When `[remote-scripts]` is enabled the agent also runs this check at
startup, but there it only logs the error and keeps running; jobs keep
failing until the directory is fixed.

You normally never see this. When the directory is missing, the agent
creates it with mode `0700` at startup, wherever `data_dir` points, and
the pairing installer also creates it at `0700`. The error appears only
when the directory already exists with another mode, for example one
created by hand with a plain `mkdir` or changed later. Fix it with
`chmod 0700` on the directory, or remove the directory and let the
agent recreate it.

**Its filesystem must allow execution for shebang scripts.** A body
that starts with `#!` is executed directly from the file the agent
writes, so on a `data_dir` mount carrying `noexec` — common for `/var`
or `/tmp` on a hardened host, and the default for a Docker `tmpfs` —
those script jobs fail with:

```
failed to start a command: fork/exec
/var/lib/proxiport-agent/scripts/<uuid>: permission denied
```

A body without a shebang is run as `<interpreter> <file>` (`/bin/sh`
when no interpreter is given) and still works, because reading a file
needs no execute bit. Ordinary *commands* run the same way. If shebang
scripts fail this way while commands and shebang-less scripts do not,
check the mount options on `data_dir`.

## Running a script

Bodies are base64-encoded because the JSON payload would otherwise
break on embedded newlines, quotes, and special characters:

```bash
SCRIPT=$(printf '#!/bin/bash\nset -euo pipefail\nuptime\nfree -h\n' | base64 -w0)
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)
CLIENT=alpha-prod

JOB=$(curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw "{
    \"script\": \"$SCRIPT\",
    \"interpreter\": \"/bin/bash\",
    \"timeout_sec\": 30
  }" \
  "https://proxiport.example.com/api/v1/clients/$CLIENT/scripts" \
  | jq -r .data.jid)

curl -s -H "Authorization: Bearer $TOKEN" \
  "https://proxiport.example.com/api/v1/clients/$CLIENT/commands/$JOB" | jq
```

Note that the **result** is fetched from the commands endpoint, not a
separate scripts endpoint. Scripts are commands under the hood.

### Request fields

| Field | Type | Notes |
| --- | --- | --- |
| `script` | string (required) | Base64-encoded body. |
| `interpreter` | string | Default `/bin/sh` on Unix, `cmd` on Windows. See aliases below. |
| `cwd` | string | Working directory on the agent. |
| `is_sudo` | bool | Prefix with `sudo -n` on Unix. |
| `timeout_sec` | int | Agent-side supervision timeout. |

## Multi-host execution

`POST /api/v1/scripts` fans a single script out to many agents:

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw "{
    \"script\": \"$SCRIPT\",
    \"interpreter\": \"/bin/bash\",
    \"client_ids\": [\"alpha-prod\",\"bravo-staging\"],
    \"group_ids\": [\"edge-fleet\"],
    \"execute_concurrently\": true,
    \"abort_on_error\": false,
    \"timeout_sec\": 60
  }" \
  https://proxiport.example.com/api/v1/scripts
```

The response carries the parent `jid`. There is no scripts read
endpoint: multi-host script jobs share the commands job store. Poll
`GET /api/v1/commands/<jid>` for the rolled-up status and
`GET /api/v1/commands/<jid>/jobs` for the per-host results. Both
routes need the `commands` permission, and only the user who started
the job or an admin can read it.

Concurrency and abort-on-error semantics are the same as
[command fan-out](command-execution.md#multi-host-execution).

## Interpreters and aliases

The server can ship any interpreter the agent can locate. Common
shapes:

- **Bash / sh / zsh** — `/bin/bash`, `/bin/sh`, `/usr/local/bin/zsh`.
  The agent invokes the interpreter against the tempfile path.

    On Linux and other non-Windows agents, if the script body begins
    with `#!`, the agent runs the tempfile directly and the shebang line
    picks the interpreter. The `interpreter` field is then ignored
    (`sudo -n` is still put in front when sudo is requested). On Windows
    the `interpreter` field is always used.

- **Python** — `/usr/bin/python3`. Use `#!/usr/bin/env python3` in
  the body if you want it to work across hosts with different paths
  (see the shebang note above).
- **PowerShell** — `powershell` (Windows PowerShell 5.x) or a full
  path like `C:\Program Files\PowerShell\7\pwsh.exe`. ProxiPort
  appends `-Noninteractive -executionpolicy bypass -File` and writes
  the tempfile as `.ps1` when the resolved interpreter path contains
  `powershell` (case-insensitive) after alias expansion. That covers
  `powershell` and the default PowerShell 7 install path, which matches
  because of its `PowerShell` directory, not because of `pwsh.exe`. A
  `pwsh.exe` installed somewhere whose path does not contain
  `powershell` (for example `C:\tools\pwsh\pwsh.exe`, or bare `pwsh`)
  is not recognised as PowerShell: it gets no flags and its script is
  written as a `.bat` file.
- **CMD** — `cmd` on Windows. The agent writes a `.bat` tempfile.

For brevity, define aliases in the agent's
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
under `[interpreter-aliases]`:

```toml
[interpreter-aliases]
  pwsh7 = 'C:\Program Files\PowerShell\7\pwsh.exe'
  bash  = 'C:\Program Files\Git\bin\bash.exe'
```

Now `"interpreter": "pwsh7"` in the API call resolves to the full path.

## The library

The **Saved scripts** tab of the SPA's **Library** page (sidebar →
**Library**) is a convenience wrapper around `/api/v1/library/scripts`.
(The separate sidebar **Scripts** item is not the library.) A library
entry stores the same fields as an ad-hoc request (name, interpreter,
sudo flag, cwd, timeout and the body) plus an optional list of `tags`.
If `timeout_sec` is omitted or 0, the server stores 60. The tab
creates, edits and deletes entries; it does not run them. To run one,
send its body as an ad-hoc script.

A [schedule](operator-runbook.md#schedules) is not linked to a library
entry. It stores its own copy of the command or script body and its
run options. You can paste a library entry's body into a schedule, but
later edits to the entry do not change the schedule.

Manage library entries via the API:

```bash
# Create
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{
    "name":"current_directory",
    "interpreter":"/bin/sh",
    "is_sudo":false,
    "cwd":"/root",
    "script":"pwd",
    "tags":["maintenance"],
    "timeout_sec":60
  }' \
  https://proxiport.example.com/api/v1/library/scripts

# List, sort by newest first
curl -s -H "Authorization: Bearer $TOKEN" \
  'https://proxiport.example.com/api/v1/library/scripts?sort=-created_at' \
  | jq

# Update (full replacement; partial updates are not supported)
curl -s -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  --data-raw '{ "name":"...", "interpreter":"...", "is_sudo":false, "cwd":"...", "script":"...", "tags":[...], "timeout_sec":... }' \
  https://proxiport.example.com/api/v1/library/scripts/<script-id>

# Delete
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/library/scripts/<script-id>
```

The list endpoint accepts `sort=` and `filter[<field>]=` on `id`,
`name`, `created_by`, `created_at`, `updated_by`, `updated_at`,
`interpreter`, `is_sudo`, `cwd`, `script` and `tags`. Use
`fields[scripts]=` to limit the returned fields; it accepts the same
names plus `timeout_sec`. Results are paginated with `page[limit]`
(default 20, maximum 100) and `page[offset]`.

## WebSocket execution

For live stdout, the server exposes `WS /api/v1/ws/scripts`. Send one
JSON message using the multi-host body of `POST /api/v1/scripts`
(`script`, `interpreter`, and at least one of `client_ids`, `group_ids`
or `tags`, plus optional `cwd`, `is_sudo`, `timeout_sec`,
`execute_concurrently`, `abort_on_error`). There is no per-client
socket, so target a single host with `client_ids: ["<id>"]`. As output
is produced, the server sends frames of the form
`{"jid", "client_id", "client_name", "result": {"stdout", "stderr", "summary"}}`,
with the new chunk in `result.stdout` or `result.stderr` and the other
fields empty. When each client's job finishes, it sends that job's full
JSON.

Use this path from your own clients or the test harness below when you
want output as it is produced. The SPA does not use it: it starts the
script with `POST /clients/{id}/scripts` and polls
`GET /clients/{id}/commands/{jid}`, showing the output once the job
finishes.

To enable the test harness UI at `/api/v1/test/scripts/ui`, set
`enable_ws_test_endpoints = true` in `[api]`. The test UI is the
easiest way to confirm a script body, interpreter, and target host
combination before wiring it into the SPA library or a scheduled job.

## Security notes

- Scripts run as the agent's OS account (`proxiport-agent` on a
  packaged install, not the server's `proxiport` account). With
  `is_sudo: true`, the agent writes the body to
  `{data_dir}/scripts/<random-uuid>` and runs
  `sudo -n <interpreter> <that file>`, or `sudo -n <that file>` when
  the body starts with a shebang. The file name changes on every run,
  so a sudoers rule cannot name one particular script. A NOPASSWD rule
  for `<interpreter> {data_dir}/scripts/*` lets every script the server
  sends run as root through that interpreter. Grant it only if you
  accept that, and limit who may send sudo scripts on the server side.
- The body lands on disk briefly under `{data_dir}/scripts/`. If
  the host has FIM (file-integrity monitoring), expect events from
  that directory during script execution.
- The `send_back_limit` cap from `[remote-commands]` (default 4 MiB
  per stream) applies. Long-running, chatty scripts should redirect
  to a logfile on the host and tail it via another mechanism.

See also: [command execution](command-execution.md),
[client groups and permissions](client-groups-permissions.md),
[operator runbook — schedules](operator-runbook.md#schedules).
