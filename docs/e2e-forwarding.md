# End-to-end encrypted forwarding

A normal tunnel is encrypted between your machine and the server, and
between the server and the agent, but the server decrypts and re-encrypts
the traffic in between. Whoever controls the server, or the host it runs
on, can read it.

With `[e2e]` enabled, the agent runs a small SSH server on its own
loopback interface. You open an ordinary tunnel to it and run a standard
SSH client through that tunnel. The SSH session is encrypted between your
SSH client and the agent, so the ProxiPort server only relays ciphertext.
You pin the agent's host key yourself, so the server can't substitute
its own key without your SSH client refusing the connection.

The agent's SSH server only does port forwarding (`ssh -L`). It refuses
shells, remote commands and reverse forwards.

## What it protects, and what it doesn't

The session protects the traffic **between your SSH client and the
agent**. The agent makes the last hop to the target itself, on its own
host or LAN, the same way it does for a normal tunnel.

The server can still see:

- that a session exists, when it starts and ends, and how much data
  flows;
- your IP address and the agent's;
- the tunnel it relays, which points at the agent's loopback SSH port.

It cannot see the targets you forward to or the data you send.

!!! warning "Disable the features that let the server act on the host"

    `[remote-commands]`, `[remote-scripts]` and `[file-reception]` let the
    server run programs or write files on the agent's host. A server that
    wants to can use them to copy the agent's host key or add its own key
    to the authorized keys file, and then read e2e sessions too. The agent
    logs a warning at startup when `[e2e]` is enabled together with any of
    them. To rely on e2e against the server itself, disable all three.
    `[remote-commands]` and `[file-reception]` are enabled by default.

## Set it up

### 1. Authorize your SSH key on the agent

Put the public keys allowed to connect in a file on the agent's host, in
the usual `authorized_keys` format:

```bash
sudo install -m 0644 -o root -g root /dev/null /etc/proxiport/e2e_authorized_keys
echo "ssh-ed25519 AAAA... alice@laptop" | sudo tee -a /etc/proxiport/e2e_authorized_keys
```

The agent reads this file on every login attempt, so adding or removing
a key takes effect without a restart. Keep it writable only by root: a
file the agent's own user can write is a file anything running as that
user can add a key to.

### 2. Enable `[e2e]` in the agent config

```toml
[e2e]
  enabled = true
  authorized_keys_file = "/etc/proxiport/e2e_authorized_keys"
  ## Loopback address to listen on. Must be a loopback IP.
  # listen = "127.0.0.1:7222"
  ## Created on first start if missing. Defaults to
  ## <data_dir>/e2e_host_ed25519_key.
  # host_key_file = "/var/lib/proxiport-agent/e2e_host_ed25519_key"

[remote-commands]
  enabled = false

[file-reception]
  enabled = false
```

Restart the agent. On its first start it generates an ed25519 host key
and logs:

```text
info: client: e2e: listening on 127.0.0.1:7222, host key SHA256:...
```

If the agent's `[client] tunnel_allowed` list is set, add the listen
address to it, or the tunnel in step 4 is refused.

### 3. Get the host key over a channel you trust

On the agent's host, print the key:

```bash
sudo proxiport -c /etc/proxiport/proxiport.conf --e2e-fingerprint
```

```text
SHA256:<fingerprint>
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA<rest of the key>
```

Copy the key to your machine through a channel that does not go through
the ProxiPort server: your own SSH or console access to the host, your
configuration management, or reading it off the screen. **Do not fetch it
through ProxiPort**, for example with a remote command: the server
could hand you its own key instead, and the pin would protect nothing.

Add it to a known-hosts file under a name of your choosing:

```bash
echo "web01-e2e ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA<rest of the key>" >> ~/.ssh/known_hosts
```

### 4. Open a tunnel to the agent's SSH port

Create a tunnel to `127.0.0.1:7222` on the agent, in the web UI or
through the API. Restrict it to your own address with `acl`:

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  "https://proxiport.example.com/api/v1/clients/<id>/tunnels?remote=127.0.0.1:7222&acl=203.0.113.7"
```

The response includes the server port the tunnel listens on (`lport`).

### 5. Forward through it

```bash
ssh -N -p <lport> \
  -o HostKeyAlias=web01-e2e -o StrictHostKeyChecking=yes \
  -L 8080:10.0.0.5:80 \
  e2e@proxiport.example.com
```

`HostKeyAlias` makes SSH check the key you pinned in step 3 instead of a
key for the server's name. With `StrictHostKeyChecking=yes`, any other key
stops the connection with `REMOTE HOST IDENTIFICATION HAS CHANGED`. The
user name is not checked; only the key is.

`http://localhost:8080` now reaches `10.0.0.5:80` from the agent's side,
and the ProxiPort server relays only the SSH session. Forwards are
subject to the agent's `tunnel_allowed` list, like normal tunnels.

## Cryptography

The session is SSH. With a current OpenSSH client it negotiates the
hybrid post-quantum key exchange `mlkem768x25519-sha256`. Run `ssh -v` and
look for the `kex: algorithm:` line to confirm.

## Rotating keys

- **Operator keys:** edit the authorized keys file. Changes apply to the
  next login.
- **Host key:** stop the agent, delete the host key file, and start it
  again. It generates a new key, which you pin again as in step 3.
