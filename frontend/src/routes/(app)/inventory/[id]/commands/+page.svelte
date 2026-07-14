<script lang="ts">
  import { page } from '$app/stores';
  import { apiPost, apiGet, ApiException } from '$lib/api';
  import type { Job } from '$lib/types';
  import { fmtRelative } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  let cmd = $state('');
  let cwd = $state('');
  let interpreter = $state('');
  let isSudo = $state(false);
  let timeout = $state('60');
  let history: Job[] = $state([]);
  let running = $state(false);
  let error = $state('');
  let stream: string[] = $state([]);
  let currentJobId = $state('');

  async function loadHistory() {
    try {
      const id = $page.params.id;
      const jobs = await apiGet<Job[]>(`/clients/${id}/commands?fields[jobs]=jid,status,finished_at,started_at,created_by,command,error&page[limit]=50&sort=-finished_at`).catch(() => []);
      history = jobs ?? [];
    } catch (_) {
      // ignore — no commands history yet
    }
  }

  $effect(() => {
    if ($page.params.id) loadHistory();
  });

  async function run(e: Event) {
    e.preventDefault();
    if (!cmd.trim() || running) return;
    running = true;
    error = '';
    stream = [];
    currentJobId = '';
    const id = $page.params.id;
    try {
      const payload: any = {
        command: cmd,
        timeout_sec: Number(timeout) || 60,
        is_sudo: isSudo
      };
      if (cwd) payload.cwd = cwd;
      if (interpreter) payload.interpreter = interpreter;
      const res = await apiPost<{ jid: string }>(`/clients/${id}/commands`, payload);
      currentJobId = res.jid;
      pollResult(res.jid);
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
      running = false;
    }
  }

  async function pollResult(jid: string) {
    // Fallback if the streaming WS isn't available — poll the per-client
    // command-result endpoint. Single-client jobs live under
    // /clients/{id}/commands/{jid}; the global /commands/{jid} endpoint is
    // for multi-client orchestrator jobs only.
    const id = $page.params.id;
    while (running) {
      await new Promise((r) => setTimeout(r, 800));
      try {
        const job = await apiGet<Job>(`/clients/${id}/commands/${jid}`);
        if (job.status === 'successful' || job.status === 'failed' || job.status === 'cancelled') {
          if (job.result?.stdout) stream = [...stream, job.result.stdout];
          if (job.result?.stderr) stream = [...stream, `[stderr] ${job.result.stderr}`];
          if (job.error) stream = [...stream, `[error] ${job.error}`];
          stream = [...stream, `\n--- status=${job.status} ---`];
          running = false;
          loadHistory();
          return;
        }
      } catch (err) {
        stream = [...stream, `[poll error: ${err instanceof Error ? err.message : err}]`];
        running = false;
        return;
      }
    }
  }

  function clearOutput() {
    stream = [];
    currentJobId = '';
    error = '';
  }
</script>

<div class="space-y-4">
  <div class="card p-4 space-y-3">
    <h2 class="font-medium">Run command on this client</h2>
    <form class="space-y-3" onsubmit={run}>
      <textarea
        bind:value={cmd}
        rows="3"
        placeholder="uname -a"
        class="font-mono"
        required
      ></textarea>
      <div class="grid grid-cols-1 md:grid-cols-4 gap-3">
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Interpreter</span>
          <select bind:value={interpreter}>
            <option value="">default</option>
            <option value="cmd">cmd</option>
            <option value="powershell">powershell</option>
            <option value="pwsh">pwsh (cross-plat)</option>
          </select>
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Working directory</span>
          <input bind:value={cwd} placeholder="/" class="font-mono" />
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Timeout (sec)</span>
          <input bind:value={timeout} inputmode="numeric" pattern="[0-9]*" class="font-mono" />
        </label>
        <label class="flex items-end gap-2 text-xs">
          <input type="checkbox" bind:checked={isSudo} class="!w-auto" />
          <span class="text-slate-300">Run as sudo</span>
        </label>
      </div>
      <ErrorBox message={error} />
      <div class="flex items-center gap-2">
        <button class="btn btn-primary" type="submit" disabled={running}>
          {#if running}<Spinner label="Running…" />{:else}Run{/if}
        </button>
        <button type="button" class="btn btn-ghost" onclick={clearOutput} disabled={running}>Clear</button>
      </div>
    </form>
  </div>

  {#if stream.length || running}
    <div class="card overflow-hidden">
      <div class="px-4 py-2 border-b border-pp-border text-sm text-slate-400 flex justify-between">
        <span>Output</span>
        <span class="font-mono text-xs">{currentJobId}</span>
      </div>
      <pre class="p-4 font-mono text-xs text-emerald-200 whitespace-pre-wrap break-all overflow-auto max-h-[60vh]">{stream.join('')}</pre>
    </div>
  {/if}

  <div class="card overflow-x-auto">
    <div class="px-4 py-2 border-b border-pp-border text-sm text-slate-400">Recent commands</div>
    {#if !history.length}
      <div class="p-4 text-sm text-slate-500">No commands run yet.</div>
    {:else}
      <table class="tbl">
        <thead><tr><th>When</th><th>By</th><th>Command</th><th>Error</th><th>Status</th></tr></thead>
        <tbody>
          {#each history as j}
            <tr>
              <td class="text-xs text-slate-400" title={j.finished_at ?? j.started_at ?? ''}>{fmtRelative(j.finished_at ?? j.started_at)}</td>
              <td class="text-xs">{j.created_by ?? '—'}</td>
              <td class="font-mono text-xs truncate max-w-[24rem]">{j.command ?? '—'}</td>
              <td class="text-xs text-red-300 truncate max-w-[14rem]" title={j.error ?? ''}>{j.error || '—'}</td>
              <td>
                <span class="pill" class:pill-good={j.status === 'successful'} class:pill-bad={j.status === 'failed'} class:pill-muted={!j.status}>{j.status ?? '—'}</span>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
