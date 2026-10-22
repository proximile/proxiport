import { describe, it, expect } from 'vitest';
import { formFromSchedule, lastRun, scheduleBody, scheduleFormError, scheduleReach, timeoutError, type ScheduleForm } from './schedules';
import { base64ToUtf8 } from './encoding';

const decode = (s: string) => base64ToUtf8(s) ?? s;

function form(over: Partial<ScheduleForm> = {}): ScheduleForm {
  return {
    name: 'nightly',
    schedule: '0 3 * * *',
    type: 'command',
    command: '/usr/bin/true',
    script: '',
    interpreter: '',
    cwd: '',
    is_sudo: false,
    timeout_sec: 30,
    client_ids: ['c1'],
    group_ids: [],
    tags: '',
    tags_operator: 'OR',
    execute_concurrently: false,
    abort_on_error: true,
    overlaps: false,
    ...over
  };
}

const stored = {
  id: 's1',
  created_at: '2026-01-01T00:00:00Z',
  created_by: 'admin',
  last_execution: { started_at: '2026-01-02T03:00:00Z', status: 'successful' },
  name: 'nightly',
  schedule: '0 3 * * *',
  type: 'command',
  command: '/usr/bin/true',
  interpreter: 'cmd',
  cwd: '/tmp',
  is_sudo: true,
  overlaps: true,
  timeout_sec: 30,
  client_ids: ['c1'],
  group_ids: [],
  tags: null,
  execute_concurrently: false,
  abort_on_error: true,
  future_field: 'kept'
};

describe('timeoutError', () => {
  it('accepts whole numbers of at least one second', () => {
    expect(timeoutError(1)).toBe('');
    expect(timeoutError('300')).toBe('');
  });

  it('rejects zero, negatives, fractions and blanks', () => {
    for (const v of [0, -5, 1.5, '', 'abc', null]) expect(timeoutError(v)).not.toBe('');
  });
});

describe('scheduleFormError', () => {
  it('requires some target', () => {
    expect(scheduleFormError(form({ client_ids: [] }))).toMatch(/target/);
  });

  it('accepts a tags-only schedule', () => {
    expect(scheduleFormError(form({ client_ids: [], tags: 'linux' }))).toBe('');
  });

  it('refuses tags together with clients', () => {
    expect(scheduleFormError(form({ tags: 'linux' }))).toMatch(/not both/);
  });

  it('checks the timeout', () => {
    expect(scheduleFormError(form({ timeout_sec: -5 }))).toMatch(/Timeout/);
  });
});

describe('scheduleBody', () => {
  it('round-trips fields the form does not edit and drops server-owned keys', () => {
    const f = formFromSchedule(stored, decode);
    const body = scheduleBody(stored, f);
    expect(body).toMatchObject({ cwd: '/tmp', is_sudo: true, overlaps: true, interpreter: 'cmd', future_field: 'kept', tags: null });
    for (const k of ['id', 'created_at', 'created_by', 'last_execution']) expect(body).not.toHaveProperty(k);
  });

  it('keeps tag targeting and leaves the id lists out entirely', () => {
    const tagged = { ...stored, client_ids: null, group_ids: null, tags: { tags: ['db', 'linux'], operator: 'AND' } };
    const f = formFromSchedule(tagged, decode);
    expect(f.tags).toBe('db, linux');
    expect(f.tags_operator).toBe('AND');
    expect(scheduleFormError(f)).toBe('');
    const body = scheduleBody(tagged, f);
    expect(body.tags).toEqual({ tags: ['db', 'linux'], operator: 'AND' });
    expect(body).not.toHaveProperty('client_ids');
    expect(body).not.toHaveProperty('group_ids');
  });

  it('base64-encodes a script body and drops the command', () => {
    const body = scheduleBody(null, form({ type: 'script', script: 'echo hi', interpreter: '/bin/sh' }));
    expect(body.script).toBe(btoa('echo hi'));
    expect(body.interpreter).toBe('/bin/sh');
    expect(body).not.toHaveProperty('command');
  });

  it('keeps an empty script interpreter empty', () => {
    const script = { ...stored, type: 'script', script: btoa('echo hi'), interpreter: '' };
    const f = formFromSchedule(script, decode);
    expect(f.script).toBe('echo hi');
    expect(scheduleBody(script, f).interpreter).toBe('');
  });

  it('drops a script interpreter when the schedule becomes a command', () => {
    const script = { ...stored, type: 'script', script: btoa('echo hi'), interpreter: '/bin/bash' };
    const body = scheduleBody(script, form({ command: 'uptime' }));
    expect(body).not.toHaveProperty('interpreter');
    expect(body).not.toHaveProperty('script');
  });
});

describe('lastRun', () => {
  it('reports a schedule that never ran', () => {
    expect(lastRun(null)).toMatchObject({ when: null, tone: 'muted' });
  });

  it('uses the status of a single-client run', () => {
    expect(lastRun({ started_at: 't', client_count: 1, success_count: 0, status: 'failed' })).toMatchObject({ label: 'failed', tone: 'bad' });
  });

  it('works out a multi-client outcome from the counts', () => {
    expect(lastRun({ started_at: 't', client_count: 3, success_count: 3, status: null }).tone).toBe('good');
    expect(lastRun({ started_at: 't', client_count: 3, success_count: 1, status: null })).toMatchObject({ label: 'partial (1/3)', tone: 'warn' });
    expect(lastRun({ started_at: 't', client_count: 3, success_count: 0, status: null }).tone).toBe('bad');
  });
});

describe('scheduleReach', () => {
  const client = { id: 'c1', groupIds: new Set(['web']), tags: ['linux', 'prod'] };

  it('matches directly, through a group, or through tags', () => {
    expect(scheduleReach({ client_ids: ['c1'] }, client)).toBe('direct');
    expect(scheduleReach({ client_ids: [], group_ids: ['web'] }, client)).toBe('group web');
    expect(scheduleReach({ tags: { tags: ['linux', 'db'], operator: 'OR' } }, client)).toBe('tags');
  });

  it('requires every tag for AND, and does not match unrelated targets', () => {
    expect(scheduleReach({ tags: { tags: ['linux', 'db'], operator: 'AND' } }, client)).toBe('');
    expect(scheduleReach({ client_ids: ['c2'], group_ids: ['db'] }, client)).toBe('');
  });
});
