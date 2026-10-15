import { ApiError } from '@nrynss/chaaya/keel';
import { errorNotice, type Notice } from '../errors/notice';

// The eight T1.4 codes live in errorNotice. This file only adds the refusals that module does not define.
const fromModule = new Set(['budget_exhausted', 'no_sources', 'check_failed', 'live_checks_off', 'busy', 'rate_limited', 'passcode_required', 'not_found']);

function codeOf(error: unknown): string {
  if (error instanceof ApiError) return error.code;
  if (typeof error === 'object' && error !== null && 'code' in error && typeof (error as { code: unknown }).code === 'string') return (error as { code: string }).code;
  return '';
}

export function refusal(error: unknown): Notice {
  const code = codeOf(error);
  if (fromModule.has(code)) return errorNotice(error);
  if (code === 'invalid_request') return { situation: 'The title or the age was not accepted.', next: 'Please check the title and enter a whole age from 3 to 17, then try again.' };
  if (code === 'network' || code === 'timeout') return { situation: 'We couldn’t reach Before Yes.', next: 'Check your connection and try again.' };
  return { situation: 'Before Yes couldn’t start this check.', next: 'Please try again, or open an example below.' };
}
