import { describe, it, expect, vi, afterEach } from 'vitest';

import { resolveRemotePath, waitForOpen, watchUploads, type MessageSource } from './uploads';

/** A minimal event-emitting socket stand-in. */
class FakeSocket implements MessageSource {
  readyState = 0;
  closed = false;
  private listeners: Record<string, Set<(ev: any) => void>> = {};
  addEventListener(type: string, l: (ev: any) => void) {
    (this.listeners[type] ??= new Set()).add(l);
  }
  removeEventListener(type: string, l: (ev: any) => void) {
    this.listeners[type]?.delete(l);
  }
  emit(type: string, ev: any = {}) {
    for (const l of [...(this.listeners[type] ?? [])]) l(ev);
  }
  send(msg: unknown) {
    this.emit('message', { data: JSON.stringify(msg) });
  }
  close() {
    this.closed = true;
  }
}

afterEach(() => {
  vi.useRealTimers();
});

describe('resolveRemotePath', () => {
  it('appends the file name to a folder path', () => {
    expect(resolveRemotePath('/tmp/', 'a.txt')).toBe('/tmp/a.txt');
    expect(resolveRemotePath('C:\\Temp\\', 'a.txt')).toBe('C:\\Temp\\a.txt');
  });

  it('defaults an empty path to /tmp/<file name>', () => {
    expect(resolveRemotePath('  ', 'a.txt')).toBe('/tmp/a.txt');
  });

  it('keeps a full file path as given', () => {
    expect(resolveRemotePath('/srv/app/conf.ini', 'a.txt')).toBe('/srv/app/conf.ini');
  });
});

describe('waitForOpen', () => {
  it('resolves true on open and false on error', async () => {
    const a = new FakeSocket();
    const pa = waitForOpen(a);
    a.emit('open');
    await expect(pa).resolves.toBe(true);

    const b = new FakeSocket();
    const pb = waitForOpen(b);
    b.emit('error');
    await expect(pb).resolves.toBe(false);
  });

  it('resolves false when the socket never opens', async () => {
    vi.useFakeTimers();
    const s = new FakeSocket();
    const p = waitForOpen(s, 1000);
    vi.advanceTimersByTime(1000);
    await expect(p).resolves.toBe(false);
  });
});

describe('watchUploads', () => {
  it('resolves with the result for this upload and client only', async () => {
    const s = new FakeSocket();
    const w = watchUploads(s);
    const p = w.wait('u1', 'host01', 60_000);
    s.send({ uuid: 'other', client_id: 'host01', status: 'success' });
    s.send({ uuid: 'u1', client_id: 'host02', status: 'success' });
    s.send({ uuid: 'u1', client_id: 'host01', status: 'error', message: 'protected directory' });
    await expect(p).resolves.toMatchObject({ status: 'error', message: 'protected directory' });
  });

  it('keeps a result that arrives before the wait starts', async () => {
    const s = new FakeSocket();
    const w = watchUploads(s);
    s.send({ uuid: 'u1', client_id: 'host01', status: 'ignored', message: 'already exists' });
    await expect(w.wait('u1', 'host01', 60_000)).resolves.toMatchObject({ status: 'ignored' });
  });

  it('ignores frames that are not upload results', async () => {
    vi.useFakeTimers();
    const s = new FakeSocket();
    const w = watchUploads(s);
    const p = w.wait('u1', 'host01', 500);
    s.emit('message', { data: 'not json' });
    s.send({ hello: 1 });
    vi.advanceTimersByTime(500);
    await expect(p).resolves.toBeNull();
  });

  it('resolves null on timeout, close or abort', async () => {
    vi.useFakeTimers();
    const s = new FakeSocket();
    const w = watchUploads(s);
    const timedOut = w.wait('u1', 'host01', 1000);
    vi.advanceTimersByTime(1000);
    await expect(timedOut).resolves.toBeNull();

    const closed = w.wait('u1', 'host01', 1000);
    s.emit('close');
    await expect(closed).resolves.toBeNull();

    const ctrl = new AbortController();
    const aborted = w.wait('u1', 'host01', 1000, ctrl.signal);
    ctrl.abort();
    await expect(aborted).resolves.toBeNull();
  });

  it('closes the socket', () => {
    const s = new FakeSocket();
    watchUploads(s).close();
    expect(s.closed).toBe(true);
  });
});
