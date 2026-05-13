# ProxiPort frontend

The OSS web UI for ProxiPort. Originally written for ProxiPort, no
upstream history. SvelteKit 2 + Svelte 5 + TypeScript + Tailwind 4.
Licensed under AGPL-3.0-or-later along with the rest of the project
(see `../LICENSE`).

## Quick start

```bash
npm install
npm run dev      # vite dev server on :5173, /api proxied to the
                 # target configured in vite.config.ts
npm run build    # static SPA into ./build/
npm run check    # svelte-check + tsc
```

## Architecture

Static SPA via `@sveltejs/adapter-static` with `fallback: 'index.html'`.
Build output is plain HTML/JS/CSS that gets served from the ProxiPort
server's `doc_root` directory.

- `src/lib/api.ts` — tiny fetch wrapper that injects the JWT, surfaces
  errors as throws, and triggers a redirect to `/auth` on 401.
- `src/lib/types.ts` — shared TypeScript types matching the
  `/api/v1/...` JSON shapes.
- `src/lib/stores.ts` — Svelte stores for auth, vault, and toasts.
- `src/lib/components/` — reusable UI bits (TopBar, Sidebar, KV pairs,
  EmptyState, ErrorBox, VaultLocked, Spinner, Toasts).
- `src/routes/` — file-system router. `(app)` is the post-auth
  layout group; `auth/` is outside it.

## Deployment

See [`../docs/SPA.md`](../docs/SPA.md) for build, ship, and deploy
steps including the `tar | scp | tar -x` pattern used against the
eval host.
