# Contributing to ProxiPort

Thanks for considering a contribution. This document covers licensing,
what kinds of contributions are welcome, and the mechanics of sending a
patch.

## Licensing of contributions

ProxiPort as a whole is **AGPL-3.0-or-later** (see [`LICENSE`](LICENSE)).

By submitting a pull request, issue, patch, or other contribution you
agree that your contribution will be licensed under AGPL-3.0-or-later
as part of the combined work. We do **not** require a separate CLA: the
inbound = outbound principle (your code is contributed under the same
licence as the project) is sufficient.

If a piece of code you contribute also carries an additional permissive
licence (MIT, Apache-2.0, BSD, etc.) you may keep that file's original
header; the combined work remains AGPL.

## What we are looking for

- **Bug reports** with reproducers — file as issues on this repository.
- **Code review and patches** for the Go server, agent, or SvelteKit
  frontend.
- **Frontend feedback** — the SvelteKit SPA is the most-changed part of
  the project; usability bugs and missing-feature reports are welcome.
- **Packaging** for distros other than the ones the maintainer ships
  natively.
- **Documentation** improvements, especially for migration from
  rport/openrport deployments.

## Out of scope

A few things that won't get merged regardless of how good the PR is:

- Features whose only purpose is to gate functionality behind a paid
  licence. ProxiPort intentionally has no edition split.
- Mesh-VPN features. ProxiPort is a remote-management / reverse-tunnel
  tool; mesh VPNs are a different problem domain and the project does
  not want to grow into one. Run a mesh tool alongside ProxiPort if you
  need one.

For everything else, open an issue first if the change is non-trivial
so we can sanity-check the direction before you spend time on a patch.

## How to send a contribution

1. Open an issue describing the change, unless it is trivial.
2. Fork, branch, push, open a PR against `main`.
3. A `Signed-off-by` trailer (`git commit -s`) is welcome but not
   required; the pull request itself is the inbound = outbound agreement.
4. Before pushing, run the core checks CI gates on. You need the Go
   version listed in `go.mod` and a C compiler: the server uses the cgo
   SQLite driver, so run the server checks with `CGO_ENABLED=1`
   (without cgo the driver compiles to a stub and the database tests
   fail). The agent (`./cmd/proxiport`) builds with `CGO_ENABLED=0`.
   - `gofmt -s -l $(git ls-files '*.go')` (must list nothing except
     generated files, which begin with a `Code generated ... DO NOT
     EDIT` line and which CI skips) and `go vet ./...`
   - the unit tests with the race detector, excluding the end-to-end
     suite: `go test -race -count=1 $(go list ./... | grep -v /bdd/)`
   - the end-to-end suite under `bdd/`, serially (each package starts a
     real `proxiportd` and agent, and parallel runs interfere):
     `go test -count=1 -p 1 ./bdd/...`
   - `golangci-lint run` (CI pins v2.12.2)
   - if you touched `frontend/`: `npm run check && npm run test &&
     npm run build` in that directory
   - if you touched `docs/` or `mkdocs.yml`: `mkdocs build --strict`

   CI runs these and more: govulncheck, agent cross-builds for Windows,
   macOS and the BSDs, and shellcheck plus the packaging tests for the
   shell scripts. A plain `go test ./...` also runs the `bdd/` packages
   in parallel and can fail spuriously, so use the two test commands
   above instead.

For sensitive disclosures see [`SECURITY.md`](SECURITY.md).

## Conduct

The project follows the Contributor Covenant — see
[`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md). The maintainer enforces it.

## Contact

Open an issue on this repository. For private vulnerability reports use
the GitHub Security Advisories flow described in
[`SECURITY.md`](SECURITY.md).
