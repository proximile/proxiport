<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { AuditEntry } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: AuditEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let filter = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const r = await apiGet<AuditEntry[]>('/auditlog?sort=-timestamp&page[limit]=100');
      rows = r ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  let filtered = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    if (!f) return rows;
    return rows.filter((r) => Object.values(r).join(' ').toLowerCase().includes(f));
  });
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Audit log</h1>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>

  <input bind:value={filter} placeholder="Filter by user / IP / action / client …" class="max-w-md" />
  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading && !rows.length}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !filtered.length}
      <EmptyState title="No audit entries match" />
    {:else}
      <table class="tbl">
        <thead><tr><th>Time</th><th>User</th><th>IP</th><th>App</th><th>Action</th><th>Client</th><th>Affected</th></tr></thead>
        <tbody>
          {#each filtered as e}
            <tr>
              <td class="text-xs text-slate-400 whitespace-nowrap">{fmtDate(e.timestamp)}</td>
              <td>{e.username ?? '—'}</td>
              <td class="font-mono text-xs">{e.remote_ip ?? '—'}</td>
              <td>{e.application ?? '—'}</td>
              <td><span class="pill pill-info">{e.action ?? '—'}</span></td>
              <td>
                {#if e.client_id}
                  <a href={`/inventory/${e.client_id}`} class="text-indigo-300 font-mono text-xs">{e.client_hostname || e.client_id}</a>
                {:else}—{/if}
              </td>
              <td class="font-mono text-xs">{e.affected_id ?? '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
