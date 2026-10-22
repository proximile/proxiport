<!--
Thanks for sending a pull request.

A few quick checks before opening:

- [ ] The core CI checks pass locally (see CONTRIBUTING.md for the full
      list): `gofmt -s -l` lists no hand-written files, `go vet ./...`, the unit tests
      (`go test -race` excluding `bdd/`), the BDD suite run serially
      (`go test -p 1 ./bdd/...`), `golangci-lint run`, and in `frontend/`
      `npm run check && npm run test && npm run build`;
      `mkdocs build --strict` if you touched the docs.
- [ ] (Optional) Commits are signed off (`git commit -s`). A DCO trailer
      is welcome but not required.
- [ ] You agree your contribution is licensed under AGPL-3.0-or-later
      (inbound = outbound — see CONTRIBUTING.md).
-->

## What this changes



## Why

<!-- The user-facing problem this solves, not a restatement of the diff. -->

## How to test

<!-- Commands or steps a reviewer can run to verify the change. -->

## Related issues

<!-- Closes #..., refs #..., etc. -->
