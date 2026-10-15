// 审查面板：「内容编辑」与「审查」的差异 + 审查快捷键（C 确认场景 / A 批准解说 / N 下一待处理）。
// 用法：PLAYWRIGHT_CORE=... CHROME=... node review_shortcuts_smoke.mjs http://127.0.0.1:8816
// 流程与审查数据用 Playwright 路由拦截模拟，不改后端数据；写操作只记录请求体，不发给后端。
import assert from 'node:assert/strict';
const base = process.argv[2] ?? 'http://127.0.0.1:8816';
const { chromium } = await import(process.env.PLAYWRIGHT_CORE ?? 'playwright-core');
const browser = await chromium.launch({ executablePath: process.env.CHROME, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
await context.addInitScript(() => { try { localStorage.setItem('vac_tour_seen_v1', '1'); } catch { /* ignore */ } });
const page = await context.newPage();
const posts = [];
const run = (stage) => ({ run_id: 'run_kb', asset_id: '_selfcheck', base_revision_id: 'b', current_revision_id: 'rev1', status: 'awaiting_review', stage, version: 3, content_mode: 'builtin' });
const state = { stage: 'scene_review' };
const review = () => ({ run: run(state.stage), revision: { revision_id: 'rev1', package_ref: 'p', edl_sha256: 's' },
  scenes: [{ scene_id: 's1', label: '场景一', decision: 'confirmed', sequence_rank: 0 }, { scene_id: 's2', label: '场景二', sequence_rank: 1 }, { scene_id: 's3', label: '场景三', sequence_rank: 2 }],
  narration: [{ id: 'n1', text: '第一句', start: 0, end: 2, source_scene_id: 's1', approved_hash: 'h' }, { id: 'n2', text: '第二句', start: 2, end: 4, source_scene_id: 's2' }], evidence_files: [] });
await page.route(/\/api\/assets(\?.*)?$/, async (route) => { const r = await route.fetch(); const l = await r.json(); await route.fulfill({ response: r, json: l.map((a) => a.asset_id === '_selfcheck' ? { ...a, agent_visible: true } : a) }); });
await page.route('**/api/assets/_selfcheck/workflows**', (route) => route.fulfill({ json: [run(state.stage)] }));
await page.route('**/api/workflows/run_kb', (route) => route.fulfill({ json: { run: run(state.stage), stages: [], tasks: [] } }));
await page.route('**/api/workflows/run_kb/review', (route) => route.fulfill({ json: review() }));
await page.route('**/api/workflows/run_kb/acceptance', (route) => route.fulfill({ json: [] }));
await page.route(/\/api\/workflows\/run_kb\/(scene|narration)-reviews$/, (route) => { posts.push({ url: route.request().url().replace(/^.*\/api/, '/api'), body: route.request().postDataJSON() }); route.fulfill({ status: 409, json: { error: 'smoke: not applied' } }); });

const visible = (sel) => page.locator(`.page:not([hidden]) ${sel}:visible`);
let n = 0; const ok = (m) => console.log(`ok ${++n} ${m}`);
async function load(h) { await page.goto('about:blank'); await page.goto(`${base}/${h}`, { waitUntil: 'domcontentloaded' }); await page.waitForSelector('.context-bar'); await page.waitForTimeout(1200); }

// 1) 内容编辑：没有确认/批准按钮，但有改稿表单；审查：有按钮、有待处理提示
await load('#/editing/content');
assert.match(await visible('.workflow-panel #workflow-title').innerText(), /内容编辑/);
assert.equal(await visible('button:has-text("确认此场景")').count(), 0);
assert.equal(await visible('button:has-text("保存新修订")').count() >= 1, true);
assert.equal(await visible('.review-todo button:has-text("去审查")').count(), 1);
await load('#/editing/review');
assert.match(await visible('.workflow-panel #workflow-title').innerText(), /审查/);
assert.equal(await visible('button:has-text("确认此场景")').count(), 3);
assert.match(await visible('.review-todo').innerText(), /还有 2 个场景待确认/);
ok('内容编辑 hides approval buttons; 审查 shows them plus a pending counter');

// 2) N 跳到下一个待处理场景（循环），C 确认当前聚焦的场景
await page.locator('body').click({ position: { x: 5, y: 400 } });
await page.keyboard.press('n'); await page.waitForTimeout(300);
assert.match(await visible('.workflow-card.is-focused').innerText(), /场景二/);
await page.keyboard.press('n'); await page.waitForTimeout(300);
assert.match(await visible('.workflow-card.is-focused').innerText(), /场景三/);
await page.keyboard.press('n'); await page.waitForTimeout(300);
assert.match(await visible('.workflow-card.is-focused').innerText(), /场景二/);
await page.keyboard.press('c'); await page.waitForTimeout(500);
const confirm = posts.find((p) => /scene-reviews/.test(p.url));
assert.ok(confirm, 'confirm request sent'); assert.equal(confirm.body.scene_id, 's2'); assert.equal(confirm.body.decision, 'confirmed'); assert.equal(confirm.body.expected_version, 3);
ok('N cycles pending scenes; C confirms the focused scene (request body checked)');

// 3) 打字时不触发：焦点在输入框里按 c/n/a 不会发请求，也不会移动聚焦
posts.length = 0;
await visible('input[aria-label="场景标签"]').first().click();
await page.keyboard.type('can'); await page.waitForTimeout(400);
assert.equal(posts.length, 0);
ok('typing c/a/n in an input does not fire shortcuts');

// 4) 解说阶段：A 批准当前聚焦（或第一条待批准）解说；非闸口阶段 C 不动作
state.stage = 'draft_review'; await load('#/editing/review');
await page.locator('body').click({ position: { x: 5, y: 400 } });
posts.length = 0;
await page.keyboard.press('c'); await page.waitForTimeout(300);
assert.equal(posts.length, 0);
await page.keyboard.press('a'); await page.waitForTimeout(500);
const approve = posts.find((p) => /narration-reviews/.test(p.url));
assert.ok(approve); assert.equal(approve.body.narration_id, 'n2');
ok('A approves the first unapproved line; C is ignored outside the scene gate');

// 5) 内容编辑页按 C/A 不会确认
posts.length = 0; await load('#/editing/content');
await page.locator('body').click({ position: { x: 5, y: 400 } });
await page.keyboard.press('a'); await page.keyboard.press('c'); await page.waitForTimeout(400);
assert.equal(posts.length, 0);
ok('content-edit page ignores C/A');

// 6) 快捷键帮助里列出新快捷键
await page.keyboard.press('?'); await page.waitForSelector('.ant-modal');
const text = await page.locator('.ant-modal').innerText();
assert.match(text, /确认当前场景/); assert.match(text, /批准当前解说/); assert.match(text, /下一个待处理/);
ok('shortcut help lists C / A / N');
await browser.close();
console.log(`review shortcuts smoke passed (${n} groups)`);
