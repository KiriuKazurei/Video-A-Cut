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
const fail = { alpha_profile: false, beta_profile: false }; const getCount = { alpha_profile: 0, beta_profile: 0 }; const startDelay = { ms: 0 };

await page.route(/\/api\/assets(\?.*)?$/, async (route) => { const r = await route.fetch(); const l = await r.json(); await route.fulfill({ response: r, json: l.map((a) => a.asset_id === '_selfcheck' ? { ...a, agent_visible: true } : a) }); });
await page.route(/\/api\/processing-profiles(\?.*)?$/, (route) => {
  if (route.request().method() === 'POST') { const { profile } = route.request().postDataJSON(); profiles[profile.profile_id] = profile; return route.fulfill({ json: profile }); }
  return route.fulfill({ json: Object.values(profiles) });
});
await page.route(/\/api\/processing-profiles\/([^/]+)\/revisions\/\d+$/, async (route) => {
  const id = decodeURIComponent(route.request().url().match(/processing-profiles\/([^/]+)\//)[1]);
  getCount[id]++;
  if (id === 'beta_profile' && gate.beta) await gate.beta; // 模拟载入慢
  if (fail[id]) return route.fulfill({ status: 500, json: { error: 'smoke: forced 500' } });
  route.fulfill({ json: { profile: profiles[id], profile_sha256: 'x' } });
});
await page.route(/\/api\/assets\/_selfcheck\/prepared-preflight$/, (route) => {
  const b = route.request().postDataJSON(); preflights.push(b);
  route.fulfill({ json: { schema_version: 1, profile_id: b.profile_id, profile_revision: b.revision, profile_sha256: 'x', status: 'ready', can_start: true, checks: [], expected_asset_version: 'av1' } });
});
await page.route(/\/api\/assets\/_selfcheck\/prepared-workflows$/, async (route) => { starts.push(route.request().postDataJSON()); if (startDelay.ms) await new Promise((r) => setTimeout(r, startDelay.ms)); route.fulfill({ status: 409, json: { error: 'smoke: not applied' } }); });

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

// b') 另一种路径：不经设置页，仅预设列表里版本变化（点页头「刷新」= 重新拉取预设列表，确定性触发，不依赖 15s 陈旧窗口）
const refreshList = () => page.locator('button:has-text("刷新"):visible').first().click();
const enabled = () => page.waitForFunction(() => { const b = [...document.querySelectorAll('.page:not([hidden]) .quickstart-card button')].find((x) => x.textContent.includes('开始生成')); return b && !b.disabled; });
profiles.alpha_profile = mk('alpha_profile', 'Alpha 预设', 3);
await refreshList();
await page.waitForFunction(() => /Alpha 预设 · v3/.test(document.querySelector('.page:not([hidden]) .quickstart-card .select-saved-profile')?.textContent ?? ''));
await enabled(); await page.waitForTimeout(300);
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts[0].revision, 3);
ok('b) preset list revision change alone (refresh) also reloads the draft → request revision = 3');

// 同一拍内双击「开始生成」只发 1 次启动请求（启动响应故意延迟，排除「第一次已完成」的假象）
startDelay.ms = 700; starts.length = 0;
await startBtn.dblclick(); await page.waitForTimeout(1200);
assert.equal(starts.length, 1, `double click sent ${starts.length} start requests`);
startDelay.ms = 0;
ok('dblclick on 开始生成 → exactly 1 start request');

// c) 强制 getProfile 返回 500：失败后不得无限重试（Gate 实测过 4s 内 2875 次）
const errAlert = card.locator('.quickstart-load-error');
const observe = async (id, ms = 4000) => { await page.waitForTimeout(ms); return getCount[id]; }; // 计数含首次尝试，调用前自行清零
// (i) 切换预设时载入失败
await page.waitForTimeout(800);
fail.beta_profile = true; getCount.beta_profile = 0;
await pick('Beta 预设');
const n1total = await observe('beta_profile');
assert.ok(n1total <= 3, `(i) switch failure: ${n1total} getProfile requests in 4s`);
await errAlert.waitFor();
assert.equal(await startBtn.isDisabled(), true, 'start disabled after load failure');
assert.match(await errAlert.innerText(), /读取预设失败/);
await shot('c1-load-failure-on-switch');
assert.ok(n1total <= 3, `(i) switch failure: ${n1total} getProfile requests in 4s`);
console.log(`   measured (i) switch failure: ${n1total} getProfile requests in total (incl. first attempt) over error + 4s`);
// 恢复：重试
fail.beta_profile = false;
await errAlert.locator('button:has-text("重试")').click();
await enabled(); await errAlert.waitFor({ state: 'detached' });
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts[0].profile_id, 'beta_profile');
ok('(i) 500 on preset switch: ≤3 requests in 4s, start disabled + error with 重试; 重试 recovers → request = beta_profile');
// 恢复：失败后换一个预设
fail.beta_profile = true; await pick('Alpha 预设'); await enabled(); await page.waitForTimeout(300);
await pick('Beta 预设'); await errAlert.waitFor();
await pick('Alpha 预设'); await enabled(); await errAlert.waitFor({ state: 'detached' });
fail.beta_profile = false;
ok('(i) after a failure, switching to another preset recovers');

// (ii) 列表版本变化时载入失败（Gate 复现路径：保存 v5 后 getProfile 500）
await page.waitForTimeout(500);
fail.alpha_profile = true; profiles.alpha_profile = mk('alpha_profile', 'Alpha 预设', 5); getCount.alpha_profile = 0;
await refreshList();
const n2before = await observe('alpha_profile');
assert.ok(n2before <= 3, `(ii) list-revision failure: ${n2before} getProfile requests in 4s`);
await errAlert.waitFor();
assert.equal(await startBtn.isDisabled(), true);
assert.ok(n2before <= 3, `(ii) list-revision failure: ${n2before} getProfile requests in 4s`);
await shot('c2-load-failure-on-list-revision');
console.log(`   measured (ii) list-revision failure: ${n2before} getProfile requests in total (incl. first attempt) over error + 4s`);
// 恢复 1：列表版本再变化（v6）
fail.alpha_profile = false; profiles.alpha_profile = mk('alpha_profile', 'Alpha 预设', 6);
await refreshList(); await enabled(); await errAlert.waitFor({ state: 'detached' });
assert.match((await shown()).picked, /v6/);
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts[0].revision, 6);
// 恢复 2：显式重试
fail.alpha_profile = true; profiles.alpha_profile = mk('alpha_profile', 'Alpha 预设', 7);
await refreshList(); await errAlert.waitFor();
fail.alpha_profile = false; await errAlert.locator('button:has-text("重试")').click(); await enabled();
starts.length = 0; await startBtn.click(); await page.waitForTimeout(400);
assert.equal(starts[0].revision, 7);
ok('(ii) 500 on list-revision change: ≤3 requests in 4s, start disabled; recovers on newer list revision and on 重试');

await browser.close();
console.log(`quickstart smoke passed (${n} groups)`);
