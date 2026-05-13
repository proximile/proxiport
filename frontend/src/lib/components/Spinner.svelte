<script lang="ts">
  let { size = 16, label = '' }: { size?: number; label?: string } = $props();
</script>

<span class="inline-flex items-center gap-2 text-slate-400">
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    class="animate-spin"
  >
    <circle cx="12" cy="12" r="10" stroke="currentColor" stroke-opacity="0.25" stroke-width="3" />
    <path d="M22 12a10 10 0 0 0-10-10" stroke="currentColor" stroke-width="3" stroke-linecap="round" />
  </svg>
  {#if label}<span class="text-sm">{label}</span>{/if}
</span>
