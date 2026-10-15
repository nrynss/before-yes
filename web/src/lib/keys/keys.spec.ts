import { test, expect, type Page, type ConsoleMessage } from '@playwright/test';
import { build } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(dir, '../../..');
const tokenKey = 'tfk/PASTE+7f3a9c1e5bONLYTOKEN';
const tavilyKey = 'tvly/PASTE+2d8e4b6a0cONLYTAVILY';
const cardId = `c_${'0123456789abcdef'.repeat(2)}`;
const checkId = `j_${'fedcba9876543210'.repeat(2)}`;
const checks = '**/api/checks';

test.describe.configure({ timeout: 120_000 });

test.beforeAll(async () => {
  const theme = readFileSync(resolve(webRoot, 'src/lib/theme/theme.css'), 'utf8');
  mkdirSync(resolve(webRoot, 'build'), { recursive: true });
  writeFileSync(resolve(webRoot, 'build/keys-harness.js'), await bundle());
  writeFileSync(resolve(webRoot, 'build/keys-harness.html'), `<!doctype html>
<html lang="en"><head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><title>Visitor keys</title><style>${theme}</style></head>
<body><script src="/keys-harness.js"></script></body></html>`);
});

test.beforeEach(async ({ context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
});

async function bundle(): Promise<string> {
  const result = await build({
    configFile: false,
    root: webRoot,
    publicDir: false,
    logLevel: 'error',
    mode: 'production',
    plugins: svelte({ configFile: false, emitCss: false, compilerOptions: { css: 'injected' } }),
    resolve: {
      alias: [
        { find: /^\$app\/navigation$/, replacement: resolve(dir, 'shims/navigation.ts') },
        { find: /^\$app\/paths$/, replacement: resolve(dir, 'shims/paths.ts') },
      ],
    },
    build: {
      write: false,
      copyPublicDir: false,
      sourcemap: false,
      cssCodeSplit: false,
      rolldownOptions: { input: resolve(dir, 'harness.ts'), output: { format: 'iife' } },
    },
  });
  const bundles = Array.isArray(result) ? result : [result];
  for (const item of bundles) {
    if (!item || typeof item !== 'object' || !('output' in item) || !Array.isArray(item.output)) continue;
    for (const chunk of item.output) {
      if (isEntry(chunk)) return chunk.code;
    }
  }
  throw new Error('keys harness bundle was empty');
}

function isEntry(chunk: unknown): chunk is { type: 'chunk'; isEntry: boolean; code: string } {
  return typeof chunk === 'object' && chunk !== null && 'type' in chunk && chunk.type === 'chunk' && 'code' in chunk && typeof chunk.code === 'string' && 'isEntry' in chunk && chunk.isEntry === true;
}

function address(title: string, age: number, client = false): string {
  const params = new URLSearchParams({ title, age: String(age) });
  if (client) params.set('client', 'passed');
  return `/keys-harness.html?${params.toString()}`;
}

async function paste(page: Page, label: string, value: string) {
  const field = page.getByLabel(label);
  await field.click();
  await page.keyboard.press('Control+A');
  await page.evaluate(async (text) => { await navigator.clipboard.writeText(text); }, value);
  await page.keyboard.press('Control+V');
  await expect(field).toHaveValue(value);
}

async function open(page: Page, title = 'Minecraft', age = 12, client = false) {
  const logs: string[] = [];
  page.on('console', (msg: ConsoleMessage) => logs.push(msg.text()));
  page.on('pageerror', (error) => logs.push(error.message));
  await page.addInitScript(() => {
    const marks: string[] = [];
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (name: string, value: string) {
      marks.push(`${name}=${value}`);
      return original.call(this, name, value);
    };
    const host = Object.getPrototypeOf(document) as Document;
    const desc = Object.getOwnPropertyDescriptor(host, 'cookie') ?? Object.getOwnPropertyDescriptor(Document.prototype, 'cookie');
    if (desc?.get && desc.set) {
      const read = desc.get;
      const write = desc.set;
      Object.defineProperty(document, 'cookie', {
        configurable: true,
        get() { return read.call(document); },
        set(value: string) { marks.push(`cookie=${value}`); write.call(document, value); },
      });
    }
    const push = history.pushState.bind(history);
    const replace = history.replaceState.bind(history);
    history.pushState = (data, unused, url) => { marks.push(`push=${String(url ?? '')}`); return push(data, unused, url); };
    history.replaceState = (data, unused, url) => { marks.push(`replace=${String(url ?? '')}`); return replace(data, unused, url); };
    (window as unknown as { marks: string[] }).marks = marks;
  });
  await page.goto(address(title, age, client));
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByRole('dialog')).toContainText(`For ${title}, age ${age}.`);
  await expect(page.getByRole('dialog')).toContainText('Your keys aren’t stored.');
  await expect(page.getByRole('dialog').getByLabel('App, game or channel')).toHaveCount(0);
  await expect(page.getByRole('dialog').getByLabel('Child’s age')).toHaveCount(0);
  return logs;
}

async function snapshot(page: Page): Promise<string> {
  return page.evaluate(() => JSON.stringify({
    marks: (window as unknown as { marks?: string[] }).marks ?? [],
    local: { ...localStorage },
    session: { ...sessionStorage },
    cookie: document.cookie,
    href: location.href,
    text: document.body.innerText,
  }));
}

function expectNoKey(haystack: string, keys: string[] = [tokenKey, tavilyKey]) {
  for (const key of keys) expect(haystack).not.toContain(key);
}

type Posted = { url: string; headers: Record<string, string>; body: string };

async function capture(page: Page, answer: { status?: number; body: unknown }, hold = false) {
  let release: () => void = () => {};
  const gate = hold ? new Promise<void>((done) => { release = done; }) : Promise.resolve();
  const seen: Posted[] = [];
  await page.route(checks, async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    if (route.request().method() !== 'POST' || pathname !== '/api/checks') { await route.continue(); return; }
    seen.push({ url: route.request().url(), headers: route.request().headers(), body: route.request().postData() ?? '' });
    await gate;
    await route.fulfill({ status: answer.status ?? 200, contentType: 'application/json', body: JSON.stringify(answer.body) });
  });
  return { seen, release };
}

async function fieldValues(page: Page): Promise<string> {
  return page.locator('input, textarea').evaluateAll((els) => els.map((el) => (el as HTMLInputElement).value).join('\n'));
}

test('pasted keys post both headers with the title and age and open the card', async ({ page }) => {
  const { seen, release } = await capture(page, { status: 200, body: { card_id: cardId } }, true);
  await open(page, 'Minecraft', 12);
  await paste(page, 'Token Factory key', tokenKey);
  await paste(page, 'Tavily key', tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect.poll(() => seen.length).toBe(1);
  await page.keyboard.press('Enter');
  await page.waitForTimeout(150);
  expect(seen).toHaveLength(1);
  expect(JSON.parse(seen[0].body)).toEqual({ title: 'Minecraft', age: 12 });
  expect(seen[0].headers['x-token-factory-key']).toBe(tokenKey);
  expect(seen[0].headers['x-tavily-key']).toBe(tavilyKey);
  expect(seen[0].headers['content-type']).toContain('application/json');
  expect(seen[0].headers['x-test-client']).toBeUndefined();
  expect(seen[0].url).not.toContain(tokenKey);
  expect(seen[0].url).not.toContain(tavilyKey);
  expect(seen[0].body).not.toContain(tokenKey);
  expect(seen[0].body).not.toContain(tavilyKey);
  release();
  await expect(page).toHaveURL(new RegExp(`/card/${cardId}$`));
  expect(await fieldValues(page)).not.toContain(tokenKey);
});

test('a check id opens the check', async ({ page }) => {
  const { seen } = await capture(page, { status: 202, body: { check_id: checkId } });
  await open(page, 'Khan Academy Kids', 5);
  await paste(page, 'Token Factory key', tokenKey);
  await paste(page, 'Tavily key', tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(page).toHaveURL(new RegExp(`/check/${checkId}$`));
  expect(JSON.parse(seen[0].body)).toEqual({ title: 'Khan Academy Kids', age: 5 });
  expect(seen[0].headers['x-token-factory-key']).toBe(tokenKey);
  expect(seen[0].headers['x-tavily-key']).toBe(tavilyKey);
});

test('storage, cookies, the URL, and the console do not contain either key', async ({ page }) => {
  const traffic: string[] = [];
  page.on('request', (request) => traffic.push(request.url(), request.postData() ?? ''));
  const { seen, release } = await capture(page, { status: 200, body: { card_id: cardId } }, true);
  const logs = await open(page);
  await paste(page, 'Token Factory key', tokenKey);
  await paste(page, 'Tavily key', tavilyKey);
  expectNoKey(await snapshot(page), [tokenKey, tavilyKey]);
  expectNoKey(page.url());
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect.poll(() => seen.length).toBe(1);
  const during = [await snapshot(page), page.url(), logs.join('\n'), traffic.join('\n')].join('\n');
  expectNoKey(during);
  expect(seen[0].headers['x-token-factory-key']).toBe(tokenKey);
  expect(seen[0].headers['x-tavily-key']).toBe(tavilyKey);
  release();
  await expect(page).toHaveURL(new RegExp(`/card/${cardId}$`));
  expectNoKey([await snapshot(page), page.url(), logs.join('\n'), await fieldValues(page)].join('\n'));
});

test('one empty key is not sent and the refusal does not echo it', async ({ page }) => {
  const traffic: string[] = [];
  page.on('request', (request) => { if (request.method() === 'POST') traffic.push(request.url(), request.postData() ?? '', JSON.stringify(request.headers())); });
  const logs = await open(page);
  await paste(page, 'Token Factory key', tokenKey);
  await page.getByLabel('Tavily key').fill('   ');
  await page.getByRole('button', { name: 'Run this check' }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toContainText('One on its own is not sent');
  await expect(alert).not.toContainText(tokenKey);
  await expect(alert).not.toContainText('ONLYTOKEN');
  expect(traffic).toEqual([]);
  expectNoKey([await snapshot(page), page.url(), logs.join('\n'), await alert.innerText()].join('\n'));
  await page.getByLabel('Token Factory key').fill('');
  await paste(page, 'Tavily key', tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(alert).not.toContainText(tavilyKey);
  expect(traffic).toEqual([]);
});

test('a key with a space, a non-printable byte, or more than 512 bytes is not sent and is not echoed', async ({ page }) => {
  const traffic: string[] = [];
  page.on('request', (request) => { if (request.method() === 'POST') traffic.push(request.url(), JSON.stringify(request.headers()), request.postData() ?? ''); });
  const logs = await open(page);
  const spaced = `${tokenKey} LEAKED`;
  await paste(page, 'Token Factory key', spaced);
  await paste(page, 'Tavily key', tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toContainText('Nothing was sent');
  await expect(alert).not.toContainText(tokenKey);
  await expect(alert).not.toContainText(tavilyKey);
  await expect(alert).not.toContainText('LEAKED');
  expect(traffic).toEqual([]);

  const control = `${tokenKey}\u0001`;
  await page.getByLabel('Token Factory key').fill(control);
  await expect(page.getByLabel('Token Factory key')).toHaveValue(control);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(alert).not.toContainText(tokenKey);
  expect(traffic).toEqual([]);

  const over = 'Z'.repeat(513);
  await page.getByLabel('Token Factory key').fill(over);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(alert).not.toContainText('ZZZZZZZZ');
  await expect(alert).not.toContainText(tavilyKey);
  expect(traffic).toEqual([]);
  expectNoKey([await snapshot(page), logs.join('\n'), page.url()].join('\n'), [tokenKey, tavilyKey, 'LEAKED']);

  const exact = 'Y'.repeat(512);
  await page.getByLabel('Token Factory key').fill(exact);
  const { seen } = await capture(page, { status: 202, body: { check_id: checkId } });
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(page).toHaveURL(new RegExp(`/check/${checkId}$`));
  expect(seen[0].headers['x-token-factory-key']).toBe(exact);
  expect(seen[0].headers['x-tavily-key']).toBe(tavilyKey);
});

test('a bad id is not opened, even when the id is a key', async ({ page }) => {
  const logs = await open(page);
  const bodies: unknown[] = [
    { card_id: `c_${'A'.repeat(32)}` },
    { card_id: `c_${'0'.repeat(31)}` },
    { card_id: `c_${'0'.repeat(33)}` },
    { card_id: `j_${'0'.repeat(32)}` },
    { check_id: `c_${'0'.repeat(32)}` },
    { card_id: cardId, check_id: checkId },
    { card_id: tokenKey },
    { check_id: tavilyKey },
    { card_id: `${cardId}\n` },
    {},
    null,
  ];
  for (const body of bodies) {
    await page.unroute(checks);
    await capture(page, { status: 200, body });
    await paste(page, 'Token Factory key', tokenKey);
    await paste(page, 'Tavily key', tavilyKey);
    await page.getByRole('button', { name: 'Run this check' }).click();
    const alert = page.getByRole('alert');
    await expect(alert).toContainText('didn’t name a check or a card');
    await expect(alert).not.toContainText(tokenKey);
    await expect(alert).not.toContainText(tavilyKey);
    await expect(page).toHaveURL(/\/keys-harness\.html/);
    expectNoKey([page.url(), logs.join('\n'), await alert.innerText(), await snapshot(page)].join('\n'));
  }
});

test('a server message that echoes a key is not shown', async ({ page }) => {
  const logs = await open(page);
  await capture(page, { status: 400, body: { error: { code: 'invalid_request', message: `rejected ${tokenKey} and ${tavilyKey}` } } });
  await paste(page, 'Token Factory key', tokenKey);
  await paste(page, 'Tavily key', tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  const alert = page.getByRole('alert');
  await expect(alert).toContainText('Those keys were refused');
  await expect(alert).not.toContainText(tokenKey);
  await expect(alert).not.toContainText('rejected');
  await expect(page.locator('body')).not.toContainText(tokenKey);
  expectNoKey([page.url(), logs.join('\n'), await snapshot(page)].join('\n'));
  await expect(page).toHaveURL(/\/keys-harness\.html/);
});

test('a passed api client is the one that posts', async ({ page }) => {
  const { seen } = await capture(page, { status: 200, body: { card_id: cardId } });
  await open(page, 'Roblox', 9, true);
  await paste(page, 'Token Factory key', tokenKey);
  await paste(page, 'Tavily key', tavilyKey);
  await page.getByRole('button', { name: 'Run this check' }).click();
  await expect(page).toHaveURL(new RegExp(`/card/${cardId}$`));
  expect(seen[0].headers['x-test-client']).toBe('passed');
  expect(seen[0].headers['x-token-factory-key']).toBe(tokenKey);
  expect(seen[0].headers['x-tavily-key']).toBe(tavilyKey);
  expect(JSON.parse(seen[0].body)).toEqual({ title: 'Roblox', age: 9 });
});

test('cancel clears the fields and leaves nothing stored', async ({ page }) => {
  const { seen, release } = await capture(page, { status: 200, body: { card_id: cardId } }, true);
  const logs = await open(page);
  await paste(page, 'Token Factory key', tokenKey);
  await paste(page, 'Tavily key', tavilyKey);
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toBeHidden();
  expect(await fieldValues(page)).not.toContain(tokenKey);
  expect(await fieldValues(page)).not.toContain(tavilyKey);
  expectNoKey([await snapshot(page), page.url(), logs.join('\n')].join('\n'));
  expect(seen).toHaveLength(0);
  release();
});

test('dialog fits a 360px screen and passes the Chaaya accessibility gate', async ({ page }) => {
  await open(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  expect(await page.getByRole('button', { name: 'Run this check' }).evaluate((node) => node.getBoundingClientRect().height)).toBeGreaterThanOrEqual(48);
  expect(await page.getByRole('button', { name: 'Cancel' }).evaluate((node) => node.getBoundingClientRect().height)).toBeGreaterThanOrEqual(48);
  const gate = readFileSync(resolve(webRoot, 'node_modules/@nrynss/chaaya/dist/testing/a11y.js'), 'utf8').replace('export async function', 'async function').replace('(await import("axe-core")).default', 'window.axe');
  await page.addScriptTag({ path: resolve(webRoot, 'node_modules/axe-core/axe.min.js') });
  await page.addScriptTag({ content: `${gate}\nwindow.runKeysGate = () => a11yGate(document.body);` });
  await page.evaluate(() => (window as unknown as { runKeysGate: () => Promise<void> }).runKeysGate());
});
