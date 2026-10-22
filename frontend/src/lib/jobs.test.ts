import { describe, it, expect } from 'vitest';

import {
  isJobFinished,
  jobStatusLabel,
  jobErrorText,
  finishedJobOutput,
  parseTimeoutSec,
  pollDeadlineMs,
  multiJobProgress
} from './jobs';
import type { Job } from './types';

function job(over: Partial<Job>): Job {
  return { jid: 'j1', client_id: 'host01', ...over };
}

describe('isJobFinished', () => {
  it('treats every status but running as final, including unknown', () => {
    expect(isJobFinished('successful')).toBe(true);
    expect(isJobFinished('failed')).toBe(true);
    expect(isJobFinished('unknown')).toBe(true);
    expect(isJobFinished('running')).toBe(false);
  });

  it('does not treat a missing status as final', () => {
    expect(isJobFinished(undefined)).toBe(false);
    expect(isJobFinished('')).toBe(false);
  });
});

describe('timed-out jobs', () => {
  it('label unknown as a timeout', () => {
    expect(jobStatusLabel('unknown')).toBe('timed out');
    expect(jobStatusLabel('failed')).toBe('failed');
    expect(jobStatusLabel(undefined)).toBe('—');
  });

  it('describe the timeout in the error column when there is no error', () => {
    expect(jobErrorText({ status: 'unknown', timeout_sec: 2 })).toMatch(/timed out after 2s/);
    expect(jobErrorText({ status: 'unknown', error: 'boom' })).toBe('boom');
    expect(jobErrorText({ status: 'successful' })).toBe('');
  });

  it('keep the partial output and end with a timeout line', () => {
    const out = finishedJobOutput(
      job({ status: 'unknown', timeout_sec: 5, pid: 4242, result: { stdout: 'partial\n', stderr: '', summary: '' } })
    );
    expect(out[0]).toBe('partial\n');
    expect(out[out.length - 1]).toMatch(/status=unknown: timeout \(5s\) reached/);
    expect(out[out.length - 1]).toMatch(/pid 4242/);
  });

  it('other statuses end with a plain status line', () => {
    const out = finishedJobOutput(job({ status: 'failed', error: 'exit 1' }));
    expect(out).toEqual(['[error] exit 1', '\n--- status=failed ---']);
  });
});

describe('parseTimeoutSec', () => {
  it('accepts whole seconds of 1 or more', () => {
    expect(parseTimeoutSec('60')).toEqual({ value: 60 });
    expect(parseTimeoutSec(' 5 ')).toEqual({ value: 5 });
  });

  it.each(['', 'abc', '-5', '0', '1.5', '1e3'])('rejects %j', (raw) => {
    const r = parseTimeoutSec(raw);
    expect(r.value).toBeUndefined();
    expect(r.error).toMatch(/whole number/);
  });
});

describe('pollDeadlineMs', () => {
  it('adds a grace period and scales with a sequence of jobs', () => {
    expect(pollDeadlineMs(10)).toBe(40_000);
    expect(pollDeadlineMs(10, 3)).toBe(120_000);
  });
});

describe('multiJobProgress', () => {
  const targets = ['host01', 'host02'];

  it('is not done before any job exists', () => {
    expect(multiJobProgress({ client_ids: targets, jobs: [] }, targets)).toBe('running');
    expect(multiJobProgress(null, targets)).toBe('running');
  });

  it('keeps going in a sequential run until every target has finished', () => {
    const summary = {
      client_ids: targets,
      concurrent: false,
      abort_on_err: false,
      jobs: [job({ client_id: 'host01', status: 'successful' })]
    };
    expect(multiJobProgress(summary, targets)).toBe('running');
    summary.jobs.push(job({ client_id: 'host02', status: 'running' }));
    expect(multiJobProgress(summary, targets)).toBe('running');
    summary.jobs[1].status = 'unknown';
    expect(multiJobProgress(summary, targets)).toBe('done');
  });

  it('reports an abort when a sequential abort-on-error run stops after a failure', () => {
    const summary = {
      client_ids: targets,
      concurrent: false,
      abort_on_err: true,
      jobs: [job({ client_id: 'host01', status: 'failed' })]
    };
    expect(multiJobProgress(summary, targets)).toBe('aborted');
  });

  it('does not report an abort for an offline target, which the server skips past', () => {
    const summary = {
      client_ids: targets,
      concurrent: false,
      abort_on_err: true,
      jobs: [job({ client_id: 'host01', status: 'failed', error: 'client is not connected' })]
    };
    expect(multiJobProgress(summary, targets)).toBe('running');
  });

  it('does not report an abort for a concurrent run', () => {
    const summary = {
      client_ids: targets,
      concurrent: true,
      abort_on_err: true,
      jobs: [job({ client_id: 'host01', status: 'failed' })]
    };
    expect(multiJobProgress(summary, targets)).toBe('running');
  });

  it('falls back to the selected targets when the summary has no client list', () => {
    const summary = { jobs: [job({ client_id: 'host01', status: 'successful' })] };
    expect(multiJobProgress(summary, ['host01'])).toBe('done');
  });
});
