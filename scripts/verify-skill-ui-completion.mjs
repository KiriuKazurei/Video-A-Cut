/** Independent browser checks of the shipped UI; all state is disposable and local. */
import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { createRequire } from 'node:module';
import { copyFile, mkdir, readFile, writeFile } from 'node:fs/promises';
import { resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const output = process.env.VAC_TEST_OUTPUT;
assert(output, 'Run through TestWorkspace.ps1');
const realFlow = process.argv.includes('--real-flow') ? JSON.parse(await readFile(join(output, 'vu-runtime-result.json'), 'utf8')) : null;
if (realFlow) assert(realFlow.runtime === 'passed' && realFlow.final_run_state === 'succeeded', 'Run the real HTTP/MCP flow first');
const folder = join(output, realFlow ? 'skill-ui-real-flow' : 'skill-ui-completion');
await mkdir(folder, { recursive: true });
const require = createRequire(join(root, 'webui/package.json'));
let browserTools;
try { browserTools = require('playwright-core'); }
catch { browserTools = createRequire(join(output, 'skill-ui-browser-deps/package.json'))('playwright-core'); }
const { chromium } = browserTools;
const token = randomUUID() + randomUUID();
const config = join(folder, 'config.json');
const delivery = realFlow?.workspace.delivery ?? join(folder, 'delivery');
const database = join(folder, `vac-${randomUUID()}.db`);
if (realFlow) await copyFile(realFlow.workspace.database, database);
await mkdir(delivery, { recursive: true });
await writeFile(config, JSON.stringify({ http_addr: '127.0.0.1:0', delivery_root: delivery,
  web_root: join(output, 'vu-ui-review-dist'), tts_python: 'python',
  tts_worker_dir: join(root, 'workers/python') }));
const runtime = spawn(join(output, 'vac-control-plane.exe'), ['-config', config, '-db', database, '-desktop'],
  { cwd: folder, env: { ...process.env, VAC_HUMAN_ACCESS_TOKEN: token }, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] });
let errors = '';
runtime.stderr.on('data', chunk => { errors += String(chunk); });
const ready = await new Promise((ok, no) => {
  const timer = setTimeout(() => no(new Error('Runtime readiness timeout')), 15000);
  let text = '';
  runtime.stdout.on('data', chunk => {
    text += String(chunk);
    if (text.includes('\n')) { clearTimeout(timer); try { ok(JSON.parse(text.split('\n')[0])); } catch (e) { no(e); } }
  });
  runtime.once('error', no);
  runtime.once('exit', code => { clearTimeout(timer); if (code) no(new Error(`Runtime exit ${code}`)); });
});
const base = ready.api_base_url;
const observations = [];
const requests = [];
const writes = [];
const consoleErrors = [];
let browser;
let releaseExecution;
const record = (id, passed, detail, layer = 'browser_with_run_fixtures') => {
  observations.push({ id, passed, detail, layer });
  console.log(JSON.stringify(observations.at(-1)));
};
try {
 if (realFlow) {
  // No intercepted responses: use a database produced by the real MCP flow.
  browser = await chromium.launch({ channel: 'msedge', headless: true, args: ['--disable-gpu', '--disk-cache-size=1048576'] });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  page.on('pageerror', error => consoleErrors.push(error.message));
  page.on('request', request => { if (request.url().includes('/api/')) requests.push({ method: request.method(), path: new URL(request.url()).pathname }); });
  const human = async path => {
    const response = await fetch(`${base}${path}`, { headers: { 'X-VAC-Human-Token': token } });
    assert.equal(response.status, 200);
    return response.json();
  };
  await page.goto(`${base}/?tour=0#/resources`, { waitUntil: 'domcontentloaded' });
  await page.locator('.project-module input[aria-label="人工访问凭证"]').fill(token);
  await page.locator('.project-module').getByRole('button', { name: '解锁人工会话', exact: true }).click();
  await page.getByRole('button', { name: '退出人工会话', exact: true }).first().waitFor();
  await page.locator('.page:not([hidden])').getByLabel('项目', { exact: true }).first().click();
  await page.locator(`.ant-select-dropdown:visible [data-value="${realFlow.workspace.project_id}"]`).click();
  await page.evaluate(() => { location.hash = '/editing/content'; });
  await page.locator('.skill-lane').getByText('Skill 运行', { exact: true }).click();
  await page.locator('.skill-run-module .ant-list-item').filter({ hasText: 'Two authorized moments' }).getByRole('button', { name: '打开', exact: true }).click();
  await page.getByRole('button', { name: '载入当前计划', exact: true }).waitFor();
  record('REAL-RUN-LOAD', true, { run: realFlow.workspace.run_id }, 'browser_real_http');
  const current = page.locator('.skill-run-module .ant-list-item').filter({ has: page.getByText('当前', { exact: true }) });
  await current.getByRole('button', { name: '播放', exact: true }).click();
  const video = page.locator('.skill-run-module video');
  await video.waitFor();
  await video.evaluate(async element => { element.muted = true; await element.play(); });
  await page.waitForFunction(() => {
    const element = document.querySelector('.skill-run-module video');
    return element && element.currentTime > .05 && element.videoWidth > 0 && !element.error;
  });
  const movie = await video.evaluate(element => ({ duration: element.duration, width: element.videoWidth, time: element.currentTime, error: element.error?.code }));
  record('REAL-FILM-PLAYBACK', Math.abs(movie.duration - .7) < .02 && movie.width > 0 && !movie.error, movie, 'browser_real_http');
  await page.screenshot({ path: join(folder, 'real-film.png'), fullPage: false });
  await page.getByText('按成片时间点', { exact: true }).click();
  await page.getByLabel('成片时间点', { exact: true }).fill('0.4');
  await page.getByLabel('修改提示词', { exact: true }).fill('Browser requests ending adjustment');
  const runPath = `/api/skill-runs/${realFlow.workspace.run_id}`;
  const editReply = page.waitForResponse(response => response.url().endsWith(`${runPath}/edits`) && response.request().method() === 'POST');
  await page.getByRole('button', { name: '提交定向修改', exact: true }).click();
  assert.equal((await editReply).status(), 200);
  const edited = await human(runPath);
  const window = edited.edit_request?.windows[0];
  record('REAL-POINT-EDIT', edited.edit_request?.prompt === 'Browser requests ending adjustment' && window?.segment_index === 1 && window?.timeline_start === .3 && window?.source_start === .5,
    { prompt: edited.edit_request?.prompt, window }, 'browser_real_http');
  await page.getByRole('button', { name: '载入当前计划', exact: true }).click();
  await page.getByLabel('片段1旁白', { exact: true }).fill('Browser revised opening');
  const saveReply = page.waitForResponse(response => response.url().endsWith(`${runPath}/plan`) && response.request().method() === 'PUT');
  await page.getByRole('button', { name: '保存计划草案', exact: true }).click();
  assert.equal((await saveReply).status(), 200);
  const saved = await human(runPath);
  record('REAL-PLAN-SAVE', saved.plan.segments[0].narration === 'Browser revised opening' && !saved.plan.finalized_by, { planVersion: saved.plan.version }, 'browser_real_http');
  const undoReply = page.waitForResponse(response => response.url().endsWith(`${runPath}/undo`));
  await page.getByRole('button', { name: '撤销', exact: true }).click();
  assert.equal((await undoReply).status(), 200);
  const undone = await human(runPath);
  record('REAL-UNDO', undone.plan.segments[0].narration === 'Opening' && !undone.plan.finalized_by && undone.steps === saved.steps,
    { narration: undone.plan.segments[0].narration, steps: undone.steps }, 'browser_real_http');
  const redoReply = page.waitForResponse(response => response.url().endsWith(`${runPath}/redo`));
  await page.getByRole('button', { name: '重做', exact: true }).click();
  assert.equal((await redoReply).status(), 200);
  const redone = await human(runPath);
  record('REAL-REDO', redone.plan.segments[0].narration === 'Browser revised opening' && !redone.plan.finalized_by && redone.frames_spent === saved.frames_spent,
    { narration: redone.plan.segments[0].narration, frames: redone.frames_spent }, 'browser_real_http');
  const approval = page.waitForResponse(response => response.url().endsWith(`${runPath}/finalize`));
  await page.getByRole('button', { name: '人工定稿', exact: true }).click();
  assert.equal((await approval).status(), 200);
  const approved = await human(runPath);
  record('REAL-HUMAN-FINALIZE', Boolean(approved.plan.finalized_by), { planVersion: approved.plan.version, state: approved.state }, 'browser_real_http');
  record('REAL-NO-PAGE-ERRORS', consoleErrors.length === 0, { errors: consoleErrors }, 'browser_real_http');
 } else {
  const projectResponse = await fetch(`${base}/api/projects`, { method: 'POST', headers: {
    'Content-Type': 'application/json', 'X-VAC-Human-Token': token },
    body: JSON.stringify({ name: 'UI completion audit', flow: 'visual_heavy', expected_version: 0, assets: [], visible: [] }) });
  assert.equal(projectResponse.status, 200);
  const project = await projectResponse.json();
  const movie = join(folder, 'preview.mp4');
  const generated = spawnSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-f', 'lavfi',
    '-i', 'color=c=blue:s=320x180:r=30:d=4', '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', movie], { windowsHide: true });
  assert.equal(generated.status, 0, String(generated.stderr));
  const media = await readFile(movie);
  const voice = name => ({ engine: 'sapi', voice: name, language: 'zh', speed: 1, volume: 1, pitch_semitones: 0, sapi_rate: 0, instruction: '' });
  const makeRun = (id, version) => ({ id, project_id: project.id, project_version: project.version,
    skill_id: 'video-use', skill_sha256: 'fixture-skill', skill_version: '1', execution_mode: 'builtin_api', flow: 'visual_heavy',
    visible_set_sha: 'fixture-visible', state: 'waiting_human', reason: 'agent_clarification', owner: 'human', version,
    steps: 1, budget: { max_steps: 24, max_seconds: 900, max_frames: 24 }, goal: `audit-${id}`,
    external_authorized: false, transcription_authorized: false, elapsed_milliseconds: 0,
    budget_step_base: 0, budget_elapsed_base: 0, frames: [], frames_spent: 0,
    plan: { body: `${id}-first\n\n${id}-second`, version, subtitles: false, finalized_by: 'human', segments: [0, 1].map(index => ({
      media_id: 'fixture-source', start: index * 2, end: index * 2 + 2, narration: `${id}-segment-${index}`,
      tts_enabled: false, voice: voice(`voice-${index}`), voice_offset: 0, voice_consent: false })) },
    artifacts: [ { id: 'film', name: `${id}.mp4`, kind: 'mp4', size_bytes: media.length, sha256: 'fixture-film', plan_version: version },
      { id: 'package', name: `${id}.zip`, kind: 'premiere_package', size_bytes: 1, sha256: 'fixture-zip', plan_version: version } ],
    created_at: '2026-10-19T00:00:00Z', updated_at: '2026-10-19T00:00:00Z' });
  const runs = { 'run-A': makeRun('run-A', 10), 'run-B': makeRun('run-B', 20) };
  const notes = [];
  let cancelConflict = true;
  const cursors = Object.fromEntries(Object.keys(runs).map(id => [id, { checkpoint_id: 'child', redo: [] }]));
  const history = run => ({ cursor: cursors[run.id], checkpoints: [
    { id: 'parent', run_id: run.id, parent_id: '', sha256: 'parent', snapshot: { artifacts: run.artifacts } },
    { id: 'child', run_id: run.id, parent_id: 'parent', sha256: 'child', snapshot: { artifacts: run.artifacts } } ] });
  const workbench = run => ({ run, history: history(run).cursor,
    segments: run.plan.segments.map((segment, index) => ({ segment_index: index, media_id: segment.media_id,
      timeline_start: index * 2, timeline_end: index * 2 + 2, source_start: segment.start, source_end: segment.end })),
    previews: [{ artifact: run.artifacts[0], available: true, is_current: true, historical: false,
      checkpoint_id: '', exportable: true, preview_url: `/api/skill-runs/${run.id}/artifacts/film?preview_ticket=fixture-only` }] });
  browser = await chromium.launch({ channel: 'msedge', headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const page = await context.newPage();
  page.on('pageerror', error => consoleErrors.push(error.message));
  page.on('request', request => { if (request.url().includes('/api/')) requests.push({ method: request.method(), path: new URL(request.url()).pathname + new URL(request.url()).search }); });
  await context.route('**/api/skill-runs**', async route => {
    const request = route.request(), url = new URL(request.url());
    const parts = url.pathname.split('/').filter(Boolean);
    const run = runs[parts[2]];
    const answer = body => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
    if (parts.length === 2) return answer(Object.values(runs).filter(run => run.project_id === url.searchParams.get('project_id')));
    if (!run) return route.continue();
    if (parts.includes('artifacts')) return route.fulfill({ status: 200, contentType: 'video/mp4', body: media });
    const action = parts[3];
    if (request.method() === 'GET') {
      if (!action) return answer(run);
      if (action === 'workbench') return answer(workbench(run));
      if (action === 'checkpoints') return answer(history(run));
      if (action === 'context') return answer({ run, skill: { name: 'video-use' }, media: [], tools: ['media.list'] });
      if (action === 'steps' || action === 'revisions') return answer([]);
    }
    const body = request.postDataJSON();
    writes.push({ id: run.id, action, body });
    if (action === 'execute') {
      run.version += 1;
      run.state = 'running';
      await new Promise(ok => { releaseExecution = ok; });
      try { return await answer(run); } catch { if (run.state !== 'cancelled') throw new Error('Unexpected execute response failure'); return; }
    }
    if (action === 'cancel') {
      if (cancelConflict) {
        cancelConflict = false;
        run.version += 1;
        return route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'conflict', message: 'run version advanced' } }) });
      }
      assert.equal(body.expected_version, run.version);
      run.state = 'cancelled';
    }
    if (action === 'plan') run.plan = { ...body.plan, version: run.plan.version + 1 };
    if (action === 'voice') run.plan.segments[body.segment_index].voice = body.settings;
    if (action === 'undo' || action === 'redo') {
      cursors[run.id] = action === 'undo' ? { checkpoint_id: 'parent', redo: ['child'] } : { checkpoint_id: 'child', redo: [] };
      run.plan.version += 1;
      run.plan.finalized_by = '';
    }
    run.version += 1;
    return answer(run);
  });
  await context.route('**/api/agent-operations?**', async route => {
    const url = new URL(route.request().url());
    const after = Number(url.searchParams.get('after') || 0);
    const all = Array.from({ length: 250 }, (_, index) => ({
      sequence: index + 1, run_id: url.searchParams.get('run_id'), previous_sha256: 'previous', sha256: 'current',
      body: { actor: 'fixture-agent', action: 'fixture.action', at: '2026-10-19', detail: { note: `detail-${index + 1}` } }
    })).concat(notes.filter(row => row.run_id === url.searchParams.get('run_id')));
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(all.filter(row => row.sequence > after).slice(0, 100)) });
  });
  await context.route('**/api/agent-operations/notes', async route => {
    const body = route.request().postDataJSON();
    notes.push({ sequence: 251 + notes.length, run_id: body.run_id, previous_sha256: 'previous', sha256: 'current',
      body: { actor: 'human', action: 'human.note', at: '2026-10-19', detail: body.note } });
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ appended: true }) });
  });
  await page.goto(`${base}/?tour=0#/resources`, { waitUntil: 'domcontentloaded' });
  await page.locator('.project-module input[aria-label="人工访问凭证"]').waitFor();
  const beforeUnlock = requests.filter(row => row.method === 'GET' && row.path === '/api/projects').length;
  await page.locator('.project-module input[aria-label="人工访问凭证"]').fill(token);
  await page.locator('.project-module').getByRole('button', { name: '解锁人工会话', exact: true }).click();
  await page.getByRole('button', { name: '退出人工会话', exact: true }).first().waitFor();
  await page.waitForTimeout(500);
  const afterUnlock = requests.filter(row => row.method === 'GET' && row.path === '/api/projects').length;
  record('UI-UNLOCK-REFRESH', afterUnlock > beforeUnlock, { projectsReadsBefore: beforeUnlock, projectsReadsAfter: afterUnlock,
    requirement: 'Successful unlock must refresh the failed protected project query without a reload' }, 'browser_real_http');
  await page.screenshot({ path: join(folder, '01-unlock.png'), fullPage: false });
  if (afterUnlock <= beforeUnlock) await page.reload({ waitUntil: 'domcontentloaded' });
  const choose = async (label, value) => {
    const control = page.locator('.page:not([hidden])').getByLabel(label, { exact: true }).first();
    await control.click();
    await page.locator(`.ant-select-dropdown:visible [data-value="${value}"]`).click();
  };
  await choose('项目', project.id);
  await page.getByLabel('项目名称', { exact: true }).fill('Browser verified project');
  await page.getByRole('button', { name: '保存项目配置', exact: true }).click();
  await page.waitForTimeout(400);
  const saved = await (await fetch(`${base}/api/projects/${project.id}`, { headers: { 'X-VAC-Human-Token': token } })).json();
  record('UI-PROJECT-SAVE', saved.name === 'Browser verified project' && saved.version > project.version,
    { versionBefore: project.version, versionAfter: saved.version, nameMatches: saved.name === 'Browser verified project' }, 'browser_real_http');
  const navigate = async hash => { await page.evaluate(value => { location.hash = value; }, hash); await page.waitForTimeout(150); };
  await navigate('/editing/content');
  await page.locator('.skill-lane').getByText('Skill 运行', { exact: true }).click();
  const openRun = async id => {
    await page.locator('.skill-run-module .ant-list-item').filter({ hasText: `audit-${id}` }).getByRole('button', { name: '打开', exact: true }).click();
    await page.waitForTimeout(250);
  };
  await openRun('run-A');
  await page.getByRole('button', { name: '载入当前计划', exact: true }).click();
  const oldDraft = 'run-A-local-unsaved\n\nrun-A-second';
  await page.getByLabel('计划正文', { exact: true }).fill(oldDraft);
  await openRun('run-B');
  const planInput = page.getByLabel('计划正文', { exact: true });
  const draftSurvived = await planInput.count() ? await planInput.inputValue() : null;
  const oldSave = page.getByRole('button', { name: '保存计划草案', exact: true });
  if (await oldSave.count() && await oldSave.isEnabled()) await oldSave.click();
  await page.waitForTimeout(250);
  const crossed = writes.find(row => row.id === 'run-B' && row.action === 'plan' && row.body.plan.body === oldDraft);
  record('UI-CROSS-RUN-DRAFT', !crossed && draftSurvived !== oldDraft, { oldDraftStillVisible: draftSurvived === oldDraft,
    submittedToRun: crossed?.id, expectedVersion: crossed?.body.expected_version,
    requirement: 'A local plan draft loaded for run-A must never be submitted to run-B with run-B version' });
  await page.screenshot({ path: join(folder, '02-cross-run-draft.png'), fullPage: false });
  await page.getByRole('button', { name: '载入当前计划', exact: true }).click();
  await page.getByLabel('计划正文', { exact: true }).fill('run-B-human-edited\n\nrun-B-second');
  await page.getByRole('button', { name: '保存计划草案', exact: true }).click();
  await page.waitForTimeout(250);
  const savedPlan = writes.find(row => row.id === 'run-B' && row.action === 'plan' && row.body.plan.body.startsWith('run-B-human-edited'));
  record('UI-PLAN-SAVE', Boolean(savedPlan?.body.expected_version === 20), { targetRun: savedPlan?.id, loadedVersion: savedPlan?.body.expected_version });
  await openRun('run-A');
  await page.getByRole('button', { name: '载入当前计划', exact: true }).click();
  runs['run-A'].version += 1;
  await page.getByRole('button', { name: /刷新$/ }).first().click();
  await page.waitForTimeout(300);
  record('UI-PLAN-STALE', await page.getByRole('button', { name: '保存计划草案', exact: true }).isDisabled(),
    { requirement: 'A refreshed run version must not silently replace the loaded plan CAS' });
  await page.locator('.skill-run-module').getByRole('button', { name: '播放', exact: true }).first().click();
  await page.locator('.skill-run-module video').waitFor();
  const movieReady = await page.locator('.skill-run-module video').evaluate(video => new Promise(ok => {
    if (video.readyState >= 1) return ok(true);
    video.addEventListener('loadedmetadata', () => ok(true), { once: true });
    setTimeout(() => ok(false), 5000);
  }));
  record('UI-PLAYER', movieReady, { mediaMetadataLoaded: movieReady, media: 'local deterministic fixture, not a production film' });
  await page.getByLabel('修改提示词', { exact: true }).fill('shorten the selected segment');
  await page.getByRole('button', { name: '提交定向修改', exact: true }).click();
  await page.waitForTimeout(250);
  const edit = writes.find(row => row.id === 'run-A' && row.action === 'edits');
  record('UI-EDIT-BINDING', Boolean(edit?.body.artifact_id === 'film' && edit.body.artifact_sha256 === 'fixture-film' && edit.body.segment_index === 0),
    { hasCurrentFilm: edit?.body.artifact_id === 'film', hasHash: edit?.body.artifact_sha256 === 'fixture-film', segment: edit?.body.segment_index });
  const beforeHistory = runs['run-A'].version;
  await page.getByRole('button', { name: '撤销', exact: true }).click();
  await page.waitForTimeout(250);
  const redoEnabled = await page.getByRole('button', { name: '重做', exact: true }).isEnabled();
  if (redoEnabled) { await page.getByRole('button', { name: '重做', exact: true }).click(); await page.waitForTimeout(250); }
  const moved = writes.filter(row => row.id === 'run-A' && ['undo', 'redo'].includes(row.action));
  record('UI-UNDO-REDO', moved.length === 2 && redoEnabled && moved[0].body.expected_version === beforeHistory && moved[1].body.expected_version === beforeHistory + 1,
    { actions: moved.map(row => row.action), versions: moved.map(row => row.body.expected_version), redoEnabled });
  await navigate('/tts');
  await page.getByRole('button', { name: '载入当前段', exact: true }).click();
  await page.getByRole('checkbox', { name: '允许本段声音外发', exact: true }).check();
  await choose('Skill 声音段落', '1');
  const voiceSave = page.getByRole('button', { name: '回写到 Skill 运行', exact: true });
  const allowedWithoutReload = await voiceSave.isEnabled();
  if (allowedWithoutReload) { await voiceSave.click(); await page.waitForTimeout(250); }
  const wrongVoice = writes.find(row => row.action === 'voice' && row.body.segment_index === 1 && row.body.settings.voice === 'voice-0');
  const consentCleared = !await page.getByRole('checkbox', { name: '允许本段声音外发', exact: true }).isChecked();
  record('UI-TTS-SEGMENT-BINDING', !wrongVoice && !allowedWithoutReload && consentCleared, { saveEnabledWithoutLoadingNewSegment: allowedWithoutReload, consentCleared,
    submittedSegment: wrongVoice?.body.segment_index, submittedVoice: wrongVoice?.body.settings.voice,
    requirement: 'Changing target segment must invalidate settings loaded for a different segment' });
  await page.getByRole('button', { name: '载入当前段', exact: true }).click();
  await voiceSave.click();
  await page.waitForTimeout(250);
  const correctVoice = writes.find(row => row.action === 'voice' && row.body.segment_index === 1 && row.body.settings.voice === 'voice-1');
  record('UI-TTS-SAVE', Boolean(correctVoice), { segment: correctVoice?.body.segment_index, voice: correctVoice?.body.settings.voice });
  await navigate('/editing/content');
  await page.getByRole('button', { name: '执行内置 Agent', exact: true }).click();
  await page.waitForTimeout(250);
  const cancelEnabled = await page.getByRole('button', { name: '取消运行', exact: true }).isEnabled();
  if (cancelEnabled) {
    await page.getByRole('button', { name: '取消运行', exact: true }).click();
    await page.waitForTimeout(350);
  }
  const cancellations = writes.filter(row => row.action === 'cancel');
  record('UI-CANCEL-INFLIGHT', cancelEnabled && cancellations.length === 2 && runs['run-A'].state === 'cancelled', { executeInFlight: Boolean(releaseExecution), cancelEnabled,
    cancellationVersions: cancellations.map(row => row.body.expected_version), finalState: runs['run-A'].state,
    requirement: 'The cancel action must remain available during the synchronous execute request' });
  record('UI-EXECUTE-NO-RETRY', writes.filter(row => row.action === 'execute').length === 1,
    { executeCalls: writes.filter(row => row.action === 'execute').length, cancellationCalls: cancellations.length });
  releaseExecution?.();
  await page.waitForTimeout(250);
  await navigate('/settings/skills');
  const skillRow = page.locator('.skill-manager-module .ant-list-item').filter({ hasText: 'video-use' }).first();
  await skillRow.getByRole('button', { name: '查看', exact: true }).click();
  const validationResponse = page.waitForResponse(response => response.url().endsWith('/api/skills/video-use/validate'));
  await page.getByRole('button', { name: '校验所选 Skill', exact: true }).click();
  const validationStatus = (await validationResponse).status();
  record('UI-SKILL-DETAIL-VALIDATE', validationStatus === 200, { status: validationStatus, scope: 'real registered video-use package and local dependency probes' }, 'browser_real_http');
  await navigate('/settings/profiles');
  const transcription = page.getByRole('checkbox', { name: '启用 ElevenLabs 转录', exact: true }).first();
  await transcription.check();
  await page.getByLabel('转录 model', { exact: true }).fill('scribe_v1');
  const fieldsShown = await page.getByLabel('转录 endpoint', { exact: true }).isVisible();
  await transcription.uncheck();
  record('UI-TRANSCRIPTION-FIELDS', fieldsShown && await page.getByLabel('转录 model', { exact: true }).count() === 0,
    { fieldsShown, fieldsRemovedOnDisable: await page.getByLabel('转录 model', { exact: true }).count() === 0,
      scope: 'controls only; no provider request or transcription performed' });
  await navigate('/settings/logs');
  await page.locator('.agent-operation-panel .ant-list-item').nth(99).waitFor();
  const next = page.locator('.agent-operation-panel').getByRole('button', { name: /下一页|加载更多|更多记录/ });
  const pagination = await next.count();
  if (pagination) {
    await next.click();
    await page.locator('.agent-operation-panel .ant-list-item').nth(199).waitFor();
    await next.click();
    await page.locator('.agent-operation-panel .ant-list-item').nth(249).waitFor();
  }
  const logReads = requests.filter(row => row.path.startsWith('/api/agent-operations?'));
  const rows = await page.locator('.agent-operation-panel .ant-list-item').count();
  record('UI-JOURNAL-PAGINATION', rows === 250 && await next.count() === 0, { renderedRows: rows,
    paginationControls: pagination, requestedOffsets: logReads.map(row => new URL(row.path, base).searchParams.get('after')),
    requirement: 'Logs beyond the first 100 rows must be reachable' });
  const firstDetails = page.locator('.agent-operation-panel .ant-list-item').first().locator('details');
  if (await firstDetails.count()) await firstDetails.locator('summary').click();
  record('UI-JOURNAL-DETAIL', await firstDetails.locator('pre').isVisible().catch(() => false),
    { requirement: 'Human operators must be able to inspect the recorded operation detail and appended notes' });
  await page.getByLabel('人工操作注释', { exact: true }).fill('human-note-visible-after-append');
  await page.getByRole('button', { name: '追加注释', exact: true }).click();
  await page.locator('.agent-operation-panel .ant-list-item').nth(250).waitFor();
  const noteDetails = page.locator('.agent-operation-panel .ant-list-item').last().locator('details');
  if (await noteDetails.count()) await noteDetails.locator('summary').click();
  record('UI-JOURNAL-NOTE', await noteDetails.getByText('human-note-visible-after-append', { exact: true }).isVisible().catch(() => false),
    { appendedNotes: notes.length });
  await page.screenshot({ path: join(folder, '03-journal.png'), fullPage: false });
  await navigate('/editing/content');
  await openRun('run-B');
  await navigate('/settings/logs');
  await page.waitForTimeout(300);
  const switchedReads = requests.filter(row => row.path.startsWith('/api/agent-operations?') && row.path.includes('run_id=run-B'));
  record('UI-JOURNAL-RUN-RESET', await page.locator('.agent-operation-panel .ant-list-item').count() === 100 && switchedReads.at(-1)?.path.includes('after=0'),
    { rowsAfterRunChange: await page.locator('.agent-operation-panel .ant-list-item').count(), latestRead: switchedReads.at(-1)?.path });
  const otherProject = await (await fetch(`${base}/api/projects`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-VAC-Human-Token': token },
    body: JSON.stringify({ name: 'Second audit project', flow: 'visual_heavy', expected_version: 0, assets: [], visible: [] }) })).json();
  await navigate('/resources');
  await page.getByRole('button', { name: /刷新$/ }).first().click();
  await page.waitForTimeout(250);
  await choose('项目', otherProject.id);
  await navigate('/editing/content');
  record('UI-PROJECT-ISOLATION', await page.getByLabel('计划正文', { exact: true }).count() === 0
    && await page.locator('.skill-run-module .ant-list-item').filter({ hasText: 'audit-run-' }).count() === 0,
    { planDraftCount: await page.getByLabel('计划正文', { exact: true }).count(), project: 'second disposable project' });
  const lockResponse = page.waitForResponse(response => response.url().endsWith('/api/human-session') && response.request().method() === 'DELETE');
  await page.getByRole('button', { name: '退出人工会话', exact: true }).first().click();
  const lockStatus = (await lockResponse).status();
  await page.waitForTimeout(300);
  record('UI-LOCK', lockStatus === 200 && await page.getByRole('button', { name: '人工会话', exact: true }).count() > 0,
    { status: lockStatus, memorySessionCleared: await page.getByRole('button', { name: '人工会话', exact: true }).count() > 0 }, 'browser_real_http');
  record('UI-NO-RENDER-CRASH', consoleErrors.length === 0, { errors: consoleErrors });
  await context.close();
 }
} catch (error) {
  record('HARNESS', false, { error: error.stack });
} finally {
  releaseExecution?.();
  if (browser) await browser.close();
  if (runtime.exitCode == null) {
    runtime.stdin.write('{"command":"shutdown"}\n');
    runtime.stdin.end();
    await Promise.race([new Promise(ok => runtime.once('exit', ok)), new Promise(ok => setTimeout(ok, 5000))]);
    if (runtime.exitCode == null) runtime.kill();
  }
  const result = { source: 'ef0880b + local fixes', observations, requests, writes, model_calls: 0,
    boundary: realFlow ? 'Production React and real HTTP, copied actual MCP-flow test database, registered 0.7s MP4; no route interception. Browser automation is not human/model quality acceptance.' : 'Real production React build with disposable Go HTTP server for human session/project operations; deterministic intercepted run/media/log fixtures for component transition checks. Not human quality acceptance.' };
  await writeFile(join(folder, 'result.json'), JSON.stringify(result, null, 2));
  await writeFile(join(folder, 'runtime.log'), errors);
}
process.exitCode = observations.some(item => !item.passed) ? 1 : 0;
