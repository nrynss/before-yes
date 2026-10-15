import { test, expect, type Locator, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import { areas, verdicts, levels, type Card, type Grade } from '../card/contract';

const read = (name: string) => JSON.parse(readFileSync(`../testdata/contracts/${name}.json`, 'utf8')) as Card;
const no = read('card-no');
const other = read('card-unclear');
const yes = read('card-yes');
const settings = read('card-settings');
const gate = readFileSync('node_modules/@nrynss/chaaya/dist/testing/a11y.js', 'utf8').replace('export async function', 'async function').replace('(await import("axe-core")).default', 'window.axe');
const sentence = (grade: Grade) => grade.reason?.trim() || (grade.level === 'unknown' ? 'Not enough verified evidence was found to answer this question.' : 'The supplied evidence supports this grade; no further explanation was provided.');

async function mock(page: Page, ...cards: Card[]) {
  for (const card of cards) await page.route(`**/api/cards/${card.id}`, route => route.fulfill({ json: card }));
}
async function fit(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect(await page.locator('main *').evaluateAll(elements => elements.filter(el => { const s = getComputedStyle(el); return ['hidden', 'clip'].includes(s.overflowX) && el.scrollWidth > el.clientWidth + 1; }).length)).toBe(0);
}
async function accessibility(page: Page) {
  await page.addScriptTag({ path: 'node_modules/axe-core/axe.min.js' });
  await page.addScriptTag({ content: gate + '\nwindow.runCompareGate = () => a11yGate(document.body);' });
  await page.evaluate(() => (window as unknown as { runCompareGate: () => Promise<void> }).runCompareGate());
}
async function boxes(row: Locator) {
  return row.evaluate(element => {
    const box = (node: Element) => { const r = node.getBoundingClientRect(); return { top: r.top, right: r.right, bottom: r.bottom, left: r.left }; };
    return { name: box(element.querySelector('h2')!), cells: [...element.querySelectorAll('.compare-cell')].map(box) };
  });
}

for (const mode of ['light', 'dark']) for (const width of [360, 899, 900, 1440]) {
  test(`no card beside its alternative at ${width}px in ${mode}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.addInitScript(mode => localStorage.setItem('chaaya-theme', mode), mode);
    const errors: string[] = [];
    page.on('pageerror', error => errors.push(error.message));
    expect(no.alternatives[0].card_id).toBe(other.id);
    expect(no.alternatives[0].verdict).toBe('yes_with_settings');
    expect(other.verdict).toBe('unclear');
    const reversed = structuredClone(no);
    reversed.grades = [...no.grades].reverse();
    expect(reversed.grades[0].area).not.toBe('contact');
    await mock(page, reversed, other);
    await page.goto(`/compare/${no.id}/${other.id}`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('The same questions.');
    await expect(page.locator('h1')).toHaveCount(1);
    const titles = page.locator('.titles article');
    await expect(titles).toHaveCount(2);
    await expect(titles.nth(0).locator('h2')).toHaveText(no.title);
    await expect(titles.nth(0).locator('.verdict')).toHaveText(verdicts[no.verdict]);
    await expect(titles.nth(0)).toContainText(`For age ${no.age}`);
    await expect(titles.nth(0).getByRole('link')).toHaveAttribute('href', `/card/${no.id}`);
    await expect(titles.nth(1).locator('h2')).toHaveText(other.title);
    await expect(titles.nth(1).locator('.verdict')).toHaveText(verdicts[other.verdict]);
    await expect(page.locator('body')).not.toContainText('Yes, with these settings');
    await expect(page.locator('.compare-row > h2')).toHaveText(areas.map(([, label]) => label));
    const titleLeft = await titles.nth(0).locator('h2').evaluate(element => element.getBoundingClientRect().left);
    const firstCellLeft = await page.locator('[data-area="contact"] [data-side="a"]').evaluate(element => element.getBoundingClientRect().left);
    expect(Math.abs(titleLeft - firstCellLeft)).toBeLessThan(2);
    for (const [area] of areas) {
      const row = page.locator(`[data-area="${area}"]`);
      const cells = row.locator('.compare-cell');
      await expect(cells).toHaveCount(2);
      await expect(row.locator('h3')).toHaveText([no.title, other.title]);
      for (const [index, card] of [no, other].entries()) {
        const grade = card.grades.find(item => item.area === area)!;
        const cell = cells.nth(index);
        await expect(cell.locator('.grade')).toHaveText(levels[grade.level]);
        await expect(cell.locator('.grade')).toHaveAttribute('data-level', grade.level);
        await expect(cell.locator('.reason')).toHaveText(sentence(grade));
        if (grade.setting) await expect(cell.locator('.setting')).toHaveText(`Setting: ${grade.setting}`);
        else await expect(cell.locator('.setting')).toHaveCount(0);
      }
      const { name, cells: cellBoxes } = await boxes(row);
      expect(Math.abs(cellBoxes[0].top - cellBoxes[1].top)).toBeLessThan(2);
      expect(cellBoxes[1].left - cellBoxes[0].right).toBeGreaterThan(4);
      if (width < 900) {
        expect(cellBoxes[0].top - name.bottom).toBeGreaterThan(4);
        expect(name.right).toBeGreaterThan(cellBoxes[1].left);
      } else {
        expect(cellBoxes[0].left - name.right).toBeGreaterThan(4);
        expect(name.top).toBeLessThan(cellBoxes[0].bottom);
        expect(name.bottom).toBeGreaterThan(cellBoxes[0].top);
        expect(Math.abs(name.top - cellBoxes[0].top)).toBeLessThan(2);
      }
    }
    await expect(page.locator('[data-area="age_gating"] [data-side="b"] .reason')).toHaveText('Not enough verified evidence was found to answer this question.');
    await expect(page.locator('[data-area="money"] [data-side="b"] .setting')).toHaveText('Setting: Ask to Buy (Family Sharing)');
    await expect(page.locator('[data-area="contact"] [data-side="a"] .grade')).toHaveAttribute('data-level', 'flag');
    await fit(page);
    await accessibility(page);
    expect(errors).toEqual([]);
    await page.screenshot({ path: `/tmp/compare-no-alternative-${mode}-${width}.png`, fullPage: true });
  });
}

test('invalid addresses request nothing and say what to do next', async ({ page }) => {
  const calls: string[] = [];
  await page.route('**/api/cards/**', route => { calls.push(new URL(route.request().url()).pathname); return route.fulfill({ json: no }); });
  const pairs = [
    ['first-arbitrary', 'second-arbitrary'],
    ['c_7Hq2mXv9pK3wLr8sTn4bYe', no.id],
    [`c_${'A'.repeat(32)}`, `c_${'a'.repeat(32)}`],
    [`c_${'a'.repeat(31)}`, `c_${'a'.repeat(33)}`],
    [no.id.slice(0, -1) + 'g', no.id.toUpperCase()],
  ];
  for (const [leftID, rightID] of pairs) {
    await page.goto(`/compare/${encodeURIComponent(leftID)}/${encodeURIComponent(rightID)}`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('The same questions.');
    await expect(page.getByRole('alert')).toContainText('Open a card and choose Compare both');
    await expect(page.locator('.compare-row')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Try again' })).toHaveCount(0);
  }
  expect(calls).toEqual([]);
});

test('a missing card hides the server message and retry recovers', async ({ page }) => {
  let first = true;
  await page.route(`**/api/cards/${no.id}`, route => route.fulfill({ json: no }));
  await page.route(`**/api/cards/${other.id}`, route => {
    if (!first) return route.fulfill({ json: other });
    first = false;
    return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: 'PRIVATE SERVER MESSAGE' } } });
  });
  await page.goto(`/compare/${no.id}/${other.id}`);
  await expect(page.getByRole('alert')).toContainText('was not found');
  await expect(page.locator('body')).not.toContainText('PRIVATE SERVER MESSAGE');
  await expect(page.locator('body')).not.toContainText('not_found');
  await expect(page.locator('.compare-row')).toHaveCount(0);
  await page.getByRole('button', { name: 'Try again' }).click();
  await expect(page.locator('.titles h2').nth(1)).toHaveText(other.title);
  await expect(page.locator('.titles .verdict').nth(1)).toHaveText('Not enough evidence');
});

test('a malformed card fails closed and shows no grades', async ({ page }) => {
  const broken = structuredClone(no);
  broken.findings[0].source.url = 'javascript:alert(1)';
  await mock(page, other);
  await page.route(`**/api/cards/${no.id}`, route => route.fulfill({ json: broken }));
  await page.goto(`/compare/${no.id}/${other.id}`);
  await expect(page.getByRole('alert')).toContainText('Try again');
  await expect(page.locator('.grade')).toHaveCount(0);
  await expect(page.locator('body')).not.toContainText(no.findings[0].quote);
  await expect(page.locator('body')).not.toContainText('Invalid card');
  await expect(page.locator('body')).not.toContainText('javascript:');
});

test('a client route change drops the stale pair', async ({ page }) => {
  let release!: () => void;
  const held = new Promise<void>(resolveHold => { release = resolveHold; });
  let aborted = false;
  page.on('requestfailed', request => { if (request.url().endsWith(`/api/cards/${no.id}`)) aborted = true; });
  await page.route(`**/api/cards/${no.id}`, async route => { await held; await route.fulfill({ json: no }).catch(() => {}); });
  await mock(page, other, yes, settings);
  await page.goto(`/compare/${no.id}/${other.id}`);
  await expect(page.getByRole('status')).toHaveText('Opening both cards.');
  await page.evaluate(href => { const link = document.createElement('a'); link.href = href; link.textContent = 'Other pair'; document.querySelector('main')!.append(link); }, `/compare/${yes.id}/${settings.id}`);
  await page.getByRole('link', { name: 'Other pair' }).click();
  await expect(page.locator('.titles h2').nth(0)).toHaveText(yes.title);
  await expect(page.locator('.titles h2').nth(1)).toHaveText(settings.title);
  await expect.poll(() => aborted).toBe(true);
  release();
  await expect(page.locator('body')).not.toContainText(no.title);
});

test('open-card links are reachable from the keyboard', async ({ page }) => {
  await mock(page, no, other);
  await page.goto(`/compare/${no.id}/${other.id}`);
  await expect(page.locator('.titles h2').nth(0)).toHaveText(no.title);
  const link = page.getByRole('link', { name: `Open ${no.title}` });
  for (let i = 0; i < 12 && !(await link.evaluate(node => node === document.activeElement)); i++) await page.keyboard.press('Tab');
  await expect(link).toBeFocused();
  expect(await link.evaluate(element => element.getBoundingClientRect().height)).toBeGreaterThanOrEqual(48);
});
