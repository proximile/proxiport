<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPost, apiPut, apiDelete } from '$lib/api';
  import type { User, Group, ServerStatus } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  const ADMIN_GROUP = 'Administrators';
  const PERM_KEYS = ['tunnels', 'commands', 'scripts', 'scheduler', 'monitoring', 'auditlog', 'uploads', 'vault'];

  let users: User[] = $state([]);
  let groups: Group[] = $state([]);
  let twoFaEnabled = $state(false);
  let twoFaMethod = $state('');
  let minLen = $state(0);
  let meUsername = $state('');
  let authSource = $state('');
  let loading = $state(true);
  let error = $state('');

  // In "Static Credentials" mode the API authenticates against a single
  // user/password pair from the config (`[api] auth = "..."`), and the server
  // refuses every user-management call. There's nothing to add or edit, so we
  // hide the controls and explain why instead of surfacing 400s.
  const staticMode = $derived(authSource === 'Static Credentials');

  // `two_fa_enabled` is the union of TOTP and delivery-based 2FA, but the two
  // are mutually exclusive and behave differently: only TOTP has a per-user
  // secret to reset, and only delivery-based 2FA uses a `two_fa_send_to`
  // recipient. Split them so controls match the actual mode.
  const totpMode = $derived(twoFaMethod === 'totp_authenticator_app');
  const deliveryTwoFa = $derived(twoFaEnabled && !totpMode);

  // Edit/create form state. `editing` holds the username being edited, or
  // '' for a brand-new user; `formOpen` gates the form card's visibility.
  let formOpen = $state(false);
  let editing = $state('');
  let fUsername = $state('');
  let fPassword = $state('');
  let fTwoFaSendTo = $state('');
  let fGroups: string[] = $state([]);
  let fExpire = $state(false);
  let fNewGroup = $state('');
  let saving = $state(false);

  async function load() {
    loading = true;
    error = '';
    try {
      // Read status + the current user first. In static-credentials mode both
      // /users and /user-groups are rejected server-side, so we branch on the
      // auth source rather than letting those calls surface as errors.
      const [s, me] = await Promise.all([
        apiGet<ServerStatus>('/status').catch(() => ({}) as ServerStatus),
        apiGet<User>('/me').catch(() => null)
      ]);
      twoFaEnabled = !!s?.two_fa_enabled;
      twoFaMethod = s?.two_fa_delivery_method ?? '';
      minLen = s?.password_min_length ?? 0;
      meUsername = me?.username ?? '';
      authSource = s?.users_auth_source ?? '';

      if (authSource === 'Static Credentials') {
        // Only the one configured account exists, and it's exactly /me.
        users = me ? [me] : [];
        groups = [];
      } else {
        users = (await apiGet<User[]>('/users')) ?? [];
        groups = (await apiGet<Group[]>('/user-groups').catch(() => [] as Group[])) ?? [];
      }
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  // Permission summary for a group name, so the admin can see what assigning
  // a group actually grants. Groups only exist as permission sets in the
  // database backend; Administrators always grants everything.
  const permsByGroup = $derived(new Map(groups.map((g) => [g.name, g.permissions ?? {}])));
  function grantsFor(name: string): string[] {
    if (name === ADMIN_GROUP) return PERM_KEYS;
    const p = permsByGroup.get(name);
    return p ? PERM_KEYS.filter((k) => p[k]) : [];
  }

  // The set of groups offered as checkboxes: every known group, plus the admin
  // group, plus any group already assigned to a user (so nothing is hidden),
  // plus whatever the admin has typed in for this form. Sorted, de-duplicated.
  const groupChoices = $derived.by(() => {
    const s = new Set<string>([ADMIN_GROUP]);
    for (const g of groups) s.add(g.name);
    for (const u of users) for (const gn of u.groups ?? []) s.add(gn);
    for (const gn of fGroups) s.add(gn);
    return [...s].sort((a, b) => a.localeCompare(b));
  });

  function openCreate() {
    editing = '';
    fUsername = '';
    fPassword = '';
    fTwoFaSendTo = '';
    fGroups = [];
    fExpire = false;
    fNewGroup = '';
    error = '';
    formOpen = true;
  }

  function openEdit(u: User) {
    editing = u.username;
    fUsername = u.username;
    fPassword = '';
    fTwoFaSendTo = u.two_fa_send_to ?? '';
    fGroups = [...(u.groups ?? [])];
    fExpire = !!u.password_expired;
    fNewGroup = '';
    error = '';
    formOpen = true;
  }

  function closeForm() {
    formOpen = false;
    fPassword = ''; // don't leave a typed password sitting in component state
  }

  function addCustomGroup() {
    const name = fNewGroup.trim();
    if (!name) return;
    if (!fGroups.includes(name)) fGroups = [...fGroups, name];
    fNewGroup = '';
  }

  async function save(e: Event) {
    e.preventDefault();
    error = '';
    saving = true;
    try {
      if (editing === '') {
        const payload: Record<string, unknown> = {
          username: fUsername.trim(),
          password: fPassword,
          groups: fGroups
        };
        if (fTwoFaSendTo) payload.two_fa_send_to = fTwoFaSendTo;
        if (fExpire) payload.password_expired = true;
        await apiPost('/users', payload);
        pushToast('good', `User "${fUsername.trim()}" created.`);
      } else {
        // Always send `groups` (a non-nil array) so the request is never a
        // no-op; password is sent only when set, so a blank field keeps the
        // existing one. `password_expired` is always sent so it can be
        // toggled on or off. The 2FA recipient only exists for delivery-based
        // 2FA, so only send it in that mode.
        const payload: Record<string, unknown> = {
          groups: fGroups,
          password_expired: fExpire
        };
        if (deliveryTwoFa) payload.two_fa_send_to = fTwoFaSendTo;
        if (fPassword) payload.password = fPassword;
        await apiPut(`/users/${encodeURIComponent(editing)}`, payload);
        pushToast('good', `User "${editing}" updated.`);
      }
      formOpen = false;
      fPassword = '';
      await load();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      saving = false;
    }
  }

  async function del(u: User) {
    if (u.username === meUsername) return;
    if (!confirm(`Delete user "${u.username}"? This cannot be undone.`)) return;
    try {
      await apiDelete(`/users/${encodeURIComponent(u.username)}`);
      pushToast('good', `User "${u.username}" deleted.`);
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }

  async function reset2fa(u: User) {
    if (!confirm(`Reset the authenticator (TOTP) secret for "${u.username}"? They will re-enroll on next login.`)) return;
    try {
      await apiDelete(`/users/${encodeURIComponent(u.username)}/totp-secret`);
      pushToast('good', `2FA secret reset for "${u.username}".`);
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">Users</h1>
    <div class="flex gap-2">
      <button class="btn btn-ghost" onclick={load}>Refresh</button>
      {#if !staticMode}<button class="btn btn-primary" onclick={openCreate}>New user</button>{/if}
    </div>
  </div>

  <ErrorBox message={error} />

  {#if staticMode}
    <div class="card p-4 space-y-2 border-amber-500/40">
      <h2 class="font-medium text-amber-200">User management is disabled in this mode</h2>
      <p class="text-sm text-slate-400">
        This server authenticates its API with a single static user/password pair
        (<span class="font-mono">[api] auth = "…"</span> in <span class="font-mono">proxiportd.conf</span>),
        so there is only ever the one account below and users can't be added or edited.
      </p>
      <p class="text-sm text-slate-400">
        To manage multiple users and their permissions, switch the API to a
        user store: set <span class="font-mono">auth_file</span> (a JSON file) or
        <span class="font-mono">auth_user_table</span> +
        <span class="font-mono">auth_group_table</span> +
        <span class="font-mono">auth_group_details_table</span> (a database),
        then restart the server.
      </p>
    </div>
  {/if}

  {#if formOpen}
    <div class="card p-4 space-y-4">
      <h2 class="font-medium">{editing === '' ? 'New user' : `Edit user — ${editing}`}</h2>
      <form class="space-y-4" onsubmit={save}>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
          <label class="text-xs">
            <span class="text-slate-400 uppercase">Username</span>
            <input bind:value={fUsername} required disabled={editing !== ''} autocomplete="off" />
          </label>
          <label class="text-xs">
            <span class="text-slate-400 uppercase">Password</span>
            <input
              type="password"
              bind:value={fPassword}
              required={editing === ''}
              minlength={minLen || undefined}
              placeholder={editing === '' ? '' : 'leave blank to keep current'}
              autocomplete="new-password"
            />
            {#if minLen > 0}<span class="text-slate-500 normal-case">Minimum {minLen} characters.</span>{/if}
          </label>
          {#if deliveryTwoFa}
            <label class="text-xs md:col-span-2">
              <span class="text-slate-400 uppercase">2FA recipient (required)</span>
              <input
                bind:value={fTwoFaSendTo}
                required={editing === ''}
                placeholder="email or phone the 2FA code is sent to"
                class="font-mono"
              />
            </label>
          {/if}
        </div>

        <fieldset class="space-y-2">
          <legend class="text-xs text-slate-400 uppercase">Groups (permissions)</legend>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {#each groupChoices as g}
              <label class="flex items-start gap-2 p-2 rounded border border-slate-700/60 text-sm">
                <input type="checkbox" value={g} bind:group={fGroups} class="mt-0.5" />
                <span class="min-w-0">
                  <span class="font-medium">{g}</span>
                  {#if g === ADMIN_GROUP}
                    <span class="pill pill-good ml-1">full admin</span>
                  {:else}
                    <span class="flex flex-wrap gap-1 mt-1">
                      {#each grantsFor(g) as k}<span class="pill pill-info">{k}</span>{/each}
                      {#if grantsFor(g).length === 0}<span class="text-slate-500 text-xs">no permissions set</span>{/if}
                    </span>
                  {/if}
                </span>
              </label>
            {/each}
          </div>
          <div class="flex gap-2 items-center">
            <input
              bind:value={fNewGroup}
              placeholder="new group name…"
              aria-label="New group name"
              class="text-sm"
              onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addCustomGroup(); } }}
            />
            <button type="button" class="btn btn-ghost" onclick={addCustomGroup}>Add group</button>
          </div>
          <p class="text-xs text-slate-500">
            A user's permissions are the union of the permissions granted by its groups.
            The <span class="font-mono">Administrators</span> group grants everything.
            Set what a group grants under <span class="font-medium">User groups</span>.
          </p>
        </fieldset>

        <label class="flex items-center gap-2 text-sm">
          <input type="checkbox" bind:checked={fExpire} />
          Require password change on next login
        </label>

        <div class="flex gap-2">
          <button class="btn btn-primary" type="submit" disabled={saving}>
            {saving ? 'Saving…' : editing === '' ? 'Create user' : 'Save changes'}
          </button>
          <button class="btn btn-ghost" type="button" onclick={closeForm}>Cancel</button>
        </div>
      </form>
    </div>
  {/if}

  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !users.length}
      <EmptyState title="No users" />
    {:else}
      <table class="tbl">
        <thead><tr><th>Username</th><th>Groups</th><th>2FA recipient</th><th></th></tr></thead>
        <tbody>
          {#each users as u}
            <tr>
              <td class="font-medium">
                {u.username}
                {#if u.username === meUsername}<span class="pill pill-muted ml-1">you</span>{/if}
              </td>
              <td>
                <div class="flex flex-wrap gap-1">
                  {#each u.groups ?? [] as g}
                    <span class="pill {g === ADMIN_GROUP ? 'pill-good' : 'pill-info'}">{g}</span>
                  {/each}
                  {#if !(u.groups ?? []).length}<span class="text-slate-500 text-xs">—</span>{/if}
                </div>
              </td>
              <td class="font-mono text-xs">{u.two_fa_send_to || '—'}</td>
              <td>
                {#if !staticMode}
                  <div class="flex gap-2 justify-end">
                    <button class="btn btn-ghost" onclick={() => openEdit(u)}>Edit</button>
                    {#if totpMode}
                      <button class="btn btn-ghost" onclick={() => reset2fa(u)}>Reset 2FA</button>
                    {/if}
                    <button
                      class="btn btn-danger"
                      disabled={u.username === meUsername}
                      title={u.username === meUsername ? "You can't delete your own account" : ''}
                      onclick={() => del(u)}
                    >Delete</button>
                  </div>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
