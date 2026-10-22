<script lang="ts">
  import { toasts, dismissToast, type Toast } from '../stores';

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

{#snippet item(t: Toast)}
  <div
    class="flex items-start gap-3 pl-4 pr-2 py-2 rounded-md shadow-lg border text-sm font-medium pointer-events-auto {toastClass(t.level)}"
  >
    <span class="py-0.5">{t.text}</span>
    <button
      type="button"
      class="flex-shrink-0 rounded p-0.5 opacity-70 hover:opacity-100 cursor-pointer"
      aria-label="Dismiss notification"
      onclick={() => dismissToast(t.id)}
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    </button>
  </div>
{/snippet}

<!-- Both regions stay mounted so screen readers announce toasts as they are
     added: errors assertively, everything else politely. -->
<div class="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
  <div role="alert" class="flex flex-col gap-2">
    {#each $toasts.filter((t) => t.level === 'bad') as t (t.id)}
      {@render item(t)}
    {/each}
  </div>
  <div role="status" class="flex flex-col gap-2">
    {#each $toasts.filter((t) => t.level !== 'bad') as t (t.id)}
      {@render item(t)}
    {/each}
  </div>
</div>
