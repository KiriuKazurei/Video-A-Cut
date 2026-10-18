import { testDirectory } from './test-paths.mjs';
// Real local API, SAPI audio and Edge DOM. Never invokes a cloud model.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import {spawn} from 'node:child_process';

const base=process.argv[2];
if (!base || !['127.0.0.1','localhost'].includes(new URL(base).hostname)) throw new Error('loopback URL required');
const out=testDirectory('tts-live/browser');await fs.mkdir(out,{recursive:true});
const socket=net.createServer();await new Promise(r=>socket.listen(0,'127.0.0.1',r));const port=socket.address().port;await new Promise(r=>socket.close(r));
const log=await fs.open(path.join(out,'edge.log'),'w');
const browser=spawn('C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',[
 '--headless=new','--disable-gpu','--no-first-run','--no-default-browser-check','--disable-background-networking',
 `--remote-debugging-port=${port}`,`--user-data-dir=${path.join(out,'edge-profile')}`,'about:blank'
],{windowsHide:true,stdio:['ignore','ignore',log.fd]});
const sleep=ms=>new Promise(r=>setTimeout(r,ms)),checks=[],errors=[],pending=new Map();let ws,seq=0;
const send=(method,params={})=>new Promise((resolve,reject)=>{const id=++seq;const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method))},15000);pending.set(id,{resolve:v=>{clearTimeout(timer);resolve(v)},reject:e=>{clearTimeout(timer);reject(e)}});ws.send(JSON.stringify({id,method,params}));});
const ev=async expression=>{const r=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result.value;};
const wait=async (expr,ms=15000)=>{const until=Date.now()+ms;while(Date.now()<until){if(await ev(`Boolean(${expr})`))return;await sleep(100)}throw Error('wait failed: '+expr)};
const visible=s=>`[...document.querySelectorAll(${JSON.stringify(s)})].find(e=>e.checkVisibility())`;
const button=name=>`[...document.querySelectorAll('button')].find(e=>e.checkVisibility()&&(e.innerText.trim()===${JSON.stringify(name)}||e.getAttribute('aria-label')===${JSON.stringify(name)}))`;
const click=async name=>{await wait(button(name));await ev(`${button(name)}.click()`)};
const setInput=async (selector,value)=>{await wait(visible(selector));await ev(`(()=>{const el=${visible(selector)};const proto=el.tagName==='TEXTAREA'?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;Object.getOwnPropertyDescriptor(proto,'value').set.call(el,${JSON.stringify(value)});el.dispatchEvent(new Event('input',{bubbles:true}));el.dispatchEvent(new Event('change',{bubbles:true}));})()`)};
const api=async (url,method='GET',body)=>{const r=await fetch(base+url,{method,headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});if(!r.ok)throw Error(`${method} ${url} ${r.status}: ${await r.text()}`);return r.json();};
const record=name=>checks.push({name,pass:true});
try{
 let targets;for(let i=0;i<100;i++){try{targets=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json();if(targets.some(t=>t.type==='page'))break}catch{}await sleep(100)}
 assert(targets?.length,'Edge did not start');ws=new WebSocket(targets.find(t=>t.type==='page').webSocketDebuggerUrl);await new Promise((r,j)=>{ws.onopen=r;ws.onerror=j});
 ws.onmessage=e=>{const m=JSON.parse(e.data);if(m.method==='Runtime.exceptionThrown')errors.push(m.params);const p=pending.get(m.id);if(p){pending.delete(m.id);m.error?p.reject(Error(m.error.message)):p.resolve(m.result)}};
 await send('Runtime.enable');await send('Page.enable');await send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
 await send('Page.navigate',{url:base+'/#/resources'});await wait(`document.body.innerText.includes('资源')`);
 await send('Input.dispatchKeyEvent',{type:'keyDown',key:'5',code:'Digit5',text:'5'});await send('Input.dispatchKeyEvent',{type:'keyUp',key:'5',code:'Digit5'});
 await wait(`location.hash==='#/tts' && document.body.innerText.includes('TTS 调控台')`);
 record('shortcut 5 opens an independent TTS workspace without an asset');
 const cap=await api('/api/tts/capabilities');assert(cap.runtime_ready&&cap.media_ready&&cap.pitch_available);assert(cap.sapi_voices.length>0);record('real Python, FFmpeg rubberband and installed SAPI capabilities');
 await wait(`${button('保存模板')} && !${button('保存模板')}.disabled`);
 await setInput('[aria-label="TTS 模板名称"]','浏览器云端模板');await click('保存模板');await wait(`document.body.innerText.includes('已保存 浏览器云端模板')`);
 const cloud=(await api('/api/tts/templates')).find(t=>t.name==='浏览器云端模板');assert(cloud&&cloud.settings.endpoint==='https://api.stepfun.com/step_plan/v1');record('cloud template saves without successful audition or credentials');
 await ev(`[...document.querySelectorAll('.ant-segmented-item')].find(e=>e.innerText==='Windows SAPI').click()`);
 await setInput('[aria-label="TTS 朗读文本"]','这是一段用于验证试听的文本。');
 await setInput('[aria-label="TTS 模板名称"]','浏览器 SAPI 模板');
 await wait(`document.body.innerText.includes('SAPI 等级')`);await wait(`${visible('[role="slider"][aria-label="语速"]')}`);await ev(`${visible('[role="slider"][aria-label="语速"]')}.focus()`);
 await send('Input.dispatchKeyEvent',{type:'keyDown',key:'ArrowUp',code:'ArrowUp',windowsVirtualKeyCode:38,nativeVirtualKeyCode:38});await send('Input.dispatchKeyEvent',{type:'keyUp',key:'ArrowUp',code:'ArrowUp',windowsVirtualKeyCode:38,nativeVirtualKeyCode:38});
 await wait(`${visible('[aria-label="语速数值"]')}.value==='1'`);await click('重置语速');await wait(`${visible('[aria-label="语速数值"]')}.value==='0'`);
 await setInput('[aria-label="音调数值"]','3');record('fader keyboard, numeric pitch and reset controls');
 await wait(`${button('生成试听')} && !${button('生成试听')}.disabled`);await click('生成试听');
 // Edit while the paid-job-style snapshot is in flight: the late result must stay old.
 await wait(`document.body.innerText.includes('已提交试听')`);await setInput('[aria-label="TTS 朗读文本"]','后续草稿不能被异步结果覆盖。');
 await wait(`${visible('audio[aria-label="TTS 试听播放器"]')}`,30000);await wait(`${visible('svg[aria-label="试听音频实际波形"]')}`);
 assert.equal(await ev(`${visible('[aria-label="TTS 朗读文本"]')}.value`),'后续草稿不能被异步结果覆盖。');assert(await ev(`document.body.innerText.includes('旧配置试听')`));record('late SAPI result stays old and leaves the current text intact');
 const jobs=await api('/api/tts/preview-jobs');const success=jobs.find(j=>j.status==='succeeded');assert(success&&success.settings.pitch_semitones===3);const wav=await fetch(base+`/api/tts/preview-jobs/${success.job_id}/audio?download=1`);assert.equal(wav.headers.get('Content-Type'),'audio/wav');assert(wav.headers.get('Content-Disposition').includes('attachment'));const audio=Buffer.from(await wav.arrayBuffer());assert.equal(audio.toString('ascii',0,4),'RIFF');record('real SAPI audition normalized WAV, waveform and WAV download');
 await click('从头重听');await wait(`${visible('audio')}.paused===false`);await ev(`location.hash='#/settings'`);await wait(`document.querySelector('audio').paused`);await ev(`location.hash='#/tts'`);await wait(`${visible('[aria-label="TTS 朗读文本"]')}`);assert.equal(await ev(`${visible('[aria-label="TTS 朗读文本"]')}.value`),'后续草稿不能被异步结果覆盖。');record('workspace switch pauses audio and preserves draft');
 await click('另存为');await wait(`document.body.innerText.includes('已保存 浏览器 SAPI 模板')`);let saved=(await api('/api/tts/templates')).find(t=>t.name==='浏览器 SAPI 模板');assert(saved&&saved.settings.engine==='sapi');
 await setInput('[aria-label="音量数值"]','0.8');await click('保存新版本');await wait(`document.body.innerText.includes('已保存 浏览器 SAPI 模板 · v2')`);saved=(await api('/api/tts/templates')).find(t=>t.template_id===saved.template_id);assert.equal(saved.revision,2);assert.equal((await api(`/api/tts/templates/${saved.template_id}/revisions/1`)).settings.volume,1);record('template clone and immutable version history');
 // Real server protocol errors and cancellation. Missing credential is tested by Python/Go too.
 const bad=await fetch(base+'/api/tts/preview-jobs',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({text:' ',settings:saved.settings,idempotency_key:'browser-invalid'})});assert.equal(bad.status,400);
 const cancel=await api('/api/tts/preview-jobs','POST',{text:'测试取消。'.repeat(100),settings:saved.settings,idempotency_key:'browser-cancel-'+Date.now()});
 for(let i=0;i<100;i++){if((await api(`/api/tts/preview-jobs/${cancel.job_id}`)).status==='running')break;await sleep(50)}
 const queued=await api('/api/tts/preview-jobs','POST',{text:'第二项合成必须等待。',settings:saved.settings,idempotency_key:'browser-queued-'+Date.now()});
 assert.equal((await api(`/api/tts/preview-jobs/${queued.job_id}`)).status,'queued');
 await api(`/api/tts/preview-jobs/${queued.job_id}/cancel`,'POST',{});await sleep(700);
 const cancelled=await api(`/api/tts/preview-jobs/${cancel.job_id}/cancel`,'POST',{});assert.equal(cancelled.status,'cancelled');await sleep(300);assert.equal((await api(`/api/tts/preview-jobs/${cancel.job_id}`)).status,'cancelled');record('single active synthesis, queued and running cancellation remain terminal');
 assert(await ev(`Boolean(${visible('[data-workspace="tts"]')})`),'desktop TTS navigation is collapsed');
 const screenshot=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(path.join(out,'tts-1440.png'),Buffer.from(screenshot.data,'base64'));
 await send('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:false});await sleep(400);
 assert(await ev(`document.documentElement.scrollWidth<=innerWidth+1`),'narrow screen overflows');
 const mobile=await send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(path.join(out,'tts-390.png'),Buffer.from(mobile.data,'base64'));record('390px responsive layout without horizontal overflow');
 assert.deepEqual(errors,[]);
}catch(e){checks.push({name:'live TTS workspace',pass:false,error:String(e)});process.exitCode=1;if(ws?.readyState===WebSocket.OPEN)await fs.writeFile(path.join(out,'failure-dom.txt'),await ev('document.body.innerText').catch(()=>''));}
finally{if(ws?.readyState===WebSocket.OPEN){await send('Browser.close').catch(()=>{});ws.close()}if(browser.exitCode===null){await Promise.race([new Promise(r=>browser.once('exit',r)),sleep(2000)]);if(browser.exitCode===null)browser.kill()}await log.close();await fs.writeFile(path.join(out,'results.json'),JSON.stringify({checks,errors,mode:'real_headless_edge_and_sapi',cloud:'not invoked',human_listening:'not performed'},null,2));console.log(JSON.stringify({passed:checks.filter(c=>c.pass).length,failed:checks.filter(c=>!c.pass).length,checks}));}
