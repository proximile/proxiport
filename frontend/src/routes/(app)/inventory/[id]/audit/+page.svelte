<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet } from '$lib/api';
  import type { AuditEntry } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let entries: AuditEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    try {
      const res = await apiGet<AuditEntry[]>(`/auditlog?filter[client_id]=${encodeURIComponent(id)}&sort=-timestamp&page[limit]=100`);
      entries = res ?? [];
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
</script>

<div class="space-y-4">
  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading && !entries.length}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !entries.length}
      <EmptyState title="No audit entries" detail="Activity scoped to this client will appear here." />
    {:else}
      <table class="tbl">
        <thead><tr><th>Time</th><th>User</th><th>IP</th><th>App</th><th>Action</th><th>Affected</th></tr></thead>
        <tbody>
          {#each entries as e}
            <tr>
              <td class="text-xs text-slate-400">{fmtDate(e.timestamp)}</td>
              <td>{e.username ?? '—'}</td>
              <td class="font-mono text-xs">{e.remote_ip ?? '—'}</td>
              <td>{e.application ?? '—'}</td>
              <td><span class="pill pill-info">{e.action ?? '—'}</span></td>
              <td class="font-mono text-xs">{e.affected_id ?? '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
