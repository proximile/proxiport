<script lang="ts">
  import { untrack } from 'svelte';
  import { apiGet, apiGetPage, asList } from '$lib/api';
  import type { AuditEntry, Client } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  // With a clientId the table shows only that client's entries and drops the
  // Client column.
  let { clientId = '' }: { clientId?: string } = $props();

  const PAGE_SIZE = 100;

  let rows: AuditEntry[] = $state([]);
  let total: number | null = $state(null);
  let offset = $state(0);
  let since = $state('');
  let filter = $state('');
  let loading = $state(true);
  let error = $state('');
  let names: Map<string, string> = $state(new Map());

  function query(): string {
    const q = [`sort=-timestamp`, `page[limit]=${PAGE_SIZE}`, `page[offset]=${offset}`];
    if (clientId) q.push(`filter[client_id]=${encodeURIComponent(clientId)}`);
    if (since) {
      const t = Date.parse(since);
      if (!isNaN(t)) q.push(`filter[timestamp][since]=${encodeURIComponent(new Date(t).toISOString())}`);
    }
    return q.join('&');
  }

  async function load() {
    loading = true;
    error = '';
    try {
      const res = await apiGetPage<AuditEntry>(`/auditlog?${query()}`);
      rows = res.rows;
      total = res.count;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      rows = [];
      total = null;
    } finally {
      loading = false;
    }
  }

  // Entries carry the client's hostname at the time; show its current
  // inventory name where it still exists. Best-effort.
  async function loadNames() {
    try {
      const list = asList<Client>(await apiGet<unknown>('/clients?fields[clients]=id,name&page[limit]=500'));
      names = new Map(list.filter((c) => c.name).map((c) => [c.id, c.name]));
    } catch {
      names = new Map();
    }
  }

  // Reload from the first page whenever the client changes; nothing else the
  // loaders read should re-run this.
  $effect(() => {
    const id = clientId;
    untrack(() => {
      offset = 0;
      load();
      if (!id) loadNames();
    });
  });

  function goPage(next: number) {
    offset = Math.max(0, next);
    load();
  }

  function applySince() {
    offset = 0;
    load();
  }

  function clientLabel(e: AuditEntry): string {
    return (e.client_id && names.get(e.client_id)) || e.client_hostname || e.client_id || '';
  }

  // The filter searches what the table shows, not the hidden request and
  // response payloads, so every match is visible in its row.
  let filtered = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    if (!f) return rows;
    return rows.filter((r) =>
      [fmtDate(r.timestamp), r.username, r.remote_ip, r.application, r.action, clientLabel(r), r.client_hostname, r.client_id, r.affected_id]
        .join(' ')
        .toLowerCase()
        .includes(f)
    );
  });

  const pageEnd = $derived(offset + rows.length);
</script>

<div class="space-y-3">
  <div class="flex flex-wrap items-end gap-3">
    <label class="block text-xs grow max-w-md">
      <span class="block text-slate-400 mb-1">Filter this page</span>
      <input
        bind:value={filter}
        placeholder="User, IP, action, client …"
        onkeydown={(e) => {
          if (e.key === 'Escape') filter = '';
        }}
      />
    </label>
    <label class="block text-xs">
      <span class="block text-slate-400 mb-1">Since</span>
      <input type="datetime-local" bind:value={since} onchange={applySince} class="font-mono" />
    </label>
    <button class="btn btn-ghost" onclick={load} disabled={loading}>Refresh</button>
  </div>
  <ErrorBox message={error} />

  <!-- On a failed load the card would be empty, unless the pager is still
       useful for moving to another page. -->
  {#if loading || !error || (total !== null && total > 0)}
  <div class="card overflow-x-auto">
    {#if loading && !rows.length}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if error}
      <!-- The ErrorBox above says why the list is missing. -->
    {:else if !rows.length}
      <EmptyState title="No audit entries" detail={clientId ? 'Activity scoped to this client will appear here.' : ''} />
    {:else if !filtered.length}
      <EmptyState title="No entries on this page match" detail="The filter searches the entries loaded below; use Since or the pager to look further back." />
    {:else}
      <table class="tbl">
        <thead>
          <tr><th>Time</th><th>User</th><th>IP</th><th>App</th><th>Action</th>{#if !clientId}<th>Client</th>{/if}<th>Affected</th></tr>
        </thead>
        <tbody>
          {#each filtered as e}
            <tr>
              <td class="text-xs text-slate-400 whitespace-nowrap">{fmtDate(e.timestamp)}</td>
              <td>{e.username || '—'}</td>
              <td class="font-mono text-xs">{e.remote_ip || '—'}</td>
              <td>{e.application || '—'}</td>
              <td><span class="pill pill-info">{e.action || '—'}</span></td>
              {#if !clientId}
                <td>
                  {#if e.client_id}
                    <a href={`/inventory/${e.client_id}`} class="text-indigo-300 font-mono text-xs" title={e.client_hostname || e.client_id}>{clientLabel(e)}</a>
                  {:else}—{/if}
                </td>
              {/if}
              <td class="font-mono text-xs">{e.affected_id || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
    {#if total !== null && total > 0}
      <div class="flex items-center justify-between gap-2 px-4 py-2 border-t border-pp-border text-xs text-slate-400">
        <span>Showing {rows.length ? offset + 1 : 0}–{pageEnd} of {total}</span>
        <div class="flex gap-2">
          <button class="btn btn-ghost btn-sm" disabled={loading || offset === 0} onclick={() => goPage(offset - PAGE_SIZE)}>Newer</button>
          <button class="btn btn-ghost btn-sm" disabled={loading || pageEnd >= total} onclick={() => goPage(offset + PAGE_SIZE)}>Older</button>
        </div>
      </div>
    {/if}
  </div>
  {/if}
</div>
