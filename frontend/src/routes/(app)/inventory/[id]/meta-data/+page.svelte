<script lang="ts">
  import { untrack } from 'svelte';
  import { page } from '$app/stores';
  import { apiGet } from '$lib/api';
  import { getClientContext } from '$lib/clientContext';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import Spinner from '$lib/components/Spinner.svelte';

  type Attributes = { tags?: string[] | null; labels?: Record<string, string> | null };

  // Tags and labels are plain client attributes, read from the agent through
  // GET /clients/:id/attributes; they are not kept in the vault.
  const ctx = getClientContext();
  let attrs: Attributes | null = $state(null);
  let loading = $state(true);
  let error = $state('');

  let seq = 0;
  async function load(id: string) {
    const mine = ++seq;
    loading = true;
    error = '';
    try {
      const res = await apiGet<Attributes>(`/clients/${id}/attributes`);
      if (mine === seq) attrs = res ?? {};
    } catch (err) {
      if (mine !== seq) return;
      attrs = null;
      error = err instanceof Error ? err.message : String(err);
    } finally {
      if (mine === seq) loading = false;
    }
  }

  $effect(() => {
    const id = $page.params.id;
    if (id) untrack(() => load(id));
  });

  // The endpoint only answers for a connected client; for a disconnected one
  // fall back to the values last stored with the client record.
  let offline = $derived(!!ctx?.client && ctx.client.connection_state !== 'connected');
  let shown: Attributes | null = $derived(
    attrs ?? (offline && ctx?.client ? { tags: ctx.client.tags, labels: ctx.client.labels } : null)
  );
  let tags = $derived(shown?.tags ?? []);
  let labels = $derived(Object.entries(shown?.labels ?? {}));
</script>

{#if loading}
  <div class="p-8 flex justify-center"><Spinner /></div>
{:else if error && !shown}
  <ErrorBox message={error} />
{:else}
  {#if error && offline}
    <div class="text-sm text-slate-400">
      The client is disconnected; showing the tags and labels it last reported.
    </div>
  {/if}
  <div class="card p-4 space-y-4">
    {#if !tags.length && !labels.length}
      <EmptyState
        title="No tags or labels set"
        detail={"Set them in the agent's configuration or with PUT /clients/{id}/attributes."}
      />
    {:else}
      <div>
        <div class="text-xs uppercase tracking-wider text-slate-500 mb-2">Tags</div>
        {#if tags.length}
          <div class="flex flex-wrap gap-1.5">
            {#each tags as t}<span class="pill pill-info">{t}</span>{/each}
          </div>
        {:else}
          <div class="text-sm text-slate-500">No tags.</div>
        {/if}
      </div>
      <div>
        <div class="text-xs uppercase tracking-wider text-slate-500 mb-2">Labels</div>
        {#if labels.length}
          <table class="tbl">
            <thead><tr><th>Key</th><th>Value</th></tr></thead>
            <tbody>
              {#each labels as [k, v]}
                <tr>
                  <td class="font-mono">{k}</td>
                  <td class="font-mono text-xs">{v || '—'}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        {:else}
          <div class="text-sm text-slate-500">No labels.</div>
        {/if}
      </div>
    {/if}
  </div>
{/if}
