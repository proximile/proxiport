# Docker reference (development use)

This directory holds the Dockerfile + supervisord/jail configs originally
written for `iotech17/proxiport-docker`. They are kept here as a **reference**
for putting together a ProxiPort container — they have not yet been rewired
for ProxiPort's module path, image name, or branding.

Outstanding work before this image is shippable:

- Replace `iotech17/proxiport-docker` references with a ProxiPort image name.
- Build the binary from the Go source in this repo rather than fetching from
  upstream proxiport release artifacts.
- Drop the bundled Vue.js frontend download — there is no frontend in this
  repo yet, and the proprietary one shouldn't be re-pulled.
- Re-evaluate fail2ban + iptables defaults; the tunnels need them tuned.
- Re-test the supervisord lifecycle (proxiportd + guacd + fail2ban) under the
  stripped server (no Plus alerting, no license checks).

The unmodified upstream repo lives at `repos/proxiport-docker/` in the
workspace if you need the original context.
