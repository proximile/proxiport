# Monitoring

The `proxiport` agent can sample basic host metrics — CPU, memory,
disk fill, network throughput, and a running-process list — and ship
them to the server, where they are persisted to `monitoring.db` and
charted in the SPA's per-client **Monitoring** tab.

Monitoring is enabled by default on both ends. Operators who want to
keep the agent footprint minimal, or who already run a dedicated
monitoring stack (Prometheus, Netdata, Grafana Agent, etc.), can
turn it off without losing tunnel or command functionality.

## What is sampled

The agent collects, on every interval:

- **CPU usage** as a percentage across all cores.
- **Memory usage** as a percentage of total.
- **IO wait** as a percentage of CPU time (CPU iowait share). Only
  Linux agents report it; on macOS and Windows the operating system
  exposes no iowait counter, so the value is always 0.
- **Filesystem fill** for every detected mount point or volume.
- **Network bandwidth** on up to two named interfaces (`net_lan`,
  `net_wan`).
- **Process list** sorted by PID descending; the top *N* are reported.

No application-level metrics (request counts, queue depth, etc.) are
collected. This is host telemetry, not APM.

## Server configuration

Under `[monitoring]` in
[`proxiportd.conf`](https://github.com/proximile/proxiport/blob/main/proxiportd.example.conf):

```toml
[monitoring]
  enabled = true
  data_storage_duration = "7d"
```

`enabled` is the system-wide kill switch. Setting it to `false`
overrides every agent's monitoring setting — no samples are accepted
or stored. The monitoring read endpoints
(`/clients/{id}/graph-metrics`, `/graph-metrics/{graph}`, `/metrics`,
`/processes`, `/mountpoints`) then answer `404` with "monitoring
disabled. re-enable to view monitoring statistics."

`data_storage_duration` (`7d` default) is the retention window. Use
`d` for days or `h` for hours, and quote the value. The minimum is
`1h`: a shorter value stops the server at startup with "monitoring
results must be stored for at least 1 hour". A background task runs
every 2 minutes and deletes rows older than the window. After a pass
that deleted rows, the task runs a `VACUUM` to shrink `monitoring.db`,
at most once every 24 hours. The older integer key
`data_storage_days` is deprecated but still honoured: when it is set
above 0 it takes precedence over `data_storage_duration`.

!!! warning "Database growth"
    `monitoring.db` can grow quickly. Each connected agent at the
    default 60-second interval writes a row roughly every minute
    across CPU, memory, IO, every mounted filesystem, every monitored
    network adapter, and the top-N processes. A fleet of a few
    hundred agents at the default retention will easily push past
    a gigabyte. Pick `data_storage_duration` deliberately, and put
    `monitoring.db` on a volume with room to grow. To keep the file
    outside the main `data_dir`, replace it with a symlink before
    first start. The packaged systemd unit runs with
    `ProtectSystem=strict` and lets the service write only under
    `/var/lib/proxiport` and `/var/log/proxiport`, so also add the
    target directory with a drop-in (`systemctl edit proxiportd`, then
    `[Service]` / `ReadWritePaths=/path/to/target-dir`). Otherwise
    SQLite cannot write there. Do not put the target under `/home`:
    the unit sets `ProtectHome=true`, which hides that tree from the
    service.

## Client configuration

The `[monitoring]` block in
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
controls what the agent sends:

```toml
[monitoring]
  enabled = true
  interval = '60s'

  fs_type_include = ['ext3','ext4','xfs','jfs','ntfs','btrfs','hfs','apfs','exfat','smbfs','nfs']
  fs_path_exclude = []
  fs_path_exclude_recurse = false
  fs_identify_mountpoints_by_device = true

  pm_enabled = true
  pm_kerneltasks_enabled = true
  pm_max_number_processes = 500

  net_lan = ['eth0', '1000']
  #net_wan = ['eth1', '1000']
```

`interval` is the sample period, written as a **quoted duration string**
(e.g. `'60s'`, `'5m'`) like the other duration keys. A bare integer is
decoded as nanoseconds, not seconds, so write `interval = '5m'`, not
`interval = 300`. Values below 60 seconds are clamped up to `'60s'` — the
storage rate is the bottleneck, not the collection rate.

`fs_type_include` whitelists filesystem types the agent will report.
`fs_path_exclude` lists mountpoints, drive letters or globs (e.g.
`['/mnt/*', 'h:']`) to skip. Each entry must match a whole mountpoint.
With `fs_path_exclude_recurse = true`, an entry can also be any
directory, and every mountpoint beneath it is excluded too. This is
useful for excluding bind mounts, tmpfs, or container overlay paths.

`net_lan` and `net_wan` are optional, and each takes a
`[device_name, max_speed_mbit]` pair. Leave a key out if you don't
want that card monitored. When you set one, the device name must be
non-empty and the max speed must be a whole number of at least 1.
Otherwise the agent fails config validation and does not start. The
agent uses the max speed to compute a usage percentage. On Windows
discover the device name with `Get-NetAdapter`.

To disable monitoring on a single agent — for example a tiny edge
device where samples cost meaningful CPU — set:

```toml
[monitoring]
  enabled = false
```

The agent then sends no samples, so the per-client Monitoring tab
shows an empty state.

## Reading the data

### SPA

The per-client **Monitoring** tab plots CPU and memory utilisation for
the last 6, 12 or 24 hours (the chart cannot cover the full retention
window). It also shows the most recent process snapshot, or the
snapshot nearest a sample you select on the chart. The tab does not
show filesystem fill levels. Query them from
`/api/v1/clients/<id>/mountpoints`.

### REST API

The raw rows are exposed via `/api/v1/clients/<id>/metrics`. Rows
come back newest first; use `sort=timestamp` for ascending order.
Select a time range with `filter[timestamp][since]=` /
`filter[timestamp][until]=` (RFC3339) or `filter[timestamp][gt]=` /
`filter[timestamp][lt]=` (unix epoch). Choose columns with
`fields[metrics]=` followed by any of `timestamp`,
`cpu_usage_percent`, `memory_usage_percent` or `io_usage_percent`
(all four are returned by default). The endpoint is paginated with
`page[limit]` and `page[offset]`. The default limit is 1 row, so a
query without `page[limit]` returns only the latest sample; the
maximum is 120. Other query parameters are ignored. Filesystem and
process snapshots are at `/api/v1/clients/<id>/mountpoints` (max
limit 100) and `/api/v1/clients/<id>/processes` (max limit 10), with
the same timestamp filters. Use it to feed another visualisation system, to export
to a long-term store, or to script alerting on top of ProxiPort
without modifying the agent.

```bash
TOKEN=$(curl -s -u admin:password \
  https://proxiport.example.com/api/v1/login | jq -r .data.token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'https://proxiport.example.com/api/v1/clients/alpha-prod/metrics?filter[timestamp][since]=2026-05-17T00:00:00Z&page[limit]=120&sort=timestamp' \
  | jq
```

### Backups

`monitoring.db` is one of the SQLite files listed in the
[operator runbook backups section](operator-runbook.md#backups).
Treat it like any other datastore — snapshot it with the
filesystem-stop-the-world technique or use SQLite's `.backup`
command for a hot copy. Losing the file resets the charts to empty
but does not break anything else; the agents continue sending samples
to a fresh `monitoring.db` on next start.

## Alerting

ProxiPort itself does not evaluate thresholds against the metrics
yet. Until alerting lands as an OSS module, wire one of these:

- Run a small cron job that queries `/api/v1/clients/<id>/metrics`,
  checks against your thresholds, and pushes to your notifier of
  choice (Alertmanager, Pushover, Slack webhook).
- Use the agent's command/script channel to invoke the host's own
  monitoring agent (`node_exporter`, `telegraf`, …) and rely on
  that pipeline for alerting. ProxiPort then sits alongside the
  existing observability stack rather than replacing it.

Alerting based on monitoring thresholds is planned but not yet shipped — tracked
as an OSS reimplementation of upstream functionality previously gated
behind a proprietary plugin. See [Changes from openrport](changes-from-openrport.md).

See also: [architecture — datastore](architecture.md#datastore),
[operator runbook — backups](operator-runbook.md#backups),
[client attributes](client-attributes.md) for labelling agents with
tags and labels, so you can find the agents you want (the inventory
search matches tags; `/api/v1/clients` accepts `filter[tags]` and
`filter[labels]`) before opening each one's Monitoring tab.
