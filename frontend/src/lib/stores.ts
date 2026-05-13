import { writable, derived, type Writable, type Readable } from 'svelte/store';
import { browser } from '$app/environment';

const TOKEN_KEY = 'proxiport.jwt';
const VAULT_PASS_KEY = 'proxiport.vault.passphrase'; // kept in sessionStorage only

export type VaultInit = 'initialized' | 'uninitialized' | 'unknown';
export type VaultLock = 'unlocked' | 'locked' | '';
export type VaultStatusShape = { init: VaultInit; status: VaultLock };

function persistedToken(): Writable<string | null> {
  const initial = browser ? localStorage.getItem(TOKEN_KEY) : null;
  const s = writable<string | null>(initial);
  if (browser) {
    s.subscribe((v) => {
      if (v) localStorage.setItem(TOKEN_KEY, v);
      else localStorage.removeItem(TOKEN_KEY);
    });
  }
  return s;
}

function sessionVault(): Writable<string | null> {
  const initial = browser ? sessionStorage.getItem(VAULT_PASS_KEY) : null;
  const s = writable<string | null>(initial);
  if (browser) {
    s.subscribe((v) => {
      if (v) sessionStorage.setItem(VAULT_PASS_KEY, v);
      else sessionStorage.removeItem(VAULT_PASS_KEY);
    });
  }
  return s;
}

export const tokenStore = persistedToken();
export const vaultPassphrase = sessionVault();
export const sidebarCollapsed = writable<boolean>(false);

// Authoritative server-side vault status. The settings/vault page and the
// (app) layout populate this; gates around vault-backed pages should derive
// from this rather than from the per-session passphrase store.
export const vaultStatus = writable<VaultStatusShape>({ init: 'unknown', status: '' });

/** True iff the server says the vault is initialized AND unlocked. */
export const vaultUnlocked: Readable<boolean> = derived(
  vaultStatus,
  ($s) => $s.init === 'initialized' && $s.status === 'unlocked'
);

/** A single short-lived toast bus. Kept tiny — one message at a time. */
export type Toast = { id: number; level: 'info' | 'good' | 'warn' | 'bad'; text: string };
export const toasts = writable<Toast[]>([]);
let toastId = 0;
export function pushToast(level: Toast['level'], text: string) {
  const id = ++toastId;
  toasts.update((arr) => [...arr, { id, level, text }]);
  setTimeout(() => toasts.update((arr) => arr.filter((t) => t.id !== id)), 4500);
}
