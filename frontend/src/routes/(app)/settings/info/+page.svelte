<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet } from '$lib/api';
  import type { ServerStatus } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import KV from '$lib/components/KV.svelte';

  let status: ServerStatus | null = $state(null);
  let provider: any = $state(null);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    try {
      const [s, p] = await Promise.allSettled([
        apiGet<ServerStatus>('/status'),
        apiGet<any>('/auth/provider').catch(() => null)
      ]);
      if (s.status === 'fulfilled') status = s.value;
      if (p.status === 'fulfilled') provider = p.value;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);
</script>

<div class="p-6 space-y-4 max-w-3xl">
  <h1 class="text-2xl font-semibold tracking-tight">Server info</h1>
  <ErrorBox message={error} />

  {#if loading && !status}
    <Spinner />
  {:else if status}
    <div class="card p-4 space-y-2">
      <KV k="Server version" v={status.version} mono />
      <KV k="Fingerprint" v={status.fingerprint} mono />
      <KV k="Pairing URL" v={status.pairing_url || '—'} mono />
      <KV k="Connect URLs" v={(status.connect_url ?? []).join(', ') || '—'} mono />
    </div>
    <div class="card p-4 space-y-2">
      <KV k="Clients connected" v={String(status.clients_connected ?? 0)} />
      <KV k="Clients disconnected" v={String(status.clients_disconnected ?? 0)} />
      <KV k="Clients total" v={String((status.clients_connected ?? 0) + (status.clients_disconnected ?? 0))} />
    </div>
    <div class="card p-4 space-y-2">
      <KV k="Auth provider" v={provider?.auth_provider ?? '—'} />
      <KV k="User auth source" v={status.users_auth_source ?? '—'} />
      <KV k="Client auth source" v={[status.clients_auth_source, status.clients_auth_mode].filter(Boolean).join(', ') || '—'} />
      <KV k="2FA enabled" v={status.two_fa_enabled ? 'yes' : 'no'} />
      <KV k="2FA delivery" v={status.two_fa_delivery_method === 'totp_authenticator_app' ? 'TOTP authenticator app' : status.two_fa_delivery_method || '—'} />
    </div>
  {/if}
</div>
