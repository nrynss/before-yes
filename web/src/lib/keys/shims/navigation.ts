// Test stand-in for $app/navigation. The dialog source imports the real
// module. The browser test aliases this file because it cannot add a route.
export function goto(url: string | URL): Promise<void> {
  const next = typeof url === 'string' ? url : `${url.pathname}${url.search}${url.hash}`;
  window.location.assign(next);
  return Promise.resolve();
}
