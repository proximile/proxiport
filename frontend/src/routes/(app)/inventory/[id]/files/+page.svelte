<script lang="ts">
  import { page } from '$app/stores';
  import { apiPostForm, ApiException } from '$lib/api';
  import { pushToast } from '$lib/stores';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let file: FileList | null = $state(null);
  let dest = $state('/tmp/');
  let mode = $state('0644');
  let user = $state('');
  let group = $state('');
  let sync = $state(false);
  let force = $state(false);
  let busy = $state(false);
  let error = $state('');

  async function push(e: Event) {
    e.preventDefault();
    const f = file?.[0];
    if (!f) return;
    busy = true;
    error = '';
    const id = $page.params.id;
    try {
      const fd = new FormData();
      fd.append('client_id', id);
      fd.append('dest', dest);
      if (mode) fd.append('mode', mode);
      if (user) fd.append('user', user);
      if (group) fd.append('group', group);
      if (sync) fd.append('sync', 'true');
      if (force) fd.append('force', 'true');
      fd.append('upload', f);
      await apiPostForm('/files', fd);
      pushToast('good', `Pushed ${f.name} to ${dest}`);
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
    } finally {
      busy = false;
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
        <input bind:value={dest} class="font-mono" required />
      </label>
      <div class="grid grid-cols-3 gap-3">
        <label class="text-xs">
          <span class="text-slate-400 uppercase">Mode</span>
          <input bind:value={mode} class="font-mono" placeholder="0644" />
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
        {#if busy}<Spinner label="Pushing…" />{:else}Push{/if}
      </button>
    </form>
  </div>
</div>
