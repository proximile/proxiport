import { describe, it, expect, vi, afterEach } from 'vitest';
import { get } from 'svelte/store';

import { copyToClipboard } from './clipboard';
import { toasts } from './stores';

afterEach(() => {
  toasts.set([]);
  vi.unstubAllGlobals();
});

function lastToast() {
  const all = get(toasts);
  return all[all.length - 1];
}

describe('copyToClipboard', () => {
  it('uses the Clipboard API in a secure context', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal('isSecureContext', true);
    vi.stubGlobal('navigator', { clipboard: { writeText } });
    expect(await copyToClipboard('hello', 'Copied it.')).toBe(true);
    expect(writeText).toHaveBeenCalledWith('hello');
    expect(lastToast()).toMatchObject({ level: 'good', text: 'Copied it.' });
  });

  it('reports a failure when the Clipboard API rejects', async () => {
    vi.stubGlobal('isSecureContext', true);
    vi.stubGlobal('navigator', { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    expect(await copyToClipboard('hello')).toBe(false);
    expect(lastToast()?.level).toBe('bad');
  });

  // Over plain http there is no navigator.clipboard; the textarea fallback is
  // used, and a refused execCommand is a failure, not a silent success.
  it('falls back to execCommand outside a secure context', async () => {
    vi.stubGlobal('isSecureContext', false);
    const exec = vi.fn().mockReturnValue(true);
    document.execCommand = exec;
    expect(await copyToClipboard('hello')).toBe(true);
    expect(exec).toHaveBeenCalledWith('copy');
    expect(document.querySelector('textarea')).toBeNull();

    exec.mockReturnValue(false);
    expect(await copyToClipboard('hello')).toBe(false);
    expect(lastToast()?.level).toBe('bad');
  });
});
