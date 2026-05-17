# ProxiPort

A self-hosted remote-management server: a REST API, an SSH-over-WebSocket
tunnel transport, and an agent that idles on a remote machine and brings
tunnels up on demand. **Fully open source, end to end** — no closed
plugin host, no proprietary frontend, no capability-gated edition.

Maintained by [Proximile LLC](https://proximile.llc).

## Try it

A public demo runs at
[**`https://demo.proxiport.net/`**](https://demo.proxiport.net/).
Sign in with `demo` / `demo` (the SPA login page shows the same
banner). The demo guest has a read-only profile — destructive
endpoints are walled off — and the whole state resets on the
half-hour, so it's safe to poke at. Three pre-registered agents
appear in the Inventory.

The demo is for evaluation only; do not depend on it. To run
your own, see [Install](docs/install.md).

## Origin and licensing

ProxiPort is a continuation of the open-source remote-management project
**rport** (CloudRadar GmbH, 2020; RealVNC Limited, 2023) and its
community fork **openrport** (2024–2025). RealVNC closed the source of
rport in September 2023; openrport carried the FOSS lineage forward; the
openrport fork itself has been effectively unmaintained since mid-2025.
ProxiPort picks it up.

ProxiPort imports the upstream openrport tree and forks **only the FOSS
portion**:

- The MIT-licensed Go server, agent, shared libraries, and tooling came
  over.
- The upstream `plus/` plugin scaffolding — whose sole function was to
  load a closed-source proprietary plugin binary — was deleted, along
  with every `IsPlusEnabled` capability gate.
- The proprietary Vue/Nuxt frontend (which the upstream shipped only as
  prebuilt JavaScript; the source was never released) is **not** part of
  ProxiPort. The frontend in `frontend/` is an original SvelteKit /
  TypeScript / Tailwind SPA written from scratch.

The combined work, as distributed under the name ProxiPort, is
**AGPL-3.0-or-later** (see [`LICENSE`](LICENSE)). The inherited MIT
attribution travels in [`LICENSE-MIT`](LICENSE-MIT). The combined-work
attribution and a precise statement of what was and was not forked from
upstream is in [`NOTICE`](NOTICE).

## Documentation

Project documentation lives in [`docs/`](docs/) and is published to
GitHub Pages.

- [`docs/index.md`](docs/index.md) — landing page
- [`docs/origin.md`](docs/origin.md) — full provenance and the FOSS-only
  fork statement
- [`docs/architecture.md`](docs/architecture.md) — system overview
- [`docs/install.md`](docs/install.md) — install and quickstart

## What ProxiPort is not

- **Not a fork of any non-FOSS code.** We took only the MIT-licensed
  source of openrport.
- **Not a mesh VPN.** ProxiPort is a remote-management / reverse-tunnel
  tool. For mesh networking, run a dedicated tool (Tailscale, Headscale,
  Nebula, etc.) alongside.
- **Not affiliated with RealVNC, CloudRadar, or the openrport authors.**

## Contributing, issues, and security

- **Bugs and feature requests** — open an issue on this repository.
- **Security disclosures** — see [`SECURITY.md`](SECURITY.md).
- **Contributing** — see [`CONTRIBUTING.md`](CONTRIBUTING.md).
- **Code of conduct** — see [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md).
