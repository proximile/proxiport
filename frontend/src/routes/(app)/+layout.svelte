<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { tokenStore } from '$lib/stores';
  import { loginUrl } from '$lib/redirect';
  import { sectionTitle } from '$lib/pageTitle';
  import { page } from '$app/stores';
  import { refreshVaultStatus } from '$lib/api';
  import { get } from 'svelte/store';
  import Sidebar from '$lib/components/Sidebar.svelte';
  import TopBar from '$lib/components/TopBar.svelte';
  import { sidebarMobileOpen } from '$lib/stores';

  let { children } = $props();
  let ready = $state(false);
  // Below the md breakpoint the sidebar is an off-canvas drawer.
  let mobile = $state(false);
  let drawerOpen = $derived(mobile && $sidebarMobileOpen);
  let title = $derived(sectionTitle($page.url.pathname));

  function toAuth() {
    goto(loginUrl(location.pathname + location.search), { replaceState: true });
  }

  onMount(() => {
    const mq = window.matchMedia('(max-width: 767.98px)');
    const sync = () => {
      mobile = mq.matches;
      // A drawer left open while the window widens would pop back open later.
      if (!mobile) sidebarMobileOpen.set(false);
    };
    sync();
    mq.addEventListener('change', sync);

    if (!get(tokenStore)) {
      toAuth();
    } else {
      ready = true;
      refreshVaultStatus().catch(() => {});
    }
    return () => mq.removeEventListener('change', sync);
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && drawerOpen) sidebarMobileOpen.set(false);
  }

  // Reactive watchdog: if any apiGet hits 401 it null-sets tokenStore.
  // Bounce to /auth as soon as that happens so child pages stop firing
  // requests against a dead session.
  $effect(() => {
    if (ready && $tokenStore === null) {
      toAuth();
    }
  });
</script>

<svelte:window onkeydown={onKeydown} />

<svelte:head>
  {#if title}<title>{title}</title>{/if}
</svelte:head>

{#if ready}
  <a
    href="#main"
    inert={drawerOpen}
    class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:px-3 focus:py-2 focus:rounded focus:bg-pp-surface-2 focus:text-slate-100"
  >Skip to content</a>
  <div class="h-screen flex">
    <Sidebar {mobile} />
    {#if $sidebarMobileOpen}
      <!-- Pointer-only backdrop; keyboard users close the drawer with Escape or its own button. -->
      <div
        class="fixed inset-0 z-30 bg-black/50 md:hidden"
        onclick={() => sidebarMobileOpen.set(false)}
        aria-hidden="true"
      ></div>
    {/if}
    <!-- While the drawer is open the page behind it is inert, which keeps focus in the drawer. -->
    <div class="flex-1 flex flex-col min-w-0" inert={drawerOpen}>
      <TopBar />
      <main id="main" tabindex="-1" class="flex-1 overflow-auto focus:outline-none">
        {@render children()}
      </main>
    </div>
  </div>
{/if}
