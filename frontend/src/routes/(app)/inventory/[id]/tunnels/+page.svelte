<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet, apiPut, apiPost, apiDelete, ApiException } from '$lib/api';
  import type { Client, Tunnel } from '$lib/types';
  import { fmtRelative } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  let client: Client | null = $state(null);
  let stored: any[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let creating = $state(false);

  // ---- Step 1: Service ----------------------------------------------------
  // Outer service preset. When set to 'forwarding', the form reveals a
  // sub-service picker plus a Destination IP/Hostname input — that's the
  // openrport "Service Forwarding" UX, where the tunnel terminates on a
  // host *behind* the client, not on the client itself.
  let service = $state<'ssh' | 'rdp' | 'vnc' | 'realvnc' | 'http' | 'https' | 'other' | 'forwarding'>('ssh');
  let subService = $state<'ssh' | 'rdp' | 'vnc' | 'realvnc' | 'http' | 'https' | 'other'>('ssh');
  let remotePort = $state(22);
  let destHost = $state('');
  let storeInLibrary = $state(false);
  let libraryName = $state('');

  const DEFAULT_PORTS: Record<string, number> = {
    ssh: 22, rdp: 3389, vnc: 5900, realvnc: 5900, http: 80, https: 443, other: 0
  };

  // Update remote port default when service changes (but don't clobber if user
  // already edited it manually away from the previous default).
  let lastDefaultPort = $state(22);
  function onServiceChange() {
    const effective = service === 'forwarding' ? subService : service;
    const next = DEFAULT_PORTS[effective] ?? 0;
    if (remotePort === lastDefaultPort) remotePort = next;
    lastDefaultPort = next;
  }

  // ---- Step 2: Public port ------------------------------------------------
  let publicPortMode = $state<'random' | 'specify'>('random');
  let publicPort = $state<number | ''>('');

  // ---- Step 3: ACL --------------------------------------------------------
  let aclMode = $state<'current' | 'specific' | 'anyone'>('current');
  let aclIp = $state(''); // populated for 'current' via /me/ip; freeform for 'specific'
  let myIp = $state('');

  async function loadMyIp() {
    try {
      const r = await apiGet<{ ip?: string } | string>('/me/ip');
      const ip = typeof r === 'string' ? r : (r?.ip ?? '');
      myIp = ip;
      if (aclMode === 'current') aclIp = ip;
    } catch {
      // best-effort
    }
  }

  function onAclModeChange() {
    if (aclMode === 'current') aclIp = myIp;
    else if (aclMode === 'anyone') aclIp = '';
    // 'specific' keeps whatever the user typed
  }

  // ---- Step 4: Timeouts ---------------------------------------------------
  let idleEnabled = $state(true);
  let idleMinutes = $state(5);
  let destroyEnabled = $state(false);
  let destroyHours = $state(2);
  let destroyMinutes = $state(30);

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
  function effectiveScheme(): string {
    return service === 'forwarding' ? subService : service;
  }

  function buildRemote(): string {
    // Forwarding: <host>:<port>. Other: just the port (server binds on 0.0.0.0).
    if (service === 'forwarding') {
      if (!destHost) return String(remotePort);
      return `${destHost}:${remotePort}`;
    }
    return String(remotePort);
  }

  function buildAcl(): string {
    if (aclMode === 'anyone') return '';
    if (aclMode === 'current') return aclIp ? `${aclIp}/32` : '';
    return aclIp; // 'specific' — already a CIDR
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

  async function createTunnel(e: Event) {
    e.preventDefault();
    const id = $page.params.id;
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

      await apiPut(`/clients/${id}/tunnels?${params}`);

      if (storeInLibrary) {
        try {
          const body: Record<string, unknown> = {
            name: libraryName || `${effectiveScheme()} ${buildRemote()}`,
            client_id: id,
            remote_port: Number(remotePort),
            scheme: effectiveScheme()
          };
          if (service === 'forwarding' && destHost) body.remote_host = destHost;
          if (publicPortMode === 'specify' && publicPort) body.local_port = Number(publicPort);
          if (acl) body.acl = acl;
          if (autoClose) body.auto_close = autoClose;
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
    if (!confirm(`Delete tunnel ${tid}?`)) return;
    try {
      await apiDelete(`/clients/${$page.params.id}/tunnels/${tid}`);
      pushToast('good', 'Tunnel deleted.');
      await load($page.params.id);
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }

  // Reactive: when service changes, refresh the default port
  $effect(() => {
    // depend on both service and subService
    void service; void subService;
    onServiceChange();
  });

  // Reactive: when ACL mode changes, sync the IP field
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
  const SUB_SERVICE_OPTIONS: Array<[string, string]> = SERVICE_OPTIONS.slice(0, -1); // no 'forwarding'
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
      <!-- Step 1 -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">1</div>
        <div class="flex-1">
          <div class="text-sm">Service to access on the remote site</div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-3">
            <label class="text-xs col-span-2">
              <span class="block text-slate-400 mb-1">Select a service</span>
              <select bind:value={service}>
                {#each SERVICE_OPTIONS as [v, label]}
                  <option value={v}>{label}</option>
                {/each}
              </select>
            </label>

            {#if service === 'forwarding'}
              <!-- Sub-service + port + destination host -->
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
                <input type="number" min="1" max="65535" bind:value={remotePort} class="font-mono" />
              </label>
            {/if}
          </div>

          {#if service === 'forwarding'}
            <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-3">
              <label class="text-xs col-span-2">
                <span class="block text-slate-400 mb-1">Destination IP / Hostname</span>
                <input bind:value={destHost} placeholder="192.168.178.1" class="font-mono" />
              </label>
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Port</span>
                <input type="number" min="1" max="65535" bind:value={remotePort} class="font-mono" />
              </label>
            </div>
          {/if}

          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-3">
            <label class="text-xs col-span-2 flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={storeInLibrary} />
              <span>Store in library for later re-use</span>
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

      <!-- Step 2 -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">2</div>
        <div class="flex-1">
          <div class="text-sm">Public port</div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-3">
            <label class="text-xs col-span-2">
              <span class="block text-slate-400 mb-1">Select a port</span>
              <select bind:value={publicPortMode}>
                <option value="random">Random free port</option>
                <option value="specify">Specific port</option>
              </select>
            </label>
            {#if publicPortMode === 'specify'}
              <label class="text-xs">
                <span class="block text-slate-400 mb-1">Port</span>
                <input type="number" min="1" max="65535" bind:value={publicPort} class="font-mono" placeholder="20000" />
              </label>
            {/if}
          </div>
        </div>
      </div>

      <!-- Step 3 -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">3</div>
        <div class="flex-1">
          <div class="text-sm">ACL — who is allowed to use the tunnel</div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-3">
            <label class="text-xs col-span-2">
              <span class="block text-slate-400 mb-1">Select an ACL</span>
              <select bind:value={aclMode}>
                <option value="current">Only my current IP address</option>
                <option value="specific">Specific network range</option>
                <option value="anyone">No restrictions (anyone can access)</option>
              </select>
            </label>
            <label class="text-xs" class:opacity-50={aclMode === 'anyone'}>
              <span class="block text-slate-400 mb-1">
                {aclMode === 'current' ? 'IP Address' : aclMode === 'specific' ? 'CIDR' : 'IP Address'}
              </span>
              <input bind:value={aclIp}
                     readonly={aclMode !== 'specific'}
                     placeholder={aclMode === 'specific' ? '192.168.1.0/24' : ''}
                     class="font-mono" />
            </label>
          </div>
        </div>
      </div>

      <!-- Step 4 -->
      <div class="flex items-start gap-3">
        <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-indigo-600 text-sm text-white">4</div>
        <div class="flex-1">
          <div class="text-sm">Further options</div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-3 gap-3 items-end">
            <label class="text-xs col-span-2 flex items-center gap-2 cursor-pointer">
              <input type="checkbox" bind:checked={idleEnabled} />
              <span>Close tunnel after inactivity of</span>
            </label>
            <label class="text-xs" class:opacity-50={!idleEnabled}>
              <span class="block text-slate-400 mb-1">Minutes</span>
              <input type="number" min="1" bind:value={idleMinutes} disabled={!idleEnabled} class="font-mono" />
            </label>
          </div>
          <div class="mt-2 grid grid-cols-1 md:grid-cols-4 gap-3 items-end">
            <label class="text-xs col-span-2 flex items-center gap-2 cursor-pointer">
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
          <tr><th>ID</th><th>Local</th><th>Remote</th><th>Scheme</th><th>ACL</th><th>Idle</th><th>Created</th><th></th></tr>
        </thead>
        <tbody>
          {#each client.tunnels as t}
            <tr>
              <td class="font-mono">{t.id}</td>
              <td class="font-mono text-emerald-300">{t.lhost ?? ''}:{t.lport}</td>
              <td class="font-mono text-slate-300">{t.rhost ?? ''}:{t.rport}</td>
              <td><span class="pill pill-info">{t.scheme || t.protocol || '—'}</span></td>
              <td class="font-mono text-xs text-slate-400">{t.acl || '—'}</td>
              <td class="text-slate-400">{t.idle_timeout_minutes ?? '—'}m</td>
              <td class="text-slate-400">{fmtRelative(t.created_at)}</td>
              <td><button class="btn btn-danger" onclick={() => deleteTunnel(String(t.id))}>Delete</button></td>
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
