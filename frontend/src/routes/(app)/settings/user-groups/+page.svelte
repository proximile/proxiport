<script lang="ts">
  import { onMount } from 'svelte';
  import { apiGet, apiPut, apiDelete, ApiException } from '$lib/api';
  import type { Group, ServerStatus } from '$lib/types';
  import Spinner from '$lib/components/Spinner.svelte';
  import EmptyState from '$lib/components/EmptyState.svelte';
  import ErrorBox from '$lib/components/ErrorBox.svelte';
  import { pushToast } from '$lib/stores';

  const ADMIN_GROUP = 'Administrators';
  const PERM_KEYS = ['tunnels', 'commands', 'scripts', 'scheduler', 'monitoring', 'auditlog', 'uploads', 'vault'];

  let groups: Group[] = $state([]);
  let editable = $state(false);
  let loading = $state(true);
  let error = $state('');
  let savingName = $state('');

  // Local editable copy of each group's permission map, keyed by group name.
  // The server always returns the full map (every key present), so a checkbox
  // can bind straight to edited[name][key].
  let edited: Record<string, Record<string, boolean>> = $state({});

  async function load() {
    loading = true;
    error = '';
    try {
      const [g, s] = await Promise.all([
        // Static-credentials / single-user providers reject /user-groups with
        // HTTP 400 — treat only that as "no editable groups" and fall back to an
        // empty list (the read-only notice below explains why). Any other error
        // (500, network) must propagate so it surfaces instead of masquerading
        // as an empty, read-only page.
        apiGet<Group[]>('/user-groups').catch((err) => {
          if (err instanceof ApiException && err.status === 400) return [] as Group[];
          throw err;
        }),
        apiGet<ServerStatus>('/status')
      ]);
      groups = g ?? [];
      editable = !!s?.group_permissions_enabled;
      const next: Record<string, Record<string, boolean>> = {};
      for (const grp of groups) {
        const perms = grp.permissions ?? {};
        next[grp.name] = Object.fromEntries(PERM_KEYS.map((k) => [k, !!perms[k]]));
      }
      edited = next;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  onMount(load);

  async function save(g: Group) {
    savingName = g.name;
    error = '';
    try {
      const payload: Record<string, unknown> = { permissions: edited[g.name] };
      // Round-trip extended restrictions untouched so a permissions edit
      // never silently drops them.
      if (g.tunnels_restricted != null) payload.tunnels_restricted = g.tunnels_restricted;
      if (g.commands_restricted != null) payload.commands_restricted = g.commands_restricted;
      const updated = await apiPut<Group>(`/user-groups/${encodeURIComponent(g.name)}`, payload);
      pushToast('good', `Permissions saved for "${g.name}".`);
      // Sync only this row from the server response. A full reload would
      // rebuild `edited` from scratch and discard unsaved toggles the admin
      // may have made in other rows.
      const idx = groups.findIndex((x) => x.name === g.name);
      if (updated && idx >= 0) {
        groups[idx] = updated;
        const perms = updated.permissions ?? {};
        edited[g.name] = Object.fromEntries(PERM_KEYS.map((k) => [k, !!perms[k]]));
      }
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    } finally {
      savingName = '';
    }
  }

  async function del(name: string) {
    if (!confirm(`Delete permission set for group "${name}"? Members lose the permissions it grants.`)) return;
    try {
      await apiDelete(`/user-groups/${encodeURIComponent(name)}`);
      pushToast('good', `Group "${name}" deleted.`);
      await load();
    } catch (err) {
      pushToast('bad', err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="p-6 space-y-4">
  <div class="flex items-center justify-between">
    <h1 class="text-2xl font-semibold tracking-tight">User groups</h1>
    <button class="btn btn-ghost" onclick={load}>Refresh</button>
  </div>

  <p class="text-sm text-slate-400">
    A group is a named set of permissions. Assign groups to a user under
    <span class="font-medium">Users</span>; the user's effective permissions are the
    union across its groups. To create a new group, assign its name to a user, then set
    what it grants here.
  </p>

  {#if !loading && !editable}
    <div class="pill pill-warn">Permissions are fixed by the current authentication backend and can't be edited here.</div>
  {/if}

  <ErrorBox message={error} />

  <div class="card overflow-x-auto">
    {#if loading}
      <div class="p-6 flex justify-center"><Spinner /></div>
    {:else if !groups.length}
      <EmptyState title="No user groups" />
    {:else}
      <table class="tbl">
        <thead>
          <tr>
            <th>Group</th>
            {#each PERM_KEYS as k}<th>{k}</th>{/each}
            {#if editable}<th></th>{/if}
          </tr>
        </thead>
        <tbody>
          {#each groups as g}
            {@const isAdmin = g.name === ADMIN_GROUP}
            <tr>
              <td class="font-medium">
                {g.name}
                {#if isAdmin}<span class="pill pill-good ml-1">built-in</span>{/if}
              </td>
              {#each PERM_KEYS as k}
                <td>
                  {#if editable && !isAdmin}
                    <input type="checkbox" bind:checked={edited[g.name][k]} aria-label={`${g.name}: ${k}`} />
                  {:else if isAdmin || g.permissions?.[k]}
                    <span class="pill pill-good">on</span>
                  {:else}
                    <span class="pill pill-muted">off</span>
                  {/if}
                </td>
              {/each}
              {#if editable}
                <td>
                  {#if !isAdmin}
                    <div class="flex gap-2 justify-end">
                      <button class="btn btn-primary" disabled={savingName === g.name} onclick={() => save(g)}>
                        {savingName === g.name ? 'Saving…' : 'Save'}
                      </button>
                      <button class="btn btn-danger" onclick={() => del(g.name)}>Delete</button>
                    </div>
                  {/if}
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>
