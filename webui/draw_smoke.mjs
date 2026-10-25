// Existing local browser runner supplies a fixture project; rendering itself is
// verified by Timeline and real MCP suites. This checks actual UI interaction.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';
import {chromium} from 'playwright-core';
const [base,output]=process.argv.slice(2);
assert(process.env.VAC_TEST_OUTPUT&&base&&output,'Use run-local-smokes via TestWorkspace');
await fs.mkdir(output,{recursive:true});
const workspace=await (await fetch(base+'/api/workspace')).json();
workspace.document.draw_compositions=[];
const imageFixture=path.join(process.env.VAC_TEST_OUTPUT,'draw','images-remotion','imported.png'),imageBytes=await fs.readFile(imageFixture);
const imageResource={id:'image-ui',kind:'image',name:'导入图片.png',source:{sha256:createHash('sha256').update(imageBytes).digest('hex'),size_bytes:imageBytes.length},metadata:{width:100,height:60},notes:'',in:0,out:0,missing:false};
workspace.document.resources.push(imageResource);

const pid=workspace.document.project.id,sequence=workspace.document.sequences[0];
assert(process.env.CHROME&&/chrome(?:\.exe)?$/i.test(process.env.CHROME)&&!/msedge/i.test(process.env.CHROME),'Use the installed Chrome executable; Edge is prohibited');
const browser=await chromium.launch({executablePath:process.env.CHROME,headless:true});
const checks=[],errors=[],calls=[];
let version=0,rendered=false,completed=false;
const context=await browser.newContext({viewport:{width:1440,height:900}});
await context.addInitScript(()=>localStorage.setItem('vac_tour_seen_v1','1'));
const page=await context.newPage();
page.on('pageerror',error=>errors.push(error.message));
await page.route('**/api/projects/'+pid+'/resources/image-ui/thumbnail**',route=>route.fulfill({body:imageBytes,contentType:'image/png'}));
await page.route('**/api/projects/'+pid+'/resources/image-ui/content',route=>route.fulfill({body:imageBytes,contentType:'image/png'}));
await page.route('**/api/workspace',route=>route.fulfill({json:workspace}));
await page.route('**/api/workspace/state',route=>{Object.assign(workspace.document.workspace,route.request().postDataJSON().state);return route.fulfill({json:workspace});});
await page.route('**/api/workspace/save',route=>{workspace.dirty=false;return route.fulfill({json:workspace});});
await page.route('**/api/projects/'+pid+'/draw-compositions**',async route=>{
 const req=route.request(),url=new URL(req.url()),body=req.method()==='POST'?req.postDataJSON():undefined;
 calls.push({method:req.method(),path:url.pathname,body});
 const ending=url.pathname.split('/').at(-1),list=workspace.document.draw_compositions;
 if(ending==='draw-compositions'){
  if(body){
   assert.equal(body.session,workspace.id);
   assert.equal(body.expected_revision,workspace.revision);
   const composition={...body.composition,id:body.composition.id||'slot-ui',revision:++version,content_version:'version-'+version};
   const at=list.findIndex(c=>c.id===composition.id);
   if(at<0)list.push(composition);else list[at]=composition;
   workspace.revision++;workspace.dirty=true;rendered=false;
   return route.fulfill({json:workspace});
  }
  return route.fulfill({json:list});
 }
 const composition=list.find(c=>c.id===url.pathname.split('/').at(-2));
 if(ending==='check')return route.fulfill({json:{valid:true,checks:['source','dimensions','timebase'],engine:composition.engine,runtime_version:'browser-fixture'}});
 if(ending==='render'){
  if(req.method()==='GET'){
   if(!rendered)return route.fulfill({status:404,json:{error:{code:'not_found',message:'尚未渲染'}}});
   const data=await fs.readFile(path.join(process.env.VAC_TEST_OUTPUT,'draw',composition.images?.length?'images-remotion':'remotion',...(composition.images?.length?['layer']:[]),'program.mp4'));
   const range=req.headers()['range']?.match(/^bytes=(\d+)-(\d*)$/),start=range?Number(range[1]):0,end=range&&range[2]?Math.min(Number(range[2]),data.length-1):data.length-1;
   return route.fulfill({status:range?206:200,headers:{'Content-Type':'video/mp4','Accept-Ranges':'bytes','Content-Length':String(end-start+1),...(range?{'Content-Range':'bytes '+start+'-'+end+'/'+data.length}:{})},body:data.subarray(start,end+1)});
  }
  await new Promise(resolve=>setTimeout(resolve,450));
  rendered=true;
  return route.fulfill({json:{content_version:composition.content_version,sha256:'verified-fixture',size_bytes:1000,width:composition.width,height:composition.height,frames:composition.duration_frames,duration:1,engine:composition.engine,runtime_version:'browser-fixture'}});
 }
 if(ending==='import'){
  assert.equal(body.sequence_id,sequence.id);
  assert.equal(body.content_version,composition.content_version);
  return route.fulfill({json:{session:workspace.id,project_revision:workspace.revision,sequence,history:[],content_version:'imported-fixture'}});
 }
 return route.fulfill({status:404,json:{error:{code:'not_found'}}});
});
const button=name=>page.getByRole('button',{name,exact:true});
const check=name=>{checks.push(name);console.log('PASS '+name);};
const checkWorkspaceTabs=async()=>{
 const labels=await page.locator('.app-nav').evaluate(nav=>[...nav.querySelectorAll(':scope > li:not(.ant-menu-overflow-item-rest) [data-workspace]')].filter(label=>{const li=label.closest('li'),s=getComputedStyle(li),r=li.getBoundingClientRect(),n=nav.getBoundingClientRect();return s.display!=='none'&&s.visibility!=='hidden'&&s.opacity!=='0'&&li.getAttribute('aria-hidden')!=='true'&&r.width>0&&r.left>=n.left&&r.right<=n.right+1;}).map(label=>label.textContent));
 assert.deepEqual(labels,['资源管理','编辑','绘制','导出','TTS'],'all workspace tabs must be visible directly on the slider');
 assert.equal(Math.round(await page.locator('.app-nav').evaluate(nav=>nav.getBoundingClientRect().width)),600,'workspace slider must stay fixed instead of filling its row');
};

try{
 await page.goto(base+'/#/draw?engine=remotion',{waitUntil:'domcontentloaded'});
 await page.getByTestId('draw-workspace').waitFor({state:'visible'});
 assert.equal(await page.locator('[data-draw-zone]').count(),4);
 await checkWorkspaceTabs();
 await button('新建构图').click();
 await page.getByRole('textbox',{name:'构图源码',exact:true}).waitFor({state:'visible'});
 await page.getByRole('spinbutton',{name:'构图宽度',exact:true}).fill('320');
 await page.getByRole('spinbutton',{name:'构图高度',exact:true}).fill('180');
 await page.getByRole('spinbutton',{name:'构图总帧数',exact:true}).fill('30');
 await page.getByRole('textbox',{name:'构图名称',exact:true}).fill('可编辑绘制测试');
 await button('保存源码').click();
 await page.waitForFunction(()=>!document.querySelector('[data-testid="draw-workspace"]').textContent.includes('源码有未保存更改'));
 assert.equal(workspace.document.draw_compositions[0].name,'可编辑绘制测试');
 check('new composition and source/settings save use current project CAS');
 await button('Lint / Check').click();
 await page.getByText(/检查通过/).first().waitFor();
 check('lint success is visible');
 await button('渲染视频').click();
 await page.getByText('正在本地渲染',{exact:true}).waitFor();
 assert(await button('保存源码').isDisabled());
 await page.waitForFunction(()=>document.querySelector('video[aria-label="绘制成片预览"]')?.readyState>=2);
 await page.getByRole('tab',{name:'渲染结果',exact:true}).click();
 await page.getByText('verified-fixture',{exact:false}).first().waitFor();
 check('render report and actual MP4 preview load');
 await button('下一帧').click();
 await page.waitForTimeout(300);

 await page.waitForFunction(()=>document.querySelector('[data-draw-zone="timeline"]')?.textContent.includes('1 / 30'),{},{timeout:5000});
 check('frame control seeks the video');
 await page.getByRole('tab',{name:'图片',exact:true}).click();
 const imageButton=page.getByRole('button',{name:'导入图片.png',exact:true});
 await imageButton.click();
 const selectedLayerContrast=await page.locator('.draw-layer-item[aria-pressed="true"]').evaluate(node=>{
  const style=getComputedStyle(node),linear=color=>color.match(/[\d.]+/g).slice(0,3).map(Number).map(value=>{const v=value/255;return v<=.04045?v/12.92:((v+.055)/1.055)**2.4;}),luminance=color=>{const [r,g,b]=linear(color);return .2126*r+.7152*g+.0722*b;},a=luminance(style.color),b=luminance(style.backgroundColor);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05);
 });
 assert(selectedLayerContrast>=4.5,'selected image label must retain readable contrast');
 for(const [label,value] of [['图片位置 X','10'],['图片位置 Y','120'],['图片宽度','80'],['图片高度','40'],['图片位移 X','180']])await page.getByRole('spinbutton',{name:label,exact:true}).fill(value);
 await button('保存源码').click();
 await page.waitForFunction(()=>!document.querySelector('[data-testid="draw-workspace"]').textContent.includes('源码有未保存更改'));
 const binding=workspace.document.draw_compositions[0].images[0];
 assert.equal(binding.resource_id,'image-ui');assert.equal(binding.x,10);assert.equal(binding.width,80);assert.equal(binding.dx,180);
 await page.screenshot({path:path.join(output,'draw-images-inspector.png')});
 await imageButton.dragTo(page.locator('[data-draw-zone="canvas"]'));
 await page.getByText('图片图层 · 2/64',{exact:true}).waitFor();
 await button('移除图层').click();
 await page.getByText('图片图层 · 1/64',{exact:true}).waitFor();
 check('project images support click/drag instances, transform settings and layer removal');

 // Editing source must invalidate the import action before a stale render can
 // be used; switching to source uses the same persisted object, not a stub slot.
 await page.getByRole('tab',{name:'组件源码',exact:true}).click();
 const source=page.getByRole('textbox',{name:'构图源码',exact:true});
 await source.fill((await source.inputValue())+'\n// edited source');
 assert(await button('导入编辑时间线').isDisabled());
 check('unsaved source disables import of stale pixels');
 await button('保存源码').click();
 await button('渲染视频').click();
 await page.waitForFunction(()=>document.querySelector('video[aria-label="绘制成片预览"]')?.readyState>=2);
 await page.setViewportSize({width:1280,height:720});
 await button('导入编辑时间线').scrollIntoViewIfNeeded();
 assert(await button('导入编辑时间线').isEnabled());
 const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);
 assert.equal(overflow,false);
 const bounds=await page.evaluate(()=>{
  const footer=document.querySelector('.app-footer').getBoundingClientRect(),timeline=document.querySelector('[data-draw-zone="timeline"]').getBoundingClientRect();
  return {timelineBottom:timeline.bottom,footerTop:footer.top,buttons:[...document.querySelectorAll('[data-draw-zone="timeline"] button')].map(button=>button.getBoundingClientRect().bottom)};
 });
 assert(bounds.timelineBottom<=bounds.footerTop+1,'timeline must fit above the status bar');
 assert(bounds.buttons.every(bottom=>bottom<=bounds.footerTop),'frame controls must be fully visible');
 await checkWorkspaceTabs();
 await page.screenshot({path:path.join(output,'draw-1280.png')});
 const desktop=await page.evaluate(()=>({source:document.querySelector('[data-draw-zone="source"]').getBoundingClientRect().width,importBottom:[...document.querySelectorAll('.draw-handoff button')][0].getBoundingClientRect().bottom,footerTop:document.querySelector('.app-footer').getBoundingClientRect().top}));
 assert(desktop.source>=280,'source editor needs readable width');assert(desktop.importBottom<=desktop.footerTop,'handoff must remain reachable above the footer');
 const normalWidth=await page.locator('.draw-canvas-frame').evaluate(n=>n.getBoundingClientRect().width);
 await button('专注预览').click();assert(await page.locator('[data-draw-zone="source"]').isHidden());assert(await page.locator('[data-draw-zone="inspector"]').isHidden());assert(await page.locator('.draw-canvas-frame').evaluate(n=>n.getBoundingClientRect().width)>normalWidth+200);
 await button('退出专注预览').click();assert(await page.locator('[data-draw-zone="source"]').isVisible());
 check('preview focus expands the monitor and restores the editing panels without losing controls');
 await page.setViewportSize({width:1024,height:768});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1),false);
 assert(await button('导入编辑时间线').isVisible());
 await checkWorkspaceTabs();
 for(const width of [900,800,640]){await page.setViewportSize({width,height:768});await checkWorkspaceTabs();}
 await page.setViewportSize({width:1024,height:768});
 check('all five workspace tabs stay directly visible at fixed 600px across desktop and narrow windows');
 await page.screenshot({path:path.join(output,'draw-1024.png')});
 await page.setViewportSize({width:1280,height:720});
 const transfer=await page.evaluateHandle(()=>{const data=new DataTransfer();data.setData('application/x-vac-resource','{}');return data;});
 await page.locator('[data-draw-zone="canvas"]').dispatchEvent('dragover',{dataTransfer:transfer});
 await page.waitForFunction(()=>Number(getComputedStyle(document.querySelector('.draw-drop-feedback')).opacity)>.99);
 await page.screenshot({path:path.join(output,'draw-drop-feedback.png')});
 await page.emulateMedia({reducedMotion:'reduce'});
 assert.equal(await page.locator('.draw-drop-feedback').evaluate(n=>parseFloat(getComputedStyle(n).transitionDuration)),0);
 assert.equal(await page.locator('.draw-drop-feedback').evaluate(n=>getComputedStyle(n).transform),'none');
 await page.locator('[data-draw-zone="canvas"]').dispatchEvent('dragleave',{relatedTarget:null});
 await page.emulateMedia({reducedMotion:'no-preference'});
 await button('更多绘制操作').focus();await page.keyboard.press('Enter');
 await page.getByRole('menuitem',{name:'重置布局',exact:true}).waitFor();await page.keyboard.press('End');await page.keyboard.press('Enter');
 await page.getByRole('menuitem',{name:'重置布局',exact:true}).waitFor({state:'hidden'});
 check('compact layout, keyboard menu, drag feedback and reduced motion remain usable');

 check('compact desktop keeps complete frame controls above the footer without horizontal overflow');
 await button('导入编辑时间线').click();
 await page.waitForURL(/#\/editing/);
 await page.locator('.timeline-row').first().waitFor({state:'visible'});
 await page.waitForTimeout(300);
 assert(calls.some(c=>c.path.endsWith('/import')));
 check('render import returns to the existing editor');
 // Extend the same project/resource UI acceptance with a RAW recipe fixture.
 const raw={...imageResource,id:'raw-ui',name:'Sony.ARW',raw_develop:{exposure:0,white_balance:'camera',contrast:1,saturation:1},metadata:{width:100,height:60,extension:'.arw',codec_name:'camera_raw'}};
 workspace.document.resources.push(raw);workspace.document.workspace.selected_resource=raw.id;workspace.document.workspace.workspace='resources';
 let rawWrites=[];
 await page.route('**/api/projects/'+pid+'/resources/raw-ui/content**',route=>route.fulfill({body:imageBytes,contentType:'image/png'}));
 await page.route('**/api/projects/'+pid+'/resources/raw-ui/thumbnail**',route=>route.fulfill({body:imageBytes,contentType:'image/png'}));
 await page.route('**/api/projects/'+pid+'/resources/raw-ui',async route=>{const patch=route.request().postDataJSON();rawWrites.push(patch);Object.assign(raw,patch);workspace.revision++;workspace.dirty=true;await route.fulfill({json:workspace});});
 await page.goto(base+'/?raw-acceptance=1#/resources');
 await page.locator('[data-resource-id="raw-ui"]').click();
 await page.getByRole('spinbutton',{name:'RAW 曝光 EV',exact:true}).waitFor({state:'visible'});
 await page.getByRole('spinbutton',{name:'RAW 曝光 EV',exact:true}).fill('1');
 await button('应用 RAW 调整').click();
 await page.waitForFunction(()=>document.querySelector('img[alt="Sony.ARW"]')?.getAttribute('src')?.includes('exposure%22%3A1'));
 assert.equal(rawWrites.at(-1).raw_develop.exposure,1);assert.equal(rawWrites.at(-1).raw_develop.saturation,1);
 await page.reload();await page.getByRole('spinbutton',{name:'RAW 曝光 EV',exact:true}).waitFor();assert.equal(Number(await page.getByRole('spinbutton',{name:'RAW 曝光 EV',exact:true}).inputValue()),1);
 await button('重置参数').click();await button('应用 RAW 调整').click();
 await page.waitForFunction(()=>document.querySelector('img[alt="Sony.ARW"]')?.getAttribute('src')?.includes('exposure%22%3A0'));
 assert.equal(rawWrites.at(-1).raw_develop.exposure,0);
 await page.screenshot({path:path.join(output,'raw-controls-1280.png')});
 check('RAW whole-image settings apply, refresh the source, persist across reload and reset');
 assert.deepEqual(errors,[]);completed=true;
}finally{
 await page.screenshot({path:path.join(output,'last-state.png')});
 await fs.writeFile(path.join(output,'results.json'),JSON.stringify({mode:'browser_fixture_ui',browser_executable:process.env.CHROME,completed,checks,errors,real_render_gate:'timeline + MCP suites',human_acceptance:'not_checked'},null,2));
 await browser.close();
}
