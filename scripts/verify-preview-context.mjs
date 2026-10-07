// Build webui first. Uses only a loopback fixture and its own headless Edge profile.
import assert from 'node:assert/strict';
import http from 'node:http';
import net from 'node:net';
import path from 'node:path';
import fs from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const out = path.join(repo, '.run-data', 'preview-context-check');
await fs.mkdir(out, { recursive: true });
const checks = [], errors = [], clients = new Set();
const clone = value => structuredClone(value);
const mkRun = (asset_id, run_id, revision = 'r1', version = 1) => ({ asset_id, run_id, current_revision_id: revision, base_revision_id: 'base', version, status: 'awaiting_review', stage: 'scene_review', content_mode: 'builtin' });
let runs = new Map(), order = [], empty = false, missingEvidence = false, reviewDelay = 0, snapshotDelay = 0, staleReview = null;
function reset() { runs = new Map([['a1', mkRun('asset_a', 'a1')], ['b1', mkRun('asset_b', 'b1')], ['page', mkRun('asset_a', 'page')]]); order = ['a1']; empty = missingEvidence = false; reviewDelay = snapshotDelay = 0; staleReview = null; }
reset();
const assets = ['asset_a', 'asset_b'].map(asset_id => ({ asset_id, status: 'ingested', agent_visible: true, human_approved: true, locked: false, allowed_agents: ['recognizer', 'ingester'], artifacts: {}, created_at: '2026-10-05T00:00:00Z', updated_at: '2026-10-05T00:00:00Z' }));
const ingestRuns = ['i1', 'i2'].map(run_id => ({ run_id, asset_id: 'asset_a', source_id: 'source', state: 'awaiting_review', stage: 'segment_review', version: 1, analysis_revision: 1, selection_revision: 0 }));
const responses = [];
function review(run) { return { run, revision: { revision_id: run.current_revision_id, package_ref: 'fixture', edl_sha256: 'fixture' },
  scenes: [{ scene_id: 'scene', label: `${run.run_id} ${run.current_revision_id}`, decision: 'confirmed', sequence_rank: 0 }],
  narration: [{ id: 'line', text: `字幕 ${run.run_id} ${run.current_revision_id}`, source_scene_id: 'scene', start: 0, end: 4 }],
  evidence_files: missingEvidence ? [] : [{ scene_id: 'scene', key: 'frame', sha256: `${run.run_id}-${run.current_revision_id}`, timestamp: '1.0' }] }; }
const emit = () => { for (const client of clients) client.write('event: workflow.changed\ndata: {}\n\nevent: review.changed\ndata: {}\n\n'); };
let failImages = false;
let ingestEmpty = false;
const requests = [];
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://localhost');
  const json = (data, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(data)); };
  try {
    requests.push(url.pathname);
    if (url.pathname === '/api/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' }); res.write(': ready\n\n'); clients.add(res); res.on('close', () => clients.delete(res)); return; }
    if (url.pathname.startsWith('/test/')) {
      const action = url.pathname.slice(6);
      if (action === 'reset') reset();
      if (action === 'revision') { runs.get('a1').current_revision_id = 'r2'; runs.get('a1').version++; }
      if (action === 'default') { runs.set('a2', mkRun('asset_a', 'a2', 'r-new')); order.unshift('a2'); }
      if (action === 'missing') order = order.filter(id => id !== 'a1');
      if (action === 'empty') empty = true;
      if (action === 'no-evidence') missingEvidence = true;
      if (action === 'fail-images') failImages = true;
      if (action === 'images-ok') failImages = false;
      if (action === 'review-delay') reviewDelay = 1200;
      if (action === 'snapshot-delay') snapshotDelay = 1200;
      if (action === 'stale-review') staleReview = review(mkRun('asset_a', 'a1'));
      if (action === 'review-current') { staleReview = null; reviewDelay = snapshotDelay = 0; }
      if (action === 'pages') { for (let i = 0; i < 20; i++) { const id = `history-${i}`; runs.set(id, mkRun('asset_a', id)); if (!order.includes(id)) order.push(id); } }
      if (action === 'source-fixture') assets[0].input_kind = 'raw_recording';
      if (action === 'source-empty') ingestEmpty = true;
      if (action === 'source-refresh') for (const client of clients) client.write('event: ingest.changed\ndata: {}\n\n');
      if (action === 'reanalyze') { ingestRuns[1].analysis_revision++; ingestRuns[1].version++; for (const client of clients) client.write('event: ingest.changed\ndata: {}\n\n'); }
      emit(); return json({ action });
    }
    if (url.pathname === '/api/assets') return json(assets);
    if (url.pathname === '/api/audit') return json([]);
    if (url.pathname === '/api/ingest-roots') return json({ configured: false, roots: [], ingesters: [] });
    if (url.pathname === '/api/processing-profiles') return json([]);
    const asset = url.pathname.match(/^\/api\/assets\/([^/]+)(.*)$/);
    if (asset) {
      if (asset[2] === '/workflows') return json(empty ? [] : asset[1] === 'asset_a' ? (Number(url.searchParams.get('offset')) > 0 ? [runs.get('page')] : order.slice(0, 20).map(id => runs.get(id))) : [runs.get('b1')]);
      if (asset[2] === '/ingest-runs') return json(asset[1] === 'asset_a' && !ingestEmpty ? ingestRuns : []);
      if (['/files', '/recordings', '/ingest-runs', '/tasks', '/narration-reviews'].includes(asset[2])) return json([]);
    }
    const ingest = url.pathname.match(/^\/api\/ingest-runs\/([^/]+)(.*)$/);
    if (ingest) {
      const run = ingestRuns.find(item => item.run_id === ingest[1]);
      if (!run) return json({}, 404);
      if (!ingest[2]) return json({ run, source: {}, plans: [], tasks: [], files: [], asset: assets[0], probe: { container: 'fixture', duration_us: 4000000, streams: [], limitations: [] }, segments: { count: 1, cut_count: 1, method_version: 'fixture', range_us: [0, 4000000] } });
      if (ingest[2] === '/segments') return json({ items: [{ segment_id: `${run.run_id}-v${run.analysis_revision}`, start_us: 0, end_us: 4000000, thumbnail_key: `thumb-${run.analysis_revision}`, reason: 'scene_change', score: 0.9 }], total: 1, offset: 0, limit: 50 });
      if (ingest[2].startsWith('/files/')) { res.writeHead(200, { 'Content-Type': 'image/svg+xml' }); res.end(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="#145777"/><text x="20" y="180" fill="white">${run.run_id} ${ingest[2]}</text></svg>`); return; }
    }
    const workflow = url.pathname.match(/^\/api\/workflows\/([^/]+)(.*)$/);
    if (workflow) {
      const run = runs.get(workflow[1]);
      if (!run) return json({ error: { code: 'not_found', message: 'missing run' } }, 404);
      if (workflow[2] === '') { const body = { run: clone(run), tasks: [], stages: [] }; if (snapshotDelay) await new Promise(resolve => setTimeout(resolve, snapshotDelay)); return json(body); }
      if (workflow[2] === '/review') { const body = clone(staleReview && run.run_id === 'a1' ? staleReview : review(run)); if (reviewDelay) await new Promise(resolve => setTimeout(resolve, reviewDelay)); responses.push({ run: run.run_id, servedRevision: body.revision.revision_id, currentRevision: run.current_revision_id }); return json(body); }
      if (workflow[2] === '/acceptance') return json([]);
      if (workflow[2].startsWith('/evidence/')) {
        if (failImages) return json({ error: { code: 'not_found', message: 'missing evidence' } }, 404);
        if (url.searchParams.get('revision_id') !== run.current_revision_id) return json({ error: { code: 'conflict', message: 'old revision' } }, 409);
        res.writeHead(200, { 'Content-Type': 'image/svg+xml', 'Cache-Control': 'no-store' });
        res.end(`<svg xmlns="http://www.w3.org/2000/svg" width="640" height="360"><rect width="640" height="360" fill="#145777"/><text x="20" y="180" fill="white" font-size="26">${run.run_id} ${run.current_revision_id}</text></svg>`); return;
      }
    }
    if (url.pathname.startsWith('/api/')) return json({ error: { code: 'not_found', message: 'fixture route unavailable' } }, 404);
    const file = path.resolve(repo, 'webui/dist', `.${url.pathname === '/' ? '/index.html' : url.pathname}`);
    if (!file.startsWith(path.join(repo, 'webui/dist') + path.sep)) return json({}, 403);
    const data = await fs.readFile(file); res.writeHead(200, { 'Content-Type': file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html' }); res.end(data);
  } catch (error) { if (!res.headersSent) json({ error: { message: String(error) } }, 500); else res.end(); }
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const debugSocket = net.createServer(); await new Promise(resolve => debugSocket.listen(0, '127.0.0.1', resolve));
const debugPort = debugSocket.address().port; await new Promise(resolve => debugSocket.close(resolve));
const edgePath = process.env.EDGE_PATH || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe';
const edgeLog = await fs.open(path.join(out, 'edge.log'), 'w');
const browser = spawn(edgePath, ['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-background-networking', `--remote-debugging-port=${debugPort}`, `--user-data-dir=${path.join(out, 'edge-profile')}`, 'about:blank'], { windowsHide: true, stdio: ['ignore', 'ignore', edgeLog.fd] });
let ws, seq = 0; const pending = new Map();
browser.on('error', error => errors.push(String(error)));
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const send = (method, params = {}) => new Promise((resolve, reject) => { const id = ++seq; const timeout = setTimeout(() => { pending.delete(id); reject(Error(`CDP timeout: ${method}`)); }, 15000); pending.set(id, { resolve: result => { clearTimeout(timeout); resolve(result); }, reject: error => { clearTimeout(timeout); reject(error); } }); ws.send(JSON.stringify({ id, method, params })); });
const ev = async expression => { const data = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true }); if (data.exceptionDetails) throw Error(JSON.stringify(data.exceptionDetails)); return data.result.value; };
const visible = selector => `[...document.querySelectorAll(${JSON.stringify(selector)})].find(e=>e.checkVisibility())`;
const root = '[data-page-view="editing"]';
const monitorImage = `${root} .monitor img.monitor-media`;
const wait = async expression => { for (let i = 0; i < 100; i++) { if (await ev(`Boolean(${expression})`)) return; await sleep(80); } throw Error(`Wait failed: ${expression}`); };
const click = async selector => {
  await wait(visible(selector));
  const point = await ev(`(()=>{const e=${visible(selector)};e.scrollIntoView({block:'center'});const r=e.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()`);
  await send('Input.dispatchMouseEvent', { type: 'mousePressed', button: 'left', clickCount: 1, ...point });
  await send('Input.dispatchMouseEvent', { type: 'mouseReleased', button: 'left', clickCount: 1, ...point });
  await sleep(140);
};
const action = async name => { await fetch(`${base}/test/${name}`); };
const route = async hash => { await ev(`location.hash=${JSON.stringify(hash)}`); await sleep(180); };
const readImage = () => ev(`${visible(monitorImage)}?.getAttribute('src') ?? null`);
const sceneReady = (text = 'a1 r1') => wait(`${visible(`${root} .scene-title`)}?.innerText.includes(${JSON.stringify(text)})`);
async function selectRun(id) {
  await click(`${root} .select-workflow-run .ant-select-selector`);
  await click(`.ant-select-dropdown:not(.ant-select-dropdown-hidden) [data-value="${id}"]`);
}
async function check(name, operation) { await operation(); checks.push({ name, pass: true }); console.log(`PASS ${name}`); }
try {
  let targets;
  for (let i = 0; i < 100; i++) { try { targets = await (await fetch(`http://127.0.0.1:${debugPort}/json/list`)).json(); if (targets.some(t => t.type === 'page')) break; } catch {} await sleep(100); }
  assert(targets?.some(t => t.type === 'page'), 'owned Edge did not start');
  ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject; });
  ws.onmessage = event => { const data = JSON.parse(event.data); if (data.method === 'Runtime.exceptionThrown') errors.push(data.params); const request = pending.get(data.id); if (request) { pending.delete(data.id); data.error ? request.reject(Error(data.error.message)) : request.resolve(data.result); } };
  await send('Runtime.enable'); await send('Page.enable');
  await send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false });
  await send('Page.navigate', { url: `${base}/#/editing/content` }); await sceneReady();
  await check('initial scene evidence', async () => { await click(`${root} .evidence-button`); await wait(`${visible(monitorImage)}?.naturalWidth>0`); assert.match(await readImage(), /a1\/evidence\/frame\?revision_id=r1$/); });
  await check('same-context SSE keeps preview', async () => { await action('refresh'); await sleep(500); assert.match(await readImage(), /revision_id=r1$/); });
  await check('content/review preserves shared preview', async () => { await route('#/editing/review'); await sceneReady(); assert.match(await readImage(), /revision_id=r1$/); await route('#/editing/content'); });
  await check('narration caption matches evidence', async () => {
    await ev(`[...document.querySelectorAll('${root} .ant-tabs-tab')].find(e=>e.checkVisibility()&&e.innerText.includes('解说草稿')).click()`);
    await wait(`[...document.querySelectorAll('${root} button')].some(e=>e.checkVisibility()&&e.innerText==='在监视器预览')`);
    await ev(`[...document.querySelectorAll('${root} button')].find(e=>e.checkVisibility()&&e.innerText==='在监视器预览').click()`);
    await wait(`${visible(`${root} .monitor-caption`)}?.innerText.includes('a1 r1')`); });
  await check('revision changes clear image, caption and focus', async () => { await action('revision'); await sceneReady('a1 r2'); assert.equal(await readImage(), null); assert.equal(await ev(`${visible(`${root} .monitor-caption`)}?.innerText ?? null`), null); assert.equal(await ev(`[...document.querySelectorAll('${root} .is-focused')].filter(e=>e.checkVisibility()).length`), 0); });
  await check('new revision can be previewed', async () => { await click(`${root} .evidence-button`); await wait(`${visible(monitorImage)}?.naturalWidth>0`); assert.match(await readImage(), /revision_id=r2$/); });
  await check('new default run clears old image', async () => { await action('default'); await sceneReady('a2 r-new'); assert.equal(await readImage(), null); });
  await check('explicit run switch clears image', async () => { await click(`${root} .evidence-button`); await selectRun('a1'); await sceneReady('a1 r2'); assert.equal(await readImage(), null); });
  await check('missing explicit run falls back safely', async () => { await click(`${root} .evidence-button`); await action('missing'); await sceneReady('a2 r-new'); assert.equal(await readImage(), null); });
  await check('pagination changes actual run', async () => { await action('pages'); await sleep(400); await click(`${root} .evidence-button`); await ev(`[...document.querySelectorAll('${root} button')].find(e=>e.checkVisibility()&&e.innerText==='较早流程').click()`); await sceneReady('page r1'); assert.equal(await readImage(), null);
    await click(`${root} .evidence-button`); await ev(`[...document.querySelectorAll('${root} button')].find(e=>e.checkVisibility()&&e.innerText==='较新流程').click()`); await sceneReady('a2 r-new'); assert.equal(await readImage(), null); });
  await check('missing evidence hides old preview', async () => { await click(`${root} .evidence-button`); await action('no-evidence'); await wait(`![...document.querySelectorAll('${root} .evidence-button')].some(e=>e.checkVisibility())`); assert.equal(await readImage(), null); });
  await action('reset'); await send('Page.reload'); await sceneReady();
  await check('asset A to B to A never restores old image', async () => { await click(`${root} .evidence-button`); await click('[data-asset-id="asset_b"]'); await sceneReady('b1 r1'); assert.equal(await readImage(), null); await click(`${root} .evidence-button`); await click('[data-asset-id="asset_a"]'); await sceneReady(); assert.equal(await readImage(), null); });
  await check('older delayed review cannot restore previous revision', async () => { await click(`${root} .evidence-button`); await action('review-delay'); await action('revision'); await sleep(250); assert.equal(await readImage(), null); await sceneReady('a1 r2'); await click(`${root} .evidence-button`); await action('stale-review'); await sleep(1600); const image = await readImage(); assert(!image || image.includes('revision_id=r2')); await action('review-current'); await sceneReady('a1 r2'); assert.match(await readImage(), /revision_id=r2$/); });
  await action('reset'); await send('Page.reload'); await sceneReady();
  await check('review first, snapshot later is safe', async () => { await click(`${root} .evidence-button`); await action('snapshot-delay'); await action('revision'); await sceneReady('a1 r2'); assert.equal(await readImage(), null); await click(`${root} .evidence-button`); await sleep(1400); assert.match(await readImage(), /revision_id=r2$/); });
  await check('image load failure is visible', async () => { await action('fail-images'); await action('default'); await sceneReady('a2 r-new'); await click(`${root} .evidence-button`); await wait(`${visible(`${root} .monitor-load-error`)}?.innerText.includes('加载失败')`); assert.equal(await readImage(), null); });
  await check('new image resets failure', async () => { await action('images-ok'); await selectRun('a1'); await sceneReady('a1 r2'); await click(`${root} .evidence-button`); await wait(`${visible(monitorImage)}?.naturalWidth>0`); assert.equal(await ev(`${visible(`${root} .monitor-load-error`)} ?? null`), null); });
  const screenshot = await send('Page.captureScreenshot', { format: 'png' });
  await fs.writeFile(path.join(out, 'preview-context-fixed.png'), Buffer.from(screenshot.data, 'base64'));
  await check('no workflows leaves no old evidence', async () => { await action('empty'); await wait(`![...document.querySelectorAll('${root} .edit-stage')].some(e=>e.checkVisibility())`); assert.equal(await readImage(), null); });
  assert(responses.some(item => item.servedRevision === 'r1' && item.currentRevision === 'r2'), 'fixture must actually serve an older review after revision advances');
  await action('source-fixture'); await send('Page.reload'); await route('#/editing/import');
  await wait(`${visible(`${root} .segment-thumb`)}?.getAttribute('aria-label').includes('i1-v1')`);
  await check('source preview follows ingest run switch', async () => {
    await click(`${root} .segment-thumb`); await wait(`${visible(monitorImage)}?.getAttribute('src').includes('/i1/')`);
    await click(`${root} .select-ingest-run .ant-select-selector`); await click('.ant-select-dropdown:not(.ant-select-dropdown-hidden) [data-value="i2"]');
    await wait(`${visible(`${root} .segment-thumb`)}?.getAttribute('aria-label').includes('i2-v1')`); assert.equal(await readImage(), null);
  });
  await check('source preview clears on reanalysis', async () => {
    await click(`${root} .segment-thumb`); await wait(`${visible(monitorImage)}?.getAttribute('src').includes('thumb-1')`);
    await action('reanalyze'); await wait(`${visible(`${root} .segment-thumb`)}?.getAttribute('aria-label').includes('i2-v2')`); assert.equal(await readImage(), null);
    await click(`${root} .segment-thumb`); await wait(`${visible(monitorImage)}?.getAttribute('src').includes('thumb-2')`);
  });
  await check('source same-context refresh keeps preview', async () => { await action('source-refresh'); await sleep(500); assert.match(await readImage(), /i2\/files\/thumb-2$/); });
  await check('source and program previews remain independent', async () => {
    await action('reset'); await route('#/editing/content'); await sceneReady(); assert.equal(await readImage(), null);
    await click(`${root} .evidence-button`); await route('#/editing/import'); assert.match(await readImage(), /i2\/files\/thumb-2$/);
    await route('#/editing/content'); assert.match(await readImage(), /a1\/evidence\/frame\?revision_id=r1$/); await route('#/editing/import');
  });
  await check('missing ingest run clears source preview', async () => { await action('source-empty'); await action('source-refresh'); await wait(`![...document.querySelectorAll('${root} .ingest-run')].some(e=>e.checkVisibility())`); assert.equal(await readImage(), null); });
  assert.deepEqual(errors, []);
} catch (error) {
  checks.push({ name: 'browser regression', pass: false, error: String(error) });
  if (ws?.readyState === WebSocket.OPEN) {
    await fs.writeFile(path.join(out, 'failure-dom.txt'), await ev('document.body.innerText').catch(() => 'unavailable'));
    const screenshot = await send('Page.captureScreenshot', { format: 'png' }).catch(() => null);
    if (screenshot) await fs.writeFile(path.join(out, 'failure.png'), Buffer.from(screenshot.data, 'base64'));
  }
  process.exitCode = 1;
} finally {
  await fs.writeFile(path.join(out, 'results.json'), JSON.stringify({ checks, errors, requests, responses, browserPID: browser.pid, fixture: base }, null, 2));
  if (ws?.readyState === WebSocket.OPEN) { await send('Browser.close').catch(() => {}); ws.close(); }
  for (const client of clients) client.end(); server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  if (browser.exitCode === null) { await Promise.race([new Promise(resolve => browser.once('exit', resolve)), sleep(2000)]); if (browser.exitCode === null) browser.kill(); }
  await edgeLog.close();
  console.log(JSON.stringify({ passed: checks.filter(c => c.pass).length, failed: checks.filter(c => !c.pass).length, results: path.join(out, 'results.json') }));
}
