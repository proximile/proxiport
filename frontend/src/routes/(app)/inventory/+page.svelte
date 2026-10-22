<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { apiGetList } from '$lib/api';
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


  let clients: Client[] = $state([]);
  // Total number of clients the server has, which can exceed what is loaded.
  let total = $state(0);
  let filter = $state('');
  let stateFilter: 'all' | 'connected' | 'disconnected' = $state('all');
  let loading = $state(true);
  let loadingMore = $state(false);
  let error = $state('');
  // Whether a list has ever loaded: a failed first load is not an empty fleet.
  let loaded = $state(false);

  const PAGE_SIZE = 100;
  const FIELDS =
    'id,name,hostname,os,os_full_name,os_arch,num_cpus,mem_total,address,connection_state,disconnected_at,last_heartbeat_at,version,tags,labels,groups,ipv4';

  function pageUrl(offset: number, limit: number): string {
    // includes disconnected clients, so historical ones are listed too
    return `/clients?fields[clients]=${FIELDS}&page[limit]=${limit}&page[offset]=${offset}`;
  }

  async function load() {
    if ((loading && loaded) || loadingMore) return;
    loading = true;
    error = '';
    try {
      // A refresh re-reads as many clients as are already shown.
      const limit = Math.min(500, Math.max(PAGE_SIZE, clients.length));
      const res = await apiGetList<Client>(pageUrl(0, limit));
      clients = res.data;
      total = res.count;
      loaded = true;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function loadMore() {
    if (loadingMore || loading) return;
    loadingMore = true;
    error = '';
    try {
      const res = await apiGetList<Client>(pageUrl(clients.length, PAGE_SIZE));
      const seen = new Set(clients.map((c) => c.id));
      clients = [...clients, ...res.data.filter((c) => !seen.has(c.id))];
      total = res.count;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loadingMore = false;
    }
  }

  onMount(load);

  let filtered = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    return clients.filter((c) => {
      if (stateFilter !== 'all' && c.connection_state !== stateFilter) return false;
      if (!f) return true;
      const hay = [
        c.id,
        c.name,
        c.hostname,
        c.os_full_name,
        c.address,
        ...(c.ipv4 ?? []),
        ...(c.tags ?? []),
        ...(c.groups ?? [])
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return hay.includes(f);
    });
  });

  let summary = $derived.by(() => {
    const conn = clients.filter((c) => c.connection_state === 'connected').length;
    return { total: Math.max(total, clients.length), conn, disc: clients.length - conn };
  });
  let partial = $derived(clients.length < summary.total);

  let emptyDetail = $derived.by(() => {
    if (clients.length === 0) return 'Wait for an agent to connect.';
    const which = stateFilter === 'all' ? '' : `${stateFilter} `;
    if (filter) return `No ${which}client matches "${filter}".`;
    return `No ${which}clients.`;
  });
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Inventory</h1>
      {#if loaded}
        <div class="text-sm text-slate-400 mt-1">
          {summary.total} client{summary.total === 1 ? '' : 's'} ·
          <span class="text-emerald-400">{summary.conn} connected</span> ·
          <span class="text-slate-500">{summary.disc} disconnected</span>
          {#if partial}<span>(of the {clients.length} loaded)</span>{/if}
        </div>
      {/if}
    </div>
    <button class="btn btn-ghost" onclick={load} disabled={loading || loadingMore} aria-busy={loading}>
      {#if loading && loaded}<Spinner />{/if}Refresh
    </button>
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
    {#if loading && !loaded}
      <div class="p-8 flex justify-center"><Spinner label="Loading clients…" /></div>
    {:else if !loaded}
      <EmptyState title="Could not load clients" detail="Use Refresh to try again." />
    {:else if filtered.length === 0}
      <EmptyState title={clients.length === 0 ? 'No clients yet' : 'No clients match'} detail={emptyDetail} />
    {:else}
      <table class="tbl">
        <thead>
          <tr>
            <th>State</th>
            <th>Name</th>
            <th class="hidden md:table-cell">Hostname</th>
            <th>OS</th>
            <th class="hidden md:table-cell">CPU</th>
            <th class="hidden md:table-cell">RAM</th>
            <th class="hidden md:table-cell">IP</th>
            <th class="hidden md:table-cell">Tags</th>
            <th class="hidden md:table-cell">Groups</th>
            <th class="hidden md:table-cell">Last heartbeat</th>
            <th class="hidden md:table-cell">Version</th>
          </tr>
        </thead>
        <tbody>
          {#each filtered as c (c.id)}
            <!-- Row click is a mouse-only convenience; keyboard/AT users use the
                 Name-cell link. The row is intentionally not a tab stop nor a
                 "button" role — that mislabelled every row with its full text
                 content and added a tab stop per row. -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <tr
              class="cursor-pointer hover:bg-pp-surface-2"
              onclick={(e) => rowClick(c, e)}
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
              <td class="hidden md:table-cell font-mono text-xs text-slate-400">{c.hostname || '—'}</td>
              <td class="text-slate-300">{c.os_full_name || c.os || '—'}</td>
              <td class="hidden md:table-cell text-slate-400">{c.num_cpus ?? '—'}</td>
              <td class="hidden md:table-cell text-slate-400">{fmtBytes(c.mem_total)}</td>
              <td class="hidden md:table-cell font-mono text-xs text-slate-400">{c.address || (c.ipv4?.[0] ?? '—')}</td>
              <td class="hidden md:table-cell">
                <div class="flex flex-wrap gap-1">
                  {#each c.tags ?? [] as t}<span class="pill pill-info">{t}</span>{/each}
                </div>
              </td>
              <td class="hidden md:table-cell">
                <div class="flex flex-wrap gap-1">
                  {#each c.groups ?? [] as g}<span class="pill pill-muted">{g}</span>{/each}
                </div>
              </td>
              <td class="hidden md:table-cell text-slate-400">{fmtRelative(c.last_heartbeat_at)}</td>
              <td class="hidden md:table-cell font-mono text-xs text-slate-500">{c.version || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  {#if loaded && partial}
    <div class="flex flex-wrap items-center gap-3 text-sm text-slate-400">
      <span>
        Showing the first {clients.length} of {summary.total} clients; the filter only searches the
        loaded ones.
      </span>
      <button class="btn btn-ghost" onclick={loadMore} disabled={loading || loadingMore}>
        {#if loadingMore}<Spinner />{/if}Load {Math.min(PAGE_SIZE, summary.total - clients.length)} more
      </button>
    </div>
  {/if}
</div>
