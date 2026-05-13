<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { ClientAuthEntry } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: ClientAuthEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let revealed: Record<string, boolean> = $state({});

  async function load() {
    loading = true;
    error = '';
    try {
      rows = (await apiGet<ClientAuthEntry[]>('/clients-auth?page[limit]=100')) ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">Client access credentials</h1>
  <p class="text-sm text-slate-400">
    Agent-side <code class="text-xs">auth=user:password</code> credentials. The server stores these in
    plaintext by design — agents need them to authenticate on connect.
  </p>
  <ErrorBox message={error} />
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else}
      <table class="tbl">
        <thead><tr><th>ID</th><th>Password</th></tr></thead>
        <tbody>
          {#each rows as r}
            <tr>
              <td class="font-mono">{r.id}</td>
              <td>
                <button class="font-mono text-xs hover:text-indigo-300 cursor-pointer" onclick={() => (revealed[r.id] = !revealed[r.id])}>
                  {revealed[r.id] ? r.password : '••••••••••'}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
