<script lang="ts">
  import VaultLocked from '$lib/components/VaultLocked.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import { vaultUnlocked } from '$lib/stores';
</script>

{#if !$vaultUnlocked}
  <VaultLocked what="Per-client documents" />
{:else}
  <EmptyState
    title="No documents yet"
    detail="Documents are stored in the vault and scoped per-client. Authoring UI ships in a later milestone — for now, use the /vault REST endpoints directly."
  />
{/if}
