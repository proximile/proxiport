<script lang="ts">
  import { onDestroy } from 'svelte';
  import { page } from '$app/stores';
  import { apiPost, apiGet, apiErrorText } from '$lib/api';
  import type { Job } from '$lib/types';
  import { finishedJobOutput, isJobFinished, parseTimeoutSec, pollDeadlineMs } from '$lib/jobs';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { utf8ToBase64 } from '$lib/encoding';

  let script = $state('#!/bin/bash\nset -e\nuname -a\n');
  let interpreter = $state('/bin/bash');
  let cwd = $state('');
  let isSudo = $state(false);
  let timeout = $state('120');
  let running = $state(false);
  let error = $state('');
  let stream: string[] = $state([]);
  let currentJobId = $state('');

  const hasShebang = $derived(script.trimStart().startsWith('#!'));

  // Polling belongs to this page: leaving it ends the loop.
  let destroyed = false;
  onDestroy(() => {
    destroyed = true;
  });

  async function run(e: Event) {
    e.preventDefault();
    if (running) return;
    if (!script.trim()) {
      error = 'The script body is required.';
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
    const id = $page.params.id;
    try {
      const payload: any = {
        script: utf8ToBase64(script),
        timeout_sec: t.value,
        interpreter,
        is_sudo: isSudo
      };
      if (cwd) payload.cwd = cwd;
      const res = await apiPost<{ jid: string }>(`/clients/${id}/scripts`, payload);
      currentJobId = res.jid;
      pollResult(res.jid, t.value);
    } catch (err) {
      error = apiErrorText(err);
      running = false;
    }
  }

  async function pollResult(jid: string, timeoutSec: number) {
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
            `stopped waiting. The result may still appear in the Commands tab history. ---`
        ];
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
      <textarea bind:value={script} rows="10" class="font-mono text-xs" aria-label="Script" required></textarea>
      <div class="grid grid-cols-1 md:grid-cols-4 gap-3">
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Interpreter</span>
          <input bind:value={interpreter} class="font-mono" />
          <span class="text-slate-500 block mt-1">
            {#if hasShebang}Linux and macOS clients use the script's #! line instead; Windows clients use this.{:else}On Linux and macOS a #! first line takes precedence over this.{/if}
          </span>
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
