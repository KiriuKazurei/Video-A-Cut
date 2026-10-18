import { testDirectory } from './test-paths.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import http from 'node:http';
import { spawn, execFileSync } from 'node:child_process';
import { CDP, sleep } from './tauri-cdp.mjs';

// Real Windows NSIS update in a copied, repository-contained app directory.
// The feed is accepted only by the debug baseline. The signed payload is the
// actual release artifact; it restarts into production HTTPS-only updater code.
const root = testDirectory('updater');
const application = path.join(root, 'app', 'video-auto-cut-desktop.exe');
const artifact = path.resolve(process.argv[2]);
const configFile = path.join(root, 'desktop.json');
const cfg = JSON.parse(await fs.readFile(configFile));
const checks = [];
let mode = 'same', c, owned, updatedPid;
const installer = await fs.readFile(artifact);
const signature = (await fs.readFile(artifact + '.sig', 'utf8')).trim();
const server = http.createServer(async (request, response) => {
  if (request.url === '/latest.json') {
    if (mode === 'offline') {response.writeHead(503); response.end(); return;}
    response.setHeader('Content-Type', 'application/json');
    response.end(JSON.stringify({ version: mode === 'same' ? '0.1.0' : '0.1.1', notes: '中文更新测试：性能与补丁，保留数据。', platforms: {
      'windows-x86_64': { url: mode === 'bad-url' ? 'http://example.invalid/installer.exe' : `http://127.0.0.1:${server.address().port}/installer.exe`, signature },
    } }));
  } else if (request.url === '/installer.exe') {
    const bytes = mode === 'tampered' ? Buffer.from(installer) : installer;
    if (mode === 'tampered') bytes[Math.floor(bytes.length / 2)] ^= 1;
    response.setHeader('Content-Length', String(bytes.length));
    response.setHeader('Content-Type', 'application/octet-stream');
    for (let offset = 0; offset < bytes.length && !response.destroyed; offset += 65536) {
      response.write(bytes.subarray(offset, offset + 65536)); await sleep(5);
    }
    response.end();
  } else {response.writeHead(404); response.end();}
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const until = async (read, test, timeout = 60000) => {
  const deadline = Date.now() + timeout; let value;
  while (Date.now() < deadline) {value = await read(); if (test(value)) return value; await sleep(100);}
  throw Error('Wait failed: ' + JSON.stringify(value));
};
const record = (name, detail) => checks.push({ name, pass: true, detail });
const probe = (pid, action, dialog) => execFileSync('pwsh', ['-NoProfile', '-File', 'scripts/Tauri-WindowProbe.ps1', '-DesktopPid', String(pid), '-Action', action, ...(dialog ? ['-DialogTitle', dialog] : [])], { encoding: 'utf8', windowsHide: true });
const appProcesses = () => JSON.parse(execFileSync('pwsh', ['-NoProfile', '-Command', `@(Get-CimInstance Win32_Process|Where-Object {$_.Name -eq 'video-auto-cut-desktop.exe' -and $_.ExecutablePath -eq '${application.replaceAll("'", "''")}'}|Select-Object ProcessId,ExecutablePath)|ConvertTo-Json -AsArray -Compress`], { encoding: 'utf8', windowsHide: true }) || '[]');
let base;
const api = async (endpoint, body) => {
  const response = await fetch(base + endpoint, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const value = await response.json(); assert(response.ok, JSON.stringify(value)); return value;
};
const click = async label => c.ev(`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.innerText===${JSON.stringify(label)});if(!b||b.disabled)throw Error('Button unavailable');b.click();return true})()`);
const status = () => c.invoke('update_status');
try {
  assert.equal(appProcesses().length, 0, 'Owned test app is already running');
  const inherited = { ...process.env, VAC_DESKTOP_CONFIG: configFile, VAC_UPDATE_TEST_ENDPOINT: `http://127.0.0.1:${server.address().port}/latest.json`, PYTHONDONTWRITEBYTECODE: '1', WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: '--remote-debugging-port=19341', WEBVIEW2_USER_DATA_FOLDER: path.join(root, 'webview-profile'), TEMP: path.join(root, 'temp'), TMP: path.join(root, 'temp') };
  delete inherited.VAC_DATA_DIR;
  owned = spawn(application, [], { env: inherited, windowsHide: true, stdio: 'ignore' });
  owned.on('error', error => {console.error(String(error));});
  c = await CDP.connect(19341);
  await c.wait('window.__TAURI__?.core && document.querySelector("button")');
  const ready = await until(() => c.invoke('runtime_status'), state => ['ready', 'fault'].includes(state.status));
  assert.equal(ready.status, 'ready', ready.message); base = ready.api_base_url;
  assert(JSON.parse(probe(owned.pid, 'inspect')).visible);
  assert.equal((await status()).current_version, '0.1.0');
  assert(JSON.stringify(await api('/api/assets')).includes('desktop_business'));
  record('actual debug baseline window, Go/Workers and retained history');
  await c.ev(`void window.__TAURI__.event.listen('update-status',e=>{(window.__updateEvents??=[]).push(e.payload)})`);
  await click('检查更新');
  await until(status, state => ['up_to_date', 'error'].includes(state.phase));
  assert.equal((await status()).phase, 'up_to_date');
  record('same-version feed reports no update in actual UI');
  mode = 'offline'; await click('重新检查');
  await until(status, state => state.phase === 'error');
  assert.equal((await c.invoke('runtime_status')).status, 'ready');
  record('feed HTTP failure is visible and keeps business service running');
  mode = 'bad-url'; await click('重新检查');
  const bad = await until(status, state => state.phase === 'error');
  assert(bad.message.includes('HTTPS')); record('non-HTTPS remote artifact URL rejected');
  mode = 'tampered'; await click('重新检查'); await until(status, state => state.phase === 'available');
  await click('下载更新'); await until(status, state => state.phase === 'error');
  assert.equal((await status()).ready_to_install, false);
  assert.equal((await c.invoke('runtime_status')).session_id, ready.session_id);
  record('tampered installer signature rejected before stopping any business process');
  mode = 'good'; await click('重新检查'); await until(status, state => state.phase === 'available');
  await click('下载更新'); await until(status, state => state.phase === 'downloading');
  await assert.rejects(c.invoke('check_update'), /已有更新操作/);
  const downloaded = await until(status, state => ['downloaded', 'error'].includes(state.phase));
  assert.equal(downloaded.phase, 'downloaded', downloaded.message); assert(downloaded.ready_to_install);
  assert.equal(downloaded.downloaded_bytes, installer.length);
  assert((await c.ev('window.__updateEvents')).some(state => state.phase === 'downloading' && state.downloaded_bytes > 0));
  await c.screenshot(path.join(root, 'update-downloaded.png'));
  record('real signed NSIS download, progress and concurrent-operation guard', { bytes: installer.length });
  const voices = (await api('/api/tts/capabilities')).sapi_voices;
  assert(voices.length);
  const job = await api('/api/tts/preview-jobs', { text: '这是安装更新时保留业务数据的本地测试。'.repeat(25), settings: { engine: 'sapi', voice: voices.find(v => v.includes('Huihui')) || voices[0], language: 'zh', speed: 1, volume: 1, pitch_semitones: 0, sapi_rate: 0, instruction: '' }, idempotency_key: 'updater-exit-' + Date.now() });
  assert((await api('/api/runtime/activity')).active_tasks > 0, 'TTS must be active before confirmation');
  await click('安装并重启');
  probe(owned.pid, 'cancel', '安装 Video Auto Cut 更新');
  await until(status, state => state.phase === 'downloaded');
  assert.equal((await c.invoke('runtime_status')).session_id, ready.session_id);
  const afterCancel = await api('/api/tts/preview-jobs/' + job.job_id);
  assert(['queued', 'running', 'succeeded'].includes(afterCancel.status), JSON.stringify(afterCancel));
  record('cancel native update confirmation preserves TTS progress and current session', { ttsStatus: afterCancel.status });
  const journal = JSON.parse(await fs.readFile(path.join(cfg.data_directory, 'desktop-session.json')));
  await click('安装并重启');
  await until(status, state => state.phase === 'awaiting_confirmation');
  await assert.rejects(c.invoke('restart_runtime'), /安装更新/);
  await c.invoke('request_exit');
  assert.equal((await status()).phase, 'awaiting_confirmation');
  record('restart and tray/toolbar exit cannot interrupt update confirmation');
  probe(owned.pid, 'confirm', '安装 Video Auto Cut 更新');
  await until(async () => {try {process.kill(owned.pid, 0); return false;} catch {return true;}}, Boolean, 45000);
  for (const pid of [journal.control_pid, ...journal.workers.map(worker => worker.pid)]) assert.throws(() => process.kill(pid, 0));
  assert.equal(JSON.parse(await fs.readFile(path.join(cfg.data_directory, 'desktop-session.json'))).status, 'stopped');
  record('update installation starts after original Go/Workers and data lock are released');
  c.close(); c = undefined;
  const processes = await until(async () => appProcesses(), rows => rows.length === 1, 60000);
  updatedPid = processes[0].ProcessId;
  c = await CDP.connect(19341);
  await c.wait('window.__TAURI__?.core && document.querySelector("button")');
  const updated = await until(() => c.invoke('runtime_status'), state => ['ready', 'fault'].includes(state.status));
  assert.equal(updated.status, 'ready', updated.message); base = updated.api_base_url;
  assert.equal((await status()).current_version, '0.1.1');
  assert.equal(path.resolve(updated.data_directory), path.resolve(cfg.data_directory));
  assert.notEqual(updated.session_id, ready.session_id); assert(updated.workers.every(worker => worker.status === 'online'));
  assert(JSON.parse(probe(updatedPid, 'inspect')).visible);
  assert(JSON.stringify(await api('/api/assets')).includes('desktop_business'));
  assert.equal((await api('/api/tts/preview-jobs/' + job.job_id)).job_id, job.job_id);
  await c.screenshot(path.join(root, 'updated-ready.png'));
  record('real 0.1.0 to 0.1.1 NSIS install and visible restart; custom config/history/TTS retained', { updatedPid, base });
  await c.invoke('stop_runtime'); await until(() => c.invoke('runtime_status'), state => state.status === 'stopped');
  await c.ev(`setTimeout(()=>window.__TAURI__.core.invoke('request_exit'),100);true`);
  await until(async () => appProcesses(), rows => rows.length === 0);
  record('updated production application exits cleanly');
  c.close(); c = undefined;
} catch (error) {
  checks.push({ name: 'real updater acceptance', pass: false, error: String(error) }); process.exitCode = 1;
  if (c) {
    await fs.writeFile(path.join(root, 'failure-dom.txt'), await c.ev('document.body.innerText').catch(() => ''));
    await c.screenshot(path.join(root, 'failure.png')).catch(() => {});
  }
} finally {
  if (c) {
    try {
      const update = await status();
      if (update.phase === 'awaiting_confirmation') probe(updatedPid || owned.pid, 'cancel', '安装 Video Auto Cut 更新');
      await sleep(200); await c.invoke('stop_runtime');
      await until(() => c.invoke('runtime_status'), state => state.status === 'stopped', 30000);
      await c.ev(`setTimeout(()=>window.__TAURI__.core.invoke('request_exit'),100);true`);
    } catch {}
    c.close();
  }
  server.closeAllConnections(); await new Promise(resolve => server.close(resolve));
  const result = { checks, passed: checks.every(check => check.pass), publication: 'GitHub release not published; local signed feed acceptance recorded above' };
  await fs.writeFile(path.join(root, 'results.json'), JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result));
}
