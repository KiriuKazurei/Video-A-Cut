import { testStorageRoot } from './test-paths.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { CDP,sleep } from './tauri-cdp.mjs';
const [portArg,pidArg,rootArg]=process.argv.slice(2),pid=Number(pidArg),root=path.resolve(rootArg);
assert(root.startsWith(testStorageRoot+path.sep));
const c=await CDP.connect(Number(portArg)),file=path.join(root,'recovery.json'),report=JSON.parse(await fs.readFile(file));
report.checks=report.checks.filter(v=>v.pass);
try {
 const journal=JSON.parse(await fs.readFile(path.join(root,'数据 目录','desktop-session.json'))),start=Date.now();
 await c.invoke('stop_runtime');
 let value;for(let i=0;i<260;i++){value=await c.invoke('runtime_status');if(value.status==='stopped')break;await sleep(100)}assert.equal(value.status,'stopped');
 for(const owned of [journal.control_pid,...journal.workers.map(w=>w.pid)])assert.throws(()=>process.kill(owned,0));
 report.checks.push({name:'controlled stop removes every Go/Worker process',pass:true,elapsed_ms:Date.now()-start});
 await c.invoke('request_exit');await sleep(1000);assert.throws(()=>process.kill(pid,0));report.checks.push({name:'explicit exit closes release desktop',pass:true});
 report.passed=true;
}catch(error){report.passed=false;report.checks.push({name:'controlled stop/exit',pass:false,error:String(error)});process.exitCode=1;}
finally{c.close();await fs.writeFile(file,JSON.stringify(report,null,2));console.log(JSON.stringify({passed:report.checks.filter(v=>v.pass).length,failed:report.checks.filter(v=>!v.pass).length}))}
