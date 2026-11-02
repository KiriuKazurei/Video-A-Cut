// Settings UX: use the shared isolated server and local Chrome. No provider calls.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {chromium} from 'playwright-core';
const [base,output]=process.argv.slice(2);
assert(process.env.VAC_TEST_OUTPUT&&base&&output,'Use run-local-smokes via TestWorkspace');
assert(process.env.CHROME&&/chrome(?:\.exe)?$/i.test(process.env.CHROME)&&!/msedge/i.test(process.env.CHROME),'Chrome only');
await fs.mkdir(output,{recursive:true});
const browser=await chromium.launch({executablePath:process.env.CHROME,headless:true});
const checks=[],errors=[],saved=[];
let completed=false,failSave=true;
let diagnosticCalls=0,diagnosticFailure=false;
let agentState='empty',agentStatusFailure=false,agentStatusCalls=0;
const profile={schema_version:1,profile_id:'ux_profile',revision:1,name:'本地剪辑',content_mode:'builtin',vision:{adapter:'builtin'},narration:{adapter:'builtin'},sampling:{max_frames:12,max_bytes:12582912,timeout_seconds:60},tts_voice:'本机声音',export_target:'premiere'};
const context=await browser.newContext({viewport:{width:1440,height:900}});
await context.addInitScript(()=>localStorage.setItem('vac_tour_seen_v1','1'));
const page=await context.newPage();
await page.clock.install();
page.on('pageerror',e=>errors.push(e.message));
await page.route('**/api/agents/status',route=>{
 agentStatusCalls++;
 if(agentStatusFailure)return route.fulfill({status:503,json:{error:{code:'unavailable',message:'status fixture unavailable'}}});
 return route.fulfill({json:{generated_at:new Date().toISOString(),stale_after_seconds:90,agents:agentState==='empty'?[]:[{agent_id:'local-client',name:'测试外部 Agent',version:'1',transport:'desktop_mcp',state:agentState,connected_at:new Date().toISOString(),last_seen_at:new Date().toISOString(),last_tool:'projects.list',active_calls:agentState==='busy'?1:0}]}});
});
await page.route('**/api/processing-profiles**',async route=>{
 const req=route.request();
 if(req.method()==='GET')return route.fulfill({json:new URL(req.url()).pathname==='/api/processing-profiles'?[profile]:{profile}});
 const body=req.postDataJSON();saved.push(body);
 await new Promise(resolve=>setTimeout(resolve,250));
 if(failSave)return route.fulfill({status:500,json:{error:{code:'save_failed',message:'测试保存失败，请重试'}}});
 Object.assign(profile,body.profile??body);
 return route.fulfill({json:profile});
});
await page.route('**/api/workspace/providers',route=>route.fulfill({json:{external_tts:false}}));
await page.route('**/api/system/diagnostics',async route=>{
 diagnosticCalls++;await new Promise(resolve=>setTimeout(resolve,350));
 if(diagnosticFailure)return route.fulfill({status:500,json:{error:{code:'diagnostic_incomplete',message:'fixture unavailable'}}});
 return route.fulfill({json:{schema_version:1,generated_at:'2026-10-29T00:00:00Z',platform:'windows',architecture:'amd64',status:'warning',redacted:true,scope:'local_read_only_no_model_requests',checks:[
  {id:'database',label:'本地数据库',status:'passed',message:'运行状态可读取。'},
  {id:'dependency.node',label:'Node.js',status:'failed',message:'未找到可执行程序。',next_step:'安装所需版本。'},
  {id:'models.configuration',label:'模型配置',status:'not_checked',message:'未验证服务连通性或模型质量。'}]}});
});
const check=name=>{checks.push(name);console.log('PASS '+name);};
const button=name=>page.getByRole('button',{name,exact:true});
const category=name=>page.getByRole('menuitem',{name,exact:true});
const selectCategory=async value=>{
 const input=page.getByRole('combobox',{name:'设置分类',exact:true});
 if(await input.getAttribute('aria-expanded')!=='true')await page.locator('.settings-category-picker .ant-select-selector').click();
 await page.locator('.ant-select-dropdown:visible [data-value="'+value+'"]').click();
 await page.locator('.ant-select-dropdown:visible').waitFor({state:'hidden'});
};
try {
 await page.goto(base+'/#/settings/profiles');
 await page.getByRole('textbox',{name:'预设名称',exact:true}).waitFor();
 assert.equal(await page.locator('.next-step-btn:visible').count(),0,'settings must not push an unrelated recording workflow');
 assert.equal(await page.getByRole('switch',{name:'允许云端配音'}).count(),0,'global provider policy belongs in providers');
 await category('Agent 与 Skill').click();
 await page.getByText('Agent MCP 连接',{exact:true}).waitFor();
 const agentPanel=page.getByRole('region',{name:'外部 Agent 状态',exact:true});
 await agentPanel.getByText('尚无 Agent 活动',{exact:true}).waitFor();
  agentState='connected';await agentPanel.getByRole('button',{name:'刷新 Agent 状态',exact:true}).click();
 await agentPanel.getByText('1 个 Agent 最近有活动',{exact:true}).waitFor();
 await agentPanel.scrollIntoViewIfNeeded();await page.screenshot({path:path.join(output,'agent-status-connected.png')});
 // Start the polling interval after screenshot work, which may exceed one second.
 const refreshed=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/agents/status');
 await agentPanel.getByRole('button',{name:'刷新 Agent 状态',exact:true}).click();await refreshed;
 await agentPanel.getByRole('button',{name:'刷新 Agent 状态',exact:true}).waitFor({state:'visible'});
 const beforePoll=agentStatusCalls;agentState='busy';
 await page.clock.fastForward(179000);await page.waitForTimeout(100);
 assert.equal(agentStatusCalls,beforePoll,'Agent status must not poll before 3 minutes');
 await page.clock.fastForward(1000);await agentPanel.getByText('1 个 Agent 正在调用工具',{exact:true}).waitFor({timeout:8000});
 agentState='stale';await agentPanel.getByRole('button',{name:'刷新 Agent 状态',exact:true}).click();
 await agentPanel.getByText('Agent 连接待确认',{exact:true}).waitFor();
 agentStatusFailure=true;await agentPanel.getByRole('button',{name:'刷新 Agent 状态',exact:true}).click();
 await agentPanel.getByText('Agent 状态读取失败',{exact:true}).waitFor();
 await page.screenshot({path:path.join(output,'agent-status-unavailable.png')});
 assert.equal(await agentPanel.getByText('1 个 Agent 最近有活动',{exact:true}).count(),0);
 agentStatusFailure=false;agentState='disconnected';await agentPanel.getByRole('button',{name:'刷新 Agent 状态',exact:true}).click();
 await agentPanel.getByText('Agent 已断开',{exact:true}).waitFor();
 check('Agent status hook polls at 3 minutes, preserves manual refresh and hides cached online state on error');
 await category('处理预设').click();
 const pausedCalls=agentStatusCalls;await page.clock.fastForward(180000);await page.waitForTimeout(100);assert.equal(agentStatusCalls,pausedCalls,'inactive settings must stop Agent polling');
 check('settings navigation separates configuration and diagnostics without unrelated next step');
 await category('状态监控').click();
 await page.getByText('统一诊断',{exact:true}).waitFor();
 assert.equal(diagnosticCalls,0,'diagnostics must be explicitly requested');
 assert.equal(await button('下载脱敏报告').isDisabled(),true);
 await button('运行诊断').click();
 await page.getByText('正在检查本机环境…',{exact:true}).waitFor();
 await page.getByText('未找到可执行程序。',{exact:true}).waitFor();
 const downloadPromise=page.waitForEvent('download');await button('下载脱敏报告').click();
 const download=await downloadPromise;const target=path.join(output,'diagnostics.json');await download.saveAs(target);
 const diagnostic=JSON.parse(await fs.readFile(target,'utf8'));assert.equal(diagnostic.redacted,true);assert.equal(diagnostic.checks[2].status,'not_checked');
 await page.screenshot({path:path.join(output,'diagnostics-1440.png')});
 diagnosticFailure=true;await button('运行诊断').click();
 await page.getByText('诊断未完成，请确认后台可连接后重试。',{exact:true}).waitFor();
 assert.equal(await button('下载脱敏报告').isDisabled(),true);
 diagnosticFailure=false;await button('运行诊断').click();
 await page.waitForFunction(()=>[...document.querySelectorAll('button')].some(button=>button.textContent==='下载脱敏报告'&&!button.disabled));
 await page.getByText('诊断未完成，请确认后台可连接后重试。',{exact:true}).waitFor({state:'hidden'});
 check('diagnostics are manual, downloadable, explicit about unverified models, and recover after an error');
 await page.setViewportSize({width:800,height:800});await page.screenshot({path:path.join(output,'diagnostics-800.png')});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
 check('diagnostics fit the compact settings layout');
 await page.setViewportSize({width:1440,height:900});await category('处理预设').click();
 await page.getByRole('textbox',{name:'预设名称',exact:true}).fill('保留中的草稿');
 await category('服务商接入').click();
 await page.getByRole('switch',{name:'允许云端配音'}).waitFor();
 await category('处理预设').click();
 assert.equal(await page.getByRole('textbox',{name:'预设名称',exact:true}).inputValue(),'保留中的草稿');
 await button('新建预设').click();
 await page.getByText('新建会替换未保存的预设草稿。',{exact:true}).waitFor();
 await button('保留草稿').click();
 assert.equal(await page.getByRole('textbox',{name:'预设名称',exact:true}).inputValue(),'保留中的草稿');
 check('category changes preserve drafts and replacement can be cancelled');
 await page.getByRole('textbox',{name:'SAPI 声音名称',exact:true}).fill('本机声音');
 await button('保存新版本').click();
 await page.getByText('测试保存失败，请重试',{exact:true}).waitFor();
 assert.equal(await page.getByRole('textbox',{name:'预设名称',exact:true}).inputValue(),'保留中的草稿');
 failSave=false;
 await button('保存新版本').click();
 await page.getByText('已保存 v1',{exact:true}).waitFor();
 assert.equal(saved.length,2);
 assert.equal((saved[1].profile??saved[1]).name,'保留中的草稿');
 check('failed save keeps editable data and retry persists the same draft');
 await page.screenshot({path:path.join(output,'settings-profiles-1440.png')});
 await button('新建预设').click();
 await page.getByRole('textbox',{name:'预设名称',exact:true}).fill('窄屏草稿');
 await category('服务商接入').click();
 await button('切换为已配置模型以接入服务商').click();
 await page.getByRole('textbox',{name:'画面识别 模型 ID',exact:true}).fill('test-vision');
 await page.getByRole('combobox',{name:'画面识别 推理强度',exact:true}).focus();
 await page.keyboard.press('ArrowDown');
 await page.locator('.ant-select-dropdown:visible .ant-select-item-option-content').filter({hasText:/^high$/}).click();
 await page.getByRole('spinbutton',{name:'画面识别 最大输出 Token',exact:true}).fill('48000');
 await category('处理预设').click();await category('服务商接入').click();
 assert.equal(await page.getByRole('spinbutton',{name:'画面识别 最大输出 Token',exact:true}).inputValue(),'48000');
 assert(await page.getByRole('combobox',{name:'画面识别 推理强度',exact:true}).evaluate(node=>node.closest('.ant-select')?.textContent.includes('high')));
 check('model effort and output limit survive category changes in an unsaved provider draft');
 await page.getByRole('checkbox',{name:'启用转录',exact:true}).check();
 await page.getByRole('textbox',{name:'本地模型目录',exact:true}).fill('C:/LocalModels/whisper');
 await page.screenshot({path:path.join(output,'settings-providers-configured-1440.png')});
 await category('处理预设').click();
 check('provider and local transcription fields remain explicit and preserve the shared profile draft');
 for(const width of [1280,1024,900,800,640]){
  await page.setViewportSize({width,height:800});
  if(width<992){
   const selector=page.getByRole('combobox',{name:'设置分类',exact:true});
   await selectCategory('providers');
   await page.getByRole('switch',{name:'允许云端配音'}).waitFor();
   await selectCategory('profiles');
  }
  assert.equal(await page.getByRole('textbox',{name:'预设名称',exact:true}).inputValue(),'窄屏草稿');
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false,`no horizontal overflow at ${width}`);
  assert.equal(Math.round(await page.locator('.app-nav').evaluate(n=>n.getBoundingClientRect().width)),600);
  await page.screenshot({path:path.join(output,`settings-${width}.png`)});
 }
 check('responsive navigation and forms work from 1440 to 640 while retaining the fixed workspace rail');
 const selector=page.getByRole('combobox',{name:'设置分类',exact:true});
 await selector.focus();await page.keyboard.press('ArrowDown');
 await page.waitForFunction(()=>document.querySelector('.settings-category-picker [role="combobox"]')?.getAttribute('aria-expanded')==='true');
 await page.keyboard.press('Escape');
 await page.waitForFunction(()=>document.querySelector('.settings-category-picker [role="combobox"]')?.getAttribute('aria-expanded')==='false');
 await page.emulateMedia({reducedMotion:'reduce'});
 assert.equal(await page.locator('.settings-category-picker .ant-select-selector').evaluate(node=>getComputedStyle(node).transitionDuration),'0s');
 await selectCategory('status');await page.getByText('控制面连接',{exact:true}).waitFor();
 await selectCategory('profiles');
 await page.getByRole('textbox',{name:'预设名称',exact:true}).focus();
 await page.keyboard.press('6');
 assert.match(page.url(),/settings\/profiles/);
 check('keyboard navigation and reduced motion do not trigger workspace shortcuts inside a field');
 await page.goto(base+'/#/tts');await page.getByRole('textbox',{name:'TTS 朗读文本'}).waitFor();
 assert.equal(await page.locator('.next-step-btn:visible').count(),0);
 check('standalone TTS does not show recording next-step prompts');
 const desktopPage = await context.newPage();
 await desktopPage.addInitScript(({ base }) => {
   window.__copied = []; window.__copyFail = false;
   window.__runtime = { revision: 1, status: 'ready', api_base_url: base, mcp_url: 'http://127.0.0.1:43210/mcp', session_id: 'test-desktop', data_directory: '', message: '', workers: [] };
   window.__TAURI__ = { core: { invoke: async (name, args) => {
     if (name === 'runtime_status') return window.__runtime;
     if (name === 'update_status') return { revision: 1, phase: 'idle', current_version: '0.3.2' };
     if (name === 'clipboard_write_text') { if (window.__copyFail) throw Error('clipboard fixture failure'); window.__copied.push(args.text); }
   } }, event: { listen: async (name, callback) => { if (name === 'runtime-status') window.__runtimeListener = callback; return () => {}; } } };
 }, { base });
 desktopPage.on('pageerror', e => errors.push(e.message));
 try {
   await desktopPage.goto(base + '/#/settings/skills');
   const install = desktopPage.getByRole('button', { name: '一键复制安装 MCP', exact: true });
   await install.waitFor(); await install.click();
   await desktopPage.getByText('MCP 安装命令已复制', { exact: true }).waitFor();
   assert.deepEqual(await desktopPage.evaluate(() => window.__copied), ['codex mcp add vac --url http://127.0.0.1:43210/mcp']);
   await desktopPage.evaluate(() => { window.__runtime = { ...window.__runtime, revision: 2, status: 'stopped' }; window.__runtimeListener({ payload: window.__runtime }); });
   await install.waitFor({ state: 'hidden' });
   assert.equal(await desktopPage.getByRole('textbox', { name: 'MCP 安装命令', exact: true }).count(), 0, 'stopped desktop replaces the entire settings shell');
   await desktopPage.evaluate(() => { window.__runtime = { ...window.__runtime, revision: 3, status: 'ready', mcp_url: 'http://127.0.0.1:54321/mcp' }; window.__runtimeListener({ payload: window.__runtime }); });
   await desktopPage.waitForFunction(() => document.querySelector('[aria-label="MCP 安装命令"]')?.value.includes('54321'));
   await install.click(); await desktopPage.waitForFunction(() => window.__copied.length === 2);
   assert.equal(await desktopPage.evaluate(() => window.__copied.at(-1)), 'codex mcp add vac --url http://127.0.0.1:54321/mcp');
   await desktopPage.evaluate(() => { window.__copyFail = true; }); await install.click();
   await desktopPage.getByRole('alert').filter({ hasText: '复制失败' }).waitFor();
   assert.equal(await desktopPage.evaluate(() => window.__copied.length), 2);
   for (const width of [1440, 800]) {
     await desktopPage.setViewportSize({ width, height: 1000 });
     await desktopPage.screenshot({ path: path.join(output, `mcp-${width}.png`) });
     assert.equal(await desktopPage.evaluate(() => document.documentElement.scrollWidth > innerWidth), false);
   }
   check('desktop MCP command copies exactly, clears on stop, refreshes after restart and reports clipboard failure');
 } finally { await desktopPage.close(); }
 assert.deepEqual(errors,[]);completed=true;
} finally {
 await page.screenshot({path:path.join(output,'last-state.png')});
 await fs.writeFile(path.join(output,'dropdown-state.json'),JSON.stringify(await page.locator('.ant-select-dropdown').evaluateAll(nodes=>nodes.map(n=>({class:n.className,rect:n.getBoundingClientRect().toJSON(),display:getComputedStyle(n).display,text:n.textContent}))),null,2));
 await fs.writeFile(path.join(output,'results.json'),JSON.stringify({completed,checks,errors,mode:'browser_fixture_ui',human_acceptance:'not_checked'},null,2));
 await browser.close();
}
