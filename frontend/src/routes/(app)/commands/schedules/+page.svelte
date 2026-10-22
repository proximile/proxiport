<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import { apiGet, apiGetPage, apiPost, apiPut, apiDelete, asList, ApiException } from '$lib/api';
  import type { Schedule, Client, ClientGroup } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import { base64ToUtf8 } from '$lib/encoding';
  import { formFromSchedule, lastRun, scheduleBody, scheduleFormError, type ScheduleForm } from '$lib/schedules';
  import { pushToast } from '$lib/stores';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  const PAGE_SIZE = 100;

  // A schedule created before the script body was base64-encoded holds raw
  // text, which does not decode to valid UTF-8 -- show it unchanged rather
  // than blanking the operator's script or displaying mojibake.
  function decodeScriptBody(stored: string): string {
    if (!stored) return '';
    return base64ToUtf8(stored) ?? stored;
  }

  let rows: Schedule[] = $state([]);
  let total: number | null = $state(null);
  let offset = $state(0);
  let clients: Client[] = $state([]);
  let groups: ClientGroup[] = $state([]);
  let loading = $state(true);
  // `error` is for the list; `formError` for the edit form, so closing the
  // form clears its messages without hiding a list failure, and vice versa.
  let error = $state('');
  let formError = $state('');

  let editing: { id?: string } | null = $state(null);
  // The schedule as stored, so an update can send back what the form does not edit.
  let original: Record<string, any> | null = $state(null);
  let form = $state<ScheduleForm>(blankForm());
  let saving = $state(false);

  function blankForm(): ScheduleForm {
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
      client_ids: [],
      group_ids: [],
      tags: '',
      tags_operator: 'OR',
      execute_concurrently: false,
      abort_on_error: true,
      overlaps: false
    };
  }

  async function loadRows() {
    loading = true;
    error = '';
    try {
      const res = await apiGetPage<Schedule>(`/schedules?page[limit]=${PAGE_SIZE}&page[offset]=${offset}`);
      rows = res.rows;
      total = res.count;
      // Deleting the last row on a later page leaves it empty; step back.
      if (!rows.length && offset > 0) {
        offset = Math.max(0, offset - PAGE_SIZE);
        return loadRows();
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function load() {
    await loadRows();
    // Clients and groups for the target pickers and the Targets column —
    // best-effort; ids are shown when they cannot be resolved.
    const [c, g] = await Promise.allSettled([
      apiGet<unknown>('/clients?fields[clients]=id,name,hostname&page[limit]=500'),
      apiGet<unknown>('/client-groups?fields[client_groups]=id,description&page[limit]=500')
    ]);
    clients = c.status === 'fulfilled' ? asList<Client>(c.value) : [];
    groups = g.status === 'fulfilled' ? asList<ClientGroup>(g.value) : [];
  }

  onMount(async () => {
    await load();
    // Deep links from a client's Schedules tab.
    const q = $page.url.searchParams;
    const editId = q.get('edit');
    const forClient = q.get('client');
    if (editId) await startEdit(editId);
    else if (q.get('new') !== null) {
      startCreate();
      if (forClient) form.client_ids = [forClient];
    }
  });

  function startCreate() {
    editing = {};
    original = null;
    form = blankForm();
    formError = '';
  }

  async function startEdit(id: string) {
    formError = '';
    try {
      const s = (await apiGet<any>(`/schedules/${id}`)) ?? {};
      const data = s.data ?? s; // tolerate a {data:…} envelope
      editing = { id };
      original = data;
      // Stored base64 has to come back as text, or an operator who edits an
      // existing schedule re-encodes the encoded form and destroys it.
      form = formFromSchedule(data, decodeScriptBody);
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
      // The row may have been deleted elsewhere; refresh so it disappears.
      if (err instanceof ApiException && err.status === 404) await loadRows();
    }
  }

  function cancel() {
    editing = null;
    original = null;
    formError = '';
  }

  async function save() {
    const why = scheduleFormError(form);
    if (why) {
      formError = why;
      return;
    }
    saving = true;
    formError = '';
    const body = scheduleBody(editing?.id ? original : null, form);
    try {
      if (editing?.id) await apiPut(`/schedules/${editing.id}`, body);
      else await apiPost('/schedules', body);
      pushToast('good', editing?.id ? 'Schedule updated.' : 'Schedule created.');
      editing = null;
      original = null;
      await loadRows();
    } catch (err) {
      formError = err instanceof Error ? err.message : String(err);
      if (err instanceof ApiException && err.status === 404) await loadRows();
    } finally {
      saving = false;
    }
  }

  async function del(id: string) {
    if (!confirm('Delete this schedule?')) return;
    try {
      await apiDelete(`/schedules/${id}`);
      pushToast('good', 'Schedule deleted.');
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
    // Reload either way: a failure is most often a schedule already removed.
    await loadRows();
  }

  function goPage(next: number) {
    offset = Math.max(0, next);
    loadRows();
  }

  const clientNames = $derived(new Map(clients.map((c) => [c.id, c.name || c.hostname || c.id])));

  function targetsText(s: Schedule): string {
    if (s.tags?.tags?.length) {
      const op = String(s.tags.operator ?? '').toUpperCase() === 'AND' ? ' and ' : ' or ';
      return `tags: ${s.tags.tags.join(op)}`;
    }
    const parts = [
      ...(s.client_ids ?? []).map((id) => clientNames.get(id) ?? id),
      ...(s.group_ids ?? []).map((id) => `group ${id}`)
    ];
    if (!parts.length) return '—';
    return parts.length > 3 ? `${parts.slice(0, 3).join(', ')} +${parts.length - 3}` : parts.join(', ');
  }

  // Every known group, plus any the schedule names that no longer exists, so
  // an edit never silently drops a target.
  const groupChoices = $derived.by(() => {
    const known = groups.map((g) => ({ id: g.id, label: g.description ? `${g.id} — ${g.description}` : g.id }));
    const extra = form.group_ids.filter((id) => !groups.some((g) => g.id === id)).map((id) => ({ id, label: `${id} (unknown)` }));
    return [...known, ...extra];
  });

  // Same for clients that are no longer listed.
  const clientChoices = $derived.by(() => {
    const known = clients.map((c) => ({ id: c.id, label: c.name || c.hostname || c.id }));
    const extra = form.client_ids.filter((id) => !clients.some((c) => c.id === id)).map((id) => ({ id, label: id }));
    return [...known, ...extra];
  });

  const pageEnd = $derived(offset + rows.length);
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
          <input type="number" min="1" step="1" bind:value={form.timeout_sec} class="font-mono" />
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
      {/if}
      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        {#if form.type === 'script'}
          <label class="block text-xs">
            <span class="block text-slate-400 mb-1">Interpreter</span>
            <input bind:value={form.interpreter} class="font-mono" placeholder="/bin/bash" />
          </label>
        {/if}
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Working directory</span>
          <input bind:value={form.cwd} class="font-mono" placeholder="/tmp" />
        </label>
        <label class="flex items-end gap-2 text-xs">
          <input type="checkbox" bind:checked={form.is_sudo} class="!w-auto" />
          <span class="text-slate-300">Run as sudo</span>
        </label>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Target clients</span>
          <select multiple bind:value={form.client_ids} class="h-32 font-mono text-xs">
            {#each clientChoices as c}
              <option value={c.id}>{c.label}</option>
            {/each}
          </select>
        </label>
        <label class="block text-xs">
          <span class="block text-slate-400 mb-1">Target groups</span>
          <select multiple bind:value={form.group_ids} class="h-32 font-mono text-xs">
            {#each groupChoices as g}
              <option value={g.id}>{g.label}</option>
            {/each}
          </select>
          {#if !groupChoices.length}<span class="block text-slate-500 mt-1">No client groups defined.</span>{/if}
        </label>
        <div class="space-y-2">
          <label class="block text-xs">
            <span class="block text-slate-400 mb-1">Or target by tags (comma-separated)</span>
            <input bind:value={form.tags} class="font-mono" placeholder="linux, db" />
          </label>
          <label class="block text-xs">
            <span class="block text-slate-400 mb-1">Tag match</span>
            <select bind:value={form.tags_operator}>
              <option value="OR">Any of the tags</option>
              <option value="AND">All of the tags</option>
            </select>
          </label>
        </div>
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
        <label class="flex items-center gap-2 text-xs">
          <input type="checkbox" bind:checked={form.overlaps} class="!w-auto" />
          <span class="text-slate-300">Allow a run to start while the previous one is still going</span>
        </label>
      </div>

      <ErrorBox message={formError} />
      <div class="flex gap-2">
        <button class="btn btn-primary" onclick={save} disabled={saving}>
          {#if saving}<Spinner label="Saving…" />{:else}Save{/if}
        </button>
        <button class="btn btn-ghost" onclick={cancel} disabled={saving}>Cancel</button>
      </div>
    </div>
  {/if}

  <div class="card overflow-x-auto">
    {#if loading && !rows.length}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if error && !rows.length}
      <!-- The ErrorBox above says why the list is missing. -->
    {:else if !rows.length}
      <EmptyState title="No schedules" detail="Cron-style scheduled commands and scripts appear here." />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Type</th><th>Cron</th><th>Targets</th><th>Last run</th><th>Last status</th><th>Created</th><th>By</th><th class="text-right">Actions</th></tr></thead>
        <tbody>
          {#each rows as s}
            {@const run = lastRun(s.last_execution)}
            <tr>
              <td title={s.id}>{s.name ?? '—'}</td>
              <td><span class="pill pill-info">{s.type ?? '—'}</span></td>
              <td class="font-mono text-xs">{s.schedule ?? '—'}</td>
              <td class="text-xs" title={targetsText(s)}>{targetsText(s)}</td>
              <td class="text-xs text-slate-400 whitespace-nowrap">{run.when ? fmtDate(run.when) : 'never'}</td>
              <td>
                {#if run.when}
                  <span class="pill pill-{run.tone}" title={s.last_execution?.summary ?? ''}>{run.label}</span>
                {:else}<span class="text-slate-500 text-xs">—</span>{/if}
              </td>
              <td class="text-xs text-slate-400">{fmtDate(s.created_at)}</td>
              <td class="text-xs">{s.created_by || '—'}</td>
              <td class="text-right whitespace-nowrap">
                <button class="btn btn-ghost btn-sm" onclick={() => startEdit(s.id ?? '')}>Edit</button>
                <button class="btn btn-ghost btn-sm text-rose-300" onclick={() => del(s.id ?? '')}>Delete</button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
    {#if total !== null && (total > rows.length || offset > 0)}
      <div class="flex items-center justify-between gap-2 px-4 py-2 border-t border-pp-border text-xs text-slate-400">
        <span>Showing {rows.length ? offset + 1 : 0}–{pageEnd} of {total}</span>
        <div class="flex gap-2">
          <button class="btn btn-ghost btn-sm" disabled={loading || offset === 0} onclick={() => goPage(offset - PAGE_SIZE)}>Previous</button>
          <button class="btn btn-ghost btn-sm" disabled={loading || pageEnd >= total} onclick={() => goPage(offset + PAGE_SIZE)}>Next</button>
        </div>
      </div>
    {/if}
  </div>
</div>
