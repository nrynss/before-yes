import { test, expect, type Page } from '@playwright/test';
import { readFileSync, readdirSync } from 'node:fs';
const directory = '../testdata/contracts/api/';
const fixtures = readdirSync(directory).filter(name => name.startsWith('post-checks-')).map(name => ({ name, ...JSON.parse(readFileSync(directory + name, 'utf8')) }));
const recent = JSON.parse(readFileSync(directory + 'get-cards.json', 'utf8')).response.body;
const a11y = readFileSync('node_modules/@nrynss/chaaya/dist/testing/a11y.js', 'utf8').replace('export async function', 'async function').replace('(await import("axe-core")).default', 'window.axe');
async function examples(page: Page, body = recent) { await page.route('**/api/cards', route => route.fulfill({ json: body })); }
async function fill(page: Page, title = 'Roblox', age = '9') { await page.getByLabel('App, game or channel').fill(title); await page.getByLabel('Child’s age').fill(age); }
async function gate(page: Page) {
  await page.addScriptTag({ path: 'node_modules/axe-core/axe.min.js' });
  await page.addScriptTag({ content: a11y + '\nwindow.runAskGate = () => a11yGate(document.body);' });
  await page.evaluate(() => (window as unknown as { runAskGate: () => Promise<void> }).runAskGate());
}
const sentences: Record<string, string> = { invalid_request: 'Please check the title', rate_limited: 'Wait 30 seconds', busy: 'Wait 20 seconds', live_checks_off: 'New checks are paused.' };
for (const fixture of fixtures) {
  test(`exact T1.4 response: ${fixture.name}`, async ({ page }) => {
    await examples(page);
    const requests: unknown[] = [];
    await page.route('**/api/checks', async route => { requests.push(route.request().postDataJSON()); await route.fulfill({ status: fixture.response.status, json: fixture.response.body }); });
    await page.goto('/');
    await fill(page, `  ${fixture.request.body.title || 'Roblox'}  `, String(fixture.request.body.age > 17 ? 9 : fixture.request.body.age));
    await page.getByRole('button', { name: 'Check this title' }).click();
    const code = fixture.response.body.error?.code as string | undefined;
    if (fixture.response.body.card_id) await expect(page).toHaveURL(`/card/${fixture.response.body.card_id}`);
    else if (fixture.response.body.check_id) {
      await expect(page).toHaveURL(`/check/${fixture.response.body.check_id}`);
      await expect(page).toHaveTitle(`Before Yes - Check in progress for ${fixture.request.body.title || 'Roblox'}`);
    }
    else if (code === 'passcode_required') {
      await expect(page.getByRole('dialog')).toBeVisible();
      await expect(page.getByRole('dialog')).toContainText('This demo requires a passcode to start a check.');
      await expect(page.getByRole('dialog')).toContainText('Enter the passcode');
    } else if (code === 'budget_exhausted') {
      const situation = 'Today’s check limit has been reached.';
      await expect(page.locator('form .notice')).toContainText(situation);
      await expect(page.locator('form .notice')).toContainText('Open a previous check or come back later.');
      const shown = page.getByRole('dialog');
      await expect(shown).toBeVisible();
      await expect(shown).toContainText('We’ve reached today’s limit for checks.');
      await expect(shown).toContainText('Your keys aren’t stored.');
      await expect(page.getByRole('dialog')).toContainText(`For ${fixture.request.body.title}, age ${fixture.request.body.age}.`);
      await expect(page.getByLabel('App, game or channel')).toHaveValue(`  ${fixture.request.body.title}  `);
      await expect(page.getByLabel('Child’s age')).toHaveValue(String(fixture.request.body.age));
      expect(await shown.evaluate(el => {
        const rect = el.getBoundingClientRect();
        if (rect.width < 1 || rect.height < 1) return false;
        const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + Math.min(rect.height / 2, 10));
        return !!hit && (el === hit || el.contains(hit));
      })).toBe(true);
      await expect(page.locator('body')).not.toContainText('budget_exhausted');
      await expect(page.locator('body')).not.toContainText("today's budget");
    } else {
      await expect(page.getByRole('alert')).toContainText(sentences[code ?? '']);
      if (code === 'live_checks_off') await expect(page.getByRole('dialog')).toHaveCount(0);
    }
    expect(requests).toEqual([{ title: fixture.request.body.title || 'Roblox', age: fixture.request.body.age > 17 ? 9 : fixture.request.body.age }]);
  });
}
test('keyboard-only submission trims title and sends integer age', async ({ page }) => {
  await examples(page);
  let payload: unknown;
  await page.route('**/api/checks', route => { payload = route.request().postDataJSON(); return route.fulfill({ status: 202, json: fixtures.find(f => f.response.status === 202).response.body }); });
  await page.goto('/');
  await expect(page.getByText('Not enough evidence', { exact: true })).toBeVisible();
  for (let i = 0; i < 8 && !(await page.getByLabel('App, game or channel').evaluate(node => node === document.activeElement)); i++) await page.keyboard.press('Tab');
  await expect(page.getByLabel('App, game or channel')).toBeFocused();
  await page.keyboard.type('  Minecraft  ');
  await page.keyboard.press('Tab'); await page.keyboard.type('9');
  await page.keyboard.press('Tab'); await page.keyboard.press('Enter');
  await expect(page).toHaveURL(/\/check\/j_/);
  expect(payload).toEqual({ title: 'Minecraft', age: 9 });
});
test('invalid ages and blank title never start a check', async ({ page }) => {
  await examples(page); let posts = 0;
  await page.route('**/api/checks', route => { posts++; return route.fulfill({ json: {} }); });
  await page.goto('/');
  for (const value of ['2', '18', '9.5', '']) { await fill(page, 'Roblox', value); await page.getByRole('button', { name: 'Check this title' }).click(); expect(await page.getByLabel('Child’s age').evaluate((node: HTMLInputElement) => node.validity.valid)).toBe(false); }
  await fill(page, '   ', '9'); await page.getByRole('button', { name: 'Check this title' }).click();
  await expect(page.getByRole('alert')).toContainText('Enter a title'); expect(posts).toBe(0);
});
test('pending request disables duplicate submits and fields', async ({ page }) => {
  await examples(page); let posts = 0; let release!: () => void;
  const wait = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/checks', async route => { posts++; await wait; await route.fulfill({ status: 202, json: fixtures.find(f => f.response.status === 202).response.body }); });
  await page.goto('/'); await fill(page); await page.getByRole('button', { name: 'Check this title' }).click();
  await expect(page.getByRole('button', { name: 'Starting your check' })).toBeDisabled();
  await expect(page.getByLabel('App, game or channel')).toBeDisabled();
  await page.keyboard.press('Enter'); expect(posts).toBe(1); release(); await expect(page).toHaveURL(/\/check\/j_/);
});
test('passcode traps focus, rejects wrong code, retries saved question once, leaves no stored secret', async ({ page }) => {
  await examples(page); const requests: { body: unknown; passcode?: string }[] = [];
  const refusal = fixtures.find(f => f.response.status === 403).response;
  await page.route('**/api/checks', route => {
    const passcode = route.request().headers()['x-passcode']; requests.push({ body: route.request().postDataJSON(), passcode });
    return route.fulfill(passcode === 'correct-secret' ? { json: fixtures.find(f => f.response.status === 200).response.body } : { status: refusal.status, json: refusal.body });
  });
  await page.goto('/'); await fill(page, '  Roblox  '); await page.getByRole('button', { name: 'Check this title' }).click();
  await expect(page.getByLabel('Passcode', { exact: true })).toBeFocused();
  for (let i = 0; i < 5; i++) { await page.keyboard.press('Tab'); expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true); }
  await page.getByLabel('Passcode', { exact: true }).focus(); await page.keyboard.type('wrong-secret'); await page.keyboard.press('Enter');
  await expect(page.getByRole('alert').filter({ hasText: 'wasn’t accepted' })).toBeVisible(); expect(requests).toHaveLength(2);
  await expect(page.getByLabel('Passcode', { exact: true })).toHaveValue('');
  await page.keyboard.type('correct-secret'); await page.keyboard.press('Enter'); await expect(page).toHaveURL(/\/card\/c_/);
  expect(requests).toHaveLength(3); expect(requests.every(r => JSON.stringify(r.body) === JSON.stringify({ title: 'Roblox', age: 9 }))).toBe(true);
  expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage }, cookie: document.cookie }))).not.toContain('secret');
  expect(page.url()).not.toContain('secret');
});
test('cancel clears passcode and restores focus; reopen starts blank', async ({ page }) => {
  await examples(page); const refusal = fixtures.find(f => f.response.status === 403).response;
  await page.route('**/api/checks', route => route.fulfill({ status: refusal.status, json: refusal.body }));
  await page.goto('/'); await fill(page); await page.getByRole('button', { name: 'Check this title' }).click();
  await page.getByLabel('Passcode', { exact: true }).fill('abandoned'); await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Check this title' })).toBeFocused();
  await page.keyboard.press('Enter'); await expect(page.getByLabel('Passcode', { exact: true })).toHaveValue('');
});
test('examples show all verdicts and follow a free link with no POST', async ({ page }) => {
  await examples(page); let posts = 0; page.on('request', request => { if (request.method() === 'POST') posts++; });
  await page.goto('/'); await expect(page.getByText('Not enough evidence', { exact: true })).toBeVisible();
  for (const item of recent.cards) await expect(page.getByRole('link', { name: new RegExp(item.title) })).toHaveAttribute('href', `/card/${item.id}`);
  await page.getByRole('link', { name: /Khan Academy Kids/ }).click(); await expect(page).toHaveURL(`/card/${recent.cards[2].id}`); expect(posts).toBe(0);
});
test('loading, empty, failed and malformed list remain honest and form stays available', async ({ page }) => {
  let release!: () => void; const wait = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/cards', async route => { await wait; await route.fulfill({ json: { cards: [] } }); });
  await page.goto('/'); await expect(page.getByRole('status')).toContainText('Loading previous checks…'); release(); await expect(page.getByText('No checks yet.', { exact: false })).toBeVisible();
  await page.unroute('**/api/cards');
  for (const response of [{ status: 500, body: 'stack SECRET' }, { status: 200, body: '{' }, { status: 200, json: { cards: [{ id: 'javascript:bad' }] } }, { status: 200, json: { cards: [recent.cards[0], recent.cards[0]] } }]) {
    await page.route('**/api/cards', route => route.fulfill(response)); await page.reload(); await expect(page.getByText('Previous checks couldn’t be loaded.', { exact: false })).toBeVisible(); await expect(page.getByRole('button', { name: 'Check this title' })).toBeEnabled(); await page.unroute('**/api/cards');
  }
});
test('network, malformed success and server text are safe and actionable', async ({ page }) => {
  await examples(page);
  for (const response of ['network', 'malformed', 'unsafe', 'ambiguous']) {
    await page.route('**/api/checks', route => response === 'network' ? route.abort() : response === 'malformed' ? route.fulfill({ body: '<html>SECRET stack</html>' }) : response === 'ambiguous' ? route.fulfill({ json: { card_id: 'c_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', check_id: 'j_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' } }) : route.fulfill({ status: 500, json: { error: { code: 'internal_error', message: 'SECRET stack trace' } } }));
    await page.goto('/'); await fill(page); await page.getByRole('button', { name: 'Check this title' }).click(); await expect(page.getByRole('alert')).toContainText(/try again/i); await expect(page.locator('body')).not.toContainText('SECRET'); await expect(page).toHaveURL('/'); await page.unroute('**/api/checks');
  }
});
for (const mode of ['light', 'dark']) test(`ask and passcode dialog at 360px: Chaaya gate in ${mode}`, async ({ page }) => {
  await page.addInitScript(mode => localStorage.setItem('chaaya-theme', mode), mode); await examples(page);
  const refusal = fixtures.find(f => f.response.status === 403).response;
  await page.route('**/api/checks', route => route.fulfill({ status: refusal.status, json: refusal.body }));
  await page.goto('/'); await expect(page.getByText('Not enough evidence', { exact: true })).toBeVisible(); await gate(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.getByRole('button', { name: 'Check this title' }).evaluate(node => node.getBoundingClientRect().bottom <= innerHeight)).toBe(true);
  await page.screenshot({ path: `/tmp/t5.2-ask-${mode}.png`, fullPage: true });
  await fill(page); await page.getByRole('button', { name: 'Check this title' }).click(); await expect(page.getByRole('dialog')).toBeVisible(); await gate(page);
  expect(await page.getByRole('dialog').evaluate(node => node.getBoundingClientRect().right <= innerWidth && node.getBoundingClientRect().left >= 0)).toBe(true);
  expect(await page.getByRole('button', { name: 'Cancel' }).evaluate(node => node.getBoundingClientRect().height)).toBeGreaterThanOrEqual(48);
  await page.screenshot({ path: `/tmp/t5.2-dialog-${mode}.png`, fullPage: true });
});
test('desktop form and examples fit the editorial shell', async ({ page }) => {
  await examples(page); await page.setViewportSize({ width: 1440, height: 1000 }); await page.goto('/'); await expect(page.getByText('Not enough evidence', { exact: true })).toBeVisible(); await gate(page); expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true); await page.screenshot({ path: '/tmp/t5.2-desktop.png', fullPage: true });
});

const refusals: { code: string; detail?: Record<string, unknown>; situation: string; next: string; keys?: boolean; passcode?: boolean }[] = [
  { code: 'budget_exhausted', detail: { pool: 'daily_checks' }, situation: 'Today’s check limit has been reached.', next: 'Open a previous check or come back later.', keys: true },
  { code: 'budget_exhausted', detail: { pool: 'token_factory' }, situation: 'The research budget is used up.', next: 'Open a previous check or come back later.', keys: true },
  { code: 'budget_exhausted', detail: { pool: 'tavily' }, situation: 'The budget for searching sources is used up.', next: 'Open a previous check or come back later.', keys: true },
  { code: 'budget_exhausted', detail: {}, situation: 'The budget for fresh checks is used up.', next: 'Open a previous check or come back later.', keys: true },
  { code: 'no_sources', situation: 'Sorry, we couldn’t research this title.', next: 'Check the spelling or try its full name.' },
  { code: 'check_failed', situation: 'This check couldn’t finish.', next: 'Return home and try again.' },
  { code: 'live_checks_off', situation: 'New checks are paused.', next: 'You can still open previous checks.' },
  { code: 'busy', detail: { retry_after_seconds: 20 }, situation: 'Other checks are running.', next: 'Wait 20 seconds, then try again.' },
  { code: 'rate_limited', detail: { retry_after_seconds: 30 }, situation: 'Too many requests.', next: 'Wait 30 seconds, then try again.' },
  { code: 'passcode_required', situation: 'This demo requires a passcode to start a check.', next: 'Enter the passcode', passcode: true },
  { code: 'not_found', situation: 'Sorry, we couldn’t find this check.', next: 'Check the link or return home to start again.' },
];
for (const item of refusals) {
  const pool = typeof item.detail?.pool === 'string' ? item.detail.pool : '';
  test(`ask refusal ${item.code}${pool ? ` ${pool}` : ''} shows the T5.6 message and next step`, async ({ page }) => {
    await examples(page);
    const server = `SECRET ${item.code} ${pool} stack`;
    await page.route('**/api/checks', route => route.fulfill({ status: 400, json: { error: { code: item.code, message: server, stack: server, detail: item.detail } } }));
    await page.goto('/');
    await fill(page, 'Fortnite', '11');
    await page.getByRole('button', { name: 'Check this title' }).click();
    const scope = item.keys || item.passcode ? page.getByRole('dialog') : page.getByRole('alert');
    await expect(scope).toContainText(item.keys ? 'We’ve reached today’s limit for checks.' : item.situation);
    await expect(scope).toContainText(item.keys ? 'Your keys aren’t stored.' : item.next);
    if (item.keys) {
      await expect(page.locator('form .notice')).toContainText(item.situation);
      await expect(page.locator('form .notice')).toContainText(item.next);
      await expect(page.getByRole('dialog')).toContainText('For Fortnite, age 11.');
      await expect(page.getByLabel('App, game or channel')).toHaveValue('Fortnite');
      await expect(page.getByLabel('Child’s age')).toHaveValue('11');
      await expect(page.getByText('Use your own API keys')).toBeVisible();
      if (pool) await expect(scope).not.toContainText(pool);
    } else if (item.passcode) {
      await expect(page.getByText('Use your own API keys')).toHaveCount(0);
    } else {
      await expect(page.getByRole('dialog')).toHaveCount(0);
      await expect(page.getByText('Use your own API keys')).toHaveCount(0);
    }
    await expect(page.locator('body')).not.toContainText('SECRET');
    await expect(page.locator('body')).not.toContainText(item.code);
  });
}
test('invalid_request and network keep a next step and hide the server sentence', async ({ page }) => {
  await examples(page);
  await page.route('**/api/checks', route => route.fulfill({ status: 400, json: { error: { code: 'invalid_request', message: 'title is required and age must be between 3 and 17 SECRET' } } }));
  await page.goto('/');
  await fill(page);
  await page.getByRole('button', { name: 'Check this title' }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toContainText('Please check the title and enter a whole age from 3 to 17, then try again.');
  await expect(page.locator('body')).not.toContainText('title is required');
  await expect(page.locator('body')).not.toContainText('SECRET');
  await expect(page.locator('body')).not.toContainText('invalid_request');
  await page.unroute('**/api/checks');
  await page.route('**/api/checks', route => route.abort());
  await page.goto('/');
  await fill(page);
  await page.getByRole('button', { name: 'Check this title' }).click();
  await expect(page.getByRole('alert')).toContainText('We couldn’t reach Before Yes.');
  await expect(page.getByRole('alert')).toContainText('Check your connection and try again.');
});
test('example list accepts ages 3 to 17 and refuses 2 and 18', async ({ page }) => {
  const base = recent.cards[0];
  for (const [age, shown] of [[2, false], [3, true], [17, true], [18, false]] as const) {
    await page.unroute('**/api/cards').catch(() => {});
    await examples(page, { cards: [{ ...base, age }] });
    await page.goto('/');
    if (shown) await expect(page.getByRole('link', { name: /Roblox/ })).toBeVisible();
    else await expect(page.getByText('Previous checks couldn’t be loaded.', { exact: false })).toBeVisible();
  }
});
test('pasted keys on a spent cap reach progress and stay out of storage and the URL', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await examples(page);
  const tokenKey = 'tfk/PASTE+7f3a9c1e5bONLYTOKEN';
  const tavilyKey = 'tvly/PASTE+2d8e4b6a0cONLYTAVILY';
  const checkId = `j_${'a1b2c3d4'.repeat(4)}`;
  const posts: { url: string; headers: Record<string, string>; body: string }[] = [];
  await page.addInitScript(() => {
    const marks: string[] = [];
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (name: string, value: string) {
      marks.push(`${name}=${value}`);
      return original.call(this, name, value);
    };
    (window as unknown as { marks: string[] }).marks = marks;
  });
  await page.route('**/api/checks', route => {
    const request = route.request();
    posts.push({ url: request.url(), headers: request.headers(), body: request.postData() ?? '' });
    if (!request.headers()['x-token-factory-key']) return route.fulfill({ status: 402, json: { error: { code: 'budget_exhausted', message: `spent ${tokenKey}`, detail: { pool: 'daily_checks' } } } });
    return route.fulfill({ status: 202, json: { check_id: checkId } });
  });
  await page.route(`**/api/checks/${checkId}`, route => route.fulfill({ json: { id: checkId, status: 'running' } }));
  await page.route(`**/api/checks/${checkId}/events`, route => route.fulfill({ status: 200, headers: { 'content-type': 'text/event-stream' }, body: '' }));
  await page.goto('/');
  await fill(page, 'Fortnite', '11');
  await page.getByRole('button', { name: 'Check this title' }).click();
  await expect(page.getByRole('dialog')).toContainText('For Fortnite, age 11.');
  await expect(page.getByLabel('App, game or channel')).toHaveValue('Fortnite');
  const paste = async (label: string, value: string) => {
    const field = page.getByLabel(label);
    await field.click();
    await page.evaluate(async (text) => { await navigator.clipboard.writeText(text); }, value);
    await page.keyboard.press('Control+V');
    await expect(field).toHaveValue(value);
  };
  await paste('Token Factory key', tokenKey);
  await paste('Tavily key', tavilyKey);
  const leaked = () => page.evaluate(() => JSON.stringify({ marks: (window as unknown as { marks?: string[] }).marks ?? [], local: { ...localStorage }, session: { ...sessionStorage }, cookie: document.cookie, href: location.href }));
  expect(await leaked()).not.toContain(tokenKey);
  expect(await leaked()).not.toContain(tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(page).toHaveURL(new RegExp(`/check/${checkId}$`));
  expect(posts).toHaveLength(2);
  expect(posts[0].headers['x-token-factory-key']).toBeUndefined();
  expect(posts[0].headers['x-tavily-key']).toBeUndefined();
  expect(posts[1].headers['x-token-factory-key']).toBe(tokenKey);
  expect(posts[1].headers['x-tavily-key']).toBe(tavilyKey);
  expect(posts[1].url).not.toContain(tokenKey);
  expect(posts[1].url).not.toContain(tavilyKey);
  expect(posts[1].body).not.toContain(tokenKey);
  expect(posts[1].body).not.toContain(tavilyKey);
  expect(JSON.parse(posts[1].body)).toEqual({ title: 'Fortnite', age: 11 });
  const after = await leaked();
  expect(after).not.toContain(tokenKey);
  expect(after).not.toContain(tavilyKey);
  expect(page.url()).not.toContain(tokenKey);
  expect(page.url()).not.toContain(tavilyKey);
  await expect(page.locator('body')).not.toContainText(tokenKey);
  await expect(page.locator('body')).not.toContainText(tavilyKey);
});
