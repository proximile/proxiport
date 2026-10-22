<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { goto } from '$app/navigation';
  import { apiGetPage, apiPost, apiPut, apiDelete } from '$lib/api';
  import { parseList, timeoutError } from '$lib/schedules';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  type Kind = 'commands' | 'scripts';
  const PAGE_SIZE = 100;

  let tab: Kind = $state('commands');
  let commands: any[] = $state([]);
  let scripts: any[] = $state([]);
  let totals: Record<Kind, number | null> = $state({ commands: null, scripts: null });
  let loading = $state(true);
  let error = $state('');

  // Edit/create form state. `editing` is null when the form is closed.
  let editing: { kind: Kind; id?: string } | null = $state(null);
  let form = $state<Record<string, any>>({});
  // The form as it was opened, to tell whether there is anything to lose.
  let initial = $state('');
  let saving = $state(false);

  const dirty = $derived(!!editing && JSON.stringify(form) !== initial);

  async function load() {
    loading = true;
    error = '';
    // allSettled never rejects, so failures are read off each result.
    const [c, s] = await Promise.allSettled([
      apiGetPage<any>(`/library/commands?page[limit]=${PAGE_SIZE}`),
      apiGetPage<any>(`/library/scripts?page[limit]=${PAGE_SIZE}`)
    ]);
    if (c.status === 'fulfilled') {
      commands = c.value.rows;
      totals.commands = c.value.count;
    }
    if (s.status === 'fulfilled') {
      scripts = s.value.rows;
      totals.scripts = s.value.count;
    }
    const failures = [c, s].filter((r) => r.status === 'rejected') as PromiseRejectedResult[];
    if (failures.length) {
      error = failures.map((f) => (f.reason instanceof Error ? f.reason.message : String(f.reason))).join('; ');
    }
    loading = false;
  }

  onMount(() => {
    if ($page.url.searchParams.get('tab') === 'scripts') tab = 'scripts';
    load();
  });

  function discardOk(): boolean {
    return !dirty || confirm('Discard unsaved changes?');
  }

  function openForm(next: { kind: Kind; id?: string }, values: Record<string, any>) {
    editing = next;
    form = values;
    initial = JSON.stringify(values);
    error = '';
  }

  function switchTab(t: Kind) {
    if (t === tab) return;
    if (!discardOk()) return;
    editing = null;
    tab = t;
    goto(t === 'commands' ? '?' : `?tab=${t}`, { replaceState: true, keepFocus: true, noScroll: true });
  }

  function startCreate(kind: Kind) {
    if (!discardOk()) return;
    openForm(
      { kind },
      kind === 'commands'
        ? { name: '', cmd: '', tags: '', timeout_sec: 60 }
        : { name: '', script: '', interpreter: '/bin/bash', cwd: '', is_sudo: false, tags: '', timeout_sec: 60 }
    );
  }

  function startEdit(kind: Kind, item: any) {
    if (!discardOk()) return;
    openForm(
      { kind, id: item.id },
      kind === 'commands'
        ? { name: item.name ?? '', cmd: item.cmd ?? '', tags: (item.tags ?? []).join(', '), timeout_sec: item.timeout_sec ?? 60 }
        : {
            name: item.name ?? '',
            script: item.script ?? '',
            interpreter: item.interpreter ?? '/bin/bash',
            cwd: item.cwd ?? '',
            is_sudo: !!item.is_sudo,
            tags: (item.tags ?? []).join(', '),
            timeout_sec: item.timeout_sec ?? 60
          }
    );
  }

  function cancel() {
    editing = null;
    error = '';
  }

  function formValid(): string {
    if (!form.name?.trim()) return 'A name is required.';
    if (editing?.kind === 'commands' && !form.cmd?.trim()) return 'The command is required.';
    if (editing?.kind === 'scripts' && !form.script?.trim()) return 'The script body is required.';
    return timeoutError(form.timeout_sec);
  }

  async function save() {
    const why = formValid();
    if (why) {
      error = why;
      return;
    }
    if (!editing) return;
    saving = true;
    error = '';
    try {
      if (editing.kind === 'commands') {
        const body = {
          name: form.name.trim(),
          cmd: form.cmd,
          tags: parseList(form.tags),
          timeout_sec: Number(form.timeout_sec)
        };
        if (editing.id) await apiPut(`/library/commands/${editing.id}`, body);
        else await apiPost('/library/commands', body);
      } else {
        const body = {
          name: form.name.trim(),
          script: form.script,
          interpreter: form.interpreter?.trim() || undefined,
          cwd: form.cwd?.trim() || undefined,
          is_sudo: !!form.is_sudo,
          tags: parseList(form.tags),
          timeout_sec: Number(form.timeout_sec)
        };
        if (editing.id) await apiPut(`/library/scripts/${editing.id}`, body);
        else await apiPost('/library/scripts', body);
      }
      editing = null;
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  async function remove(kind: Kind, item: any) {
    if (!confirm(`Delete "${item.name ?? item.id}"? This cannot be undone.`)) return;
    error = '';
    try {
      await apiDelete(`/library/${kind}/${item.id}`);
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  function uniqueTags(tags: string[] | null | undefined): string[] {
    return [...new Set(tags ?? [])];
  }

  function firstLine(s: string | undefined): { text: string; more: number } {
    const lines = (s ?? '').split('\n');
    return { text: lines[0], more: lines.length - 1 };
  }

  const shown = $derived(tab === 'commands' ? commands.length : scripts.length);
  const totalForTab = $derived(totals[tab]);
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Library</h1>
    <button class="btn btn-primary" onclick={() => startCreate(tab)}>New {tab === 'commands' ? 'command' : 'script'}</button>
  </div>

  <div class="flex gap-1 border-b border-pp-border">
    {#each ['commands', 'scripts'] as const as t}
      <button
        class="px-4 py-2 text-sm border-b-2 -mb-px cursor-pointer"
        class:border-indigo-400={tab === t}
        class:text-indigo-300={tab === t}
        class:border-transparent={tab !== t}
        class:text-slate-400={tab !== t}
        onclick={() => switchTab(t)}
      >
        {t === 'commands' ? `Saved commands (${commands.length})` : `Saved scripts (${scripts.length})`}
      </button>
    {/each}
  </div>

  <ErrorBox message={error} />

  {#if editing}
    <div class="card p-4 space-y-3">
      <h2 class="font-medium">
        {editing.id ? 'Edit' : 'New'} {editing.kind === 'commands' ? 'command' : 'script'}
      </h2>
      <label class="block text-xs">
        <span class="block text-slate-400 mb-1">Name</span>
        <input bind:value={form.name} placeholder="e.g. Restart nginx" />
      </label>

      {#if editing.kind === 'commands'}
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Command</span>
          <textarea bind:value={form.cmd} rows="4" class="font-mono text-xs" placeholder="systemctl restart nginx"></textarea>
        </label>
      {:else}
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Script</span>
          <textarea bind:value={form.script} rows="8" class="font-mono text-xs" placeholder="#!/bin/bash&#10;set -e&#10;…"></textarea>
        </label>
        <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
          <label class="block text-xs">
            <span class="block text-slate-400 mb-1">Interpreter</span>
            <input bind:value={form.interpreter} class="font-mono" placeholder="/bin/bash" />
          </label>
          <label class="block text-xs">
            <span class="block text-slate-400 mb-1">Working directory</span>
            <input bind:value={form.cwd} class="font-mono" placeholder="/tmp" />
          </label>
          <label class="flex items-end gap-2 text-xs">
            <input type="checkbox" bind:checked={form.is_sudo} class="!w-auto" />
            <span class="text-slate-300">Run as sudo</span>
          </label>
        </div>
      {/if}

      <label class="block text-xs max-w-[12rem]">
        <span class="block text-slate-400 mb-1">Timeout (sec)</span>
        <input type="number" min="1" step="1" bind:value={form.timeout_sec} class="font-mono" />
      </label>

      <label class="block text-xs">
        <span class="block text-slate-400 mb-1">Tags (comma-separated)</span>
        <input bind:value={form.tags} placeholder="ops, nginx" />
      </label>

      <div class="flex gap-2">
        <button class="btn btn-primary" onclick={save} disabled={saving}>
          {#if saving}<Spinner label="Saving…" />{:else}Save{/if}
        </button>
        <button class="btn btn-ghost" onclick={cancel} disabled={saving}>Cancel</button>
      </div>
    </div>
  {/if}

  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if tab === 'commands'}
      {#if !commands.length}
        <EmptyState title="No saved commands" detail="Create one to reuse it across clients." />
      {:else}
        <table class="tbl">
          <thead><tr><th>Name</th><th>Command</th><th>Tags</th><th class="text-right">Actions</th></tr></thead>
          <tbody>
            {#each commands as c}
              {@const cmd = firstLine(c.cmd)}
              <tr>
                <td class="font-medium">{c.name ?? '—'}</td>
                <td class="font-mono text-xs truncate max-w-md" title={c.cmd ?? ''}>
                  {cmd.text || '—'}{#if cmd.more}<span class="ml-1 text-slate-500 font-sans">(+{cmd.more} more {cmd.more === 1 ? 'line' : 'lines'})</span>{/if}
                </td>
                <td>{#each uniqueTags(c.tags) as t}<span class="pill pill-info">{t}</span>{/each}</td>
                <td class="text-right whitespace-nowrap">
                  <button class="btn btn-ghost btn-sm" onclick={() => startEdit('commands', c)}>Edit</button>
                  <button class="btn btn-ghost btn-sm text-rose-300" onclick={() => remove('commands', c)}>Delete</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    {:else}
      {#if !scripts.length}
        <EmptyState title="No saved scripts" detail="Create one to reuse it across clients." />
      {:else}
        <table class="tbl">
          <thead><tr><th>Name</th><th>Interpreter</th><th>Tags</th><th class="text-right">Actions</th></tr></thead>
          <tbody>
            {#each scripts as s}
              <tr>
                <td class="font-medium">{s.name ?? '—'}</td>
                <td class="font-mono text-xs">{s.interpreter?.trim() || '—'}</td>
                <td>{#each uniqueTags(s.tags) as t}<span class="pill pill-info">{t}</span>{/each}</td>
                <td class="text-right whitespace-nowrap">
                  <button class="btn btn-ghost btn-sm" onclick={() => startEdit('scripts', s)}>Edit</button>
                  <button class="btn btn-ghost btn-sm text-rose-300" onclick={() => remove('scripts', s)}>Delete</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    {/if}
    {#if !loading && totalForTab !== null && totalForTab > shown}
      <div class="px-4 py-2 border-t border-pp-border text-xs text-slate-400">
        Showing the first {shown} of {totalForTab}.
      </div>
    {/if}
  </div>
</div>
