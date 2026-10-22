import { describe, it, expect } from 'vitest';

import { DEFAULT_LANDING, loginUrl, safeNext } from './redirect';

const origin = 'https://pp.example.com';

describe('loginUrl', () => {
  it('carries the page the user was on', () => {
    expect(loginUrl('/inventory/abc/tunnels?x=1')).toBe('/auth?next=%2Finventory%2Fabc%2Ftunnels%3Fx%3D1');
  });

  it('adds no return path for the root or the sign-in page', () => {
    expect(loginUrl('/')).toBe('/auth');
    expect(loginUrl('')).toBe('/auth');
    expect(loginUrl('/auth?next=%2Ftunnels')).toBe('/auth');
  });
});

describe('safeNext', () => {
  it('returns a same-origin relative path unchanged', () => {
    expect(safeNext('/tunnels', origin)).toBe('/tunnels');
    expect(safeNext('/inventory/abc/files?path=%2Ftmp#top', origin)).toBe('/inventory/abc/files?path=%2Ftmp#top');
  });

  it('falls back when nothing was asked for', () => {
    expect(safeNext(null, origin)).toBe(DEFAULT_LANDING);
    expect(safeNext(undefined, origin)).toBe(DEFAULT_LANDING);
    expect(safeNext('', origin)).toBe(DEFAULT_LANDING);
  });

  it('refuses anything that could leave the app', () => {
    for (const bad of [
      'https://evil.example.net/',
      'javascript:alert(1)',
      '//evil.example.net/x',
      '/\\evil.example.net',
      '/.//evil.example.net',
      '/x/..//evil.example.net',
      '/\t/evil.example.net',
      'tunnels',
      ' /tunnels'
    ]) {
      expect(safeNext(bad, origin), bad).toBe(DEFAULT_LANDING);
    }
  });

  it('never loops back to the sign-in page', () => {
    expect(safeNext('/auth', origin)).toBe(DEFAULT_LANDING);
    expect(safeNext('/auth?next=%2Ftunnels', origin)).toBe(DEFAULT_LANDING);
  });
});
