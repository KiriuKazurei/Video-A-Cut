// Managed browser acceptance: real production UI, local HTTP/SSE fixtures, no model credentials.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, mkdir, stat } from 'node:fs/promises';
import { resolve, join, extname } from 'node:path';
const { chromium } = await import(process.env.PLAYWRIGHT_CORE || 'playwright-core');
assert.ok(process.env.VAC_TEST_OUTPUT && process.env.VAC_DIST, 'run via TestWorkspace.ps1');
const out = join(process.env.VAC_TEST_OUTPUT, 'script-workbench-20261018', 'browser');
await mkdir(out, { recursive: true });
const dist = resolve(process.env.VAC_DIST);
const now = new Date().toISOString();
const asset = { asset_id:'clip', status:'ingested', agent_visible:true, human_approved:false, locked:false, allowed_agents:['recognizer','narrator','exporter'], artifacts:{}, created_at:now, updated_at:now };
let visibleSHA = 'visible-1', revision = 1, version = 0, generated = 0, saved = 0, slow = false;
const media = [{ id:'m1', asset_id:'clip', name:'抽卡演示.mp4', in:0, out:3 }, { id:'m2', asset_id:'clip', name:'结果.mp4', in:0, out:3 }];
let script = { script_id:'s1', asset_id:'clip', status:'draft', visible_set_sha:visibleSHA, title:'', body:'', created_at:now, updated_at:now };
let plan = { script, version, segments:[], sources:[], questions:[], tts_enabled:false, subtitles_enabled:false, allow_web_research:true, rejected:false };
const revisions = [], events = new Set(), calls = [];
const parse = body => ({ version:1, diagnostics: /\{未知效果\}|\{黑屏$/u.test(body) ? [{ paragraph:0, column:1, message:'未知或未闭合语法' }] : [], paragraphs: body.trim() ? body.split(/\n\s*\n/u).map((source,index) => ({ index, source, instruction:source.replace(/\{[^}]*\}/gu,'').trim(), video:/\{@([^}]+)\}/u.exec(source)?.[1], effect: /\{(黑屏|白屏|交叉溶解)\}/u.exec(source)?.[1], subtitle: /\{"/u.test(source) ? { text:'重点说明', size_px:32, font:'Microsoft YaHei' } : undefined })) : [] });
const view = () => ({ ...plan, script, version, document:parse(script.body) });
const push = name => { for (const res of events) res.write(`event: ${name}\ndata: {}\n\n`); };
const snapshot = action => { revisions.unshift({ version, action, actor:'human:webui', snapshot:JSON.stringify(view()), created_at:now }); };
const json = (res,status,body) => { res.writeHead(status,{'Content-Type':'application/json'}); res.end(JSON.stringify(body)); };
const error = (res,status,message) => json(res,status,{error:{code:'conflict',message}});
const server = createServer(async (req,res) => {
  const url = new URL(req.url,'http://localhost'), p = url.pathname;
  try {
    if (!p.startsWith('/api/')) {
      let file = resolve(dist,'.'+p); if(!file.startsWith(dist)) return error(res,403,'path');
      try { if((await stat(file)).isDirectory()) file=join(file,'index.html'); } catch { file=join(dist,'index.html'); }
      res.writeHead(200,{'Content-Type':{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[extname(file)]||'application/octet-stream'});res.end(await readFile(file));return;
    }
    if(p==='/api/events'){res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-cache'});res.write(': ok\n\n');events.add(res);req.on('close',()=>events.delete(res));return;}
    if(req.method==='GET') {
      if(p==='/api/assets') return json(res,200,[asset]);
      if(p==='/api/assets/clip') return json(res,200,asset);
      if(p==='/api/visible-set') return json(res,200,{sha256:visibleSHA,confirmed:true,script_finalize:true,asset_ids:['clip'],assets:[{...asset,ingested:true}]});
      if(p==='/api/script-workbench/media') return json(res,200,media);
      if(p==='/api/script-workbench/fonts') return json(res,200,['Microsoft YaHei','Arial']);
      if(p==='/api/assets/clip/scripts') return json(res,200,[script]);
      if(p==='/api/scripts/s1/workbench') return json(res,200,view());
      if(p==='/api/scripts/s1/revisions') return json(res,200,revisions);
      if(p==='/api/processing-profiles') return json(res,200,[{profile_id:'p',revision,name:'本次生成预设',content_mode:'configured',vision:{endpoint:'https://example.invalid',api_format:'openai'},narration:{endpoint:'https://example.invalid',api_format:'openai'}}]);
      if(p==='/api/ingest-roots') return json(res,200,{configured:false,roots:[],policy:{},ingesters:[]});
      return json(res,200,[]);
    }
    let raw='';for await(const c of req)raw+=c;const b=raw?JSON.parse(raw):{};calls.push({p,body:b});
    if(p==='/api/script-workbench/parse') return json(res,200,parse(b.body));
    if(p.includes('/consent')) return json(res,200,{});
    if(p.startsWith('/api/scripts/s1/')){
      if(b.expected_version!==version) return error(res,409,'版本冲突，请载入最新修订');
      if(p.endsWith('/generate')) {
        generated++;
        if(slow) await new Promise(r=>setTimeout(r,3000));
        if(res.destroyed)return;
        plan.segments=[{index:0,narration:'抽卡会随机获得角色或道具，具体概率以游戏公布规则为准。',media_id:'m1',start:0,end:3,source_urls:[]}];
        plan.questions=[];version++;snapshot('generate');return json(res,200,view());
      }
      if(p.endsWith('/finalize')){script.status='finalized';version++;snapshot('finalize');return json(res,200,view());}
      if(p.endsWith('/export'))return json(res,200,{version,visible_set_sha:visibleSHA,created_at:now});
      if(p.endsWith('/reject')){plan.segments=[];plan.rejected=true;script.status='needs_refinalize';version++;snapshot('reject');return json(res,200,view());}
      if(req.method==='PUT') {
        saved++;script={...script,title:b.title,body:b.body,status:script.status==='finalized'?'needs_refinalize':script.status};
        plan={...plan,segments:b.segments,tts_enabled:b.tts_enabled,subtitles_enabled:b.subtitles_enabled,allow_web_research:b.allow_web_research,rejected:false};version++;snapshot('save');return json(res,200,view());
      }
    }
    return json(res,200,{});
  }catch(e){error(res,500,String(e));}
});
await new Promise(r=>server.listen(0,'127.0.0.1',r));
const browser=await chromium.launch({executablePath:process.env.CHROME,headless:true});
const errors=[];
try {
  const context=await browser.newContext({viewport:{width:1440,height:1000}});
  await context.addInitScript(()=>localStorage.setItem('vac_tour_seen_v1','1'));
  const page=await context.newPage();page.on('pageerror',e=>errors.push(String(e)));
  await page.goto(`http://127.0.0.1:${server.address().port}/#/editing/content`);
  const workbench=page.locator('.page:not([hidden]) [data-testid="script-workbench"]');
  await workbench.waitFor();
  const body=workbench.getByRole('textbox',{name:'脚本指令',exact:true});
  const save=workbench.getByRole('button',{name:'保存草稿',exact:true});
  const generate=workbench.getByRole('button',{name:'理解脚本并生成文稿',exact:true});
  const consent=workbench.locator('.script-model-consent input');
  await body.fill('{未知效果}介绍机制');await page.waitForTimeout(400);assert.equal(await save.isDisabled(),true);
  await workbench.getByText('请修正特殊语法',{exact:true}).waitFor();
  const source='{@抽卡演示.mp4}{"重点说明", 字号:32px, 字体:"Microsoft YaHei",}{黑屏}介绍抽卡机制';
  await body.fill(source);await page.waitForTimeout(350);
  assert.equal(await workbench.locator('.script-highlight mark').count(),3);
  await save.click();await page.waitForFunction(()=>document.querySelector('.script-workbench')?.textContent.includes('修订 1'));
  assert.equal(saved,1);assert.equal(script.body,source);assert.equal(plan.segments[0].narration,'');
  assert.equal(await workbench.getByRole('button',{name:'人工定稿',exact:true}).isDisabled(),true);
  console.log('ok 1 syntax highlighting, invalid syntax blocking and incomplete draft save');
  await consent.check();revision=2;push('profile.changed');
  await page.waitForTimeout(400);assert.equal(await consent.isChecked(),false);
  revision=1;push('profile.changed');await page.waitForTimeout(400);assert.equal(await consent.isChecked(),false);
  await consent.check();visibleSHA='visible-2';push('asset_updated');await page.waitForTimeout(400);assert.equal(await consent.isChecked(),false);
  visibleSHA='visible-1';push('asset_updated');await page.waitForTimeout(400);assert.equal(await consent.isChecked(),false);
  await consent.check();await generate.dblclick();
  const narration=workbench.locator('#script-narration-0');
  await page.waitForFunction(()=>document.querySelector('#script-narration-0')?.value.includes('随机获得'));
  assert.equal(generated,1);assert.equal(await consent.isChecked(),false);
  assert.ok(calls.some(c=>c.p.endsWith('/generate')&&c.body.expected_version===1&&c.body.profile_revision===1));
  console.log('ok 2 profile-version consent reset, one request per double click and generated prose');
  await narration.fill('手工修订：抽卡会消耗资源并随机获得奖励。');await save.click();
  await workbench.getByRole('button',{name:'人工定稿',exact:true}).click();
  const exportButton=workbench.getByRole('button',{name:'生成画面与 Premiere 交付包',exact:true});
  await exportButton.click();await workbench.getByRole('link',{name:'下载 Premiere 交付包',exact:true}).waitFor();
  assert.equal(script.status,'finalized');
  await body.fill(source+'，请补充说明');await page.waitForTimeout(350);
  assert.equal(await workbench.getByRole('link',{name:'下载 Premiere 交付包',exact:true}).count(),0);
  await save.click();await page.waitForTimeout(150);assert.equal(script.status,'needs_refinalize');
  console.log('ok 3 manual editing/finalization/export, edit invalidates delivery');
  await narration.fill('本地尚未保存的稿件');version++;plan.segments[0].narration='其他会话的新稿';snapshot('save');push('script.changed');
  await workbench.getByText('其他操作已保存了新修订，当前编辑不会覆盖它',{exact:true}).waitFor();
  await save.click();await workbench.getByText(/版本冲突，请载入最新修订/).waitFor();
  assert.equal(plan.segments[0].narration,'其他会话的新稿');
  await workbench.getByRole('button',{name:'载入最新修订',exact:true}).click();
  await page.getByRole('button',{name:'确定',exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('#script-narration-0')?.value==='其他会话的新稿');
  console.log('ok 4 concurrent revision does not overwrite newer saved work');
  await body.scrollIntoViewIfNeeded();await workbench.screenshot({path:join(out,'workbench.png')});
  const beforeCancel=version;slow=true;await consent.check();await generate.click();
  for(let n=0;n<80&&generated<2;n++)await page.waitForTimeout(25);
  assert.equal(generated,2);
  await workbench.getByRole('button',{name:'停止生成',exact:true}).click();await page.waitForTimeout(3300);
  await workbench.getByText('已停止生成，已保存的草稿保留。',{exact:true}).waitFor();
  assert.equal(version,beforeCancel,'a cancelled response did not save a new revision');
  console.log('ok 5 cancelled generation discards a late model response');
  await context.setOffline(true);await generate.click();await page.waitForTimeout(500);
  await context.setOffline(false);await page.waitForTimeout(500);
  assert.equal(generated,2,'offline generation was not silently queued for reconnect');
  console.log('ok 6 offline generation does not resume a queued provider call');
  await body.fill(Array.from({length:40},(_,i)=>`${i} {黑屏}自然语言与较长文字${'测试'.repeat(60)}`).join('\n'));
  const alignment=await body.evaluate(el=>{
    el.scrollTop=el.scrollHeight;el.dispatchEvent(new Event('scroll',{bubbles:true}));
    const pre=el.closest('.script-code-field').querySelector('pre');
    return {width:el.clientWidth,highlightWidth:pre.clientWidth,scroll:el.scrollTop,highlightScroll:pre.scrollTop,font:getComputedStyle(el).font,line:getComputedStyle(el).lineHeight,highlightFont:getComputedStyle(pre).font,highlightLine:getComputedStyle(pre).lineHeight};
  });
  assert.ok(Math.abs(alignment.width-alignment.highlightWidth)<=2,JSON.stringify(alignment));
  assert.ok(Math.abs(alignment.scroll-alignment.highlightScroll)<=2,JSON.stringify(alignment));
  assert.equal(alignment.font,alignment.highlightFont);assert.equal(alignment.line,alignment.highlightLine);
  console.log('ok 7 wrapped/scrolling syntax overlay stays aligned with the editor');
  assert.deepEqual(errors,[]);console.log('PASS: script workbench browser acceptance (7 groups)');
} finally {
  await browser.close();for(const res of events)res.end();await new Promise(r=>server.close(r));
}
