<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet, ApiException } from '$lib/api';
  import { vaultUnlocked } from '$lib/stores';
  import VaultLocked from '$lib/components/VaultLocked.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import Spinner from '$lib/components/Spinner.svelte';

  let attrs: Record<string, unknown> | null = $state(null);
  let loading = $state(true);
  let error = $state('');
  let locked = $state(false);

  async function load(id: string) {
    loading = true;
    error = '';
    locked = false;
    try {
      const res = await apiGet<any>(`/clients/${id}/attributes`);
      attrs = res ?? {};
    } catch (err) {
      if (err instanceof ApiException && (err.status === 401 || err.status === 423)) {
        locked = true;
      } else {
        error = err instanceof Error ? err.message : String(err);
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = $page.params.id;
    if (id) load(id);
  });
</script>

{#if !$vaultUnlocked || locked}
  <VaultLocked what="Per-client meta-data" />
{:else if loading}
  <div class="p-8 flex justify-center"><Spinner /></div>
{:else}
  <ErrorBox message={error} />
  <div class="card p-4">
    <div class="text-xs uppercase tracking-wider text-slate-500 mb-3">Custom attributes</div>
    {#if !attrs || Object.keys(attrs).length === 0}
      <EmptyState title="No meta-data set" detail="Use the API or this UI to add KV pairs." />
    {:else}
      <table class="tbl">
        <thead><tr><th>Key</th><th>Value</th></tr></thead>
        <tbody>
          {#each Object.entries(attrs) as [k, v]}
            <tr>
              <td class="font-mono">{k}</td>
              <td class="font-mono text-xs">{typeof v === 'object' ? JSON.stringify(v) : String(v)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
{/if}
