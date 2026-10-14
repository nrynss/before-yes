import { ApiError } from '@nrynss/chaaya/keel';

export function refusal(error: unknown): string {
  if (!(error instanceof ApiError)) return 'We couldn’t read the answer. Please try again, or open an example below.';
  const seconds = error.detail.retry_after_seconds ?? error.retryAfterSeconds;
  const wait = typeof seconds === 'number' && Number.isFinite(seconds) && seconds > 0 ? ` Wait ${Math.ceil(seconds)} seconds, then try again.` : ' Please wait a moment, then try again.';
  switch (error.code) {
    case 'invalid_request': return 'Please check the title and enter a whole age from 3 to 17, then try again.';
    case 'budget_exhausted': return 'The budget for fresh checks is used up. Open a recent example below, or come back later.';
    case 'live_checks_off': return 'Fresh checks are paused. You can still open the free example cards below.';
    case 'busy': return 'Other checks are running.' + wait;
    case 'rate_limited': return 'You’ve asked for several checks.' + wait;
    case 'network': case 'timeout': return 'We couldn’t reach Before Yes. Check your connection and try again.';
    default: return 'Before Yes couldn’t start this check. Please try again, or open an example below.';
  }
}
