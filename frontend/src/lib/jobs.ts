/**
 * Helpers shared by the command and script runners: when a job is finished,
 * how its result is rendered, and how a timeout field is read.
 */
import type { Job } from './types';

/**
 * A job is finished once it has any status other than "running". The agent
 * reports "unknown" when its timeout fires while the process is still going,
 * and that is as final as "successful" or "failed": nothing more will arrive.
 */
export function isJobFinished(status?: string | null): boolean {
  return !!status && status !== 'running';
}

/** Label for a status pill; "unknown" only ever means the timeout fired. */
export function jobStatusLabel(status?: string | null): string {
  if (status === 'unknown') return 'timed out';
  return status || '—';
}

/** Text for the Error column of a job history row. */
export function jobErrorText(job: Pick<Job, 'status' | 'error' | 'timeout_sec'>): string {
  if (job.error) return job.error;
  if (job.status === 'unknown') {
    const after = job.timeout_sec ? ` after ${job.timeout_sec}s` : '';
    return `timed out${after}; the command may still be running`;
  }
  return '';
}

/** The output lines of a finished job, ending with a status line. */
export function finishedJobOutput(job: Job): string[] {
  const out: string[] = [];
  if (job.result?.stdout) out.push(job.result.stdout);
  if (job.result?.stderr) out.push(`[stderr] ${job.result.stderr}`);
  if (job.error) out.push(`[error] ${job.error}`);
  if (job.status === 'unknown') {
    const after = job.timeout_sec ? ` (${job.timeout_sec}s)` : '';
    const pid = job.pid ? ` as pid ${job.pid}` : '';
    out.push(
      `\n--- status=unknown: timeout${after} reached; the output above is what was captured so far. ` +
        `The client stopped watching the process, which may still be running${pid}. ---`
    );
  } else {
    out.push(`\n--- status=${job.status} ---`);
  }
  return out;
}

/**
 * Read a timeout field. The server decodes timeout_sec as an integer, so only
 * a whole number of seconds, 1 or more, is accepted.
 */
export function parseTimeoutSec(raw: string | number): { value?: number; error?: string } {
  const s = String(raw ?? '').trim();
  if (!/^\d+$/.test(s) || Number(s) < 1) {
    return { error: 'Timeout must be a whole number of seconds (1 or more).' };
  }
  return { value: Number(s) };
}

/**
 * How long to keep polling before giving up on a job that never reports a
 * final status (for example because its client went away mid-run).
 */
export function pollDeadlineMs(timeoutSec: number, jobsInSequence = 1): number {
  return (timeoutSec + 30) * 1000 * Math.max(1, jobsInSequence);
}

export type MultiJobSummary = {
  client_ids?: string[];
  concurrent?: boolean;
  abort_on_err?: boolean;
  jobs?: Job[] | null;
};

// The error the server records for a target that was offline. Such a failure
// never stops a sequential run, even with abort-on-error on.
const NOT_CONNECTED = 'client is not connected';

/**
 * Progress of a multi-client run.
 *
 * - "done": every target has a finished job.
 * - "aborted": a sequential run with abort-on-error had a target fail (other
 *   than by being offline) and nothing is running, so the remaining targets
 *   will never get a job.
 * - "running": anything else. In a sequential run the job for the next
 *   target only appears once the previous one has finished, so targets with
 *   no job yet are still to come, not done.
 */
export function multiJobProgress(
  summary: MultiJobSummary | null | undefined,
  targets: string[]
): 'done' | 'aborted' | 'running' {
  const ids = summary?.client_ids?.length ? summary.client_ids : targets;
  const jobs = summary?.jobs ?? [];
  if (!ids.length) return 'running';
  const byClient = new Map(jobs.map((j) => [j.client_id, j]));
  if (ids.every((id) => isJobFinished(byClient.get(id)?.status))) return 'done';
  const anyRunning = jobs.some((j) => !isJobFinished(j.status));
  if (
    summary &&
    summary.concurrent === false &&
    summary.abort_on_err &&
    !anyRunning &&
    jobs.some((j) => j.status === 'failed' && j.error !== NOT_CONNECTED)
  ) {
    return 'aborted';
  }
  return 'running';
}
