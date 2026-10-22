/** Pure helpers for the schedule editor and the schedule tables. */
import { utf8ToBase64 } from './encoding';
import type { Schedule, ScheduleExecution } from './types';

export type ScheduleForm = {
  name: string;
  schedule: string;
  type: string;
  command: string;
  script: string;
  interpreter: string;
  cwd: string;
  is_sudo: boolean;
  timeout_sec: number | string;
  client_ids: string[];
  group_ids: string[];
  tags: string;
  tags_operator: string;
  execute_concurrently: boolean;
  abort_on_error: boolean;
  overlaps: boolean;
};

// Keys the server sets itself; sending them back on an update is noise.
const SERVER_OWNED = ['id', 'created_at', 'created_by', 'last_execution'];

export function parseList(s: string): string[] {
  return [...new Set(s.split(',').map((t) => t.trim()).filter(Boolean))];
}

/** Validation message for a timeout field, or '' when it is a whole number >= 1. */
export function timeoutError(v: unknown): string {
  const t = typeof v === 'string' && v.trim() === '' ? NaN : Number(v);
  if (!Number.isInteger(t) || t < 1) return 'Timeout must be a whole number of at least 1 second.';
  return '';
}

export function scheduleFormError(f: ScheduleForm): string {
  if (!f.name?.trim()) return 'A name is required.';
  if (!f.schedule?.trim()) return 'A cron schedule is required.';
  if (f.type === 'command' && !f.command?.trim()) return 'The command is required.';
  if (f.type === 'script' && !f.script?.trim()) return 'The script body is required.';
  const t = timeoutError(f.timeout_sec);
  if (t) return t;
  const byId = f.client_ids.length > 0 || f.group_ids.length > 0;
  const byTags = parseList(f.tags).length > 0;
  if (byId && byTags) return 'Target either clients and groups, or tags, not both.';
  if (!byId && !byTags) return 'Select at least one target client, group or tag.';
  return '';
}

/** Form state for an existing schedule as GET /schedules/{id} returns it. */
export function formFromSchedule(data: Record<string, any>, decodeScript: (s: string) => string): ScheduleForm {
  return {
    name: data.name ?? '',
    schedule: data.schedule ?? '',
    type: data.type ?? 'command',
    command: data.command ?? '',
    script: decodeScript(data.script ?? ''),
    // An empty interpreter is valid and means the client's default; keep it
    // rather than replacing it with a guess.
    interpreter: data.interpreter ?? '',
    cwd: data.cwd ?? '',
    is_sudo: !!data.is_sudo,
    timeout_sec: data.timeout_sec ?? 60,
    client_ids: data.client_ids ?? [],
    group_ids: data.group_ids ?? [],
    tags: (data.tags?.tags ?? []).join(', '),
    tags_operator: String(data.tags?.operator ?? '').toUpperCase() === 'AND' ? 'AND' : 'OR',
    execute_concurrently: !!data.execute_concurrently,
    abort_on_error: data.abort_on_error ?? true,
    overlaps: !!data.overlaps
  };
}

/**
 * The POST/PUT body for a schedule. An update replaces the whole record, so
 * it starts from the stored schedule: anything the form does not edit is sent
 * back unchanged instead of being reset to its zero value.
 *
 * Targeting is exclusive on the server: a body that carries tags must not
 * carry client_ids or group_ids at all, not even as empty lists.
 */
export function scheduleBody(original: Record<string, any> | null, f: ScheduleForm): Record<string, unknown> {
  const body: Record<string, unknown> = { ...(original ?? {}) };
  for (const k of SERVER_OWNED) delete body[k];
  Object.assign(body, {
    name: f.name.trim(),
    schedule: f.schedule.trim(),
    type: f.type,
    timeout_sec: Number(f.timeout_sec),
    cwd: f.cwd.trim(),
    is_sudo: !!f.is_sudo,
    execute_concurrently: !!f.execute_concurrently,
    abort_on_error: !!f.abort_on_error,
    overlaps: !!f.overlaps
  });
  if (f.type === 'command') {
    body.command = f.command;
    delete body.script;
    // A command only accepts the server's short list of command
    // interpreters; a script's interpreter path would be rejected.
    if (original?.type !== 'command') delete body.interpreter;
  } else {
    // The endpoint base64-decodes this and runs the decoded bytes.
    body.script = utf8ToBase64(f.script);
    body.interpreter = f.interpreter.trim();
    delete body.command;
  }
  const tags = parseList(f.tags);
  if (tags.length) {
    body.tags = { tags, operator: f.tags_operator === 'AND' ? 'AND' : 'OR' };
    delete body.client_ids;
    delete body.group_ids;
  } else {
    body.client_ids = f.client_ids;
    body.group_ids = f.group_ids;
    body.tags = null;
  }
  return body;
}

export type LastRun = { when: string | null; label: string; tone: 'good' | 'bad' | 'warn' | 'muted' };

/**
 * Outcome of a schedule's most recent run. The server reports a status only
 * for single-client runs; for a run on several clients it is worked out from
 * how many of them succeeded.
 */
export function lastRun(e: ScheduleExecution | null | undefined): LastRun {
  if (!e || !e.started_at) return { when: null, label: 'never run', tone: 'muted' };
  const when = e.started_at;
  if (e.status === 'successful') return { when, label: 'successful', tone: 'good' };
  if (e.status === 'failed') return { when, label: 'failed', tone: 'bad' };
  const total = e.client_count ?? 0;
  const ok = e.success_count ?? 0;
  if (total > 0) {
    if (ok >= total) return { when, label: `successful (${ok}/${total})`, tone: 'good' };
    if (ok === 0) return { when, label: `failed (0/${total})`, tone: 'bad' };
    return { when, label: `partial (${ok}/${total})`, tone: 'warn' };
  }
  return { when, label: e.status || 'running', tone: 'muted' };
}

export type ClientTargeting = { id: string; groupIds: Set<string>; tags: string[] };

/**
 * How a schedule reaches a given client: directly by id, through a client
 * group it belongs to, or through its tags (exact match, all tags for AND and
 * any tag otherwise, as the server matches them). '' when it does not.
 */
export function scheduleReach(s: Schedule, c: ClientTargeting): string {
  if (s.client_ids?.includes(c.id)) return 'direct';
  const g = (s.group_ids ?? []).find((id) => c.groupIds.has(id));
  if (g) return `group ${g}`;
  const want = s.tags?.tags ?? [];
  if (want.length) {
    const has = (t: string) => c.tags.includes(t);
    const and = String(s.tags?.operator ?? '').toUpperCase() === 'AND';
    if (and ? want.every(has) : want.some(has)) return 'tags';
  }
  return '';
}
