import { test, expect, type Page } from '@playwright/test';
import { readFileSync, readdirSync } from 'node:fs';
import { contrastGate, type ContrastPair } from '@nrynss/chaaya/testing';
import { areas, verdicts, levels, sourceLabel, type Card } from './contract';
const fixtures = readdirSync('../testdata/contracts').filter(n => /^card-.*\.json$/.test(n)).map(name => ({ name, card: JSON.parse(readFileSync(`../testdata/contracts/${name}`, 'utf8')) as Card }));
const no = fixtures.find(f => f.card.verdict === 'no')!.card;
const yes = fixtures.find(f => f.card.verdict === 'yes')!.card;
const settings = fixtures.find(f => f.card.verdict === 'yes_with_settings')!.card;
const gate = readFileSync('node_modules/@nrynss/chaaya/dist/testing/a11y.js', 'utf8').replace('export async function', 'async function').replace('(await import("axe-core")).default', 'window.axe');
async function mock(page: Page, card: Card) { await page.route(`**/api/cards/${card.id}`, route => route.fulfill({ json: card })); }
async function open(page: Page, card: Card) { await mock(page, card); await page.goto(`/card/${card.id}`); await expect(page.getByRole('heading', { level: 1 })).toHaveText(verdicts[card.verdict]); }
async function fit(page: Page) {
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 expect(await page.locator('main *').evaluateAll(elements => elements.filter(el => { const s = getComputedStyle(el); return ['hidden', 'clip'].includes(s.overflowX) && el.scrollWidth > el.clientWidth + 1; }).length)).toBe(0);
}
async function accessibility(page: Page) { await page.addScriptTag({path: 'node_modules/axe-core/axe.min.js'}); await page.addScriptTag({content: gate + '\nwindow.runCardGate = () => a11yGate(document.body);'}); await page.evaluate(() => (window as unknown as {runCardGate: () => Promise<void>}).runCardGate()); }
test('all card drawn contrast pairs pass the Chaaya gate in both modes', () => {
 const pairs: ContrastPair[] = ['ground', 'surface', 'raised'].flatMap(bg => ['text', 'dim', 'accent', 'ok', 'warn', 'stop'].map(fg => [fg, bg] as ContrastPair));
 pairs.push(['text', 'stop-soft'], ['dim', 'stop-soft'], ['accent', 'stop-soft']);
 contrastGate(readFileSync('src/lib/theme/theme.css', 'utf8'), pairs);
});
for (const { name, card } of fixtures) for (const mode of ['light', 'dark']) for (const width of [360, 1440]) {
 test(`${name} ${mode} ${width}: exact contract, accessible, no clipping`, async ({page}) => {
  await page.setViewportSize({width,height:900}); await page.addInitScript(mode => localStorage.setItem('chaaya-theme', mode), mode);
  const errors: string[] = []; page.on('pageerror', e => errors.push(e.message)); await open(page, card);
  await expect(page.locator('.title')).toHaveText(card.title); await expect(page.locator('.metadata')).toContainText(`For age ${card.age}`); await expect(page.locator('time')).toHaveAttribute('datetime', card.checked_at); await expect(page.locator('time')).toContainText('October 13, 2026');
  await expect(page.locator('[data-area] h3')).toHaveText(areas.map(a => a[1]));
  for (const [area] of areas) {
   const grade = card.grades.find(g => g.area === area)!; const row = page.locator(`[data-area="${area}"]`);
   const explanation = grade.reason?.trim() ? grade.reason : grade.level === 'unknown' ? 'We don’t have enough evidence to assess this area.' : 'The supplied evidence supports this grade; no further explanation was provided.';
   await expect(row.locator('.grade')).toHaveText(levels[grade.level]); await expect(row).toContainText(explanation);
   if (grade.level === 'unknown' && !grade.reason?.trim()) await expect(row).toContainText('Don’t assume it is safe.');
   if (grade.level !== 'flag' && grade.findings.length) await row.locator('summary').click();
   await expect(row.locator('figure')).toHaveCount(grade.findings.length);
   for (const id of grade.findings) { const finding = card.findings.find(f => f.id === id)!; const quote = row.locator(`[data-finding="${id}"]`); await expect(quote.locator('blockquote')).toHaveText(finding.quote); await expect(quote.locator('a')).toHaveAttribute('href', finding.source.url); }
  }
  const value = (label: string) => page.locator('dl > div').filter({has:page.locator('dt', {hasText:new RegExp(`^${label}$`)})}).locator('dd');
  await expect(value('Title type')).toHaveText(card.kind_label); await expect(value('AI processing cost')).toHaveText('$0.0254 USD'); await expect(value('Search credits used')).toHaveText('8'); await expect(value('Sources read')).toHaveText('9'); await expect(value('Reviews read')).toContainText(String(card.stats.reviews));
  await expect(value('Quotes that couldn’t be verified')).toHaveText('4'); await expect(value('Findings without a source quote')).toHaveText('2'); await expect(value('Other excluded findings')).toHaveText('1 unrecognised checklist area · 0 unrecognised finding category · 3 duplicates');
  await expect(value('Checklist version')).toHaveText(card.checklist_version); await expect(value('AI model used to read sources')).toHaveText(card.stats.reader_model); await expect(value('AI model used to assess findings')).toHaveText(card.stats.judge_model);
  if (card.verdict === 'no') {
   await expect(page.locator('.lead blockquote')).toHaveText(no.findings[0].quote); await expect(page.locator('.lead a')).toHaveText('Review 355'); await expect(page.locator('.lead h2')).toHaveText('Why this is a concern');
   expect(await page.locator('.lead').evaluate(el => el.compareDocumentPosition(document.querySelector('.areas')!) & Node.DOCUMENT_POSITION_FOLLOWING)).toBeTruthy();
   expect(await page.locator('.lead blockquote').evaluate(el => parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThanOrEqual(22);
   await expect(page.getByRole('link', {name:'View check'})).toHaveAttribute('href', `/card/${no.alternatives[0].card_id}`); await expect(page.getByRole('link', {name:'Compare titles'})).toHaveAttribute('href', `/compare/${no.id}/${no.alternatives[0].card_id}`); await expect(page.locator('.alternative-note')).toHaveText('More alternatives can be checked when the daily limit resets.');
   await expect(page.locator('[data-area="contact"] figure')).toHaveCount(3);
  }
  if (card.gate?.length) {
   const items = value('Rule checks').locator('li'); await expect(items).toHaveCount(card.gate.length);
   for (const [i, action] of card.gate.entries()) {
    await expect(items.nth(i)).toContainText(areas.find(a => a[0] === action.area)![1]);
    if (action.kind === 'info') { await expect(items.nth(i)).toContainText(action.note); await expect(items.nth(i)).not.toContainText('Checked by rule:'); }
    else { await expect(items.nth(i)).toContainText('Checked by rule:'); await expect(items.nth(i)).toContainText(action.note.charAt(0).toLowerCase() + action.note.slice(1)); }
   }
  } else await expect(page.locator('dl dt', { hasText: /^Rule checks$/ })).toHaveCount(0);
  const h1size = await page.locator('h1').evaluate(el => parseFloat(getComputedStyle(el).fontSize)); expect(h1size).toBeGreaterThanOrEqual(44); expect(h1size).toBeLessThanOrEqual(64);
  await fit(page); await accessibility(page); expect(errors).toEqual([]); await page.screenshot({path:`/tmp/card-${name}-${mode}-${width}.png`,fullPage:true});
 });
}
test('settings work by keyboard without changing verdict or storage; canonical copy and clipboard fallback', async ({page}) => {
 await page.addInitScript(() => { Object.defineProperty(navigator, 'clipboard', {configurable:true,value:{writeText: async (url: string) => { (window as unknown as {copied:string}).copied = url; }}}); });
 await mock(page, settings); await page.goto(`/card/${settings.id}?private=omit#fragment`); await expect(page.locator('h1')).toHaveText(verdicts[settings.verdict]);
 const before = await page.evaluate(() => JSON.stringify(localStorage)); const checkbox = page.getByRole('checkbox'); await checkbox.focus(); await page.keyboard.press('Space'); await expect(checkbox).toBeChecked(); await expect(page.locator('h1')).toHaveText(verdicts[settings.verdict]);
 await page.getByRole('button', {name:'Copy card link'}).click(); await expect(page.getByText('Link copied.', {exact:true})).toBeVisible(); const url = `http://127.0.0.1:4173/card/${settings.id}`;
 expect(await page.evaluate(() => (window as unknown as {copied:string}).copied)).toBe(url); await expect(page.getByLabel('Card link')).toHaveValue(url);
 await page.evaluate(() => { Object.defineProperty(navigator, 'clipboard', {value:undefined, configurable:true}); }); await page.getByRole('button', {name:'Copy card link'}).click(); await expect(page.getByText('Copy the address below to share this card.')).toBeVisible(); expect(await page.evaluate(() => JSON.stringify(localStorage))).toBe(before);
 expect(await page.getByRole('button', {name:'Copy card link'}).evaluate(el => el.getBoundingClientRect().height)).toBeGreaterThanOrEqual(48);
});
test('rule checks say what the code changed, in plain words', async ({page}) => {
 const card = structuredClone(yes); const data = card.grades.find(g => g.area === 'data')!; const label = card.findings.find(f => data.findings.includes(f.id) && f.source.kind === 'store')!;
 card.gate = [
  { area: 'data', rule: 'data.flag_without_basis', from: 'flag', to: 'unknown', findings: [], note: 'None of the quotes behind this concern describes data being sold, tracked for ads, shared for marketing or breached.' },
  { area: 'data', rule: 'data.label_not_linked', from: 'unknown', to: 'ok', findings: [label.id], note: 'The App Store privacy label says the developer reports no data linked to the user.' },
  { area: 'money', rule: 'money.flag_capped_by_control', from: 'flag', to: 'ok_with_setting', findings: card.grades.find(g => g.area === 'money')!.findings, note: 'A source names a control that answers this concern (Ask to Buy).' }
 ];
 await open(page, card); const items = page.locator('dl > div').filter({has: page.locator('dt', {hasText: /^Rule checks$/})}).locator('li'); await expect(items).toHaveCount(3);
 await expect(items.nth(0)).toHaveText('Privacy and data – Changed from Concern to Not enough evidence. Checked by rule: none of the quotes behind this concern describes data being sold, tracked for ads, shared for marketing or breached.');
 await expect(items.nth(1)).toHaveText('Privacy and data – Changed from Not enough evidence to OK. Checked by rule: the App Store privacy label says the developer reports no data linked to the user.');
 await expect(items.nth(2)).toContainText('Changed from Concern to Needs a setting change. Checked by rule: a source names a control');
 await expect(page.locator('[data-area="data"] .grade')).toHaveText(levels[data.level]);
});
test('an information rule check shows the fact as plain information, with no change wording', async ({page}) => {
 const card = structuredClone(no); const news = card.findings.find(f => ['news', 'article'].includes(f.source.kind))!;
 card.gate = [{ area: news.area, rule: 'information.regulator_action_other_product', kind: 'info', from: 'flag', to: 'flag', findings: [news.id], note: 'Microsoft will pay a fine of $20 million to settle Federal Trade Commission charges — a different product, so this is shown as information and is not evidence about Minecraft.' }];
 await open(page, card);
 const item = page.locator('dl > div').filter({has: page.locator('dt', {hasText: /^Rule checks$/})}).locator('li');
 await expect(item).toHaveCount(1);
 await expect(item).toContainText(areas.find(([id]) => id === news.area)![1]);
 await expect(item).toContainText('a different product, so this is shown as information');
 await expect(item).not.toContainText('Checked by rule:');
 await expect(item).not.toContainText('Stays');
 await expect(item).not.toContainText('Changed from');
});
test('a card without rule checks, or with a null list, still opens; a rule check citing a missing finding fails closed', async ({page}) => {
 const old = structuredClone(yes); delete old.gate; await open(page, old); await expect(page.locator('dl dt', { hasText: /^Rule checks$/ })).toHaveCount(0);
 const nul = structuredClone(yes) as unknown as Record<string, unknown>; nul.gate = null; await page.route(`**/api/cards/${yes.id}`, route => route.fulfill({json: nul})); await page.goto(`/card/${yes.id}`); await expect(page.locator('h1')).toHaveText(verdicts[yes.verdict]);
 const bad = structuredClone(yes); bad.gate = [{ area: 'data', rule: 'data.label_not_linked', from: 'unknown', to: 'ok', findings: ['f_missing'], note: 'x' }];
 await page.route(`**/api/cards/${yes.id}`, route => route.fulfill({json: bad})); await page.goto(`/card/${yes.id}`); await expect(page.locator('h1')).toHaveText('The card couldn’t be opened.');
});
test('pending and refused alternatives stay hidden, server verdict stays authoritative, uncited risks stay hidden', async ({page}) => {
 const card = structuredClone(no); card.verdict = 'unclear'; card.alternatives.push({title:'Pending secret title',status:'pending',reason:'',card_id:'',verdict:'no'}, {title:'Refused secret title',status:'refused',reason:'',card_id:'',verdict:'no'}); card.findings.push({...card.findings[0], id:'f_uncited', quote:'Uncited sentence must not appear.'}); await open(page,card);
 await expect(page.locator('body')).not.toContainText('secret title'); await expect(page.locator('body')).not.toContainText('Uncited sentence'); await expect(page.locator('.lead')).toHaveCount(0);
});
for (const failure of ['404', 'network', 'malformed']) test(`${failure} loads safe error and retry recovers`, async ({page}) => {
 let first = true; await page.route(`**/api/cards/${no.id}`, async route => { if (!first) return route.fulfill({json:no}); first = false; if (failure === 'network') return route.abort(); if (failure === '404') return route.fulfill({status:404,json:{error:{code:'not_found',message:'PRIVATE SERVER MESSAGE'}}}); return route.fulfill({json:{id:no.id}}); });
 await page.goto(`/card/${no.id}`); await expect(page.locator('h1')).toHaveText(failure === '404' ? 'Sorry, we couldn’t find this check.' : 'The card couldn’t be opened.'); if (failure === '404') await expect(page.getByRole('alert')).toHaveText('Check the link or return home to start again.'); await expect(page.locator('body')).not.toContainText('PRIVATE SERVER MESSAGE'); await page.getByRole('button', {name:'Try again'}).click(); await expect(page.locator('h1')).toHaveText('No');
});
for (const mutation of ['unsafe source', 'missing finding', 'wrong area', 'bad id', 'route mismatch', 'bad rule kind']) test(`fails closed on ${mutation}`, async ({page}) => {
 const card = structuredClone(no); if (mutation === 'unsafe source') card.findings[0].source.url = 'javascript:alert(1)'; if (mutation === 'missing finding') card.findings.shift(); if (mutation === 'wrong area') card.findings[0].area = 'money'; if (mutation === 'bad id') card.id = 'abcdef1234'; if (mutation === 'route mismatch') card.id = yes.id; if (mutation === 'bad rule kind') (card.gate![0] as unknown as Record<string, unknown>).kind = 'changes';
 await page.route(`**/api/cards/${no.id}`, route => route.fulfill({json:card})); await page.goto(`/card/${no.id}`); await expect(page.locator('h1')).toHaveText('The card couldn’t be opened.'); await expect(page.locator('figure')).toHaveCount(0);
});
test('unsafe route id requests nothing and XSS quote is plain exact text', async ({page}) => {
 let calls = 0; await page.route('**/api/cards/*', route => {calls++;return route.fulfill({json:no});});
 for (const id of ['c_1', 'c_7Hq2mXv9pK3wLr8sTn4bYe', `c_${'a'.repeat(22)}`, `c_${'a'.repeat(31)}`, `c_${'a'.repeat(33)}`, `c_${'a'.repeat(32)}\n`, `c_${'a'.repeat(31)}g`, `c_${'A'.repeat(32)}`, 'abcdef1234']) {
  await page.goto(`/card/${encodeURIComponent(id)}`); await expect(page.locator('h1')).toHaveText('Sorry, we couldn’t find this check.'); await expect(page.getByRole('alert')).toHaveText('Check the link or return home to start again.');
 }
 expect(calls).toBe(0); await page.unroute('**/api/cards/*');
 const card = structuredClone(no); card.findings[0].quote = '<img src=x onerror="window.pwned=true">'; await open(page,card); await expect(page.locator('.lead blockquote')).toHaveText(card.findings[0].quote); await expect(page.locator('.lead img')).toHaveCount(0); expect(await page.evaluate(() => 'pwned' in window)).toBe(false);
});
test('client route change aborts stale request and never shows stale evidence', async ({page}) => {
 let aborted = false; page.on('requestfailed', request => { if (request.url().endsWith(`/api/cards/${no.id}`)) aborted = true; });
 let release!: () => void; let requested!: () => void; const started = new Promise<void>(r => requested = r); const held = new Promise<void>(r => release = r);
 await page.route(`**/api/cards/${no.id}`, async route => {requested();await held;await route.fulfill({json:no}).catch(() => {});}); await mock(page,yes);
 await page.goto(`/card/${no.id}`); await started; await expect(page.locator('h1')).toHaveText('Reading your card…');
 // Follow an actual SvelteKit link to another card while the first GET is held.
 await page.evaluate(id => {const link=document.createElement('a');link.href=`/card/${id}`;link.textContent='Go to second card';document.querySelector('main')!.append(link);},yes.id);
 await page.getByRole('link', {name:'Go to second card'}).click(); await expect(page.locator('h1')).toHaveText('Yes'); await expect.poll(() => aborted).toBe(true); release(); await expect(page.locator('.title')).toHaveText(yes.title); await expect(page.locator('body')).not.toContainText(no.findings[0].quote);
});
test('Go nil empty collections render honestly without relaxing citation integrity', async ({page}) => {
 const card = structuredClone(fixtures.find(f => f.name === 'card-unknown-kind.json')!.card) as unknown as Record<string, unknown>;
 card.settings = null; card.findings = null;
 for (const grade of card.grades as Record<string, unknown>[]) grade.findings = null;
 const stats = card.stats as Record<string, unknown>; stats.conflicted_areas = null; stats.missing_kinds = null;
 await page.route(`**/api/cards/${card.id}`, r => r.fulfill({json:card})); await page.goto(`/card/${card.id}`); await expect(page.locator('h1')).toHaveText('Not enough evidence'); await expect(page.locator('[data-area]')).toHaveCount(6); await expect(page.locator('figure')).toHaveCount(0); await expect(page.locator('body')).toContainText('kind unknown');
});
test('an unknown area with no reason shows it is unanswered, and a real reason stays', async ({page}) => {
 const card = structuredClone(no);
 const data = card.grades.find(grade => grade.area === 'data')!;
 const record = card.grades.find(grade => grade.area === 'developer_record')!;
 data.reason = '';
 record.reason = 'No news finding was verified.';
 await open(page, card);
 const blank = page.locator('[data-area="data"]');
 await expect(blank).toContainText('We don’t have enough evidence to assess this area.');
 await expect(blank).toContainText('Don’t assume it is safe.');
 await expect(blank).not.toContainText('Not enough verified evidence was found to answer this question.');
 await expect(blank).not.toContainText('OK');
 const kept = page.locator('[data-area="developer_record"]');
 await expect(kept).toContainText('No news finding was verified.');
 await expect(kept).not.toContainText('Don’t assume it is safe.');
 await expect(kept).not.toContainText('We don’t have enough evidence to assess this area.');
 data.reason = '   ';
 await page.unroute(`**/api/cards/${card.id}`);
 await open(page, card);
 await expect(page.locator('[data-area="data"]')).toContainText('Don’t assume it is safe.');
 await expect(page.locator('[data-area="developer_record"]')).toContainText('No news finding was verified.');
 await expect(page.locator('[data-area="developer_record"]')).not.toContainText('unanswered, not a pass');
});

test('a source is labelled by its kind, and a help page is named as one', () => {
 const finding = (kind: string, url = 'https://example.org/a') => ({ id: 'f', area: 'contact' as const, claim: 'c', stance: 'mitigation' as const, quote: 'q', source: {kind, url, fetched_at: '2026-10-13T18:58:00Z'} });
 expect(sourceLabel(finding('review', 'https://itunes.apple.com/x'))).toBe('Review');
 expect(sourceLabel({...finding('review'), source: {...finding('review').source, index: 12}})).toBe('Review 12');
 expect(sourceLabel(finding('policy'))).toBe('Privacy policy');
 expect(sourceLabel(finding('help', 'https://help.minecraft.net/hc/en-us/articles/4408968616077'))).toBe('Help page');
 expect(sourceLabel(finding('news', 'https://www.securitymagazine.com/articles/1'))).toBe('www.securitymagazine.com');
});
