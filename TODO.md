# TODO

Going-forward items for the project. The pre-publication cleanup
pass (removing upstream-rport branding, copyright assertions,
fictional domains, and stale URLs) was folded into the initial
commit; live infrastructure (`docs.proxiport.net`,
`pairing.proxiport.net`, the goreleaser-driven release pipeline) is
already running.

## CI / tooling migrations

Two CI steps are currently set `continue-on-error: true` in
`.github/workflows/ci.yml` because they trip over inherited config
that needs migration. Drop the override once each is fixed.

- **`golangci-lint` config v2 migration.** The inherited
  `.golangci.yml` uses pre-v2 keys (`run.deadline`,
  `output.format`, `linters-settings.govet.check-shadowing`, the
  legacy `depguard.list-type` / `include-go-root` / `packages`
  shape, `unused.check-exported`, `unparam.algo`, top-level `new`,
  and the now-deleted `maligned` / `golint` linters). golangci-lint
  v1.64+ rejects the config with "configuration contains invalid
  elements" before linting anything. Migrate to the current schema
  (or as a stopgap, pin
  `golangci/golangci-lint-action@v6` to `version: v1.55.0`), then
  drop the `continue-on-error` from the lint step.
- **BDD harness vs Go 1.22.** `cmd/proxiportd/main.go` uses
  `viperCfg.SetConfigName("proxiportd.conf")` for config discovery.
  Under the toolchain CI installs via `go-version-file: go.mod`
  (currently `go 1.22`), proxiportd refuses to start under the BDD
  harness with "Invalid config: client authentication must be
  enabled" even though the per-package `proxiportd.conf` files do
  set `auth = "..."`. Repros neither on Go 1.25 nor in isolated
  `go test ./bdd/<pkg>/` runs. Most likely fix: replace the
  `AddConfigPath(".")` + `SetConfigName("proxiportd.conf")` pair
  with `SetConfigFile("./proxiportd.conf")` +
  `SetConfigType("toml")`. Once green, drop the
  `continue-on-error` from the BDD step.

## Documentation gaps

The current docs site ships:

- Home, Origin and licensing, What changed from openrport,
  Architecture, Install, Migrating from rport / openrport,
  Operator runbook.

Pages that the upstream rport docs used to cover and that this
project does **not** yet have, in rough priority order:

- API authentication (database column requirements, OAuth/OIDC,
  2FA delivery setup including SMTP and Pushover).
- Client authentication (auth_file format vs database table).
- Securing proxiportd with HTTPS (built-in ACME / external TLS).
- Command execution (`/commands` endpoint, environment, security
  posture).
- Scripts (`/scripts` endpoint, interpreter aliases, the script
  library).
- Monitoring (data retention, problems, ruleset, notifications).
- Tunnel hosting (subdomain routing, ACL, file reception).
- IP-address determination (when to set `ip_api_url`, security
  considerations of using third-party IP-discovery hosts).
- Watchdog integration.
- Client groups and permissions model.
- Vault (the encrypted credential store, lock/unlock semantics).
- Client attributes file format.

Each of these has corresponding example.conf options that
previously linked into the upstream docs and now don't. Filling
them in is the largest remaining content gap before v0.2.

## Infrastructure follow-ups

- **Demo / eval host.** Separate droplet, secured but with
  navigable pages, redeployable from this repo via a small script.
  Subdomain TBD (likely `demo.proxiport.net` or `try.proxiport.net`).
- Apex `proxiport.net` currently has no A record — optionally
  point it at the pairing droplet (or a small landing page) so
  bare-domain visitors don't 404.

## Optional polish

- Add an SVG wordmark alongside `proxiport.png` so the docs theme
  can use the sharper version at HTML scale.
- Replace the favicon's abstract "P" with an icon that explicitly
  conveys "tunnel" or "reverse-proxy" — only if you want the
  identity to be more on-the-nose than abstract.
