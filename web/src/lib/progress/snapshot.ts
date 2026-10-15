import { ApiError, type JobSnapshot, type JobStatus } from '@nrynss/chaaya/keel';
export const validId = (id: unknown, prefix: 'j' | 'c'): id is string => typeof id === 'string' && id.length === 34 && new RegExp(`^${prefix}_[0-9a-f]{32}$`).test(id);
const statuses = ['queued', 'running', 'done', 'error', 'cancelled', 'interrupted'];
/** The latest stage GET /api/checks/{id} reports while a check is queued or running. */
export type StageSnapshot = {stage: string; message?: string; current?: number; total?: number};
export function snapshot(body: unknown, id: string): JobSnapshot & {cardId?: string; progress?: StageSnapshot} {
 if (!body || typeof body !== 'object') throw new Error('Malformed snapshot');
 const value = body as Record<string, unknown>;
 if (value.id !== id || typeof value.status !== 'string' || !statuses.includes(value.status)) throw new Error('Malformed snapshot');
 const result: JobSnapshot & {cardId?: string; progress?: StageSnapshot} = {jobId:id, status:value.status as JobStatus};
 if ((result.status === 'running' || result.status === 'queued') && typeof value.stage === 'string' && value.stage !== '') {
  const progress: StageSnapshot = {stage:value.stage};
  if (typeof value.message === 'string' && value.message !== '') progress.message = value.message;
  if (Number.isFinite(value.current) && Number.isFinite(value.total)) {progress.current = value.current as number; progress.total = value.total as number;}
  result.progress = progress;
 }
 if (result.status === 'done') {
  if (!validId(value.card_id, 'c')) throw new Error('Malformed card id');
  result.cardId = value.card_id;
 }
 if (result.status === 'error') {
  result.error = {code:typeof value.code === 'string' ? value.code : 'job_failed', message:'', detail:value.detail};
 }
 return result;
}
export function failure(error: unknown): string {
 const code = error instanceof ApiError ? error.code : typeof error === 'object' && error !== null && 'code' in error ? error.code : '';
 const detail = typeof error === 'object' && error !== null && 'detail' in error && typeof error.detail === 'object' && error.detail !== null ? error.detail as Record<string, unknown> : {};
 switch (code) {
  case 'budget_exhausted': {
   const pool = detail.pool === 'token_factory' ? 'research budget' : detail.pool === 'tavily' ? 'source search budget' : detail.pool === 'daily_checks' ? 'daily check allowance' : 'budget for fresh checks';
   return `The ${pool} is used up. Open a recent example from the ask page, or come back later.`;
  }
  case 'no_sources': return 'We couldn’t find enough sources to read. Try a more specific title on the ask page.';
  case 'live_checks_off': return 'Fresh checks are paused. Open a free recent example from the ask page.';
  case 'busy': return 'Other checks are running. Wait a moment, then try checking the title again.';
  case 'rate_limited': return 'There have been several requests. Wait a moment, then try again.';
  case 'passcode_required': return 'This check needs the shared passcode. Return to the ask page and enter it there.';
  case 'not_found': return 'We couldn’t find this check. Return to the ask page to start a new one.';
  case 'network': case 'timeout': return 'We couldn’t reach Before Yes. Check your connection and retry following this check.';
  case 'check_failed': return 'The research couldn’t finish. Try checking the title again from the ask page.';
  default: return 'We couldn’t read this check’s result. Retry following it, or start again from the ask page.';
 }
}
