import { describe, it, expect } from 'vitest';
import { utf8ToBase64, base64ToUtf8, singleHostAcl } from './encoding';

describe('utf8ToBase64', () => {
  it('round-trips plain ASCII', () => {
    expect(base64ToUtf8(utf8ToBase64('#!/bin/bash\nset -e\n'))).toBe('#!/bin/bash\nset -e\n');
  });

  // btoa() throws outright above U+00FF. These are ordinary things to paste
  // into a script or type into a password.
  it.each(['€100', '😀', '日本語', 'café — “smart quotes”'])(
    'encodes %s, which btoa alone cannot',
    (s) => {
      expect(() => utf8ToBase64(s)).not.toThrow();
      expect(base64ToUtf8(utf8ToBase64(s))).toBe(s);
    }
  );

  // The quieter half of the bug: for U+0080..U+00FF btoa does not throw, it
  // just encodes the Latin-1 byte, so the server receives different bytes
  // than the user typed.
  it('encodes Latin-1 range as UTF-8, not as a single byte', () => {
    expect(utf8ToBase64('é')).toBe('w6k='); // 0xC3 0xA9
    expect(btoa('é')).toBe('6Q=='); // 0xE9 -- what the old path sent
  });

  it('handles a body larger than the chunk size without overflowing', () => {
    const big = 'a'.repeat(0x8000 * 2 + 17);
    expect(base64ToUtf8(utf8ToBase64(big))).toBe(big);
  });
});

describe('base64ToUtf8', () => {
  it('returns null for input that is not base64 at all', () => {
    expect(base64ToUtf8('#!/bin/bash\nset -e')).toBeNull();
  });

  it('returns null for base64 that does not decode to valid UTF-8', () => {
    expect(base64ToUtf8('///+')).toBeNull();
  });

  // The dangerous case from the audit: a short command like 'date', 'free' or
  // 'sync' is four characters from the base64 alphabet, so the server accepted
  // it and executed the three bytes it decodes to. Those bytes (0x75 0xa7
  // 0x5e for 'date') are not valid UTF-8, so a strict decoder rejects them --
  // which is what lets the edit form tell a real encoded body from a legacy
  // raw one and fall back to showing the raw text instead of mojibake.
  it.each(['date', 'free', 'sync'])(
    'rejects %s, which is accidentally valid base64 but not valid UTF-8',
    (s) => {
      expect(base64ToUtf8(s)).toBeNull();
    }
  );
});

describe('singleHostAcl', () => {
  it('uses /32 for IPv4', () => {
    expect(singleHostAcl('203.0.113.7')).toBe('203.0.113.7/32');
  });

  // The bug: /32 on an IPv6 address is an ISP-sized block, and the server
  // accepts it without complaint.
  it('uses /128 for IPv6, not /32', () => {
    expect(singleHostAcl('2a02:1234:5678::9')).toBe('2a02:1234:5678::9/128');
    expect(singleHostAcl('::1')).toBe('::1/128');
  });

  it('is empty for an empty address, so the form does not send a bare slash', () => {
    expect(singleHostAcl('')).toBe('');
    expect(singleHostAcl('   ')).toBe('');
  });
});
