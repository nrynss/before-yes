import { test, expect, type Page, type Locator } from '@playwright/test';
import { createServer, type ViteDevServer } from 'vite';

const codes = ['budget_exhausted', 'no_sources', 'check_failed', 'live_checks_off', 'busy', 'rate_limited', 'passcode_required', 'not_found'] as const;
const serverSentences: Record<(typeof codes)[number], string> = {
  budget_exhausted: "today's budget for fresh checks is spent",
  no_sources: 'nothing could be read about that title; check the spelling, or try its full name',
  check_failed: 'the check could not finish; try again shortly',
  live_checks_off: 'live checks are switched off; cached cards are still served',
  busy: 'too many checks are running; try again shortly',
  rate_limited: 'the route rate limit was reached',
  passcode_required: 'a shared passcode is required',
  not_found: 'no such check',
};
const expects: Record<(typeof codes)[number], { situation: string; next: string }> = {
  budget_exhausted: { situation: 'The budget for fresh checks is used up.', next: 'Open an example card, or come back later.' },
  no_sources: { situation: 'We couldn’t find writing about this title.', next: 'Go back to the ask page and try the full name.' },
  check_failed: { situation: 'The check stopped before it had an answer.', next: 'Start it again from the ask page.' },
  live_checks_off: { situation: 'Fresh checks are paused right now.', next: 'Open an example card while you wait.' },
  busy: { situation: 'Other checks are still in progress.', next: 'Wait a moment, then try this title again.' },
  rate_limited: { situation: 'Too many checks were asked for in a short time.', next: 'Wait a moment, then try again.' },
  passcode_required: { situation: 'This site asks for the shared passcode before starting a check.', next: 'Go back to the ask page and enter it there.' },
  not_found: { situation: 'Nothing matches that address.', next: 'Go back to the ask page and start again.' },
};

let vite: ViteDevServer;
test.beforeAll(async () => {
  vite = await createServer({ server: { middlewareMode: true, hmr: false }, appType: 'custom', logLevel: 'error' });
});
test.afterAll(async () => { await vite.close(); });

async function show(page: Page, props: { error?: unknown; unknownArea?: boolean }): Promise<Locator> {
  const view = await vite.ssrLoadModule('/src/lib/errors/render.ts') as { renderNotice: (props: { error?: unknown; unknownArea?: boolean }) => Promise<string> };
  const body = await view.renderNotice(props);
  await page.setContent(`<!DOCTYPE html><html><head><meta charset="utf-8"></head><body>${body}</body></html>`);
  return page.locator('.notice');
}

async function rendered(notice: Locator): Promise<string> {
  return notice.innerText();
}

for (const code of codes) {
  test(`${code} renders a next step and hides the code, the server sentence, and the stack`, async ({ page }) => {
    const stack = `Error: PRIVATE STACK TRACE\n    at Object.<anonymous> (/tmp/secret.js:1:1)\n    at Module._compile (node:internal/modules/cjs/loader:1:1)`;
    const notice = await show(page, {
      error: {
        code,
        message: `${serverSentences[code]}\n${stack}`,
        stack,
        detail: { pool: `not-a-pool ${code}`, note: serverSentences[code] },
      },
    });
    await expect(notice.getByRole('paragraph').nth(0)).toHaveText(expects[code].situation);
    await expect(notice.getByRole('paragraph').nth(1)).toHaveText(expects[code].next);
    const text = await rendered(notice);
    const html = await notice.innerHTML();
    expect(text.length).toBeGreaterThan(expects[code].situation.length);
    expect(expects[code].next.length).toBeGreaterThan(0);
    for (const secret of [code, serverSentences[code], 'PRIVATE', 'secret.js', 'Module._compile', 'not-a-pool', 'token_factory', 'daily_checks', 'tavily']) {
      expect(text).not.toContain(secret);
      expect(html).not.toContain(secret);
    }
  });
}

for (const [pool, situation] of [
  ['daily_checks', 'Today’s allowance of fresh checks is used up.'],
  ['token_factory', 'The research budget is used up.'],
  ['tavily', 'The budget for searching sources is used up.'],
] as const) {
  test(`budget_exhausted names ${pool} in parent language`, async ({ page }) => {
    const notice = await show(page, { error: { code: 'budget_exhausted', message: 'the Token Factory budget is spent', detail: { pool } } });
    await expect(notice.locator('.situation')).toHaveText(situation);
    await expect(notice.locator('.next')).toHaveText('Open an example card, or come back later.');
    const text = await rendered(notice);
    expect(text).not.toContain(pool);
    expect(text).not.toContain('budget_exhausted');
    expect(text).not.toContain('Token Factory');
    expect(text).not.toContain('Tavily');
    expect(text.toLowerCase()).not.toContain('tavily');
  });
}

for (const value of [0, -1, Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY, '20', null, undefined, true]) {
  test(`busy ignores a delay that is not a finite positive number (${String(value)})`, async ({ page }) => {
    const notice = await show(page, { error: { code: 'busy', message: 'retry in 20', retryAfterSeconds: value, detail: { retry_after_seconds: value } } });
    await expect(notice.locator('.next')).toHaveText('Wait a moment, then try this title again.');
    expect(await rendered(notice)).not.toMatch(/\d/);
  });
}

test('rate_limited includes a supplied whole-second delay', async ({ page }) => {
  const notice = await show(page, { error: { code: 'rate_limited', detail: { retry_after_seconds: 30 }, message: 'the route rate limit was reached' } });
  await expect(notice.locator('.next')).toHaveText('Wait 30 seconds, then try again.');
  expect(await rendered(notice)).not.toContain('rate_limited');
  expect(await rendered(notice)).not.toContain('the route rate limit was reached');
});

test('busy rounds a fractional delay up and prefers detail over the header field', async ({ page }) => {
  const notice = await show(page, { error: { code: 'busy', retryAfterSeconds: 99, detail: { retry_after_seconds: 20.2 } } });
  await expect(notice.locator('.next')).toHaveText('Wait 21 seconds, then try this title again.');
});

test('a one-second delay is singular, and an invalid detail falls through to the header field', async ({ page }) => {
  const notice = await show(page, { error: { code: 'rate_limited', retryAfterSeconds: 0.2, detail: { retry_after_seconds: 0 } } });
  await expect(notice.locator('.next')).toHaveText('Wait 1 second, then try again.');
});

test('an unknown area says evidence was missing and does not read as a pass', async ({ page }) => {
  const notice = await show(page, {
    unknownArea: true,
    error: { code: 'budget_exhausted', message: 'OK Yes PRIVATE STACK TRACE', detail: { pool: 'daily_checks' } },
  });
  await expect(notice).toHaveAttribute('role', 'status');
  await expect(notice.locator('.situation')).toHaveText('Not enough evidence was found to answer this question.');
  await expect(notice.locator('.next')).toHaveText('Read the sources on the card before you decide. This question is unanswered, not a pass.');
  const text = await rendered(notice);
  expect(text).not.toMatch(/\bOK\b/i);
  expect(text).not.toMatch(/\bYes\b/i);
  expect(text).not.toContain('PRIVATE');
  expect(text).not.toContain('budget_exhausted');
  expect(text).not.toContain('daily_checks');
  expect(await page.locator('body').innerText()).not.toMatch(/\bOK\b/);
  expect(await page.locator('body').innerText()).not.toMatch(/\bYes\b/);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('an unknown code, a thrown stack, and a hostile pool render only the fallback', async ({ page }) => {
  const attack = '<img src=x onerror=alert(1)> job_failed toString constructor';
  const notice = await show(page, { error: { code: 'job_failed', message: attack, stack: attack, detail: { pool: 'toString', retry_after_seconds: Number.MAX_VALUE } } });
  await expect(notice.locator('.situation')).toHaveText('This check did not finish.');
  await expect(notice.locator('.next')).toHaveText('Go back to the ask page and try again.');
  const html = await page.content();
  for (const secret of ['job_failed', 'onerror', '<img', 'toString', 'constructor', attack]) {
    expect(html).not.toContain(secret);
  }
});
