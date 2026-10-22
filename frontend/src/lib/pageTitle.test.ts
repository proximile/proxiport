import { describe, it, expect } from 'vitest';

import { documentTitle, sectionTitle } from './pageTitle';

describe('sectionTitle', () => {
  it('names the section of a top-level page', () => {
    expect(sectionTitle('/inventory')).toBe('Inventory · ProxiPort');
    expect(sectionTitle('/settings/vault')).toBe('Vault · ProxiPort');
    expect(sectionTitle('/commands/library')).toBe('Library · ProxiPort');
    expect(sectionTitle('/settings/users/')).toBe('Users · ProxiPort');
  });

  it('leaves client detail pages to their own layout', () => {
    expect(sectionTitle('/inventory/abc')).toBeNull();
    expect(sectionTitle('/inventory/abc/tunnels')).toBeNull();
  });

  it('falls back to the product name for anything else', () => {
    expect(sectionTitle('/settings/no-such-page')).toBe('ProxiPort');
  });
});

describe('documentTitle', () => {
  it('joins the parts most-specific first and skips empty ones', () => {
    expect(documentTitle('host01', 'Monitoring')).toBe('host01 · Monitoring · ProxiPort');
    expect(documentTitle('', null, 'Inventory')).toBe('Inventory · ProxiPort');
  });
});
