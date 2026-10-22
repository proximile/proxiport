# ProxiPort frontend

The OSS web UI for ProxiPort. Originally written for ProxiPort, no
upstream history. SvelteKit 2 + Svelte 5 + TypeScript + Tailwind 4.
Licensed under AGPL-3.0-or-later along with the rest of the project
(see `../LICENSE`).

## Quick start

```bash
npm install
npm run dev      # vite dev server on 0.0.0.0:5173 (all interfaces);
                 # /api is proxied to $PROXIPORT_DEV_API
                 # (default http://127.0.0.1:8000, see vite.config.ts)
npm run build    # static SPA into ./build/
npm run check    # svelte-check + tsc
npm run test     # vitest unit tests (npm run test:watch to re-run on change)
```

A `proxiportd` running the seeded config listens for the API on
`127.0.0.1:3000`, so point the dev server at it with
`PROXIPORT_DEV_API=http://127.0.0.1:3000 npm run dev`.

The dev server listens on all interfaces, so other machines on your
network can reach it and, through its `/api` proxy, the backend. Run
`npm run dev -- --host 127.0.0.1` to keep it local.

## Architecture

Static SPA via `@sveltejs/adapter-static` with `fallback: 'index.html'`.
Build output is plain HTML/JS/CSS that gets served from the ProxiPort
server's `doc_root` directory.

- `src/lib/api.ts` — tiny fetch wrapper that injects the JWT, surfaces
  errors as throws, and clears the session token on 401 (except on the
  vault-unlock endpoint, where 401 means a wrong passphrase); the `(app)`
  layout then redirects to `/auth`.
- `src/lib/types.ts` — shared TypeScript types matching the
  `/api/v1/...` JSON shapes.
- `src/lib/stores.ts` — Svelte stores for auth, vault, and toasts.
- `src/lib/components/` — reusable UI bits (TopBar, Sidebar, KV pairs,
  EmptyState, ErrorBox, VaultLocked, Spinner, Toasts).
- `src/routes/` — file-system router. `(app)` is the post-auth
  layout group; `auth/` is outside it.

## Deployment

`npm run build` writes the static site to `build/`; the release packages
ship that directory as `/usr/share/proxiport/docroot` and the server
serves it from `[api] doc_root`. See [install](../docs/install.md) for
the package and tarball layouts.
