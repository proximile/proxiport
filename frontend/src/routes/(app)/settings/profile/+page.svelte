<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { User } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import KV from '$lib/components/KV.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let me: User | null = $state(null);
  let loading = $state(true);
  let error = $state('');

  onMount(async () => {
    try {
      me = await apiGet<User>('/me');
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  });
</script>

<div class="p-6 space-y-4 max-w-2xl">
  <h1 class="text-2xl font-semibold tracking-tight">Profile</h1>
  <ErrorBox message={error} />
  {#if loading}
    <Spinner />
  {:else if me}
    <div class="card p-4 space-y-2">
      <KV k="Username" v={me.username} mono />
      <KV k="Groups" v={(me.groups ?? []).join(', ') || '—'} />
      <KV k="2FA recipient" v={me.two_fa_send_to ?? '—'} />
    </div>
    <div class="text-sm text-slate-400">
      Theme, history size, and terminal-extension settings ship in a later milestone — for now this server uses ProxiPort defaults.
    </div>
  {/if}
</div>
