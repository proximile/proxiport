import { writable, derived, type Writable, type Readable } from 'svelte/store';
import { browser } from '$app/environment';

const TOKEN_KEY = 'proxiport.jwt';

// Matches the server-side constants in server/vault/model.go:
//   DbStatusInit    = "setup-completed"
//   DbStatusNotInit = "uninitialized"
// 'initialized' was an earlier client-side guess that never matched the
// real API value, which made the gate always read locked even when the
// vault was unlocked. Kept as an alias here so a fix on either side
// won't re-break the gate.
export type VaultInit = 'setup-completed' | 'initialized' | 'uninitialized' | 'unknown';
export type VaultLock = 'unlocked' | 'locked' | '';
export type VaultStatusShape = { init: VaultInit; status: VaultLock };

function persistedToken(): Writable<string | null> {
  let initial: string | null = null;
  if (browser) {
    try {
      initial = localStorage.getItem(TOKEN_KEY);
    } catch {
      // storage unavailable: the session lasts until the tab closes
    }
  }
  const s = writable<string | null>(initial);
  if (browser) {
    s.subscribe((v) => {
      try {
        if (v) localStorage.setItem(TOKEN_KEY, v);
        else localStorage.removeItem(TOKEN_KEY);
      } catch {
        // storage unavailable
      }
    });
  }
  return s;
}

export const tokenStore = persistedToken();
// In-memory only: the master passphrase is never persisted (no localStorage /
// sessionStorage). Unlock state is read from vaultStatus/vaultUnlocked below.
export const vaultPassphrase = writable<string | null>(null);
// Remembered per browser. Storage can be unavailable (private mode, blocked
// site data), in which case the flag simply starts expanded each time.
function persistedFlag(key: string): Writable<boolean> {
  let initial = false;
  if (browser) {
    try {
      initial = localStorage.getItem(key) === '1';
    } catch {
      // storage unavailable
    }
  }
  const s = writable<boolean>(initial);
  if (browser) {
    s.subscribe((v) => {
      try {
        localStorage.setItem(key, v ? '1' : '0');
      } catch {
        // storage unavailable
      }
    });
  }
  return s;
}

export const SIDEBAR_COLLAPSED_KEY = 'proxiport.sidebarCollapsed';
export const sidebarCollapsed = persistedFlag(SIDEBAR_COLLAPSED_KEY);
// Mobile-only: whether the off-canvas sidebar drawer is open. Desktop ignores this.
export const sidebarMobileOpen = writable<boolean>(false);

// Authoritative server-side vault status. The settings/vault page and the
// (app) layout populate this; gates around vault-backed pages should derive
// from this rather than from the per-session passphrase store.
export const vaultStatus = writable<VaultStatusShape>({ init: 'unknown', status: '' });

// Why the last vault-status request failed, or '' after a successful one. A
// failed request leaves vaultStatus at its initial value, which must not be
// read as "locked".
export const vaultStatusError = writable<string>('');

/** True iff the server says the vault is initialized AND unlocked. */
export const vaultUnlocked: Readable<boolean> = derived(
  vaultStatus,
  ($s) => ($s.init === 'setup-completed' || $s.init === 'initialized') && $s.status === 'unlocked'
);

/** A single short-lived toast bus. Kept tiny — one message at a time. */
export type Toast = { id: number; level: 'info' | 'good' | 'warn' | 'bad'; text: string };
export const toasts = writable<Toast[]>([]);
let toastId = 0;
export function pushToast(level: Toast['level'], text: string) {
  const id = ++toastId;
  toasts.update((arr) => [...arr, { id, level, text }]);
  setTimeout(() => dismissToast(id), 4500);
}
export function dismissToast(id: number) {
  toasts.update((arr) => arr.filter((t) => t.id !== id));
}
