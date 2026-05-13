<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { ClientGroup } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: ClientGroup[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      rows = (await apiGet<ClientGroup[]>('/client-groups')) ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">Client groups</h1>
  <ErrorBox message={error} />
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Description</th><th>Clients</th><th>Allowed user-groups</th></tr></thead>
        <tbody>
          {#each rows as g}
            <tr>
              <td class="font-medium">{g.id}</td>
              <td class="text-sm text-slate-400">{g.description ?? '—'}</td>
              <td><span class="pill pill-info">{g.num_clients ?? 0}</span></td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#each g.allowed_user_groups ?? [] as ug}<span class="pill pill-muted">{ug}</span>{/each}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
