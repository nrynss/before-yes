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
const sentences: Record<string, string> = { invalid_request: 'Please check the title', budget_exhausted: 'budget for fresh checks', rate_limited: 'Wait 30 seconds', busy: 'Wait 20 seconds', live_checks_off: 'free example cards below' };
for (const fixture of fixtures) {
  test(`exact T1.4 response: ${fixture.name}`, async ({ page }) => {
    await examples(page);
    const requests: unknown[] = [];
    await page.route('**/api/checks', async route => { requests.push(route.request().postDataJSON()); await route.fulfill({ status: fixture.response.status, json: fixture.response.body }); });
    await page.goto('/');
    await fill(page, `  ${fixture.request.body.title || 'Roblox'}  `, String(fixture.request.body.age > 17 ? 9 : fixture.request.body.age));
    await page.getByRole('button', { name: 'Check before yes' }).click();
    if (fixture.response.body.card_id) await expect(page).toHaveURL(`/card/${fixture.response.body.card_id}`);
    else if (fixture.response.body.check_id) await expect(page).toHaveURL(`/check/${fixture.response.body.check_id}`);
    else if (fixture.response.body.error.code === 'passcode_required') await expect(page.getByRole('dialog')).toBeVisible();
    else await expect(page.getByRole('alert')).toContainText(sentences[fixture.response.body.error.code]);
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
  for (const value of ['2', '18', '9.5', '']) { await fill(page, 'Roblox', value); await page.getByRole('button', { name: 'Check before yes' }).click(); expect(await page.getByLabel('Child’s age').evaluate((node: HTMLInputElement) => node.validity.valid)).toBe(false); }
  await fill(page, '   ', '9'); await page.getByRole('button', { name: 'Check before yes' }).click();
  await expect(page.getByRole('alert')).toContainText('Enter a title'); expect(posts).toBe(0);
});
test('pending request disables duplicate submits and fields', async ({ page }) => {
  await examples(page); let posts = 0; let release!: () => void;
  const wait = new Promise<void>(resolve => { release = resolve; });
  await page.route('**/api/checks', async route => { posts++; await wait; await route.fulfill({ status: 202, json: fixtures.find(f => f.response.status === 202).response.body }); });
  await page.goto('/'); await fill(page); await page.getByRole('button', { name: 'Check before yes' }).click();
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
  await page.goto('/'); await fill(page, '  Roblox  '); await page.getByRole('button', { name: 'Check before yes' }).click();
  await expect(page.getByLabel('Passcode', { exact: true })).toBeFocused();
  for (let i = 0; i < 5; i++) { await page.keyboard.press('Tab'); expect(await page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true); }
  await page.getByLabel('Passcode', { exact: true }).focus(); await page.keyboard.type('wrong-secret'); await page.keyboard.press('Enter');
  await expect(page.getByRole('alert')).toContainText('wasn’t accepted'); expect(requests).toHaveLength(2);
  await expect(page.getByLabel('Passcode', { exact: true })).toHaveValue('');
  await page.keyboard.type('correct-secret'); await page.keyboard.press('Enter'); await expect(page).toHaveURL(/\/card\/c_/);
  expect(requests).toHaveLength(3); expect(requests.every(r => JSON.stringify(r.body) === JSON.stringify({ title: 'Roblox', age: 9 }))).toBe(true);
  expect(await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage }, cookie: document.cookie }))).not.toContain('secret');
  expect(page.url()).not.toContain('secret');
});
test('cancel clears passcode and restores focus; reopen starts blank', async ({ page }) => {
  await examples(page); const refusal = fixtures.find(f => f.response.status === 403).response;
  await page.route('**/api/checks', route => route.fulfill({ status: refusal.status, json: refusal.body }));
  await page.goto('/'); await fill(page); await page.getByRole('button', { name: 'Check before yes' }).click();
  await page.getByLabel('Passcode', { exact: true }).fill('abandoned'); await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Check before yes' })).toBeFocused();
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
  await page.goto('/'); await expect(page.getByRole('status')).toContainText('Finding recent'); release(); await expect(page.getByText('No example cards are here yet.', { exact: false })).toBeVisible();
  await page.unroute('**/api/cards');
  for (const response of [{ status: 500, body: 'stack SECRET' }, { status: 200, body: '{' }, { status: 200, json: { cards: [{ id: 'javascript:bad' }] } }, { status: 200, json: { cards: [recent.cards[0], recent.cards[0]] } }]) {
    await page.route('**/api/cards', route => route.fulfill(response)); await page.reload(); await expect(page.getByText('Recent examples couldn’t be loaded.', { exact: false })).toBeVisible(); await expect(page.getByRole('button', { name: 'Check before yes' })).toBeEnabled(); await page.unroute('**/api/cards');
  }
});
test('network, malformed success and server text are safe and actionable', async ({ page }) => {
  await examples(page);
  for (const response of ['network', 'malformed', 'unsafe', 'ambiguous']) {
    await page.route('**/api/checks', route => response === 'network' ? route.abort() : response === 'malformed' ? route.fulfill({ body: '<html>SECRET stack</html>' }) : response === 'ambiguous' ? route.fulfill({ json: { card_id: 'c_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', check_id: 'j_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' } }) : route.fulfill({ status: 500, json: { error: { code: 'internal_error', message: 'SECRET stack trace' } } }));
    await page.goto('/'); await fill(page); await page.getByRole('button', { name: 'Check before yes' }).click(); await expect(page.getByRole('alert')).toContainText(/try again/i); await expect(page.locator('body')).not.toContainText('SECRET'); await expect(page).toHaveURL('/'); await page.unroute('**/api/checks');
  }
});
for (const mode of ['light', 'dark']) test(`ask and passcode dialog at 360px: Chaaya gate in ${mode}`, async ({ page }) => {
  await page.addInitScript(mode => localStorage.setItem('chaaya-theme', mode), mode); await examples(page);
  const refusal = fixtures.find(f => f.response.status === 403).response;
  await page.route('**/api/checks', route => route.fulfill({ status: refusal.status, json: refusal.body }));
  await page.goto('/'); await expect(page.getByText('Not enough evidence', { exact: true })).toBeVisible(); await gate(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.getByRole('button', { name: 'Check before yes' }).evaluate(node => node.getBoundingClientRect().bottom <= innerHeight)).toBe(true);
  await page.screenshot({ path: `/tmp/t5.2-ask-${mode}.png`, fullPage: true });
  await fill(page); await page.getByRole('button', { name: 'Check before yes' }).click(); await expect(page.getByRole('dialog')).toBeVisible(); await gate(page);
  expect(await page.getByRole('dialog').evaluate(node => node.getBoundingClientRect().right <= innerWidth && node.getBoundingClientRect().left >= 0)).toBe(true);
  expect(await page.getByRole('button', { name: 'Cancel' }).evaluate(node => node.getBoundingClientRect().height)).toBeGreaterThanOrEqual(48);
  await page.screenshot({ path: `/tmp/t5.2-dialog-${mode}.png`, fullPage: true });
});
test('desktop form and examples fit the editorial shell', async ({ page }) => {
  await examples(page); await page.setViewportSize({ width: 1440, height: 1000 }); await page.goto('/'); await expect(page.getByText('Not enough evidence', { exact: true })).toBeVisible(); await gate(page); expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true); await page.screenshot({ path: '/tmp/t5.2-desktop.png', fullPage: true });
});
