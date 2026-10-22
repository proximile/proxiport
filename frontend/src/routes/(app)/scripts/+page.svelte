<script lang="ts">
  import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">Scripts</h1>
  <p class="text-sm text-slate-400">
    Running one script on several clients at once is not available in the console yet. To run a
    script, open a client and use its Scripts tab. The Commands page runs a single command, not a
    multi-line script, on several clients; the API's <span class="font-mono">POST /api/v1/scripts</span>
    endpoint runs a script on several clients.
  </p>
  <a href="/inventory" class="btn btn-primary inline-flex w-fit">Choose a client</a>
  <EmptyState title="Multi-target scripts are not in the console yet" detail="Use a client's Scripts tab to run a script on one client." />
</div>
