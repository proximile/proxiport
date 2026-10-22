import { describe, it, expect } from 'vitest';
import { normalizeProcesses, fmtPid, latestSample, timeTicks, tunnelPublicUrl, fmtIdle } from './clientData';

describe('normalizeProcesses', () => {
  it('accepts the array the API sends', () => {
    const list = [{ pid: 1, parent_pid: 0, name: 'init' }];
    expect(normalizeProcesses(list)).toEqual(list);
  });
  it('accepts a JSON-encoded string', () => {
    expect(normalizeProcesses('[{"pid":7}]')).toEqual([{ pid: 7 }]);
  });
  it('yields an empty list for null, bad JSON and non-lists', () => {
    expect(normalizeProcesses(null)).toEqual([]);
    expect(normalizeProcesses('not json')).toEqual([]);
    expect(normalizeProcesses('{"pid":1}')).toEqual([]);
    expect(normalizeProcesses({ pid: 1 })).toEqual([]);
  });
});

describe('fmtPid', () => {
  it('shows 0 and hides the unavailable marker', () => {
    expect(fmtPid(0)).toBe('0');
    expect(fmtPid(42)).toBe('42');
    expect(fmtPid(-1)).toBe('—');
    expect(fmtPid(undefined)).toBe('—');
  });
});

describe('latestSample', () => {
  it('takes the first element of a list', () => {
    expect(latestSample([{ cpu_usage_percent: 21.6 }, { cpu_usage_percent: 3 }])).toEqual({ cpu_usage_percent: 21.6 });
  });
  it('is null for an empty list or nothing', () => {
    expect(latestSample([])).toBeNull();
    expect(latestSample(null)).toBeNull();
  });
  it('passes an object through', () => {
    expect(latestSample({ cpu_usage_percent: 1 })).toEqual({ cpu_usage_percent: 1 });
  });
});

describe('timeTicks', () => {
  it('spaces ticks evenly including both ends', () => {
    expect(timeTicks(0, 100, 5)).toEqual([0, 25, 50, 75, 100]);
  });
  it('returns a single tick for an empty span', () => {
    expect(timeTicks(10, 10, 5)).toEqual([10]);
  });
});

describe('tunnelPublicUrl', () => {
  const host = 'pp.example.com';
  it('prefers the subdomain URL', () => {
    expect(tunnelPublicUrl({ lport: '20001', http_proxy: true, tunnel_url: 'https://abc.example.com' }, host)).toBe(
      'https://abc.example.com'
    );
  });
  it('uses https when the server terminates TLS', () => {
    expect(tunnelPublicUrl({ lport: '20001', scheme: 'http', http_proxy: true }, host)).toBe('https://pp.example.com:20001');
  });
  it('uses the http/https scheme of the tunnel', () => {
    expect(tunnelPublicUrl({ lport: '20002', scheme: 'http' }, host)).toBe('http://pp.example.com:20002');
    expect(tunnelPublicUrl({ lport: '20003', scheme: 'https' }, host)).toBe('https://pp.example.com:20003');
  });
  it('leaves other schemes as host:port', () => {
    expect(tunnelPublicUrl({ lport: '20004', scheme: 'ssh' }, host)).toBe('pp.example.com:20004');
    expect(tunnelPublicUrl({ lport: '20005' }, '')).toBe(':20005');
    expect(tunnelPublicUrl({ lport: '20006', lhost: '203.0.113.7' }, '')).toBe('203.0.113.7:20006');
  });
});

describe('fmtIdle', () => {
  it('reads 0 or unset as never', () => {
    expect(fmtIdle(0)).toBe('never');
    expect(fmtIdle(undefined)).toBe('never');
    expect(fmtIdle(5)).toBe('5m');
  });
});
