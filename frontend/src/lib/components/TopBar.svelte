<script lang="ts">
  import { logout } from '../api';
  import { goto, afterNavigate } from '$app/navigation';
  import { onMount } from 'svelte';
  import { apiGet } from '../api';
  import { sidebarMobileOpen } from '../stores';
  import type { User } from '../types';

  let me: User | null = $state(null);
  let menuOpen = $state(false);
  let menuWrap: HTMLDivElement | undefined = $state();
  let menuBtn: HTMLButtonElement | undefined = $state();

  // The user menu is a disclosure: it closes on Escape (returning focus to its
  // button), on a click or focus move outside it, and on navigation.
  afterNavigate(() => (menuOpen = false));

  $effect(() => {
    if (!menuOpen) return;
    const onPointerDown = (e: PointerEvent) => {
      if (menuWrap && !menuWrap.contains(e.target as Node)) menuOpen = false;
    };
    document.addEventListener('pointerdown', onPointerDown);
    return () => document.removeEventListener('pointerdown', onPointerDown);
  });

  function onMenuKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && menuOpen) {
      menuOpen = false;
      menuBtn?.focus();
    }
  }

  function onMenuFocusout(e: FocusEvent) {
    const next = e.relatedTarget as Node | null;
    if (next && menuWrap && !menuWrap.contains(next)) menuOpen = false;
  }

  onMount(async () => {
    try {
      me = await apiGet<User>('/me');
    } catch (_) {
      // ignore - if /me fails, the layout will redirect to /login
    }
  });

  async function doLogout() {
    await logout();
    goto('/auth', { replaceState: true });
  }
</script>

<header class="h-14 flex items-center justify-between gap-2 px-3 sm:px-5 border-b border-pp-border bg-pp-surface flex-shrink-0">
  <div class="flex items-center gap-3 text-sm text-slate-400 min-w-0 flex-1">
    <button
      id="sidebar-open"
      class="md:hidden -ml-1 p-1.5 rounded text-slate-300 hover:text-slate-100 hover:bg-pp-surface-2 cursor-pointer flex-shrink-0"
      onclick={() => { menuOpen = false; sidebarMobileOpen.set(true); }}
      aria-label="Open menu"
      aria-controls="app-sidebar"
      aria-expanded={$sidebarMobileOpen}
    >
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M4 6h16M4 12h16M4 18h16" />
      </svg>
    </button>
    <span class="hidden sm:inline text-slate-500 flex-shrink-0">Server:</span>
    <span class="font-mono text-slate-300 truncate min-w-0" title={location.host}>{location.host}</span>
  </div>

  <div class="relative flex-shrink-0" bind:this={menuWrap} onkeydown={onMenuKeydown} onfocusout={onMenuFocusout} role="presentation">
    <button
      bind:this={menuBtn}
      class="flex items-center gap-2 px-2 sm:px-3 py-1.5 rounded-md hover:bg-pp-surface-2 cursor-pointer"
      onclick={() => (menuOpen = !menuOpen)}
      aria-expanded={menuOpen}
      aria-controls="user-menu"
      aria-label={me ? `User menu for ${me.username}` : 'User menu'}
    >
      <div class="w-7 h-7 rounded-full bg-indigo-500/20 text-indigo-300 flex items-center justify-center text-xs font-semibold">
        {(me?.username ?? '?').slice(0, 1).toUpperCase()}
      </div>
      <span class="text-sm text-slate-200 hidden min-[360px]:inline max-w-[10rem] truncate">{me?.username ?? '…'}</span>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 9l6 6 6-6" /></svg>
    </button>

    {#if menuOpen}
      <div id="user-menu" class="absolute right-0 mt-1 w-44 card py-1 z-30">
        <a href="/settings/profile" class="block px-3 py-2 text-sm hover:bg-pp-surface-2" onclick={() => (menuOpen = false)}>Profile</a>
        <a href="/settings/api-tokens" class="block px-3 py-2 text-sm hover:bg-pp-surface-2" onclick={() => (menuOpen = false)}>API tokens</a>
        <button class="block w-full text-left px-3 py-2 text-sm hover:bg-pp-surface-2 text-red-300 cursor-pointer" onclick={doLogout}>
          Sign out
        </button>
      </div>
    {/if}
  </div>
</header>
