<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet, apiPut, apiDelete, ApiException } from '$lib/api';
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

  // form state
  let remote = $state('22');
  let local = $state('');
  let scheme = $state('ssh');
  let acl = $state('');
  let idleMin = $state('');

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
    if (id) load(id);
  });

  async function createTunnel(e: Event) {
    e.preventDefault();
    const id = $page.params.id;
    creating = true;
    error = '';
    try {
      const params = new URLSearchParams();
      params.set('remote', remote);
      if (local) params.set('local', local);
      if (scheme) params.set('scheme', scheme);
      if (acl) params.set('acl', acl);
      if (idleMin) params.set('idle-timeout-minutes', idleMin);
      await apiPut(`/clients/${id}/tunnels?${params}`);
      pushToast('good', 'Tunnel created.');
      await load(id);
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
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
</script>

<div class="space-y-6">
  <ErrorBox message={error} />

  <div class="card p-4 space-y-3">
    <div class="flex items-center justify-between">
      <h2 class="font-medium">New tunnel</h2>
      {#if client?.connection_state !== 'connected'}
        <span class="pill pill-warn">client offline</span>
      {/if}
    </div>
    <form class="grid grid-cols-1 md:grid-cols-5 gap-3 items-end" onsubmit={createTunnel}>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Remote (host:port or port)</span>
        <input bind:value={remote} placeholder="22 or 127.0.0.1:80" required class="font-mono" />
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Local</span>
        <input bind:value={local} placeholder="auto" class="font-mono" />
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Scheme</span>
        <select bind:value={scheme}>
          <option value="">—</option>
          <option value="ssh">ssh</option>
          <option value="rdp">rdp</option>
          <option value="vnc">vnc</option>
          <option value="http">http</option>
          <option value="https">https</option>
        </select>
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">ACL (CIDR)</span>
        <input bind:value={acl} placeholder="0.0.0.0/0" class="font-mono" />
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Idle min</span>
        <input bind:value={idleMin} placeholder="5" class="font-mono" inputmode="numeric" />
      </label>
      <button class="btn btn-primary md:col-start-5" type="submit" disabled={creating || client?.connection_state !== 'connected'}>
        {#if creating}<Spinner />{:else}Create tunnel{/if}
      </button>
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
