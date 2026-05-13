<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { apiGet } from '$lib/api';
  import type { Client } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let { children } = $props();
  let client: Client | null = $state(null);
  let loading = $state(true);
  let error = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    try {
      client = await apiGet<Client>(`/clients/${id}`);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
      client = null;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = $page.params.id;
    if (id) load(id);
  });

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
</script>

<div class="p-6 space-y-4">
  <div class="flex items-baseline gap-3">
    <a href="/inventory" class="text-slate-500 hover:text-slate-300 text-sm">← Inventory</a>
    {#if client}
      <h1 class="text-2xl font-semibold tracking-tight">{client.name || client.id}</h1>
      <span class="font-mono text-xs text-slate-500">{client.id}</span>
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

  <div class="flex gap-1 border-b border-pp-border overflow-x-auto">
    {#each TABS as tab}
      {@const href = tab.slug ? `${baseHref}/${tab.slug}` : baseHref}
      {@const active = isActive(tab.slug, $page.url.pathname)}
      <a
        {href}
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
</div>
