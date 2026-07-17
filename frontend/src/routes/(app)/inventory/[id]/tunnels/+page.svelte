<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet, apiPut, apiPost, apiDelete, ApiException } from '$lib/api';
  import type { Client, Tunnel } from '$lib/types';
  import { fmtRelative } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';
  import { copyToClipboard, sshCommandFor } from '$lib/clipboard';

  let client: Client | null = $state(null);
  let stored: any[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let creating = $state(false);

  // Hostname users will type into their browser/SSH client to reach a tunnel.
  // Server-side lhost is typically 0.0.0.0 (bind-all), which is useless to
  // surface — show the host this SPA was loaded from instead.
  const serverHost = typeof window !== 'undefined' ? window.location.hostname : '';

  // ---- Step 1: Service ----------------------------------------------------
  let service = $state<'ssh' | 'rdp' | 'vnc' | 'realvnc' | 'http' | 'https' | 'other' | 'forwarding'>('ssh');
  let subService = $state<'ssh' | 'rdp' | 'vnc' | 'realvnc' | 'http' | 'https' | 'other'>('ssh');
  let remotePort = $state<number | undefined>(22);
  let destHost = $state('');

  // TLS reverse proxy in front of the tunnel — server terminates TLS using
  // its own configured cert, then forwards plaintext to the client. Replaces
  // the per-tunnel "cert pass" notion: ProxiPort doesn't ship per-tunnel
  // certs, the server-wide cert covers all proxied tunnels.
  let tlsProxy = $state(false);
  let tlsHostname = $state('');

  // Explicit acknowledgement required before opening a plaintext-HTTP tunnel
  // (scheme http with no TLS). See insecureHttp() for the rationale.
  let allowInsecureHttp = $state(false);

  const DEFAULT_PORTS: Record<string, number> = {
    ssh: 22, rdp: 3389, vnc: 5900, realvnc: 5900, http: 80, https: 443, other: 0
  };

  let lastDefaultPort = $state(22);
  function onServiceChange() {
    const effective = service === 'forwarding' ? subService : service;
    const next = DEFAULT_PORTS[effective] ?? 0;
    if (remotePort === lastDefaultPort) remotePort = next;
    lastDefaultPort = next;
    if (effective !== 'http' && effective !== 'https') tlsProxy = false;
  }

  function effectiveScheme(): string {
    return service === 'forwarding' ? subService : service;
  }

  function tlsProxyAvailable(): boolean {
    const s = effectiveScheme();
    return s === 'http' || s === 'https';
  }

  // A plain http:// tunnel with no TLS in front of it: the traffic between the
  // server and the browser is unencrypted, so anyone on the network path can
  // read URLs, headers, cookies and page contents. It counts as secure only
  // when the server terminates TLS with its own cert (tlsProxy) or the target
  // already speaks HTTPS end to end (https scheme). Requires an explicit
  // acknowledgement to create, just like opening a tunnel to any IP.
  function insecureHttp(): boolean {
    return effectiveScheme() === 'http' && !tlsProxy;
  }

  // Drop a stale acknowledgement whenever the tunnel is no longer plaintext
  // HTTP (scheme changed, or TLS termination enabled), so re-entering the
  // insecure state always requires ticking the box again.
  $effect(() => {
    if (!insecureHttp()) allowInsecureHttp = false;
  });

  // ---- Step 2: Public port ------------------------------------------------
  let publicPortMode = $state<'random' | 'specify'>('random');
  let publicPort = $state<number | undefined>(undefined);

  // ---- Step 3: ACL --------------------------------------------------------
  let aclMode = $state<'current' | 'specific' | 'anyone'>('current');
  let aclIp = $state('');
  let myIp = $state('');
  let myIpError = $state('');

  async function loadMyIp() {
    myIpError = '';
    try {
      const r = await apiGet<{ ip?: string } | string>('/me/ip');
      const ip = typeof r === 'string' ? r : (r?.ip ?? '');
      myIp = ip;
      if (!ip) myIpError = 'The server could not determine your IP address.';
      if (aclMode === 'current') aclIp = ip;
    } catch (err) {
      // Surface the failure: with "Only my current IP" selected, an empty IP
      // must block tunnel creation (see formValid) rather than silently drop
      // the ACL and open the tunnel to everyone.
      myIpError = err instanceof Error ? err.message : 'Failed to look up your current IP address.';
    }
  }

  function onAclModeChange() {
    if (aclMode === 'current') aclIp = myIp;
    else if (aclMode === 'anyone') aclIp = '';
  }

  // ---- Step 4: Timeouts ---------------------------------------------------
  let idleEnabled = $state(true);
  let idleMinutes = $state(5);
  let destroyEnabled = $state(false);
  let destroyHours = $state(2);
  let destroyMinutes = $state(30);

  // The server normally refuses TCP tunnels to a remote port nothing is
  // listening on. check_port=0 skips that probe — needed when the target
  // service starts later or only listens once traffic arrives.
  let skipPortCheck = $state(false);

  // ---- Library ------------------------------------------------------------
  let storeInLibrary = $state(false);
  let libraryName = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    try {
      const [c, s] = await Promise.allSettled([
        apiGet<Client>(`/clients/${id}`),
        apiGet<any[]>(`/clients/${id}/stored-tunnels`).catch(() => [])
      ]);
      if (c.status === 'fulfilled') client = c.value;
      if (s.status === 'fulfilled') stored = s.value ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = $page.params.id;
    if (id) {
      load(id);
      loadMyIp();
    }
  });

  // ---- Build API params ---------------------------------------------------
  function buildRemote(): string {
    if (service === 'forwarding') {
      if (!destHost) return String(remotePort);
      return `${destHost}:${remotePort}`;
    }
    return String(remotePort);
  }

  function buildAcl(): string {
    if (aclMode === 'anyone') return '';
    if (aclMode === 'current') return aclIp ? `${aclIp}/32` : '';
    return aclIp;
  }

  function buildAutoClose(): string {
    if (!destroyEnabled) return '';
    const h = Math.max(0, destroyHours || 0);
    const m = Math.max(0, destroyMinutes || 0);
    if (h === 0 && m === 0) return '';
    const parts: string[] = [];
    if (h > 0) parts.push(`${h}h`);
    if (m > 0) parts.push(`${m}m`);
    return parts.join('');
  }

  function formValid(): string {
    const p = Number(remotePort);
    if (!p || p < 1 || p > 65535) return 'Remote port must be 1–65535.';
    if (service === 'forwarding' && !destHost.trim()) return 'Destination host is required for service forwarding.';
    if (publicPortMode === 'specify') {
      const pp = Number(publicPort);
      if (!pp || pp < 1 || pp > 65535) return 'Public port must be 1–65535.';
    }
    if (aclMode === 'specific' && !aclIp.trim()) return 'CIDR is required for a specific ACL.';
    // Fail closed: "Only my current IP" with no resolved IP would otherwise
    // build an empty ACL and create a tunnel open to everyone.
    if (aclMode === 'current' && !aclIp.trim()) {
      return 'Could not determine your current IP address. Retry, or choose a specific range or "No restrictions".';
    }
    // Fail closed on plaintext HTTP: force the user to acknowledge the exposure
    // (or enable server TLS / use HTTPS) before the tunnel can be created.
    if (insecureHttp() && !allowInsecureHttp) {
      return 'This tunnel would serve plaintext http:// with no TLS. Tick the acknowledgement below, enable server TLS termination, or use an HTTPS service.';
    }
    return '';
  }

  // Local URL we show in the active-tunnels table — the address users
  // actually type. window.location.hostname plus the server-assigned lport.
  function localUrl(t: Tunnel): string {
    const host = serverHost || t.lhost || '';
    return `${host}:${t.lport}`;
  }

  function isSsh(t: Tunnel): boolean {
    return (t.scheme || t.protocol) === 'ssh';
  }

  // The remote endpoint the tunnel forwards to on the client side.
  function remoteUrl(t: Tunnel): string {
    const r = (t as any).rport ?? '';
    const h = t.rhost ?? '';
    return `${h}:${r}`;
  }

  async function createTunnel(e: Event) {
    e.preventDefault();
    const id = $page.params.id;
    if (!id) return;
    const why = formValid();
    if (why) {
      error = why;
      return;
    }
    creating = true;
    error = '';
    try {
      const params = new URLSearchParams();
      params.set('remote', buildRemote());
      const sch = effectiveScheme();
      if (sch && sch !== 'other') params.set('scheme', sch);
      if (publicPortMode === 'specify' && publicPort) {
        params.set('local', String(publicPort));
      }
      const acl = buildAcl();
      if (acl) params.set('acl', acl);
      if (idleEnabled && idleMinutes > 0) {
        params.set('idle-timeout-minutes', String(idleMinutes));
      } else if (!idleEnabled) {
        params.set('skip-idle-timeout', '1');
      }
      const autoClose = buildAutoClose();
      if (autoClose) params.set('auto-close', autoClose);
      if (tlsProxy && tlsProxyAvailable()) {
        params.set('http_proxy', 'true');
        if (tlsHostname.trim()) params.set('host_header', tlsHostname.trim());
      }
      // Required server-side override for a plaintext-HTTP tunnel; formValid()
      // guarantees the user has acknowledged the exposure before we get here.
      if (insecureHttp()) params.set('allow_insecure_http', 'true');
      if (skipPortCheck) params.set('check_port', '0');

      await apiPut(`/clients/${id}/tunnels?${params}`);

      if (storeInLibrary) {
        try {
          const body: Record<string, unknown> = {
            name: libraryName || `${sch} ${buildRemote()}`,
            client_id: id,
            remote_port: Number(remotePort),
            scheme: sch
          };
          if (service === 'forwarding' && destHost) body.remote_host = destHost;
          if (publicPortMode === 'specify' && publicPort) body.local_port = Number(publicPort);
          if (acl) body.acl = acl;
          if (autoClose) body.auto_close = autoClose;
          if (tlsProxy && tlsProxyAvailable()) {
            body.http_proxy = true;
            if (tlsHostname.trim()) body.host_header = tlsHostname.trim();
          }
          await apiPost(`/clients/${id}/stored-tunnels`, body);
        } catch (libErr) {
          pushToast('warn', 'Tunnel created, but couldn\'t save to library: ' +
                    (libErr instanceof Error ? libErr.message : String(libErr)));
        }
      }

      pushToast('good', 'Tunnel created.');
      await load(id);
    } catch (err) {
      error = err instanceof ApiException
        ? err.errors[0]?.title || err.message
        : (err instanceof Error ? err.message : String(err));
    } finally {
      creating = false;
    }
  }

  async function deleteTunnel(tid: string) {
    const id = $page.params.id;
    if (!id) return;
    if (!confirm(`Delete tunnel ${tid}?`)) return;
    try {
      await apiDelete(`/clients/${id}/tunnels/${tid}`);
      pushToast('good', 'Tunnel deleted.');
      await load(id);
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }

  $effect(() => {
    void service; void subService;
    onServiceChange();
  });

  $effect(() => {
    void aclMode;
    onAclModeChange();
  });

  const SERVICE_OPTIONS: Array<[string, string]> = [
    ['ssh', 'SSH'],
    ['rdp', 'Remote Desktop (RDP)'],
    ['vnc', 'VNC'],
    ['realvnc', 'RealVNC'],
    ['http', 'HTTP'],
    ['https', 'HTTPS'],
    ['other', 'Any Service'],
    ['forwarding', 'Service Forwarding']
  ];
  const SUB_SERVICE_OPTIONS: Array<[string, string]> = SERVICE_OPTIONS.slice(0, -1);
</script>

<div class="space-y-6">
  <ErrorBox message={error} />

  <div class="card">
    <div class="px-4 py-3 border-b border-pp-border flex items-center justify-between">
      <h2 class="font-medium">New tunnel</h2>
      {#if client?.connection_state !== 'connected'}
        <span class="pill pill-warn">client offline</span>
      {/if}
    </div>

    <form onsubmit={createTunnel} class="p-4 space-y-6">
      <!-- Step 1: Service + destination + remote port -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">1</div>
        <div class="flex-1 space-y-3">
          <div class="text-sm">Service to access on the remote site</div>

          <div class="grid grid-cols-1 md:grid-cols-3 gap-x-6 gap-y-3">
            <label class="text-xs md:col-span-2">
              <span class="block text-slate-400 mb-1">Service</span>
              <select bind:value={service}>
                {#each SERVICE_OPTIONS as [v, label]}
                  <option value={v}>{label}</option>
                {/each}
              </select>
            </label>

            {#if service === 'forwarding'}
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Sub-service</span>
                <select bind:value={subService}>
                  {#each SUB_SERVICE_OPTIONS as [v, label]}
                    <option value={v}>{label}</option>
                  {/each}
                </select>
              </label>
            {:else}
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Port</span>
                <input type="number" min="1" max="65535" bind:value={remotePort} class="font-mono" required />
              </label>
            {/if}
          </div>

          {#if service === 'forwarding'}
            <div class="grid grid-cols-1 md:grid-cols-3 gap-x-6 gap-y-3">
              <label class="text-xs md:col-span-2">
                <span class="block text-slate-400 mb-1">Destination IP / Hostname</span>
                <input bind:value={destHost} placeholder="192.168.178.1" class="font-mono" />
              </label>
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Port</span>
                <input type="number" min="1" max="65535" bind:value={remotePort} class="font-mono" required />
              </label>
            </div>
          {/if}

          {#if tlsProxyAvailable()}
            <div class="pt-3 border-t border-pp-border space-y-2">
              <label class="text-xs flex items-center gap-2 cursor-pointer">
                <input type="checkbox" bind:checked={tlsProxy} />
                <span>Terminate TLS on the server using the server's certificate</span>
              </label>
              {#if tlsProxy}
                <label class="text-xs block max-w-md">
                  <span class="block text-slate-400 mb-1">Hostname (Host header forwarded to the client)</span>
                  <input bind:value={tlsHostname} placeholder="intranet.local" class="font-mono w-full" />
                </label>
              {/if}
              {#if insecureHttp()}
                <div class="pt-2 space-y-1">
                  <label class="text-xs flex items-center gap-2 cursor-pointer text-amber-400">
                    <input type="checkbox" bind:checked={allowInsecureHttp} />
                    <span>Allow insecure <span class="font-mono">http://</span> without TLS</span>
                  </label>
                  <p class="text-xs text-amber-400/80 max-w-xl">
                    Plain <span class="font-mono">http://</span> traffic between the server and your browser is
                    <strong>unencrypted</strong> — anyone able to observe the network path can read the request
                    URLs, headers, cookies and page contents. Prefer “Terminate TLS on the server” above, or use an
                    HTTPS service. Only tick this for a trusted, private network.
                  </p>
                </div>
              {/if}
            </div>
          {/if}
        </div>
      </div>

      <!-- Step 2: Public port -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">2</div>
        <div class="flex-1">
          <div class="text-sm">Public port</div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-x-6 gap-y-3">
            <label class="text-xs md:col-span-2">
              <span class="block text-slate-400 mb-1">Mode</span>
              <select bind:value={publicPortMode}>
                <option value="random">Random free port</option>
                <option value="specify">Specific port</option>
              </select>
            </label>
            {#if publicPortMode === 'specify'}
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Port</span>
                <input type="number" min="1" max="65535" bind:value={publicPort} class="font-mono" placeholder="20000" required />
              </label>
            {/if}
          </div>
        </div>
      </div>

      <!-- Step 3: ACL -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">3</div>
        <div class="flex-1">
          <div class="text-sm">ACL — who is allowed to use the tunnel</div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-x-6 gap-y-3">
            <label class="text-xs md:col-span-2">
              <span class="block text-slate-400 mb-1">Mode</span>
              <select bind:value={aclMode}>
                <option value="current">Only my current IP address</option>
                <option value="specific">Specific network range</option>
                <option value="anyone">No restrictions (anyone can access)</option>
              </select>
            </label>
            {#if aclMode !== 'anyone'}
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">
                  {aclMode === 'specific' ? 'CIDR' : 'IP address'}
                </span>
                <input bind:value={aclIp}
                       readonly={aclMode !== 'specific'}
                       placeholder={aclMode === 'specific' ? '192.168.1.0/24' : ''}
                       class="font-mono" />
              </label>
            {/if}
            {#if aclMode === 'current' && !aclIp.trim()}
              <p class="md:col-span-3 text-xs text-rose-400">
                Couldn’t determine your current IP address{myIpError ? ` — ${myIpError}` : ''}. The tunnel can’t be
                restricted to it. <button type="button" class="underline" onclick={loadMyIp}>Retry</button>,
                or choose a specific range or “No restrictions”.
              </p>
            {/if}
          </div>
        </div>
      </div>

      <!-- Step 4: Timeouts -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">4</div>
        <div class="flex-1 space-y-3">
          <div class="text-sm">Timeouts</div>
          <div class="grid grid-cols-1 md:grid-cols-4 gap-x-6 gap-y-3 items-end">
            <label class="text-xs md:col-span-2 flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={idleEnabled} />
              <span>Close tunnel after inactivity of</span>
            </label>
            <label class="text-xs md:col-start-4" class:opacity-50={!idleEnabled}>
              <span class="block text-slate-400 mb-1">Minutes</span>
              <input type="number" min="1" bind:value={idleMinutes} disabled={!idleEnabled} class="font-mono" />
            </label>
          </div>
          <div class="grid grid-cols-1 md:grid-cols-4 gap-x-6 gap-y-3 items-end">
            <label class="text-xs md:col-span-2 flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={destroyEnabled} />
              <span>Destroy tunnel after</span>
            </label>
            <label class="text-xs" class:opacity-50={!destroyEnabled}>
              <span class="block text-slate-400 mb-1">Hours</span>
              <input type="number" min="0" bind:value={destroyHours} disabled={!destroyEnabled} class="font-mono" />
            </label>
            <label class="text-xs" class:opacity-50={!destroyEnabled}>
              <span class="block text-slate-400 mb-1">Minutes</span>
              <input type="number" min="0" max="59" bind:value={destroyMinutes} disabled={!destroyEnabled} class="font-mono" />
            </label>
          </div>
          <label class="text-xs flex items-center gap-2 cursor-pointer">
            <input type="checkbox" bind:checked={skipPortCheck} />
            <span>Skip the remote port check (create the tunnel even if nothing is listening on the port yet)</span>
          </label>
        </div>
      </div>

      <!-- Library -->
      <div class="flex items-start gap-3 pt-2 border-t border-pp-border">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-slate-700 text-sm text-slate-300">★</div>
        <div class="flex-1">
          <div class="grid grid-cols-1 md:grid-cols-3 gap-x-6 gap-y-3 items-end">
            <label class="text-xs md:col-span-2 flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={storeInLibrary} />
              <span>Also save these settings to the stored-tunnels library</span>
            </label>
            {#if storeInLibrary}
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Library name (optional)</span>
                <input bind:value={libraryName} placeholder="auto" />
              </label>
            {/if}
          </div>
        </div>
      </div>

      <div class="flex justify-end">
        <button class="btn btn-primary" type="submit"
                disabled={creating || client?.connection_state !== 'connected'}>
          {#if creating}<Spinner />{:else}Create tunnel{/if}
        </button>
      </div>
    </form>
  </div>

  <div class="card overflow-x-auto">
    <div class="px-4 py-2 border-b border-pp-border text-sm text-slate-400">Active tunnels</div>
    {#if loading && !client}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !client?.tunnels?.length}
      <EmptyState title="No active tunnels" />
    {:else}
      <table class="tbl">
        <thead>
          <tr><th>ID</th><th>Public URL</th><th>Remote</th><th>Scheme</th><th>ACL</th><th>Idle</th><th>Created</th><th></th></tr>
        </thead>
        <tbody>
          {#each client.tunnels as t}
            <tr>
              <td class="font-mono">{t.id}</td>
              <td class="font-mono text-emerald-300">{localUrl(t)}</td>
              <td class="font-mono text-slate-300">{remoteUrl(t)}</td>
              <td><span class="pill pill-info">{t.scheme || t.protocol || '—'}</span>{#if t.http_proxy}<span class="pill pill-info ml-1">TLS</span>{/if}</td>
              <td class="font-mono text-xs text-slate-400">{t.acl || '—'}</td>
              <td class="text-slate-400">{t.idle_timeout_minutes ?? '—'}m</td>
              <td class="text-slate-400">{fmtRelative(t.created_at)}</td>
              <td class="whitespace-nowrap">
                {#if isSsh(t)}
                  <button class="btn btn-ghost" title="Copy ssh command"
                    onclick={() => copyToClipboard(sshCommandFor(serverHost || t.lhost || '', t.lport), 'SSH command copied.')}>Copy SSH</button>
                {:else}
                  <button class="btn btn-ghost" title="Copy public address"
                    onclick={() => copyToClipboard(localUrl(t), 'Address copied.')}>Copy</button>
                {/if}
                <button class="btn btn-danger" onclick={() => deleteTunnel(String(t.id))}>Delete</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  <div class="card overflow-x-auto">
    <div class="px-4 py-2 border-b border-pp-border text-sm text-slate-400">
      Stored tunnels (templates for one-click recreation)
    </div>
    {#if !stored.length}
      <EmptyState title="No stored tunnels yet" />
    {:else}
      <table class="tbl">
        <thead>
          <tr><th>Name</th><th>Local</th><th>Remote</th><th>Scheme</th><th>ACL</th><th>Auto-close</th></tr>
        </thead>
        <tbody>
          {#each stored as s}
            <tr>
              <td>{s.name || '—'}</td>
              <td class="font-mono">{s.local_host ?? ''}:{s.local_port ?? ''}</td>
              <td class="font-mono">{s.remote_host ?? ''}:{s.remote_port ?? ''}</td>
              <td>{s.scheme || '—'}</td>
              <td class="font-mono text-xs">{s.acl || '—'}</td>
              <td>{s.auto_close ?? '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
