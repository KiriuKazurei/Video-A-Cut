import { testStorageRoot } from './test-paths.mjs';
// Same machine, same Go service, same preview (720p preset, no upscaling), GPU enabled.
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import net from 'node:net';
import { spawn } from 'node:child_process';
import { CDP, sleep } from './tauri-cdp.mjs';
const [desktopPortArg,desktopPidArg,rootArg]=process.argv.slice(2), desktopPid=Number(desktopPidArg), root=path.resolve(rootArg);
assert(root.startsWith(testStorageRoot+path.sep));
const desktop=await CDP.connect(Number(desktopPortArg)), status=await desktop.invoke('runtime_status');
assert.equal(status.status,'ready');const base=status.api_base_url;
const server=net.createServer();await new Promise(r=>server.listen(0,'127.0.0.1',r));const port=server.address().port;await new Promise(r=>server.close(r));
const edge=spawn('C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',['--no-first-run','--no-default-browser-check','--disable-background-networking','--disable-component-update','--disk-cache-size=1048576','--media-cache-size=1048576',`--remote-debugging-port=${port}`,`--user-data-dir=${path.join(root,'edge-profile')}`,base+'/#/resources'],{windowsHide:false,stdio:'ignore'});
let browser;const report={mode:'pilot_same_machine_release_gpu_enabled',source:'real local Go/Worker generated 4-second preview using the 720p preset without upscaling, from a synthetic fixture',note:'Small engineering sample. No claim about model quality, long recordings, sustained load or human acceptance.'};
const video=`[...document.querySelectorAll('video')].find(e=>e.checkVisibility()&&e.getAttribute('src')?.includes('/api/previews/'))`;
const run=(exe,args)=>new Promise((resolve,reject)=>{const p=spawn(exe,args,{windowsHide:true,stdio:['ignore','pipe','pipe']});let log='';p.stdout.on('data',b=>log+=b);p.stderr.on('data',b=>log+=b);p.on('error',reject);p.on('exit',code=>code?reject(Error(log)):resolve(log))});
async function sample(c) {
 await c.send('Page.bringToFront');
 await c.send('Emulation.setFocusEmulationEnabled',{enabled:true});
 await c.send('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false});
 await c.wait(`document.querySelector('.app-shell')&&document.body.innerText.includes('已连接')`);
 await c.wait(`document.querySelector('[data-asset-id="desktop_business"]')`);
 await c.ev(`document.querySelector('[data-asset-id="desktop_business"]').click(); location.hash='#/export/delivery'`);
 await c.wait(`${video}?.readyState>=2`,45000);
 const media=await c.ev(`(async()=>{const v=${video};v.muted=true;v.loop=true;v.currentTime=0;const q=v.getVideoPlaybackQuality();await v.play();await new Promise(r=>setTimeout(r,6000));v.pause();const z=v.getVideoPlaybackQuality();return{width:v.videoWidth,height:v.videoHeight,duration:v.duration,total_frames:z.totalVideoFrames-q.totalVideoFrames,dropped_frames:z.droppedVideoFrames-q.droppedVideoFrames,source:v.currentSrc}})()`);
 const seek=[];for(const target of [0.5,2.5,1,3,0.25]){seek.push(await c.ev(`new Promise((resolve,reject)=>{const v=${video},start=performance.now(),timer=setTimeout(()=>reject(Error('seek timeout')),5000);v.addEventListener('seeked',()=>{clearTimeout(timer);resolve(performance.now()-start)},{once:true});v.currentTime=${target}})`))}
 const ui=[];for(let i=0;i<5;i++){for(const route of ['#/resources','#/tts','#/export/delivery']){ui.push(await c.ev(`(async()=>{const start=performance.now();location.hash=${JSON.stringify(route)};await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));return performance.now()-start})()`))}}
 const median=a=>[...a].sort((a,b)=>a-b)[Math.floor(a.length/2)];
 return{media,seek_ms:seek,seek_median_ms:median(seek),navigation_paint_ms:ui,navigation_median_ms:median(ui),user_agent:await c.ev('navigator.userAgent')};
}
try {
 report.desktop=await sample(desktop);await desktop.screenshot(path.join(root,'release-program-monitor.png'));
 report.desktop_memory=JSON.parse(await run('pwsh',['-NoProfile','-File','scripts/Measure-TauriProcessMemory.ps1','-RootPid',String(desktopPid)]));
 browser=await CDP.connect(port);await browser.send('Page.navigate',{url:base+'/#/resources'});report.browser=await sample(browser);await browser.screenshot(path.join(root,'browser-baseline.png'));
 const journal=JSON.parse(await fs.readFile(path.join(root,'数据 目录','desktop-session.json')));
 const roots=[edge.pid,journal.control_pid,...journal.workers.map(w=>w.pid)];
 report.browser_memory=JSON.parse(await run('pwsh',['-NoProfile','-File','scripts/Measure-TauriProcessMemory.ps1','-RootPid',roots.join(',')]));
 report.passed=true;
} catch(error){report.passed=false;report.error=String(error);process.exitCode=1;if(browser){report.browser_errors=browser.errors;await fs.writeFile(path.join(root,'baseline-failure-dom.txt'),await browser.ev('location.href+"\n"+document.body.innerText').catch(()=>''));}}
finally {
 if(browser){await browser.send('Browser.close').catch(()=>{});browser.close()}
 if(edge.exitCode===null){await Promise.race([new Promise(r=>edge.once('exit',r)),sleep(2000)]);if(edge.exitCode===null)edge.kill()}
 desktop.close();await fs.writeFile(path.join(root,'performance.json'),JSON.stringify(report,null,2));console.log(JSON.stringify({passed:report.passed,error:report.error,desktop:report.desktop?.navigation_median_ms,browser:report.browser?.navigation_median_ms}));
}
