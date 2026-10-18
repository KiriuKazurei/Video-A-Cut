// Isolated regression probe using the existing review data and local static fixture.
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
const source = path.resolve(process.argv[2]);
const webui = path.join(source, 'webui');
const output = path.join(process.env.VAC_TEST_OUTPUT, 'docs-review-20261016');
fs.mkdirSync(output, { recursive: true });
const probe = path.join(output, 'timeline-focus-fixture.mjs');
let review = fs.readFileSync(path.join(webui, 'review_shortcuts_smoke.mjs'), 'utf8').split('// 1) ')[0];
assert.ok(review.includes('async function load'));
review += `
await load('#/editing/review');
await visible('.scene-card[data-focus-key="scene:s3"]').focus();
await page.waitForTimeout(150);
await visible('.timeline-clip').filter({ hasText: '场景二' }).first().focus();
const domFocus = await page.evaluate(() => document.activeElement?.textContent);
posts.length = 0;
await page.keyboard.press('c'); await page.waitForTimeout(500);
const sent = posts.find(p => /scene-reviews/.test(p.url))?.body;
const result = { probe: 'timeline-keyboard-focus', dom_focus: domFocus, confirmed_scene: sent?.scene_id,
  previous_card: 's3', target_timeline_scene: 's2', reproduced: sent?.scene_id === 's3', scope: 'route-mocked local API; no backend write' };
await page.screenshot({ path: process.argv[3] + '/timeline-focus.png' });
console.log(JSON.stringify(result, null, 2));
await browser.close();
`;
fs.writeFileSync(probe, review);
let fixture = fs.readFileSync(path.join(webui, 'run-local-smokes.mjs'), 'utf8').replaceAll('\r\n', '\n').split('try {\n  const scripts')[0];
assert.ok(fixture.includes('const base ='));
fixture = fixture.replace('const here = path.dirname(fileURLToPath(import.meta.url));', `const here = ${JSON.stringify(webui)};`).replace("'review-smokes'", "'docs-review-20261016'");
fixture += `
try {
  const child = spawn(process.execPath, [${JSON.stringify(probe)}, base, output], { cwd: here, windowsHide: true });
  let captured = '';
  child.stdout.on('data', data => { captured += data; process.stdout.write(data); });
  child.stderr.pipe(process.stderr);
  const code = await new Promise((resolve, reject) => { child.once('error', reject); child.once('close', resolve); });
  fs.writeFileSync(path.join(output, 'timeline-focus.log'), captured);
  process.exitCode = code;
} finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
`;
const server = path.join(output, 'timeline-server.mjs');
fs.writeFileSync(server, fixture);
const child = spawn(process.execPath, [server], { windowsHide: true });
child.stdout.pipe(process.stdout); child.stderr.pipe(process.stderr);
process.exitCode = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); });
