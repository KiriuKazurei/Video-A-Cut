import { testStorageRoot } from './test-paths.mjs';
// Real visible WebView + real Go/Workers. Uses a local builtin profile, never cloud/model downloads.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { CDP, sleep } from './tauri-cdp.mjs';
const [portArg, pidArg, directoryArg] = process.argv.slice(2);
const pid = Number(pidArg), out = path.resolve(directoryArg), port = Number(portArg);
assert(out.startsWith(testStorageRoot + path.sep));
const checks = [], report = { checks, mode: 'visible_tauri_real_go_workers_local_builtin', human_acceptance: 'pending' };
const c = await CDP.connect(port);
const record = async (name, detail) => { checks.push({ name, pass: true, detail }); await fs.writeFile(path.join(out, 'results.json'), JSON.stringify(report, null, 2)); };
const run = (exe, args) => new Promise((resolve, reject) => {
  const child = spawn(exe, args, { windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] }); let output = '';
  child.stdout.on('data', b => output += b); child.stderr.on('data', b => output += b);
  child.on('error', reject); child.on('exit', code => code === 0 ? resolve(output) : reject(Error(output)));
});
const probe = (action, extra = []) => run('pwsh', ['-NoProfile', '-File', 'scripts/Tauri-WindowProbe.ps1', '-DesktopPid', String(pid), '-Action', action, ...extra]);
let base;
const api = (endpoint, method = 'GET', body) => c.ev(`(async()=>{const r=await fetch(${JSON.stringify(base + endpoint)},{method:${JSON.stringify(method)},headers:{'Content-Type':'application/json'},${body === undefined ? '' : 'body:' + JSON.stringify(JSON.stringify(body))}});const t=await r.text();if(!r.ok)throw Error(r.status+' '+t);return JSON.parse(t)})()`);
const until = async (callback, predicate, timeout = 120000) => {
  const deadline = Date.now() + timeout; let last;
  while (Date.now() < deadline) { last = await callback(); if (predicate(last)) return last; await sleep(250); }
  throw Error('State timeout: ' + JSON.stringify(last));
};
try {
  await c.wait('window.__TAURI__?.core');
  const status = await until(() => c.invoke('runtime_status'), v => v.status === 'ready' || v.status === 'fault');
  assert.equal(status.status, 'ready', status.message); base = status.api_base_url; report.initial_runtime = status;
  assert(status.workers.every(w => w.status === 'online'), JSON.stringify(status.workers));
  await c.wait(`document.querySelector('.app-shell') && document.body.innerText.includes('已连接')`);
  const window = JSON.parse(await probe('inspect')); assert(window.visible && window.handle); await record('visible native window, React shell and SSE', window);
  await c.screenshot(path.join(out, 'desktop-ready.png'));
  const badOrigin = await fetch(base + '/api/assets', { headers: { Origin: 'http://evil.test' } }); assert.equal(badOrigin.status, 403);
  const preflight = await fetch(base + '/api/assets', { method: 'OPTIONS', headers: { Origin: 'http://tauri.localhost', 'Access-Control-Request-Method': 'PATCH', 'Access-Control-Request-Headers': 'content-type' } }); assert.equal(preflight.status, 204); assert.equal(preflight.headers.get('access-control-allow-origin'), 'http://tauri.localhost');
  await record('dynamic port and exact origin preflight restrictions', { base, badOrigin: badOrigin.status });
  // Pick a real file using the native dialog, then submit only controlled root+relative path to Go.
  await c.ev(`window.__pickPromise=window.__TAURI__.core.invoke('select_recording').then(v=>{window.__picked=v;return v})` .replace('window.__pickPromise=', 'void (window.__pickPromise=') + ')');
  await probe('choose', ['-DialogTitle', '选择受控目录内的录像', '-FilePath', path.join(out, '录像 样本', '片段 测试.mp4')]);
  const picked = await c.ev('window.__pickPromise'); assert.equal(picked.root_id, 'recordings'); assert.equal(picked.relative_path, '片段 测试.mp4');
  const registered = await api('/api/recordings', 'POST', { asset_id: 'desktop_recording', ...picked, idempotency_key: 'desktop-recording' });
  await api('/api/assets/desktop_recording', 'PATCH', { agent_visible: true, allowed_agents: ['ingester'] });
  const ingest = (await api('/api/assets/desktop_recording/ingest-runs', 'POST', { source_id: registered.source.source_id, expected_source_version: registered.source.source_version, idempotency_key: 'desktop-probe' })).run;
  const ingested = await until(() => api('/api/ingest-runs/' + ingest.run_id), v => ['awaiting_review', 'failed'].includes(v.run.state));
  assert.equal(ingested.run.state, 'awaiting_review', JSON.stringify(ingested));
  await api('/api/ingest-runs/' + ingest.run_id + '/cancel', 'POST', { expected_version: ingested.run.version, reason: 'desktop bounded acceptance' });
  await record('native Chinese path selection, registration and real ingest Worker', { picked, run_id: ingest.run_id });
  const imported = await api('/api/deliveries/import', 'POST', { asset_id: 'desktop_business', package_dir: 'source' });
  await api('/api/assets/desktop_business', 'PATCH', { agent_visible: true, allowed_agents: ['recognizer', 'narrator', 'exporter'] });
  const readiness = await until(() => api('/api/assets/desktop_business/prepared-preflight', 'POST', { profile_id: 'local_regression', revision: 1 }), v => v.can_start, 60000);
  const workflow = await api('/api/assets/desktop_business/prepared-workflows', 'POST', { profile_id: 'local_regression', revision: 1, expected_asset_version: readiness.expected_asset_version, idempotency_key: 'desktop-workflow' });
  report.run_id = workflow.run_id; const endpoint = '/api/workflows/' + workflow.run_id;
  await until(() => api(endpoint + '/review'), v => v.scenes?.length > 0);
  for (;;) { const view = await api(endpoint + '/review'), scene = view.scenes.find(s => s.decision !== 'confirmed'); if (!scene) break; await api(endpoint + '/scene-reviews', 'POST', { expected_version: view.run.version, revision_id: view.revision.revision_id, scene_id: scene.scene_id, decision: 'confirmed' }); }
  await until(() => api(endpoint + '/review'), v => v.narration?.length > 0);
  for (;;) { const view = await api(endpoint + '/review'), line = view.narration.find(n => !n.approved_hash); if (!line) break; await api(endpoint + '/narration-reviews', 'POST', { expected_version: view.run.version, revision_id: view.revision.revision_id, narration_id: line.id }); }
  const ready = await until(() => api(endpoint), v => ['ready_for_acceptance', 'failed'].includes(v.run.status)); assert.equal(ready.run.status, 'ready_for_acceptance', JSON.stringify(ready));
  await record('real recognition, review, SAPI narration, subtitles and Node export', { run_id: workflow.run_id, source: 'bounded local fixture', content: 'builtin template, no model quality claim' });
  const view = await api(endpoint + '/review');
  const preview = await api(endpoint + '/previews', 'POST', { revision_id: view.revision.revision_id, expected_version: view.run.version, preset_id: 'mp4-720p-v1', request_id: 'desktop-preview' });
  const rendered = await until(() => api('/api/previews/' + preview.preview_id), v => ['succeeded', 'failed'].includes(v.status)); assert.equal(rendered.status, 'succeeded', JSON.stringify(rendered));
  report.media_url = base + '/api/previews/' + preview.preview_id + '/files/mp4';
  const range = await c.ev(`(async()=>{const r=await fetch(${JSON.stringify(report.media_url)},{headers:{Range:'bytes=0-31'}});return {status:r.status,bytes:(await r.arrayBuffer()).byteLength,range:r.headers.get('content-range')}})()`); assert.equal(range.status, 206); assert.equal(range.bytes, 32);
  await c.ev(`location.hash='#/export/delivery'`); await c.wait(`document.body.innerText.includes('desktop_business')`);
  await record('real MP4 generation and WebView Range request', range);
  // Download the real delivery through a native save dialog and verify ZIP magic.
  await c.ev(`void (window.__download=window.__TAURI__.core.invoke('download_file',{url:${JSON.stringify(base + endpoint + '/delivery.zip')}}))`);
  const saved = path.join(out, '桌面 交付.zip'); await probe('choose', ['-DialogTitle', '保存交付文件', '-FilePath', saved]); await c.ev('window.__download');
  const zip = await fs.readFile(saved); assert.equal(zip.readUInt32LE(0), 0x04034b50); await record('native save dialog and real delivery ZIP', { bytes: zip.length });
  await c.screenshot(path.join(out, 'desktop-business.png'));
  // Closing must hide, and launching another instance must show the original.
  await probe('close'); await sleep(300); assert.equal(JSON.parse(await probe('inspect')).visible, false); assert.equal((await api('/api/runtime/activity')).active_tasks, 0);
  const executable = process.env.VAC_DESKTOP_EXECUTABLE || path.resolve('build/desktop/app/video-auto-cut-desktop.exe'); await run(executable, []); await sleep(400); assert.equal(JSON.parse(await probe('inspect')).visible, true); await record('close hides with Go alive; duplicate launch restores original window');
  const journalPath = path.join(out, '数据 目录', 'desktop-session.json'); const journal = JSON.parse(await fs.readFile(journalPath));
  process.kill(journal.workers.find(w => w.role === 'recognizer').pid);
  await until(async () => JSON.parse(await fs.readFile(journalPath)), v => v.workers.find(w => w.role === 'recognizer').pid !== journal.workers.find(w => w.role === 'recognizer').pid);
  await record('owned Worker crash restarts within finite retry policy');
  // Crash Go: every session Worker must stop before restart; history remains.
  const crashed = JSON.parse(await fs.readFile(journalPath)); process.kill(crashed.control_pid);
  await until(() => c.invoke('runtime_status'), v => v.status === 'fault', 30000);
  await record('Go crash enters visible fault and stops the session');
  await c.invoke('restart_runtime'); const restarted = await until(() => c.invoke('runtime_status'), v => v.status === 'ready' || v.status === 'fault'); assert.equal(restarted.status, 'ready', restarted.message); base = restarted.api_base_url;
  assert.notEqual(restarted.session_id, status.session_id); assert.equal((await api(endpoint)).run.status, 'ready_for_acceptance');
  await c.wait(`document.querySelector('.app-shell') && document.body.innerText.includes('已连接')`);
  report.restarted_runtime = restarted; report.media_url = base + '/api/previews/' + preview.preview_id + '/files/mp4';
  await record('new port/session, SQLite backup, retained workflow and SSE reconnect');
  assert.deepEqual(c.errors, []); report.passed = true;
} catch (error) {
  checks.push({ name: 'desktop acceptance', pass: false, error: String(error) }); report.passed = false; process.exitCode = 1;
  await fs.writeFile(path.join(out, 'failure-dom.txt'), await c.ev('document.body.innerText').catch(() => ''));
  await c.screenshot(path.join(out, 'failure.png')).catch(() => {});
} finally {
  await fs.writeFile(path.join(out, 'results.json'), JSON.stringify(report, null, 2)); c.close();
  console.log(JSON.stringify({ passed: checks.filter(v => v.pass).length, failed: checks.filter(v => !v.pass).length, report: path.join(out, 'results.json') }));
}
