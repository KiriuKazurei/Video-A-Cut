import { testDirectory } from './test-paths.mjs';
// Real local page for the ASS style editor. Loopback base URL required.
// Schematic placement in the editor is not the burned MP4; this script records that distinction.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import { spawn } from 'node:child_process';

const [base] = process.argv.slice(2);
if (!base || !['127.0.0.1', 'localhost'].includes(new URL(base).hostname)) throw new Error('loopback base URL required');
const out = testDirectory('ass-editor-live');
await fs.mkdir(out, { recursive: true });
const checks = [];
const errors = [];
const socket = net.createServer();
await new Promise(resolve => socket.listen(0, '127.0.0.1', resolve));
const port = socket.address().port;
await new Promise(resolve => socket.close(resolve));
const edgeLog = await fs.open(path.join(out, 'edge.log'), 'w');
const browser = spawn(process.env.EDGE_PATH || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe', [
  '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check', '--disable-background-networking',
  `--remote-debugging-port=${port}`, `--user-data-dir=${path.join(out, 'edge-profile')}`, 'about:blank'
], { windowsHide: true, stdio: ['ignore', 'ignore', edgeLog.fd] });
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
let ws;
let seq = 0;
const pending = new Map();
const send = (method, params = {}) => new Promise((resolve, reject) => {
  const id = ++seq;
  const timer = setTimeout(() => { pending.delete(id); reject(new Error(`CDP timeout ${method}`)); }, 20000);
  pending.set(id, { resolve: value => { clearTimeout(timer); resolve(value); }, reject: error => { clearTimeout(timer); reject(error); } });
  ws.send(JSON.stringify({ id, method, params }));
});
const ev = async expression => {
  const result = await send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
  return result.result.value;
};
const visible = selector => `[...document.querySelectorAll(${JSON.stringify(selector)})].find(element => element.checkVisibility())`;
const wait = async expression => {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (await ev(`Boolean(${expression})`)) return;
    await sleep(100);
  }
  throw new Error(`wait failed: ${expression}`);
};
try {
  let targets;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    try {
      targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
      if (targets.some(target => target.type === 'page')) break;
    } catch { /* Edge is still starting */ }
    await sleep(100);
  }
  assert(targets?.some(target => target.type === 'page'), 'Edge did not start');
  ws = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject; });
  ws.onmessage = event => {
    const message = JSON.parse(event.data);
    if (message.method === 'Runtime.exceptionThrown') errors.push(message.params);
    const waiter = pending.get(message.id);
    if (!waiter) return;
    pending.delete(message.id);
    message.error ? waiter.reject(new Error(message.error.message)) : waiter.resolve(message.result);
  };
  await send('Runtime.enable');
  await send('Page.enable');
  const openEditor = async (width, height) => {
    await send('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: false });
    await send('Page.navigate', { url: `${base}/#/export/delivery` });
    await wait(visible('[data-testid="ass-editor-open"]:not([disabled])'));
    await ev(`(${visible('[data-testid="ass-editor-open"]')}).click()`);
    await wait(visible('[data-testid="ass-editor"]'));
    await wait(visible('[data-testid="ass-stage"]'));
  };
  await openEditor(1440, 900);
  const desktopTracks = await ev(`getComputedStyle(${visible('[data-testid="ass-editor"]')}).gridTemplateColumns.split(' ').filter(Boolean).length`);
  assert.equal(desktopTracks, 2);
  const note = await ev(`${visible('[data-testid="ass-schematic-note"]')}.textContent`);
  assert.match(note, /示意/);
  assert.match(note, /不是 FFmpeg 烧录成片/);
  const cueInputs = await ev(`[...document.querySelectorAll('[data-testid="ass-cue-list"] input:not([type="checkbox"])')].length`);
  assert.equal(cueInputs, 0);
  checks.push({ name: 'desktop editor splits cue list and schematic preview', pass: true, tracks: desktopTracks });
  await openEditor(375, 812);
  const narrowTracks = await ev(`getComputedStyle(${visible('[data-testid="ass-editor"]')}).gridTemplateColumns.split(' ').filter(Boolean).length`);
  const scrollWidth = await ev('document.documentElement.scrollWidth');
  assert.equal(narrowTracks, 1);
  assert.ok(scrollWidth <= 375);
  checks.push({ name: 'narrow editor stacks cue list above settings', pass: true, tracks: narrowTracks, scrollWidth });
  const shot = await send('Page.captureScreenshot', { format: 'png' });
  await fs.writeFile(path.join(out, 'ass-editor-375.png'), Buffer.from(shot.data, 'base64'));
  checks.push({
    name: 'schematic preview is not the burned MP4',
    pass: true,
    pending: '人工核对真实成片的换行、字体和时序；本脚本只确认编辑器标明示意与成片的区别'
  });
  assert.deepEqual(errors, []);
} catch (error) {
  checks.push({ name: 'live ASS editor', pass: false, error: String(error) });
  process.exitCode = 1;
  if (ws?.readyState === WebSocket.OPEN) await fs.writeFile(path.join(out, 'failure-dom.txt'), await ev('document.body.innerText').catch(() => ''));
} finally {
  if (ws?.readyState === WebSocket.OPEN) {
    await send('Browser.close').catch(() => {});
    ws.close();
  }
  if (browser.exitCode === null) {
    await Promise.race([new Promise(resolve => browser.once('exit', resolve)), sleep(2000)]);
    if (browser.exitCode === null) browser.kill();
  }
  await edgeLog.close();
  await fs.writeFile(path.join(out, 'results.json'), JSON.stringify({
    checks, errors, browserPID: browser.pid, server: base, mode: 'ass_editor_browser',
    schematicVersusBurn: '编辑器画面是脚本坐标示意。ASS/MP4 下载和播放器里的文件才是烧录结果。'
  }, null, 2));
  console.log(JSON.stringify({ passed: checks.filter(check => check.pass).length, failed: checks.filter(check => !check.pass).length }));
}
