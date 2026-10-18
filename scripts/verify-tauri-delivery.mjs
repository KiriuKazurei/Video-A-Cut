import { testDirectory } from './test-paths.mjs';
import fs from 'node:fs/promises';
import path from 'node:path';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { CDP, sleep } from './tauri-cdp.mjs';

// Run after resizing the final app, exiting it and relaunching with the same
// isolated data. No production data or user processes are selected by this test.
const pid = Number(process.argv[2]);
assert(Number.isInteger(pid) && pid > 0, 'Owned final delivery PID is required');
const c = await CDP.connect(19331);
const directory = testDirectory('tauri-live');
const defaults = process.argv.includes('--defaults');
const config = defaults ? {
  control_binary: path.resolve('build/desktop/app/control-plane.exe'),
  data_directory: testDirectory('tauri-default/数据 目录'),
} : JSON.parse(await fs.readFile(path.join(directory, 'desktop.json')));
const checks = [];
try {
  await c.wait(`window.__TAURI__ !== undefined`);
  await c.wait(`document.body.innerText.includes('后台服务已就绪')`, 60000);
  const status = await c.invoke('runtime_status');
  assert.equal(status.status, 'ready');
  assert.equal(path.resolve(status.data_directory), path.resolve(config.data_directory));
  assert(status.workers.every(worker => worker.status === 'online'));
  assert.equal(status.workers.length, defaults ? 3 : 4);
  assert.equal(path.resolve(config.control_binary), path.resolve('build/desktop/app/control-plane.exe'));
  checks.push({ name: 'final delivery resources and Go/Worker startup', pass: true, detail: status });
  const window = JSON.parse(execFileSync('pwsh', ['-NoProfile', '-File', 'scripts/Tauri-WindowProbe.ps1', '-DesktopPid', String(pid)], { encoding: 'utf8' }));
  assert(window.visible);
  checks.push({ name: 'final delivery actual visible window', pass: true, detail: window });
  const size = await c.invoke('plugin:window|inner_size', { label: 'main' });
  assert.deepEqual(size, { width: 1928, height: 1174 });
  checks.push({ name: 'actual resize persisted across full app exit and relaunch', pass: true, detail: size });
  const assets = await c.ev(`fetch(${JSON.stringify(status.api_base_url + '/api/assets')}).then(async r=>{if(!r.ok)throw Error(r.status);return r.json()})`);
  if (defaults) {
    assert(!JSON.stringify(assets).includes('desktop_business'));
    checks.push({ name: 'default resource lookup uses isolated fresh business data', pass: true });
  } else {
    assert(JSON.stringify(assets).includes('desktop_business'));
    checks.push({ name: 'final app sees retained business history', pass: true });
  }
  const journal = JSON.parse(await fs.readFile(path.join(config.data_directory, 'desktop-session.json')));
  await c.invoke('stop_runtime');
  for (let attempts = 0; (await c.invoke('runtime_status')).status !== 'stopped'; attempts++) {
    assert(attempts < 300, 'Stop timed out'); await sleep(100);
  }
  for (const owned of [journal.control_pid, ...journal.workers.map(worker => worker.pid)]) {
    assert.throws(() => process.kill(owned, 0));
  }
  await c.ev(`setTimeout(()=>window.__TAURI__.core.invoke('request_exit'),100);true`);
  for (let attempts = 0; ; attempts++) {
    try { process.kill(pid, 0); } catch { break; }
    assert(attempts < 100, 'Exit timed out'); await sleep(100);
  }
  checks.push({ name: 'final delivery controlled exit removes owned backend and desktop', pass: true });
} catch (error) {
  checks.push({ name: 'final delivery acceptance', pass: false, error: String(error) });
  process.exitCode = 1;
} finally {
  c.close();
  const result = { checks, passed: checks.every(check => check.pass), pending: ['Actual notification icon click: current Shell UI Automation did not expose a clickable icon. Window hiding and duplicate-launch restoration were verified separately.'] };
  await fs.writeFile(path.join(directory, defaults ? 'defaults.json' : 'delivery.json'), JSON.stringify(result, null, 2));
  console.log(JSON.stringify(result));
}
