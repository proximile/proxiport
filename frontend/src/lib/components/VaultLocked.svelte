<script lang="ts">
  import { vaultStatus } from '$lib/stores';
  let { what = 'this section' }: { what?: string } = $props();

  // Distinguish "vault has never been initialized" from "initialized but locked".
  let uninitialized = $derived($vaultStatus.init === 'uninitialized');
  let title = $derived(uninitialized ? 'Vault is not initialized' : 'Vault is locked');
  let detail = $derived(
    uninitialized
      ? `${what} lives in the encrypted vault. Initialize it from`
      : `${what} is stored in the encrypted vault. Open`
  );
</script>

<div class="card p-8 text-center max-w-lg mx-auto">
  <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" class="mx-auto text-slate-500 mb-3">
    <rect x="5" y="11" width="14" height="10" rx="1" />
    <path d="M7 11V8a5 5 0 0 1 10 0v3" />
  </svg>
  <div class="font-medium text-slate-200">{title}</div>
  <div class="text-sm text-slate-500 mt-1">
    {detail}
    <a class="text-indigo-300 hover:text-indigo-200" href="/settings/vault">Vault settings</a>
    {uninitialized ? 'to set a master passphrase.' : 'to unlock it.'}
  </div>
</div>
