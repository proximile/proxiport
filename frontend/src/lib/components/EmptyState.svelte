<script lang="ts">
  let { title = 'Nothing here yet', detail = '' }: { title?: string; detail?: string } = $props();
</script>

<div class="text-center py-12 px-4">
  <div class="text-slate-300 font-medium">{title}</div>
  {#if detail}
    <div class="text-slate-500 text-sm mt-1">{detail}</div>
  {/if}
</div>
