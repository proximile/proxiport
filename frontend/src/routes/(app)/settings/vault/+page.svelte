<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiDelete, refreshVaultStatus, ApiException } from '$lib/api';
  import { vaultPassphrase, vaultStatus, vaultUnlocked, pushToast } from '$lib/stores';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { fmtDate } from '$lib/format';

  type VaultEntry = { id?: number; client_id?: string; required_group?: string; key?: string; value?: string; type?: string; created_at?: string; updated_by?: string };

  let entries: VaultEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let pass = $state('');

  async function loadEntries() {
    try {
      entries = (await apiGet<VaultEntry[]>('/vault?page[limit]=100')) ?? [];
    } catch (_) {
      // 409 (locked) or 412 (uninit) — leave list empty.
    }
  }

  async function load() {
    loading = true;
    error = '';
    try {
      await refreshVaultStatus();
    } catch (err) {
      if (err instanceof ApiException) error = err.errors[0]?.title || err.message;
    }
    if ($vaultUnlocked) {
      await loadEntries();
    } else {
      entries = [];
    }
    loading = false;
  }

  onMount(load);

  async function init(e: Event) {
    e.preventDefault();
    error = '';
    try {
      await apiPost('/vault-admin/init', { password: pass });
      vaultPassphrase.set(pass);
      pushToast('good', 'Vault initialized and unlocked.');
      pass = '';
      await load();
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
    }
  }

  async function unlock(e: Event) {
    e.preventDefault();
    error = '';
    try {
      // Vault API: POST /vault-admin/sesame to unlock, DELETE to lock.
      await apiPost('/vault-admin/sesame', { password: pass });
      vaultPassphrase.set(pass);
      pushToast('good', 'Vault unlocked.');
      pass = '';
      await load();
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
    }
  }

  async function lock() {
    try {
      await apiDelete('/vault-admin/sesame');
      vaultPassphrase.set(null);
      entries = [];
      pushToast('good', 'Vault locked.');
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4 max-w-3xl">
  <h1 class="text-2xl font-semibold tracking-tight">Vault</h1>
  <p class="text-sm text-slate-400">
    Server-side encrypted store. Unlock the vault with a passphrase to access per-client meta-data,
    documents, and documentation.
  </p>
  <ErrorBox message={error} />

  {#if loading}
    <Spinner />
  {:else if $vaultStatus.init === 'uninitialized'}
    <div class="card p-4 space-y-3">
      <h2 class="font-medium">Initialize vault</h2>
      <p class="text-xs text-slate-500">
        Set a master passphrase. ProxiPort uses it to encrypt vault contents. There is no
        recovery — store this passphrase somewhere safe.
      </p>
      <form class="flex gap-3" onsubmit={init}>
        <input type="password" bind:value={pass} placeholder="Set master passphrase" required />
        <button class="btn btn-primary" type="submit">Initialize</button>
      </form>
    </div>
  {:else if !$vaultUnlocked}
    <div class="card p-4 space-y-3">
      <h2 class="font-medium">Unlock vault</h2>
      <form class="flex gap-3" onsubmit={unlock}>
        <input type="password" bind:value={pass} placeholder="Master passphrase" required />
        <button class="btn btn-primary" type="submit">Unlock</button>
      </form>
    </div>
  {:else}
    <div class="card p-4 flex justify-between items-center">
      <span class="text-emerald-300">Vault is unlocked.</span>
      <button class="btn btn-ghost" onclick={lock}>Lock</button>
    </div>

    <div class="card overflow-x-auto">
      <div class="px-4 py-2 border-b border-pp-border text-sm text-slate-400">
        Entries ({entries.length})
      </div>
      {#if !entries.length}
        <EmptyState title="No entries yet" />
      {:else}
        <table class="tbl">
          <thead><tr><th>Client</th><th>Group</th><th>Key</th><th>Type</th><th>Updated</th><th>By</th></tr></thead>
          <tbody>
            {#each entries as v}
              <tr>
                <td class="font-mono text-xs">{v.client_id ?? '—'}</td>
                <td>{v.required_group ?? '—'}</td>
                <td class="font-mono">{v.key}</td>
                <td><span class="pill pill-info">{v.type ?? '—'}</span></td>
                <td class="text-xs text-slate-400">{fmtDate(v.created_at)}</td>
                <td class="text-xs">{v.updated_by ?? '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}
</div>
