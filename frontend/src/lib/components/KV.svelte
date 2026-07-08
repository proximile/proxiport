<script lang="ts">
  let { k, v, mono = false }: { k: string; v?: string | null | undefined; mono?: boolean } = $props();
</script>

<div class="flex justify-between gap-4 text-sm">
  <span class="text-slate-500 flex-shrink-0">{k}</span>
  <span class={mono ? 'font-mono text-slate-200 break-all text-right min-w-0' : 'text-slate-200 truncate text-right min-w-0'}>
    {v ?? '—'}
  </span>
</div>
