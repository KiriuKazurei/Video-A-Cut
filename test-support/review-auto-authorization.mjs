// Generate a focused probe from the existing mock fixture; never modify application source.
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';

const source = path.resolve(process.argv[2]);
const output = path.join(process.env.VAC_TEST_OUTPUT, 'docs-review-20261016');
fs.mkdirSync(output, { recursive: true });
const file = path.join(output, 'authorization-fixture.mjs');
const webui = path.join(source, 'webui');
let fixture = fs.readFileSync(path.join(webui, 'auto_ui_smoke.mjs'), 'utf8').split('// 1) ')[0];
assert.ok(fixture.includes('await card.locator(\'.auto-notice\').waitFor();'));
fixture = fixture.replace('const here = path.dirname(fileURLToPath(import.meta.url));', `const here = ${JSON.stringify(webui)};`);
fixture += `
const authBox = card.locator('.auto-authorize input[type=checkbox]');
await authBox.check();
await page.waitForFunction(() => !document.querySelector('.page:not([hidden]) .auto-start-btn')?.disabled);
profiles[0].revision = 3;
NOTICES.cloud_cut = { ...NOTICES.cloud_cut, notice_sha256: 'new-revision-sha', notice: 'REVIEW NEW VERSION 3: different disclosure and provider destination' };
await page.locator('.app-header-right button').filter({ hasText: '刷新' }).first().click();
await page.waitForFunction(() => document.querySelector('[data-testid="auto-notice-text"]')?.textContent.includes('REVIEW NEW VERSION 3'));
await wait(150);
const checked = await authBox.isChecked();
const enabled = await startBtn.isEnabled();
mk.startMode = 'fail500';
if (enabled) { await startBtn.click(); await page.waitForFunction(() => document.querySelector('.auto-start-error')); }
const result = { probe: 'authorization-profile-revision', old_revision: 2, new_revision: 3, authorization_still_checked: checked,
  start_still_enabled: enabled, sent: mk.bodies.start.at(-1), reproduced: checked && enabled, scope: 'local mock API, no provider requests' };
fs.writeFileSync(path.join(shots, 'authorization-result.json'), JSON.stringify(result, null, 2));
await shot('authorization-v3');
console.log(JSON.stringify(result, null, 2));
await browser.close();
for (const response of mk.sse) response.end();
await new Promise(resolve => server.close(resolve));
`;
fs.writeFileSync(file, fixture);
const child = spawn(process.execPath, [file, output], { windowsHide: true, cwd: webui });
child.stdout.pipe(process.stdout); child.stderr.pipe(process.stderr);
const code = await new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve); });
process.exitCode = code;
