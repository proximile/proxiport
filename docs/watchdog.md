# Watchdog integration

The `proxiport` agent already self-supervises its connection: a broken
chisel session is retried with exponential backoff, and connection
keepalives detect a peer that has silently dropped. In normal
operation you do not need a watchdog.

The watchdog integration exists for the corner case where the agent
**process is alive but its connection logic has wedged** — typically
a kernel-level network issue that the Go runtime hasn't surfaced as
an error. With the watchdog enabled, the agent emits a periodic
heartbeat to a file (or to the systemd notify socket) that an external
supervisor can use to restart the process.

## When to use it

- Long-running agent on a host with flaky networking, where the
  reconnect loop has been observed to hang.
- A deployment where agents disappear for hours and an operator
  notices late.
- Belt-and-braces hardening on critical infrastructure.

If you have not seen a stuck agent, leave it off. The watchdog adds
no behaviour beyond what `systemctl restart proxiport` would do for
you on demand.

## Enabling on the agent

In
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
under `[connection]`:

```toml
[connection]
  keep_alive = '3m'
  max_retry_count = -1
  watchdog_integration = true
```

Two preconditions:

- `keep_alive` must be greater than `0s`. Disable keepalives and there
  are no heartbeats to emit.
- `max_retry_count` must be `-1` (unlimited). If the agent gives up
  after a bounded number of attempts, the watchdog would be the thing
  restarting an agent that already chose to stop — the wrong shape.

Restart the agent:

```bash
sudo systemctl restart proxiport
```

The agent will create `state.json` in its data directory.

## The state.json heartbeat

Path: `{data_dir}/state.json`, defaulting to
`/var/lib/proxiport/state.json`.

```json
{
  "last_update":    "2026-05-17T10:37:25.643839+00:00",
  "last_update_ts": 1747475845,
  "last_state":     "connected",
  "last_message":   "ping to proxiport server proxiport.example.com:443 succeeded within 14.682823ms"
}
```

The two `last_update*` fields are the ones a supervisor should compare
against the current clock. As long as the timestamp keeps advancing,
the agent is alive.

`last_state` carries one of:

- `initialized` — the agent has just started and has not produced a
  connection result yet.
- `connected` — the chisel session is up.
- `reconnecting` — the agent is dialling the server and has not yet
  succeeded.

`last_message` is a free-text description of the most recent event —
useful for logs, not stable enough to parse.

The file is updated on three events:

1. A connection attempt succeeded.
2. A connection attempt failed (re-attempt interval governed by
   `max_retry_interval`).
3. A keepalive ping was sent (interval governed by `keep_alive`).

## Implementing a watchdog

### Linux: the systemd watchdog (recommended)

systemd has a built-in watchdog. Add `WatchdogSec=N` to the agent's
unit file, and systemd creates a Unix socket that the agent will
push heartbeats to automatically — no extra script needed.

```ini
# /etc/systemd/system/proxiport.service
[Unit]
Description=ProxiPort agent
ConditionFileIsExecutable=/usr/local/bin/proxiport

[Service]
ExecStart=/usr/local/bin/proxiport -c /etc/proxiport/proxiport.conf
LimitNOFILE=1048576
User=proxiport
Restart=always
RestartSec=120
WatchdogSec=200

[Install]
WantedBy=multi-user.target

[Unit]
StartLimitIntervalSec=5
StartLimitBurst=10
```

`WatchdogSec` must be slightly longer than the worst case of
`max_retry_interval` and `keep_alive`. A common combination:

- `keep_alive = '3m'`
- `max_retry_interval = '3m'`
- `WatchdogSec = 200` (≈3:20)

When the agent detects the systemd notify socket (the
`NOTIFY_SOCKET` environment variable), it logs a confirmation at
debug level:

```
Using NOTIFY_SOCKET /run/systemd/notify for systemd watchdog integration
```

If 200 seconds pass with no notification, systemd kills the agent and
respawns it through `Restart=always`. The state.json file is still
written even when the systemd socket is in use, so you can read it
to debug what the agent thought was happening.

### Windows: a scheduled PowerShell check

Windows services do not have a built-in watchdog equivalent. A
scheduled task that runs every minute is the usual shape:

```powershell
$stateFile = 'C:\Program Files\proxiport\data\state.json'
$threshholdSec = 600

$now = [int][double]::Parse((Get-Date -UFormat %s))
$lastUpdate = (Get-Content $stateFile | ConvertFrom-Json).last_update_ts
$diff = $now - $lastUpdate

if ($diff -gt $threshholdSec) {
    Write-Output "ProxiPort wedged. No activity for $diff seconds."
    Restart-Service proxiport
} else {
    Write-Output "ProxiPort is healthy. Last activity $diff seconds ago."
}
```

Save as `check-proxiport.ps1`, register as a scheduled task that
runs every minute under `SYSTEM`. Adjust `$threshholdSec` to a
small multiple of your `keep_alive` value.

### Custom supervisor

Any process that reads `state.json` once a minute, compares
`last_update_ts` against the current Unix timestamp, and restarts
the agent on excess staleness will work. The check is intentionally
trivial — language and tooling are an implementation detail.

A minimal shell variant:

```bash
#!/bin/sh
threshold=600
now=$(date +%s)
last=$(jq -r .last_update_ts /var/lib/proxiport/state.json)
if [ $((now - last)) -gt "$threshold" ]; then
    logger -t proxiport-watchdog "stale heartbeat; restarting"
    systemctl restart proxiport
fi
```

Schedule from cron, run as root.

## Verifying the integration

1. With the agent connected, watch `state.json` update:

   ```bash
   watch -n 5 'jq . /var/lib/proxiport/state.json'
   ```

   The timestamp should advance on the `keep_alive` cadence.

2. Block the agent's egress to simulate a network failure. The file
   should switch to `last_state: "reconnecting"` within
   `max_retry_interval`. The supervisor should not restart the agent
   yet — the agent is correctly trying.

3. Push the agent into a wedged state. Easiest reproduction is
   `kill -STOP $(pidof proxiport)` to freeze the process. The
   heartbeat stops; after the threshold, the supervisor restarts
   `proxiport`. Unfreeze with `kill -CONT` if you do not want to
   exercise the full restart path during the test.

## Hardening checklist

- Pick a `WatchdogSec` (or threshold) that is **larger** than the
  worst case of `max_retry_interval + keep_alive`. Too tight, and
  systemd will restart the agent during normal reconnects.
- Leave `Restart=always` and `RestartSec` set on the unit. Without
  them, systemd's kill won't be followed by a respawn.
- Monitor the restart counter (`systemctl status proxiport`) — a
  watchdog that fires constantly is hiding a real network problem.

See also: [operator runbook — service control](operator-runbook.md#service-control)
and [architecture — agent](architecture.md#agent).
