<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet } from '$lib/api';
  import type { Client } from '$lib/types';
  import { fmtBytes, fmtRelative } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import KV from '$lib/components/KV.svelte';

  let client: Client | null = $state(null);
  let metrics: any = $state(null);
  let updates: any = $state(null);
  let loading = $state(true);
  let error = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    try {
      const [c, m, u] = await Promise.allSettled([
        apiGet<Client>(`/clients/${id}`),
        apiGet<any>(`/clients/${id}/metrics?include_processes=false`).catch(() => null),
        apiGet<any>(`/clients/${id}/updates-status`).catch(() => null)
      ]);
      if (c.status === 'fulfilled') client = c.value;
      if (m.status === 'fulfilled') metrics = m.value;
      if (u.status === 'fulfilled') updates = u.value;
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

{#if loading && !client}
  <div class="p-8 flex justify-center"><Spinner label="Loading client…" /></div>
{:else if client}
  <ErrorBox message={error} />

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
    <div class="card p-4 space-y-2">
      <div class="text-xs uppercase tracking-wider text-slate-500">ProxiPort Client</div>
      <KV k="ID" v={client.id} mono />
      <KV k="Name" v={client.name} />
      <KV k="Hostname" v={client.hostname} mono />
      <KV k="Version" v={client.version} mono />
      <KV k="Last heartbeat" v={fmtRelative(client.last_heartbeat_at)} />
      {#if client.disconnected_at}
        <KV k="Disconnected" v={fmtRelative(client.disconnected_at)} />
      {/if}
    </div>

    <div class="card p-4 space-y-2">
      <div class="text-xs uppercase tracking-wider text-slate-500">Operating System</div>
      <KV k="OS" v={client.os_full_name || client.os} />
      <KV k="Family" v={client.os_family} />
      <KV k="Kernel" v={client.os_kernel} mono />
      <KV k="Arch" v={client.os_arch} mono />
      <KV k="Version" v={client.os_version} mono />
    </div>

    <div class="card p-4 space-y-2">
      <div class="text-xs uppercase tracking-wider text-slate-500">CPU / Memory</div>
      <KV k="CPUs" v={client.num_cpus != null ? String(client.num_cpus) : '—'} />
      <KV k="CPU model" v={client.cpu_model} />
      <KV k="CPU vendor" v={client.cpu_vendor} />
      <KV k="RAM total" v={fmtBytes(client.mem_total)} />
      {#if metrics?.cpu_usage_percent != null}
        <KV k="CPU now" v={`${metrics.cpu_usage_percent.toFixed(1)}%`} />
      {/if}
      {#if metrics?.memory_usage_percent != null}
        <KV k="RAM now" v={`${metrics.memory_usage_percent.toFixed(1)}%`} />
      {/if}
    </div>

    <div class="card p-4 space-y-2 lg:col-span-2">
      <div class="text-xs uppercase tracking-wider text-slate-500">Network</div>
      <KV k="Server-side address" v={client.address} mono />
      <KV
        k="IPv4"
        v={(client.ipv4 ?? []).length ? (client.ipv4 ?? []).join(', ') : '—'}
        mono
      />
      <KV
        k="IPv6"
        v={(client.ipv6 ?? []).length
          ? (client.ipv6 ?? []).slice(0, 3).join(', ') + ((client.ipv6?.length ?? 0) > 3 ? ' …' : '')
          : '—'}
        mono
      />
    </div>

    <div class="card p-4 space-y-2">
      <div class="text-xs uppercase tracking-wider text-slate-500">Updates</div>
      {#if updates?.updates_summary}
        <KV k="Available" v={String(updates.updates_summary.updates_count ?? 0)} />
        <KV k="Security" v={String(updates.updates_summary.security_updates_count ?? 0)} />
      {:else}
        <div class="text-sm text-slate-500">No data yet.</div>
      {/if}
    </div>

    <div class="card p-4 space-y-2 lg:col-span-3">
      <div class="text-xs uppercase tracking-wider text-slate-500">Tags &amp; Labels</div>
      <div class="flex flex-wrap gap-1.5 pt-1">
        {#each client.tags ?? [] as t}
          <span class="pill pill-info">{t}</span>
        {/each}
        {#each Object.entries(client.labels ?? {}) as [k, v]}
          <span class="pill pill-muted">{k}={v}</span>
        {/each}
        {#if !(client.tags?.length || Object.keys(client.labels ?? {}).length)}
          <span class="text-sm text-slate-500">No tags or labels.</span>
        {/if}
      </div>
      <div class="text-xs uppercase tracking-wider text-slate-500 pt-3">Groups</div>
      <div class="flex flex-wrap gap-1.5">
        {#each client.groups ?? [] as g}
          <span class="pill pill-muted">{g}</span>
        {/each}
        {#if !client.groups?.length}<span class="text-sm text-slate-500">None.</span>{/if}
      </div>
    </div>
  </div>
{/if}
