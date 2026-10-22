# Demo deploy

Builds the local tree (proxiportd binary + SvelteKit SPA) and deploys
it to an existing Linux host. Intended for one-shot evaluation
installs that can be torn down and recreated cheaply.

## Prerequisites on the deploy operator's workstation

- `go` 1.26+ on `PATH` (the version `go.mod` requires)
- `node` 22+ and `npm`
- `openssl`
- `ssh` / `scp` and an SSH key authorised on the target
- a C compiler (`gcc` or `cc`) targeting linux/amd64: `proxiportd` is
  built with `CGO_ENABLED=1` for its embedded SQLite (not needed with
  `SKIP_BUILD`)

## Prerequisites on the target host

- Debian or Ubuntu (apt-based) on x86_64 (amd64), with `systemd`;
  [`deploy/server/install.sh`](../server/install.sh) installs `certbot`
  and `acl` with `apt-get`
- `rsync` installed (it copies the SPA into
  `/usr/share/proxiport/docroot`, and `install.sh` does not install it)
- Passwordless (`NOPASSWD`) `sudo` for the SSH user, or log in as root.
  The script runs `sudo` over a non-interactive SSH session, so it
  cannot prompt for a password.
- DNS A record for `$HOST` already pointing at this machine
- Ports 80/443 open externally; `$used_ports` range (default
  `20000-20099`) open if you intend to expose tunnel endpoints

## Usage

```bash
export HOST=demo.example.com
export EMAIL=ops@example.com
export SSH_TARGET=root@demo.example.com
# Optional: SSH_KEY=~/.ssh/id_ed25519
# Optional: STAGE_DIR=./build/demo-stage (local build/stage dir; relative to the current directory)
# Optional: REMOTE_STAGE=/tmp/proxiport-demo (stage dir on the target)
deploy/demo/deploy.sh
```

Run the script from the repository root. `STAGE_DIR` defaults to
`build/demo-stage` under the current working directory, and that is
also where `.demo-credentials` is written.

The script:

1. Builds `proxiportd` for `linux/amd64`.
2. Builds the SvelteKit SPA.
3. Renders `proxiportd.conf` from
   [`proxiportd.conf.template`](proxiportd.conf.template) with fresh
   random credentials.
4. Stages everything under `$REMOTE_STAGE` (default
   `/tmp/proxiport-demo`) on the target.
5. Runs [`deploy/server/install.sh`](../server/install.sh) remotely
   (installs the systemd unit, issues a Let's Encrypt cert via
   certbot).
6. Copies the SPA into the root-owned `/usr/share/proxiport/docroot`
   (`rsync --delete`, root 0755/0644), then restarts `proxiportd` and
   prints the systemd status.

Re-run any time to redeploy the latest local tree. Existing certs
are reused. Every run regenerates the admin and client-auth
passwords, and also `key_seed` and `jwt_secret`. A new `key_seed`
changes the server's SSH host-key fingerprint, so agents that pin
`fingerprint` must be re-pinned to the new SHA-256 value that
`proxiportd` logs at startup. A new `jwt_secret` logs out every
existing session.

## Dry-run

```bash
DRY_RUN=1 deploy/demo/deploy.sh
```

Runs the local build (unless `SKIP_BUILD=1` is also set), renders
the config, and prints the remote commands without executing
them. Useful when you want to see what the run will do before
committing to it. A dry run still regenerates `.demo-credentials`.

## Credentials

Each run writes the generated admin and client-auth passwords to
`$STAGE_DIR/.demo-credentials` (default
`build/demo-stage/.demo-credentials`, mode 0600) under the directory
you run the script from. The file is written before any remote step,
so a dry run (`DRY_RUN=1`) or a run that fails partway also replaces
it, with passwords that were never deployed. Copy the file somewhere
safe after a successful deploy. The passwords are not transmitted
back from the server. Keep that file out of version control.
