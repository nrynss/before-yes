import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { parseCard, verdicts, type Card } from './contract';

// The three demo cards under testdata/demo-cards are the stored form with an
// empty id. The store gives each its public id when it is seeded, so this
// spec gives each one a fixed public id of the same shape and opens it the
// way the app opens a stored card.
const demo = [
 ['roblox', 'c_6f1b0a9e2d3c4b5a8e7f6d5c4b3a2910', 'no'],
 ['minecraft', 'c_0a1b2c3d4e5f60718293a4b5c6d7e8f9', 'yes_with_settings'],
 ['khan-academy-kids', 'c_9d8c7b6a5f4e3d2c1b0a9f8e7d6c5b4a', 'yes'],
] as const;

const gate = readFileSync('node_modules/@nrynss/chaaya/dist/testing/a11y.js', 'utf8').replace('export async function', 'async function').replace('(await import("axe-core")).default', 'window.axe');

for (const [slug, id, verdict] of demo) for (const mode of ['light', 'dark']) for (const width of [360, 1440]) {
 test(`demo card ${slug} ${mode} ${width}: parses, renders, fits, accessible`, async ({ page }) => {
  const card = { ...JSON.parse(readFileSync(`../testdata/demo-cards/${slug}.json`, 'utf8')), id } as Card;
  expect(parseCard(card, id).verdict).toBe(verdict);
  await page.setViewportSize({ width, height: 900 });
  await page.addInitScript(mode => localStorage.setItem('chaaya-theme', mode), mode);
  const errors: string[] = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route(`**/api/cards/${id}`, route => route.fulfill({ json: card }));
  await page.goto(`/card/${id}`);
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(verdicts[verdict]);
  await expect(page.locator('.title')).toHaveText(card.title);
  await expect(page.locator('[data-area] h3')).toHaveCount(6);
  if (card.gate?.length) {
   const items = page.locator('dl > div').filter({ has: page.locator('dt', { hasText: /^Rule checks$/ }) }).locator('li');
   await expect(items).toHaveCount(card.gate.length);
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.addScriptTag({ path: 'node_modules/axe-core/axe.min.js' });
  await page.addScriptTag({ content: gate + '\nwindow.runCardGate = () => a11yGate(document.body);' });
  await page.evaluate(() => (window as unknown as { runCardGate: () => Promise<void> }).runCardGate());
  expect(errors).toEqual([]);
 });
}
