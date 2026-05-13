<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { login, verify2fa, ApiException } from '$lib/api';
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

  onMount(() => {
    if (get(tokenStore)) goto('/inventory', { replaceState: true });
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
        <div class="text-sm text-slate-300">
          Enter the 6-digit TOTP code for <span class="font-mono text-indigo-300">{username}</span>.
        </div>
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
          }}
        >
          Back
        </button>
      </form>
    {/if}
  </div>
</div>
