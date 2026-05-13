# Origin, provenance, and the FOSS-only fork

This page is the long-form companion to [`NOTICE`](https://github.com/proximile/proxiport/blob/main/NOTICE). If anything
here conflicts with `NOTICE`, the `NOTICE` file is authoritative.

## The lineage

| Year | Project | Maintainer | Licence | Status |
| --- | --- | --- | --- | --- |
| 2020 | rport | CloudRadar GmbH | MIT | original release |
| 2022 | rport | RealVNC Limited (acquired) | MIT | continued FOSS development |
| Sep 2023 | rport | RealVNC Limited | proprietary | source closed; subsequent versions are not FOSS |
| 2024 | openrport | openrport authors | MIT | community fork of the last MIT rport tree |
| Jun 2025 | openrport | (effectively unmaintained) | MIT | last commit early-mid 2025; single-author |
| 2026 | **ProxiPort** | Proximile LLC | AGPL-3.0-or-later | this project |

ProxiPort imports the openrport Go server and agent tree at the
MIT-licensed state of the fork. We are continuing the FOSS lineage.

## What we forked

Specifically taken from openrport, under its MIT licence, and now part of
the ProxiPort tree under AGPL-3.0-or-later (as the combined work):

- the Go server (`server/`)
- the Go agent (`client/` upstream, renamed to the ProxiPort agent
  packaging)
- the shared libraries (`share/`)
- the build scripts, BDD harness, and API documentation skeleton
- the chisel-derived tunnel transport, by way of the upstream fork

The inherited MIT copyright notice for that imported code is preserved
verbatim in [`LICENSE-MIT`](https://github.com/proximile/proxiport/blob/main/LICENSE-MIT).

## What we did not fork

### The `plus/` plugin scaffolding

openrport's source tree contained a directory called `plus/`. Its sole
function was to load a separately-distributed proprietary plugin binary
(the "Plus" plugin) that gated several enterprise features (OAuth/OIDC,
RBAC, alerting, etc.). The Plus binary itself was never in any public
source repository — it was distributed by the previous upstream
maintainer under a proprietary licence, behind a paywall.

We deleted the `plus/` scaffolding from ProxiPort along with every
`IsPlusEnabled` capability gate. We do not ship the proprietary binary,
we do not load it, and we do not reimplement Plus features as a
license-gated commercial add-on. Features that previously lived behind
the gate (OAuth/OIDC, RBAC, alerting, …) will be reimplemented in the
open under AGPL.

### The proprietary Vue/Nuxt frontend

The upstream rport / openrport projects shipped a web frontend, but
only as a prebuilt JavaScript bundle. The source for that frontend was
never released as FOSS. We could not have forked it even if we wanted
to, because the source was not available.

The frontend in ProxiPort's `frontend/` is original work: a SvelteKit
SPA written from scratch by Proximile LLC, in TypeScript, against the
existing REST API. It is AGPL-licensed along with the rest of the tree.

### Names and attribution

"RPort" and "openrport" are names used by the upstream projects.
ProxiPort is not affiliated with, endorsed by, or sponsored by RealVNC
Limited, CloudRadar GmbH, or the openrport authors.
"ProxiPort" is the project name used by Proximile LLC.

## Why AGPL?

The upstream rport / openrport tree is MIT-licensed and remains so as
imported — the inherited MIT notice in [`LICENSE-MIT`](https://github.com/proximile/proxiport/blob/main/LICENSE-MIT) is
the authoritative attribution for that code. The MIT licence permits
relicensing of the combined work as long as the original notice is
preserved, which it is.

We chose AGPL-3.0-or-later for the combined work for two reasons:

1. **Network-use protection.** ProxiPort is a server. AGPL is the
   licence specifically designed to ensure that users interacting with a
   modified server over the network get the right to its source.
   Permissive licences (MIT, Apache) do not give that guarantee, and the
   one thing we are not interested in repeating is the cycle of FOSS
   server projects being closed-sourced once they reach a useful state.
2. **No closed forks of ProxiPort itself.** If someone wants to build
   commercial features on top of ProxiPort, they may, but the AGPL
   ensures their modifications to ProxiPort itself flow back. The
   inherited MIT code is unaffected — anyone who wants the
   permissively-licensed upstream remains free to take it directly from
   openrport.

## If you find a problem

If you find a file in this tree that is not FOSS, is missing
attribution, or you believe is licensed incorrectly, please open an
issue. Attribution bugs are bugs and we will fix them.
