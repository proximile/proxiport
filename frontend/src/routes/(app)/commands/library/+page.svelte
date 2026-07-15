<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiPut, apiDelete } from '$lib/api';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  type Kind = 'commands' | 'scripts';

  let tab: Kind = $state('commands');
  let commands: any[] = $state([]);
  let scripts: any[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  // Edit/create form state. `editing` is null when the form is closed.
  let editing: { kind: Kind; id?: string } | null = $state(null);
  let form = $state<Record<string, any>>({});
  let saving = $state(false);

  async function load() {
    loading = true;
    error = '';
    // allSettled never rejects, so failures are read off each result.
    const [c, s] = await Promise.allSettled([
      apiGet<any[]>('/library/commands?page[limit]=100'),
      apiGet<any[]>('/library/scripts?page[limit]=100')
    ]);
    if (c.status === 'fulfilled') commands = c.value ?? [];
    if (s.status === 'fulfilled') scripts = s.value ?? [];
    const failures = [c, s].filter((r) => r.status === 'rejected') as PromiseRejectedResult[];
    if (failures.length) {
      error = failures.map((f) => (f.reason instanceof Error ? f.reason.message : String(f.reason))).join('; ');
    }
    loading = false;
  }

  onMount(load);

  function startCreate(kind: Kind) {
    editing = { kind };
    form =
      kind === 'commands'
        ? { name: '', cmd: '', tags: '', timeout_sec: 60 }
        : { name: '', script: '', interpreter: '/bin/bash', cwd: '', is_sudo: false, tags: '' };
  }

  function startEdit(kind: Kind, item: any) {
    editing = { kind, id: item.id };
    form =
      kind === 'commands'
        ? { name: item.name ?? '', cmd: item.cmd ?? '', tags: (item.tags ?? []).join(', '), timeout_sec: item.timeout_sec ?? 60 }
        : {
            name: item.name ?? '',
            script: item.script ?? '',
            interpreter: item.interpreter ?? '/bin/bash',
            cwd: item.cwd ?? '',
            is_sudo: !!item.is_sudo,
            tags: (item.tags ?? []).join(', ')
          };
  }

  function cancel() {
    editing = null;
  }

  function parseTags(s: string): string[] {
    return s.split(',').map((t) => t.trim()).filter(Boolean);
  }

  function formValid(): string {
    if (!form.name?.trim()) return 'A name is required.';
    if (editing?.kind === 'commands' && !form.cmd?.trim()) return 'The command is required.';
    if (editing?.kind === 'scripts' && !form.script?.trim()) return 'The script body is required.';
    return '';
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
          tags: parseTags(form.tags),
          timeout_sec: Number(form.timeout_sec) || 60
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
          tags: parseTags(form.tags)
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
        onclick={() => (tab = t)}
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
        <label class="block text-xs max-w-[12rem]">
          <span class="block text-slate-400 mb-1">Timeout (sec)</span>
          <input type="number" min="1" bind:value={form.timeout_sec} class="font-mono" />
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
              <tr>
                <td class="font-medium">{c.name ?? '—'}</td>
                <td class="font-mono text-xs truncate max-w-md">{c.cmd ?? '—'}</td>
                <td>{#each c.tags ?? [] as t}<span class="pill pill-info">{t}</span>{/each}</td>
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
                <td class="font-mono text-xs">{s.interpreter ?? '—'}</td>
                <td>{#each s.tags ?? [] as t}<span class="pill pill-info">{t}</span>{/each}</td>
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
  </div>
</div>
