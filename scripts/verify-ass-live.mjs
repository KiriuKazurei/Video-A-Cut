// Real local Go API only. Run while TestVariantRealWorkerChain owns the server.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import { spawn } from 'node:child_process';

const [base,runId]=process.argv.slice(2);
if (!base || !['127.0.0.1','localhost'].includes(new URL(base).hostname) || !runId) throw Error('loopback API and run ID required');
const out=path.resolve(process.env.VAC_TEST_OUTPUT || '.run-data/ass-live-browser');
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
 const panel='[data-testid="ass-panel"]';
 await wait(visible(panel));
 await wait(visible(panel+' [data-testid="ass-downloads"]'));
 const original=await getJSON(`${base}/api/workflows/${encodeURIComponent(runId)}/delivery-variants`);
 const done=original.variants.find(v=>v.status==='succeeded'&&v.is_current);assert(done);
 const links=await ev(`(()=>{const p=${visible(panel)};return [...p.querySelectorAll('[data-testid="ass-downloads"] a')].map(a=>({text:a.innerText,href:a.getAttribute('href')}))})()`);
 assert(links.some(l=>l.text==='下载 ASS'&&l.href.includes(done.variant_id)));assert(links.some(l=>l.text==='下载原 SRT'));assert(links.some(l=>l.text==='下载完整交付包'));
 checks.push({name:'real registered ASS/SRT/ZIP links visible',pass:true});
 const file=await fetch(base+links.find(l=>l.text==='下载 ASS').href);assert.equal(file.status,200);assert.match(file.headers.get('content-disposition'),/attachment/);assert.match(await file.text(),/Dialogue: 0/);checks.push({name:'real ASS attachment download',pass:true});
 await wait(`[...document.querySelectorAll('${panel} button')].some(b=>b.innerText==='生成 ASS'&&!b.disabled)`);
 await ev(`(()=>{[...document.querySelectorAll('${panel} button')].find(b=>b.innerText==='生成 ASS').click()})()`);
 let fresh;
 for(let i=0;i<50;i++){const list=await getJSON(`${base}/api/workflows/${encodeURIComponent(runId)}/delivery-variants`);fresh=list.variants.find(v=>v.variant_id!==done.variant_id&&v.status==='queued');if(fresh)break;await sleep(80);}
 assert(fresh);checks.push({name:'UI ASS regeneration creates new queued task',pass:true});
 await wait(`[...document.querySelectorAll('${panel} button')].some(b=>b.innerText==='取消 ASS 生成')`);
 await ev(`(()=>{[...document.querySelectorAll('${panel} button')].find(b=>b.innerText==='取消 ASS 生成').click()})()`);
 await wait(`![...document.querySelectorAll('${panel} button')].some(b=>b.innerText==='取消 ASS 生成')`);
 assert.equal((await getJSON(`${base}/api/delivery-variants/${fresh.variant_id}`)).status,'cancelled');checks.push({name:'UI ASS cancellation persists through Go',pass:true});
 await send('Emulation.setDeviceMetricsOverride',{width:375,height:812,deviceScaleFactor:1,mobile:false});await sleep(300);
 const width=await ev('document.documentElement.scrollWidth');assert(width<=375);checks.push({name:'375px no document overflow',pass:true,scrollWidth:width});
 await ev(`${visible(panel)}.scrollIntoView({block:'center'})`);await sleep(150);
 const shot=await send('Page.captureScreenshot',{format:'png'});await fs.writeFile(path.join(out,'ass-375.png'),Buffer.from(shot.data,'base64'));
 await getJSON(`${base}/api/workflows/${encodeURIComponent(runId)}/cancel`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({expected_version:original.expected_version,reason:'controlled ASS stale revision check'})});
 await wait(`!${visible(panel+' [data-testid="ass-downloads"]')}`);checks.push({name:'workflow invalidation clears current ASS links',pass:true});
 assert.deepEqual(errors,[]);
} catch(e){checks.push({name:'live browser verification',pass:false,error:String(e)});process.exitCode=1;
 if(ws?.readyState===WebSocket.OPEN)await fs.writeFile(path.join(out,'failure-dom.txt'),await ev('document.body.innerText').catch(()=>''));
} finally {
 if(ws?.readyState===WebSocket.OPEN){await send('Browser.close').catch(()=>{});ws.close()}
 if(browser.exitCode===null){await Promise.race([new Promise(r=>browser.once('exit',r)),sleep(2000)]);if(browser.exitCode===null)browser.kill()}
 await edgeLog.close();await fs.writeFile(path.join(out,'results.json'),JSON.stringify({checks,errors,browserPID:browser.pid,server:base,mode:'real_go_ass_worker_ffmpeg_browser'},null,2));
 console.log(JSON.stringify({passed:checks.filter(c=>c.pass).length,failed:checks.filter(c=>!c.pass).length}));
}
