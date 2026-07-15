<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiPut, apiDelete, asList } from '$lib/api';
  import type { Schedule, Client } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import { pushToast } from '$lib/stores';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: Schedule[] = $state([]);
  let clients: Client[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  let editing: { id?: string } | null = $state(null);
  let form = $state<Record<string, any>>({});
  let saving = $state(false);

  function blankForm() {
    return {
      name: '',
      schedule: '*/5 * * * *',
      type: 'command',
      command: '',
      script: '',
      interpreter: '/bin/bash',
      cwd: '',
      is_sudo: false,
      timeout_sec: 60,
      client_ids: [] as string[],
      group_ids: '',
      execute_concurrently: false,
      abort_on_error: true
    };
  }

  async function load() {
    loading = true;
    error = '';
    try {
      rows = asList<Schedule>(await apiGet<unknown>('/schedules?page[limit]=100'));
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
    // Client list for the target picker — best-effort.
    try {
      clients = asList<Client>(
        await apiGet<unknown>('/clients?fields[clients]=id,name,hostname&page[limit]=500')
      );
    } catch {
      clients = [];
    }
  }

  onMount(load);

  function startCreate() {
    editing = {};
    form = blankForm();
  }

  async function startEdit(id: string) {
    error = '';
    try {
      const s = (await apiGet<any>(`/schedules/${id}`)) ?? {};
      const data = s.data ?? s; // tolerate a {data:…} envelope
      editing = { id };
      form = {
        ...blankForm(),
        name: data.name ?? '',
        schedule: data.schedule ?? '',
        type: data.type ?? 'command',
        command: data.command ?? '',
        script: data.script ?? '',
        interpreter: data.interpreter ?? '/bin/bash',
        cwd: data.cwd ?? '',
        is_sudo: !!data.is_sudo,
        timeout_sec: data.timeout_sec ?? 60,
        client_ids: data.client_ids ?? [],
        group_ids: (data.group_ids ?? []).join(', '),
        execute_concurrently: !!data.execute_concurrently,
        abort_on_error: data.abort_on_error ?? true
      };
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  function cancel() {
    editing = null;
  }

  function parseList(s: string): string[] {
    return s.split(',').map((t) => t.trim()).filter(Boolean);
  }

  function formValid(): string {
    if (!form.name?.trim()) return 'A name is required.';
    if (!form.schedule?.trim()) return 'A cron schedule is required.';
    if (form.type === 'command' && !form.command?.trim()) return 'The command is required.';
    if (form.type === 'script' && !form.script?.trim()) return 'The script body is required.';
    if (!form.client_ids.length && !parseList(form.group_ids).length) {
      return 'Select at least one target client or group.';
    }
    return '';
  }

  async function save() {
    const why = formValid();
    if (why) {
      error = why;
      return;
    }
    saving = true;
    error = '';
    const body: Record<string, any> = {
      name: form.name.trim(),
      schedule: form.schedule.trim(),
      type: form.type,
      client_ids: form.client_ids,
      group_ids: parseList(form.group_ids),
      timeout_sec: Number(form.timeout_sec) || 60,
      execute_concurrently: !!form.execute_concurrently,
      abort_on_error: !!form.abort_on_error
    };
    if (form.type === 'command') {
      body.command = form.command;
    } else {
      body.script = form.script;
      body.interpreter = form.interpreter?.trim() || undefined;
      body.cwd = form.cwd?.trim() || undefined;
      body.is_sudo = !!form.is_sudo;
    }
    try {
      if (editing?.id) await apiPut(`/schedules/${editing.id}`, body);
      else await apiPost('/schedules', body);
      pushToast('good', editing?.id ? 'Schedule updated.' : 'Schedule created.');
      editing = null;
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  async function del(id: string) {
    if (!confirm('Delete this schedule?')) return;
    try {
      await apiDelete(`/schedules/${id}`);
      pushToast('good', 'Schedule deleted.');
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Schedules</h1>
    <div class="flex gap-2">
      <button class="btn btn-primary" onclick={startCreate}>New schedule</button>
      <button class="btn btn-ghost" onclick={load}>Refresh</button>
    </div>
  </div>
  <ErrorBox message={error} />

  {#if editing}
    <div class="card p-4 space-y-3">
      <h2 class="font-medium">{editing.id ? 'Edit' : 'New'} schedule</h2>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Name</span>
          <input bind:value={form.name} placeholder="Nightly cleanup" />
        </label>
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Cron schedule</span>
          <input bind:value={form.schedule} class="font-mono" placeholder="*/5 * * * *" />
        </label>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Type</span>
          <select bind:value={form.type}>
            <option value="command">Command</option>
            <option value="script">Script</option>
          </select>
        </label>
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Timeout (sec)</span>
          <input type="number" min="1" bind:value={form.timeout_sec} class="font-mono" />
        </label>
      </div>

      {#if form.type === 'command'}
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Command</span>
          <textarea bind:value={form.command} rows="3" class="font-mono text-xs" placeholder="apt-get update"></textarea>
        </label>
      {:else}
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Script</span>
          <textarea bind:value={form.script} rows="6" class="font-mono text-xs" placeholder={"#!/bin/bash\n…"}></textarea>
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

      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Target clients</span>
          <select multiple bind:value={form.client_ids} class="h-32 font-mono text-xs">
            {#each clients as c}
              <option value={c.id}>{c.name || c.hostname || c.id}</option>
            {/each}
          </select>
        </label>
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Target groups (comma-separated IDs)</span>
          <input bind:value={form.group_ids} placeholder="linux-servers, db" />
        </label>
      </div>

      <div class="flex flex-wrap gap-4">
        <label class="flex items-center gap-2 text-xs">
          <input type="checkbox" bind:checked={form.execute_concurrently} class="!w-auto" />
          <span class="text-slate-300">Execute on targets concurrently</span>
        </label>
        <label class="flex items-center gap-2 text-xs">
          <input type="checkbox" bind:checked={form.abort_on_error} class="!w-auto" />
          <span class="text-slate-300">Abort remaining targets on error</span>
        </label>
      </div>

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
    {:else if !rows.length}
      <EmptyState title="No schedules" detail="Cron-style scheduled commands and scripts appear here." />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Type</th><th>Cron</th><th>Targets</th><th>Created</th><th>By</th><th class="text-right">Actions</th></tr></thead>
        <tbody>
          {#each rows as s}
            <tr>
              <td>{s.name ?? '—'}</td>
              <td><span class="pill pill-info">{s.type ?? '—'}</span></td>
              <td class="font-mono text-xs">{s.schedule ?? '—'}</td>
              <td class="text-xs">{(s.client_ids ?? []).length} client(s) / {(s.group_ids ?? []).length} group(s)</td>
              <td class="text-xs text-slate-400">{fmtDate(s.created_at)}</td>
              <td class="text-xs">{s.created_by ?? '—'}</td>
              <td class="text-right whitespace-nowrap">
                <button class="btn btn-ghost btn-sm" onclick={() => startEdit(s.id ?? '')}>Edit</button>
                <button class="btn btn-ghost btn-sm text-rose-300" onclick={() => del(s.id ?? '')}>Delete</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
