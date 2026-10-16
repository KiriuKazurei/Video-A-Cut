// 自动模式 × 真实 Go 控制面联调：浏览器真实走「授权 → 自动运行 → 高档完成」「中档 → 直接导出」「中档 → 修改意见」
// 「低档 → 自动重剪 → 完成」「低档耗尽 → 决策卡不能导出 → 撤销授权」。
// 后端：control-plane 的 TestAutoUILive（真实控制面 + 真实 Python/Node worker + 本机 vision/narration/stepfun-TTS stub + 假 token；
// 720p 渲染由后端离线 E2E 的模拟器代替，见该测试注释）。用法：node auto_ui_live.mjs <后端地址> [截图目录]
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import dayjs from 'dayjs';

const base = process.argv[2];
assert.ok(base, 'usage: node auto_ui_live.mjs <base-url> [shots-dir]');
const shots = process.argv[3] ?? path.join(process.cwd(), '..', '.run-data', 'auto-ui', 'real');
fs.mkdirSync(shots, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT_CORE ?? 'playwright-core');
const browser = await chromium.launch({ executablePath: process.env.CHROME, args: ['--no-sandbox'] });
const context = await browser.newContext({ viewport: { width: 1500, height: 1000 } });
await context.addInitScript(() => { try { localStorage.setItem('vac_tour_seen_v1', '1'); localStorage.setItem('vac_start_mode_v1', 'auto'); } catch { /* ignore */ } });
const page = await context.newPage();
process.on('uncaughtException', async (e) => { console.error(e); try { await page.screenshot({ path: path.join(shots, '_failure.png') }); console.error((await page.locator('.quickstart-card').first().innerText()).slice(0, 1500)); } catch { /* ignore */ } process.exit(1); });
const pageErrors = []; page.on('pageerror', (e) => pageErrors.push(String(e)));
const apiErrors = []; page.on('response', (r) => { if (r.url().includes('/api/') && r.status() >= 500) apiErrors.push(`${r.status()} ${r.url()}`); });
let n = 0; const ok = (m) => console.log(`ok ${++n} ${m}`);

const card = page.locator('.page:not([hidden]) .quickstart-card');
const progress = card.locator('.auto-progress');
const kind = (k, timeout = 90000) => page.locator(`.page:not([hidden]) .auto-progress[data-auto-kind="${k}"]`).waitFor({ timeout });
const shot = async (name) => { await page.addStyleTag({ content: '.sticky-monitor{display:none!important}' }).catch(() => {}); await card.first().screenshot({ path: path.join(shots, `${name}.png`) }); };
const steer = (conf, improve) => fetch(`${base}/__live/stub`, { method: 'POST', body: JSON.stringify({ conf, ...(improve === undefined ? {} : { improve }) }) }).then(r => assert.equal(r.status, 204));
const J = async (p) => { const r = await fetch(base + p); assert.equal(r.status, 200, p); return r.json(); };
const currentRun = async () => (await J('/api/assets/e2e_clip/workflows'))[0];
const autoOf = async () => J(`/api/workflows/${(await currentRun()).run_id}/auto`);

async function startAuto(brief, { reopen = false } = {}) {
  if (reopen) await card.locator('button.auto-restart').click();
  await card.locator('#auto-brief').fill(brief);
  await card.locator('.auto-notice').waitFor();
  const box = card.locator('.auto-authorize input[type=checkbox]');
  await box.check();
  const btn = card.locator('button.auto-start-btn');
  await page.waitForFunction(() => { const b = document.querySelector('.page:not([hidden]) .auto-start-btn'); return b && !b.disabled; });
  await btn.click();
  await progress.waitFor();
}

/** 字段校对：界面上显示的内容必须与后端 GET …/auto 的原始 JSON 一致。 */
async function crossCheck(expectKind) {
  const a = await autoOf();
  const text = await progress.innerText();
  if (expectKind === 'done') {
    assert.equal(a.state, 'completed'); assert.equal(a.delivery.delivered, true); assert.equal(a.delivery.package_ready, true);
    assert.equal(a.delivery.preview_is_current, true); assert.equal(a.delivery.spec, '720p'); assert.equal(a.decision, null); assert.equal(a.delivery_hold, false);
    assert.equal(a.delivery_hold_enforced, false);
    const href = await progress.locator('a[href*="/files/mp4"]').getAttribute('href');
    assert.match(href, new RegExp(`/api/previews/${a.delivery.preview_id}/files/mp4\\?download=1$`), 'preview link uses delivery.preview_id');
    const mp4 = await fetch(base + href); assert.equal(mp4.status, 200);
    const zipHref = await progress.locator('a[href$="delivery.zip"]').getAttribute('href');
    assert.equal(zipHref, `/api/workflows/${a.run_id}/delivery.zip`);
    const zip = await fetch(base + zipHref); assert.equal(zip.status, 200); assert.match(zip.headers.get('content-type') ?? '', /zip|octet/);
    assert.match(text, new RegExp(`AI 自评：${{ high: '高', medium: '中', low: '低' }[a.latest_review.band]}档`));
    assert.match(text, new RegExp(`综合 ${Math.round(a.latest_review.score * 100)} 分`));
  } else if (expectKind === 'decision') {
    assert.equal(a.state, 'awaiting_user_decision'); assert.ok(a.decision, 'decision prompt present'); assert.equal(a.delivery_hold, true);
    const dl = dayjs(a.decision.deadline_at).format('YYYY-MM-DD HH:mm');
    assert.ok((await card.locator('.auto-deadline').innerText()).includes(dl), `deadline ${dl} shown`);
    const exportBtn = card.locator('.auto-decision').getByRole('button', { name: '直接导出' });
    assert.equal(await exportBtn.isEnabled(), a.decision.can_export, 'can_export ↔ 直接导出');
    const reviseBtn = card.locator('.auto-decision').getByRole('button', { name: '提出修改意见' });
    assert.equal(await reviseBtn.isEnabled(), a.decision.can_revise, 'can_revise ↔ 提出修改意见');
    // 低档卡在「场景置信度」闸门时还没有产生评审（latest_review=null）：界面不能因此崩，也不显示分数。
    if (a.latest_review) assert.match(text, new RegExp(`综合 ${Math.round(a.latest_review.score * 100)} 分`));
    else assert.doesNotMatch(text, /综合 \d+ 分/);
  }
  return a;
}

await page.goto(`${base}/#/editing/import`, { waitUntil: 'domcontentloaded' });
await card.waitFor();
await card.locator('button.auto-start-btn').waitFor();

// ---- A) 高档：授权 → 自动运行 → 完成 ----
const notice = await J('/api/processing-profiles/e2e/revisions/1/auto-notice');
assert.equal(await card.locator('[data-testid="auto-notice-text"]').innerText(), notice.notice, 'notice text shown verbatim');
assert.match(await card.locator('.auto-notice').innerText(), /数据只在本机处理/);
assert.equal(await card.locator('button.auto-start-btn').isDisabled(), true);
await shot('01-real-start-form');
await steer(0.95);
await startAuto('团战');
await kind('running');
const first = await currentRun();
assert.equal((await autoOf()).brief, '团战');
await shot('02-real-running');
// 阶段随真实流程前进：至少经过一个中间阶段
const seen = new Set();
const t0 = Date.now();
while (Date.now() - t0 < 90000) {
  const done = await progress.locator('.ant-steps-item-finish').count();
  seen.add(done);
  if ((await progress.getAttribute('data-auto-kind')) === 'done') break;
  await page.waitForTimeout(200);
}
assert.ok(seen.size >= 3, `progress moved through ${[...seen].join(',')} finished steps`);
await kind('done');
const doneA = await crossCheck('done');
assert.equal(doneA.latest_review.band, 'high');
assert.equal(await progress.locator('.ant-steps-item-finish').count(), 7);
await shot('03-real-high-done');
ok(`HIGH: authorize → real pipeline (${[...seen].join('→')} steps done) → 自动完成; delivery/preview links, band and score match GET …/auto`);

// ---- B) 中档 → 直接导出 ----
await steer(0.65);
await startAuto('团战', { reopen: true });
await kind('decision');
assert.notEqual((await currentRun()).run_id, first.run_id, 'a new run was started');
const medA = await crossCheck('decision');
assert.equal(medA.latest_review.band, 'medium'); assert.equal(medA.decision.reason, 'medium'); assert.equal(medA.decision.can_export, true);
assert.equal(await progress.locator('a[href*="delivery.zip"]').count(), 0, 'no download links while 待你确认');
await shot('04-real-medium-decision');
await card.locator('.auto-decision').getByRole('button', { name: '直接导出' }).click();
await kind('done');
const exported = await crossCheck('done');
assert.equal(exported.decisions.filter(d => d.kind === 'user_export').length, 1, 'export recorded once');
await shot('05-real-medium-exported');
ok('MEDIUM: decision card (deadline, can_export/can_revise match the API) → 直接导出 → done with delivery');

// ---- C) 中档 → 提出修改意见 ----
await steer(0.65);
await startAuto('团战', { reopen: true });
await kind('decision');
await crossCheck('decision');
await steer(0.95); // 重剪后模型输出变好
await card.locator('.auto-decision').getByRole('button', { name: '提出修改意见' }).click();
await card.locator('textarea[aria-label="修改意见"]').fill('只保留最精彩的团战');
await shot('06-real-revise-feedback');
await card.locator('.auto-decision').getByRole('button', { name: '按这个意见重新剪辑' }).click();
await kind('revising', 30000);
const rv = await autoOf();
assert.equal(rv.total_rounds, 1); assert.equal(rv.round, 0);
await shot('07-real-revising');
await kind('done');
const revised = await crossCheck('done');
assert.equal(revised.total_rounds, 1);
assert.equal(revised.decisions.filter(d => d.kind === 'user_revise').length, 1);
await shot('08-real-revised-done');
ok('MEDIUM → 提出修改意见: revise accepted (total_rounds=1, round=0) → 「按你的修改意见重新剪辑」 → re-reviewed and done');

// ---- D) 低档 → 自动重剪第 1 轮 → 完成 ----
await steer(0.5, 0.95);
await startAuto('团战', { reopen: true });
await kind('rerunning');
assert.match(await progress.innerText(), /正在自动重剪第 1 轮/);
const rr = await autoOf(); assert.equal(rr.round, 1);
await shot('09-real-low-rerunning');
await kind('done');
const mended = await crossCheck('done');
assert.equal(mended.round, 1);
await shot('10-real-low-mended-done');
ok('LOW: 「正在自动重剪第 1 轮」shown while round=1 → second pass is good → done');

// ---- E) 低档耗尽：决策卡不能直接导出；撤销授权 ----
await steer(0.5);
await startAuto('团战', { reopen: true });
await kind('decision');
const ex = await crossCheck('decision');
assert.equal(ex.decision.reason, 'exhausted'); assert.equal(ex.decision.can_export, false);
assert.match(await progress.innerText(), /已自动重剪多轮，仍未达到高档/);
assert.match(await progress.innerText(), /目前还没有可导出的成片/);
await shot('11-real-low-exhausted');
await progress.getByRole('button', { name: '撤销授权' }).click();
await page.locator('.ant-popconfirm-buttons button.ant-btn-primary').click();
await kind('revoked', 15000);
const rev = await autoOf();
assert.equal(rev.state, 'revoked');
await shot('12-real-revoked');
ok('LOW exhausted: can_export=false disables 直接导出; 撤销授权 → state=revoked on the real backend');

assert.deepEqual(pageErrors, [], 'page errors: ' + pageErrors.join('\n'));
assert.deepEqual(apiErrors, [], 'API 5xx: ' + apiErrors.join('\n'));
await browser.close();
console.log(`auto-ui live run passed (${n} scenarios); screenshots in ${shots}`);
process.exit(0);
