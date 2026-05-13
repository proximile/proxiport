<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { User } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let users: User[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      users = (await apiGet<User[]>('/users')) ?? [];
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
    <h1 class="text-2xl font-semibold tracking-tight">Users</h1>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>
  <ErrorBox message={error} />
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else}
      <table class="tbl">
        <thead><tr><th>Username</th><th>Groups</th><th>2FA recipient</th></tr></thead>
        <tbody>
          {#each users as u}
            <tr>
              <td class="font-medium">{u.username}</td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#each u.groups ?? [] as g}<span class="pill pill-info">{g}</span>{/each}
                </div>
              </td>
              <td class="font-mono text-xs">{u.two_fa_send_to ?? '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
