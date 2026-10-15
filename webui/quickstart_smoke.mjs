// 导入页「开始生成」卡片：卡片上显示的预设/版本 == 点「开始生成」时请求体里的 profile_id/revision。
//  a) 下拉换成 Beta 但没点任何「加载」：自动载入；载入未完成期间按钮禁用；点击后请求体必须是 Beta。
//  b) 在设置页保存 Alpha v2 后回到卡片：卡片显示 v2，请求体 revision 必须是 2（不是陈旧的 1）。
// 用法：PLAYWRIGHT_CORE=... CHROME=... node quickstart_smoke.mjs http://127.0.0.1:8816 [截图目录]
// 预设、预检、启动全部用 Playwright 路由模拟，不改后端数据；「启动」只记录请求体，不发给后端。
import assert from 'node:assert/strict';
import fs from 'node:fs';
const base = process.argv[2] ?? 'http://127.0.0.1:8816';
const shots = process.argv[3];
if (shots) fs.mkdirSync(shots, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_CORE ?? 'playwright-core');
const browser = await chromium.launch({ executablePath: process.env.CHROME, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
await context.addInitScript(() => { try { localStorage.setItem('vac_tour_seen_v1', '1'); } catch { /* ignore */ } });
const page = await context.newPage();

const mk = (id, name, revision) => ({ schema_version: 1, profile_id: id, revision, name, content_mode: 'builtin', vision: { adapter: 'builtin' }, narration: { adapter: 'builtin' }, sampling: { max_frames: 12, max_bytes: 12582912, timeout_seconds: 60 }, tts_voice: 'v', export_target: 'premiere' });
const profiles = { alpha_profile: mk('alpha_profile', 'Alpha 预设', 1), beta_profile: mk('beta_profile', 'Beta 预设', 1) };
const starts = []; const preflights = []; const gate = { beta: null };

await page.route(/\/api\/assets(\?.*)?$/, async (route) => { const r = await route.fetch(); const l = await r.json(); await route.fulfill({ response: r, json: l.map((a) => a.asset_id === '_selfcheck' ? { ...a, agent_visible: true } : a) }); });
await page.route(/\/api\/processing-profiles(\?.*)?$/, (route) => {
  if (route.request().method() === 'POST') { const { profile } = route.request().postDataJSON(); profiles[profile.profile_id] = profile; return route.fulfill({ json: profile }); }
  return route.fulfill({ json: Object.values(profiles) });
});
await page.route(/\/api\/processing-profiles\/([^/]+)\/revisions\/\d+$/, async (route) => {
  const id = decodeURIComponent(route.request().url().match(/processing-profiles\/([^/]+)\//)[1]);
  if (id === 'beta_profile' && gate.beta) await gate.beta; // 模拟载入慢
  route.fulfill({ json: { profile: profiles[id], profile_sha256: 'x' } });
});
await page.route(/\/api\/assets\/_selfcheck\/prepared-preflight$/, (route) => {
  const b = route.request().postDataJSON(); preflights.push(b);
  route.fulfill({ json: { schema_version: 1, profile_id: b.profile_id, profile_revision: b.revision, profile_sha256: 'x', status: 'ready', can_start: true, checks: [], expected_asset_version: 'av1' } });
});
await page.route(/\/api\/assets\/_selfcheck\/prepared-workflows$/, (route) => { starts.push(route.request().postDataJSON()); route.fulfill({ status: 409, json: { error: 'smoke: not applied' } }); });

const card = page.locator('.page:not([hidden]) .quickstart-card');
const startBtn = card.locator('button:has-text("开始生成")');
const picker = card.locator('.select-saved-profile');
let n = 0; const ok = (m) => console.log(`ok ${++n} ${m}`);
const shot = async (name) => { if (shots) await page.screenshot({ path: `${shots}/${name}.png` }); };
const shown = async () => ({ picked: (await picker.innerText()).trim() });
async function pick(label) { await picker.click(); await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option', { hasText: label }).click(); }

await page.goto(`${base}/#/editing/import`, { waitUntil: 'domcontentloaded' });
await card.waitFor();
await startBtn.waitFor();
await page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
assert.match((await shown()).picked, /Alpha 预设 · v1/);

// 技术词不再出现在卡片里
const cardText = await card.innerText();
for (const w of ['历史版本', '加载版本', '新建预设']) assert.equal(cardText.includes(w), false, `card still shows「${w}」`);
ok('quickstart card shows only a simple preset dropdown (no 历史版本/加载版本/新建预设)');

// a) 换成 Beta：没有点任何加载按钮；故意让 Beta 的载入慢，期间「开始生成」必须禁用（安全网）
let release; gate.beta = new Promise((r) => { release = r; });
await pick('Beta 预设');
await page.waitForTimeout(300);
assert.match((await shown()).picked, /Beta 预设/);
assert.equal(await startBtn.isDisabled(), true, 'start must be disabled while the picked preset is still loading');
await shot('a1-picker-changed-loading-disabled');
release(); gate.beta = null;
await page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
await page.waitForTimeout(200);
await shot('a2-picker-changed-loaded');
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts.length, 1); assert.equal(starts[0].profile_id, 'beta_profile'); assert.equal(starts[0].revision, 1);
assert.deepEqual(preflights.at(-1), { profile_id: 'beta_profile', revision: 1 });
ok('a) picker=Beta without any load click → start disabled while loading, then request body = beta_profile / v1');

// 换回 Alpha 再启动一次：请求体 = alpha_profile / v1
await pick('Alpha 预设'); await page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
await page.waitForTimeout(200);
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts[0].profile_id, 'alpha_profile'); assert.equal(starts[0].revision, 1);
ok('a) switching back to Alpha → request body = alpha_profile / v1');

// b) 在设置页保存 Alpha v2（真实的 PreparationPanel 保存流程），回到卡片
await page.goto('about:blank'); // 重新进入，保证从干净状态开始
await page.goto(`${base}/#/editing/import`, { waitUntil: 'domcontentloaded' }); await card.waitFor();
await page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
assert.match((await shown()).picked, /Alpha 预设 · v1/);
await page.evaluate(() => { location.hash = '#/settings/profiles'; });
await page.waitForSelector('.page:not([hidden]) button[data-nav-anchor="profile-save"]');
const settingsPicker = page.locator('.page:not([hidden]) .select-saved-profile').first();
await settingsPicker.click();
await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option', { hasText: 'Alpha 预设' }).click();
await page.locator('.page:not([hidden]) button:has-text("加载版本")').click(); await page.waitForTimeout(400);
await page.locator('.page:not([hidden]) button[data-nav-anchor="profile-save"]').click(); await page.waitForTimeout(800);
assert.equal(profiles.alpha_profile.revision, 2, 'settings save produced v2');
await page.evaluate(() => { location.hash = '#/editing/import'; });
await card.waitFor();
await page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
await page.waitForTimeout(300);
assert.match((await shown()).picked, /Alpha 预设 · v2/);
await shot('b1-card-after-v2-save');
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts.length, 1); assert.equal(starts[0].profile_id, 'alpha_profile'); assert.equal(starts[0].revision, 2, 'start must carry v2, not stale v1');
assert.deepEqual(preflights.at(-1), { profile_id: 'alpha_profile', revision: 2 });
ok('b) after saving v2 in Settings the card reloads → shows v2 and request body revision = 2');

// b') 另一种路径：不经设置页，仅预设列表里版本变化（列表重新拉取）也要重新载入
profiles.alpha_profile = mk('alpha_profile', 'Alpha 预设', 3);
await page.evaluate(() => { Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' }); document.dispatchEvent(new Event('visibilitychange')); });
await page.waitForFunction(() => /Alpha 预设 · v3/.test(document.querySelector('.page:not([hidden]) .quickstart-card .select-saved-profile')?.textContent ?? ''));
await page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
await page.waitForTimeout(300);
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts[0].revision, 3);
ok('b) preset list revision change alone (refetch) also reloads the draft → request revision = 3');

await browser.close();
console.log(`quickstart smoke passed (${n} groups)`);
