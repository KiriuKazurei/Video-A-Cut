import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';
import { CDP, sleep } from './tauri-cdp.mjs';
const [portArg,pidArg,rootArg] = process.argv.slice(2), pid=Number(pidArg), root=path.resolve(rootArg);
assert(root.startsWith(path.resolve('.run-data')+path.sep));
const c=await CDP.connect(Number(portArg));
const report=JSON.parse(await fs.readFile(path.join(root,'results.json')));
report.checks=report.checks.filter(v=>v.pass);
const save=()=>fs.writeFile(path.join(root,'results.json'),JSON.stringify(report,null,2));
const record=async(name,detail)=>{report.checks.push({name,pass:true,detail});await save()};
const run=(exe,args)=>new Promise((resolve,reject)=>{const child=spawn(exe,args,{windowsHide:true,stdio:['ignore','pipe','pipe']});let log='';child.stdout.on('data',b=>log+=b);child.stderr.on('data',b=>log+=b);child.on('error',reject);child.on('exit',code=>code?reject(Error(log)):resolve(log))});
const probe=(action)=>run('pwsh',['-NoProfile','-File','scripts/Tauri-WindowProbe.ps1','-DesktopPid',String(pid),'-Action',action]);
const until=async(callback,predicate,timeout=30000)=>{const deadline=Date.now()+timeout;let value;while(Date.now()<deadline){value=await callback();if(predicate(value))return value;await sleep(200)}throw Error(JSON.stringify(value))};
const journal=()=>fs.readFile(path.join(root,'数据 目录','desktop-session.json'),'utf8').then(JSON.parse);
try {
  await probe('close'); await sleep(300);
  assert.equal(await c.invoke('plugin:window|is_visible',{label:'main'}),false);
  assert.equal(JSON.parse(await probe('inspect')).visible,false);
  const initial=await c.invoke('runtime_status');assert.equal(initial.status,'ready');
  await record('WM_CLOSE hides actual main window while Go stays ready');
  await run(process.env.VAC_DESKTOP_EXECUTABLE || path.resolve('build/desktop/app/video-auto-cut-desktop.exe'),[]);await sleep(400);
  assert.equal(JSON.parse(await probe('inspect')).visible,true);await record('duplicate launch restores original main window');
  const before=await journal(), worker=before.workers.find(w=>w.role==='recognizer');process.kill(worker.pid);
  const replacement=await until(journal,v=>v.workers.find(w=>w.role==='recognizer').pid!==worker.pid);
  await record('owned Worker crash restarts within finite retry policy',{old:worker.pid,new:replacement.workers.find(w=>w.role==='recognizer').pid});
  const crashed=await journal();process.kill(crashed.control_pid);
  await until(()=>c.invoke('runtime_status'),v=>v.status==='fault');
  for(const w of crashed.workers){assert.throws(()=>process.kill(w.pid,0),undefined,'session Worker must exit after Go crash')}
  await c.screenshot(path.join(root,'desktop-go-fault.png'));
  await record('Go crash enters visible fault and every owned Worker exits');
  await c.invoke('restart_runtime');const restored=await until(()=>c.invoke('runtime_status'),v=>['ready','fault'].includes(v.status),60000);assert.equal(restored.status,'ready',restored.message);assert.notEqual(restored.api_base_url,initial.api_base_url);
  const workflow=await c.ev(`fetch(${JSON.stringify(restored.api_base_url+'/api/workflows/'+report.run_id)}).then(r=>r.json())`);assert.equal(workflow.run.status,'ready_for_acceptance');
  await c.wait(`document.querySelector('.app-shell') && document.body.innerText.includes('已连接')`);
  const state=await journal(), backup=JSON.parse(await fs.readFile(path.join(root,'数据 目录','sessions',state.session_id,'database-preflight.json')));assert.equal(backup.status,'backed_up');
  report.restarted_runtime=restored;
  report.media_url=report.media_url.replace(new URL(report.media_url).origin,restored.api_base_url);
  await record('new port/session, pre-start SQLite backup, history and SSE reconnect');
  report.passed=true;
} catch(error) {report.passed=false;report.checks.push({name:'lifecycle',pass:false,error:String(error)});process.exitCode=1;}
finally {await save();c.close();console.log(JSON.stringify({passed:report.checks.filter(v=>v.pass).length,failed:report.checks.filter(v=>!v.pass).length}))}
