// 统一导航冒烟：阶段条 / 面包屑 / 下一步 的跳转目标、禁用原因、深链与 DOM 嵌套。
// 用法：PLAYWRIGHT_CORE=/path/to/playwright-core/index.mjs CHROME=/usr/bin/google-chrome node navigation_smoke.mjs http://127.0.0.1:8816 [截图目录]
// 需要演示数据：资产 _selfcheck（EDL 包、Agent 不可见、无流程）。流程状态用 Playwright 路由拦截模拟，不改后端数据。
import assert from 'node:assert/strict';
import fs from 'node:fs';
const base = process.argv[2] ?? 'http://127.0.0.1:8816';
const shots = process.argv[3];
const { chromium } = await import(process.env.PLAYWRIGHT_CORE ?? 'playwright-core');
const browser = await chromium.launch({ executablePath: process.env.CHROME, args: ['--no-sandbox'] });
const page = await (await browser.newContext({ viewport: { width: 1600, height: 1000 } })).newPage();
const warnings = [];
page.on('console', (m) => { if (['warning', 'error'].includes(m.type()) && /validateDOMNesting|cannot (be a )?descendant|nested/i.test(m.text())) warnings.push(m.text()); });
if (shots) fs.mkdirSync(shots, { recursive: true });

const state = { visible: false, workflow: null };
await page.route(/\/api\/assets(\?.*)?$/, async (route) => {
  const response = await route.fetch(); const list = await response.json();
  await route.fulfill({ response, json: list.map((a) => a.asset_id === '_selfcheck' ? { ...a, agent_visible: state.visible } : a) });
});
await page.route('**/api/assets/_selfcheck/workflows**', (route) => route.fulfill({ json: state.workflow ? [state.workflow] : [] }));
const wf = (status, stage) => ({ run_id: 'run_smoke', asset_id: '_selfcheck', base_revision_id: 'b', current_revision_id: 'c', status, stage, version: 1, content_mode: 'builtin' });

const hash = () => page.evaluate(() => location.hash);
const crumbs = () => page.$$eval('.context-crumbs li', (l) => l.map((x) => x.textContent.replace('›', '').trim()));
const nextBtn = () => page.locator('.next-step-btn');
const step = (label) => page.locator('.pipeline-step-item', { hasText: label });
const anchor = () => page.evaluate(() => document.activeElement?.getAttribute('data-nav-anchor'));
async function load(h) {
  await page.goto('about:blank');
  await page.goto(`${base}/${h}`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('.context-bar'); await page.waitForTimeout(900);
}
async function shot(name) { if (shots) await page.screenshot({ path: `${shots}/${name}.png`, clip: { x: 0, y: 0, width: 1024 * 1.5625, height: 100 } }); }
let n = 0; const ok = (m) => console.log(`ok ${++n} ${m}`);

// 1) 五个工作区的面包屑形态一致；深链与旧路径仍可用
for (const [h, want] of [['#/resources', ['资源管理', '_selfcheck']], ['#/editing/review', ['编辑', '审查', '_selfcheck']], ['#/export/tasks', ['导出', '任务监控', '_selfcheck']],
  ['#/settings/preparation', ['设置', '运行准备', '_selfcheck']], ['#/settings/logs', ['设置', '日志与诊断']], ['#/tts', ['TTS', '声音调控台']], ['#/import', ['编辑', '导入与粗剪', '_selfcheck']], ['#/monitor', ['导出', '任务监控', '_selfcheck']]]) {
  await load(h);
  assert.deepEqual(await crumbs(), want, h);
  assert.equal(await page.locator('.context-bar h1').count(), 1);
  assert.equal(await page.locator('.context-crumbs [aria-current="page"]').count(), 1);
}
ok('breadcrumbs on all workspaces + legacy deep links');

// 2) Agent 不可见 → 下一步禁用并写明原因
await load('#/resources');
assert.equal(await nextBtn().isDisabled(), true);
await page.locator('.next-step-wrap').hover();
await page.waitForSelector('.ant-tooltip:not(.ant-tooltip-hidden)');
assert.match(await page.locator('.ant-tooltip:not(.ant-tooltip-hidden)').innerText(), /对 Agent 不可见.*设为 Agent 可见/);
await shot('blocked-tooltip');
assert.match(await step('准备').getAttribute('aria-label'), /被阻塞.*对 Agent 不可见/);
ok('blocked next step is disabled with the exact reason (tooltip + stage bar)');

// 3) 可见后：去运行准备 → 跳到设置/运行准备并聚焦「运行条件」，资产不变
state.visible = true; await load('#/resources');
assert.equal(await nextBtn().isDisabled(), false);
assert.equal((await nextBtn().innerText()).trim(), '去运行准备');
await nextBtn().click(); await page.waitForTimeout(700);
assert.equal(await hash(), '#/settings/preparation');
assert.equal(await anchor(), 'prepare-start');
assert.deepEqual(await crumbs(), ['设置', '运行准备', '_selfcheck']);
assert.equal((await nextBtn().innerText()).trim(), '开始生成');
assert.match(await step('准备').getAttribute('class'), /is-current/);
ok('去运行准备 → settings/preparation, anchor focused, asset kept, label becomes 开始生成');

// 4) 待确认场景：阶段条标出下一步；点击下一步去审查并聚焦
state.workflow = wf('awaiting_review', 'scene_review'); await load('#/resources');
assert.equal((await nextBtn().innerText()).trim(), '去确认场景');
assert.match(await step('审查').getAttribute('class'), /is-next/);
assert.match(await step('生成').getAttribute('class'), /is-done/);
await shot('awaiting-scene');
await nextBtn().click(); await page.waitForTimeout(700);
assert.equal(await hash(), '#/editing/review');
assert.equal(await anchor(), 'review');
assert.match(await step('审查').getAttribute('class'), /is-current/);
ok('awaiting scene review → 去确认场景 → editing/review (focused)');

// 5) 阶段条每一步都去它真正发生的页面
for (const [label, want, anc] of [['导入与粗剪', '#/editing/import', 'ingest'], ['准备', '#/settings/preparation', 'prepare-start'], ['生成', '#/export/tasks', 'tasks'], ['审查', '#/editing/review', 'review'], ['交付', '#/export/delivery', 'delivery']]) {
  await step(label).click(); await page.waitForTimeout(600);
  assert.equal(await hash(), want, label); assert.equal(await anchor(), anc, label);
  assert.deepEqual((await crumbs()).slice(-1), ['_selfcheck']);
}
ok('every stage navigates to its page and focuses its section');

// 6) 面包屑可点击：回到分区 / 工作区 / 资产
await load('#/editing/review');
await page.locator('.context-crumbs a', { hasText: '编辑' }).click(); await page.waitForTimeout(400);
assert.match(await hash(), /^#\/editing\//);
await page.locator('.context-crumbs a', { hasText: '编辑' }).waitFor({ state: 'detached' }).catch(() => undefined);
await load('#/editing/review');
await page.locator('.context-crumbs a', { hasText: '_selfcheck' }).click(); await page.waitForTimeout(400);
assert.equal(await hash(), '#/resources');
ok('breadcrumb segments navigate');

// 7) 生成中 / 失败 / 已取消 / 待交付
for (const [w, label, stage] of [[wf('running', 'recognize'), '查看生成进度', '生成'], [wf('failed', 'narrate'), '查看失败并重试', '审查'], [wf('cancelled', 'recognize'), '重新启动流程', '准备'], [wf('ready_for_acceptance', 'acceptance'), '去交付与验收', '交付']]) {
  state.workflow = w; await load('#/resources');
  assert.equal((await nextBtn().innerText()).trim(), label, w.status);
  assert.match(await step(stage).getAttribute('class'), /is-next/, w.status);
  await shot(`next-${w.status}`);
}
ok('running / failed / cancelled / ready_for_acceptance next steps');

// 8) 二级菜单带阶段标记；没有 button 嵌套 button
await load('#/editing/import');
assert.ok(await page.locator('.ant-tabs-tab .stage-dot').count() >= 2);
await load('#/settings/preparation');
assert.equal(await page.locator('.ant-menu-item .stage-dot').count(), 1);
for (const h of ['#/resources', '#/editing/import', '#/editing/review', '#/export/delivery', '#/export/tasks', '#/settings/preparation', '#/tts']) {
  await load(h);
  assert.equal(await page.evaluate(() => document.querySelectorAll('button button, a button, button a').length), 0, h);
}
assert.deepEqual(warnings, []);
ok('secondary menus carry stage dots; no nested interactive elements; no nesting warnings');
await browser.close();
console.log(`navigation smoke passed (${n} groups)`);
