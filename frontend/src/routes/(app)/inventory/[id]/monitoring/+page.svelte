<script lang="ts">
  import { page } from '$app/stores';
  import { apiGet } from '$lib/api';
  import { fmtPercent, fmtBytes, fmtDate } from '$lib/format';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  type GMPoint = { timestamp: string; cpu_usage_percent: { avg: number; min: number; max: number } | number; memory_usage_percent: { avg: number; min: number; max: number } | number; io_usage_percent?: { avg: number } | number };
  type Process = { pid?: number; ppid?: number; name?: string; state?: string; cmdline?: string; rss?: number; memory_usage_percent?: number };

  let metrics: GMPoint[] = $state([]);
  let processes: Process[] = $state([]);
  let processTimestamp = $state('');
  let selectedTimestamp: string | null = $state(null);
  // Server enforces a [2h, 48h] range for graph-metrics, so 6h is the
  // smallest sensible bucket. 1h would always 400.
  let period: '6h' | '12h' | '24h' = $state('6h');
  let loading = $state(true);
  let error = $state('');

  function periodSeconds(p: typeof period): number {
    return p === '6h' ? 21600 : p === '12h' ? 43200 : 86400;
  }

  async function load(id: string) {
    loading = true;
    error = '';
    try {
      const since = new Date(Date.now() - periodSeconds(period) * 1000).toISOString();
      const until = new Date().toISOString();
      const res = await apiGet<GMPoint[]>(
        `/clients/${id}/graph-metrics?filter[timestamp][since]=${encodeURIComponent(since)}&filter[timestamp][until]=${encodeURIComponent(until)}&sort=timestamp`
      );
      metrics = res ?? [];
      // initial process snapshot — most recent
      await loadProcesses(id, null);
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  async function loadProcesses(id: string, ts: string | null) {
    try {
      let url = `/clients/${id}/processes?page[limit]=1`;
      if (ts) {
        const t = new Date(ts).getTime() / 1000;
        url += `&filter[timestamp][lt]=${Math.floor(t + 60)}&filter[timestamp][gt]=${Math.floor(t - 60)}`;
      }
      const arr = await apiGet<{ timestamp: string; processes: string }[]>(url);
      const first = arr?.[0];
      if (first) {
        processTimestamp = first.timestamp;
        try {
          processes = JSON.parse(first.processes);
        } catch {
          processes = [];
        }
      } else {
        processes = [];
        processTimestamp = '';
      }
    } catch (err) {
      processes = [];
    }
  }

  $effect(() => {
    const id = $page.params.id;
    if (id) load(id);
  });

  // chart math
  let chart = $derived.by(() => {
    if (!metrics.length) return null;
    const n = metrics.length;
    const W = 800;
    const H = 200;
    const padL = 36;
    const padB = 22;
    const padT = 8;
    const padR = 8;
    const t0 = new Date(metrics[0].timestamp).getTime();
    const t1 = new Date(metrics[n - 1].timestamp).getTime();
    const dx = (W - padL - padR) / Math.max(1, t1 - t0);
    function num(v: any): number {
      if (typeof v === 'number') return v;
      if (v && typeof v === 'object') return v.avg ?? v.min ?? 0;
      return 0;
    }
    const ymax = 100;
    const yScale = (val: number) => H - padB - (val / ymax) * (H - padT - padB);
    const xScale = (ts: string) => padL + (new Date(ts).getTime() - t0) * dx;
    const cpuPath = metrics.map((p, i) => `${i === 0 ? 'M' : 'L'}${xScale(p.timestamp).toFixed(1)},${yScale(num(p.cpu_usage_percent)).toFixed(1)}`).join(' ');
    const memPath = metrics.map((p, i) => `${i === 0 ? 'M' : 'L'}${xScale(p.timestamp).toFixed(1)},${yScale(num(p.memory_usage_percent)).toFixed(1)}`).join(' ');
    return { W, H, padL, padB, padT, padR, cpuPath, memPath, ymax, points: metrics, xScale, yScale };
  });

  function selectPoint(p: GMPoint) {
    selectedTimestamp = p.timestamp;
    loadProcesses($page.params.id, p.timestamp);
  }
</script>

<div class="space-y-4">
  <div class="flex gap-2">
    {#each ['6h', '12h', '24h'] as const as p}
      <button
        class="btn"
        class:btn-primary={period === p}
        class:btn-ghost={period !== p}
        onclick={() => {
          period = p;
          load($page.params.id);
        }}
      >{p}</button>
    {/each}
    <button class="btn btn-ghost ml-auto" onclick={() => load($page.params.id)}>Refresh</button>
  </div>

  <ErrorBox message={error} />

  <div class="card p-4">
    <div class="text-xs uppercase tracking-wider text-slate-500 mb-2 flex justify-between">
      <span>CPU &amp; Memory utilisation (%)</span>
      <span class="text-slate-400">{metrics.length} samples</span>
    </div>
    {#if loading && !metrics.length}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !chart}
      <EmptyState title="No monitoring data" detail="Monitoring may not be enabled on this client, or the period is empty." />
    {:else}
      <svg viewBox={`0 0 ${chart.W} ${chart.H}`} class="w-full max-h-72" preserveAspectRatio="none">
        <!-- y-axis grid -->
        {#each [0, 25, 50, 75, 100] as y}
          <line x1={chart.padL} y1={chart.yScale(y)} x2={chart.W - chart.padR} y2={chart.yScale(y)} stroke="#243056" stroke-width="0.5" />
          <text x={chart.padL - 4} y={chart.yScale(y) + 3} fill="#475569" font-size="9" text-anchor="end">{y}</text>
        {/each}
        <!-- CPU -->
        <path d={chart.cpuPath} fill="none" stroke="#6366f1" stroke-width="1.5" />
        <!-- Memory -->
        <path d={chart.memPath} fill="none" stroke="#10b981" stroke-width="1.5" />
        <!-- selectable points -->
        {#each chart.points as p}
          <circle
            cx={chart.xScale(p.timestamp)}
            cy={chart.H - chart.padB}
            r="4"
            fill="transparent"
            class="cursor-pointer"
            role="button"
            tabindex="0"
            onclick={() => selectPoint(p)}
            onkeydown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') selectPoint(p);
            }}
          />
        {/each}
        {#if selectedTimestamp}
          <line x1={chart.xScale(selectedTimestamp)} y1={chart.padT} x2={chart.xScale(selectedTimestamp)} y2={chart.H - chart.padB} stroke="#fbbf24" stroke-width="0.8" stroke-dasharray="2,2" />
        {/if}
      </svg>
      <div class="flex gap-4 text-xs text-slate-400 mt-1 px-2">
        <span><span class="inline-block w-3 h-3 align-middle rounded-sm" style="background: #6366f1"></span> CPU%</span>
        <span><span class="inline-block w-3 h-3 align-middle rounded-sm" style="background: #10b981"></span> Memory%</span>
      </div>
    {/if}
  </div>

  <div class="card overflow-x-auto">
    <div class="px-4 py-2 border-b border-pp-border text-sm text-slate-400 flex flex-col gap-1 sm:flex-row sm:justify-between sm:items-center">
      <span>Processes at {processTimestamp ? fmtDate(processTimestamp) : '—'}</span>
      <span class="text-xs">click a point on the chart above to time-travel</span>
    </div>
    {#if !processes.length}
      <div class="p-6 text-sm text-slate-500">No process snapshot available.</div>
    {:else}
      <table class="tbl">
        <thead><tr><th>PID</th><th>Name</th><th>PPID</th><th>State</th><th>RSS</th><th>Mem%</th><th>Command line</th></tr></thead>
        <tbody>
          {#each processes.slice(0, 200) as p}
            <tr>
              <td class="font-mono">{p.pid ?? '—'}</td>
              <td>{p.name ?? '—'}</td>
              <td class="font-mono">{p.ppid ?? '—'}</td>
              <td>{p.state ?? '—'}</td>
              <td>{fmtBytes(p.rss)}</td>
              <td>{fmtPercent(p.memory_usage_percent)}</td>
              <td class="font-mono text-xs truncate max-w-[40rem]">{p.cmdline ?? '—'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
