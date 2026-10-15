import { test, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';
const routes = [ ['/', 'Research first.'], ['/check/arbitrary-check-123','Checking this title'], ['/card/c_00000000000000000000000000000001','No'], ['/compare/first-arbitrary/second-arbitrary','Compare titles'] ];
// Use the published Chaaya gate itself inside the rendered browser document.
const gate = readFileSync('node_modules/@nrynss/chaaya/dist/testing/a11y.js', 'utf8').replace('export async function', 'async function').replace('(await import("axe-core")).default', 'window.axe');
for (const [route, heading] of routes) {
 for (const mode of ['light','dark']) {
  test(`${route} built direct reload at 360px in ${mode}: render, fit and accessibility`, async ({page}) => {
   const errors: string[] = [];
   page.on('pageerror', error => errors.push(error.message));
   await page.addInitScript(mode => localStorage.setItem('chaaya-theme', mode), mode);
   if (route.startsWith('/card/')) await page.route('**/api/cards/*', r => r.fulfill({json: JSON.parse(readFileSync('../testdata/contracts/card-no.json', 'utf8'))}));
   await page.goto(route);
   await expect(page.getByRole('heading',{level:1})).toContainText(heading);
   await expect(page.getByRole('button', { name: /^Theme:/ })).toHaveAttribute('data-mode', mode);
   await page.reload();
   await expect(page.getByRole('heading',{level:1})).toContainText(heading);
   expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
   await page.addScriptTag({ path: 'node_modules/axe-core/axe.min.js' });
   await page.addScriptTag({ content: gate + '\nwindow.runChaayaGate = () => a11yGate(document.body);' });
   await page.evaluate(() => (window as unknown as {runChaayaGate: () => Promise<void>}).runChaayaGate());
   expect(errors).toEqual([]);
   await page.screenshot({path:`/tmp/before-yes-${route.split('/')[1] || 'ask'}-${mode}.png`,fullPage:true});
  });
 }
}
test('three-state theme follows system, forces either mode, persists and returns to system', async ({page}) => {
 await page.emulateMedia({colorScheme:'dark'});
 await page.goto('/');
 await expect(page.getByRole('button', { name: /^Theme:/ })).toHaveAttribute('data-mode', 'system');
 const scheme = () => page.evaluate(() => getComputedStyle(document.documentElement).colorScheme);
 expect(await scheme()).toBe('dark');
 await page.getByRole('button', { name: /^Theme:/ }).click();
 expect(await scheme()).toBe('light');
 await page.reload();
 await expect(page.getByRole('button', { name: /^Theme:/ })).toHaveAttribute('data-mode', 'light');
 expect(await scheme()).toBe('light');
 await page.getByRole('button', { name: /^Theme:/ }).click();
 await page.emulateMedia({colorScheme:'light'});
 expect(await scheme()).toBe('dark');
 await page.getByRole('button', { name: /^Theme:/ }).click();
 expect(await scheme()).toBe('light');
 await page.emulateMedia({colorScheme:'dark'});
 expect(await scheme()).toBe('dark');
 expect(await page.evaluate(() => document.documentElement.hasAttribute('data-theme'))).toBe(false);
});
test('desktop reading width and keyboard theme control', async ({page}) => {
 await page.setViewportSize({width:1440,height:1000});
 await page.goto('/');
 await expect(page.getByRole('heading',{level:1})).toContainText('Research first.');
 await page.getByRole('button', { name: /^Theme:/ }).focus();
 await expect(page.getByRole('button', { name: /^Theme:/ })).toBeFocused();
 await page.keyboard.press('Enter');
 await expect(page.getByRole('button', { name: /^Theme:/ })).toHaveAttribute('data-mode', 'light');
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.screenshot({path:'/tmp/before-yes-ask-desktop.png',fullPage:true});
});
