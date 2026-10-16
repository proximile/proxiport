# Docker reference (development use)

This directory holds the Dockerfile + supervisord/jail configs originally
written for the upstream rport Docker image. They are kept here as a **reference**
for putting together a ProxiPort container — they have not yet been rewired
for ProxiPort's module path, image name, or branding.

Outstanding work before this image is shippable:

- Replace the upstream image-name references with a ProxiPort image name.
- Build the binary from the Go source in this repo rather than fetching from
  upstream proxiport release artifacts.
- Drop the bundled Vue.js frontend download and serve the SvelteKit build
  from `frontend/` instead; the proprietary one must not be re-pulled.
- Re-evaluate fail2ban + iptables defaults; the tunnels need them tuned.
- Re-test the supervisord lifecycle (proxiportd + guacd + fail2ban) under the
  stripped server (no Plus alerting, no license checks).
