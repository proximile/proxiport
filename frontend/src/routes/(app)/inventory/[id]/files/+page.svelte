<script lang="ts">
  import { onDestroy } from 'svelte';
  import { page } from '$app/stores';
  import { apiPostForm, apiErrorText, openAuthedSocket } from '$lib/api';
  import { pushToast } from '$lib/stores';
  import { resolveRemotePath, waitForOpen, watchUploads } from '$lib/uploads';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  // How long to wait for the client to report the outcome of a push.
  const RESULT_TIMEOUT_MS = 60_000;

  let file: FileList | null = $state(null);
  let dest = $state('');
  let mode = $state('0644');
  let user = $state('');
  let group = $state('');
  let sync = $state(false);
  let force = $state(false);
  let busy = $state(false);
  let waiting = $state(false);
  let error = $state('');

  const target = $derived.by(() => {
    const f = (file as FileList | null)?.[0];
    return f ? resolveRemotePath(dest, f.name) : '';
  });

  const ctrl = new AbortController();
  onDestroy(() => ctrl.abort());

  async function push(e: Event) {
    e.preventDefault();
    const f = file?.[0];
    if (!f) return;
    const id = $page.params.id;
    if (!id) return;
    const path = resolveRemotePath(dest, f.name);
    busy = true;
    error = '';
    // The outcome on the client is only reported over the uploads socket, so
    // it has to be listening before the push starts. Without it the push still
    // goes ahead, but the page can only say the server accepted it.
    let watcher: ReturnType<typeof watchUploads> | null = null;
    try {
      const sock = await openAuthedSocket('/ws/uploads');
      watcher = watchUploads(sock);
      if (!(await waitForOpen(sock))) {
        watcher.close();
        watcher = null;
      }
    } catch (_) {
      watcher = null;
    }
    try {
      const fd = new FormData();
      fd.append('client_id', id);
      fd.append('dest', path);
      if (mode) fd.append('mode', mode);
      if (user) fd.append('user', user);
      if (group) fd.append('group', group);
      if (sync) fd.append('sync', 'true');
      if (force) fd.append('force', 'true');
      fd.append('upload', f);
      const res = await apiPostForm<{ uuid: string }>('/files', fd);
      if (!watcher || !res?.uuid) {
        pushToast('info', `Upload of ${f.name} accepted by the server; its result on the client is not known here. Check the audit log.`);
        return;
      }
      waiting = true;
      const outcome = await watcher.wait(res.uuid, id, RESULT_TIMEOUT_MS, ctrl.signal);
      if (ctrl.signal.aborted) return;
      if (!outcome) {
        pushToast('warn', `Upload of ${f.name} accepted by the server, but the client has not reported a result yet. Check the audit log.`);
      } else if (outcome.status === 'success') {
        pushToast('good', `Pushed ${f.name} to ${outcome.filepath || path}`);
      } else if (outcome.status === 'ignored') {
        pushToast('warn', `Not written: ${outcome.message || 'the client ignored the upload'}`);
      } else {
        error = outcome.message || `The client reported status "${outcome.status}".`;
      }
    } catch (err) {
      error = apiErrorText(err);
    } finally {
      watcher?.close();
      busy = false;
      waiting = false;
    }
  }
</script>

<div class="space-y-4">
  <div class="card p-4 space-y-3 max-w-3xl">
    <h2 class="font-medium">Push file to client</h2>
    <p class="text-sm text-slate-500">
      One-shot upload from your laptop to the agent's filesystem. This is a write-only push form —
      it does not browse the agent's existing files.
    </p>
    <form class="space-y-3" onsubmit={push}>
      <label class="text-xs block">
        <span class="text-slate-400 uppercase">Local file</span>
        <input type="file" bind:files={file} required />
      </label>
      <label class="text-xs block">
        <span class="text-slate-400 uppercase">Remote path</span>
        <input bind:value={dest} class="font-mono" placeholder="/tmp/<file name>" />
        <span class="text-slate-500 block mt-1">
          Folder plus file name, e.g. /tmp/test.txt. A path ending in / gets the local file's name appended.
          {#if target}Will write to <span class="font-mono">{target}</span>.{/if}
        </span>
      </label>
      <div class="grid grid-cols-3 gap-3">
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Mode</span>
          <input bind:value={mode} class="font-mono" placeholder="client default" />
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Owner</span>
          <input bind:value={user} class="font-mono" />
        </label>
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Group</span>
          <input bind:value={group} class="font-mono" />
        </label>
      </div>
      <div class="flex items-center gap-4 text-sm">
        <label class="flex items-center gap-2"><input type="checkbox" bind:checked={sync} class="!w-auto" /> sync (atomic)</label>
        <label class="flex items-center gap-2"><input type="checkbox" bind:checked={force} class="!w-auto" /> force overwrite</label>
      </div>
      <ErrorBox message={error} />
      <button class="btn btn-primary" disabled={busy} type="submit">
        {#if waiting}<Spinner label="Waiting for the client…" />{:else if busy}<Spinner label="Pushing…" />{:else}Push{/if}
      </button>
    </form>
  </div>
</div>
