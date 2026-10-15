import { mount } from 'svelte';
import { api, type ApiRequestInit } from '@nrynss/chaaya/keel';
import Keys from './Keys.svelte';

// Title and age come from the query. A key must never be read from it.
const params = new URLSearchParams(window.location.search);
const title = params.get('title') ?? 'Roblox';
const ageText = params.get('age');
const age = ageText === null ? 9 : Number(ageText);
const root = document.createElement('div');
root.id = 'keys-root';
document.body.appendChild(root);

if (params.get('client') === 'passed') {
  mount(Keys, { target: root, props: { title, age, api: marked } });
} else {
  mount(Keys, { target: root, props: { title, age } });
}

function marked<T>(path: string, init?: ApiRequestInit): Promise<T> {
  const headers = new Headers(init?.headers);
  headers.set('X-Test-Client', 'passed');
  return api<T>(path, { ...init, headers });
}
