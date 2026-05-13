<script lang="ts">
  import { page } from '$app/stores';
  import { sidebarCollapsed } from '../stores';

  type Item = { href: string; label: string; icon: string; matchPrefix?: string };
  type Section = { title?: string; items: Item[] };

  const sections: Section[] = [
    {
      items: [
        { href: '/inventory', label: 'Inventory', icon: 'server', matchPrefix: '/inventory' },
        { href: '/tunnels', label: 'Tunnels', icon: 'network', matchPrefix: '/tunnels' },
        { href: '/commands', label: 'Commands', icon: 'terminal' },
        { href: '/scripts', label: 'Scripts', icon: 'code', matchPrefix: '/scripts' },
        { href: '/commands/schedules', label: 'Schedules', icon: 'clock' },
        { href: '/commands/library', label: 'Library', icon: 'library' },
        { href: '/settings/audit', label: 'Audit', icon: 'shield' }
      ]
    },
    {
      title: 'Admin',
      items: [
        { href: '/settings/users', label: 'Users', icon: 'user' },
        { href: '/settings/user-groups', label: 'User Groups', icon: 'users' },
        { href: '/settings/client-access', label: 'Client Access', icon: 'key' },
        { href: '/settings/client-groups', label: 'Client Groups', icon: 'group' },
        { href: '/settings/vault', label: 'Vault', icon: 'lock' },
        { href: '/settings/api-tokens', label: 'API Tokens', icon: 'token' },
        { href: '/settings/info', label: 'Info', icon: 'info' },
        { href: '/documentation', label: 'Documentation', icon: 'book' },
        { href: '/settings/support', label: 'Support', icon: 'help' }
      ]
    }
  ];

  function active(item: Item, currentPath: string): boolean {
    if (item.matchPrefix) return currentPath === item.matchPrefix || currentPath.startsWith(item.matchPrefix + '/');
    // exact-match for items without a prefix; commands/library should not light up Commands etc.
    return currentPath === item.href;
  }

  const ICONS: Record<string, string> = {
    server: 'M2 5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2zm0 11a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2v3a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2zM6 6.5h.01M6 17.5h.01',
    network: 'M5 12V5h14v7M5 12h14M5 12v7h14v-7M9 5v3M15 5v3M9 19v-3M15 19v-3',
    terminal: 'M4 6h16v12H4zM7 10l3 2-3 2M13 14h4',
    code: 'M16 18l6-6-6-6M8 6l-6 6 6 6',
    clock: 'M12 8v4l3 2M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z',
    library: 'M3 5h2v14H3zM7 5h2v14H7zM12 4l9 16-2 1L10 5z',
    shield: 'M12 2 4 6v6c0 5 3.4 9.4 8 10 4.6-.6 8-5 8-10V6z',
    user: 'M12 12a4 4 0 1 0-4-4 4 4 0 0 0 4 4zM4 22a8 8 0 0 1 16 0',
    users: 'M9 12a4 4 0 1 0-4-4 4 4 0 0 0 4 4zm10 4v6h-4v-4a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v4h4M16 11a3 3 0 1 0-3-3 3 3 0 0 0 3 3z',
    key: 'M21 2l-3 3m-2 2l-7 7m-3 3l-2 2-2-2 2-2m6-6a4 4 0 1 1 6 6 4 4 0 0 1-6-6z',
    group: 'M3 7l9-4 9 4-9 4zM3 12l9 4 9-4M3 17l9 4 9-4',
    lock: 'M5 11h14v10H5zM7 11V8a5 5 0 0 1 10 0v3',
    token: 'M2 12l5 5 13-13M14 6h6v6',
    info: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zm0-13v.01M12 11v6',
    book: 'M4 4h7a3 3 0 0 1 3 3v14a2 2 0 0 0-2-2H4zM20 4h-7a3 3 0 0 0-3 3v14a2 2 0 0 1 2-2h8z',
    help: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.5 9a2.5 2.5 0 1 1 3.6 2.2c-.9.5-1.6 1-1.6 2v.3M12 17h.01'
  };
</script>

<aside
  class="bg-pp-surface border-r border-pp-border flex-shrink-0 flex flex-col h-full transition-all duration-150"
  class:w-56={!$sidebarCollapsed}
  class:w-14={$sidebarCollapsed}
>
  <div class="h-14 flex items-center justify-between px-3 border-b border-pp-border">
    {#if !$sidebarCollapsed}
      <div class="text-base font-semibold text-indigo-300 tracking-tight">ProxiPort</div>
    {:else}
      <div class="text-base font-semibold text-indigo-300 mx-auto">PP</div>
    {/if}
    <button
      class="text-slate-400 hover:text-slate-100 cursor-pointer p-1 rounded hover:bg-pp-surface-2"
      onclick={() => sidebarCollapsed.update((v) => !v)}
      aria-label="Toggle sidebar"
    >
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d={$sidebarCollapsed ? 'M9 6l6 6-6 6' : 'M15 6l-6 6 6 6'} />
      </svg>
    </button>
  </div>

  <nav class="flex-1 overflow-y-auto py-2">
    {#each sections as section}
      {#if section.title && !$sidebarCollapsed}
        <div class="px-3 pt-3 pb-1 text-xs uppercase tracking-wider text-slate-500">
          {section.title}
        </div>
      {/if}
      <ul>
        {#each section.items as item}
          <li>
            <a
              href={item.href}
              class="flex items-center gap-3 px-3 py-2 mx-2 my-0.5 rounded-md text-sm transition-colors"
              class:bg-pp-surface-2={active(item, $page.url.pathname)}
              class:text-indigo-300={active(item, $page.url.pathname)}
              class:text-slate-300={!active(item, $page.url.pathname)}
              class:hover:bg-pp-surface-2={!active(item, $page.url.pathname)}
              title={$sidebarCollapsed ? item.label : ''}
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="flex-shrink-0">
                <path d={ICONS[item.icon] ?? ''} />
              </svg>
              {#if !$sidebarCollapsed}
                <span class="truncate">{item.label}</span>
              {/if}
            </a>
          </li>
        {/each}
      </ul>
    {/each}
  </nav>
</aside>
