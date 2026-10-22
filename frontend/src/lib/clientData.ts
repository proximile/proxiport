/**
 * Pure helpers for the per-client data panels (overview, monitoring,
 * tunnels). Kept out of the components so they can be unit-tested.
 */
import type { Tunnel } from './types';

/** One row of a process snapshot, as the agent reports it. */
export type Process = {
  pid?: number;
  parent_pid?: number;
  name?: string;
  state?: string;
  cmdline?: string;
  rss?: number;
  memory_usage_percent?: number;
  cpu_avg_usage_percent?: number;
};

/**
 * The processes field of a snapshot. The API sends it as a JSON array; an
 * older server or a stored row it could not decode may send a JSON-encoded
 * string or null instead. Anything that is not a list yields [].
 */
export function normalizeProcesses(raw: unknown): Process[] {
  let v = raw;
  if (typeof v === 'string') {
    try {
      v = JSON.parse(v);
    } catch {
      return [];
    }
  }
  return Array.isArray(v) ? (v as Process[]) : [];
}

/** The agent reports -1 when a process's parent could not be read. */
export function fmtPid(n: number | undefined | null): string {
  return n != null && n >= 0 ? String(n) : '—';
}

/**
 * The most recent sample of a /metrics response. The endpoint answers with a
 * list (newest first by default); an object is accepted as-is.
 */
export function latestSample<T>(v: unknown): T | null {
  if (Array.isArray(v)) return (v[0] as T) ?? null;
  if (v && typeof v === 'object') return v as T;
  return null;
}

/** Evenly spaced timestamps (ms) from t0 to t1 inclusive, for an x axis. */
export function timeTicks(t0: number, t1: number, count: number): number[] {
  if (!(t1 > t0) || count < 2) return [t0];
  const step = (t1 - t0) / (count - 1);
  return Array.from({ length: count }, (_, i) => t0 + step * i);
}

/** A tick label: time only for spans up to 12 h, date and time beyond that. */
export function fmtTick(ms: number, spanMs: number): string {
  const d = new Date(ms);
  if (spanMs > 12 * 3600 * 1000) {
    return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  }
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

/**
 * The address a user opens to reach a tunnel. A tunnel served under its own
 * subdomain carries the full URL in tunnel_url. Otherwise it is this server's
 * host and the tunnel's port, with a scheme where one applies: https when the
 * server terminates TLS (http_proxy), the tunnel's own http/https scheme, and
 * none for ssh, tcp and the rest.
 */
export function tunnelPublicUrl(t: Tunnel, host: string): string {
  if (t.tunnel_url) return t.tunnel_url;
  const addr = `${host || t.lhost || ''}:${t.lport ?? ''}`;
  if (t.http_proxy) return `https://${addr}`;
  const s = t.scheme || t.protocol;
  if (s === 'http' || s === 'https') return `${s}://${addr}`;
  return addr;
}

/** The idle timeout cell: 0 or unset means the tunnel never idles out. */
export function fmtIdle(minutes: number | undefined | null): string {
  return minutes ? `${minutes}m` : 'never';
}
