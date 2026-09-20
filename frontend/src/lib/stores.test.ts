import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';

import { tokenStore, vaultStatus, vaultUnlocked, type VaultInit, type VaultLock } from './stores';

beforeEach(() => {
  localStorage.clear();
  tokenStore.set(null);
  vaultStatus.set({ init: 'unknown', status: '' });
});

describe('the vault gate', () => {
  // 'initialized' was an earlier client-side guess that never matched the real
  // API value ('setup-completed'), which made the gate read locked even when
  // the vault was unlocked. Both spellings must work so a fix on either side
  // cannot re-break it -- and nothing else may read as unlocked.
  const cases: Array<{ init: VaultInit; status: VaultLock; want: boolean }> = [
    { init: 'setup-completed', status: 'unlocked', want: true },
    { init: 'initialized', status: 'unlocked', want: true },
    { init: 'setup-completed', status: 'locked', want: false },
    { init: 'initialized', status: 'locked', want: false },
    { init: 'uninitialized', status: 'unlocked', want: false },
    { init: 'uninitialized', status: 'locked', want: false },
    { init: 'unknown', status: '', want: false },
    { init: 'setup-completed', status: '', want: false }
  ];

  for (const c of cases) {
    it(`is ${c.want} for init=${c.init} status=${c.status || '<empty>'}`, () => {
      vaultStatus.set({ init: c.init, status: c.status });
      expect(get(vaultUnlocked)).toBe(c.want);
    });
  }

  it('defaults to locked before the server has been asked', () => {
    expect(get(vaultUnlocked)).toBe(false);
  });
});

describe('token persistence', () => {
  it('persists a token so a reload keeps the session', () => {
    tokenStore.set('jwt-abc');
    expect(localStorage.getItem('proxiport.jwt')).toBe('jwt-abc');
  });

  it('removes the stored token on logout rather than storing an empty one', () => {
    tokenStore.set('jwt-abc');
    tokenStore.set(null);
    // A leftover empty-string entry would make a reload look authenticated.
    expect(localStorage.getItem('proxiport.jwt')).toBeNull();
  });

  it('treats an empty string as no token', () => {
    tokenStore.set('jwt-abc');
    tokenStore.set('');
    expect(localStorage.getItem('proxiport.jwt')).toBeNull();
  });
});
