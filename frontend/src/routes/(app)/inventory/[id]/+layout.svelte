<script lang="ts">
  import { untrack } from 'svelte';
  import { page } from '$app/stores';
  import { apiGet } from '$lib/api';
  import type { Client } from '$lib/types';
  import { setClientContext } from '$lib/clientContext';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let { children } = $props();
  let client: Client | null = $state(null);
  let loading = $state(true);
  let error = $state('');

  // Only the newest request may write state: switching from one client to
  // another while the first is still loading must not show the first one's
  // record under the second one's URL.
  let seq = 0;

  async function load(id: string) {
    const mine = ++seq;
    loading = true;
    error = '';
    if (client && client.id !== id) client = null;
    try {
      const c = await apiGet<Client>(`/clients/${id}`);
      if (mine === seq) client = c;
    } catch (err) {
      if (mine !== seq) return;
      // A failed refresh keeps the record already shown for this id.
      error = err instanceof Error ? err.message : String(err);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  // Track the id alone, so moving between tabs of the same client does not
  // fetch it again (load reads client, which must not become a dependency).
  let id = $derived($page.params.id);
  $effect(() => {
    const cur = id;
    if (cur) untrack(() => load(cur));
  });

  setClientContext({
    get client() {
      return client;
    },
    get loading() {
      return loading;
    },
    get error() {
      return error;
    },
    reload: async () => {
      if (id) await load(id);
    }
  });

  // A failed first load (an unknown id, say) leaves nothing to show in the
  // tabs; the error alone is the page.
  let failed = $derived(!!error && !client && !loading);

  const TABS = [
    { slug: '', label: 'Inventory' },
    { slug: 'tunnels', label: 'Tunnels' },
    { slug: 'meta-data', label: 'Meta-Data' },
    { slug: 'documents', label: 'Documents' },
    { slug: 'commands', label: 'Commands' },
    { slug: 'scripts', label: 'Scripts' },
    { slug: 'monitoring', label: 'Monitoring' },
    { slug: 'files', label: 'Files' },
    { slug: 'audit', label: 'Audit' },
    { slug: 'schedules', label: 'Schedules' }
  ];

  let baseHref = $derived(`/inventory/${$page.params.id}`);

  function isActive(slug: string, current: string): boolean {
    const path = current.replace(baseHref, '').replace(/^\//, '');
    return slug === '' ? path === '' : path === slug || path.startsWith(slug + '/');
  }

  // Bring the active tab into view within the horizontally-scrolling strip so
  // there is always a visible active-tab indicator on narrow viewports.
  function scrollActiveIntoView(el: HTMLElement, active: boolean) {
    const reveal = (isActive: boolean) => {
      if (isActive) el.scrollIntoView({ inline: 'nearest', block: 'nearest' });
    };
    reveal(active);
    return { update: reveal };
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
    <a href="/inventory" class="text-slate-500 hover:text-slate-300 text-sm whitespace-nowrap">← Inventory</a>
    {#if client}
      <h1 class="text-2xl font-semibold tracking-tight whitespace-nowrap">{client.name || client.id}</h1>
      <span class="font-mono text-xs text-slate-500 break-all min-w-0">{client.id}</span>
      {#if client.connection_state === 'connected'}
        <span class="pill pill-good">connected</span>
      {:else}
        <span class="pill pill-muted">disconnected</span>
      {/if}
    {:else if loading}
      <Spinner />
    {/if}
  </div>

  <ErrorBox message={error} />

  {#if failed}
    <button class="btn btn-ghost" onclick={() => id && load(id)}>Retry</button>
  {:else}
  <div class="flex gap-1 border-b border-pp-border overflow-x-auto">
    {#each TABS as tab}
      {@const href = tab.slug ? `${baseHref}/${tab.slug}` : baseHref}
      {@const active = isActive(tab.slug, $page.url.pathname)}
      <a
        {href}
        use:scrollActiveIntoView={active}
        class="px-4 py-2 text-sm whitespace-nowrap border-b-2 -mb-px"
        class:border-indigo-400={active}
        class:text-indigo-300={active}
        class:border-transparent={!active}
        class:text-slate-400={!active}
        class:hover:text-slate-200={!active}
        class:hover:border-slate-600={!active}
      >
        {tab.label}
      </a>
    {/each}
  </div>

  <div>
    {@render children()}
  </div>
  {/if}
</div>
