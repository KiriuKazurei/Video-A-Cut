// Script authoring acceptance using the shared isolated UI server. No external model calls.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { chromium } from 'playwright-core';
const [base, output] = process.argv.slice(2);
assert(process.env.VAC_TEST_OUTPUT && base && output && process.env.CHROME, 'Use run-local-smokes via TestWorkspace');
await fs.mkdir(output, { recursive: true });
const browser = await chromium.launch({ executablePath: process.env.CHROME, headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
await page.addInitScript(() => localStorage.setItem('vac_tour_seen_v1', '1'));
const errors = [], checks = [], calls = [];
page.on('pageerror', error => errors.push(error.message));
let version = 1, slow = false, failSave = false, external = false, profileRevision = 2, offline = false;
let script = { script_id: 's1', asset_id: '_selfcheck', body: '介绍片段', title: '脚本测试', status: 'draft', visible_set_sha: 'visible-1' };
let segments = [];
const parse = body => ({ version: 1, diagnostics: body.includes('{未知}') ? [{ paragraph: 0, column: 1, message: '未知语法' }] : [], paragraphs: body.trim() ? body.split(/\n\s*\n/).map((source, index) => ({ index, source, instruction: source })) : [] });
const view = () => ({ script, version, segments, sources: [], questions: [], document: parse(script.body), tts_enabled: false, subtitles_enabled: false, allow_web_research: false });
await page.route('**/api/**', async route => {
  if (offline) return route.continue(); // Let the browser's real offline network fail, rather than fulfilling a fixture.
  const req = route.request(), p = new URL(req.url()).pathname;
  if (p === '/api/processing-profiles') return route.fulfill({ json: [{ profile_id: 'p1', revision: profileRevision, name: '内部模型', content_mode: 'configured', vision: { adapter: 'vision' }, narration: { adapter: 'narration' } }] });
  if (p.endsWith('/auto-notice')) return route.fulfill({ json: { profile_id: 'p1', revision: profileRevision, profile_sha256: 'p1-sha-' + profileRevision, external } });
  if (p.endsWith('/external-consent')) { calls.push({ p }); return route.fulfill({ json: { granted: true } }); }
  if (p === '/api/assets/_selfcheck/scripts') return route.fulfill({ json: [script] });
  if (p === '/api/visible-set') return route.fulfill({ json: { sha256: 'visible-1', confirmed: true, assets: [] } });
  if (p === '/api/script-workbench/media') return route.fulfill({ json: [{ id: 'm1', asset_id: '_selfcheck', name: '审查素材', in: 0, out: 6 }] });
  if (p === '/api/script-workbench/fonts') return route.fulfill({ json: ['Arial'] });
  if (p === '/api/script-workbench/parse') return route.fulfill({ json: parse(req.postDataJSON().body) });
  if (p.endsWith('/references')) return route.fulfill({ json: [] });
  if (p.startsWith('/api/scripts/'+script.script_id+'/')) {
    if (p.endsWith('/revisions')) return route.fulfill({ json: [] });
    if (p.endsWith('/delivery')) return route.fulfill({ json: {} });
    if (req.method() === 'GET') return route.fulfill({ json: view() });
    const b = req.postDataJSON(); calls.push({ p, b });
    if (failSave && req.method() === 'PUT') return route.fulfill({ status: 409, json: { error: { code: 'conflict', message: '版本冲突，请载入最新修订' } } });
    assert.equal(b.expected_version, version);
    if(p.endsWith('/export')) return route.fulfill({json:{script_id:script.script_id,version,visible_set_sha:script.visible_set_sha,width:1920,height:1080,duration:6,frames:180,sha256:'fixture-export',tts_enabled:false,files:[]}});
    if (req.method() === 'PUT') { script = { ...script, body: b.body, title: b.title, status: 'draft' }; segments = b.segments; }
    if (p.endsWith('/generate')) { if (slow) await new Promise(r => setTimeout(r, 2500)); segments = [{ index: 0, narration: '模型生成的旁白', media_id: 'm1', start: 0, end: 6 }]; }
    if (p.endsWith('/finalize')) script.status = 'finalized';
    version++; return route.fulfill({ json: view() }).catch(() => {});
  }
  return route.continue();
});
const check = name => { checks.push(name); console.log('PASS ' + name); };
let completed = false;
try {
  const startup = await browser.newPage({ viewport: { width: 1000, height: 740 } });
  const recent = [{ path: 'C:/projects/first.avp', name: '项目甲' }, { path: 'C:/projects/second.avp', name: '项目乙' }];
  await startup.addInitScript(({ base, recent }) => {
    localStorage.setItem('vac_tour_seen_v1', '1');
    const listeners = new Map();
    window.recentReads = 0;
    window.hasRuntimeListener = () => listeners.has('runtime-status');
    window.setBackendStatus = (status, revision) => listeners.get('runtime-status')?.({ payload: {
      status, revision, api_base_url: status === 'ready' ? base : undefined, session_id: 'runtime-1', data_directory: 'C:/vac-fixture', workers: [], message: status,
    } });
    window.__TAURI__ = {
      core: { invoke: async command => {
        if (command === 'recent_projects') {
          window.recentReads++;
          return recent;
        }
        return ({
          runtime_status: { status: 'starting', revision: 0, data_directory: '', workers: [], message: '正在检查本地运行环境…' },
          update_status: { phase: 'idle', current_version: '0.5.3' },
        })[command];
      } },
      event: { listen: async (name, callback) => { listeners.set(name, callback); return () => {
        if (listeners.get(name) === callback) listeners.delete(name);
      }; } },
    };
  }, { base, recent });
  const startupCalls = [], startupRequests = [];
  startup.on('request', request => {
    if (new URL(request.url()).pathname.startsWith('/api/')) startupRequests.push(request.url());
  });
  const startupSession = { id: 'startup-session', dirty: false, revision: 0, active_tasks: 0, recent, recoveries: [] };
  let opened = startupSession;
  await startup.route('**/api/workspace', route => { startupCalls.push('workspace'); return route.fulfill({ json: opened }); });
  await startup.route('**/api/workspace/{open,inspect-project}', route => {
    const request = route.request();
    startupCalls.push(new URL(request.url()).pathname.split('/').at(-1));
    assert.equal(request.postDataJSON().path, recent[1].path);
    assert.equal(request.postDataJSON().expected_session, startupSession.id);
    opened = { ...startupSession, id: 'opened-session', path: recent[1].path,
      document: { project: { id: 'startup-project', name: '项目乙' }, resources: [], scripts: [], sequences: [], workspace: {} } };
    return route.fulfill({ json: opened });
  });
  await startup.goto(base + '/');
  await startup.waitForFunction(() => window.hasRuntimeListener());
  assert.equal(await startup.evaluate(() => window.recentReads), 0);
  await startup.evaluate(() => window.setBackendStatus('starting', 1));
  await startup.getByText('项目甲', { exact: true }).waitFor({ timeout: 5000 });
  const recentButton = name => startup.getByRole('listitem').filter({ hasText: name }).getByRole('button', { name: '打开', exact: true });
  await recentButton('项目甲').click();
  await startup.getByRole('status').filter({ hasText: '项目甲' }).waitFor();
  await startup.getByRole('button', { name: '取消', exact: true }).click();
  await startup.getByRole('status').filter({ hasText: '项目甲' }).waitFor({ state: 'hidden' });
  await recentButton('项目甲').click();
  await recentButton('项目乙').click();
  await startup.getByRole('status').filter({ hasText: '项目乙' }).waitFor();
  assert.deepEqual(startupRequests, []);
  await startup.screenshot({ path: path.join(output, 'startup-queued.png') });
  await startup.evaluate(() => window.setBackendStatus('fault', 2));
  await startup.getByRole('button', { name: '重新启动' }).waitFor();
  await startup.evaluate(() => window.setBackendStatus('ready', 3));
  await startup.waitForFunction(() => document.title.includes('项目乙'));
  assert.deepEqual(startupCalls.filter(call => call !== 'workspace'), ['open']);
  await startup.close();
  check('desktop startup shows recent projects, preserves the last selection through retry and opens it once');
  await page.goto(base + '/#/editing/content?script=s1');
  const workbench = page.locator('.script-editor-sider [data-testid="script-workbench"]');
  await workbench.waitFor();
  assert.equal(await page.locator('[data-workspace="editing"][aria-current="page"]').count(), 1);
  assert.equal(await page.locator('[data-workspace="script"]').count(), 0);
  assert.equal(await workbench.evaluate(el => !!el.closest('.script-editor-sider')), true);
  const handle=page.getByRole('separator',{name:'脚本侧栏宽度',exact:true});
  const pane=page.getByRole('complementary',{name:'脚本工作台侧栏',exact:true});
  const initial=Number(await handle.getAttribute('aria-valuenow'));
  const bounds=await handle.boundingBox();
  await page.mouse.move(bounds.x+bounds.width/2,bounds.y+30);await page.mouse.down();
  await page.mouse.move(bounds.x-80,bounds.y+30,{steps:8});await page.mouse.up();
  assert(Number(await handle.getAttribute('aria-valuenow'))>initial);
  await handle.focus();await handle.press('ArrowRight');
  const resized=Number(await handle.getAttribute('aria-valuenow'));
  assert.equal(Number(await page.evaluate(()=>localStorage.getItem('vac_right_sider_width'))),resized);
  await page.reload();await workbench.waitFor();
  assert.equal(Number(await handle.getAttribute('aria-valuenow')),resized);
  assert((await workbench.boundingBox()).width<720);
  assert((await pane.boundingBox()).x>720,'script pane must be on the right, alongside the program');
  assert(await page.locator('.program-monitor').isVisible());
  check('right script pane supports pointer and keyboard resize, persists width, and coexists with editing');
  for (const id of ['resources', 'editing', 'draw', 'templates', 'export', 'tts']) assert(await page.locator(`.app-nav [data-workspace="${id}"]`).evaluate(el => {
    const item = el.closest('li'), rail = el.closest('ul');
    return getComputedStyle(item).opacity === '1' && item.getBoundingClientRect().right <= rail.getBoundingClientRect().right + 1;
  }), id + ' must remain directly visible');
  check('legacy script link opens the script in the editor right pane');
  const body = workbench.getByRole('textbox', { name: '脚本指令', exact: true });
  await body.fill('{黑');await page.getByRole('option',{name:'黑场淡变',exact:true}).waitFor();await body.press('Tab');assert.equal(await body.inputValue(),'{黑屏}');assert(await body.evaluate(el=>document.activeElement===el));
  await body.fill('{黑');await page.getByRole('option',{name:'黑场淡变',exact:true}).waitFor();await body.press('Shift+Tab');assert.equal(await body.inputValue(),'{黑');assert.equal(await body.evaluate(el=>document.activeElement===el),false);
  await body.fill('无候选描述');await body.press('Tab');assert.equal(await body.inputValue(),'无候选描述');assert.equal(await body.evaluate(el=>document.activeElement===el),false);
  await workbench.getByText('使用内部模型生成文稿（兼容）',{exact:true}).click();
  const send = workbench.getByRole('button', { name: '发送给内部模型', exact: true });
  await body.fill('{未知}'); await page.waitForTimeout(350); assert(await send.isDisabled());
  await body.fill('直接发送尚未保存的脚本'); await page.waitForTimeout(350);
  await send.dblclick();
  await workbench.getByText('Agent 出稿 · 分段 · 历史',{exact:true}).click();
  await page.waitForFunction(() => document.querySelector('#script-narration-0')?.value === '模型生成的旁白');
  assert.equal(script.body, '直接发送尚未保存的脚本');
  assert.deepEqual(calls.map(c => c.p), ['/api/scripts/s1/workbench', '/api/scripts/s1/generate']);
  assert.equal(calls[1].b.expected_version, 2); assert.equal(calls[1].b.profile_id, 'p1'); assert.equal(calls[1].b.profile_revision, 2);
  check('one click saves the current draft then sends exactly once with the new revision and chosen model');
  assert.equal(await workbench.getByRole('button', { name: '人工定稿', exact: true }).count(),0);
  await workbench.getByRole('button', {name:'生成成片与 Premiere 包',exact:true}).click();
  await page.waitForFunction(() => document.querySelector('.script-workbench')?.textContent.includes('可导出'));
  assert.deepEqual(calls.slice(-2).map(call=>call.p),['/api/scripts/s1/workbench/finalize','/api/scripts/s1/export']);
  check('export adopts the current saved narration without a manual approval button');
  await body.fill('未保存脚本保留'); await page.waitForTimeout(350);
  await page.getByRole('button',{name:'收起脚本侧栏',exact:true}).click();
  assert.equal(await workbench.isVisible(),false);
  await page.getByRole('button',{name:'展开脚本侧栏',exact:true}).click();
  assert.equal(await body.inputValue(),'未保存脚本保留');
  check('collapsing and reopening the script pane retains the unsaved draft');
  await page.locator('[data-workspace="resources"]').click();
  await page.locator('[data-workspace="editing"]').click();
  await page.getByRole('button', { name: '脚本', exact: true }).click();
  assert.equal(await body.inputValue(), '未保存脚本保留');
  await workbench.getByRole('button', { name: '查看任务文本', exact: true }).click();
  const handoff = page.getByRole('textbox', { name: 'Agent 任务文本', exact: true });
  assert.match(await handoff.inputValue(), /review-project/); assert.match(await handoff.inputValue(), /未保存脚本保留/);
  await page.getByRole('button', { name: 'Close', exact: true }).click();
  await workbench.getByRole('button', { name: '配置外部 Agent', exact: true }).click();
  await page.getByText('Agent MCP 连接', { exact: true }).waitFor();
  await page.locator('[data-workspace="script"]').click(); assert.equal(await body.inputValue(), '未保存脚本保留');
  check('workspace and Agent settings navigation retain the unsaved script draft');
  const before = calls.filter(c => c.p.endsWith('/generate')).length;
  failSave = true; await send.click();
  await workbench.getByRole('alert').filter({ hasText: '版本冲突' }).waitFor();
  assert.equal(calls.filter(c => c.p.endsWith('/generate')).length, before);
  failSave = false;
  check('save conflicts block model dispatch and retain editable text');
  slow = true; await send.click();
  const stop = workbench.getByRole('button', { name: '停止生成', exact: true });
  await stop.waitFor(); await stop.click(); await stop.waitFor({ state: 'hidden' });
  check('in-flight generation can be stopped');
  await page.waitForTimeout(2700); slow = false; external = true; profileRevision = 3;
  await page.reload(); await body.waitFor();
  await workbench.getByText('使用内部模型生成文稿（兼容）',{exact:true}).click();
  const consent = workbench.locator('.script-model-consent input'); await consent.waitFor();
  assert(await send.isDisabled()); await consent.check();
  await body.fill('需要明确外发授权的新稿'); await page.waitForTimeout(350);
  assert.equal(await consent.isChecked(), false); assert(await send.isDisabled());
  const expectedRevision = version + 2;
  await consent.check(); await send.click();
  await page.waitForFunction(expected => document.querySelector('.script-workbench')?.textContent.includes(`已保存 · 修订 ${expected}`) && !document.querySelector('.script-model-consent input')?.checked, expectedRevision);
  const last = calls.slice(-3);
  assert.equal(last[0].p, '/api/scripts/s1/workbench');
  assert.equal(last[1].p, '/api/processing-profiles/p1/revisions/3/external-consent');
  assert.equal(last[2].p, '/api/scripts/s1/generate');
  check('external profiles require explicit authorization that draft changes and completed requests clear');
  await body.fill('逐行校对\n'.repeat(45) + '{黑屏}'); await page.waitForTimeout(350);
  assert.equal(await workbench.locator('.script-highlight mark').count(), 1);
  await body.evaluate(el => { el.scrollTop = el.scrollHeight; el.dispatchEvent(new Event('scroll')); });
  assert(await body.evaluate(el => el.scrollTop > 0 && el.closest('.script-code-field').querySelector('pre').scrollTop === el.scrollTop));
  check('syntax highlighting and editor scroll remain aligned in the standalone page');
  const beforeOffline = calls.length;
  await consent.check(); offline = true; await page.context().setOffline(true); await send.click();
  await workbench.getByRole('alert').filter({ hasText: /fetch|网络|连接|Failed/i }).waitFor();
  offline = false; await page.context().setOffline(false); await page.waitForTimeout(400);
  assert.equal(calls.length, beforeOffline, 'offline clicks must not enqueue a later model request');
  check('offline model dispatch fails without sending after reconnection');
  for (const width of [1440, 800]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.waitForTimeout(250);
    assert((await pane.boundingBox()).y<200,'script pane must remain reachable in narrow windows');
    await page.screenshot({ path: path.join(output, `script-${width}.png`) });
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
  }
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(base+'/#/export/acceptance');
  await page.locator('[data-workspace="editing"][aria-current="page"]').waitFor();
  assert.equal(await page.getByText('人工验收',{exact:true}).count(),0);
  assert.equal(await page.getByRole('button',{name:'下载验收摘要'}).count(),0);
  check('retired acceptance links open editing without acceptance controls');
  const empty={id:'blank-session',path:'fixture.avp',dirty:false,revision:1,active_tasks:0,document:{format:'avp',schema_version:3,project:{id:'blank-project',name:'空工程'},resources:[],scripts:[],sequences:[],settings:{},workspace:{}},recent:[],recoveries:[]};
  await page.route('**/api/workspace',route=>route.fulfill({json:empty}));
  await page.route('**/api/workspace/state',route=>route.fulfill({json:empty}));
  await page.route('**/api/workspace/save',route=>{empty.document.scripts=empty.document.scripts.map(doc=>({...doc,title:script.title,body:script.body}));return route.fulfill({json:empty});});
  await page.route('**/api/projects/blank-project/resources',route=>{
    if(route.request().method()==='POST'){
      assert.equal(route.request().postDataJSON().kind,'script');version=1;segments=[];script={script_id:'new-script',asset_id:'new-script-resource',title:'未命名脚本',body:'',status:'draft',visible_set_sha:'visible-1'};
      empty.document.resources=[{id:'new-script-resource',asset_id:'new-script-resource',script_id:'new-script',kind:'script',name:'未命名脚本',source:{size_bytes:0},metadata:{},notes:'',in:0,out:0,missing:false}];
      empty.document.scripts=[{format:'avs',schema_version:2,id:'new-script',title:'未命名脚本',body:'',segments:[],bindings:[]}];return route.fulfill({json:empty});
    }return route.fulfill({json:empty.document.resources});
  });
  await page.route('**/api/assets/new-script-resource/scripts',route=>route.fulfill({json:[script]}));
  await page.goto(base+'/#/script');
  await page.getByText('点击「新建脚本」开始，无需先导入素材',{exact:true}).waitFor();
  await page.getByRole('button',{name:'新建脚本',exact:true}).click();
  await page.getByRole('textbox',{name:'脚本指令',exact:true}).fill('按照保存的脚本生成片段');
  await page.getByRole('button',{name:'保存脚本',exact:true}).click();
  await page.getByText('脚本已保存，Agent 可读取当前修订。',{exact:true}).waitFor();
  await page.reload();
  assert.equal(await page.getByRole('textbox',{name:'脚本指令',exact:true}).inputValue(),'按照保存的脚本生成片段');
  check('empty project creates a script and preserves saved instructions after reload');
  await page.evaluate(()=>Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>{window.__agentTask=text;}}}));
  await page.getByRole('textbox',{name:'脚本指令',exact:true}).fill('交给Agent的新修订');
  await page.waitForTimeout(350);
  await page.getByRole('button',{name:'复制 Agent 任务',exact:true}).click();
  await page.waitForFunction(()=>window.__agentTask?.includes('resources.read'));
  assert.equal(script.body,'交给Agent的新修订');
  assert.match(await page.evaluate(()=>window.__agentTask),/new-script-resource/);
  assert.match(await page.evaluate(()=>window.__agentTask),/以下为已保存脚本/);
  await page.getByRole('button',{name:'保存脚本',exact:true}).scrollIntoViewIfNeeded();
  assert((await pane.boundingBox()).x>720,'empty project script pane must remain on the right');
  await page.screenshot({path:path.join(output,'saved-script-agent.png')});
  check('Agent handoff saves pending edits and references the persisted script resource');
  const copied=await page.evaluate(()=>window.__agentTask);
  failSave=true;
  await page.getByRole('textbox',{name:'脚本指令',exact:true}).fill('保存失败时不能交接此草稿');
  await page.getByRole('button',{name:'复制 Agent 任务',exact:true}).click();
  await page.getByRole('status').filter({hasText:'保存或复制失败'}).waitFor();
  assert.equal(await page.evaluate(()=>window.__agentTask),copied);
  assert.equal(await page.getByRole('textbox',{name:'脚本指令',exact:true}).inputValue(),'保存失败时不能交接此草稿');
  failSave=false;
  check('failed script saves preserve the draft and never hand stale instructions to the Agent');
  const library = await browser.newPage({ viewport: { width: 1280, height: 850 } });
  library.on('pageerror', error => errors.push(error.message));
  const payload = { version: 1, id: 'user-title', summary: '我的标题', category: '开场', duration_frames: 90, source: 'export default function Title(){return null;}' };
  const card = { id: payload.id, summary: payload.summary, category: payload.category, styles: [{ id: 'default', description: payload.summary }], user: true };
  let imported = false;
  await library.route('**/api/workspace', route => route.fulfill({ json: { id: 'no-project', revision: 0, recent: [], recoveries: [] } }));
  await library.route('**/api/shotcraft/templates', route => {
    if (route.request().method() === 'POST') {
      assert.deepEqual(route.request().postDataJSON(), payload);
      imported = true;
      return route.fulfill({ status: 201, json: card });
    }
    return route.fulfill({ json: imported ? [card] : [] });
  });
  await library.route('**/api/shotcraft/templates/user-title', route => {
    if (route.request().method() === 'DELETE') {
      imported = false;
      return route.fulfill({ json: { deleted: true } });
    }
    return route.fulfill({ json: payload });
  });
  await library.route('**/api/shotcraft/templates/user-title/preview?*', route => {
    assert.equal(new URL(route.request().url()).searchParams.get('position'), '50');
    return route.fulfill({ contentType: 'image/png', body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aL1sAAAAASUVORK5CYII=', 'base64') });
  });
  await library.goto(base + '/#/resources');
  try {
    await library.getByRole('button', { name: '模板库', exact: true }).click({ timeout: 8000 });
  } catch (error) {
    await library.screenshot({ path: path.join(output, 'shotcraft-home-failure.png') });
    throw new Error((await library.locator('body').innerText()).slice(0, 1000), { cause: error });
  }
  await library.getByRole('heading', { name: 'Shotcraft 模板库' }).waitFor();
  await library.locator('input[type=file]').setInputFiles({ name: 'user-title.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(payload)) });
  await library.getByText('{#user-title}', { exact: true }).waitFor();
  await library.getByRole('img', { name: 'user-title 效果预览' }).waitFor();
  await library.evaluate(() => Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async text => { window.__shotReference = text; } } }));
  await library.getByRole('button', { name: '复制脚本引用' }).click();
  await library.waitForFunction(() => window.__shotReference === '{#user-title}');
  const download = library.waitForEvent('download');
  await library.getByRole('button', { name: '导出 JSON' }).click();
  assert.equal((await download).suggestedFilename(), 'user-title.json');
  await library.setViewportSize({ width: 680, height: 780 });
  await library.screenshot({ path: path.join(output, 'shotcraft-library-680.png') });
  assert.equal(await library.locator('.shotcraft-library').evaluate(el => el.scrollWidth <= el.clientWidth), true);
  await library.getByRole('button', { name: '移除模板' }).click();
  await library.getByRole('dialog').getByRole('button', { name: '移除模板' }).click();
  await library.getByText('还没有导入模板', { exact: true }).waitFor();
  assert.equal(imported, false);
  await library.close();
  check('global Shotcraft library without a project imports, previews, copies, exports and removes a card');
  assert.deepEqual(errors, []); completed = true;
} finally {
  await page.screenshot({ path: path.join(output, 'last-state.png') });
  await fs.writeFile(path.join(output, 'results.json'), JSON.stringify({ completed, checks, errors, mode: 'browser_fixture_ui', real_model: 'not_checked' }, null, 2));
  await browser.close();
}
