<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiDelete } from '$lib/api';
  import type { ApiToken, User } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import { datetimeLocalValue, expiryToRfc3339, tokenFormError } from '$lib/apiTokens';
  import { copyToClipboard } from '$lib/clipboard';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  let tokens: ApiToken[] = $state([]);
  let me = $state<User | null>(null);
  let loading = $state(true);
  // `loadError` is for the list, `error` for the create form, so a failed
  // list is never shown as "no tokens".
  let loadError = $state('');
  let error = $state('');
  let newName = $state('');
  let newScope = $state('read+write');
  let newExpiry = $state('');
  let issued = $state('');
  let submitting = $state(false);
  let minExpiry = $state(datetimeLocalValue(new Date()));

  async function load() {
    loading = true;
    loadError = '';
    try {
      me = await apiGet<User>('/me');
      tokens = (await apiGet<ApiToken[]>('/me/tokens')) ?? [];
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function create(e: Event) {
    e.preventDefault();
    if (submitting) return;
    error = '';
    const why = tokenFormError(newName, newExpiry);
    if (why) {
      error = why;
      return;
    }
    submitting = true;
    issued = '';
    try {
      const payload: Record<string, string> = { name: newName.trim(), scope: newScope };
      const at = expiryToRfc3339(newExpiry);
      if (at) payload.expires_at = at;
      const res = await apiPost<{ token: string; prefix: string }>('/me/tokens', payload);
      issued = res?.token || '';
      pushToast('good', 'Token issued. Copy it now — it won\'t be shown again.');
      newName = '';
      newExpiry = '';
      minExpiry = datetimeLocalValue(new Date());
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
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

  const user = $derived(me?.username ?? '…');
</script>

<div class="p-6 space-y-4 max-w-4xl">
  <h1 class="text-2xl font-semibold tracking-tight">API tokens</h1>
  <p class="text-sm text-slate-400">
    Personal API tokens for <span class="font-mono text-indigo-300">{user}</span>.
    A token is used as the password in HTTP Basic auth for this user, for example
    <code class="text-xs break-words">curl -u {user}:&lt;token&gt; {typeof location === 'undefined' ? '' : location.origin}/api/v1/status</code>.
  </p>

  <div class="card p-4 space-y-3">
    <h2 class="font-medium">New token</h2>
    <form class="grid grid-cols-1 md:grid-cols-[1fr_auto_minmax(14rem,auto)_auto] gap-3 items-end" onsubmit={create}>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Name</span>
        <input bind:value={newName} required maxlength="249" />
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Scope</span>
        <select bind:value={newScope}>
          <option value="read">read</option>
          <option value="read+write">read+write</option>
        </select>
      </label>
      <label class="text-xs">
        <span class="text-slate-400 uppercase">Expires</span>
        <input type="datetime-local" bind:value={newExpiry} min={minExpiry} class="font-mono" />
        <span class="block text-slate-500 mt-1">Blank: one year from now.</span>
      </label>
      <button class="btn btn-primary" type="submit" disabled={submitting}>
        {#if submitting}<Spinner label="Creating…" />{:else}Create{/if}
      </button>
    </form>

    <ErrorBox message={error} />

    {#if issued}
      <div class="border border-amber-500/40 bg-amber-500/10 text-amber-200 p-3 rounded text-xs">
        <div class="flex items-start gap-2">
          <span class="font-mono break-all select-all flex-1">{issued}</span>
          <button type="button" class="btn btn-ghost text-xs shrink-0" onclick={() => copyToClipboard(issued, 'Token copied.')}>Copy</button>
        </div>
        <div class="text-xs text-amber-400/80 mt-1">Save this now — it won't be shown again.</div>
      </div>
    {/if}
  </div>

  <ErrorBox message={loadError} />

  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if loadError}
      <div class="p-4 flex justify-center"><button class="btn btn-ghost" onclick={load}>Retry</button></div>
    {:else if !tokens.length}
      <EmptyState title="No API tokens yet" />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Prefix</th><th>Scope</th><th>Created</th><th>Expires</th><th class="text-right"><span class="sr-only">Actions</span></th></tr></thead>
        <tbody>
          {#each tokens as t}
            <tr>
              <td>{t.name?.trim() || '—'}</td>
              <td class="font-mono">{t.prefix}</td>
              <td><span class="pill pill-info">{t.scope ?? '—'}</span></td>
              <td class="text-xs text-slate-400">{fmtDate(t.created_at)}</td>
              <td class="text-xs text-slate-400">{t.expires_at ? fmtDate(t.expires_at) : 'never'}</td>
              <td><button class="btn btn-danger" onclick={() => revoke(t.prefix)}>Revoke</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
