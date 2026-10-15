// Parent-facing copy for the T1.4 codes. Callers branch on the code; this module never repeats it, a server sentence, or a stack.

export type Notice = { situation: string; next: string };

const budgetByPool: Record<string, string> = {
  daily_checks: 'Today’s allowance of fresh checks is used up.',
  token_factory: 'The research budget is used up.',
  tavily: 'The budget for searching sources is used up.',
};

const fallback: Notice = {
  situation: 'This check did not finish.',
  next: 'Go back to the ask page and try again.',
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
        next: 'Open an example card, or come back later.',
      };
    }
    case 'no_sources':
      return {
        situation: 'We couldn’t find writing about this title.',
        next: 'Go back to the ask page and try the full name.',
      };
    case 'check_failed':
      return {
        situation: 'The check stopped before it had an answer.',
        next: 'Start it again from the ask page.',
      };
    case 'live_checks_off':
      return {
        situation: 'Fresh checks are paused right now.',
        next: 'Open an example card while you wait.',
      };
    case 'busy':
      return {
        situation: 'Other checks are still in progress.',
        next: wait(seconds, 'try this title again.'),
      };
    case 'rate_limited':
      return {
        situation: 'Too many checks were asked for in a short time.',
        next: wait(seconds, 'try again.'),
      };
    case 'passcode_required':
      return {
        situation: 'This site asks for the shared passcode before starting a check.',
        next: 'Go back to the ask page and enter it there.',
      };
    case 'not_found':
      return {
        situation: 'Nothing matches that address.',
        next: 'Go back to the ask page and start again.',
      };
    default:
      return fallback;
  }
}

// An unknown area is unanswered. The wording must not read as a pass.
export function unknownAreaNotice(): Notice {
  return {
    situation: 'Not enough evidence was found to answer this question.',
    next: 'Read the sources on the card before you decide. This question is unanswered, not a pass.',
  };
}
