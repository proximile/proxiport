<script lang="ts">
  import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">Scripts</h1>
  <p class="text-sm text-slate-400">
    Multi-target script runner. The single-client variant lives at the per-client Scripts tab.
    The multi-target variant shares ~80% of the Commands page; for now use Commands with an
    interpreter selector — full multi-target script UI ships in a later milestone.
  </p>
  <a href="/commands" class="btn btn-primary inline-flex w-fit">Use the Commands runner with an interpreter</a>
  <EmptyState title="Multi-target scripts UI coming next" detail="Per-client scripts tab is fully working today." />
</div>
