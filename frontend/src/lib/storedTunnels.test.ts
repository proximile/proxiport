import { describe, it, expect } from 'vitest';

import {
  ACL_MODE_CURRENT,
  aclForStoredTunnel,
  storedAclFollowsUser,
  storedAclLabel
} from './storedTunnels';

const follows = { acl: '192.0.2.10/32', further_options: { acl_mode: ACL_MODE_CURRENT } };
const fixed = { acl: '198.51.100.0/24', further_options: { auto_close: '1h' } };

describe('storedAclFollowsUser', () => {
  it('is true only for an entry saved with the marker', () => {
    expect(storedAclFollowsUser(follows)).toBe(true);
    expect(storedAclFollowsUser(fixed)).toBe(false);
    expect(storedAclFollowsUser({ acl: '192.0.2.10/32' })).toBe(false);
    expect(storedAclFollowsUser({ acl: '192.0.2.10/32', further_options: null })).toBe(false);
    expect(storedAclFollowsUser({ further_options: { acl_mode: 'specific' } })).toBe(false);
  });
});

describe('storedAclLabel', () => {
  it('names the choice instead of the address that was current when it was saved', () => {
    expect(storedAclLabel(follows)).toBe('My current IP address');
  });

  it('shows a fixed ACL as it is, and a dash when there is none', () => {
    expect(storedAclLabel(fixed)).toBe('198.51.100.0/24');
    expect(storedAclLabel({ acl: '' })).toBe('—');
    expect(storedAclLabel({})).toBe('—');
  });
});

describe('aclForStoredTunnel', () => {
  it('uses the address of whoever uses the entry, not the saved one', async () => {
    const acl = await aclForStoredTunnel(follows, async () => '203.0.113.7');
    expect(acl).toBe('203.0.113.7/32');
  });

  it('makes a single-host entry for an IPv6 address', async () => {
    const acl = await aclForStoredTunnel(follows, async () => '2001:db8::7');
    expect(acl).toBe('2001:db8::7/128');
  });

  it('does not look the address up for an entry with a fixed ACL', async () => {
    let asked = 0;
    const lookup = async () => {
      asked += 1;
      return '203.0.113.7';
    };
    expect(await aclForStoredTunnel(fixed, lookup)).toBe('198.51.100.0/24');
    expect(await aclForStoredTunnel({ acl: '192.0.2.10/32' }, lookup)).toBe('192.0.2.10/32');
    expect(await aclForStoredTunnel({}, lookup)).toBe('');
    expect(asked).toBe(0);
  });

  it('refuses rather than returning an empty ACL when the address is unknown', async () => {
    await expect(aclForStoredTunnel(follows, async () => '')).rejects.toThrow(/current IP address/);
    await expect(aclForStoredTunnel(follows, async () => '   ')).rejects.toThrow(/current IP address/);
  });

  it('refuses when the lookup fails, and says why', async () => {
    const failing = async () => {
      throw new Error('request timed out');
    };
    await expect(aclForStoredTunnel(follows, failing)).rejects.toThrow(/request timed out/);
  });

  it('never falls back to the saved address', async () => {
    await expect(aclForStoredTunnel(follows, async () => '')).rejects.toThrow();
  });
});
