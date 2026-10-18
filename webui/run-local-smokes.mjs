// Serve an isolated built UI for the existing route-mocked browser checks.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

assert.ok(process.env.VAC_TEST_OUTPUT && process.env.VAC_DIST, 'Run through TestWorkspace.ps1 with VAC_DIST');
const here = path.dirname(fileURLToPath(import.meta.url));
const dist = path.resolve(process.env.VAC_DIST);
const output = path.join(process.env.VAC_TEST_OUTPUT, 'review-smokes');
fs.mkdirSync(output, { recursive: true });
const asset = { asset_id: '_selfcheck', status: 'ingested', agent_visible: true, human_approved: true, locked: false, allowed_agents: [], artifacts: {} };
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = value => { res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(value)); };
  if (url.pathname === '/api/events') {
    res.writeHead(200, { 'Content-Type': 'text/event-stream' }); res.write(': local fixture\n\n'); return;
  }
  if (url.pathname === '/api/assets') return json([asset]);
  if (url.pathname === '/api/assets/_selfcheck') return json(asset);
  if (url.pathname === '/api/ingest-roots') return json({ configured: false, roots: [], policy: {}, ingesters: [] });
  if (/^\/api\/workflows\/[^/]+\/previews$/.test(url.pathname)) return json({ previews: [], presets: [], workers: [] });
  if (url.pathname.startsWith('/api/')) {
    if (req.method !== 'GET') { res.writeHead(405); res.end(); return; }
    return json([]);
  }
  const file = path.resolve(dist, '.' + (url.pathname === '/' ? '/index.html' : url.pathname));
  if (!file.startsWith(dist + path.sep)) { res.writeHead(403); res.end(); return; }
  try {
    res.writeHead(200, { 'Content-Type': ({ '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' })[path.extname(file)] ?? 'application/octet-stream' });
    res.end(fs.readFileSync(file));
  } catch { res.writeHead(404); res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
try {
  const scripts = process.argv.slice(2);
  for (const script of scripts.length ? scripts : ['quickstart_smoke.mjs', 'review_shortcuts_smoke.mjs']) {
    assert.ok(['quickstart_smoke.mjs', 'review_shortcuts_smoke.mjs'].includes(script));
    const log = fs.createWriteStream(path.join(output, `${script}.log`));
    const child = spawn(process.execPath, [path.join(here, script), base, path.join(output, script)], { cwd: here, windowsHide: true });
    child.stdout.pipe(log, { end: false }); child.stderr.pipe(log, { end: false });
    const code = await new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve); });
    await new Promise(resolve => log.end(resolve));
    assert.equal(code, 0, `${script} failed; see ${path.join(output, `${script}.log`)}`);
    console.log(`PASS: ${script}`);
  }
} finally {
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
}
