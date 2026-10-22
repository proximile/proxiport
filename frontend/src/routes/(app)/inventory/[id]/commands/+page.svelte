<script lang="ts">
  import { onDestroy } from 'svelte';
  import { page } from '$app/stores';
  import { apiPost, apiGet, apiErrorText } from '$lib/api';
  import type { Job } from '$lib/types';
  import { fmtRelative } from '$lib/format';
  import { finishedJobOutput, isJobFinished, jobErrorText, jobStatusLabel, parseTimeoutSec, pollDeadlineMs } from '$lib/jobs';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

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

  // Polling belongs to this page: leaving it ends every loop and refresh.
  let destroyed = false;
  let historyTimer: ReturnType<typeof setTimeout> | undefined;
  onDestroy(() => {
    destroyed = true;
    clearTimeout(historyTimer);
  });

  async function loadHistory() {
    clearTimeout(historyTimer);
    try {
      const id = $page.params.id;
      // Newest first by start time, so a job that is still running (and has
      // no finished_at yet) is at the top rather than past the page limit.
      const jobs = await apiGet<Job[]>(`/clients/${id}/commands?fields[jobs]=jid,status,finished_at,started_at,created_by,command,error,timeout_sec&page[limit]=50&sort=-started_at`).catch(() => []);
      if (destroyed) return;
      history = jobs ?? [];
      // Keep a running row current until it finishes.
      if (history.some((j) => !isJobFinished(j.status))) {
        historyTimer = setTimeout(() => {
          if (!destroyed) loadHistory();
        }, 5000);
      }
    } catch (_) {
      // ignore — no commands history yet
    }
  }

  $effect(() => {
    if ($page.params.id) loadHistory();
  });

  async function run(e: Event) {
    e.preventDefault();
    if (running) return;
    if (!cmd.trim()) {
      error = 'Command cannot be empty.';
      return;
    }
    const t = parseTimeoutSec(timeout);
    if (t.value === undefined) {
      error = t.error ?? '';
      return;
    }
    running = true;
    error = '';
    stream = [];
    currentJobId = '';
    const id = $page.params.id;
    try {
      const payload: any = {
        command: cmd,
        timeout_sec: t.value,
        is_sudo: isSudo
      };
      if (cwd) payload.cwd = cwd;
      if (interpreter) payload.interpreter = interpreter;
      const res = await apiPost<{ jid: string }>(`/clients/${id}/commands`, payload);
      currentJobId = res.jid;
      loadHistory();
      pollResult(res.jid, t.value);
    } catch (err) {
      error = apiErrorText(err);
      running = false;
    }
  }

  async function pollResult(jid: string, timeoutSec: number) {
    // Poll the per-client command-result endpoint. Single-client jobs live
    // under /clients/{id}/commands/{jid}; the global /commands/{jid} endpoint
    // is for multi-client orchestrator jobs only.
    const id = $page.params.id;
    const deadline = Date.now() + pollDeadlineMs(timeoutSec);
    while (running && !destroyed) {
      await new Promise((r) => setTimeout(r, 800));
      if (destroyed) return;
      try {
        const job = await apiGet<Job>(`/clients/${id}/commands/${jid}`);
        if (destroyed) return;
        if (isJobFinished(job.status)) {
          stream = [...stream, ...finishedJobOutput(job)];
          running = false;
          loadHistory();
          return;
        }
      } catch (err) {
        stream = [...stream, `[poll error: ${err instanceof Error ? err.message : err}]`];
        running = false;
        return;
      }
      if (Date.now() > deadline) {
        stream = [
          ...stream,
          `\n--- no final status from the client within ${timeoutSec}s plus a grace period; ` +
            `stopped waiting. Recent commands below shows the job once it finishes. ---`
        ];
        running = false;
        loadHistory();
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
          </select>
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Working directory</span>
          <input bind:value={cwd} placeholder="/" class="font-mono" />
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Timeout (sec)</span>
          <input bind:value={timeout} inputmode="numeric" class="font-mono" />
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
              <td class="text-xs truncate max-w-[14rem]" class:text-red-300={!!j.error} class:text-amber-300={!j.error && j.status === 'unknown'} title={jobErrorText(j)}>{jobErrorText(j) || '—'}</td>
              <td>
                <span class="pill" class:pill-good={j.status === 'successful'} class:pill-bad={j.status === 'failed'} class:pill-warn={j.status === 'unknown'} class:pill-info={j.status === 'running'} class:pill-muted={!j.status} title={j.status ?? ''}>{jobStatusLabel(j.status)}</span>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
