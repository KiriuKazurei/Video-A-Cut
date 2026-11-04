// Existing keyboard browser suite updated for the unified editor. API writes are
// recorded local fixtures; authoritative editing/rendering is covered in Go/native.
import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {join} from 'node:path';
const base=process.argv[2]??'http://127.0.0.1:8816';
const {chromium}=await import(process.env.PLAYWRIGHT_CORE??'playwright-core');
let page;
assert(process.env.CHROME&&/chrome(?:\.exe)?$/i.test(process.env.CHROME)&&!/msedge/i.test(process.env.CHROME),'Use the installed Chrome executable; Edge is prohibited');
const browser=await chromium.launch({executablePath:process.env.CHROME,args:['--no-sandbox']});
try {
 const context=await browser.newContext({viewport:{width:1600,height:1000}});
 await context.addInitScript(()=>localStorage.setItem('vac_tour_seen_v1','1'));
 page=await context.newPage();const errors=[],writes=[],approvals=[];page.on('pageerror',error=>errors.push(String(error)));
 let view,number=0;
 const ok=text=>console.log(`ok ${++number} ${text}`);
 const frameRequests=[],movieRequests=[],releasedPreviews=[];
 let releaseFrame,releaseMovie;const firstFrame=new Promise(resolve=>{releaseFrame=resolve;}),movieFinished=new Promise(resolve=>{releaseMovie=resolve;});
 const png='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a4F8AAAAASUVORK5CYII=';
 await page.route(/\/sequences\/seq-review\/preview\/frame(?:\?|$)/,async route=>{
  if(route.request().method()==='DELETE'){releasedPreviews.push(route.request().url());return route.fulfill({json:{closed:true}});}
  const body=route.request().postDataJSON();frameRequests.push(body);
  if(frameRequests.length===1)await firstFrame;
  if(body.frame===46)await new Promise(resolve=>setTimeout(resolve,500));
  await route.fulfill({json:{content_version:'browser-fixture',frame:body.frame,image:png+'#'+body.frame,diagnostics:{backend:'webgl2',hardware_accelerated:true,renderer:'browser fixture GPU'}}}).catch(()=>{});
 });
 await page.route(/\/sequences\/seq-review\/preview(?:\?|$)/,async route=>{
  if(route.request().method()==='GET')return route.fulfill({path:fileURLToPath(new URL('../workers/node/timeline-cli/fixtures/media/clip_01.mp4',import.meta.url)),contentType:'video/mp4'});
  movieRequests.push(route.request().postDataJSON());
  // Hold the movie response so the monitor must display the current still.
  await movieFinished;await route.fulfill({json:{content_version:'browser-fixture',frames:180,duration:6,width:1920,height:1080,fps:{num:30,den:1}}}).catch(()=>{});
 });
 await page.route('**/api/projects/review-project/sequences/seq-review',async route=>{
  if(!view)view=await (await route.fetch()).json();await route.fulfill({json:view});
 });
 await page.route('**/api/projects/review-project/sequences/seq-review/commands',async route=>{
  const request=route.request().postDataJSON();writes.push(request);view={...view,sequence:{...view.sequence,revision:view.sequence.revision+1}};await route.fulfill({json:view});
 });
 await page.route(/\/api\/workflows\/[^/]+\/(scene|narration)-reviews$/,route=>{approvals.push(route.request().postDataJSON());return route.fulfill({status:409,json:{error:'unexpected legacy approval'}});});
 const load=async()=>{await page.goto(base+'/#/editing/import');await page.locator('.sequence-timeline').waitFor();await page.locator('[data-clip-id="clip-0"]').waitFor();};
 const clip=page.locator('[data-clip-id="clip-0"]'),timeline=page.locator('.sequence-timeline');
 const press=async key=>{await page.keyboard.press(key);await page.waitForTimeout(200);};
 const cancel=async()=>{await page.locator('.ant-modal-confirm:visible').waitFor();await page.locator('.ant-modal-confirm:visible .ant-btn').filter({hasText:'取消'}).click();await page.locator('.ant-modal-confirm:visible').waitFor({state:'hidden'});};
 const command=async key=>{const count=writes.length;await press(key);for(let i=0;i<100&&writes.length===count;i++)await page.waitForTimeout(20);assert.equal(writes.length,count+1);return writes.at(-1);};
 await load();for(let i=0;i<150&&!frameRequests.length;i++)await page.waitForTimeout(20);
 assert.equal(frameRequests[0]?.frame,45,'request the saved playhead before the movie');assert.equal(movieRequests.length,0,'movie must wait for the first frame');releaseFrame();
 await page.getByRole('img',{name:'当前帧预览',exact:true}).waitFor();for(let i=0;i<100&&!movieRequests.length;i++)await page.waitForTimeout(20);
 assert.equal(movieRequests.length,1);assert.equal(await page.getByRole('img',{name:'当前帧预览',exact:true}).isVisible(),true);assert.equal(await page.getByText('GPU 合成',{exact:true}).count(),1);ok('current playhead and actual preview backend appear while the full movie is still rendering');
 const exposed=await page.getByRole('img',{name:'当前帧预览',exact:true}).evaluate(image=>{const r=image.getBoundingClientRect(),status=image.parentElement.querySelector('.program-updating')?.getBoundingClientRect();return {topmost:document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)===image,statusHeight:status?.height??0,imageHeight:r.height};});
 assert(exposed.topmost&&exposed.statusHeight<exposed.imageHeight/4,'background movie progress must not cover the current frame');ok('background progress leaves the current frame exposed');
 await page.getByRole('button',{name:'节目前进一帧',exact:true}).click();
 for(let i=0;i<100&&!frameRequests.some(r=>r.frame===46);i++)await page.waitForTimeout(20);
 await page.getByRole('button',{name:'节目前进一帧',exact:true}).click();
 const still=page.getByRole('img',{name:'当前帧预览',exact:true});await still.waitFor();
 for(let i=0;i<100&&!(await still.getAttribute('src'))?.endsWith('#47');i++)await page.waitForTimeout(20);
 await page.waitForTimeout(550);assert((await still.getAttribute('src')).endsWith('#47'),'late old frame replaced the latest playhead');assert.equal(movieRequests.length,1,'seeking restarted the movie');
 await page.getByRole('button',{name:'节目时间码',exact:true}).click();await page.getByRole('textbox',{name:'节目时间码',exact:true}).fill('1.5');await page.getByRole('textbox',{name:'节目时间码',exact:true}).press('Enter');ok('late seek results are discarded without restarting the background movie');
 assert.equal(await page.locator('.timeline-row').count(),6);assert.equal(await page.locator('.workflow-panel:visible').count(),0);assert.equal(await page.locator('.timeline-ruler>.track-heading').innerText(),'00:00:01:15');await page.reload();await clip.waitFor();assert.equal(await page.locator('.timeline-ruler>.track-heading').innerText(),'00:00:01:15');ok('unified editor restores a saved nonzero playhead on load and reload with six tracks');
 assert.equal(await clip.evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(62, 111, 176)');assert.match(await page.locator('[data-clip-id="clip-3"]').innerText(),/流 1/);ok('ordinary video and audio use theme blue and show actual stream identity');
 await clip.focus();await press('Enter');assert.equal(await clip.getAttribute('aria-pressed'),'true');assert.equal(await page.locator('[data-clip-id="clip-3"]').getAttribute('aria-pressed'),'true');ok('keyboard clip selection includes its linked sound');
 await press('c');assert.equal(await page.getByRole('button',{name:'剃刀工具 C',exact:true}).getAttribute('aria-pressed'),'true');assert.equal(approvals.length,0);ok('C selects the razor without legacy approval');
 await press('v');assert.equal(await page.getByRole('button',{name:'选择工具 V',exact:true}).getAttribute('aria-pressed'),'true');ok('V restores selection tool');
 const split=await command('Control+k');assert.equal(split.session,'review-session');assert.equal(split.operations[0].type,'clip.split');assert.deepEqual(split.operations[0].clip_ids,['clip-0','clip-3']);assert(split.expected_revision>=1&&split.idempotency_key);ok('Ctrl+K dispatches linked stable IDs with CAS and idempotency');
 const search=page.getByRole('searchbox',{name:'搜索当前工程资源'});await search.focus();const count=writes.length;await page.keyboard.type('mcio');assert.equal(writes.length,count);assert.equal(await page.locator('.ant-modal:visible').count(),0);await search.fill('');ok('typing letters in a resource input never edits tracks');
 await search.focus();await page.locator('.timeline-scale').click({position:{x:60,y:8}});assert.equal(await timeline.evaluate(n=>document.activeElement===n),true);await press('m');assert.equal(await page.getByRole('textbox',{name:'标记备注',exact:true}).count(),1);await press('Control+k');assert.equal(writes.length,count);await cancel();ok('M offers one point and visible modal guards background editing');
 await timeline.focus();await press('i');assert.match(await page.getByRole('textbox',{name:'标记备注',exact:true}).inputValue(),/^入点/);await cancel();await timeline.focus();await press('o');assert.match(await page.getByRole('textbox',{name:'标记备注',exact:true}).inputValue(),/^出点/);await cancel();ok('I and O open separately editable in and out markers');
 await timeline.focus();const before=await page.locator('.timeline-ruler>.track-heading').innerText();await press('ArrowRight');assert.notEqual(await page.locator('.timeline-ruler>.track-heading').innerText(),before);await press('ArrowLeft');assert.equal(await page.locator('.timeline-ruler>.track-heading').innerText(),before);ok('arrows step one frame and return to the same timecode');
 await clip.click({button:'right'});await page.getByRole('menuitem',{name:/在播放头分割/}).waitFor();const n=writes.length;await page.getByRole('menuitem',{name:/在播放头分割/}).click();await page.waitForTimeout(250);assert.equal(writes.length,n+1);assert.equal(writes.at(-1).operations[0].type,'clip.split');ok('context menu uses the same split command');
 await clip.focus();await press('Control+c');const pasted=await command('Control+v');assert.equal(pasted.operations.length,2);assert(pasted.operations.every(op=>op.type==='clip.paste'));ok('copy and paste dispatch video and sound as one command');
 await clip.focus();assert.equal((await command('Control+z')).operations[0].type,'history.undo');assert.equal((await command('Control+Shift+z')).operations[0].type,'history.redo');ok('undo and redo stay in the focused sequence');
 await timeline.focus();await press('?');await page.locator('.ant-modal:visible').waitFor();assert.match(await page.locator('.ant-modal:visible').getByRole('row').filter({hasText:'剃刀切开'}).innerText(),/C[\s\S]*Delete[\s\S]*波纹闭合/);const prior=writes.length;await press('Control+k');assert.equal(writes.length,prior);await page.locator('.ant-modal:visible .ant-modal-close').click();await page.locator('.ant-modal:visible').waitFor({state:'hidden'});const priorReleases=releasedPreviews.length;await page.evaluate(()=>location.hash='/resources');await page.locator('.sequence-timeline:visible').waitFor({state:'hidden'});assert.equal(await page.locator('.source-monitor video').count(),0);await press('Control+k');assert.equal(writes.length,prior);assert.deepEqual(errors,[]);assert.equal(approvals.length,0);assert(releasedPreviews.length>priorReleases&&releasedPreviews.some(url=>url.includes('session=review-session')),'leaving the editor must release the owned preview worker');ok('help and hidden editor suppress stale commands and release owned preview resources');
 await page.evaluate(()=>location.hash='/editing/import');await timeline.waitFor();releaseMovie();
 const video=page.getByLabel('节目播放器',{exact:true});await video.waitFor();
 await page.waitForFunction(()=>{const v=document.querySelector('video[aria-label="节目播放器"]');return v&&!v.classList.contains('is-loading')&&v.readyState>=2;});
 assert.equal(await page.getByRole('img',{name:'当前帧预览',exact:true}).count(),0);assert.deepEqual(errors,[]);ok('completed movie replaces the still after its target frame is ready');
 // Exercise code clips through the same editor/workbench fixture, without a model call.
 const workspace=await (await page.request.get(base+'/api/workspace')).json(),codeRuns=[],created=[];
 const prototype=structuredClone(view.sequence.tracks[0].clips[0]);
 const makeCode=(id,name,at)=>({...prototype,id,name,at,duration:60,link_id:undefined,source_out:{value:96000,scale:48000},code:{description:'原画面',generated_description:'原画面',revision:1,content_version:id+'-source',description_state:'ready'}});
 view.sequence.tracks.push({id:'code-track',kind:'code',name:'代码画面',height:96,locked:false,hidden:false,muted:false,solo:false,gain_db:0,clips:[makeCode('code-late','后画面',90),makeCode('code-first','先画面',0)]});
 workspace.document.sequences=[view.sequence];workspace.document.workspace.right_collapsed=false;
 await page.route('**/api/workspace',route=>route.fulfill({json:workspace}));
 await page.route('**/api/workspace/state',route=>route.fulfill({json:workspace}));
 await page.route('**/api/projects/review-project/visible-set',route=>route.fulfill({json:{confirmed:true,sha256:'code-visible',media:[]}}));
 await page.route(/\/api\/skill-runs(?:\?[^/]*)?$/,async route=>{
  if(route.request().method()==='POST'){const request=route.request().postDataJSON();created.push(request);const run={...request,id:'code-run-'+created.length,state:'queued',version:1,owner:'human:window'};codeRuns.push(run);return route.fulfill({json:run});}
  return route.fulfill({json:codeRuns});
 });
 await page.route('**/api/projects/review-project/sequences/seq-review/commands',async route=>{
  const request=route.request().postDataJSON();writes.push(request);
  for(const op of request.operations){const target=view.sequence.tracks.flatMap(t=>t.clips).find(c=>c.id===op.clip_ids?.[0]);
   if(op.type==='code.script'){target.code.description=op.description.split('\n').filter(line=>!line.startsWith('{')).join('\n').trim();target.code.revision++;}
   if(op.type==='code.job')target.code.run_id=op.code_run_id;
  }
  view.sequence.revision++;workspace.document.sequences=[view.sequence];await route.fulfill({json:view});
 });
 await page.reload();await page.locator('[data-clip-id="code-late"]').click();await page.locator('[data-clip-id="code-first"]').click({modifiers:['Control']});
 const first=page.getByRole('textbox',{name:'代码条脚本 1 先画面',exact:true}),second=page.getByRole('textbox',{name:'代码条脚本 2 后画面',exact:true});
 await first.waitFor();assert.match(await first.inputValue(),/时间:00:00:00:00/);assert.match(await second.inputValue(),/时间:00:00:03:00/);
 const codeRow=await page.locator('[data-track-id="code-track"]').boundingBox(),scrollArea=await page.locator('.timeline-scroll').boundingBox();
 assert(codeRow.y>=scrollArea.y&&codeRow.y+codeRow.height<=scrollArea.y+scrollArea.height,'opening the script dock must keep the selected code track in view');
 assert.equal(await page.locator('[data-clip-id="code-first"] .clip-thumbnails img').count(),1);assert.equal(await page.locator('[data-clip-id="code-first"] .clip-gain-band').count(),1);
 assert.notEqual(await page.locator('[data-clip-id="code-first"]').evaluate(n=>getComputedStyle(n).borderColor),await page.locator('[data-clip-id="code-late"]').evaluate(n=>getComputedStyle(n).borderColor));
 for(const [id,title] of [['code-first','1 · 先画面'],['code-late','2 · 后画面']]){
  const marker=page.locator(`[data-clip-id="${id}"] .clip-code-color`);
  assert.equal(await marker.count(),1,'code identity must stay visible inside the white selection outline');
  const stripe=await marker.boundingBox(),clipBox=await page.locator(`[data-clip-id="${id}"]`).boundingBox();
  assert(stripe.width>=3&&stripe.height>20&&stripe.x>=clipBox.x+2,'selection must not cover the color stripe');
  assert.equal(await marker.evaluate(n=>getComputedStyle(n).backgroundColor),await page.locator('.code-script-card').filter({hasText:title}).evaluate(n=>getComputedStyle(n).borderColor));
 }
 ok('code multiselection shows ordered, colored scripts with thumbnails and gain lines');
 await first.fill((await first.inputValue()).replace('原画面','绿色背景'));
 await page.waitForFunction(()=>document.body.innerText.includes('代码条脚本'));
 await page.waitForTimeout(1100);assert(writes.some(w=>w.operations.some(op=>op.type==='code.script'&&op.clip_ids[0]==='code-first')));
 assert.equal(created.length,0,'editing description must not start generation');
 await page.getByRole('button',{name:'重新生成画面',exact:true}).click();
 await page.waitForFunction(()=>document.body.innerText.includes('等待外部Agent处理'));
 assert.equal(created.length,1);assert.deepEqual(Object.keys(created[0].code_targets),['code-first']);assert.equal(created[0].code_action,'regenerate');
 assert.equal(second&&await second.inputValue(),'原画面\n{音量, 时间:00:00:03:00, 增益:0dB}');
 assert.deepEqual(errors,[]);ok('autosave preserves other scripts and generation captures only changed code clips');
 console.log(`unified editor browser smoke passed (${number} groups)`);
} catch(error) {console.error('Browser failure state',await page?.evaluate(()=>({body:document.body.innerText.slice(-4000),url:location.href})).catch(()=>undefined));throw error;} finally {if(page&&process.env.VAC_TEST_OUTPUT)await page.screenshot({path:join(process.env.VAC_TEST_OUTPUT,'code-track-ui.png')}).catch(()=>{});await browser.close();}
