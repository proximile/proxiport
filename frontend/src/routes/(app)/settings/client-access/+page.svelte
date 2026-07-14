<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiDelete } from '$lib/api';
  import type { ClientAuthEntry } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';

  let rows: ClientAuthEntry[] = $state([]);
  let loading = $state(true);
  let error = $state('');

  // server-status-derived state
  let authMode = $state<string>('');
  let authSource = $state<string>('');
  let pairingUrl = $state<string>('');
  let connectUrl = $state<string>('');
  let fingerprint = $state<string>('');
  let canWrite = $derived(authMode !== 'Read Only');

  // add-credential modal state machine: 'form' → 'pairing' → (close)
  let modalOpen = $state(false);
  let modalStage = $state<'form' | 'pairing'>('form');
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
  let copied = $state<'linux' | 'windows' | null>(null);

  // installer-flag toggles (match `retrieve/templates/linux/install.sh`
  // getopts in proxiport-pairing).
  let optX = $state(true);  // -x: enable remote command/script execution
  let optS = $state(false); // -s: sudo rights for those commands
  let optR = $state(false); // -r: enable file reception
  let optB = $state(false); // -b: sudo rights for file reception

  // delete confirmation
  let pendingDelete = $state<string | null>(null);
  let deletingId = $state<string | null>(null);

  // Same charset + length rule the upstream rport UI imposes.
  const PWD_CHARSET = /^[A-Za-z0-9_+\-.:]+$/;
  const PWD_MIN = 12;
  const ID_CHARSET = /^[A-Za-z0-9_\-.]+$/;

  function validateId(s: string): string {
    if (!s) return 'ID is required.';
    if (!ID_CHARSET.test(s)) return 'ID may contain letters, digits, _ - .';
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

  async function loadRows() {
    loading = true;
    error = '';
    try {
      rows = (await apiGet<ClientAuthEntry[]>('/clients-auth?page[limit]=100')) ?? [];
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
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
      }>('/status');
      authMode = status?.clients_auth_mode ?? '';
      authSource = status?.clients_auth_source ?? '';
      pairingUrl = status?.pairing_url ?? '';
      const cu = status?.connect_url;
      connectUrl = Array.isArray(cu) ? (cu[0] ?? '') : (cu ?? '');
      fingerprint = status?.fingerprint ?? '';
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
        // No pairing service configured; close the modal.
        modalOpen = false;
      }
    } catch (err) {
      modalError = err instanceof Error ? err.message : String(err);
    } finally {
      submitting = false;
    }
  }

  async function depositPairing() {
    pairingLoading = true;
    pairingError = '';
    try {
      const url = pairingUrl.replace(/\/+$/, '') + '/';
      const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          connect_url: connectUrl,
          fingerprint,
          client_id: newId,
          password: newPassword
        })
      });
      if (!res.ok) {
        const t = await res.text();
        throw new Error(`pairing service returned ${res.status}: ${t || res.statusText}`);
      }
      const j = await res.json();
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

  async function copyText(text: string, which: 'linux' | 'windows') {
    try {
      await navigator.clipboard.writeText(text);
      copied = which;
      setTimeout(() => { if (copied === which) copied = null; }, 1500);
    } catch {
      // ignore
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
    deletingId = pendingDelete;
    pendingDelete = null;
    try {
      await apiDelete(`/clients-auth/${encodeURIComponent(deletingId)}`);
      await loadRows();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      deletingId = null;
    }
  }

  let liveIdErr = $derived(newId ? validateId(newId) : '');
  let livePwdErr = $derived(newPassword ? validatePassword(newPassword) : '');

  // Compose the installer-flag string from the checkboxes.
  let flagString = $derived(
    [optX && '-x', optS && '-s', optR && '-r', optB && '-b']
      .filter(Boolean)
      .join(' ')
  );

  // Append flags to the second line of the pairing service's rendered
  // installer one-liner (the "sudo sh proxiport-installer.sh" line for
  // Linux, the "powershell -ExecutionPolicy ... .\proxiport-installer.ps1"
  // line for Windows). The pairing service returns the un-flagged base
  // form; flag selection is a client-side concern.
  function appendFlags(base: string, flags: string): string {
    if (!base || !flags) return base;
    const lines = base.split('\n');
    // Match the EXECUTION line, not the download line. The pairing
    // service emits two lines:
    //   curl <url> > proxiport-installer.sh
    //   sudo sh proxiport-installer.sh
    // and a similar pair for PowerShell. We want flags on the second
    // line of each pair (`sudo sh ...` for linux, the
    // `powershell -ExecutionPolicy ...` line for windows), not on
    // the redirect target of the download line.
    const idx = lines.findIndex((l) =>
      /^\s*(sudo\s+sh|powershell)\b/.test(l) &&
      /proxiport-installer\.(sh|ps1)/.test(l)
    );
    if (idx === -1) return base;
    // Don't double-append: strip any prior trailing -<single-letter>
    // tokens that came from a previous pass.
    lines[idx] = lines[idx].replace(/(\s+-[xsrb])+\s*$/g, '');
    lines[idx] = lines[idx].replace(/\s*$/, '') + ' ' + flags;
    return lines.join('\n');
  }

  let linuxCommand = $derived(appendFlags(pairingLinux, flagString));
  let windowsCommand = $derived(appendFlags(pairingWindows, flagString));

  function fmtExpires(s: string): string {
    if (!s) return '';
    try {
      const d = new Date(s);
      const secs = Math.max(0, Math.round((d.getTime() - Date.now()) / 1000));
      return `expires in ${Math.floor(secs / 60)}m ${secs % 60}s`;
    } catch {
      return '';
    }
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
      <button class="btn btn-primary whitespace-nowrap" onclick={openAddModal}>
        + Add credential
      </button>
    {/if}
  </div>

  <ErrorBox message={error} />

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
                    <button class="text-slate-500 hover:text-red-400 cursor-pointer text-sm px-2"
                            disabled={deletingId === r.id}
                            onclick={() => askDelete(r.id)}
                            title="Delete credential">
                      {deletingId === r.id ? '…' : '×'}
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

  {#if !loading && rows.length > 0}
    <p class="text-xs text-slate-500">
      The installer one-liner is generated when a credential is created — the secret can't be read back later.
      To pair a new agent, add a fresh credential{#if canWrite} with <span class="text-slate-400">+ Add credential</span>{/if}.
    </p>
  {/if}
</div>

<!-- Add-credential modal -->
{#if modalOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/70 p-4"
       role="dialog" aria-modal="true" aria-labelledby="add-cred-title"
       onclick={(e) => { if (e.target === e.currentTarget && modalStage === 'form') closeAddModal(); }}>
    <div class="card w-full max-w-lg p-6 space-y-4">
      {#if modalStage === 'form'}
        <h2 id="add-cred-title" class="text-lg font-semibold">Add client-auth credential</h2>
        <p class="text-xs text-slate-400">
          Used by an agent's <code>auth = "&lt;id&gt;:&lt;password&gt;"</code> line to register with this server.
          Case-sensitive.
        </p>
        <form class="space-y-3" onsubmit={submitAdd}>
          <div>
            <label class="block text-xs uppercase tracking-wide text-slate-400 mb-1" for="cred-id">ID</label>
            <input id="cred-id" class="w-full font-mono" type="text" autocomplete="off"
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
            <code class="block mt-1 font-mono text-xs bg-slate-900 p-2 rounded whitespace-pre-wrap">[client]
server = "{connectUrl}"
auth = "{newId}:{newPassword}"
fingerprint = "{fingerprint}"</code>
          </div>
        {:else if pairingCode}
          <div class="text-xs text-slate-500 mb-2">
            Pairing code <code class="font-mono text-slate-300">{pairingCode}</code> · {fmtExpires(pairingExpires)}
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

          <div>
            <div class="flex items-center justify-between mb-1">
              <div class="text-xs uppercase tracking-wide text-slate-400">Linux / macOS</div>
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
            <pre class="font-mono text-xs bg-slate-900 p-3 rounded whitespace-pre-wrap break-all">{windowsCommand}</pre>
          </div>
        {/if}

        <div class="flex justify-end gap-2 pt-2">
          <button class="btn btn-primary" onclick={closeAddModal}>Done</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<!-- Delete confirmation -->
{#if pendingDelete}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/70 p-4"
       role="dialog" aria-modal="true"
       onclick={(e) => { if (e.target === e.currentTarget) cancelDelete(); }}>
    <div class="card w-full max-w-sm p-6 space-y-4">
      <h2 class="text-lg font-semibold">Delete credential</h2>
      <p class="text-sm text-slate-400">
        Remove <code class="font-mono text-slate-200">{pendingDelete}</code>?
        Any agent currently using this credential will be unable to reconnect after its next disconnect.
      </p>
      <div class="flex justify-end gap-2 pt-2">
        <button class="btn btn-ghost" onclick={cancelDelete}>Cancel</button>
        <button class="btn btn-danger" onclick={confirmDelete}>Delete</button>
      </div>
    </div>
  </div>
{/if}
