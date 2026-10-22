<script lang="ts">
  import { untrack } from 'svelte';
  import { page } from '$app/stores';
  import { apiGet, asList } from '$lib/api';
  import { fmtRelative } from '$lib/format';
  import VaultLocked from '$lib/components/VaultLocked.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import Spinner from '$lib/components/Spinner.svelte';
  import { vaultUnlocked } from '$lib/stores';

  type VaultKey = { id: number; client_id?: string; key: string; created_by?: string; created_at?: string };

  let entries: VaultKey[] = $state([]);
  let loading = $state(false);
  let error = $state('');

  let seq = 0;
  async function load(id: string) {
    const mine = ++seq;
    loading = true;
    error = '';
    try {
      const res = await apiGet<unknown>(`/vault?filter[client_id]=${encodeURIComponent(id)}&sort=key`);
      if (mine === seq) entries = asList<VaultKey>(res);
    } catch (err) {
      if (mine !== seq) return;
      entries = [];
      error = err instanceof Error ? err.message : String(err);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  // The vault only lists entries while it is unlocked.
  $effect(() => {
    const id = $page.params.id;
    if (id && $vaultUnlocked) untrack(() => load(id));
  });
</script>

{#if !$vaultUnlocked}
  <VaultLocked what="Per-client documents" plural />
{:else if loading}
  <div class="p-8 flex justify-center"><Spinner /></div>
{:else if error}
  <ErrorBox message={error} />
{:else if !entries.length}
  <EmptyState
    title="No documents yet"
    detail="Vault entries stored for this client are listed here. Add them through the /vault REST endpoints."
  />
{:else}
  <div class="card overflow-x-auto">
    <table class="tbl">
      <thead><tr><th>Key</th><th>Created by</th><th>Created</th></tr></thead>
      <tbody>
        {#each entries as e (e.id)}
          <tr>
            <td class="font-mono">{e.key}</td>
            <td>{e.created_by || '—'}</td>
            <td class="text-slate-400">{fmtRelative(e.created_at)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
