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
5. The agent deletes the tempfile when the process exits, regardless
   of the exit code.

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

If `[remote-commands] enabled` is `false`, scripts are also disabled —
script execution reuses the command-execution code path.

The script body, after the interpreter is prepended, must still pass
the `[remote-commands]` allow/deny filter. The default allow regex
covers `/usr/bin/.*`, `/usr/local/bin/.*`, and `C:\Windows\System32\.*`,
which covers `/bin/sh`, `/usr/bin/python3`, `powershell.exe`, etc.

A common pitfall: if you override `allow` to a restrictive list, you
must include the interpreters you want scripts to run with, otherwise
the agent will reject them before the body executes.

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

Concurrency and abort-on-error semantics are the same as
[command fan-out](command-execution.md#multi-host-execution).

## Interpreters and aliases

The server can ship any interpreter the agent can locate. Common
shapes:

- **Bash / sh / zsh** — `/bin/bash`, `/bin/sh`, `/usr/local/bin/zsh`.
  The agent invokes the interpreter against the tempfile path.
- **Python** — `/usr/bin/python3`. Use `#!/usr/bin/env python3` in
  the body if you want it to work across hosts with different paths.
- **PowerShell** — `powershell` (Windows PowerShell 5.x) or a full
  path like `C:\Program Files\PowerShell\7\pwsh.exe`. ProxiPort
  appends `-Noninteractive -executionpolicy bypass -File` automatically
  when the executable name contains `powershell` (case-insensitive).
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

The SPA's **Library → Scripts** page is a convenience wrapper around
`/api/v1/library/scripts`. A library entry stores the same fields as
an ad-hoc request — name, interpreter, sudo flag, cwd, and the body —
and can be run against one or many agents with a click. Library entries
are also the unit a [schedule](operator-runbook.md#schedules) targets.

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
    "script":"pwd"
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
  --data-raw '{ "name":"...", "interpreter":"...", "is_sudo":false, "cwd":"...", "script":"..." }' \
  https://proxiport.example.com/api/v1/library/scripts/<script-id>

# Delete
curl -s -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://proxiport.example.com/api/v1/library/scripts/<script-id>
```

The endpoint accepts `sort=` and `filter[<field>]=` on `id`, `name`,
`created_by`, and `created_at`.

## WebSocket execution

For live stdout, the server exposes `WS /ws/scripts`. Send the same
JSON payload as the REST endpoint; receive each chunk as a frame. The
SPA uses this path so the operator sees output as it is produced
instead of waiting for the job to complete.

To enable the test harness UI at `/api/v1/test/scripts/ui`, set
`enable_ws_test_endpoints = true` in `[api]`. The test UI is the
easiest way to confirm a script body, interpreter, and target host
combination before wiring it into the SPA library or a scheduled job.

## Security notes

- Scripts inherit the OS account that runs `proxiport`. Use sudo
  rules to elevate only specific scripts, not all of them.
- The body lands on disk briefly under `{data_dir}/scripts/`. If
  the host has FIM (file-integrity monitoring), expect events from
  that directory during script execution.
- The `send_back_limit` cap from `[remote-commands]` (default 4 MiB
  per stream) applies. Long-running, chatty scripts should redirect
  to a logfile on the host and tail it via another mechanism.

See also: [command execution](command-execution.md),
[client groups and permissions](client-groups-permissions.md),
[operator runbook — schedules](operator-runbook.md#schedules).
