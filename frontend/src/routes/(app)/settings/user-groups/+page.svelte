<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { Group } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let groups: Group[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      groups = (await apiGet<Group[]>('/user-groups')) ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const PERM_KEYS = ['tunnels', 'commands', 'scripts', 'scheduler', 'monitoring', 'auditlog', 'uploads', 'vault'];
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">User groups</h1>
  <ErrorBox message={error} />
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else}
      <table class="tbl">
        <thead>
          <tr>
            <th>Group</th>
            {#each PERM_KEYS as k}<th>{k}</th>{/each}
          </tr>
        </thead>
        <tbody>
          {#each groups as g}
            <tr>
              <td class="font-medium">{g.name}</td>
              {#each PERM_KEYS as k}
                <td>
                  {#if g.permissions?.[k]}<span class="pill pill-good">on</span>{:else}<span class="pill pill-muted">off</span>{/if}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
