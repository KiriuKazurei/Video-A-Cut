// Real local Go API only. Run while TestPreviewRealWorkerChain owns the server.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import { spawn } from 'node:child_process';

const [base,runId]=process.argv.slice(2);
if (!base || !['127.0.0.1','localhost'].includes(new URL(base).hostname) || !runId) throw Error('loopback API and run ID required');
const out=path.resolve(process.env.VAC_TEST_OUTPUT || '.run-data/mp4-live-browser');
await fs.mkdir(out,{recursive:true});
const checks=[],errors=[];
const socket=net.createServer();await new Promise(r=>socket.listen(0,'127.0.0.1',r));const port=socket.address().port;await new Promise(r=>socket.close(r));
const edgeLog=await fs.open(path.join(out,'edge.log'),'w');
const browser=spawn(process.env.EDGE_PATH || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',[
 '--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--disable-background-networking',`--remote-debugging-port=${port}`,`--user-data-dir=${path.join(out,'edge-profile')}`,'about:blank'
],{windowsHide:true,stdio:['ignore','ignore',edgeLog.fd]});
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
let ws,seq=0;const pending=new Map();
const send=(method,params={})=>new Promise((resolve,reject)=>{
 const id=++seq;const timer=setTimeout(()=>{pending.delete(id);reject(Error(`CDP timeout ${method}`));},15000);
 pending.set(id,{resolve:r=>{clearTimeout(timer);resolve(r)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params}));
});
const ev=async expression=>{const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result.value;};
const visible=selector=>`[...document.querySelectorAll(${JSON.stringify(selector)})].find(e=>e.checkVisibility())`;
const root='[data-page-view="export"]';const video=visible(root+' video');
const wait=async expression=>{for(let i=0;i<125;i++){if(await ev(`Boolean(${expression})`))return;await sleep(80);}throw Error(`wait failed: ${expression}`);};
async function getJSON(url,init){const res=await fetch(url,init);const body=await res.json();if(!res.ok)throw Error(JSON.stringify(body));return body;}
try {
 let targets;
 for(let i=0;i<100;i++){try{targets=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json();if(targets.some(t=>t.type==='page'))break;}catch{}await sleep(100);}
 assert(targets?.some(t=>t.type==='page'),'Edge did not start');
 ws=new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);await new Promise((r,j)=>{ws.onopen=r;ws.onerror=j});
 ws.onmessage=e=>{const msg=JSON.parse(e.data);if(msg.method==='Runtime.exceptionThrown')errors.push(msg.params);const p=pending.get(msg.id);if(p){pending.delete(msg.id);msg.error?p.reject(Error(msg.error.message)):p.resolve(msg.result)}};
 await send('Runtime.enable');await send('Page.enable');await send('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false});
 await send('Page.navigate',{url:base+'/#/export/delivery'});
 await wait(visible(root+' .preview-media-card'));
 await ev(`(()=>{const b=[...document.querySelectorAll('${root} .preview-media-card button')].find(e=>e.innerText.includes('播放'));b?.click()})()`);
 await wait(`${video}?.readyState>=2`);
 await ev(`(async()=>{const v=${video};v.muted=true;await v.play()})()`);await wait(`${video}?.currentTime>0`);
 const playback=await ev(`(()=>{const v=${video};return {time:v.currentTime,duration:v.duration,readyState:v.readyState,width:v.videoWidth,height:v.videoHeight,src:v.getAttribute('src')}})()`);
 assert(playback.time>0 && playback.width===320 && playback.height===180);checks.push({name:'real Go registered MP4 browser playback',pass:true,playback});
 await ev(`(()=>{const v=${video};v.pause();v.currentTime=3.5})()`);await wait(`Math.abs(${video}?.currentTime-3.5)<0.05 && !${video}?.seeking`);checks.push({name:'real API MP4 seek',pass:true});
 let list=await getJSON(`${base}/api/workflows/${encodeURIComponent(runId)}/previews`);
 const preview=list.previews.find(p=>p.status==='succeeded'&&p.is_current);
 assert(preview);
 const range=await fetch(`${base}/api/previews/${preview.preview_id}/files/mp4`,{headers:{Range:'bytes=0-31'}});assert.equal(range.status,206);await range.arrayBuffer();checks.push({name:'Range 206',pass:true});
 const download=await fetch(`${base}/api/previews/${preview.preview_id}/files/mp4?download=1`);assert.match(download.headers.get('content-disposition'),/attachment/);await download.arrayBuffer();checks.push({name:'attachment download',pass:true});
 // The real Worker has exited after its first task. A fresh UI action must
 // enqueue a different task rather than return the old succeeded preview.
 await wait(`[...document.querySelectorAll('${root} button')].some(b=>b.innerText.includes('生成 MP4 预览')&&!b.disabled)`);
 await ev(`(()=>{const b=[...document.querySelectorAll('${root} button')].find(b=>b.innerText.includes('生成 MP4 预览'));b.click()})()`);
 let fresh;
 for(let i=0;i<50;i++){
  list=await getJSON(`${base}/api/workflows/${encodeURIComponent(runId)}/previews`);
  fresh=list.previews.find(p=>p.preview_id!==preview.preview_id&&p.status==='queued');
  if(fresh)break;await sleep(80);
 }
 assert(fresh&&fresh.revision_id===preview.revision_id,'UI regeneration must enqueue a fresh preview');
 checks.push({name:'successful preview UI regeneration creates a new task',pass:true,oldPreview:preview.preview_id,newPreview:fresh.preview_id});
 await getJSON(`${base}/api/workflows/${encodeURIComponent(runId)}/cancel`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({expected_version:list.expected_version,reason:'controlled browser invalidation test'})});
 await wait(`!${video}`);checks.push({name:'real workflow invalidation stops and clears program source',pass:true});
 await send('Emulation.setDeviceMetricsOverride',{width:375,height:812,deviceScaleFactor:1,mobile:false});await sleep(300);
 const width=await ev('document.documentElement.scrollWidth');assert(width<=375);checks.push({name:'375px no document overflow',pass:true,scrollWidth:width});
 const shot=await send('Page.captureScreenshot',{format:'png'});await fs.writeFile(path.join(out,'invalidated-375.png'),Buffer.from(shot.data,'base64'));
 assert.deepEqual(errors,[]);
} catch(e){checks.push({name:'live browser verification',pass:false,error:String(e)});process.exitCode=1;
 if(ws?.readyState===WebSocket.OPEN)await fs.writeFile(path.join(out,'failure-dom.txt'),await ev('document.body.innerText').catch(()=>''));
} finally {
 if(ws?.readyState===WebSocket.OPEN){await send('Browser.close').catch(()=>{});ws.close()}
 if(browser.exitCode===null){await Promise.race([new Promise(r=>browser.once('exit',r)),sleep(2000)]);if(browser.exitCode===null)browser.kill()}
 await edgeLog.close();await fs.writeFile(path.join(out,'results.json'),JSON.stringify({checks,errors,browserPID:browser.pid,server:base,mode:'real_go_worker_ffmpeg_browser'},null,2));
 console.log(JSON.stringify({passed:checks.filter(c=>c.pass).length,failed:checks.filter(c=>!c.pass).length}));
}
