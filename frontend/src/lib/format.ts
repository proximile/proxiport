/** Formatting helpers for tables. */

export function fmtBytes(n: number | undefined | null): string {
  if (n == null || isNaN(n)) return '—';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  let i = 0;
  let v = Number(n);
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export function fmtPercent(n: number | undefined | null): string {
  if (n == null || isNaN(n)) return '—';
  return `${n.toFixed(1)}%`;
}

export function fmtDate(s: string | undefined | null): string {
  if (!s) return '—';
  try {
    const d = new Date(s);
    return d.toLocaleString();
  } catch {
    return String(s);
  }
}

export function fmtRelative(s: string | undefined | null): string {
  if (!s) return '—';
  const d = new Date(s).getTime();
  if (isNaN(d)) return String(s);
  const diff = Date.now() - d;
  const absSec = Math.abs(diff) / 1000;
  if (absSec < 60) return diff >= 0 ? 'just now' : 'in a moment';
  const absMin = absSec / 60;
  if (absMin < 60) return diff >= 0 ? `${Math.floor(absMin)}m ago` : `in ${Math.floor(absMin)}m`;
  const absHr = absMin / 60;
  if (absHr < 24) return diff >= 0 ? `${Math.floor(absHr)}h ago` : `in ${Math.floor(absHr)}h`;
  const absDay = absHr / 24;
  return diff >= 0 ? `${Math.floor(absDay)}d ago` : `in ${Math.floor(absDay)}d`;
}

export function fmtDuration(seconds: number | undefined | null): string {
  if (seconds == null || isNaN(seconds)) return '—';
  const s = Math.floor(seconds);
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`;
}
