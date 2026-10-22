<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiDelete, refreshVaultStatus, ApiException } from '$lib/api';
  import { vaultPassphrase, vaultStatus, vaultUnlocked, pushToast } from '$lib/stores';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { fmtDate } from '$lib/format';

  // What GET /vault lists for each entry; values are only returned one at a time.
  type VaultEntry = { id?: number; client_id?: string; key?: string; created_at?: string; created_by?: string };

  let entries: VaultEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  // True when the vault's state could not be read, so no form is offered.
  let statusFailed = $state(false);
  let pass = $state('');
  let pass2 = $state('');

  function message(err: unknown): string {
    if (err instanceof ApiException) return err.errors[0]?.title || err.message;
    return err instanceof Error ? err.message : String(err);
  }

  async function loadEntries() {
    try {
      entries = (await apiGet<VaultEntry[]>('/vault?page[limit]=100')) ?? [];
    } catch (err) {
      entries = [];
      if (err instanceof ApiException && (err.status === 409 || err.status === 412)) {
        // Locked or not initialised after all: the status read was stale.
        await refreshVaultStatus().catch(() => {});
      } else {
        error = 'Could not list vault entries: ' + message(err);
      }
    }
  }

  async function load() {
    loading = true;
    error = '';
    statusFailed = false;
    try {
      await refreshVaultStatus();
    } catch (err) {
      statusFailed = true;
      error = 'Could not read vault status: ' + message(err);
    }
    if (!statusFailed && $vaultUnlocked) {
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
    if (pass !== pass2) {
      error = 'The passphrases do not match.';
      return;
    }
    try {
      await apiPost('/vault-admin/init', { password: pass });
      vaultPassphrase.set(pass);
      pushToast('good', 'Vault initialized and unlocked.');
      pass = '';
      pass2 = '';
      await load();
    } catch (err) {
      error = message(err);
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
      error = message(err);
    }
  }

  async function lock() {
    try {
      await apiDelete('/vault-admin/sesame');
      vaultPassphrase.set(null);
      entries = [];
      pushToast('good', 'Vault locked.');
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
    // Re-read either way, so the page shows the vault's real state.
    await load();
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
  {:else if statusFailed || $vaultStatus.init === 'unknown'}
    <div class="card p-4 flex justify-between items-center">
      <span class="text-slate-400">Vault status unavailable.</span>
      <button class="btn btn-ghost" onclick={load}>Retry</button>
    </div>
  {:else if $vaultStatus.init === 'uninitialized'}
    <div class="card p-4 space-y-3">
      <h2 class="font-medium">Initialize vault</h2>
      <p class="text-xs text-slate-500">
        Set a master passphrase. ProxiPort uses it to encrypt vault contents. There is no
        recovery — store this passphrase somewhere safe.
      </p>
      <form class="flex flex-col sm:flex-row gap-3" onsubmit={init}>
        <input type="password" bind:value={pass} placeholder="Set master passphrase" autocomplete="new-password" required />
        <input type="password" bind:value={pass2} placeholder="Repeat passphrase" autocomplete="new-password" required />
        <button class="btn btn-primary" type="submit" disabled={!pass || pass !== pass2}>Initialize</button>
      </form>
      {#if pass2 && pass !== pass2}<p class="text-xs text-amber-300">The passphrases do not match.</p>{/if}
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
      {#if error}
        <!-- The ErrorBox above says why the list is missing. -->
      {:else if !entries.length}
        <EmptyState title="No entries yet" />
      {:else}
        <table class="tbl">
          <thead><tr><th>Key</th><th>Client</th><th>Created</th><th>Created by</th></tr></thead>
          <tbody>
            {#each entries as v}
              <tr>
                <td class="font-mono">{v.key || '—'}</td>
                <td class="font-mono text-xs">{v.client_id || '—'}</td>
                <td class="text-xs text-slate-400">{fmtDate(v.created_at)}</td>
                <td class="text-xs">{v.created_by || '—'}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}
</div>
