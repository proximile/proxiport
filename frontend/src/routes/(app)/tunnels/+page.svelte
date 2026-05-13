<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiDelete } from '$lib/api';
  import type { Tunnel, Client } from '$lib/types';
  import { fmtRelative } from '$lib/format';
  import { pushToast } from '$lib/stores';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import Spinner from '$lib/components/Spinner.svelte';

  type Row = Tunnel & { client_name?: string };
  let rows: Row[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let filter = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const t = await apiGet<Tunnel[]>('/tunnels?page[limit]=100');
      const clients = await apiGet<Client[]>('/clients?fields[clients]=id,name&page[limit]=100').catch(() => [] as Client[]);
      const map = new Map((clients ?? []).map((c) => [c.id, c.name || c.id]));
      rows = (t ?? []).map((x) => ({ ...x, client_name: map.get(x.client_id ?? '') }));
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  let filtered = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    if (!f) return rows;
    return rows.filter((r) =>
      [r.id, r.client_id, r.client_name, r.lhost, r.lport, r.rhost, r.rport, r.scheme, r.acl]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
        .includes(f)
    );
  });

  async function deleteTunnel(client_id: string, tid: string) {
    if (!confirm(`Delete tunnel ${tid}?`)) return;
    try {
      await apiDelete(`/clients/${client_id}/tunnels/${tid}`);
      pushToast('good', 'Tunnel deleted.');
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Tunnels</h1>
      <div class="text-sm text-slate-400 mt-1">{rows.length} active tunnel{rows.length === 1 ? '' : 's'}</div>
    </div>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>

  <input bind:value={filter} placeholder="Filter…" class="max-w-md" />
  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading && !rows.length}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !filtered.length}
      <EmptyState title="No active tunnels" detail="Create one from a client's Tunnels tab." />
    {:else}
      <table class="tbl">
        <thead>
          <tr><th>Client</th><th>ID</th><th>Local</th><th>Remote</th><th>Scheme</th><th>ACL</th><th>Idle</th><th>Created</th><th></th></tr>
        </thead>
        <tbody>
          {#each filtered as r}
            <tr>
              <td>
                <a href={`/inventory/${r.client_id}/tunnels`} class="text-indigo-300 hover:text-indigo-200">{r.client_name || r.client_id}</a>
              </td>
              <td class="font-mono">{r.id}</td>
              <td class="font-mono text-emerald-300">{r.lhost ?? ''}:{r.lport}</td>
              <td class="font-mono text-slate-300">{r.rhost ?? ''}:{r.rport}</td>
              <td><span class="pill pill-info">{r.scheme || r.protocol || '—'}</span></td>
              <td class="font-mono text-xs text-slate-400">{r.acl || '—'}</td>
              <td>{r.idle_timeout_minutes ?? '—'}m</td>
              <td class="text-slate-400">{fmtRelative(r.created_at)}</td>
              <td><button class="btn btn-danger" onclick={() => deleteTunnel(r.client_id ?? '', String(r.id))}>Delete</button></td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
