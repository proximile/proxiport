<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { apiGet, apiPost, apiDelete, ApiException } from '$lib/api';
  import type { ClientAuthEntry } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import {
    appendFlags,
    egressFlags,
    fetchProxy,
    installerFlags,
    proxyError,
    proxyIsTor,
    withFetchProxy
  } from '$lib/pairingFlags';
  import { copyToClipboard } from '$lib/clipboard';
  import { modal, focusInitial } from '$lib/dialog';

  let rows: ClientAuthEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  // Set only when the list itself failed to load, so the table does not
  // claim there are no credentials when it simply could not fetch them.
  let loadError = $state('');
  let addButton: HTMLButtonElement | undefined = $state();
  let addDialog: HTMLDialogElement | undefined = $state();

  // server-status-derived state
  let authMode = $state<string>('');
  let authSource = $state<string>('');
  let pairingUrl = $state<string>('');
  let connectUrl = $state<string>('');
  let fingerprint = $state<string>('');
  let canWrite = $derived(authMode !== 'Read Only');

  // add-credential modal state machine: 'form' → 'pairing' → (close), or
  // 'form' → 'created' → (close) when no pairing service is configured.
  let modalOpen = $state(false);
  let modalStage = $state<'form' | 'pairing' | 'created'>('form');
  let newId = $state('');
  let newPassword = $state('');
  let modalError = $state('');
  let submitting = $state(false);
  let showPwd = $state(false);

  // pairing result
  let pairingLoading = $state(false);
  let pairingError = $state('');
  let pairingCode = $state('');
  let pairingExpires = $state('');
  let pairingLinux = $state('');
  let pairingWindows = $state('');
  let copied = $state<'linux' | 'windows' | 'config' | null>(null);
  let now = $state(Date.now());

  // installer-flag toggles (match `retrieve/templates/linux/install.sh`
  // getopts in proxiport-pairing).
  let optX = $state(true);  // -x: enable remote command/script execution
  let optS = $state(false); // -s: sudo rights for those commands
  let optR = $state(false); // -r: enable file reception
  let optB = $state(false); // -b: sudo rights for file reception

  // Egress toggles, Linux installer only (the PowerShell installer has no
  // equivalent yet). They become the agent's `transports` chain, in order: the
  // proxy first, Tor second, the open internet last and only when asked for.
  let optTor = $state(false);    // -o
  let optProxy = $state('');     // -e <url>, tried before Tor (a VPN sidecar's proxy)
  let optDirect = $state(false); // -e direct
  let proxyErr = $derived(proxyError(optProxy));
  let egressOn = $derived(optTor || (optProxy.trim() !== '' && !proxyErr));

  // delete confirmation
  let pendingDelete = $state<string | null>(null);
  let deletingId = $state<string | null>(null);

  // Same charset + length rule the upstream rport UI imposes.
  const PWD_CHARSET = /^[A-Za-z0-9_+\-.:]+$/;
  const PWD_MIN = 12;
  const ID_CHARSET = /^[A-Za-z0-9_\-.]+$/;
  // The server refuses shorter IDs. The charset is ASCII, so characters and
  // bytes count the same.
  const ID_MIN = 3;

  function validateId(s: string): string {
    if (!s) return 'ID is required.';
    if (!ID_CHARSET.test(s)) return 'ID may contain letters, digits, _ - .';
    if (s.length < ID_MIN) return `ID must be at least ${ID_MIN} characters.`;
    return '';
  }
  function validatePassword(s: string): string {
    if (s.length < PWD_MIN) return `Password must be at least ${PWD_MIN} characters (currently ${s.length}).`;
    if (!PWD_CHARSET.test(s)) return 'Password may contain letters, digits, and _ + - . :';
    return '';
  }

  function randomPassword(len = 24): string {
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_+-.:';
    const buf = new Uint32Array(len);
    crypto.getRandomValues(buf);
    let out = '';
    for (let i = 0; i < len; i++) out += chars[buf[i] % chars.length];
    return out;
  }

  // An error's title plus the server's detail, e.g. "Invalid or missing ID.
  // Min size is 3."
  function errorText(err: unknown): string {
    if (err instanceof ApiException) {
      const e = err.errors[0];
      const text = [e?.title, e?.detail].filter(Boolean).join(' ');
      if (text) return text;
    }
    return err instanceof Error ? err.message : String(err);
  }

  async function loadRows() {
    loading = true;
    loadError = '';
    try {
      rows = (await apiGet<ClientAuthEntry[]>('/clients-auth?page[limit]=100')) ?? [];
    } catch (err) {
      loadError = errorText(err);
    } finally {
      loading = false;
    }
  }

  async function loadStatus() {
    try {
      const status = await apiGet<{
        clients_auth_mode?: string;
        clients_auth_source?: string;
        pairing_url?: string;
        connect_url?: string[] | string;
        fingerprint?: string;
        fingerprint_sha256?: string;
      }>('/status');
      authMode = status?.clients_auth_mode ?? '';
      authSource = status?.clients_auth_source ?? '';
      pairingUrl = status?.pairing_url ?? '';
      const cu = status?.connect_url;
      connectUrl = Array.isArray(cu) ? (cu[0] ?? '') : (cu ?? '');
      // Pin the SHA-256 fingerprint; the MD5 one is only for older servers.
      fingerprint = status?.fingerprint_sha256 || status?.fingerprint || '';
    } catch {
      // best-effort; UI still works without it
    }
  }

  onMount(async () => {
    await loadStatus();
    await loadRows();
  });

  function openAddModal() {
    newId = '';
    newPassword = '';
    modalError = '';
    showPwd = false;
    modalStage = 'form';
    pairingCode = '';
    pairingLinux = '';
    pairingWindows = '';
    pairingError = '';
    copied = null;
    modalOpen = true;
  }
  function closeAddModal() {
    modalOpen = false;
  }
  // Escape closes the form, but not a stage that shows the one-time password:
  // that one closes only through Done.
  function onAddCancel(e: Event) {
    e.preventDefault();
    if (modalStage === 'form' && !submitting) closeAddModal();
  }
  // Some browsers close the dialog on a repeated Escape even though cancel was
  // prevented; reopen it rather than lose a password that is not yet copied.
  function onAddClose() {
    if (modalOpen && addDialog && (modalStage !== 'form' || submitting)) {
      addDialog.showModal();
      focusInitial(addDialog);
    } else {
      modalOpen = false;
    }
  }

  async function submitAdd(e: Event) {
    e.preventDefault();
    modalError = '';
    const idErr = validateId(newId);
    if (idErr) { modalError = idErr; return; }
    const pwdErr = validatePassword(newPassword);
    if (pwdErr) { modalError = pwdErr; return; }
    submitting = true;
    try {
      await apiPost('/clients-auth', { id: newId, password: newPassword });
      await loadRows();
      if (pairingUrl) {
        modalStage = 'pairing';
        // fire-and-await; pairingLoading drives the spinner
        await depositPairing();
      } else {
        // No pairing service configured: show the agent config once instead.
        modalStage = 'created';
      }
    } catch (err) {
      modalError = errorText(err);
    } finally {
      submitting = false;
    }
  }

  async function depositPairing() {
    pairingLoading = true;
    pairingError = '';
    try {
      // The pairing service lives on its own origin, so the server brokers the
      // deposit for us: we POST it same-origin (with our session) and proxiportd
      // forwards it server-to-server. No cross-origin browser call is involved.
      const j = await apiPost<{
        pairing_code?: string;
        expires?: string;
        installers?: { linux?: string; windows?: string };
      }>('/pairing', {
        connect_url: connectUrl,
        fingerprint,
        client_id: newId,
        password: newPassword
      });
      pairingCode = j.pairing_code ?? '';
      pairingExpires = j.expires ?? '';
      pairingLinux = j.installers?.linux ?? '';
      pairingWindows = j.installers?.windows ?? '';
    } catch (err) {
      pairingError = err instanceof Error ? err.message : String(err);
    } finally {
      pairingLoading = false;
    }
  }

  async function copyText(text: string, which: 'linux' | 'windows' | 'config') {
    const label = which === 'config' ? 'Agent config copied.' : 'Installer command copied.';
    if (await copyToClipboard(text, label)) {
      copied = which;
      setTimeout(() => { if (copied === which) copied = null; }, 1500);
    }
  }

  function askDelete(id: string) {
    pendingDelete = id;
  }
  function cancelDelete() {
    pendingDelete = null;
  }
  async function confirmDelete() {
    if (!pendingDelete) return;
    const id = pendingDelete;
    error = '';
    deletingId = id;
    pendingDelete = null;
    try {
      const path = `/clients-auth/${encodeURIComponent(id)}`;
      try {
        await apiDelete(path);
      } catch (err) {
        // The server refuses to delete a credential while any client is still
        // bound to it (active or disconnected), e.g. a stale disconnected agent.
        // The user already confirmed the delete in this dialog, so remove those
        // bound clients along with the credential by retrying with force.
        if (
          err instanceof ApiException &&
          err.errors.some((e) => e.code === 'ERR_CODE_CLIENT_AUTH_HAS_CLIENT')
        ) {
          await apiDelete(`${path}?force=true`);
        } else {
          throw err;
        }
      }
      await loadRows();
      // The reload re-renders the add button; put focus back on it rather
      // than leaving it on the removed row.
      await tick();
      addButton?.focus();
    } catch (err) {
      error = errorText(err);
    } finally {
      deletingId = null;
    }
  }

  let liveIdErr = $derived(newId ? validateId(newId) : '');
  let livePwdErr = $derived(newPassword ? validatePassword(newPassword) : '');

  // A sub-option is cleared when the option it extends goes off, so a
  // greyed-out box never stays ticked.
  $effect(() => {
    if (!optX) optS = false;
    if (!optR) optB = false;
    if (!egressOn) optDirect = false;
  });

  // Compose the installer-flag string from the checkboxes. The pairing
  // service returns the un-flagged base form; flag selection is a
  // client-side concern ($lib/pairingFlags does the splicing).
  let installerOpts = $derived({ x: optX, s: optS, r: optR, b: optB });
  let egress = $derived({ tor: optTor, proxy: optProxy, direct: optDirect });
  let torDup = $derived(proxyIsTor(egress));
  let linuxFlags = $derived([installerFlags(installerOpts), egressFlags(egress)].filter(Boolean).join(' '));

  let linuxCommand = $derived(withFetchProxy(appendFlags(pairingLinux, linuxFlags), fetchProxy(egress)));
  let windowsCommand = $derived(appendFlags(pairingWindows, installerFlags(installerOpts, 'windows')));

  let agentConfig = $derived(
    `[client]\nserver = "${connectUrl}"\nauth = "${newId}:${newPassword}"\nfingerprint = "${fingerprint}"`
  );

  // Tick the expiry countdown while a pairing code is on screen.
  $effect(() => {
    if (!modalOpen || modalStage !== 'pairing' || !pairingExpires) return;
    now = Date.now();
    const t = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(t);
  });

  // Move focus into each new stage; the button that led there is gone.
  $effect(() => {
    modalStage;
    pairingLoading;
    if (addDialog?.open && !addDialog.contains(document.activeElement)) focusInitial(addDialog);
  });

  function fmtExpires(s: string, nowMs: number): string {
    if (!s) return '';
    const t = new Date(s).getTime();
    if (Number.isNaN(t)) return '';
    const secs = Math.max(0, Math.round((t - nowMs) / 1000));
    if (secs === 0) return 'expired';
    return `expires in ${Math.floor(secs / 60)}m ${secs % 60}s`;
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex items-start justify-between gap-4">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Client access credentials</h1>
      <p class="text-sm text-slate-400 mt-1">
        Agent-side <code class="text-xs">auth=user:password</code> credentials. The server stores only a
        salted <code class="text-xs">bcrypt</code> hash of each secret — the password is shown once, when you
        create it, and cannot be retrieved afterward.
      </p>
    </div>
    {#if !loading && canWrite}
      <button class="btn btn-primary whitespace-nowrap" bind:this={addButton} onclick={openAddModal}>
        + Add credential
      </button>
    {/if}
  </div>

  <ErrorBox message={loadError || error} />

  {#if !loading && !canWrite}
    <div class="card p-4 text-sm text-slate-300 border border-amber-900/40 bg-amber-950/20">
      <div class="font-medium text-amber-200 mb-1">Server is in read-only client-auth mode.</div>
      <div class="text-slate-400">
        Source: <code class="text-xs">{authSource || 'unknown'}</code>. To add or remove credentials via this UI,
        switch the server's <code class="text-xs">[server]</code> section to <code class="text-xs">auth_file = "&lt;path&gt;"</code>
        or <code class="text-xs">auth_table = "&lt;name&gt;"</code> and restart <code class="text-xs">proxiportd</code>.
        See <a class="underline hover:text-indigo-300" href="https://docs.proxiport.net/operator-runbook/#rotating-credentials">the operator runbook</a> for details.
      </div>
    </div>
  {/if}

  {#if loading || !loadError}
  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else}
      <table class="tbl">
        <thead>
          <tr>
            <th>ID</th>
            <th>Password</th>
            {#if canWrite}<th class="w-16 text-right"></th>{/if}
          </tr>
        </thead>
        <tbody>
          {#if rows.length === 0}
            <tr>
              <td colspan={canWrite ? 3 : 2} class="p-6 text-center text-slate-500 italic">
                No client-auth credentials registered yet.
              </td>
            </tr>
          {:else}
            {#each rows as r}
              <tr>
                <td class="font-mono">{r.id}</td>
                <td>
                  <span class="font-mono text-xs text-slate-500"
                        title="Stored as a salted bcrypt hash; not retrievable">
                    ••••••••••
                    <span class="ml-1 text-[10px] uppercase tracking-wide text-slate-600">hashed</span>
                  </span>
                </td>
                {#if canWrite}
                  <td class="text-right">
                    <button class="text-slate-500 hover:text-red-400 cursor-pointer text-sm px-2 py-1 min-h-6 min-w-6"
                            disabled={deletingId === r.id}
                            onclick={() => askDelete(r.id)}
                            aria-label={deletingId === r.id ? `Deleting credential ${r.id}` : `Delete credential ${r.id}`}
                            title={`Delete credential ${r.id}`}>
                      <span aria-hidden="true">{deletingId === r.id ? '…' : '×'}</span>
                    </button>
                  </td>
                {/if}
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    {/if}
  </div>
  {/if}

  {#if !loading && !loadError && rows.length > 0}
    <p class="text-xs text-slate-500">
      The installer one-liner is generated when a credential is created — the secret can't be read back later.
      <!-- Each branch carries the whole sentence. Svelte trims the whitespace at a
           block boundary, so a leading space inside {#if} is dropped and the two
           words run together ("credentialwith") in the rendered page. -->
      {#if canWrite}
        To pair a new agent, add a fresh credential with <span class="text-slate-400">+ Add credential</span>.
      {:else}
        To pair a new agent, add a fresh credential.
      {/if}
    </p>
  {/if}
</div>

{#snippet configBlock()}
  <div class="flex items-center justify-between mt-2 mb-1">
    <div class="text-xs uppercase tracking-wide text-slate-400">Agent config</div>
    <button class="btn btn-ghost text-xs" data-autofocus onclick={() => copyText(agentConfig, 'config')}>
      {copied === 'config' ? 'Copied ✓' : 'Copy'}
    </button>
  </div>
  <pre class="font-mono text-xs bg-slate-900 p-2 rounded whitespace-pre-wrap break-all">{agentConfig}</pre>
  <p class="text-xs text-amber-300 mt-1">
    This is the only time the password is shown. Copy it now; it cannot be retrieved after you close this dialog.
  </p>
{/snippet}

<!-- Add-credential modal. A native modal <dialog> keeps focus inside and makes
     the page behind inert; the dialog itself scrolls when it is taller than
     the window. -->
{#if modalOpen}
  <dialog class="m-auto w-[calc(100%-2rem)] max-w-lg max-h-[calc(100dvh-2rem)] overflow-y-auto bg-transparent p-0 text-inherit backdrop:bg-slate-950/70"
          aria-labelledby="add-cred-title"
          bind:this={addDialog}
          use:modal={() => addButton}
          oncancel={onAddCancel}
          onclose={onAddClose}
          onclick={(e) => { if (e.target === e.currentTarget && modalStage === 'form' && !submitting) closeAddModal(); }}>
    <div class="card p-6 space-y-4">
      {#if modalStage === 'form'}
        <h2 id="add-cred-title" class="text-lg font-semibold">Add client-auth credential</h2>
        <p class="text-xs text-slate-400">
          Used by an agent's <code>auth = "&lt;id&gt;:&lt;password&gt;"</code> line to register with this server.
          Case-sensitive.
        </p>
        <form class="space-y-3" onsubmit={submitAdd}>
          <div>
            <label class="block text-xs uppercase tracking-wide text-slate-400 mb-1" for="cred-id">ID</label>
            <input id="cred-id" class="w-full font-mono" type="text" autocomplete="off" data-autofocus
                   bind:value={newId} placeholder="my-new-agent" required />
            {#if liveIdErr}
              <p class="text-xs text-red-400 mt-1">{liveIdErr}</p>
            {/if}
          </div>
          <div>
            <label class="block text-xs uppercase tracking-wide text-slate-400 mb-1" for="cred-pwd">Password</label>
            <div class="flex gap-2">
              <input id="cred-pwd" class="w-full font-mono"
                     type={showPwd ? 'text' : 'password'} autocomplete="new-password"
                     bind:value={newPassword} required />
              <button type="button" class="btn btn-ghost text-xs whitespace-nowrap"
                      onclick={() => (showPwd = !showPwd)}>
                {showPwd ? 'Hide' : 'Show'}
              </button>
              <button type="button" class="btn btn-ghost text-xs whitespace-nowrap"
                      onclick={() => { newPassword = randomPassword(); showPwd = true; }}>
                Generate
              </button>
            </div>
            <div class="flex justify-between mt-1">
              <p class="text-xs text-slate-500">
                ≥ {PWD_MIN} chars, alphanumeric + <code>_ + - . :</code>
              </p>
              <p class="text-xs text-slate-500 font-mono">{newPassword.length}</p>
            </div>
            {#if livePwdErr}
              <p class="text-xs text-red-400 mt-1">{livePwdErr}</p>
            {/if}
          </div>
          {#if modalError}
            <ErrorBox message={modalError} />
          {/if}
          <div class="flex justify-end gap-2 pt-2">
            <button type="button" class="btn btn-ghost" onclick={closeAddModal} disabled={submitting}>Cancel</button>
            <button type="submit" class="btn btn-primary"
                    disabled={submitting || !!liveIdErr || !!livePwdErr || !newId || !newPassword}>
              {submitting ? 'Adding…' : 'Add credential'}
            </button>
          </div>
        </form>
      {:else if modalStage === 'pairing'}
        <h2 id="add-cred-title" class="text-lg font-semibold">
          Pair an agent for <code class="font-mono">{newId}</code>
        </h2>
        <p class="text-xs text-slate-400">
          Run one of these on the machine you want to add as a client. The pairing service
          (<code class="text-xs">{pairingUrl}</code>) bakes the connect URL, fingerprint, and credentials
          into the installer.
        </p>

        {#if pairingLoading}
          <div class="py-8 flex flex-col items-center gap-2 text-slate-400">
            <Spinner />
            <div class="text-xs">contacting pairing service…</div>
          </div>
        {:else if pairingError}
          <ErrorBox message={pairingError} />
          <div class="text-xs text-slate-500">
            The credential <code class="font-mono">{newId}</code> was created and is usable. Only the
            installer one-liner failed; you can configure the agent manually with:
            {@render configBlock()}
          </div>
        {:else if pairingCode}
          <div class="text-xs text-slate-500 mb-2">
            Pairing code <code class="font-mono text-slate-300">{pairingCode}</code> · {fmtExpires(pairingExpires, now)}
          </div>

          <fieldset class="space-y-2 text-sm">
            <legend class="sr-only">Installer options</legend>
            <label class="grid grid-cols-[auto_1fr] gap-2 items-start cursor-pointer">
              <input type="checkbox" bind:checked={optX} class="mt-0.5" />
              <span>Enable remote script and command execution
                <code class="text-xs text-slate-500">(-x)</code></span>
            </label>
            <label class="grid grid-cols-[auto_1fr] gap-2 items-start cursor-pointer">
              <input type="checkbox" bind:checked={optS} disabled={!optX} class="mt-0.5" />
              <span class:opacity-50={!optX}>
                Give remote scripts and commands sudo rights <span class="text-xs text-slate-500">(Linux only)</span>
                <code class="text-xs text-slate-500">(-s)</code>
              </span>
            </label>
            <label class="grid grid-cols-[auto_1fr] gap-2 items-start cursor-pointer">
              <input type="checkbox" bind:checked={optR} class="mt-0.5" />
              <span>Enable file transfer
                <code class="text-xs text-slate-500">(-r)</code></span>
            </label>
            <label class="grid grid-cols-[auto_1fr] gap-2 items-start cursor-pointer">
              <input type="checkbox" bind:checked={optB} disabled={!optR} class="mt-0.5" />
              <span class:opacity-50={!optR}>
                Enable file transfer sudo rights <span class="text-xs text-slate-500">(Linux only)</span>
                <code class="text-xs text-slate-500">(-b)</code>
              </span>
            </label>
          </fieldset>

          <fieldset class="space-y-2 text-sm card p-3">
            <legend class="sr-only">Egress options</legend>
            <div class="text-xs uppercase tracking-wide text-slate-400">
              Egress <span class="normal-case tracking-normal">(Linux only)</span>
            </div>
            <label class="grid grid-cols-[auto_1fr] gap-2 items-start cursor-pointer">
              <input type="checkbox" bind:checked={optTor} class="mt-0.5" />
              <span>Connect through Tor on the agent host
                <span class="text-xs text-slate-500">(needs a tor daemon listening on 127.0.0.1:9050 there)</span>
                <code class="text-xs text-slate-500">(-o)</code></span>
            </label>
            <div>
              <label class="block text-xs text-slate-400 mb-1" for="egress-proxy">
                Proxy to try before Tor, such as a VPN sidecar's
                <code class="text-xs text-slate-500">(-e)</code>
              </label>
              <input id="egress-proxy" class="w-full font-mono text-xs" type="text" autocomplete="off"
                     spellcheck="false" placeholder="socks5h://gluetun:1080" bind:value={optProxy} />
              {#if proxyErr}
                <p class="text-xs text-red-400 mt-1">{proxyErr}</p>
              {:else if torDup}
                <p class="text-xs text-amber-300 mt-1">
                  This is the Tor daemon that -o already adds, so it is listed once. Leave the field empty, or
                  enter a different proxy to try before Tor.
                </p>
              {/if}
            </div>
            <label class="grid grid-cols-[auto_1fr] gap-2 items-start cursor-pointer">
              <input type="checkbox" bind:checked={optDirect} disabled={!egressOn} class="mt-0.5" />
              <span class:opacity-50={!egressOn}>Fall back to the open internet if every proxy fails
                <span class="text-xs text-slate-500">(the agent logs each such connection as an anonymity downgrade)</span>
                <code class="text-xs text-slate-500">(-e direct)</code></span>
            </label>
            {#if egressOn}
              <p class="text-xs text-slate-500">
                The installer writes this chain as <code>transports</code>, pins the server fingerprint, turns off
                package-manager update checks, and fetches its downloads through the same path.
                {#if optDirect}
                  With the fallback, a connection over the open internet is logged as an error, not hidden.
                {:else}
                  Without the fallback a Linux agent stays disconnected rather than use the open internet.
                {/if}
              </p>
            {/if}
          </fieldset>

          <div>
            <div class="flex items-center justify-between mb-1">
              <div class="text-xs uppercase tracking-wide text-slate-400">Linux</div>
              <button class="btn btn-ghost text-xs" onclick={() => copyText(linuxCommand, 'linux')}>
                {copied === 'linux' ? 'Copied ✓' : 'Copy'}
              </button>
            </div>
            <pre class="font-mono text-xs bg-slate-900 p-3 rounded whitespace-pre-wrap break-all">{linuxCommand}</pre>
          </div>

          <div>
            <div class="flex items-center justify-between mb-1">
              <div class="text-xs uppercase tracking-wide text-slate-400">Windows (PowerShell)</div>
              <button class="btn btn-ghost text-xs" onclick={() => copyText(windowsCommand, 'windows')}>
                {copied === 'windows' ? 'Copied ✓' : 'Copy'}
              </button>
            </div>
            {#if egressOn}
              <p class="text-xs text-amber-300 mb-1">
                The Windows installer has no Tor or proxy support yet: an agent installed with this command,
                and its download, use the open internet.
              </p>
            {/if}
            <pre class="font-mono text-xs bg-slate-900 p-3 rounded whitespace-pre-wrap break-all">{windowsCommand}</pre>
          </div>
        {/if}

        <div class="flex justify-end gap-2 pt-2">
          <button class="btn btn-primary" data-autofocus onclick={closeAddModal}>Done</button>
        </div>
      {:else if modalStage === 'created'}
        <h2 id="add-cred-title" class="text-lg font-semibold">
          Credential <code class="font-mono">{newId}</code> created
        </h2>
        <div class="text-xs text-slate-500">
          No pairing service is configured on this server (<code class="text-xs">server.pairing_url</code>), so
          there is no installer one-liner. Configure the agent with:
          {@render configBlock()}
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button class="btn btn-primary" onclick={closeAddModal}>Done</button>
        </div>
      {/if}
    </div>
  </dialog>
{/if}

<!-- Delete confirmation -->
{#if pendingDelete}
  <dialog class="m-auto w-[calc(100%-2rem)] max-w-sm max-h-[calc(100dvh-2rem)] overflow-y-auto bg-transparent p-0 text-inherit backdrop:bg-slate-950/70"
          aria-labelledby="del-cred-title"
          use:modal={() => addButton}
          oncancel={(e) => { e.preventDefault(); cancelDelete(); }}
          onclose={cancelDelete}
          onclick={(e) => { if (e.target === e.currentTarget) cancelDelete(); }}>
    <div class="card p-6 space-y-4">
      <h2 id="del-cred-title" class="text-lg font-semibold">Delete credential</h2>
      <p class="text-sm text-slate-400">
        Remove <code class="font-mono text-slate-200">{pendingDelete}</code>?
        Any clients still bound to it — including disconnected ones — are removed
        too, and an agent using this credential will be unable to reconnect.
      </p>
      <div class="flex justify-end gap-2 pt-2">
        <button class="btn btn-ghost" data-autofocus onclick={cancelDelete}>Cancel</button>
        <button class="btn btn-danger" onclick={confirmDelete}>Delete</button>
      </div>
    </div>
  </dialog>
{/if}
