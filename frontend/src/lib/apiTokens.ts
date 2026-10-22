/** Helpers for the API token form. */

/**
 * A `<input type="datetime-local">` value (local wall-clock time, no zone) as
 * the RFC3339 UTC timestamp the API expects, or null when it is not a date.
 */
export function expiryToRfc3339(local: string): string | null {
  const t = Date.parse(local);
  if (!local || isNaN(t)) return null;
  return new Date(t).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

/** `now` formatted for a datetime-local input's min attribute. */
export function datetimeLocalValue(now: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}T${pad(now.getHours())}:${pad(now.getMinutes())}`;
}

/** Validation message for the new-token form, or '' when it may be sent. */
export function tokenFormError(name: string, expiry: string, now = Date.now()): string {
  if (!name.trim()) return 'A name is required.';
  if (expiry) {
    const at = expiryToRfc3339(expiry);
    if (!at) return 'The expiry is not a valid date and time.';
    if (Date.parse(at) <= now) return 'The expiry must be in the future.';
  }
  return '';
}
