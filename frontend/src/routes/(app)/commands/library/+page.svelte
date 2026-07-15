<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let tab: 'commands' | 'scripts' = $state('commands');
  let commands: any[] = $state([]);
  let scripts: any[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    // Promise.allSettled never rejects, so failures must be read off each
    // result — otherwise a failed fetch is indistinguishable from an empty
    // library and the ErrorBox below never shows.
    const [c, s] = await Promise.allSettled([
      apiGet<any[]>('/library/commands?page[limit]=100'),
      apiGet<any[]>('/library/scripts?page[limit]=100')
    ]);
    if (c.status === 'fulfilled') commands = c.value ?? [];
    if (s.status === 'fulfilled') scripts = s.value ?? [];
    const failures = [c, s].filter((r) => r.status === 'rejected') as PromiseRejectedResult[];
    if (failures.length) {
      error = failures
        .map((f) => (f.reason instanceof Error ? f.reason.message : String(f.reason)))
        .join('; ');
    }
    loading = false;
  }

  onMount(load);
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">Library</h1>
  <div class="flex gap-1 border-b border-pp-border">
    {#each ['commands', 'scripts'] as const as t}
      <button
        class="px-4 py-2 text-sm border-b-2 -mb-px cursor-pointer"
        class:border-indigo-400={tab === t}
        class:text-indigo-300={tab === t}
        class:border-transparent={tab !== t}
        class:text-slate-400={tab !== t}
        onclick={() => (tab = t)}
      >
        {t === 'commands' ? `Saved commands (${commands.length})` : `Saved scripts (${scripts.length})`}
      </button>
    {/each}
  </div>
  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if tab === 'commands'}
      {#if !commands.length}
        <EmptyState title="No saved commands" />
      {:else}
        <table class="tbl">
          <thead><tr><th>Name</th><th>Command</th><th>Folder</th><th>Tags</th></tr></thead>
          <tbody>
            {#each commands as c}
              <tr>
                <td class="font-medium">{c.name ?? '—'}</td>
                <td class="font-mono text-xs truncate max-w-md">{c.cmd ?? c.command ?? '—'}</td>
                <td class="text-slate-400">{c.folder ?? '/'}</td>
                <td>
                  {#each c.tags ?? [] as t}<span class="pill pill-info">{t}</span>{/each}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    {:else}
      {#if !scripts.length}
        <EmptyState title="No saved scripts" />
      {:else}
        <table class="tbl">
          <thead><tr><th>Name</th><th>Interpreter</th><th>Folder</th><th>Tags</th></tr></thead>
          <tbody>
            {#each scripts as s}
              <tr>
                <td class="font-medium">{s.name ?? '—'}</td>
                <td class="font-mono text-xs">{s.interpreter ?? '—'}</td>
                <td class="text-slate-400">{s.folder ?? '/'}</td>
                <td>
                  {#each s.tags ?? [] as t}<span class="pill pill-info">{t}</span>{/each}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    {/if}
  </div>
</div>
