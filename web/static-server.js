// Serve the built artifact with the same HTML fallback expected from the Go host.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { resolve, extname, sep } from 'node:path';
const root = resolve('build');
const types = { '.html':'text/html', '.js':'text/javascript', '.css':'text/css', '.json':'application/json' };
createServer(async (request, response) => {
 const pathname = decodeURIComponent(new URL(request.url || '/', 'http://localhost').pathname);
 const path = resolve(root, '.' + pathname);
 if (path !== root && !path.startsWith(root + sep)) { response.writeHead(403).end(); return; }
 try {
  const data = await readFile(path);
  response.setHeader('Content-Type', types[/** @type {keyof typeof types} */ (extname(path))] || 'application/octet-stream');
  response.end(data);
 } catch {
  if (pathname.startsWith('/_app/')) { response.writeHead(404).end(); return; }
  response.setHeader('Content-Type','text/html');
  response.end(await readFile(resolve(root,'index.html')));
 }
}).listen(4173,'127.0.0.1');
