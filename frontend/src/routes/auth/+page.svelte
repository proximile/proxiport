<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { login, verify2fa, createTotpSecret, ApiException } from '$lib/api';
  import { tokenStore } from '$lib/stores';
  import { get } from 'svelte/store';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let username = $state('');
  let password = $state('');
  let totp = $state('');
  let stage: 'creds' | '2fa' = $state('creds');
  let busy = $state(false);
  let error = $state('');
  let loginToken = $state(''); // short-lived JWT from /login when TOTP is enabled

  // First-login TOTP enrollment: when the server says no secret exists yet
  // (totp_key_status === 'pending'), the interim token may create one. We
  // show the QR inline so the user can scan it, then verify as usual.
  let enrollQr = $state('');
  let enrollSecret = $state('');
  let enrollBusy = $state(false);

  // Only the demo deployment exposes guest credentials in the UI.
  // Detection is hostname-prefix-based so the banner is invisible on
  // every other install. Matches `demo.*` so future demo subdomains
  // (try.proxiport.net, etc.) need explicit opt-in here.
  let isDemo = $state(false);

  onMount(() => {
    if (get(tokenStore)) goto('/inventory', { replaceState: true });
    if (typeof window !== 'undefined' && window.location.hostname.startsWith('demo.')) {
      isDemo = true;
    }
  });

  async function submitCreds(e: Event) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      const res = await login(username, password);
      if (res.token && !res.two_fa) {
        tokenStore.set(res.token);
        goto('/inventory', { replaceState: true });
      } else {
        loginToken = res.token ?? '';
        stage = '2fa';
        enrollQr = '';
        enrollSecret = '';
        if (res.two_fa?.totp_key_status === 'pending' && loginToken) {
          // No authenticator enrolled yet — create the secret with the
          // interim token and show the QR right on the 2FA step.
          enrollBusy = true;
          try {
            const t = await createTotpSecret(loginToken);
            enrollQr = t.qr ?? '';
            enrollSecret = t.secret ?? '';
          } catch (enrollErr) {
            error = enrollErr instanceof ApiException
              ? enrollErr.errors[0]?.title || enrollErr.message
              : String(enrollErr);
          } finally {
            enrollBusy = false;
          }
        }
      }
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
    } finally {
      busy = false;
    }
  }

  async function submit2fa(e: Event) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      const tok = await verify2fa(username, totp, loginToken);
      if (!tok) throw new Error('Server returned no token');
      tokenStore.set(tok);
      goto('/inventory', { replaceState: true });
    } catch (err) {
      error = err instanceof ApiException ? err.errors[0]?.title || err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center px-4">
  <div class="w-full max-w-sm card p-6 space-y-5">
    <div class="text-center">
      <div class="text-2xl font-semibold tracking-tight text-indigo-300">ProxiPort</div>
      <div class="text-xs text-slate-500 mt-1">remote access · OSS</div>
    </div>

    {#if isDemo}
      <div class="rounded border border-indigo-700/50 bg-indigo-950/40 px-3 py-2 text-xs text-indigo-200">
        <div class="font-semibold mb-1">Public demo</div>
        Sign in with
        <span class="font-mono text-indigo-100">demo</span> /
        <span class="font-mono text-indigo-100">demo</span>.
        State resets on the half-hour; destructive actions are
        disabled.
      </div>
    {/if}

    {#if stage === 'creds'}
      <form class="space-y-3" onsubmit={submitCreds}>
        <label class="block">
          <span class="text-xs uppercase tracking-wider text-slate-400">Username</span>
          <input bind:value={username} autocomplete="username" required autofocus />
        </label>
        <label class="block">
          <span class="text-xs uppercase tracking-wider text-slate-400">Password</span>
          <input type="password" bind:value={password} autocomplete="current-password" required />
        </label>
        <ErrorBox message={error} />
        <button class="btn btn-primary w-full justify-center" disabled={busy} type="submit">
          {#if busy}<Spinner />{:else}Sign in{/if}
        </button>
      </form>
    {:else}
      <form class="space-y-3" onsubmit={submit2fa}>
        {#if enrollBusy}
          <Spinner label="Preparing authenticator enrollment…" />
        {:else if enrollQr}
          <div class="text-sm text-slate-300">
            No authenticator app is enrolled for
            <span class="font-mono text-indigo-300">{username}</span> yet.
            Scan this QR code with your authenticator app, then enter the
            6-digit code it shows.
          </div>
          <img
            src={`data:image/png;base64,${enrollQr}`}
            alt="TOTP enrollment QR code"
            class="mx-auto w-44 h-44 bg-white p-2 rounded"
          />
          <details class="text-xs text-slate-400">
            <summary class="cursor-pointer">Can't scan? Enter the secret manually</summary>
            <code class="font-mono select-all break-all">{enrollSecret}</code>
          </details>
        {:else}
          <div class="text-sm text-slate-300">
            Enter the 6-digit TOTP code for <span class="font-mono text-indigo-300">{username}</span>.
          </div>
        {/if}
        <label class="block">
          <span class="text-xs uppercase tracking-wider text-slate-400">TOTP code</span>
          <input
            bind:value={totp}
            inputmode="numeric"
            pattern="[0-9]*"
            maxlength="6"
            autocomplete="one-time-code"
            required
            autofocus
            class="font-mono text-center tracking-widest text-lg"
          />
        </label>
        <ErrorBox message={error} />
        <button class="btn btn-primary w-full justify-center" disabled={busy} type="submit">
          {#if busy}<Spinner />{:else}Verify{/if}
        </button>
        <button
          type="button"
          class="btn btn-ghost w-full justify-center"
          onclick={() => {
            stage = 'creds';
            totp = '';
            error = '';
            enrollQr = '';
            enrollSecret = '';
          }}
        >
          Back
        </button>
      </form>
    {/if}
  </div>
</div>
