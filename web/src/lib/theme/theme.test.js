import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { checkTokens } from '@nrynss/chaaya/tokens';
import { contrastGate } from '@nrynss/chaaya/testing';
const css = readFileSync(new URL('./theme.css', import.meta.url), 'utf8');
test('all four themes declare every Chaaya role', () => {
 assert.equal((css.match(/\bcolor-scheme: (?:light|dark);/g) || []).length, 4);
 assert.deepEqual(checkTokens(css), []);
});
test('light and dark themes pass Chaaya contrast gate for every drawn pair', () => {
 /** @type {import('@nrynss/chaaya/testing').ContrastPair[]} */
 const pairs = ['ground','surface','raised','sunken','accent-soft'].flatMap(bg => ['text','dim','faint','accent'].map(fg => /** @type {[string, string]} */ ([fg,bg])));
 pairs.push(['on-accent','accent'], ['ok','ok-soft'], ['warn','warn-soft'], ['stop','stop-soft']);
 contrastGate(css, pairs);
});

test('Chaaya first-paint script is verbatim and precedes the application head', async () => {
 const { themeScript } = await import('@nrynss/chaaya/theme');
 const html = readFileSync(new URL('../../app.html', import.meta.url), 'utf8');
 assert.ok(html.includes('<script>' + themeScript + '</script>'));
 assert.ok(html.indexOf(themeScript) < html.indexOf('%sveltekit.head%'));
});
