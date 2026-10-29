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
`/var/lib/proxiport-agent/state.json`.

```json
{
  "last_update":    "2026-05-17T10:37:25.643839Z",
  "last_update_ts": 1779014245,
  "last_state":     "connected",
  "last_message":   "ping to 203.0.113.7:443 succeeded within 14.682823ms"
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

`last_message` is a free-text description of the most recent event,
such as `Connected to <server> (connection request answered in <duration>)`,
`ping to <ip:port> succeeded within <rtt>` or
`Retrying in <duration>...` — useful for logs, not stable enough to
parse.

The file is written on four events:

1. The agent starts (`last_state: initialized`).
2. A connection attempt succeeded (`connected`).
3. A connection attempt failed and a retry is scheduled
   (`reconnecting`). The agent then waits before retrying. The wait
   grows exponentially from about 5 seconds to a fixed ceiling of 10
   minutes, with jitter. If the server timed out, the wait resets to a
   few seconds. `max_retry_interval` is accepted in the config but
   currently has no effect on this wait. Once `max_retry_count` is
   exhausted, the agent gives up without writing the file again.
4. A keepalive ping succeeded (`connected`; interval governed by
   `keep_alive`). A failed or timed-out keepalive ping does not update
   the file. The agent closes the connection, and the next update is
   the `reconnecting` entry written when the reconnect attempt is
   scheduled.

## Implementing a watchdog

### Linux: the systemd watchdog (recommended)

systemd has a built-in watchdog. Add `WatchdogSec=N` to the agent's
unit, and systemd creates a Unix socket that the agent will push
heartbeats to automatically — no extra script needed. Add it with a
drop-in rather than by copying a whole unit:

```sh
sudo systemctl edit proxiport
```

```ini
[Service]
WatchdogSec=960
```

```sh
sudo systemctl restart proxiport
```

A drop-in keeps the installed unit's `ExecStart` and the rest of its
settings. The binary is `/usr/bin/proxiport` for deb/rpm and tarball
installs, and `/usr/local/bin/proxiport` for pairing-script installs,
so don't copy a whole unit with a hard-coded path from another host.

`WatchdogSec` must be longer than the longest gap the agent can leave
between two heartbeats:

- While connected, the agent sends a heartbeat after each successful
  keepalive ping. One cycle can take `keep_alive` + up to 5 s of
  jitter + up to three `keep_alive_timeout` waits (default 30 s each)
  plus about 6 s of retry back-off. With `keep_alive = '3m'` that is
  about 4m40s.
- While reconnecting, the agent sends a heartbeat before each back-off
  wait. The wait grows to a fixed ceiling of 10 minutes;
  `max_retry_interval` does not cap it. The next connection attempt
  can then take up to 5 minutes to time out (or
  `transport_dial_timeout` when a transport chain is set).

A `WatchdogSec` shorter than these gaps turns a normal reconnect
back-off into a kill-and-restart cycle during any prolonged outage.
With `keep_alive = '3m'`, `WatchdogSec = 960` (16 minutes) rides out
both.

When the agent detects the systemd notify socket (the
`NOTIFY_SOCKET` environment variable), it logs a confirmation at
debug level:

```
Using NOTIFY_SOCKET /run/systemd/notify for systemd watchdog integration
```

If 960 seconds pass with no notification, systemd kills the agent and
respawns it through `Restart=always`. The state.json file is still
written even when the systemd socket is in use, so you can read it
to debug what the agent thought was happening.

### Windows: a scheduled PowerShell check

Windows services do not have a built-in watchdog equivalent. A
scheduled task that runs every minute is the usual shape:

```powershell
$stateFile = 'C:\Program Files\proxiport\data\state.json'
$threshholdSec = 960

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
runs every minute under `SYSTEM`. Keep `$threshholdSec` above the
longest heartbeat gap described for `WatchdogSec` above.

### Custom supervisor

Any process that reads `state.json` once a minute, compares
`last_update_ts` against the current Unix timestamp, and restarts
the agent on excess staleness will work. The check is intentionally
trivial — language and tooling are an implementation detail.

A minimal shell variant:

```bash
#!/bin/sh
threshold=960
now=$(date +%s)
last=$(jq -r .last_update_ts /var/lib/proxiport-agent/state.json)
if [ $((now - last)) -gt "$threshold" ]; then
    logger -t proxiport-watchdog "stale heartbeat; restarting"
    systemctl restart proxiport
fi
```

Schedule from cron, run as root.

## Verifying the integration

1. With the agent connected, watch `state.json` update:

   ```bash
   watch -n 5 'jq . /var/lib/proxiport-agent/state.json'
   ```

   The timestamp should advance on the `keep_alive` cadence.

2. Block the agent's egress to simulate a network failure. The agent
   notices at the next keepalive: after `keep_alive` (plus a little
   jitter), a ping times out after `keep_alive_timeout`, and is
   retried up to three times. The agent then drops the connection, and
   `state.json` switches to `last_state: "reconnecting"`. After that, a
   `reconnecting` heartbeat is written on each reconnect attempt,
   spaced by the agent's built-in back-off (5 seconds, growing to at
   most 10 minutes). The supervisor should not restart the agent yet —
   the agent is correctly trying.

3. Push the agent into a wedged state. Easiest reproduction is
   `kill -STOP $(pidof proxiport)` to freeze the process. The
   heartbeat stops; after the threshold, the supervisor restarts
   `proxiport`. Unfreeze with `kill -CONT` if you do not want to
   exercise the full restart path during the test.

## Hardening checklist

- Pick a `WatchdogSec` (or threshold) that is **larger** than the
  longest heartbeat gap: the 10-minute reconnect back-off ceiling plus
  one connection attempt, and the keepalive cycle while connected (see
  [above](#linux-the-systemd-watchdog-recommended)). Too tight, and
  systemd will restart the agent during normal reconnects.
- Leave `Restart=always` and `RestartSec` set on the unit. Without
  them, systemd's kill won't be followed by a respawn.
- Monitor the restart counter (`systemctl status proxiport`) — a
  watchdog that fires constantly is hiding a real network problem.

See also: [operator runbook — service control](operator-runbook.md#service-control)
and [architecture — agent](architecture.md#agent).
