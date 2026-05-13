<script lang="ts">
  import VaultLocked from '$lib/components/VaultLocked.svelte';
  import { vaultUnlocked } from '$lib/stores';
  import EmptyState from '$lib/components/EmptyState.svelte';
</script>

{#if !$vaultUnlocked}
  <div class="p-6">
    <VaultLocked what="Documentation" />
  </div>
{:else}
  <div class="p-6">
    <h1 class="text-2xl font-semibold tracking-tight">Documentation</h1>
    <EmptyState
      title="No documentation yet"
      detail="Per-server runbooks. Authoring UI ships in a later milestone — for now use the /vault REST endpoints directly."
    />
  </div>
{/if}
