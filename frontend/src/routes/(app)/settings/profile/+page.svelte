<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiDelete, ApiException } from '$lib/api';
  import type { User } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import KV from '$lib/components/KV.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  let me: User | null = $state(null);
  let loading = $state(true);
  let error = $state('');

  // ---- TOTP enrollment ------------------------------------------------
  // GET/POST/DELETE /me/totp-secret. The endpoints 400 when the server
  // config has totp_enabled = false, 404 when no secret exists yet.
  type TotpSecret = { secret?: string; qr?: string };
  let totpStatus: 'loading' | 'disabled' | 'none' | 'enrolled' = $state('loading');
  let totp: TotpSecret | null = $state(null);
  let showQr = $state(false);
  let totpBusy = $state(false);
  let totpError = $state('');

  async function loadTotp() {
    totpError = '';
    try {
      totp = await apiGet<TotpSecret>('/me/totp-secret');
      totpStatus = 'enrolled';
    } catch (err) {
      if (err instanceof ApiException && err.status === 404) totpStatus = 'none';
      else if (err instanceof ApiException && err.status === 400) totpStatus = 'disabled';
      else {
        totpStatus = 'none';
        totpError = err instanceof Error ? err.message : String(err);
      }
    }
  }

  async function enrollTotp() {
    if (totpBusy) return;
    totpBusy = true;
    totpError = '';
    try {
      totp = await apiPost<TotpSecret>('/me/totp-secret');
      totpStatus = 'enrolled';
      showQr = true;
      pushToast('good', 'TOTP secret created — scan the QR code with your authenticator app now.');
    } catch (err) {
      if (err instanceof ApiException && err.status === 409) {
        await loadTotp();
        totpError = 'A TOTP secret already exists for this account.';
      } else {
        totpError = err instanceof Error ? err.message : String(err);
      }
    } finally {
      totpBusy = false;
    }
  }

  async function removeTotp() {
    if (totpBusy) return;
    if (!confirm('Remove the TOTP secret? Your authenticator codes will stop working and logins will no longer ask for them.')) return;
    totpBusy = true;
    totpError = '';
    try {
      await apiDelete('/me/totp-secret');
      totp = null;
      showQr = false;
      totpStatus = 'none';
      pushToast('good', 'TOTP secret removed.');
    } catch (err) {
      totpError = err instanceof Error ? err.message : String(err);
    } finally {
      totpBusy = false;
    }
  }

  onMount(async () => {
    try {
      me = await apiGet<User>('/me');
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
    loadTotp();
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

    <div class="card p-4 space-y-3">
      <h2 class="font-medium">Two-factor authentication (TOTP)</h2>
      <ErrorBox message={totpError} />
      {#if totpStatus === 'loading'}
        <Spinner />
      {:else if totpStatus === 'disabled'}
        <p class="text-sm text-slate-400">
          TOTP is disabled in the server configuration. Set
          <code class="font-mono text-xs">totp_enabled = true</code> in the
          <code class="font-mono text-xs">[api]</code> section of the server
          config to allow authenticator-app logins.
        </p>
      {:else if totpStatus === 'none'}
        <p class="text-sm text-slate-400">
          No authenticator app is enrolled for this account. After enrolling,
          every login will ask for a 6-digit code from your app.
        </p>
        <button class="btn btn-primary" onclick={enrollTotp} disabled={totpBusy}>
          {#if totpBusy}<Spinner label="Generating…" />{:else}Enroll authenticator app{/if}
        </button>
      {:else}
        <p class="text-sm text-slate-400">
          An authenticator app is enrolled. Logins ask for a 6-digit code.
        </p>
        {#if showQr && totp?.qr}
          <div class="space-y-2">
            <img src={`data:image/png;base64,${totp.qr}`} alt="TOTP enrollment QR code" class="w-48 h-48 bg-white p-2 rounded" />
            <div class="text-xs text-slate-400">
              Scan with your authenticator app, or enter the secret manually:
              <code class="font-mono select-all break-all">{totp.secret}</code>
            </div>
          </div>
        {/if}
        <div class="flex gap-2">
          {#if !showQr}
            <button class="btn btn-ghost" onclick={() => (showQr = true)} disabled={!totp?.qr}>Show QR code</button>
          {:else}
            <button class="btn btn-ghost" onclick={() => (showQr = false)}>Hide QR code</button>
          {/if}
          <button class="btn btn-danger" onclick={removeTotp} disabled={totpBusy}>Remove TOTP</button>
        </div>
      {/if}
    </div>

    <div class="text-sm text-slate-400">
      Theme, history size, and terminal-extension settings ship in a later milestone — for now this server uses ProxiPort defaults.
    </div>
  {/if}
</div>
