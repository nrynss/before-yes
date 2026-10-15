// Parent-facing copy for the T1.4 codes. Callers branch on the code; this module never repeats it, a server sentence, or a stack.

export type Notice = { situation: string; next: string };

const budgetByPool: Record<string, string> = {
  daily_checks: 'Today’s check limit has been reached.',
  token_factory: 'The research budget is used up.',
  tavily: 'The budget for searching sources is used up.',
};

const fallback: Notice = {
  situation: 'This check couldn’t finish.',
  next: 'Return home and try again.',
};

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

// A delay is included only for a finite positive number of seconds. Zero, negatives, non-numbers, and non-finite values are dropped.
function positiveSeconds(value: unknown): number | undefined {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return undefined;
  const whole = Math.ceil(value);
  return Number.isSafeInteger(whole) ? whole : undefined;
}

function wait(seconds: number | undefined, then: string): string {
  if (seconds === undefined) return `Wait a moment, then ${then}`;
  const unit = seconds === 1 ? 'second' : 'seconds';
  return `Wait ${seconds} ${unit}, then ${then}`;
}

export function errorNotice(error: unknown): Notice {
  const body = record(error) ? error : {};
  const detail = record(body.detail) ? body.detail : {};
  const seconds = positiveSeconds(detail.retry_after_seconds) ?? positiveSeconds(body.retryAfterSeconds);
  switch (body.code) {
    case 'budget_exhausted': {
      const pool = typeof detail.pool === 'string' && Object.hasOwn(budgetByPool, detail.pool) ? budgetByPool[detail.pool] : undefined;
      return {
        situation: pool ?? 'The budget for fresh checks is used up.',
        next: 'Open a previous check or come back later.',
      };
    }
    case 'no_sources':
      return {
        situation: 'Sorry, we couldn’t research this title.',
        next: 'Check the spelling or try its full name.',
      };
    case 'check_failed':
      return {
        situation: 'This check couldn’t finish.',
        next: 'Return home and try again.',
      };
    case 'live_checks_off':
      return {
        situation: 'New checks are paused.',
        next: 'You can still open previous checks.',
      };
    case 'busy':
      return {
        situation: 'Other checks are running.',
        next: wait(seconds, 'try again.'),
      };
    case 'rate_limited':
      return {
        situation: 'Too many requests.',
        next: wait(seconds, 'try again.'),
      };
    case 'passcode_required':
      return {
        situation: 'This site asks for the shared passcode before starting a check.',
        next: 'Go back to the ask page and enter it there.',
      };
    case 'not_found':
      return {
        situation: 'Sorry, we couldn’t find this check.',
        next: 'Check the link or return home to start again.',
      };
    default:
      return fallback;
  }
}

// An unknown area is unanswered. The wording must not read as a pass.
export function unknownAreaNotice(): Notice {
  return {
    situation: 'We don’t have enough evidence to assess this area.',
    next: 'Don’t assume it is safe.',
  };
}
