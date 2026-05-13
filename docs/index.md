# ProxiPort

A self-hosted remote-management server. ProxiPort gives you a small REST
API, an SSH-over-WebSocket tunnel transport, and an agent that idles on a
remote machine and brings tunnels up on demand. It is fully open source,
top to bottom — no proprietary plugin host, no closed frontend, no
capability gates.

ProxiPort is maintained by [Proximile LLC](https://proximile.llc) and
licensed under **AGPL-3.0-or-later**.

## Why it exists

The upstream **rport** project was MIT-licensed when CloudRadar GmbH
released it in 2020 and remained MIT under RealVNC Limited's stewardship
through 2023. In September 2023 RealVNC closed the source. The community
fork **openrport** carried the FOSS lineage forward for about eighteen
months and has been effectively unmaintained since mid-2025.

ProxiPort continues that work. We:

- removed the closed-source plugin scaffolding (`plus/`) and every
  capability gate it fed,
- wrote a new SvelteKit frontend from scratch to replace the proprietary
  Vue/Nuxt SPA the upstream shipped only as compiled bundles, and
- rebranded the tree so users do not confuse this with either the
  RealVNC closed product or the now-stale openrport fork.

The REST API, the chisel-based SSH-over-WebSocket transport, the agent
protocol, and the operational model are all kept as-is — they are the
value of the original project and we are not trying to rewrite them.

See [origin](origin.md) for the full provenance and a precise statement
of what was and was not forked from upstream.

## Quick links

- **Source** — this repository at
  [`github.com/proximile/proxiport`](https://github.com/proximile/proxiport).
- **Issues** — open one on the repository for bugs or feature requests.
- **Security** — see [SECURITY.md](https://github.com/proximile/proxiport/blob/main/SECURITY.md)
  for private vulnerability reporting via GitHub Security Advisories.

## Documentation

- [Origin and licensing](origin.md) — full provenance, what was forked,
  what was not, and why.
- [What changed from openrport](changes-from-openrport.md) — concrete
  behaviour diff for anyone migrating from rport / openrport.
- [Architecture](architecture.md) — server, agent, tunnel transport,
  data model.
- [Install](install.md) — install the server and connect an agent.
- [Migrating from rport / openrport](migration.md) — stop, replace,
  restart.
- [Operator runbook](operator-runbook.md) — service control,
  backups, credential rotation, common pitfalls.
