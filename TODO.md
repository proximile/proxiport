# TODO

Going-forward items for the project. The pre-publication cleanup
pass (removing upstream-rport branding, copyright assertions,
fictional domains, and stale URLs) is complete and folded into the
initial commit.

## Infrastructure (operator-owned)

- **`docs.proxiport.net`** — create a CNAME record at the registrar
  pointing to `proximile.github.io`. The repo already includes
  `docs/CNAME` so GitHub Pages will pick up the custom domain on
  the first successful Pages build. GitHub will then issue a
  Let's Encrypt cert automatically.
- **`pairing.proxiport.net`** — first-party pairing service. Pick
  an implementation, run it on a DigitalOcean droplet, then either
  flip `cmd/proxiportd/main.go`'s `DefaultPairingURL` to that URL
  or document that operators set `pairing_url` per deployment.
- **Demo / eval host** — separate droplet, secured but with
  navigable pages, redeployable from this repo via a small script.
  Subdomain TBD (likely `demo.proxiport.net` or `try.proxiport.net`).

## Release plumbing

- Tag `v0.1.0` to trigger goreleaser, after the GitHub-side toggles
  are set (private vulnerability reporting on, Pages source =
  GitHub Actions, topics added, branch protection on `main`).

## Documentation gaps

The current docs site ships these pages:

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

## Optional polish

- Add an SVG wordmark alongside `proxiport.png` so the docs theme
  can use the sharper version at HTML scale.
- Replace the favicon's abstract "P" with an icon that explicitly
  conveys "tunnel" or "reverse-proxy" — only if you want the
  identity to be more on-the-nose than abstract.
