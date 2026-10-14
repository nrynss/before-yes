// The browser test loads this file through Vite. Importing the component from the test runner uses a second Svelte copy and the render throws.
import { render } from 'svelte/server';
import Notice from './Notice.svelte';

export async function renderNotice(props: { error?: unknown; unknownArea?: boolean }): Promise<string> {
  const rendered = await render(Notice, { props });
  return rendered.body;
}
