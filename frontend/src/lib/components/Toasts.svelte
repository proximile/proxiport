<script lang="ts">
  import { toasts } from '../stores';

  function toastClass(level: string): string {
    switch (level) {
      case 'good':
        return 'border-emerald-500 bg-emerald-500/10 text-emerald-200';
      case 'bad':
        return 'border-red-500 bg-red-500/10 text-red-200';
      case 'warn':
        return 'border-amber-500 bg-amber-500/10 text-amber-200';
      default:
        return 'border-indigo-500 bg-indigo-500/10 text-indigo-200';
    }
  }
</script>

<div class="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
  {#each $toasts as t (t.id)}
    <div
      class="px-4 py-2 rounded-md shadow-lg border text-sm font-medium pointer-events-auto {toastClass(t.level)}"
    >
      {t.text}
    </div>
  {/each}
</div>
