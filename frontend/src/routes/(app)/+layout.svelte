<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { tokenStore } from '$lib/stores';
  import { refreshVaultStatus } from '$lib/api';
  import { get } from 'svelte/store';
  import Sidebar from '$lib/components/Sidebar.svelte';
  import TopBar from '$lib/components/TopBar.svelte';
  import { sidebarMobileOpen } from '$lib/stores';

  let { children } = $props();
  let ready = $state(false);

  onMount(() => {
    if (!get(tokenStore)) {
      goto('/auth', { replaceState: true });
      return;
    }
    ready = true;
    refreshVaultStatus().catch(() => {});
  });

  // Reactive watchdog: if any apiGet hits 401 it null-sets tokenStore.
  // Bounce to /auth as soon as that happens so child pages stop firing
  // requests against a dead session.
  $effect(() => {
    if (ready && $tokenStore === null) {
      goto('/auth', { replaceState: true });
    }
  });
</script>

{#if ready}
  <div class="h-screen flex">
    <Sidebar />
    {#if $sidebarMobileOpen}
      <button
        class="fixed inset-0 z-30 bg-black/50 md:hidden"
        onclick={() => sidebarMobileOpen.set(false)}
        aria-label="Close menu"
      ></button>
    {/if}
    <div class="flex-1 flex flex-col min-w-0">
      <TopBar />
      <main class="flex-1 overflow-auto">
        {@render children()}
      </main>
    </div>
  </div>
{/if}
