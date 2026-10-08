// Real Go REST/MCP + Worker + FFmpeg, with actual React controls and HTML video decoding.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import { spawn } from 'node:child_process';
const [base,runId,assetA,assetB]=process.argv.slice(2);
if (!base || !['127.0.0.1','localhost'].includes(new URL(base).hostname) || !runId) throw Error('loopback fixture required');
const out=path.resolve(process.env.VAC_TEST_OUTPUT || '.run-data/ass-acceptance/browser');await fs.mkdir(out,{recursive:true});
await fs.writeFile(path.join(out,'progress.log'),'');
const checks=[],errors=[];let primary={};
const check=async(name,evidence={})=>{checks.push({name,pass:true,...evidence});await fs.appendFile(path.join(out,'progress.log'),JSON.stringify(checks.at(-1))+'\n');};
const sleep=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const sock=net.createServer();await new Promise(r=>sock.listen(0,'127.0.0.1',r));const port=sock.address().port;await new Promise(r=>sock.close(r));
const log=await fs.open(path.join(out,'edge.log'),'w');
const browser=spawn(process.env.EDGE_PATH || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',[
 '--headless=new','--disable-gpu','--disable-extensions','--no-first-run','--no-default-browser-check','--disable-background-networking','--disk-cache-size=1048576','--media-cache-size=1048576',
 `--remote-debugging-port=${port}`,`--user-data-dir=${path.join(out,'edge-profile')}`,'about:blank'
],{windowsHide:true,stdio:['ignore','ignore',log.fd]});
let ws,seq=0,heldResponse;const pending=new Map();
const send=(method,params={})=>new Promise((resolve,reject)=>{
 const id=++seq,timer=setTimeout(()=>{pending.delete(id);reject(Error(`CDP timeout ${method}`));},20000);
 pending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value)},reject:error=>{clearTimeout(timer);reject(error)}});ws.send(JSON.stringify({id,method,params}));
});
const ev=async expression=>{const result=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(result.exceptionDetails)throw Error(JSON.stringify(result.exceptionDetails));return result.result.value;};
const visible=selector=>`[...document.querySelectorAll(${JSON.stringify(selector)})].find(e=>e.checkVisibility())`;
const wait=async(expression,timeout=15000)=>{for(let time=0;time<timeout;time+=100){if(await ev(`Boolean(${expression})`))return;await sleep(100)}throw Error(`wait failed: ${expression}`);};
const click=async selector=>{await wait(visible(selector));await ev(`(${visible(selector)}).click()`);await sleep(100);};
const input=async(selector,value)=>{await wait(visible(selector));await ev(`(()=>{const root=${visible(selector)},e=root.tagName==='INPUT'?root:root.querySelector('input');Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(e,${JSON.stringify(String(value))});e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));e.blur()})()`);await sleep(80);};
const value=selector=>ev(`(()=>{const e=${visible(selector)};return (e.tagName==='INPUT'?e:e.querySelector('input')).value})()`);
const close=()=>click('.ant-modal-close');
const get=async(url,init)=>{const response=await fetch(base+url,init);const body=await response.json();if(!response.ok)throw Error(`${response.status} ${JSON.stringify(body)}`);return body;};
const post=(url,body)=>get(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
const draft=()=>get(`/api/workflows/${runId}/subtitle-style`);
const editor=async(asset=assetA)=>{
 await wait(visible(`[data-asset-id="${asset}"]`));await click(`[data-asset-id="${asset}"]`);
 await wait(visible('[data-testid="ass-editor-open"]:not([disabled])'));await click('[data-testid="ass-editor-open"]');
 await wait(visible('[data-testid="ass-editor"]'));await wait(visible('[data-testid="ass-generate"]:not([disabled])'));
};
const cue=index=>click(`.ass-cue:nth-child(${index})`);
const shot=async name=>{const result=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(path.join(out,name),Buffer.from(result.data,'base64'));};
try {
 let targets;
 for(let i=0;i<100;i++){try{targets=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json();if(targets.some(t=>t.type==='page'))break}catch{}await sleep(100)}
 assert(targets?.some(t=>t.type==='page'),'Edge did not start');
 ws=new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);await new Promise((r,j)=>{ws.onopen=r;ws.onerror=j});
 ws.onmessage=e=>{const msg=JSON.parse(e.data);if(msg.method==='Runtime.exceptionThrown')errors.push(msg.params);
  if(msg.method==='Fetch.requestPaused'){if(!heldResponse)heldResponse=msg.params;else void send('Fetch.continueRequest',{requestId:msg.params.requestId});}
  const job=pending.get(msg.id);if(job){pending.delete(msg.id);msg.error?job.reject(Error(msg.error.message)):job.resolve(msg.result)}};
 await send('Runtime.enable');await send('Page.enable');await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
 await send('Page.navigate',{url:base+'/#/export/delivery'});await editor();
 assert.equal(await ev(`getComputedStyle(${visible('[data-testid="ass-editor"]')}).gridTemplateColumns.split(' ').length`),2);
 await check('real API editor entry and desktop layout');
 await input('[data-testid="ass-font-size"]',40);await close();await send('Page.reload');await editor();
 assert.equal(await value('[data-testid="ass-font-size"]'),'40');await check('unsaved browser draft survives refresh');
 const original=await draft();const external=structuredClone(original.document);external.style.font_size=37;
 await get(`/api/workflows/${runId}/subtitle-style`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({revision_id:original.revision_id,expected_draft_version:original.draft_version,document:external})});
 await click('[data-testid="ass-save"]');await wait(`document.body.innerText.includes('conflict') || document.body.innerText.includes('冲突')`);
 assert.equal(await value('[data-testid="ass-font-size"]'),'40');assert.equal((await draft()).document.style.font_size,37);
 await check('409 conflict preserves local input');
 await wait(`document.body.innerText.includes('服务端已是 1')`);await click('[data-testid="ass-reload-server"]');
 await wait(`(()=>{const e=${visible('[data-testid="ass-font-size"]')};return (e?.tagName==='INPUT'?e:e?.querySelector('input'))?.value==='37'})()`);
 await input('[data-testid="ass-font-size"]',64);
 assert.equal(await ev(`(${visible('[data-testid="ass-font-size-slider"]')}).querySelector('[role="slider"]').getAttribute('aria-valuenow')`),'64');
 await check('font size number and slider agree');
 const schematicGeometry=await ev(`(()=>{const stage=document.querySelector('.ass-stage'),area=document.querySelector('.ass-caption-placement'),caption=document.querySelector('.ass-caption');return {width:stage.clientWidth,height:stage.clientHeight,top:parseFloat(getComputedStyle(area).top),font:parseFloat(getComputedStyle(caption).fontSize)}})()`);
 assert(Math.abs(schematicGeometry.top/schematicGeometry.height-36/720)<0.005);
 assert(Math.abs(schematicGeometry.font/schematicGeometry.width-64/1280)<0.001);
 await check('schematic margins and font scale against the complete ASS canvas',{schematicGeometry});
 await input('[data-testid="ass-primary-color-hex"]','#FF3020');await input('[data-testid="ass-outline-color-hex"]','#2060FF');
 await input('[data-testid="ass-outline"]',3);await input('[data-testid="ass-shadow"]',0);
 await input('[data-testid="ass-fade-in"]',250);await input('[data-testid="ass-fade-out"]',250);
 await ev(`[...document.querySelectorAll('.ass-checks label')].forEach(e=>{if(!e.querySelector('input').checked)e.click()})`);
 await cue(1);await input('[data-testid="ass-start-ms"]',1200);
 assert.equal(await value('[data-testid="ass-end-ms"]'),'3200');assert.equal(await value('[data-testid="ass-duration-ms"]'),'2000');
 await input('[data-testid="ass-duration-ms"]',2200);assert.equal(await value('[data-testid="ass-end-ms"]'),'3400');
 await input('[data-testid="ass-duration-ms"]',2000);
 await check('start preserves duration; duration updates end');
 await click('[data-testid="ass-play-schematic"]');await sleep(100);
 assert((await ev(`document.querySelector('.ass-caption').getAnimations().length`))>0);
 await check('schematic fade playback runs a real animation');
 await cue(2);await click('[data-testid="ass-scope-cues"]');
 await input('[data-testid="ass-font-size"]',48);await input('[data-testid="ass-primary-color-hex"]','#2070FF');
 await input('[data-testid="ass-outline-color-hex"]','#FFFF00');await click('[data-testid="ass-align-8"]');
 await wait(visible('[data-testid="ass-override-font_size"]'));
 await click('[data-testid="ass-restore"]');assert.equal(await value('[data-testid="ass-font-size"]'),'64');
 await click('[data-testid="ass-undo"]');assert.equal(await value('[data-testid="ass-font-size"]'),'48');
 await click('[data-testid="ass-apply-all"]');assert.equal(await value('[data-testid="ass-font-size"]'),'64');
 await cue(1);assert.equal(await value('[data-testid="ass-start-ms"]'),'1200');
 await click('[data-testid="ass-undo"]');await cue(2);assert.equal(await value('[data-testid="ass-font-size"]'),'48');
 await check('restore and apply global clear styles, preserve timing, and support undo');
 await input('[data-testid="ass-start-ms"]',2900);await wait(visible('[data-testid="ass-overlap"]'));
 assert(!(await ev(`(${visible('[data-testid="ass-save"]')}).disabled`)));await input('[data-testid="ass-start-ms"]',4000);
 await check('overlapping cues warn without preventing save');
 await click('[data-testid="ass-save"]');await wait(`document.body.innerText.includes('草稿已保存')`);
 const saved=await draft();assert.equal(saved.document.style.font_size,64);assert.equal(saved.document.overrides.find(o=>o.cue_id===2).style.alignment,8);
 assert.equal(saved.document.overrides.find(o=>o.cue_id===1).end_ms,3200);
 await check('real SQLite save includes global style, per-cue overrides and timing');
 await click('[data-testid="ass-generate"]');await wait(`${visible('[data-testid="ass-job"]')}?.innerText.includes('succeeded')`,45000);
 const variants=await get(`/api/workflows/${runId}/delivery-variants`);
 const ass=variants.variants.find(v=>v.spec_id==='ass-basic-v2'&&v.status==='succeeded'&&v.style_sha256===saved.style_sha256);assert(ass);
 const assResponse=await fetch(`${base}/api/delivery-variants/${ass.variant_id}/files/ass`);assert.equal(assResponse.status,200);assert.match(assResponse.headers.get('content-disposition'),/attachment/);
 const assText=await assResponse.text();assert(assText.includes('0:00:01.20,0:00:03.20'));assert(assText.includes('\\fad(250,250)'));assert(assText.includes('&H002030FF'));
 const sourceResponse=await fetch(`${base}/api/delivery-variants/${ass.variant_id}/files/srt`);assert((await sourceResponse.text()).includes('00:00:01,000 --> 00:00:03,000'));
 const zip=await fetch(`${base}/api/delivery-variants/${ass.variant_id}/delivery.zip`);assert.equal(zip.status,200);assert((await zip.arrayBuffer()).byteLength>100);
 await check('real Worker v2 ASS plus original SRT and package downloads',{variant_id:ass.variant_id});
 await click('[data-testid="mp4-generate"]');await wait(visible('[data-testid="ass-editor-player"]'),60000);
 const previews=await get(`/api/workflows/${runId}/previews`);
 const mp4=previews.previews.find(p=>p.status==='succeeded'&&p.subtitle_variant_id===ass.variant_id&&p.style_sha256===saved.style_sha256);assert(mp4);
 primary={variant_id:ass.variant_id,preview_id:mp4.preview_id,style_sha256:saved.style_sha256};
 assert.equal((await get(`/api/workflows/${runId}/delivery-variants`)).variants.filter(v=>v.spec_id==='ass-basic-v2').length,1,'matching ASS must be reused');
 await wait(`${visible('[data-testid="ass-editor-player"]')}?.readyState>=2`);
 await ev(`(async()=>{const v=${visible('[data-testid="ass-editor-player"]')};window.acceptancePlayer=v;v.muted=true;await v.play()})()`);
 await wait(`window.acceptancePlayer.currentTime>0.1`);
 const playback=await ev(`({time:window.acceptancePlayer.currentTime,duration:window.acceptancePlayer.duration,width:window.acceptancePlayer.videoWidth,height:window.acceptancePlayer.videoHeight})`);
 assert.equal(playback.width,640);assert.equal(playback.height,360);assert(playback.duration>=7.9);
 await ev('window.acceptancePlayer.pause();window.acceptancePlayer.currentTime=2');await wait('!window.acceptancePlayer.seeking&&Math.abs(window.acceptancePlayer.currentTime-2)<0.05');
 await ev('window.acceptancePlayer.scrollIntoView({block:"center"})');await shot('desktop-styled-playback.png');
 await check('exact generated ASS burns to a real decoded, playable and seekable MP4',{playback,...primary});
 const range=await fetch(`${base}/api/previews/${mp4.preview_id}/files/mp4`,{headers:{Range:'bytes=0-31'}});assert.equal(range.status,206);await range.arrayBuffer();
 const download=await fetch(`${base}/api/previews/${mp4.preview_id}/files/mp4?download=1`);assert.match(download.headers.get('content-disposition'),/attachment/);await download.arrayBuffer();
 await check('MP4 Range 206 and attachment download');
 await ev(`[...document.querySelectorAll('.ass-editor-modal button')].find(e=>e.innerText==='在监视器播放').click()`);await close();
 await wait(`${visible('video.monitor-media')}?.readyState>=2`);
 assert.equal(await ev(`(${visible('video.monitor-media')}).getAttribute('src')`),`/api/previews/${mp4.preview_id}/files/mp4`);
 await ev(`(async()=>{const v=${visible('video.monitor-media')};window.acceptanceMonitor=v;v.muted=true;await v.play()})()`);
 await wait('window.acceptanceMonitor.currentTime>0.1');await ev('window.acceptanceMonitor.pause()');await editor(assetA);
 await check('generated style MP4 opens and plays in the existing Monitor');
 // Exercise the real cancellation API and a fresh explicit retry after cancellation.
 let list=await get(`/api/workflows/${runId}/previews`);
 const cancelled=await post(`/api/workflows/${runId}/previews`,{revision_id:list.revision_id,expected_version:list.expected_version,preset_id:'mp4-720p-v1',request_id:crypto.randomUUID(),subtitle_variant_id:ass.variant_id});
 await post(`/api/previews/${cancelled.preview_id}/cancel`,{});
 assert.equal((await get(`/api/previews/${cancelled.preview_id}`)).status,'cancelled');
 await click('[data-testid="mp4-retry"]:not([disabled])');
 await wait(visible('[data-testid="mp4-cancel"]'),45000);await click('[data-testid="mp4-cancel"]');
 for(let i=0;i<100;i++){list=await get(`/api/workflows/${runId}/previews`);if(list.previews.some(p=>p.status==='cancelled'&&p.preview_id!==cancelled.preview_id))break;await sleep(100)}
 assert(list.previews.some(p=>p.status==='cancelled'&&p.preview_id!==cancelled.preview_id),'UI cancellation did not cancel the actual MP4 task');
 await check('UI MP4 cancellation cancels the real task');
 await click('[data-testid="mp4-retry"]:not([disabled])');
 await wait(`(()=>{const v=${visible('[data-testid="ass-editor-player"]')};return v?.readyState>=2&&v.getAttribute('src')!==${JSON.stringify(`/api/previews/${mp4.preview_id}/files/mp4`)}})()`,60000);
 list=await get(`/api/workflows/${runId}/previews`);assert(list.previews.some(p=>p.status==='succeeded'&&p.preview_id!==mp4.preview_id&&p.style_sha256===saved.style_sha256));
 await check('real MP4 cancellation and UI explicit retry complete');
 await click('[data-testid="ass-scope-global"]');await input('[data-testid="ass-font-size"]',80);
 await wait(visible('[data-testid="ass-old-style"]'));await check('unsaved editing marks existing products as old style');
 await close();assert(await ev('window.acceptancePlayer.paused'));
 await editor(assetB);assert.equal(await value('[data-testid="ass-font-size"]'),'36');await close();
 await editor(assetA);assert.equal(await value('[data-testid="ass-font-size"]'),'80');
 await check('asset A -> B -> A isolates drafts and pauses closed playback');
 // Hold an actual Go response for A, switch to B, then deliver that late A response.
 await send('Fetch.enable',{patterns:[{urlPattern:base+`/api/workflows/${runId}/subtitle-style`,requestStage:'Response'}]});
 for(let i=0;!heldResponse&&i<100;i++)await sleep(100);assert(heldResponse,'A draft response was not intercepted');
 await close();await editor(assetB);await send('Fetch.continueRequest',{requestId:heldResponse.requestId});await send('Fetch.disable');
 await sleep(250);assert.equal(await value('[data-testid="ass-font-size"]'),'36');
 await close();await editor(assetA);assert.equal(await value('[data-testid="ass-font-size"]'),'80');
 await check('late real draft response for A cannot overwrite active B');
 await send('Emulation.setDeviceMetricsOverride',{width:375,height:812,deviceScaleFactor:1,mobile:false});
 assert.equal(await ev(`getComputedStyle(${visible('[data-testid="ass-editor"]')}).gridTemplateColumns.split(' ').length`),1);
 assert((await ev('document.documentElement.scrollWidth'))<=375);await shot('narrow-editor.png');await check('375px responsive editor has no document overflow');
 await ev(`(${visible('[data-testid="ass-save"]')}).scrollIntoView({block:'center'})`);await sleep(100);
 const reachable=await ev(`(()=>{const b=${visible('[data-testid="ass-save"]')},r=b.getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight&&!b.disabled})()`);assert(reachable,'narrow editor actions cannot be reached by scrolling');
 await shot('narrow-editor-actions.png');await check('narrow editor can scroll to usable save and generation controls');
 await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
 await close();await post('/acceptance-fixture/revision',{});await wait(`document.body.innerText.includes('rev-export-2')`);await editor(assetA);
 assert.equal(await value('[data-testid="ass-font-size"]'),'36');await cue(1);assert.equal(await value('[data-testid="ass-start-ms"]'),'1000');
 await check('new export revision starts a new draft without old time overrides');
 assert.deepEqual(errors,[]);
} catch(error) {
 checks.push({name:'complete real ASS editor chain',pass:false,error:error.stack??String(error)});process.exitCode=1;
 if(ws?.readyState===WebSocket.OPEN){await fs.writeFile(path.join(out,'failure-dom.txt'),await ev('document.body.innerText').catch(()=>''));await fs.writeFile(path.join(out,'failure-dom.html'),await ev('document.body.outerHTML').catch(()=>''));await shot('failure.png').catch(()=>{});}
} finally {
 if(ws?.readyState===WebSocket.OPEN){await send('Browser.close').catch(()=>{});ws.close();}
 if(browser.exitCode===null){await Promise.race([new Promise(r=>browser.once('exit',r)),sleep(2000)]);if(browser.exitCode===null)browser.kill();}
 await log.close();await fs.writeFile(path.join(out,'results.json'),JSON.stringify({mode:'real_go_mcp_worker_ffmpeg_browser',primary,checks,errors,browserPID:browser.pid},null,2));
 console.log(JSON.stringify({passed:checks.filter(c=>c.pass).length,failed:checks.filter(c=>!c.pass).length}));
}
