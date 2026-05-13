<script lang="ts">
  import { page } from '$app/stores';
  import { apiPost, apiGet, ApiException } from '$lib/api';
  import type { Job } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let script = $state('#!/bin/bash\nset -e\nuname -a\n');
  let interpreter = $state('/bin/bash');
  let cwd = $state('');
  let isSudo = $state(false);
  let timeout = $state('120');
  let running = $state(false);
  let error = $state('');
  let stream: string[] = $state([]);
  let currentJobId = $state('');

  async function run(e: Event) {
    e.preventDefault();
    if (!script.trim() || running) return;
    running = true;
    error = '';
    stream = [];
    const id = $page.params.id;
    try {
      const payload: any = {
        script: btoa(script),
        timeout_sec: Number(timeout) || 120,
        interpreter,
        is_sudo: isSudo
      };
      if (cwd) payload.cwd = cwd;
      const res = await apiPost<{ jid: string }>(`/clients/${id}/scripts`, payload);
      currentJobId = res.jid;
      pollResult(res.jid);
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
      running = false;
    }
  }

  async function pollResult(jid: string) {
    const id = $page.params.id;
    while (running) {
      await new Promise((r) => setTimeout(r, 800));
      try {
        const job = await apiGet<Job>(`/clients/${id}/commands/${jid}`);
        if (job.status === 'successful' || job.status === 'failed' || job.status === 'cancelled') {
          if (job.result?.stdout) stream = [...stream, job.result.stdout];
          if (job.result?.stderr) stream = [...stream, `[stderr] ${job.result.stderr}`];
          stream = [...stream, `\n--- exit_code=${job.result?.exit_code ?? '?'} status=${job.status} ---`];
          running = false;
          return;
        }
      } catch (err) {
        stream = [...stream, `[poll error: ${err instanceof Error ? err.message : err}]`];
        running = false;
        return;
      }
    }
  }
</script>

<div class="space-y-4">
  <div class="card p-4 space-y-3">
    <h2 class="font-medium">Run script on this client</h2>
    <form class="space-y-3" onsubmit={run}>
      <textarea bind:value={script} rows="10" class="font-mono text-xs" required></textarea>
      <div class="grid grid-cols-1 md:grid-cols-4 gap-3">
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Interpreter</span>
          <input bind:value={interpreter} class="font-mono" />
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Working directory</span>
          <input bind:value={cwd} placeholder="/tmp" class="font-mono" />
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
      <button class="btn btn-primary" type="submit" disabled={running}>
        {#if running}<Spinner label="Running…" />{:else}Run script{/if}
      </button>
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
</div>
