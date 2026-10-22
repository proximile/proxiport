<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet, apiGetPage } from '$lib/api';
  import type { Client, Schedule } from '$lib/types';
  import { fmtDate } from '$lib/format';
  import { lastRun, scheduleReach } from '$lib/schedules';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import Spinner from '$lib/components/Spinner.svelte';

  const PAGE_SIZE = 100;

  let schedules: { s: Schedule; via: string }[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let note = $state('');

  // Every schedule, page by page: a schedule can reach this client through a
  // group or its tags, so the server's client_ids filter alone is not enough.
  async function allSchedules(): Promise<Schedule[]> {
    const out: Schedule[] = [];
    for (let offset = 0; ; offset += PAGE_SIZE) {
      const { rows, count } = await apiGetPage<Schedule>(`/schedules?page[limit]=${PAGE_SIZE}&page[offset]=${offset}`);
      out.push(...rows);
      if (rows.length < PAGE_SIZE || count === null || out.length >= count) return out;
    }
  }

  async function load(id: string) {
    loading = true;
    error = '';
    note = '';
    try {
      const [all, client] = await Promise.all([
        allSchedules(),
        apiGet<Client>(`/clients/${encodeURIComponent(id)}?fields[clients]=id,tags,groups`).catch(() => null)
      ]);
      if (!client) note = 'Could not read this client\'s groups and tags; only schedules that name it directly are listed.';
      const target = { id, groupIds: new Set(client?.groups ?? []), tags: client?.tags ?? [] };
      schedules = all.map((s) => ({ s, via: scheduleReach(s, target) })).filter((r) => r.via);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const id = $page.params.id;
    if (id) load(id);
  });
</script>

<div class="space-y-4">
  <div class="flex items-center justify-end gap-2">
    <a class="btn btn-primary" href={`/commands/schedules?new=1&client=${encodeURIComponent($page.params.id ?? '')}`}>New schedule</a>
    <button class="btn btn-ghost" onclick={() => $page.params.id && load($page.params.id)}>Refresh</button>
  </div>
  <ErrorBox message={error} />
  {#if note}<div class="text-xs text-amber-300">{note}</div>{/if}
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if error}
      <!-- The ErrorBox above says why the list is missing. -->
    {:else if !schedules.length}
      <EmptyState title="No schedules target this client" />
    {:else}
      <table class="tbl">
        <thead><tr><th>Name</th><th>Type</th><th>Schedule</th><th>Targets via</th><th>Last run</th><th>Last status</th><th>Created</th><th>By</th></tr></thead>
        <tbody>
          {#each schedules as { s, via }}
            {@const run = lastRun(s.last_execution)}
            <tr>
              <td><a class="text-indigo-300" href={`/commands/schedules?edit=${encodeURIComponent(s.id ?? '')}`}>{s.name || '—'}</a></td>
              <td><span class="pill pill-info">{s.type ?? '—'}</span></td>
              <td class="font-mono text-xs">{s.schedule ?? '—'}</td>
              <td class="text-xs">{via}</td>
              <td class="text-xs text-slate-400 whitespace-nowrap">{run.when ? fmtDate(run.when) : 'never'}</td>
              <td>
                {#if run.when}
                  <span class="pill pill-{run.tone}" title={s.last_execution?.summary ?? ''}>{run.label}</span>
                {:else}<span class="text-slate-500 text-xs">—</span>{/if}
              </td>
              <td class="text-xs text-slate-400">{fmtDate(s.created_at)}</td>
              <td class="text-xs">{s.created_by || '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
