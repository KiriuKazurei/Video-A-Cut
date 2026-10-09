import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { CDP,sleep } from './tauri-cdp.mjs';
const [portArg,pidArg,rootArg]=process.argv.slice(2),pid=Number(pidArg),root=path.resolve(rootArg);
assert(root.startsWith(path.resolve('.run-data')+path.sep));
const c=await CDP.connect(Number(portArg)),file=path.join(root,'desktop.json'),original=await fs.readFile(file,'utf8');
const checks=[],record=(name,detail)=>checks.push({name,pass:true,detail});let holder;
const run=(exe,args)=>new Promise((resolve,reject)=>{const p=spawn(exe,args,{windowsHide:true,stdio:['ignore','pipe','pipe']});let log='';p.stdout.on('data',b=>log+=b);p.stderr.on('data',b=>log+=b);p.on('error',reject);p.on('exit',code=>code?reject(Error(log)):resolve(log))});
const probe=(action,title,extra=[])=>run('pwsh',['-NoProfile','-File','scripts/Tauri-WindowProbe.ps1','-DesktopPid',String(pid),'-Action',action,'-DialogTitle',title,...extra]);
const until=async(callback,predicate,timeout=30000)=>{const deadline=Date.now()+timeout;let last;while(Date.now()<deadline){last=await callback();if(predicate(last))return last;await sleep(100)}throw Error('Timeout '+JSON.stringify(last))};
const status=()=>c.invoke('runtime_status');
let base=(await status()).api_base_url;
const api=async(endpoint,body,method=body?'POST':'GET')=>{const r=await fetch(base+endpoint,{method,headers:{'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});if(!r.ok)throw Error(await r.text());return r.json()};
try {
  await c.invoke('stop_runtime');await until(status,v=>v.status==='stopped');
  const cfg=JSON.parse(original);
  holder=spawn(cfg.python,['-c',"import sys,msvcrt;f=open(sys.argv[1],'r+b');f.write(b'0');f.flush();f.seek(0);msvcrt.locking(f.fileno(),msvcrt.LK_NBLCK,1);print('locked',flush=True);sys.stdin.read()",path.join(cfg.data_directory,'runtime.lock')],{windowsHide:true,stdio:['pipe','pipe','pipe']});
  await new Promise((resolve,reject)=>{holder.stdout.once('data',resolve);holder.once('error',reject);holder.once('exit',code=>reject(Error('Lock holder exited '+code)))});
  await c.invoke('restart_runtime');const locked=await until(status,v=>v.status==='fault');assert(locked.message.includes('其他程序使用'),locked.message);record('data lock held by another process blocks startup',locked.message);
  holder.stdin.end();await new Promise(resolve=>holder.once('exit',resolve));holder=null;
  await fs.writeFile(file,'{broken configuration');await c.invoke('restart_runtime');const invalid=await until(status,v=>v.status==='fault');await c.wait(`document.body.innerText.includes('重新启动')`);record('malformed configuration appears in window without killing desktop',invalid.message);
  await fs.writeFile(file,original);await c.invoke('restart_runtime');const ready=await until(status,v=>['fault','ready'].includes(v.status),60000);assert.equal(ready.status,'ready',ready.message);base=ready.api_base_url;record('fix config and retry reads updated file; history retained',{base});
  // Native picker must reject a real MP4 outside the configured source root.
  await c.ev(`void (window.__outside=window.__TAURI__.core.invoke('select_recording').then(v=>({value:v}),e=>({error:String(e)})))`);
  await probe('choose','选择受控目录内的录像',['-FilePath',path.join(root,'数据 目录','delivery','source','a.mp4')]);
  const outside=await c.ev('window.__outside');assert(outside.error?.includes('不在已配置'),JSON.stringify(outside));record('native picker rejects file outside controlled recording root');
  // Reconnect has no replay: verify actual asset snapshot requests after offline recovery.
  await c.send('Network.enable');let snapshots=0;const dispatch=c.ws.onmessage;
  c.ws.onmessage=event=>{const m=JSON.parse(event.data);if(m.method==='Network.requestWillBeSent'&&m.params.request.url===base+'/api/assets')snapshots++;dispatch(event)};
  await c.send('Network.emulateNetworkConditions',{offline:true,latency:0,downloadThroughput:-1,uploadThroughput:-1});await sleep(700);
  await api('/api/assets/desktop_business',{locked:true},'PATCH');const before=snapshots;
  await c.send('Network.emulateNetworkConditions',{offline:false,latency:0,downloadThroughput:-1,uploadThroughput:-1});await c.wait(`document.body.innerText.includes('已连接')`);await until(async()=>snapshots,v=>v>before);await api('/api/assets/desktop_business',{locked:false},'PATCH');record('SSE offline reconnect triggers actual REST asset snapshots',{snapshots});
  const cap=await api('/api/tts/capabilities');const voice=cap.sapi_voices.find(v=>v.includes('Huihui'))||cap.sapi_voices[0];assert(voice);
  const job=await api('/api/tts/preview-jobs',{text:'这是退出确认的本地测试。'.repeat(50),settings:{engine:'sapi',voice,language:'zh',speed:1,volume:1,pitch_semitones:0,sapi_rate:0,instruction:''},idempotency_key:'desktop-exit-'+Date.now()});
  assert((await api('/api/runtime/activity')).active_tasks>0);await c.invoke('request_exit');await probe('cancel','退出 Video Auto Cut');assert.equal((await status()).status,'ready');record('standalone TTS job triggers native exit confirmation; cancel keeps service running');
  const current=await api('/api/tts/preview-jobs/'+job.job_id);
  if(['queued','running'].includes(current.status)){try{await api('/api/tts/preview-jobs/'+job.job_id+'/cancel',{})}catch(error){const final=await api('/api/tts/preview-jobs/'+job.job_id);if(['queued','running'].includes(final.status))throw error}}
  await until(()=>api('/api/runtime/activity'),v=>v.active_tasks===0);
  const journal=JSON.parse(await fs.readFile(path.join(cfg.data_directory,'desktop-session.json')));
  const start=Date.now();await c.invoke('stop_runtime');await until(status,v=>v.status==='stopped',30000);
  for(const owned of [journal.control_pid,...journal.workers.map(w=>w.pid)])assert.throws(()=>process.kill(owned,0));
  record('controlled stop removes every Go/Worker process',{elapsed_ms:Date.now()-start});
  await c.invoke('request_exit');await sleep(800);assert.throws(()=>process.kill(pid,0));record('explicit exit closes release desktop');
} catch(error){checks.push({name:'release recovery and exit',pass:false,error:String(error)});process.exitCode=1;}
finally {if(holder){holder.stdin.end();holder.kill()}await fs.writeFile(file,original);c.close();await fs.writeFile(path.join(root,'recovery.json'),JSON.stringify({checks,passed:checks.every(v=>v.pass)},null,2));console.log(JSON.stringify({passed:checks.filter(v=>v.pass).length,failed:checks.filter(v=>!v.pass).length}))}
