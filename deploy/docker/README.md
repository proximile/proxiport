# Docker reference (development use)

This directory holds a Dockerfile and supervisord/fail2ban configs ported
from the upstream rport Docker image. They already use ProxiPort names and
paths: the `proxiportd` binary, the `proxiport` user, `/var/lib/proxiport`,
and a `proxiportd-client-connect` fail2ban jail. The SvelteKit frontend is
built from `frontend/` in the `frontend-builder` stage and copied to
`/var/www/html/`. The image is kept here as a **reference** for putting
together a ProxiPort container and is not yet shippable.

Outstanding work before this image is shippable:

- Build `proxiportd` from this repo's Go source in a builder stage instead
  of downloading a release tarball. The current download step is broken:
  it requests `proxiportd_<ver>_Linux_x86_64.tar.gz`, but releases name the
  asset `proxiportd_<ver>_linux_x86_64.tar.gz`, and the default
  `proxiport_version` (0.1.0) is long out of date.
- Ship a server config: supervisord starts
  `proxiportd -c /etc/proxiport/proxiportd.conf`, but the image does not
  create that file, so the server exits on start. Copy one in, mount one,
  or generate one when the container starts.
- Set `[api] doc_root = "/var/www/html"` in that config so `proxiportd`
  serves the SvelteKit build the image copies there.
- Settle the build context: the `frontend-builder` stage copies
  `frontend/` (repository root as context) while the final stage copies
  `supervisord.conf` from this directory.
- Decide whether the image should run fail2ban. If it should, install
  fail2ban and iptables, copy `jail.conf` and `defaults-debian.conf` into
  the image, add a fail2ban program to `supervisord.conf`, and tune the
  defaults for the tunnels. The image does none of this today.
- Re-test the supervisord lifecycle (proxiportd + guacd) under the
  stripped server (no Plus alerting, no license checks).
