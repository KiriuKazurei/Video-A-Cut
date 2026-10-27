// Existing browser acceptance entry: UI fixtures only. Real pixels, services,
// frame clocks and native MCP are verified by the Node/Go drawing suites.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {chromium} from 'playwright-core';
const [base,output]=process.argv.slice(2);
assert(process.env.VAC_TEST_OUTPUT&&base&&output,'Use run-local-smokes via TestWorkspace');
assert(process.env.CHROME&&/chrome(?:\.exe)?$/i.test(process.env.CHROME)&&!/msedge/i.test(process.env.CHROME),'Installed Chrome required; Edge is prohibited');
await fs.mkdir(output,{recursive:true});
const workspace=await (await fetch(base+'/api/workspace')).json();workspace.document.draw_compositions=[];
const imageBytes=await fs.readFile(path.join(process.env.VAC_TEST_OUTPUT,'draw','unified','background.png'));
const frameBytes=await fs.readFile(path.join(process.env.VAC_TEST_OUTPUT,'draw','unified','frame-15.png'));
const resource={id:'image-ui',kind:'image',name:'导入图片.png',source:{sha256:createHash('sha256').update(imageBytes).digest('hex'),size_bytes:imageBytes.length},metadata:{width:320,height:180},notes:'',in:0,out:0,missing:false};
workspace.document.resources.push(resource);
const pid=workspace.document.project.id,sequence=workspace.document.sequences[0],calls=[],checks=[],errors=[];
let runRecord={id:'run-draw',project_id:pid,version:1,state:'queued',execution_mode:'external_mcp',strategy:'draw',owner:'human:desktop',created_at:new Date().toISOString()};
let version=0,failPreview=false,live=0,maxLive=0;
let page;
const browser=await chromium.launch({executablePath:process.env.CHROME,headless:true});
try{
 const context=await browser.newContext({viewport:{width:1440,height:900}});await context.addInitScript(()=>localStorage.setItem('vac_tour_seen_v1','1'));
 page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/workspace',route=>route.fulfill({json:workspace}));
 await page.route('**/api/workspace/state',route=>{workspace.document.workspace=route.request().postDataJSON().state;workspace.revision++;return route.fulfill({json:workspace});});
 await page.route('**/api/projects/'+pid+'/resources/image-ui/thumbnail**',route=>route.fulfill({body:imageBytes,contentType:'image/png'}));
 await page.route('**/api/projects/'+pid+'/visible-set',route=>route.fulfill({json:{sha256:'ui-visible',confirmed:true,assets:[]}}));
 await page.route('**/api/skill-runs',route=>{if(route.request().method()==='POST'){calls.push({agent:route.request().postDataJSON()});return route.fulfill({json:runRecord});}return route.fulfill({json:[]});});
 await page.route('**/api/skill-runs/run-draw',route=>route.fulfill({json:runRecord}));
 await page.route('**/api/projects/'+pid+'/draw-compositions**',async route=>{
  const req=route.request(),url=new URL(req.url()),body=req.method()==='POST'?req.postDataJSON():undefined;
  calls.push({method:req.method(),path:url.pathname,body});
  const ending=url.pathname.split('/').at(-1);
  if(ending==='history')return route.fulfill({json:{can_undo:false,can_redo:false}});
  if(ending==='draw-compositions'){
   if(body){assert.equal(body.session,workspace.id);assert.equal(body.expected_revision,workspace.revision);const c={...structuredClone(body.composition),id:body.composition.id||'slot-ui',revision:++version,content_version:'version-'+version};const at=workspace.document.draw_compositions.findIndex(x=>x.id===c.id);if(at<0)workspace.document.draw_compositions.push(c);else workspace.document.draw_compositions[at]=c;workspace.revision++;workspace.dirty=true;}
   return route.fulfill({json:body?workspace:workspace.document.draw_compositions});
  }
  if(ending==='preview'){
   if(req.method()==='DELETE')return route.fulfill({json:{closed:true}});
   live++;maxLive=Math.max(maxLive,live);await new Promise(r=>setTimeout(r,100));live--;
   if(failPreview)return route.fulfill({status:409,json:{error:{code:'conflict',message:'测试：构图版本已改变'}}});
   return route.fulfill({json:{session:workspace.id,content_version:'pixels',revision:'pixels',request_id:body.request_id,frame:body.frame,png:frameBytes.toString('base64'),geometry:body.document.nodes.filter(n=>!n.hidden).map(n=>({id:n.id,x:n.transform.x,y:n.transform.y,width:n.transform.width??1280,height:n.transform.height??720}))}});
  }
  if(ending==='check')return route.fulfill({json:{valid:true,checks:['graph','clock','source'],engine:'unified',runtime_version:'browser-fixture'}});
  if(ending==='import')return route.fulfill({json:{session:workspace.id,sequence,history:[],content_version:'imported'}});
  return route.fulfill({status:404,json:{error:{code:'not_found',message:'UI fixture has no rendered movie'}}});
 });
 await page.goto(base+'/#/draw');await page.getByRole('button',{name:'新建构图',exact:true}).click();
 await page.getByAltText('当前构图合成帧').waitFor();await page.getByText('当前画面 · 第 0 帧',{exact:true}).waitFor();
 assert.equal(await page.getByRole('radio',{name:'HyperFrames'}).count(),0);assert.equal(await page.getByRole('radio',{name:'Remotion'}).count(),0);checks.push('one default unified canvas without required engine selection');
 await page.getByRole('tab',{name:'素材',exact:true}).click();await page.getByRole('button',{name:'导入图片.png',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('.draw-preview-status')?.textContent?.startsWith('当前画面'));
 await page.evaluate(()=>{window.__drawPointerEvents=[];for(const type of ['pointerdown','pointermove','pointerup','pointercancel','dragstart'])document.addEventListener(type,e=>{if(window.__drawPointerEvents.length<60)window.__drawPointerEvents.push({type,target:e.target.tagName,x:e.clientX,y:e.clientY});},true);});
 const beforeDrag=Number(await page.getByRole('spinbutton',{name:'位置 X',exact:true}).inputValue()),bounds=await page.locator('.draw-selection').boundingBox();
 await page.mouse.move(bounds.x+bounds.width*.25,bounds.y+bounds.height*.4);await page.mouse.down();await page.mouse.move(bounds.x+bounds.width*.25+50,bounds.y+bounds.height*.4+20,{steps:8});await page.mouse.up();
 const afterDrag=Number(await page.getByRole('spinbutton',{name:'位置 X',exact:true}).inputValue());
 await fs.writeFile(path.join(output,'pointer-events.json'),JSON.stringify(await page.evaluate(()=>window.__drawPointerEvents),null,2));
 assert(afterDrag>beforeDrag+10,'canvas image pointer drag must move its layer');checks.push('real browser pointer drag moves the image instead of starting a native image drag');
 await page.getByRole('spinbutton',{name:'位置 X',exact:true}).fill('120');await page.getByRole('spinbutton',{name:'位置 X',exact:true}).press('Tab');
 await page.waitForFunction(()=>document.querySelector('.draw-preview-status')?.textContent?.startsWith('当前画面'));
 assert(calls.some(c=>c.body?.document?.nodes.some(n=>n.kind==='image'&&n.transform.x===120)));checks.push('imported image becomes independently editable live layer');
 await page.getByText('高级 · 源码与实现策略',{exact:true}).click();
 await page.getByRole('button',{name:'添加 HTML 画面',exact:true}).click();await page.getByRole('button',{name:'添加 React 画面',exact:true}).click();
 await page.waitForFunction(()=>document.querySelector('.draw-preview-status')?.textContent?.startsWith('当前画面'));
 const mixed=calls.filter(c=>c.body?.document).at(-1).body.document;assert(mixed.sources.some(s=>s.engine==='hyperframes')&&mixed.sources.some(s=>s.engine==='remotion'));checks.push('HTML and React submit one mixed preview snapshot');
 await page.getByRole('button',{name:'撤销绘制修改'}).click();await page.getByRole('button',{name:'重做绘制修改'}).click();
 const slider=page.getByRole('slider',{name:'构图播放帧'});await slider.focus();for(let i=0;i<20;i++)await slider.press('ArrowRight');
 await page.waitForFunction(()=>document.querySelector('.draw-preview-status')?.textContent?.startsWith('当前画面'));
 assert.equal(maxLive,1);assert(!calls.some(c=>c.path?.endsWith('/render')));checks.push('rapid seeks coalesce to one request; no preliminary movie');
 await Promise.all([page.waitForResponse(r=>r.url().endsWith('/draw-compositions')&&r.request().method()==='POST'),page.getByRole('button',{name:'保存',exact:true}).click()]);await page.getByRole('button',{name:'撤销绘制修改'}).click();await Promise.all([page.waitForResponse(r=>r.url().endsWith('/draw-compositions')&&r.request().method()==='POST'),page.getByRole('button',{name:'保存',exact:true}).click()]);
 assert(calls.filter(c=>c.path?.endsWith('draw-compositions')&&c.body).length>=3);checks.push('save followed by undo remains saveable');
 await page.getByRole('button',{name:'专注预览',exact:true}).click();assert(await page.locator('[data-draw-zone=source]').isHidden());await page.getByRole('button',{name:'退出专注预览',exact:true}).click();
 for(const [width,height]of [[1440,900],[1200,800],[900,1000]]){await page.setViewportSize({width,height});await page.waitForTimeout(150);const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2);assert(!overflow,'horizontal overflow at '+width);await page.screenshot({path:path.join(output,'unified-'+width+'.png'),fullPage:true});}checks.push('desktop and narrow layout, focus mode and retained draft');
 await page.setViewportSize({width:1440,height:900});await page.emulateMedia({reducedMotion:'reduce'});
 await page.locator('.draw-layer-item').filter({hasText:'导入图片.png'}).click();
 const duration=await page.locator('.draw-selection').evaluate(e=>getComputedStyle(e).transitionDuration);assert(duration.split(',').every(d=>parseFloat(d)===0));checks.push('reduced motion retains immediate high-frequency edits');
 failPreview=true;await slider.focus();await slider.press('ArrowRight');await page.getByText('测试：构图版本已改变',{exact:false}).waitFor();assert(await page.getByAltText('当前构图合成帧').count());checks.push('stale/error frame is explicitly marked while last pixels remain visible');
 failPreview=false;await page.getByRole('textbox',{name:'Agent 绘制目标'}).fill('图片背景叠加界面演示与数据卡');await page.getByRole('button',{name:'交给 Agent 制作',exact:true}).click();
 await page.waitForTimeout(250);assert.equal(calls.find(c=>c.agent)?.agent.skill_id,'vac-draw-pipeline');checks.push('one goal uses the automatic pipeline');
 await page.waitForFunction(()=>location.hash.includes('run=run-draw'));
 await page.waitForFunction(()=>document.querySelector('.draw-agent-status')?.textContent?.includes('等待 Agent 领取'));
 runRecord={...runRecord,version:2,owner:'agent:fixture',lease_until:new Date(Date.now()+120000).toISOString(),progress:{phase:'generating',output_chars:128,started_at:new Date().toISOString(),updated_at:new Date().toISOString()}};
 await page.waitForFunction(()=>document.querySelector('.draw-agent-status')?.textContent?.includes('已接收 128 字符'));
 await page.reload();
 await page.waitForFunction(()=>document.querySelector('.draw-agent-status')?.textContent?.includes('已接收 128 字符'));
 assert.match(page.url(),/run=run-draw/);
 checks.push('reloading a drawing task restores live progress from its URL without transient workspace state');
 runRecord={...runRecord,version:3,state:'waiting_human',reason:'model_output_truncated'};
 await page.waitForFunction(()=>document.querySelector('.draw-agent-status')?.textContent?.includes('输出额度不足'));
 runRecord={...runRecord,version:4,state:'succeeded'};
 await page.waitForFunction(()=>document.querySelector('.draw-agent-status')?.textContent?.includes('制作完成'));
 checks.push('task panel follows claim, reported generation, truncation and completion');
 assert.deepEqual(errors,[]);await fs.writeFile(path.join(output,'unified-ui-verification.json'),JSON.stringify({checks,errors,max_in_flight:maxLive,scope:'Chrome UI with mocked API; actual pixels and MCP verified separately'},null,2));
 console.log(JSON.stringify({passed:checks.length,checks}));
}catch(error){if(page){await page.screenshot({path:path.join(output,"failure.png"),fullPage:true});await fs.writeFile(path.join(output,"failure.json"),JSON.stringify({error:String(error),errors,text:await page.locator("body").innerText()},null,2));}throw error;}finally{await browser.close();}
