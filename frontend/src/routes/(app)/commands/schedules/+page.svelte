<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiDelete, asList } from '$lib/api';
  import type { Schedule } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import { pushToast } from '$lib/stores';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: Schedule[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      rows = asList<Schedule>(await apiGet<unknown>('/schedules?page[limit]=100'));
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function del(id: string) {
    if (!confirm('Delete this schedule?')) return;
    try {
      await apiDelete(`/schedules/${id}`);
      pushToast('good', 'Schedule deleted.');
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Schedules</h1>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>
  <ErrorBox message={error} />
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !rows.length}
      <EmptyState title="No schedules" detail="Cron-style scheduled commands and scripts appear here." />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Type</th><th>Cron</th><th>Targets</th><th>Created</th><th>By</th><th></th></tr></thead>
        <tbody>
          {#each rows as s}
            <tr>
              <td>{s.name ?? '—'}</td>
              <td><span class="pill pill-info">{s.type ?? '—'}</span></td>
              <td class="font-mono text-xs">{s.schedule ?? '—'}</td>
              <td class="text-xs">{(s.client_ids ?? []).length} client(s) / {(s.group_ids ?? []).length} group(s)</td>
              <td class="text-xs text-slate-400">{fmtDate(s.created_at)}</td>
              <td class="text-xs">{s.created_by ?? '—'}</td>
              <td><button class="btn btn-danger" onclick={() => del(s.id ?? '')}>Delete</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
