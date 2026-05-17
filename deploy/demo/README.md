# Demo deploy

Builds the local tree (proxiportd binary + SvelteKit SPA) and deploys
it to an existing Linux host. Intended for one-shot evaluation
installs that can be torn down and recreated cheaply.

## Prerequisites on the deploy operator's workstation

- `go` 1.22+ on `PATH`
- `node` 22+ and `npm`
- `openssl`
- `ssh` / `scp` and an SSH key authorised on the target

## Prerequisites on the target host

- Linux with `systemd`
- `sudo` available to the SSH user
- DNS A record for `$HOST` already pointing at this machine
- Ports 80/443 open externally; `$used_ports` range (default
  `20000-20099`) open if you intend to expose tunnel endpoints

## Usage

```bash
export HOST=demo.example.com
export EMAIL=ops@example.com
export SSH_TARGET=root@demo.example.com
# Optional: SSH_KEY=~/.ssh/id_ed25519
deploy/demo/deploy.sh
```

The script:

1. Builds `proxiportd` for `linux/amd64`.
2. Builds the SvelteKit SPA.
3. Renders `proxiportd.conf` from
   [`proxiportd.conf.template`](proxiportd.conf.template) with fresh
   random credentials.
4. Stages everything under `/tmp/proxiport-demo` on the target.
5. Runs [`deploy/server/install.sh`](../server/install.sh) remotely
   (installs the systemd unit, issues a Let's Encrypt cert via
   certbot, drops the SPA into `/var/lib/proxiport/docroot`).
6. Starts `proxiportd` and prints the systemd status.

Re-run any time to redeploy the latest local tree. Existing certs
are reused; the admin/client-auth passwords are regenerated on
every run.

## Dry-run

```bash
DRY_RUN=1 deploy/demo/deploy.sh
```

Runs the local build (unless `SKIP_BUILD=1` is also set), renders
the config, and prints the remote commands without executing
them. Useful when you want to see what the run will do before
committing to it.

## Credentials

After a successful run, the generated admin and client-auth
passwords are written to
`build/demo-stage/.demo-credentials` (mode 0600) on the operator's
workstation. They are not transmitted back from the server. Keep
that file out of version control.
