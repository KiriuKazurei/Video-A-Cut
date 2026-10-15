// 自动模式（一句话剪辑）前端冒烟：自带静态服务器（dist）+ 内存 mock 后端（含真实 SSE），不依赖 Go 控制面。
// 覆盖：授权勾选后才可启动 / 外发提示 / 放宽阈值需确认 / 高·中·低三档面板 / 决策卡 export·revise / 撤销 / 失败不死循环 / 双击只发一次。
// 用法：先 npm run build，再  CHROME=/usr/bin/google-chrome node auto_ui_smoke.mjs [截图目录=../.run-data/auto-ui]
import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const dist = process.env.VAC_DIST ?? path.join(here, 'dist');
const shots = process.argv[2] ?? path.join(here, '..', '.run-data', 'auto-ui');
fs.mkdirSync(shots, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_CORE ?? 'playwright-core');

// ---------- mock 后端 ----------
const now = () => new Date().toISOString();
const asset = { asset_id: 'clip_auto', status: 'ingested', agent_visible: true, human_approved: true, locked: false, allowed_agents: [], artifacts: {}, created_at: now(), updated_at: now(), input_kind: 'edl_package' };
const mkProfile = (id, name, revision, mode = 'configured') => ({ schema_version: 1, profile_id: id, revision, name, content_mode: mode, vision: { adapter: 'vision', api_format: 'openai', endpoint: 'https://api.example.com/v1', model: 'm', token_env: 'VAC_VISION_TOKEN' }, narration: { adapter: 'narration', api_format: 'openai', endpoint: 'https://api.example.com/v1', model: 'm', token_env: 'VAC_NARRATION_TOKEN' }, sampling: { max_frames: 12, max_bytes: 12582912, timeout_seconds: 60 }, tts_voice: 'v', export_target: 'premiere' });
const profiles = [mkProfile('cloud_cut', '云端剪辑', 2), mkProfile('local_cut', '本机剪辑', 1), mkProfile('regress', '工程回归', 1, 'builtin')];
const NOTICES = {
  cloud_cut: { external: true, notice_sha256: 'sha-cloud', notice: '自动模式将在无逐步确认的情况下完成剪辑、配音与字幕，并向以下外部端点发送数据：抽帧图像、场景标签、旁白文本和你的需求描述。\n视觉识别：api.example.com\n解说生成：api.example.com' },
  local_cut: { external: false, notice_sha256: 'sha-local', notice: '自动模式将在无逐步确认的情况下完成剪辑、配音与字幕。所有端点均为本机地址，不向外部发送数据。' }
};
const mk = {
  run: null, auto: null, autoFail: 0, autoNotFound: false, startMode: 'ok', startFail: 0, noticeFail: 0,
  calls: { auto: 0, start: 0, decision: 0, revoke: 0, notice: 0 }, bodies: { start: [], decision: [], revoke: [] }, sse: new Set()
};
const baseAuto = (over = {}) => ({
  run_id: 'run_auto1', auto_version: 4, state: 'active', brief: '做个高光集锦', scope: { external_egress: true, max_rounds: 1, max_total_rounds: 5, thresholds: {}, relaxed_thresholds: false },
  round: 0, max_rounds: 1, total_rounds: 0, max_total_rounds: 5, delivery_hold: false, latest_review: null,
  delivery: { delivered: false, package_ready: false, preview_is_current: false, preview_preset: 'mp4-720p-v1', spec: '720p' }, decision: null, delivery_hold_enforced: false, ...over
});
const review = (band, score, codes = []) => ({ review_id: 'rv1', run_id: 'run_auto1', round: 0, revision_id: 'rev1', score, band, reasons: codes.map(code => ({ code, severity: 'warn', improvable: true })) });
const delivered = { delivered: true, package_ready: true, preview_id: 'pv1', preview_status: 'succeeded', preview_is_current: true, preview_preset: 'mp4-720p-v1', spec: '720p' };
const newRun = (stage, status = 'running') => ({ run_id: 'run_auto1', asset_id: 'clip_auto', base_revision_id: 'b1', current_revision_id: 'rev1', status, stage, version: 7, content_mode: 'configured' });
const push = (name, data = {}) => { for (const r of mk.sse) r.write(`event: ${name}\ndata: ${JSON.stringify(data)}\n\n`); };
const setAuto = (auto, run) => { if (auto) mk.auto = auto; if (run) mk.run = run; push('auto.changed', { run_id: 'run_auto1' }); push('workflow.changed', { asset_id: 'clip_auto', run_id: 'run_auto1' }); };

const json = (res, status, body) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
const err = (res, status, code, message) => json(res, status, { error: { code, message } });
const readBody = (req) => new Promise((r) => { let b = ''; req.on('data', (c) => (b += c)); req.on('end', () => r(b ? JSON.parse(b) : {})); });

async function api(req, res, url) {
  const p = url.pathname; const m = req.method;
  if (p === '/api/events') { res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', Connection: 'keep-alive' }); res.write(': ok\n\n'); mk.sse.add(res); req.on('close', () => mk.sse.delete(res)); return; }
  if (p === '/api/ingest-roots') return json(res, 200, { configured: false, roots: [], policy: {}, ingesters: [] });
  if (p === '/api/assets' && m === 'GET') return json(res, 200, [asset]);
  if (p === '/api/assets/clip_auto' && m === 'GET') return json(res, 200, asset);
  if (p === '/api/assets/clip_auto/workflows') return json(res, 200, mk.run ? [mk.run] : []);
  if (p === '/api/assets/clip_auto/files' || p === '/api/assets/clip_auto/recordings') return json(res, 200, []);
  if (p === '/api/processing-profiles' && m === 'GET') return json(res, 200, profiles);
  let g;
  if ((g = p.match(/^\/api\/processing-profiles\/([^/]+)\/revisions\/(\d+)\/auto-notice$/))) {
    mk.calls.notice++;
    if (mk.noticeFail > 0) { mk.noticeFail--; return err(res, 500, 'internal', 'boom'); }
    const n = NOTICES[g[1]]; if (!n) return err(res, 404, 'not_found', 'x');
    return json(res, 200, { profile_id: g[1], revision: Number(g[2]), profile_sha256: 'psha', ...n });
  }
  if ((g = p.match(/^\/api\/processing-profiles\/([^/]+)\/revisions\/\d+$/))) return json(res, 200, { profile: profiles.find(x => x.profile_id === g[1]), profile_sha256: 'psha' });
  if (p === '/api/assets/clip_auto/prepared-preflight') {
    const b = await readBody(req); const ext = b.profile_id === 'cloud_cut';
    return json(res, 200, { schema_version: 1, profile_id: b.profile_id, profile_revision: b.revision, profile_sha256: 'psha', status: ext ? 'blocked' : 'ready', can_start: !ext, expected_asset_version: 'av1',
      checks: ext ? [{ code: 'external_authorization', status: 'blocked', message: '该预设版本尚未授权外发' }] : [] });
  }
  if (p === '/api/assets/clip_auto/auto-runs' && m === 'POST') {
    const body = await readBody(req); mk.calls.start++; mk.bodies.start.push(body);
    if (mk.startMode === 'slow') await new Promise(r => setTimeout(r, 700));
    if (mk.startMode === 'fail500') return err(res, 500, 'internal', 'sql: boom');
    if (mk.startMode === 'conflict') return err(res, 409, 'conflict', 'service: auto_run_in_progress');
    mk.run = newRun('recognize'); mk.auto = baseAuto();
    return json(res, 201, { run: mk.run, auto: mk.auto });
  }
  if ((g = p.match(/^\/api\/workflows\/([^/]+)\/auto$/)) && m === 'GET') {
    if (!mk.run || g[1] !== mk.run.run_id) return err(res, 404, 'not_found', 'x');
    mk.calls.auto++;
    if (mk.autoNotFound) return err(res, 404, 'not_found', 'not an auto run');
    if (mk.autoFail > 0) return err(res, 500, 'internal', 'boom');
    return json(res, 200, mk.auto);
  }
  if ((g = p.match(/^\/api\/workflows\/([^/]+)\/auto\/decision$/)) && m === 'POST') {
    const body = await readBody(req); mk.calls.decision++; mk.bodies.decision.push(body);
    if (body.expected_auto_version !== mk.auto.auto_version) return err(res, 409, 'conflict', 'service: auto version conflict');
    if (body.decision === 'export') mk.auto = baseAuto({ state: 'completed', auto_version: mk.auto.auto_version + 1, latest_review: mk.auto.latest_review, delivery: delivered });
    else mk.auto = baseAuto({ state: 'active', auto_version: mk.auto.auto_version + 1, total_rounds: 1, latest_review: mk.auto.latest_review });
    mk.run = newRun(body.decision === 'export' ? 'acceptance' : 'recognize', body.decision === 'export' ? 'ready_for_acceptance' : 'running');
    push('auto.changed', { run_id: 'run_auto1' });
    return json(res, 200, { auto: mk.auto, run: mk.run });
  }
  if ((g = p.match(/^\/api\/workflows\/([^/]+)\/auto\/revoke$/)) && m === 'POST') {
    const body = await readBody(req); mk.calls.revoke++; mk.bodies.revoke.push(body);
    if (body.expected_auto_version !== mk.auto.auto_version) return err(res, 409, 'conflict', 'service: auto version conflict');
    mk.auto = baseAuto({ state: 'revoked', auto_version: mk.auto.auto_version + 1 });
    push('auto.changed', { run_id: 'run_auto1' });
    return json(res, 200, { auto: mk.auto });
  }
  if ((g = p.match(/^\/api\/workflows\/([^/]+)$/))) return mk.run && g[1] === mk.run.run_id ? json(res, 200, { run: mk.run, stages: [], tasks: [] }) : err(res, 404, 'not_found', 'x');
  if (/^\/api\/workflows\/[^/]+\/(review|acceptance)$/.test(p)) return /acceptance$/.test(p) ? json(res, 200, []) : err(res, 404, 'not_found', 'x');
  if (/^\/api\/workflows\/[^/]+\/previews$/.test(p) && m === 'GET') return json(res, 200, { run_id: 'run_auto1', revision_id: 'rev1', expected_version: 7, can_start: false, checks: [], previews: [] });
  if (/^\/api\/workflows\/[^/]+\/delivery-variants$/.test(p) && m === 'GET') return json(res, 200, { run_id: 'run_auto1', revision_id: 'rev1', expected_version: 7, can_start: false, checks: [], specs: [], variants: [] });
  if (p.startsWith('/api/previews') || p.endsWith('delivery.zip')) { res.writeHead(200, { 'Content-Type': 'application/octet-stream' }); return res.end('x'); }
  if (m === 'GET') return json(res, 200, []);
  return err(res, 404, 'not_found', 'no mock for ' + p);
}

const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png' };
const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://x');
  try {
    if (url.pathname.startsWith('/api/')) return await api(req, res, url);
    let f = path.join(dist, url.pathname === '/' ? 'index.html' : url.pathname);
    if (!f.startsWith(dist) || !fs.existsSync(f) || fs.statSync(f).isDirectory()) f = path.join(dist, 'index.html');
    res.writeHead(200, { 'Content-Type': mime[path.extname(f)] ?? 'application/octet-stream' }); res.end(fs.readFileSync(f));
  } catch (e) { res.writeHead(500); res.end(String(e)); }
});
await new Promise(r => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;

// ---------- 浏览器 ----------
const browser = await chromium.launch({ executablePath: process.env.CHROME, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } });
await context.addInitScript(() => { try { localStorage.setItem('vac_tour_seen_v1', '1'); } catch { /* ignore */ } });
const page = await context.newPage();
process.on('uncaughtException', async (e) => { console.error(e); console.error('PAGEERRORS', pageErrors); try { await page.screenshot({ path: path.join(shots, '_failure.png') }); console.error('URL', page.url()); console.error((await page.locator('body').innerText()).slice(0, 1500)); } catch { /* ignore */ } process.exit(1); });
page.on('console', (m) => { if (process.env.DEBUG_SMOKE) console.log('[console]', m.type(), m.text().slice(0, 300)); }); page.on('requestfailed', (r) => { if (process.env.DEBUG_SMOKE) console.log('[reqfail]', r.url()); });
page.on('request', (r) => { if (process.env.DEBUG_SMOKE && r.url().includes('/api/')) console.log('[req]', r.method(), r.url().replace(base, '')); });
const pageErrors = []; page.on('pageerror', (e) => pageErrors.push(String(e)));
let n = 0; const ok = (m) => console.log(`ok ${++n} ${m}`);
// 截图：隐藏吸顶的源监视器（会盖住卡片），只截「开始生成」卡；整页另存一张 full-。
const shot = async (name) => {
  await page.addStyleTag({ content: '.sticky-monitor{display:none!important}' }).catch(() => {});
  const c = page.locator('.page:not([hidden]) .quickstart-card');
  if (await c.count()) await c.first().screenshot({ path: path.join(shots, `${name}.png`) });
  else await page.screenshot({ path: path.join(shots, `${name}.png`) });
};
const V = '.page:not([hidden])';
const card = page.locator(`${V} .quickstart-card`);
const startBtn = card.locator('button.auto-start-btn');
const progress = card.locator('.auto-progress');
const wait = (ms) => page.waitForTimeout(ms);
const kind = (k) => page.locator(`${V} .auto-progress[data-auto-kind="${k}"]`).waitFor({ timeout: 8000 });
const doneCount = async () => (await progress.locator('.ant-steps-item-finish').count());

await page.goto(`${base}/#/editing/import`, { waitUntil: 'domcontentloaded' });
await card.waitFor();
assert.equal(await progress.count(), 0, 'no progress panel before any run');
await card.locator('.start-mode .ant-segmented-item', { hasText: '自动模式' }).click();
await startBtn.waitFor();
await card.locator('.auto-notice').waitFor();

// 1) 授权勾选后才可启动 + 外发提示
const noticeText = await card.locator('[data-testid="auto-notice-text"]').innerText();
assert.match(noticeText, /外部端点发送数据/);
assert.match(await card.locator('.auto-notice').innerText(), /数据会发送到外部服务/);
assert.equal(await startBtn.isDisabled(), true, 'start disabled before authorization');
await shot('01-auto-start-unauthorized');
const authBox = card.locator('.auto-authorize input[type=checkbox]');
await authBox.check();
await page.waitForFunction(() => { const b = document.querySelector('.page:not([hidden]) .auto-start-btn'); return b && !b.disabled; });
await authBox.uncheck();
assert.equal(await startBtn.isDisabled(), true, 'unchecking authorization disables start again');
ok('start is disabled until 全局授权 is ticked (and again after unticking); egress notice with endpoints is shown');

// 本机预设：提示变成「只在本机处理」，仍需授权
await card.locator('.auto-profile-select').click();
await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option', { hasText: '本机剪辑' }).click();
await card.locator('.auto-notice', { hasText: '数据只在本机处理' }).waitFor();
assert.equal(await startBtn.isDisabled(), true);
await card.locator('.auto-profile-select').click();
const opts = await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option').allInnerTexts();
assert.equal(opts.some(t => t.includes('工程回归')), false, 'builtin preset is not offered for auto mode');
await page.locator('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option', { hasText: '云端剪辑' }).click();
await card.locator('.auto-notice', { hasText: '数据会发送到外部服务' }).waitFor();
assert.equal(await authBox.isChecked(), false, 'changing the preset resets the authorization');
ok('local preset shows 「只在本机处理」; builtin regression preset is not selectable; preset change resets the authorization tick');

// 2) 放宽阈值需明确确认
await authBox.check();
await card.locator('.auto-advanced .ant-collapse-header').click();
await card.locator('.auto-advanced').getByText('放宽自评标准（不推荐）').click();
const relaxConfirm = card.locator('.auto-relax input[type=checkbox]');
await relaxConfirm.waitFor();
assert.equal(await startBtn.isDisabled(), true, 'relaxing without confirmation keeps start disabled');
await shot('02-auto-start-relax-needs-confirm');
await relaxConfirm.check();
await page.waitForFunction(() => { const b = document.querySelector('.page:not([hidden]) .auto-start-btn'); return b && !b.disabled; });
ok('relaxing the self-review threshold needs its own explicit confirmation tick');

// 3) 启动失败：只发 1 次请求、不自动重试、给人话
await card.locator('#auto-brief').fill('剪成一分钟高光，节奏紧凑');
mk.startMode = 'fail500'; mk.calls.start = 0; mk.bodies.start.length = 0;
await startBtn.click();
await card.locator('.auto-start-error').waitFor();
await wait(3000);
assert.equal(mk.calls.start, 1, `failed start sent ${mk.calls.start} requests`);
assert.match(await card.locator('.auto-start-error').innerText(), /稍后再试/);
assert.doesNotMatch(await card.locator('.auto-start-error').innerText(), /sql|boom/);
assert.equal(await startBtn.isDisabled(), false, 'user can retry by clicking again');
await shot('03-start-failed');
mk.startMode = 'conflict';
await startBtn.click(); await wait(600);
assert.equal(mk.calls.start, 2);
assert.match(await card.locator('.auto-start-error').innerText(), /刚刚发生了变化/);
const keys = new Set(mk.bodies.start.map(b => b.idempotency_key));
assert.equal(keys.size, 1, 'same content retried with the same idempotency key');
ok('failed start: exactly 1 request (no loop), plain-Chinese error; manual retry reuses the idempotency key');

// 双击只发一次 + 请求体
mk.startMode = 'slow'; mk.calls.start = 0; mk.bodies.start.length = 0;
await startBtn.dblclick();
await card.locator('.auto-progress').waitFor({ timeout: 8000 });
assert.equal(mk.calls.start, 1, `double click sent ${mk.calls.start} start requests`);
const body = mk.bodies.start[0];
assert.equal(body.profile_id, 'cloud_cut'); assert.equal(body.revision, 2);
assert.equal(body.expected_asset_version, 'av1');
assert.equal(body.brief, '剪成一分钟高光，节奏紧凑');
assert.deepEqual(body.authorization, { accept_external_egress: true, notice_sha256: 'sha-cloud' });
assert.deepEqual(body.scope, { max_rounds: 1, thresholds: { high_score_min: 0.8 }, relax_thresholds: true });
assert.ok(body.idempotency_key);
ok('dblclick → 1 start request; body carries profile, notice_sha256, accept_external_egress, relax_thresholds+high_score_min');

// 4) 进度面板：人话阶段 + 启动区在进行中隐藏
await kind('running');
assert.equal(await card.locator('.auto-start-btn').count(), 0, 'start form is hidden while the run is live');
assert.match(await progress.innerText(), /Agent 正在自动剪辑/);
const stepText = await progress.locator('.ant-steps-item-title').allInnerTexts();
assert.deepEqual(stepText.map(t => t.trim()), ['识别画面', '确认场景', '撰写解说', '配音', '字幕', '混音', '导出成片']);
await shot('04-progress-recognize');
setAuto(null, newRun('tts'));
await page.waitForFunction(() => document.querySelectorAll('.page:not([hidden]) .auto-progress .ant-steps-item-finish').length === 3);
assert.match(await progress.locator('.ant-steps-item-process').innerText(), /配音/);
setAuto(null, newRun('export'));
await page.waitForFunction(() => document.querySelectorAll('.page:not([hidden]) .auto-progress .ant-steps-item-finish').length === 6);
await shot('05-progress-export');
const bodyText = await card.innerText();
for (const w of ['awaiting_review', 'workflow', 'auto_version', 'actor', 'system:auto-review', 'halted']) assert.equal(bodyText.includes(w), false, `jargon「${w}」visible`);
ok('progress shows 7 plain-Chinese phases following the run stage via SSE; no backend jargon on screen');

// 5) 低档：正在自动重剪第 N 轮
setAuto(baseAuto({ auto_version: 5, round: 1, total_rounds: 1, latest_review: review('low', 0.52, ['low_confidence', 'pacing_out_of_range']) }), newRun('recognize'));
await kind('rerunning');
assert.match(await progress.innerText(), /正在自动重剪第 1 轮/);
assert.match(await progress.innerText(), /AI 自评：低档/);
assert.match(await progress.innerText(), /部分画面的识别把握偏低/);
assert.doesNotMatch(await progress.innerText(), /low_confidence|pacing_out_of_range/);
await shot('06-low-rerunning-round1');
ok('LOW band: 「正在自动重剪第 1 轮」with plain-language reasons (no reason codes)');

// 6) 中档：决策卡
setAuto(baseAuto({ auto_version: 6, state: 'awaiting_user_decision', delivery_hold: true, round: 1, total_rounds: 1, latest_review: review('medium', 0.78, ['item_below_floor']), delivery: delivered,
  decision: { reason: 'medium', can_export: true, can_revise: true, deadline_at: '2026-10-19T12:00:00+09:00' } }), newRun('acceptance', 'ready_for_acceptance'));
await kind('decision');
const dec = progress.locator('.auto-decision');
assert.match(await dec.innerText(), /待你确认/);
assert.match(await dec.innerText(), /2026-10-19 \d\d:00 前决定/);
assert.match(await dec.innerText(), /不会自动导出/);
assert.equal(await dec.getByRole('button', { name: '直接导出' }).isEnabled(), true);
assert.equal(await progress.locator('a[href*="delivery.zip"], a[href*="/files/mp4"]').count(), 0, 'no download links while 待你确认');
await shot('07-medium-decision-card');
// revise：空意见不能提交
await dec.getByRole('button', { name: '提出修改意见' }).click();
const fb = dec.locator('textarea[aria-label="修改意见"]');
await fb.waitFor();
assert.equal(await dec.getByRole('button', { name: '按这个意见重新剪辑' }).isDisabled(), true);
await fb.fill('节奏再快一点，多留团战');
await shot('08-medium-revise-feedback');
mk.calls.decision = 0; mk.bodies.decision.length = 0;
await dec.getByRole('button', { name: '按这个意见重新剪辑' }).click();
await kind('revising');
assert.equal(mk.calls.decision, 1);
assert.deepEqual({ ...mk.bodies.decision[0], idempotency_key: '<k>' }, { decision: 'revise', feedback: '节奏再快一点，多留团战', expected_auto_version: 6, idempotency_key: '<k>' });
assert.match(await progress.innerText(), /按你的修改意见重新剪辑/);
ok('MEDIUM: decision card with deadline; 提出修改意见 needs text, POSTs decision=revise + feedback + expected_auto_version once → panel shows revising');

// 6b) 再次中档 → 直接导出
setAuto(baseAuto({ auto_version: 8, state: 'awaiting_user_decision', delivery_hold: true, round: 1, total_rounds: 1, latest_review: review('medium', 0.8), delivery: delivered,
  decision: { reason: 'medium', can_export: true, can_revise: true, deadline_at: '2026-10-19T12:00:00+09:00' } }), newRun('acceptance', 'ready_for_acceptance'));
await kind('decision');
mk.calls.decision = 0; mk.bodies.decision.length = 0;
await dec.getByRole('button', { name: '直接导出' }).dblclick();
await kind('done');
assert.equal(mk.calls.decision, 1, `dblclick on 直接导出 sent ${mk.calls.decision}`);
assert.equal(mk.bodies.decision[0].decision, 'export'); assert.equal('feedback' in mk.bodies.decision[0], false);
assert.equal(mk.bodies.decision[0].expected_auto_version, 8);
ok('MEDIUM → 直接导出: decision=export once (dblclick-safe) → done panel');

// 7) 高档 / 完成：下载入口
const mp4 = progress.locator('a[href*="/api/previews/pv1/files/mp4"]');
assert.equal(await mp4.count(), 1);
assert.match(await mp4.innerText(), /720p 预览规格/);
assert.match((await mp4.getAttribute('href')) ?? '', /download=1/);
assert.equal(await progress.locator('a[href$="/api/workflows/run_auto1/delivery.zip"]').count(), 1);
assert.match(await progress.innerText(), /不等于人工验收/);
await shot('09-done-download');
setAuto(baseAuto({ auto_version: 9, state: 'completed', latest_review: review('high', 0.95), delivery: delivered }), newRun('acceptance', 'ready_for_acceptance'));
await page.waitForFunction(() => /AI 自评：高档/.test(document.querySelector('.page:not([hidden]) .auto-progress')?.textContent ?? ''));
assert.equal(await doneCount(), 7, 'all seven phases finished');
await shot('10-high-auto-export-done');
assert.equal(await card.locator('.start-mode').count(), 1, 'start form is available again after the run ends');
ok('HIGH/done: all 7 phases finished; 720p download (labelled 预览规格) + delivery package link; 「AI 自评≠人工验收」');

// 8) 耗尽：不能导出，只能改意见
setAuto(baseAuto({ auto_version: 10, state: 'awaiting_user_decision', halt_reason: 'exhausted', round: 1, total_rounds: 1, latest_review: review('low', 0.5, ['low_confidence']),
  decision: { reason: 'exhausted', can_export: false, can_revise: true, deadline_at: '2026-10-19T12:00:00+09:00' } }), newRun('acceptance', 'ready_for_acceptance'));
await kind('decision');
assert.match(await progress.innerText(), /已自动重剪多轮，仍未达到高档/);
assert.equal(await dec.getByRole('button', { name: '直接导出' }).isDisabled(), true);
await shot('11-low-exhausted');
ok('LOW exhausted: plain explanation; 直接导出 disabled (nothing delivered), revise still possible');

// 9) halted：人话原因 + 改为手动
setAuto(baseAuto({ auto_version: 11, state: 'halted', halt_reason: 'human_takeover', latest_review: review('medium', 0.7) }), newRun('draft_review', 'awaiting_review'));
await kind('halted');
assert.match(await progress.innerText(), /你手动改动了内容，自动模式已停止/);
assert.equal(await progress.getByRole('button', { name: '撤销授权' }).count(), 0, 'nothing left to revoke');
await shot('12-halted-human-takeover');
setAuto(baseAuto({ auto_version: 12, state: 'halted', halt_reason: 'not_improvable' }), newRun('acceptance', 'ready_for_acceptance'));
await page.waitForFunction(() => /重剪能解决/.test(document.querySelector('.page:not([hidden]) .auto-progress')?.textContent ?? ''));
await progress.getByRole('button', { name: '改为手动处理' }).click();
await page.waitForFunction(() => location.hash.includes('review'));
ok('halted: plain-language reason; 「改为手动处理」 navigates to the review page');
await page.evaluate(() => { location.hash = '#/editing/import'; });
await card.waitFor();

// 10) 撤销
setAuto(baseAuto({ auto_version: 20, state: 'active', latest_review: null }), newRun('narrate'));
await kind('running');
mk.calls.revoke = 0; mk.bodies.revoke.length = 0;
await progress.getByRole('button', { name: '撤销授权' }).click();
await page.locator('.ant-popover:not(.ant-popover-hidden), .ant-popconfirm').first().waitFor();
await shot('13-revoke-confirm');
await page.locator('.ant-popconfirm-buttons button', { hasText: '先不撤销' }).click();
await wait(300);
assert.equal(mk.calls.revoke, 0, 'cancelling the confirmation does not revoke');
await progress.getByRole('button', { name: '撤销授权' }).click();
await page.locator('.ant-popconfirm .ant-checkbox-wrapper', { hasText: '同时撤销该预设版本的外发许可' }).click();
await page.locator('.ant-popconfirm-buttons button.ant-btn-primary').click();
await kind('revoked');
assert.equal(mk.calls.revoke, 1);
assert.deepEqual(mk.bodies.revoke[0], { reason: 'user_revoked_from_ui', expected_auto_version: 20, also_revoke_consent: true });
assert.match(await progress.innerText(), /你已撤销自动授权/);
await shot('14-revoked');
ok('revoke: confirmation first (cancel → no request); confirm → POST revoke with expected_auto_version + also_revoke_consent → revoked panel');

// 冲突：版本过期给人话并刷新
setAuto(baseAuto({ auto_version: 30, state: 'active' }), newRun('narrate'));
await kind('running');
mk.auto = baseAuto({ auto_version: 31, state: 'active' }); // 后端悄悄变了版本，前端还没收到事件
await progress.getByRole('button', { name: '撤销授权' }).click();
await page.locator('.ant-popconfirm-buttons button.ant-btn-primary').click();
await progress.locator('.ant-alert-error', { hasText: '刚刚发生了变化' }).waitFor();
ok('stale auto_version (409) → plain-language message and the view refreshes');

// 11) getAuto 失败不死循环：出错后不再轮询；点「重试」只再发一次
mk.autoFail = 1; mk.calls.auto = 0;
push('auto.changed', { run_id: 'run_auto1' });
await card.locator('.auto-load-error').waitFor();
await wait(4500);
assert.ok(mk.calls.auto <= 2, `auto GET after failure: ${mk.calls.auto} requests in ~4.5s`);
const before = mk.calls.auto;
await wait(6000); // 超过 5s 轮询间隔，仍不得再发
assert.equal(mk.calls.auto, before, 'no polling after an error');
mk.autoFail = 0;
await card.locator('.auto-load-error button', { hasText: '重试' }).click();
await kind('running');
assert.equal(mk.calls.auto, before + 1, 'retry sends exactly one request');
ok(`auto view 500: ${before} request(s) then silence (even past the 5s poll interval); 重试 sends exactly one more and recovers`);

// 12) 非自动流程：404 = 不是自动模式，面板不出现，也不重复请求
mk.autoNotFound = true; mk.calls.auto = 0;
setAuto(null, newRun('scene_review', 'awaiting_review'));
await wait(4500);
assert.equal(await progress.count(), 0, 'no auto panel for a non-auto run');
assert.ok(mk.calls.auto <= 3, `404 non-auto run requested ${mk.calls.auto} times`);
await shot('15-non-auto-run');
ok('non-auto run (404): no auto panel, no request loop');
mk.autoNotFound = false;

// 13) 授权说明读取失败：不给启动
mk.run = null; mk.auto = null; mk.noticeFail = 1;
await page.reload({ waitUntil: 'domcontentloaded' });
await card.waitFor();
await page.waitForFunction(() => document.querySelector('.page:not([hidden]) .start-mode'));
await card.locator('.start-mode .ant-segmented-item', { hasText: '自动模式' }).click();
await page.getByText('读取授权说明失败').waitFor();
await card.locator('.auto-authorize input[type=checkbox]').check();
assert.equal(await startBtn.isDisabled(), true, 'no start without a readable notice');
const noticeCalls = mk.calls.notice; await wait(2500);
assert.equal(mk.calls.notice, noticeCalls, 'notice failure does not loop');
await card.locator('.auto-notice').count();
await page.getByRole('button', { name: '重试' }).first().click();
await card.locator('.auto-notice').waitFor();
await page.waitForFunction(() => { const b = document.querySelector('.page:not([hidden]) .auto-start-btn'); return b && !b.disabled; });
ok('notice read failure: start stays disabled, no loop; 重试 recovers');

assert.deepEqual(pageErrors, [], 'page errors: ' + pageErrors.join('\n'));
await browser.close(); server.close();
console.log(`auto-ui smoke passed (${n} groups); screenshots in ${shots}`);
process.exit(0);
