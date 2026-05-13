<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiDelete } from '$lib/api';
  import type { ApiToken, User } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  let tokens: ApiToken[] = $state([]);
  let me: User | null = $state(null);
  let loading = $state(true);
  let error = $state('');
  let newName = $state('');
  let newScope = $state('read+write');
  let newExpiry = $state('');
  let issued = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      me = await apiGet<User>('/me');
      tokens = (await apiGet<ApiToken[]>('/me/tokens')) ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function create(e: Event) {
    e.preventDefault();
    error = '';
    issued = '';
    try {
      const payload: any = { name: newName, scope: newScope };
      if (newExpiry) payload.expires_at = newExpiry;
      const res = await apiPost<{ token: string; prefix: string }>('/me/tokens', payload);
      issued = res?.token || '';
      pushToast('good', 'Token issued. Copy it now — it won\'t be shown again.');
      newName = '';
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    }
  }

  async function revoke(prefix: string) {
    if (!confirm(`Revoke token ${prefix}?`)) return;
    try {
      await apiDelete(`/me/tokens/${prefix}`);
      pushToast('good', 'Token revoked.');
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4 max-w-4xl">
  <h1 class="text-2xl font-semibold tracking-tight">API tokens</h1>
  <p class="text-sm text-slate-400">
    Personal API tokens for <span class="font-mono text-indigo-300">{me?.username ?? '…'}</span>.
    Use them in <code class="text-xs">Authorization: Bearer &lt;token&gt;</code> headers.
  </p>

  <div class="card p-4 space-y-3">
    <h2 class="font-medium">New token</h2>
    <form class="grid grid-cols-1 md:grid-cols-4 gap-3 items-end" onsubmit={create}>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Name</span>
        <input bind:value={newName} required />
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Scope</span>
        <select bind:value={newScope}>
          <option value="read">read</option>
          <option value="read+write">read+write</option>
        </select>
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Expires (RFC3339)</span>
        <input bind:value={newExpiry} placeholder="leave blank for none" class="font-mono" />
      </label>
      <button class="btn btn-primary" type="submit">Create</button>
    </form>

    {#if issued}
      <div class="border border-amber-500/40 bg-amber-500/10 text-amber-200 p-3 rounded font-mono text-xs break-all">
        {issued}
        <div class="text-xs text-amber-400/80 mt-1 font-sans">Save this now — it won't be shown again.</div>
      </div>
    {/if}
  </div>

  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !tokens.length}
      <EmptyState title="No API tokens yet" />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Prefix</th><th>Scope</th><th>Created</th><th>Last used</th><th>Expires</th><th></th></tr></thead>
        <tbody>
          {#each tokens as t}
            <tr>
              <td>{t.name ?? '—'}</td>
              <td class="font-mono">{t.prefix}</td>
              <td><span class="pill pill-info">{t.scope ?? '—'}</span></td>
              <td class="text-xs text-slate-400">{fmtDate(t.created_at)}</td>
              <td class="text-xs text-slate-400">{fmtDate(t.last_used_at)}</td>
              <td class="text-xs text-slate-400">{t.expires_at ? fmtDate(t.expires_at) : 'never'}</td>
              <td><button class="btn btn-danger" onclick={() => revoke(t.prefix)}>Revoke</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
