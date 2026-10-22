import { describe, it, expect } from 'vitest';
import { datetimeLocalValue, expiryToRfc3339, tokenFormError } from './apiTokens';

describe('expiryToRfc3339', () => {
  it('converts a local date-time to a UTC RFC3339 timestamp', () => {
    const local = '2030-06-01T12:00';
    expect(expiryToRfc3339(local)).toBe(new Date(2030, 5, 1, 12, 0).toISOString().replace('.000Z', 'Z'));
  });

  it('returns null for an empty or invalid value', () => {
    expect(expiryToRfc3339('')).toBeNull();
    expect(expiryToRfc3339('tomorrow')).toBeNull();
  });
});

describe('datetimeLocalValue', () => {
  it('formats a date for a datetime-local input', () => {
    expect(datetimeLocalValue(new Date(2030, 0, 2, 3, 4))).toBe('2030-01-02T03:04');
  });
});

describe('tokenFormError', () => {
  const now = new Date(2030, 0, 1, 0, 0).getTime();

  it('rejects a blank or whitespace-only name', () => {
    expect(tokenFormError('   ', '', now)).toMatch(/name/);
  });

  it('rejects an expiry that is not in the future', () => {
    expect(tokenFormError('ci', '2020-01-01T00:00', now)).toMatch(/future/);
  });

  it('accepts a name with no expiry or a future one', () => {
    expect(tokenFormError('ci', '', now)).toBe('');
    expect(tokenFormError('ci', '2031-01-01T00:00', now)).toBe('');
  });
});
