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
- **IO usage** as a percentage (Linux only; the equivalent on macOS
  and Windows is best-effort).
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
or stored.

`data_storage_duration` (`7d` default) is the retention window. Older
rows are purged on a background cleanup pass. Use `d` for days or `h`
for hours; quote the value.

!!! warning "Database growth"
    `monitoring.db` can grow quickly. Each connected agent at the
    default 60-second interval writes a row roughly every minute
    across CPU, memory, IO, every mounted filesystem, every monitored
    network adapter, and the top-N processes. A fleet of a few
    hundred agents at the default retention will easily push past
    a gigabyte. Pick `data_storage_duration` deliberately, and put
    `monitoring.db` on a volume with room to grow. To keep the file
    outside the main `data_dir`, replace it with a symlink before
    first start.

## Client configuration

The `[monitoring]` block in
[`proxiport.conf`](https://github.com/proximile/proxiport/blob/main/proxiport.example.conf)
controls what the agent sends:

```toml
[monitoring]
  enabled = true
  interval = 60

  fs_type_include = ['ext3','ext4','xfs','jfs','ntfs','btrfs','hfs','apfs','exfat','smbfs','nfs']
  fs_path_exclude = []
  fs_path_exclude_recurse = false
  fs_identify_mountpoints_by_device = true

  pm_enabled = true
  pm_enable_kerneltask_monitoring = true
  pm_max_number_monitored_processes = 500

  net_lan = ['eth0', '1000']
  net_wan = ['', '1000']
```

`interval` is the sample period in seconds. Values below 60 are
clamped to 60 — the storage rate is the bottleneck, not the
collection rate.

`fs_type_include` whitelists filesystem types the agent will report.
`fs_path_exclude` is a list of paths (or globs, with
`fs_path_exclude_recurse = true`) to skip; useful for excluding bind
mounts, tmpfs, or container overlay paths.

`net_lan` and `net_wan` each take a `[device_name, max_speed_mbit]`
pair. The agent uses the max speed to compute a usage percentage. On
Windows discover the device name with `Get-NetAdapter`.

To disable monitoring on a single agent — for example a tiny edge
device where samples cost meaningful CPU — set:

```toml
[monitoring]
  enabled = false
```

The agent then sends no samples. The server stops accepting samples
from it as well; the per-client Monitoring tab shows an empty state.

## Reading the data

### SPA

The per-client **Monitoring** tab plots CPU and memory series over the
retention window. Filesystem fill levels and process lists are
queryable from the same view.

### REST API

The raw rows are exposed via `/api/v1/clients/<id>/metrics`. The
endpoint supports range queries (`from=`, `to=`) and filtering by
metric name. Use it to feed another visualisation system, to export
to a long-term store, or to script alerting on top of ProxiPort
without modifying the agent.

```bash
TOKEN=$(curl -s -u admin:password \
  http://proxiport.example.com:3000/api/v1/login | jq -r .data.token)

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://proxiport.example.com:3000/api/v1/clients/alpha-prod/metrics?from=2026-05-17T00:00:00Z' \
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

Alerting based on monitoring thresholds is planned for v0.2 — tracked
as an OSS reimplementation of upstream functionality previously gated
behind a proprietary plugin. See [Changes from openrport](changes-from-openrport.md).

See also: [architecture — datastore](architecture.md#datastore),
[operator runbook — backups](operator-runbook.md#backups),
[client attributes](client-attributes.md) for labelling agents so the
monitoring view can be filtered by environment, role, or location.
