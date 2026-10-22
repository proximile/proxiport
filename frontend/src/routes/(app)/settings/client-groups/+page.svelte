<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { ClientGroup } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: ClientGroup[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      // The list returns only id and description unless other fields are asked for.
      rows =
        (await apiGet<ClientGroup[]>(
          '/client-groups?fields[client_groups]=id,description,num_clients,allowed_user_groups&page[limit]=500'
        )) ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Client groups</h1>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>
  <p class="text-sm text-slate-400">
    Groups are created, edited and deleted through the API
    (<span class="font-mono">POST</span>, <span class="font-mono">PUT</span> and
    <span class="font-mono">DELETE /api/v1/client-groups</span>, administrators only);
    this page lists them.
  </p>
  <ErrorBox message={error} />
  {#if loading || !error}
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !rows.length}
      {#if !error}<EmptyState title="No client groups" />{/if}
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Description</th><th>Clients</th><th>Allowed user-groups</th></tr></thead>
        <tbody>
          {#each rows as g}
            <tr>
              <td class="font-medium">{g.id}</td>
              <td class="text-sm text-slate-400">{g.description || '—'}</td>
              <td><span class="pill pill-info">{g.num_clients ?? 0}</span></td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#each g.allowed_user_groups ?? [] as ug}<span class="pill pill-muted">{ug}</span>{/each}
                  {#if !(g.allowed_user_groups ?? []).length}<span class="text-slate-500 text-xs">—</span>{/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
  {/if}
</div>
