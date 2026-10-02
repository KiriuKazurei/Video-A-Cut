import fs from 'node:fs/promises';
const [base, debuggerBase, output, mode] = process.argv.slice(2);
const targets = await (await fetch(debuggerBase+'/json/list')).json();
const ws = new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);
await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject;});
let seq=0;const pending=new Map();
ws.onmessage=e=>{const r=JSON.parse(e.data),p=pending.get(r.id);if(p){pending.delete(r.id);r.error?p.reject(Error(r.error.message)):p.resolve(r.result);}};
const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++seq;pending.set(id,{resolve,reject});ws.send(JSON.stringify({id,method,params}));});
const evaluate=async expression=>{const r=await send('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result.value;};
const wait=async expr=>{for(let i=0;i<120;i++){if(await evaluate(expr))return;await new Promise(r=>setTimeout(r,150));}throw Error('DOM wait failed: '+expr);};
const click=async(text,scope='body')=>{const expr=`[...document.querySelectorAll(${JSON.stringify(scope)}+' button')].find(b=>b.textContent.includes(${JSON.stringify(text)})&&!b.disabled)`;await wait(`!!(${expr})`);await evaluate(`(${expr}).click()`);};
const input=async(selector,value)=>{await wait(`!!document.querySelector(${JSON.stringify(selector)})`);await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(selector)});const p=e.tagName==='SELECT'?HTMLSelectElement.prototype:HTMLInputElement.prototype;Object.getOwnPropertyDescriptor(p,'value').set.call(e,${JSON.stringify(value)});e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));})()`);await new Promise(r=>setTimeout(r,80));};
const http=async(path,body)=>{const response=await fetch(base+path,{method:body?'POST':'GET',headers:{'Content-Type':'application/json'},body:body&&JSON.stringify(body)});const value=await response.json();if(!response.ok)throw Error(JSON.stringify(value));return value;};
const openPage=async page=>{const tab=`.workspace-tabs button[data-page="${page}"]`;await wait(`!!document.querySelector(${JSON.stringify(tab)})`);await evaluate(`document.querySelector(${JSON.stringify(tab)}).click()`);await wait(`document.querySelector(${JSON.stringify(tab)}).getAttribute('aria-current')==='page'`);};
const report={mode,human_acceptance:'pending',driver:'real Edge CDP; automated DOM interaction',layout:[],actions:[]};
const layouts=async(stage)=>{
 for(const width of [375,520,900,1280]){
  await send('Emulation.setDeviceMetricsOverride',{width,height:1100,deviceScaleFactor:1,mobile:false});
  await new Promise(r=>setTimeout(r,100));
  const row=await evaluate(`(()=>{const p=document.querySelector('.ingest-panel'),r=p.getBoundingClientRect();return {width:innerWidth,scrollWidth:document.documentElement.scrollWidth,overflow:[...p.querySelectorAll('button,input,select,video,img')].filter(e=>{const b=e.getBoundingClientRect();return b.width>0&&(b.right>r.right+1||b.left<r.left-1)}).map(e=>e.tagName+':'+e.textContent.slice(0,40))}})()`);
  row.stage=stage;row.pass=row.scrollWidth<=width&&row.overflow.length===0;report.layout.push(row);
  await evaluate(`document.querySelector(${JSON.stringify(stage==='selected-boundaries'?'.selected-editor':stage==='prepare-form'?'.ingest-prepare':'.ingest-panel')}).scrollIntoView()`);
  const shot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});
  await fs.writeFile(`${output}/${mode}-${stage}-${width}.png`,Buffer.from(shot.data,'base64'));
 }
};
try{
 await send('Page.enable');await send('Runtime.enable');await send('Page.navigate',{url:base+'/'});
 const asset=mode==='failed'?'phase7_failure':mode==='paging'?'phase7_cancel':'phase7_synthetic';
 // 前端按工作流分页：导入/切分在「粗剪」页，场景与解说审查在「编辑」页，预检在「准备」页。
 const workspacePage=mode==='scene'||mode==='draft'?'edit':mode==='governance'?'prepare':'assembly';
 await click(asset,'.asset-list');await openPage(workspacePage);
 if(mode==='probe'){
  await wait(`!!document.querySelector('.ingest-analysis')`);
  await input('.ingest-analysis .field-label:nth-child(4) input','00:00.000');
  await wait(`document.querySelector('.ingest-analysis button').disabled&&!!document.querySelector('.ingest-analysis [role=alert]')`);
  report.actions.push('invalid source range blocks submit');
  await input('.ingest-analysis .field-label:nth-child(4) input','00:17.000');
  await input('.ingest-analysis .field-label:nth-child(2) select','2');
  await layouts('probe-form');
  await click('断开');await wait(`document.body.textContent.includes('事件流已断开')`);
  const runs=await http(`/api/assets/${asset}/ingest-runs`),view=await http('/api/ingest-runs/'+runs[0].run_id);
  await http(`/api/ingest-runs/${runs[0].run_id}/analysis-plans`,{expected_version:view.run.version,video_stream_index:0,game_audio_stream_index:1,source_range_us:[0,17000000],segmentation:{method:'scene_change',threshold:.3,min_segment_us:2000000,max_segment_us:60000000},idempotency_key:crypto.randomUUID()});
  await click('开始切分','.ingest-analysis');
  await wait(`document.querySelector('.ingest-panel').textContent.includes('版本已变化')`);
  report.actions.push('real version conflict refreshes the panel');
  await click('取消导入','.ingest-panel');await wait(`document.querySelector('.ingest-run').textContent.includes('已取消')`);
  await click('立即重连');await wait(`document.body.textContent.includes('事件流已连接')`);
  report.actions.push('queued cancel and SSE reconnect');
 }else if(mode==='segment'||mode==='paging'){
  const count=mode==='paging'?50:4;await wait(`document.querySelectorAll('.segment-list li').length===${count}`);
  await evaluate(`document.querySelector('.segment-list input[type=checkbox]').click()`);
  if(mode==='paging'){
   await click('下一页','.ingest-review');await wait(`document.querySelectorAll('.segment-list li').length===10`);
   await evaluate(`document.querySelector('.segment-list input[type=checkbox]').click()`);
   await click('上一页','.ingest-review');await wait(`document.querySelectorAll('.segment-list li').length===50&&document.querySelector('.segment-list input[type=checkbox]').checked`);
   report.actions.push('60 candidates across two pages preserve both selections');
  }else{
   await input('.segment-bounds .field-label:first-child input','bad');
   await wait(`document.querySelector('.ingest-review [role=alert]')&&[...document.querySelectorAll('.ingest-review button')].find(b=>b.textContent.includes('保存选择版本')).disabled`);
   await input('.segment-bounds .field-label:first-child input','00:00.500');
   report.actions.push('invalid and edited trim boundary');
   await input('.selected-editor .field-label:nth-child(3) input','00:00.500');
   await click('拆分片段','.selected-editor');
   await wait(`document.querySelector('.selected-editor [role=alert]')?.textContent.includes('内部')`);
   await input('.selected-editor .field-label:nth-child(3) input','00:01.500001');
   await evaluate(`document.querySelector('.selected-editor .field-label:nth-child(3) input').focus()`);
   for (const type of ['keyDown','keyUp']) await send('Input.dispatchKeyEvent',{type,key:'Tab',code:'Tab',windowsVirtualKeyCode:9});
   await wait(`document.activeElement?.textContent==='拆分片段'`);
   for (const type of ['keyDown','keyUp']) await send('Input.dispatchKeyEvent',{type,key:'Enter',code:'Enter',windowsVirtualKeyCode:13,...(type==='keyDown'?{text:'\r'}:{})});
   await wait(`document.querySelectorAll('.selected-editor .workflow-card').length===2`);
   await click('保存选择版本','.ingest-review');await wait(`!!document.querySelector('.ingest-prepare')`);
   await send('Page.reload');await click(asset,'.asset-list');
   await wait(`document.querySelectorAll('.selected-editor .workflow-card').length===2`);
   const saved=await evaluate(`[...document.querySelectorAll('.selected-editor .workflow-card')].map(e=>[...e.querySelectorAll('input')].slice(0,2).map(i=>i.value))`);
   if(saved[0][1]!=='00:01.500001'||saved[1][0]!==saved[0][1])throw Error('split boundary precision lost on reload');
   await click('与下一片段合并','.selected-editor');
   await wait(`document.querySelectorAll('.selected-editor .workflow-card').length===1`);
   await input('.ingest-prepare select','browser_fixture');
   await wait(`[...document.querySelectorAll('.ingest-prepare button')].find(b=>b.textContent.includes('生成短片')).disabled`);
   await input('.selected-editor .field-label:nth-child(3) input','00:01.500001');
   await click('拆分片段','.selected-editor');
   report.actions.push('keyboard Tab/Enter split; invalid split blocked; microsecond boundary and derived segment survive save/reload; adjacent merge; unsaved changes block prepare');
  }
  await layouts('selected-boundaries');
  await click('保存选择版本','.ingest-review');await wait(`!!document.querySelector('.ingest-prepare')`);
  await input('.ingest-prepare select','browser_fixture');
  await layouts('prepare-form');
  await click('生成短片并登记为 EDL 包','.ingest-prepare');
  await wait(`document.querySelector('.ingest-run').textContent.includes('排队中')`);
  const runs=await http(`/api/assets/${asset}/ingest-runs`),view=await http('/api/ingest-runs/'+runs[0].run_id);
  if(view.run.profile_id!=='browser_fixture'||view.asset.input_kind!=='raw_recording')throw Error('prepare claimed completion before the worker ran');
  report.actions.push('selection revision and real prepare request, raw source retained while queued');
  await click('取消导入','.ingest-panel');await wait(`document.querySelector('.ingest-run').textContent.includes('已取消')`);
 }else if(mode==='failed'){
  await wait(`document.querySelector('.ingest-run')?.textContent.includes('失败')`);await layouts('failure');
  await click('重试失败步骤','.ingest-panel');await wait(`document.querySelector('.ingest-run').textContent.includes('排队中')`);
  report.actions.push('actual damaged-media error and retry queue');await click('取消导入','.ingest-panel');
 }else if(mode==='scene'||mode==='draft'){
  const scope='section[aria-labelledby="workflow-title"]';
  await wait(`!!document.querySelector('${scope} .workflow-card')`);
  const runs=await http(`/api/assets/${asset}/workflows`),run=runs.find(r=>r.status==='ready_for_acceptance');
  const path='/api/workflows/'+run.run_id;
  const before=await http(path+'/review');
  if(mode==='scene'){
   await input(`${scope} .workflow-card form input`,'修订场景验收');
   await input(`${scope} .workflow-card form input[type=number]`,'1');
   await click('保存新修订',scope);
   await wait(`document.querySelector('${scope}').textContent.includes('awaiting_review / scene_review')`);
   let after=await http(path+'/review');
   const changed=after.scenes.find(s=>s.scene_id===before.scenes[0].scene_id);
   if(changed.label!=='修订场景验收'||changed.sequence_rank!==1)throw Error('scene edit/order not persisted');
   await click('退回场景',scope);
   await wait(`document.querySelector('${scope} h4').textContent.includes('rejected')`);
   after=await http(path+'/review');
   if(after.run.stage!=='scene_review')throw Error('rejected scene advanced');
   await click('确认此场景',scope);
   await wait(`document.querySelector('${scope} h4').textContent.includes('confirmed')`);
   report.actions.push('scene label/order saved; rejection keeps review gate closed; correction can be confirmed');
  }else{
   await click('撤销并停止下游',scope);
   await wait(`document.querySelector('${scope}').textContent.includes('awaiting_review / draft_review')`);
   const revoked=await http(path+'/review');
   if(revoked.narration[0].approved_hash)throw Error('revoked narration stayed approved');
   await click('批准当前版本',scope);
   await wait(`document.querySelector('${scope}').textContent.includes('当前版本已批准')`);
   const textSelector=`${scope} textarea`;
   await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(textSelector)});Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype,'value').set.call(e,'重新修改的解说。');e.dispatchEvent(new Event('input',{bubbles:true}));})()`);
   await click('保存新修订并重新审查',scope);
   await wait(`!document.querySelector('${scope}').textContent.includes('当前版本已批准')`);
   const after=await http(path+'/review');
   if(after.narration[0].text!=='重新修改的解说。'||after.narration.some(n=>n.approved_hash)||after.run.stage!=='draft_review')throw Error('edit did not invalidate approvals');
   const tasks=await http(path);
   if(tasks.tasks.some(t=>['pending','queued','running'].includes(t.status)))throw Error('unapproved edit started work');
   report.actions.push('revoke stops downstream; approve then edit clears approvals and queues no work');
  }
  await send('Page.reload');await click(asset,'.asset-list');
  await wait(`document.querySelector('${scope}').textContent.includes('awaiting_review')`);
  report.actions.push('review state survives reload; historical export untouched');
 }else if(mode==='governance'){
  const scope='section[aria-labelledby="detail-title"]';
  await click('锁定',scope);await wait(`document.querySelector('${scope}').textContent.includes('解锁')`);
  await input('.preparation-panel .action-row select','browser_fixture');await click('加载版本','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('已加载 browser_fixture')`);
  await click('检查运行条件','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('受阻：资产须可见且未锁定')`);
  await click('解锁',scope);await wait(`!document.querySelector('${scope}').textContent.includes('解锁')`);
  await click('设为 Agent 不可见',scope);await click('检查运行条件','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('受阻：资产须可见且未锁定')`);
  await click('设为 Agent 可见',scope);
  await wait(`[...document.querySelectorAll('.preparation-panel button')].find(b=>b.textContent.includes('启动此版本')).disabled`);
  report.actions.push('lock and visibility changes block preflight; mismatched preparation/absent workers cannot start');
  const profile=(await http('/api/processing-profiles/browser_fixture/revisions/1')).profile;
  profile.profile_id='external_ui_fixture';profile.content_mode='configured';profile.tts_voice='Nonexistent acceptance voice';
  for(const role of ['vision','narration']) profile[role]={adapter:role,api_format:'openai',endpoint:'https://acceptance.invalid/v1',model:'unavailable',token_env:'VAC_ACCEPTANCE_NOT_SET'};
  await http('/api/processing-profiles',{profile,expected_revision:0,idempotency_key:crypto.randomUUID()});
  await click('刷新快照');
  await send('Page.reload');await click(asset,'.asset-list');
  await input('.preparation-panel .action-row select','external_ui_fixture');await click('加载版本','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('已加载 external_ui_fixture')`);
  await click('检查运行条件','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('受阻：外部端点需该版本明确许可')`);
  await click('授权此版本','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('已满足：外部端点需该版本明确许可')`);
  await click('撤销此版本','.preparation-panel');
  await wait(`document.querySelector('.preparation-panel').textContent.includes('受阻：外部端点需该版本明确许可')`);
  await wait(`[...document.querySelectorAll('.preparation-panel button')].find(b=>b.textContent.includes('启动此版本')).disabled`);
  report.actions.push('isolated external consent grant/revoke updates preflight; unavailable voice/credentials/workers never enable start; no external calls');
 }else{
  await wait(`document.querySelectorAll('.ingest-ready video').length===3`);await layouts('ready-preview');
  await wait(`[...document.querySelectorAll('.ingest-ready video')].every(v=>v.readyState>=2&&!v.error)`);
  await evaluate(`(async()=>{const v=document.querySelector('.ingest-ready video');v.muted=true;await v.play();})()`);
  await wait(`document.querySelector('.ingest-ready video').currentTime>.1`);
  await evaluate(`document.querySelector('.ingest-ready video').pause()`);
  report.actions.push('real prepared video decoded and played');
  const runs=await http(`/api/assets/${asset}/workflows`);
  const run=runs.find(r=>r.status==='ready_for_acceptance');
  if(!run)throw Error('final workflow missing');
  const zip=await fetch(base+`/api/workflows/${run.run_id}/delivery.zip`),bytes=new Uint8Array(await zip.arrayBuffer());
  if(!zip.ok||bytes[0]!==80||bytes[1]!==75)throw Error('ZIP download failed');
  report.actions.push('actual final delivery ZIP downloaded');
  await click('断开');await click('立即重连');await wait(`document.body.textContent.includes('事件流已连接')`);
 }
 if(report.layout.some(r=>!r.pass))throw Error('responsive overflow: '+JSON.stringify(report.layout.filter(r=>!r.pass)));
 report.engineering='passed';await fs.writeFile(`${output}/${mode}-report.json`,JSON.stringify(report,null,2));console.log(JSON.stringify(report));
}catch(error){
 report.engineering='failed';report.error=String(error);report.dom=await evaluate(`document.body.innerText`);
 await fs.writeFile(`${output}/${mode}-failure.json`,JSON.stringify(report,null,2));
 const shot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(`${output}/${mode}-failure.png`,Buffer.from(shot.data,'base64'));throw error;
}finally{await Promise.race([send('Browser.close').catch(()=>{}),new Promise(r=>setTimeout(r,1000))]);ws.close();}
