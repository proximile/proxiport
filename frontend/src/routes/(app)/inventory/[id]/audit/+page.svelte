<script lang="ts">
  import { page } from '$app/state';
  import AuditTable from '#lib/components/AuditTable.svelte';
</script>

{#if page.params.id}
  <AuditTable clientId={page.params.id} />
{/if}
