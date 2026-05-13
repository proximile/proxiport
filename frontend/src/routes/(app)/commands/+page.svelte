<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, ApiException } from '$lib/api';
  import type { Client } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let clients: Client[] = $state([]);
  let selected: Set<string> = $state(new Set());
  let cmd = $state('');
  let cwd = $state('');
  let interpreter = $state('');
  let isSudo = $state(false);
  let timeout = $state('60');
  let concurrent = $state(true);
  let abortOnError = $state(false);
  let running = $state(false);
  let error = $state('');
  // per-client output buffers, keyed by client_id
  let buffers: Record<string, string[]> = $state({});
  let statuses: Record<string, string> = $state({});
  let multiJobId = $state('');

  onMount(async () => {
    try {
      const c = await apiGet<Client[]>('/clients?fields[clients]=id,name,hostname,connection_state&page[limit]=100');
      clients = (c ?? []).filter((x) => x.connection_state === 'connected');
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  });

  function toggle(id: string) {
    if (selected.has(id)) selected.delete(id);
    else selected.add(id);
    selected = new Set(selected);
  }

  async function run(e: Event) {
    e.preventDefault();
    if (!cmd.trim() || selected.size === 0 || running) return;
    running = true;
    error = '';
    buffers = {};
    statuses = {};
    multiJobId = '';
    selected.forEach((id) => (statuses[id] = 'pending'));

    try {
      const payload: any = {
        client_ids: [...selected],
        command: cmd,
        timeout_sec: Number(timeout) || 60,
        is_sudo: isSudo,
        execute_concurrently: concurrent,
        abort_on_error: abortOnError
      };
      if (cwd) payload.cwd = cwd;
      if (interpreter) payload.interpreter = interpreter;
      const res = await apiPost<{ jid: string }>(`/commands`, payload);
      multiJobId = res.jid;
      pollMulti(res.jid);
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
      running = false;
    }
  }

  async function pollMulti(jid: string) {
    while (running) {
      await new Promise((r) => setTimeout(r, 1000));
      try {
        const summary = await apiGet<any>(`/commands/${jid}`);
        const jobs = summary?.jobs ?? [];
        for (const j of jobs) {
          if (j.client_id) {
            statuses[j.client_id] = j.status;
            if (j.result?.stdout) buffers[j.client_id] = [j.result.stdout];
            if (j.result?.stderr) buffers[j.client_id] = [...(buffers[j.client_id] ?? []), `[stderr] ${j.result.stderr}`];
          }
        }
        statuses = { ...statuses };
        buffers = { ...buffers };
        if ((jobs.every((j: any) => ['successful', 'failed', 'cancelled'].includes(j.status)))) {
          running = false;
          return;
        }
      } catch (err) {
        running = false;
        return;
      }
    }
  }

  function nameFor(id: string): string {
    const c = clients.find((x) => x.id === id);
    return c?.name || c?.hostname || id;
  }
</script>

<div class="p-6 space-y-4">
  <h1 class="text-2xl font-semibold tracking-tight">Commands</h1>
  <p class="text-sm text-slate-400">Run a command across one or more connected clients.</p>

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
    <div class="card p-4 lg:col-span-1">
      <div class="text-xs uppercase tracking-wider text-slate-500 mb-2">Targets</div>
      <div class="text-xs text-slate-400 mb-2">{selected.size} of {clients.length} selected</div>
      <ul class="max-h-96 overflow-auto space-y-1 -mx-2">
        {#each clients as c}
          <li>
            <label class="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-pp-surface-2 cursor-pointer">
              <input type="checkbox" checked={selected.has(c.id)} onchange={() => toggle(c.id)} class="!w-auto" />
              <span class="text-sm flex-1 truncate">{c.name || c.id}</span>
              <span class="text-xs text-slate-500 font-mono truncate">{c.hostname || ''}</span>
            </label>
          </li>
        {/each}
        {#if !clients.length}
          <li class="text-sm text-slate-500 px-2 py-3">No connected clients.</li>
        {/if}
      </ul>
    </div>

    <div class="card p-4 lg:col-span-2">
      <form class="space-y-3" onsubmit={run}>
        <textarea bind:value={cmd} rows="4" class="font-mono" placeholder="uname -a" required></textarea>
        <div class="grid grid-cols-2 md:grid-cols-4 gap-3">
          <label class="text-xs">
            <span class="text-slate-400 uppercase">Interpreter</span>
            <select bind:value={interpreter}>
              <option value="">default</option>
              <option value="cmd">cmd</option>
              <option value="powershell">powershell</option>
              <option value="pwsh">pwsh</option>
              <option value="tacoscript">tacoscript</option>
            </select>
          </label>
          <label class="text-xs">
            <span class="text-slate-400 uppercase">Working dir</span>
            <input bind:value={cwd} class="font-mono" />
          </label>
          <label class="text-xs">
            <span class="text-slate-400 uppercase">Timeout</span>
            <input bind:value={timeout} class="font-mono" inputmode="numeric" />
          </label>
          <label class="flex items-end gap-2 text-xs">
            <input type="checkbox" bind:checked={isSudo} class="!w-auto" /> sudo
          </label>
        </div>
        <div class="flex flex-wrap gap-4 text-sm">
          <label class="flex items-center gap-2"><input type="checkbox" bind:checked={concurrent} class="!w-auto" /> Execute concurrently</label>
          <label class="flex items-center gap-2"><input type="checkbox" bind:checked={abortOnError} class="!w-auto" /> Abort on error</label>
        </div>
        <ErrorBox message={error} />
        <button class="btn btn-primary" type="submit" disabled={running || selected.size === 0}>
          {#if running}<Spinner label={`Running on ${selected.size}…`} />{:else}Run on {selected.size} client{selected.size === 1 ? '' : 's'}{/if}
        </button>
      </form>
    </div>
  </div>

  {#if Object.keys(buffers).length}
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3">
      {#each [...selected] as id}
        <div class="card overflow-hidden">
          <div class="px-3 py-2 border-b border-pp-border text-xs flex justify-between">
            <span class="font-medium text-slate-200 truncate">{nameFor(id)}</span>
            <span class="pill" class:pill-good={statuses[id] === 'successful'} class:pill-bad={statuses[id] === 'failed'} class:pill-warn={statuses[id] === 'running'} class:pill-muted={!statuses[id]}>{statuses[id] ?? '—'}</span>
          </div>
          <pre class="p-3 font-mono text-xs text-emerald-200 whitespace-pre-wrap break-all overflow-auto max-h-64">{(buffers[id] ?? []).join('')}</pre>
        </div>
      {/each}
    </div>
  {/if}
</div>
