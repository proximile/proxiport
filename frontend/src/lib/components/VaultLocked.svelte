<script lang="ts">
  import { vaultStatus, vaultStatusError } from '$lib/stores';
  import { refreshVaultStatus } from '$lib/api';
  import Spinner from './Spinner.svelte';
  let { what = 'this section', plural = false }: { what?: string; plural?: boolean } = $props();

  // Distinguish "vault has never been initialized" from "initialized but locked",
  // and both from "the status could not be read" or "not read yet".
  let failed = $derived($vaultStatusError !== '');
  let pending = $derived(!failed && $vaultStatus.init === 'unknown');
  let uninitialized = $derived($vaultStatus.init === 'uninitialized');
  let title = $derived(uninitialized ? 'Vault is not initialized' : 'Vault is locked');
  let detail = $derived(
    uninitialized
      ? `${what} ${plural ? 'live' : 'lives'} in the encrypted vault. Initialize it from`
      : `${what} ${plural ? 'are' : 'is'} stored in the encrypted vault. Open`
  );
  let retrying = $state(false);

  async function retry() {
    retrying = true;
    try {
      await refreshVaultStatus();
    } catch {
      // vaultStatusError now holds the new reason
    } finally {
      retrying = false;
    }
  }
</script>

{#if pending}
  <div class="p-8 flex justify-center"><Spinner label="Checking vault status…" /></div>
{:else}
<div class="card p-8 text-center max-w-lg mx-auto">
  <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="mx-auto text-slate-500 mb-3">
    <rect x="5" y="11" width="14" height="10" rx="1" />
    <path d="M7 11V8a5 5 0 0 1 10 0v3" />
  </svg>
  {#if failed}
    <div class="font-medium text-slate-200">Could not load the vault status</div>
    <div class="text-sm text-slate-500 mt-1">{$vaultStatusError}</div>
    <button class="btn btn-ghost mt-3" onclick={retry} disabled={retrying}>
      {#if retrying}<Spinner />{/if}Retry
    </button>
  {:else}
    <div class="font-medium text-slate-200">{title}</div>
    <div class="text-sm text-slate-500 mt-1">
      {detail}
      <a class="text-indigo-300 hover:text-indigo-200" href="/settings/vault">Vault settings</a>
      {uninitialized ? 'to set a master passphrase.' : 'to unlock it.'}
    </div>
  {/if}
</div>
{/if}
