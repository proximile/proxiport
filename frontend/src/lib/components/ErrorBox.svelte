<script lang="ts">
  let { message = '' }: { message?: string } = $props();
</script>

{#if message}
  <div class="border border-red-500/40 bg-red-500/10 text-red-200 px-3 py-2 rounded-md text-sm">
    {message}
  </div>
{/if}
