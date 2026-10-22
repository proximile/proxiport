<script lang="ts">
  import { page } from '$app/stores';
  import { tokenStore } from '$lib/stores';

  let notFound = $derived($page.status === 404);
  let heading = $derived(notFound ? 'Page not found' : 'Something went wrong');
</script>

<svelte:head><title>{heading} · ProxiPort</title></svelte:head>

<main class="min-h-screen flex items-center justify-center px-4">
  <div class="w-full max-w-md card p-6 space-y-4 text-center">
    <div class="text-2xl font-semibold tracking-tight text-indigo-300">ProxiPort</div>
    <div>
      <div class="font-mono text-sm text-slate-500">{$page.status}</div>
      <h1 class="text-lg font-medium text-slate-200 mt-1">{heading}</h1>
      <p class="text-sm text-slate-400 mt-2">
        {#if notFound}
          There is no page at <span class="font-mono break-all text-slate-300">{$page.url.pathname}</span>.
        {:else}
          {$page.error?.message ?? 'The page could not be loaded.'}
        {/if}
      </p>
    </div>
    <div class="flex justify-center gap-2">
      {#if $tokenStore}
        <a href="/inventory" class="btn btn-primary">Go to Inventory</a>
      {:else}
        <a href="/auth" class="btn btn-primary">Sign in</a>
      {/if}
    </div>
  </div>
</main>
