// Existing keyboard browser suite updated for the unified editor. API writes are
// recorded local fixtures; authoritative editing/rendering is covered in Go/native.
import assert from 'node:assert/strict';
const base=process.argv[2]??'http://127.0.0.1:8816';
const {chromium}=await import(process.env.PLAYWRIGHT_CORE??'playwright-core');
let page;
const browser=await chromium.launch({executablePath:process.env.CHROME,channel:process.env.CHROME?undefined:'msedge',args:['--no-sandbox']});
try {
 const context=await browser.newContext({viewport:{width:1600,height:1000}});
 await context.addInitScript(()=>localStorage.setItem('vac_tour_seen_v1','1'));
 page=await context.newPage();const errors=[],writes=[],approvals=[];page.on('pageerror',error=>errors.push(String(error)));
 let view,number=0;
 const ok=text=>console.log(`ok ${++number} ${text}`);
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
 await load();assert.equal(await page.locator('.timeline-row').count(),6);assert.equal(await page.locator('.workflow-panel:visible').count(),0);assert.equal(await page.locator('.timeline-ruler>.track-heading').innerText(),'00:00:01:15');await page.reload();await clip.waitFor();assert.equal(await page.locator('.timeline-ruler>.track-heading').innerText(),'00:00:01:15');ok('unified editor restores a saved nonzero playhead on load and reload with six tracks');
 assert.equal(await clip.evaluate(n=>getComputedStyle(n).backgroundColor),'rgb(62, 111, 176)');assert.match(await page.locator('[data-clip-id="clip-3"]').innerText(),/流 1/);ok('ordinary video and audio use theme blue and show actual stream identity');
 await clip.focus();await press('Enter');assert.equal(await clip.getAttribute('aria-pressed'),'true');assert.equal(await page.locator('[data-clip-id="clip-3"]').getAttribute('aria-pressed'),'true');ok('keyboard clip selection includes its linked sound');
 await press('c');assert.equal(await page.getByRole('button',{name:/剃刀 C/}).getAttribute('aria-pressed'),'true');assert.equal(approvals.length,0);ok('C selects the razor without legacy approval');
 await press('v');assert.equal(await page.getByRole('button',{name:/选择 V/}).getAttribute('aria-pressed'),'true');ok('V restores selection tool');
 const split=await command('Control+k');assert.equal(split.session,'review-session');assert.equal(split.operations[0].type,'clip.split');assert.deepEqual(split.operations[0].clip_ids,['clip-0','clip-3']);assert(split.expected_revision>=1&&split.idempotency_key);ok('Ctrl+K dispatches linked stable IDs with CAS and idempotency');
 const search=page.getByRole('searchbox',{name:'搜索当前工程资源'});await search.focus();const count=writes.length;await page.keyboard.type('mcio');assert.equal(writes.length,count);assert.equal(await page.locator('.ant-modal:visible').count(),0);await search.fill('');ok('typing letters in a resource input never edits tracks');
 await search.focus();await page.locator('.timeline-scale').click({position:{x:60,y:8}});assert.equal(await timeline.evaluate(n=>document.activeElement===n),true);await press('m');assert.equal(await page.getByRole('textbox',{name:'标记备注',exact:true}).count(),1);await press('Control+k');assert.equal(writes.length,count);await cancel();ok('M offers one point and visible modal guards background editing');
 await timeline.focus();await press('i');assert.match(await page.getByRole('textbox',{name:'标记备注',exact:true}).inputValue(),/^入点/);await cancel();await timeline.focus();await press('o');assert.match(await page.getByRole('textbox',{name:'标记备注',exact:true}).inputValue(),/^出点/);await cancel();ok('I and O open separately editable in and out markers');
 await timeline.focus();const before=await page.locator('.timeline-ruler>.track-heading').innerText();await press('ArrowRight');assert.notEqual(await page.locator('.timeline-ruler>.track-heading').innerText(),before);await press('ArrowLeft');assert.equal(await page.locator('.timeline-ruler>.track-heading').innerText(),before);ok('arrows step one frame and return to the same timecode');
 await clip.click({button:'right'});await page.getByRole('menuitem',{name:/在播放头分割/}).waitFor();const n=writes.length;await page.getByRole('menuitem',{name:/在播放头分割/}).click();await page.waitForTimeout(250);assert.equal(writes.length,n+1);assert.equal(writes.at(-1).operations[0].type,'clip.split');ok('context menu uses the same split command');
 await clip.focus();await press('Control+c');const pasted=await command('Control+v');assert.equal(pasted.operations.length,2);assert(pasted.operations.every(op=>op.type==='clip.paste'));ok('copy and paste dispatch video and sound as one command');
 await clip.focus();assert.equal((await command('Control+z')).operations[0].type,'history.undo');assert.equal((await command('Control+Shift+z')).operations[0].type,'history.redo');ok('undo and redo stay in the focused sequence');
 await timeline.focus();await press('?');await page.locator('.ant-modal:visible').waitFor();assert.match(await page.locator('.ant-modal:visible').innerText(),/时间线剃刀工具/);const prior=writes.length;await press('Control+k');assert.equal(writes.length,prior);await page.locator('.ant-modal:visible .ant-modal-close').click();await page.locator('.ant-modal:visible').waitFor({state:'hidden'});await page.evaluate(()=>location.hash='/resources');await page.locator('.sequence-timeline:visible').waitFor({state:'hidden'});assert.equal(await page.locator('.source-monitor video').count(),0);await press('Control+k');assert.equal(writes.length,prior);assert.deepEqual(errors,[]);assert.equal(approvals.length,0);ok('help and hidden editor suppress stale commands and hidden source media without page errors');
 console.log(`unified editor browser smoke passed (${number} groups)`);
} catch(error) {console.error('Browser failure state',await page?.evaluate(()=>({body:document.body.innerText.slice(-4000),url:location.href})).catch(()=>undefined));throw error;} finally {await browser.close();}
