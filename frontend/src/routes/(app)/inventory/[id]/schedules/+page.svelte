<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet, asList } from '$lib/api';
  import type { Schedule } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import Spinner from '$lib/components/Spinner.svelte';

  let schedules: Schedule[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    try {
      const all = await apiGet<unknown>('/schedules?page[limit]=100');
      schedules = asList<Schedule>(all).filter((s) => s.client_ids?.includes(id));
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
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !schedules.length}
      <EmptyState title="No schedules target this client" />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Type</th><th>Schedule</th><th>Created</th><th>By</th></tr></thead>
        <tbody>
          {#each schedules as s}
            <tr>
              <td>{s.name ?? '—'}</td>
              <td><span class="pill pill-info">{s.type ?? '—'}</span></td>
              <td class="font-mono text-xs">{s.schedule ?? '—'}</td>
              <td class="text-xs text-slate-400">{fmtDate(s.created_at)}</td>
              <td class="text-xs">{s.created_by ?? '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
