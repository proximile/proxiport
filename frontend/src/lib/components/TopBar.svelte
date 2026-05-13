<script lang="ts">
  import { logout } from '../api';
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { apiGet } from '../api';
  import type { User } from '../types';

  let me: User | null = $state(null);
  let menuOpen = $state(false);

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

<header class="h-14 flex items-center justify-between px-5 border-b border-pp-border bg-pp-surface flex-shrink-0">
  <div class="flex items-center gap-3 text-sm text-slate-400">
    <span class="text-slate-500">Server:</span>
    <span class="font-mono text-slate-300">{location.host}</span>
  </div>

  <div class="relative">
    <button
      class="flex items-center gap-2 px-3 py-1.5 rounded-md hover:bg-pp-surface-2 cursor-pointer"
      onclick={() => (menuOpen = !menuOpen)}
    >
      <div class="w-7 h-7 rounded-full bg-indigo-500/20 text-indigo-300 flex items-center justify-center text-xs font-semibold">
        {(me?.username ?? '?').slice(0, 1).toUpperCase()}
      </div>
      <span class="text-sm text-slate-200">{me?.username ?? '…'}</span>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M6 9l6 6 6-6" /></svg>
    </button>

    {#if menuOpen}
      <div class="absolute right-0 mt-1 w-44 card py-1 z-30" role="menu">
        <a href="/settings/profile" class="block px-3 py-2 text-sm hover:bg-pp-surface-2" onclick={() => (menuOpen = false)}>Profile</a>
        <a href="/settings/api-tokens" class="block px-3 py-2 text-sm hover:bg-pp-surface-2" onclick={() => (menuOpen = false)}>API tokens</a>
        <button class="block w-full text-left px-3 py-2 text-sm hover:bg-pp-surface-2 text-red-300 cursor-pointer" onclick={doLogout}>
          Sign out
        </button>
      </div>
    {/if}
  </div>
</header>
