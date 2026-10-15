import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
const fixture = (name:string) => JSON.parse(readFileSync(`../testdata/contracts/api/${name}.json`,'utf8')).response.body;
const id: string = fixture('get-check-running').id;
const card: string = fixture('get-check-done').card_id;
const cardFixture = JSON.parse(readFileSync('../testdata/contracts/card-no.json','utf8'));
test.beforeEach(async ({page}) => {
 await page.route(`**/api/cards/${card}`, route => route.fulfill({json:cardFixture}));
});
const frames = readFileSync('../testdata/contracts/api/events-done.txt','utf8').split('200 text/event-stream\n\n')[1].trim().split('\n\n');
async function mock(page: Page, state = fixture('get-check-running')) {
 await page.addInitScript(({id,state}) => {
  const native = window.fetch;
  const harness = {state, requests:[] as {url:string,lastId:string|null}[], streams:[] as ReadableStreamDefaultController<Uint8Array>[], reads:0};
  (window as unknown as {research:typeof harness}).research = harness;
  window.fetch = async (input,init) => {
   const url = String(input);
   if (!url.includes(`/api/checks/${id}`)) return native(input,init);
   harness.requests.push({url,lastId:new Headers(init?.headers).get('Last-Event-ID')});
   if(url.endsWith('/events')) return new Response(new ReadableStream({start(controller){harness.streams.push(controller);}}),{headers:{'Content-Type':'text/event-stream'}});
   harness.reads++;
   return new Response(JSON.stringify(harness.state),{headers:{'Content-Type':'application/json'}});
  };
 },{id,state});
}
async function push(page: Page, frame:string, stream = 0) {
 await page.evaluate(({frame,stream}) => (window as unknown as {research:{streams:ReadableStreamDefaultController<Uint8Array>[]}}).research.streams[stream].enqueue(new TextEncoder().encode(frame+'\n\n')),{frame,stream});
}
async function state(page: Page, state:unknown) {
 await page.evaluate(state => {(window as unknown as {research:{state:unknown}}).research.state = state;},state);
}
async function opened(page: Page, count=1) {await expect.poll(() => page.evaluate(() => (window as unknown as {research:{streams:unknown[]}}).research.streams.length)).toBe(count);}
test('exact fixture Gather Read Judge done, counts reset and card route', async ({page}) => {
 await mock(page);await page.goto(`/check/${id}`);await opened(page);
 await push(page,frames[0]);await expect(page.getByRole('status')).toContainText('searching for the privacy policy');
 await push(page,frames[1]);await expect(page.getByRole('status')).toContainText('120 of 500');
 await push(page,frames[2]);await expect(page.getByRole('status')).toContainText('grading 6 areas');await expect(page.getByRole('status')).not.toContainText('120');
 await state(page,fixture('get-check-done'));await push(page,frames[3]);await expect(page).toHaveURL(`/card/${card}`);
});
test('dropped mid-read replays with Last-Event-ID and finishes', async ({page}) => {
 await mock(page);await page.goto(`/check/${id}`);await opened(page);await push(page,frames[0]);await push(page,frames[1]);
 await expect(page.getByRole('status')).toContainText('120 of 500');
 await page.evaluate(() => (window as unknown as {research:{streams:ReadableStreamDefaultController<Uint8Array>[]}}).research.streams[0].close());
 await opened(page,2);
 expect(await page.evaluate(() => (window as unknown as {research:{requests:{lastId:string|null}[]}}).research.requests.filter(r=>r.lastId).at(-1)?.lastId)).toBe('2');
 await push(page,frames[2],1);await state(page,fixture('get-check-done'));await push(page,frames[3],1);await expect(page).toHaveURL(`/card/${card}`);
});
test('page opened mid-read shows the saved stage and count at once', async ({page}) => {
 await mock(page);await page.goto(`/check/${id}`);
 const status = page.getByRole('status');
 await expect(status).toContainText('reading 14 sources');await expect(status).toContainText('3 of 9');
 await expect(page.locator('li[aria-current="step"]')).toContainText('Read');
 await opened(page);await expect(page.getByRole('status')).toContainText('3 of 9');
});
test('reconnect rereads the saved stage and count', async ({page}) => {
 await mock(page);await page.goto(`/check/${id}`);await opened(page);await push(page,frames[0]);
 await expect(page.getByRole('status')).toContainText('searching for the privacy policy');
 await state(page,{...fixture('get-check-running'),message:'reading 14 sources',current:7,total:9});
 await page.evaluate(() => (window as unknown as {research:{streams:ReadableStreamDefaultController<Uint8Array>[]}}).research.streams[0].close());
 await opened(page,2);
 await expect(page.getByRole('status')).toContainText('7 of 9');
 await push(page,frames[2],1);await expect(page.getByRole('status')).toContainText('grading 6 areas');await expect(page.getByRole('status')).not.toContainText('7 of 9');
});
const shown: Record<string, [string, string]> = {
 budget_exhausted: ['The research budget is used up.', 'Open an example card, or come back later.'],
 no_sources: ['We couldn’t find writing about this title.', 'Go back to the ask page and try the full name.'],
 check_failed: ['The check stopped before it had an answer.', 'Start it again from the ask page.'],
 live_checks_off: ['Fresh checks are paused right now.', 'Open an example card while you wait.'],
 busy: ['Other checks are still in progress.', 'Wait a moment, then try this title again.'],
 rate_limited: ['Too many checks were asked for in a short time.', 'Wait a moment, then try again.'],
 passcode_required: ['This site asks for the shared passcode before starting a check.', 'Go back to the ask page and enter it there.'],
 not_found: ['Nothing matches that address.', 'Go back to the ask page and start again.'],
 internal_error: ['This check did not finish.', 'Go back to the ask page and try again.'],
 job_failed: ['This check did not finish.', 'Go back to the ask page and try again.'],
 unknown: ['This check did not finish.', 'Go back to the ask page and try again.'],
};
for (const [code, [situation, next]] of Object.entries(shown)) {
 test(`terminal generic stream rereads specific ${code} safely`,async({page})=>{
  await mock(page);await page.goto(`/check/${id}`);await opened(page);
  await state(page,{id,status:'error',code,message:'PRIVATE STACK TRACE',detail:{pool:'token_factory'}});
  await push(page,`event: error\nid: 1\ndata: ${JSON.stringify({job_id:id,status:'error',error:{error:{code:'job_failed',message:'PRIVATE STACK TRACE'}}})}`);
  const status = page.getByRole('status');
  await expect(status).toContainText(situation);await expect(status).toContainText(next);
  await expect(status).not.toContainText('PRIVATE');await expect(status).not.toContainText(code);
 });
}
for (const [pool, situation] of [['daily_checks', 'Today’s allowance of fresh checks is used up.'], ['tavily', 'The budget for searching sources is used up.']] as const) {
 test(`budget pool ${pool} uses its T5.6 sentence`, async ({page}) => {
  await mock(page); await page.goto(`/check/${id}`); await opened(page);
  await state(page, {id, status:'error', code:'budget_exhausted', message:'PRIVATE', detail:{pool}});
  await push(page, `event: error\nid: 1\ndata: ${JSON.stringify({job_id:id,status:'error',error:{error:{code:'job_failed',message:'PRIVATE'}}})}`);
  const status = page.getByRole('status');
  await expect(status).toContainText(situation);
  await expect(status).toContainText('Open an example card, or come back later.');
  await expect(status).not.toContainText(pool);
  await expect(status).not.toContainText('PRIVATE');
  await expect(status).not.toContainText('budget_exhausted');
 });
}
test('saved done on initial load goes straight to card',async({page})=> {await mock(page,fixture('get-check-done'));await page.goto(`/check/${id}`);await expect(page).toHaveURL(`/card/${card}`);});
for(const status of ['cancelled','interrupted']) test(`${status} tells parent to restart`,async({page})=>{await mock(page,{id,status});await page.goto(`/check/${id}`);await expect(page.getByRole('status')).toContainText('start it again');});
test('malformed snapshot retry works with keyboard',async({page})=>{
 await mock(page,{id,status:'done',card_id:'../../unsafe'});await page.goto(`/check/${id}`);await expect(page.getByRole('status')).toContainText('This check did not finish.');await state(page,fixture('get-check-done'));await page.getByRole('button',{name:'Retry following'}).focus();await page.keyboard.press('Enter');await expect(page).toHaveURL(`/card/${card}`);
});
for (const status of ['done','error']) test(`count-free terminal snapshot: ${status} after counted Read reconnect`,async({page})=>{
 await mock(page);await page.goto(`/check/${id}`);await opened(page);await push(page,frames[1]);await expect(page.getByRole('status')).toContainText('120 of 500');
 await state(page,{id,status,card_id:card,code:'budget_exhausted',detail:{pool:'token_factory'}});
 await page.evaluate(() => (window as unknown as {research:{streams:ReadableStreamDefaultController<Uint8Array>[]}}).research.streams[0].close());await opened(page,2);
 if(status === 'done') {await expect(page).toHaveURL(`/card/${card}`);await expect(page.getByRole('heading',{level:1})).toHaveText('No');}else await expect(page.getByRole('status')).toContainText('The research budget is used up.');
});
for(const mode of ['light','dark']) test(`active Read at 360px ${mode} passes Chaaya accessibility`,async({page})=>{
 await mock(page);await page.addInitScript(mode=>localStorage.setItem('chaaya-theme',mode),mode);await page.goto(`/check/${id}`);await opened(page);await push(page,frames[1]);await expect(page.getByRole('status')).toContainText('120 of 500');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 const gate = readFileSync('node_modules/@nrynss/chaaya/dist/testing/a11y.js','utf8').replace('export async function','async function').replace('(await import("axe-core")).default','window.axe');
 await page.addScriptTag({path:'node_modules/axe-core/axe.min.js'});await page.addScriptTag({content:gate+'\nwindow.runChaayaGate = () => a11yGate(document.body);'});await page.evaluate(()=>(window as unknown as {runChaayaGate:()=>Promise<void>}).runChaayaGate());await page.screenshot({path:`/tmp/before-yes-progress-${mode}.png`,fullPage:true});
 await page.setViewportSize({width:1440,height:1000});await page.screenshot({path:`/tmp/before-yes-progress-desktop-${mode}.png`,fullPage:true});
});
test('alternatives is real state, omitted messages and counters do not leak',async({page})=>{
 await mock(page);await page.goto(`/check/${id}`);await opened(page);await push(page,frames[1]);await expect(page.getByRole('status')).toContainText('120 of 500');await push(page,`event: progress\nid: 3\ndata: ${JSON.stringify({job_id:id,stage:'judge'})}`);await expect(page.getByRole('status')).not.toContainText('120');await expect(page.getByRole('status')).not.toContainText('reading 500');await push(page,`event: progress\nid: 4\ndata: ${JSON.stringify({job_id:id,stage:'alternatives',detail:{message:'checking two alternatives'}})}`);await expect(page.getByRole('status')).toContainText('checking two alternatives');await expect(page.locator('.complete')).toHaveCount(3);
});
test('route unmount closes stream and prevents further requests',async({page})=>{
 await mock(page);await page.goto(`/check/${id}`);await opened(page);await push(page,frames[1]);await page.getByRole('link',{name:'Ask about a title'}).click();await expect(page).toHaveURL('/');
 const count = await page.evaluate(()=>(window as unknown as {research:{requests:unknown[]}}).research.requests.length);
 await page.waitForTimeout(1000);expect(await page.evaluate(()=>(window as unknown as {research:{requests:unknown[]}}).research.requests.length)).toBe(count);
});
for(const kind of ['404','network','malformed']) test(`${kind} load failure offers safe retry`,async({page})=>{
 let retry=false;
 await page.route(`**/api/checks/${id}`,async route=>{if(retry){await route.fulfill({json:fixture('get-check-done')});return;}if(kind==='network'){await route.abort();return;}await route.fulfill({status:kind==='404'?404:200,json:kind==='404'?fixture('get-check-404'):{id,status:'unknown',message:'PRIVATE'}});});
 await page.goto(`/check/${id}`);await expect(page.getByRole('button',{name:'Retry following'})).toBeVisible();await expect(page.getByRole('status')).not.toContainText('PRIVATE');retry=true;await page.getByRole('button',{name:'Retry following'}).click();await expect(page).toHaveURL(`/card/${card}`);
});
test('leaving during a late saved result never navigates to its card',async({page})=>{
 let release!:()=>void;const pending = new Promise<void>(resolve=>{release=resolve;});let reads=0;
 await page.route(`**/api/checks/${id}`,async route=>{reads++;await pending;await route.fulfill({json:fixture('get-check-done')}).catch(()=>{});});
 await page.goto(`/check/${id}`);await expect.poll(()=>reads).toBe(1);await page.getByRole('link',{name:'Ask about a title'}).click();await expect(page).toHaveURL('/');release();await page.waitForTimeout(200);await expect(page).toHaveURL('/');expect(reads).toBe(1);
});
test('initial stream refusal offers retry with saved state',async({page})=>{
 let retry=false;
 await page.route(`**/api/checks/${id}`,route=>route.fulfill({json:retry?fixture('get-check-done'):fixture('get-check-running')}));
 await page.route(`**/api/checks/${id}/events`,route=>route.fulfill({status:503,json:{error:{code:'busy',message:'busy'}}}));
 await page.goto(`/check/${id}`);await expect(page.getByRole('status')).toContainText('connection stopped');retry=true;await page.getByRole('button',{name:'Retry following'}).click();await expect(page).toHaveURL(`/card/${card}`);
});
for(const result of ['nonterminal','malformed','network']) test(`terminal final GET ${result} is recoverable`,async({page})=>{
 await mock(page);await page.goto(`/check/${id}`);await opened(page);
 if(result === 'network') await page.evaluate(()=>{const prior=window.fetch;window.fetch=(input,init)=>String(input).endsWith('/events')?prior(input,init):Promise.reject(new TypeError('network'));});
 else await state(page,result === 'nonterminal'?{id,status:'running'}:{id,status:'done',card_id:'bad'});
 await push(page,frames[3]);await expect(page.getByRole('button',{name:'Retry following'})).toBeVisible();await expect(page).toHaveURL(`/check/${id}`);
});
test('rapid retry ignores stale failed read from the previous attempt',async({page})=>{
 await mock(page,{id,status:'invalid'});await page.goto(`/check/${id}`);await expect(page.getByRole('button',{name:'Retry following'})).toBeVisible();
 await page.evaluate(({id,card})=>{
  let reads=0;const pending:{release?:()=>void}={};(window as unknown as {stale:typeof pending}).stale=pending;
  window.fetch=async(input)=>{
   if(String(input).includes(`/api/checks/${id}`)) {reads++;if(reads===1) return new Promise<Response>(resolve=>{pending.release=()=>resolve(new Response('{bad'));});return new Response(JSON.stringify({id,status:'done',card_id:card}));}
   return new Response('{}');
  };
 },{id,card});
 // Two starts in one event turn deliberately stress overlap, even before rerender removes the button.
 await page.getByRole('button',{name:'Retry following'}).evaluate(button=>{(button as HTMLButtonElement).click();(button as HTMLButtonElement).click();});
 await expect(page).toHaveURL(`/card/${card}`);await page.evaluate(()=>(window as unknown as {stale:{release:()=>void}}).stale.release());await page.waitForTimeout(100);await expect(page).toHaveURL(`/card/${card}`);
});

for (const bad of ['j_legacy', 'j_' + 'A'.repeat(32), 'j_' + '0'.repeat(31), 'j_' + '0'.repeat(33), 'c_' + '0'.repeat(32)]) test(`invalid check ID ${bad} refuses requests`, async ({page}) => {
 const requests: string[] = []; page.on('request', request => {if(request.url().includes('/api/checks/')) requests.push(request.url());});
 await page.goto(`/check/${bad}`);await expect(page.getByRole('status')).toContainText('Nothing matches that address.');await expect(page.getByRole('status')).toContainText('Go back to the ask page and start again.');expect(requests).toEqual([]);
});
for (const bad of ['c_legacy', 'c_' + 'A'.repeat(32), 'c_' + '0'.repeat(31), 'c_' + '0'.repeat(33), 'j_' + '0'.repeat(32), 'c_' + '0'.repeat(32) + '\n']) test(`invalid saved card ID ${JSON.stringify(bad)} stays on check`, async ({page}) => {
 await mock(page,{id,status:'done',card_id:bad});await page.goto(`/check/${id}`);await expect(page.getByRole('status')).toContainText('This check did not finish.');await expect(page).toHaveURL(`/check/${id}`);
});
