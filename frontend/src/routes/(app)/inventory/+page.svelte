<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { apiGet } from '$lib/api';
  import type { Client } from '$lib/types';
  import { fmtRelative, fmtBytes } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  function rowClick(c: Client, e: MouseEvent) {
    // let nested anchors/buttons handle their own clicks
    if ((e.target as HTMLElement).closest('a, button')) return;
    // preserve open-in-new-tab semantics
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    goto(`/inventory/${c.id}`);
  }

  function rowKey(c: Client, e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      goto(`/inventory/${c.id}`);
    }
  }

  let clients: Client[] = $state([]);
  let filter = $state('');
  let stateFilter: 'all' | 'connected' | 'disconnected' = $state('all');
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      // include disconnected so we can show historical clients too
      const res = await apiGet<Client[]>('/clients?fields[clients]=id,name,hostname,os,os_full_name,os_arch,num_cpus,mem_total,address,connection_state,disconnected_at,last_heartbeat_at,version,tags,labels,groups,ipv4&page[limit]=100');
      clients = res ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  let filtered = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    return clients.filter((c) => {
      if (stateFilter !== 'all' && c.connection_state !== stateFilter) return false;
      if (!f) return true;
      const hay = [c.id, c.name, c.hostname, c.os_full_name, c.address, ...(c.tags ?? []), ...(c.groups ?? [])]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return hay.includes(f);
    });
  });

  let summary = $derived.by(() => {
    const conn = clients.filter((c) => c.connection_state === 'connected').length;
    return { total: clients.length, conn, disc: clients.length - conn };
  });
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Inventory</h1>
      <div class="text-sm text-slate-400 mt-1">
        {summary.total} client{summary.total === 1 ? '' : 's'} ·
        <span class="text-emerald-400">{summary.conn} connected</span> ·
        <span class="text-slate-500">{summary.disc} disconnected</span>
      </div>
    </div>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>

  <div class="flex gap-3 items-center">
    <div class="flex-1 max-w-md">
      <input bind:value={filter} placeholder="Filter by id / hostname / tag / IP …" />
    </div>
    <select bind:value={stateFilter} class="max-w-[12rem]">
      <option value="all">All</option>
      <option value="connected">Connected only</option>
      <option value="disconnected">Disconnected only</option>
    </select>
  </div>

  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading && clients.length === 0}
      <div class="p-8 flex justify-center"><Spinner label="Loading clients…" /></div>
    {:else if filtered.length === 0}
      <EmptyState title="No clients match" detail={filter ? `No client matches "${filter}".` : 'Wait for an agent to connect.'} />
    {:else}
      <table class="tbl">
        <thead>
          <tr>
            <th>State</th>
            <th>Name</th>
            <th>Hostname</th>
            <th>OS</th>
            <th>CPU</th>
            <th>RAM</th>
            <th>IP</th>
            <th>Tags</th>
            <th>Groups</th>
            <th>Last heartbeat</th>
            <th>Version</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as c (c.id)}
            <tr
              class="cursor-pointer hover:bg-pp-surface-2"
              role="button"
              tabindex="0"
              onclick={(e) => rowClick(c, e)}
              onkeydown={(e) => rowKey(c, e)}
            >
              <td>
                {#if c.connection_state === 'connected'}
                  <span class="pill pill-good">connected</span>
                {:else}
                  <span class="pill pill-muted">disconnected</span>
                {/if}
              </td>
              <td>
                <a href={`/inventory/${c.id}`} class="text-indigo-300 hover:text-indigo-200 font-medium">
                  {c.name || c.id}
                </a>
              </td>
              <td class="font-mono text-xs text-slate-400">{c.hostname || '—'}</td>
              <td class="text-slate-300">{c.os_full_name || c.os || '—'}</td>
              <td class="text-slate-400">{c.num_cpus ?? '—'}</td>
              <td class="text-slate-400">{fmtBytes(c.mem_total)}</td>
              <td class="font-mono text-xs text-slate-400">{c.address || (c.ipv4?.[0] ?? '—')}</td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#each c.tags ?? [] as t}<span class="pill pill-info">{t}</span>{/each}
                </div>
              </td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#each c.groups ?? [] as g}<span class="pill pill-muted">{g}</span>{/each}
                </div>
              </td>
              <td class="text-slate-400">{fmtRelative(c.last_heartbeat_at)}</td>
              <td class="font-mono text-xs text-slate-500">{c.version || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
