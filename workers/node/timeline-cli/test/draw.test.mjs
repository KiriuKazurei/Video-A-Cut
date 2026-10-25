import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdir,readFile,writeFile} from 'node:fs/promises';
import {join} from 'node:path';
import {createHash} from 'node:crypto';
import {checkDraw,renderDraw,drawBrowser} from '../src/draw-runtime.mjs';
import {runTool} from '../src/media.mjs';
import {createServer} from 'node:http';
import puppeteer from 'puppeteer-core';
import {guardPage} from '../src/draw-network.mjs';
import {spawn,execFileSync} from 'node:child_process';
import {existsSync} from 'node:fs';
const html='<!doctype html><html><head><style>body{margin:0;background:#101820}#root{position:relative;width:320px;height:180px;overflow:hidden}#box{position:absolute;top:60px;width:40px;height:40px;background:#ff8800}</style></head><body><div id="root" data-composition-id="root" data-width="320" data-height="180" data-duration="1"><div id="box"></div><span style="color:white">VAC draw</span></div><script>const tl=gsap.timeline({paused:true});tl.to("#box",{x:200,duration:1,ease:"none"},0);window.__timelines={root:tl};</script></body></html>';
const react='import React from "react";import {AbsoluteFill,useCurrentFrame} from "remotion";export default function Scene(){const frame=useCurrentFrame();return <AbsoluteFill style={{background:"#101820"}}><div style={{position:"absolute",top:60,left:frame*6,width:40,height:40,background:"#ff8800"}}/><span style={{color:"white"}}>VAC draw</span></AbsoluteFill>}';
export const composition=(engine='hyperframes')=>{const source=engine==='hyperframes'?html:react;return {id:'drawing',name:'Draw test',engine,source,width:320,height:180,fps:{num:30,den:1},duration_frames:30,revision:1,content_version:createHash('sha256').update(source).digest('hex')};};
test('draw check validates source, dimensions, explicit modules and timebase without running code',async()=>{
 for(const engine of ['hyperframes','remotion'])assert.equal((await checkDraw({composition:composition(engine)})).valid,true);
 await assert.rejects(checkDraw({composition:{...composition(),width:321}}),/specification/);
 await assert.rejects(checkDraw({composition:{...composition(),fps:{num:-30,den:-1}}}),/specification/);
 await assert.rejects(checkDraw({composition:{...composition(),source:html.replace('data-duration="1"','data-duration="invalid"')}}),/data-duration/);

 await assert.rejects(checkDraw({composition:{...composition('remotion'),source:'import fs from "node:fs";export default ()=>null;'}}),/Only react/);
 await assert.rejects(checkDraw({composition:{...composition('remotion'),source:'export default ()=>{fetch("http://127.0.0.1");return null;}'}}),/Unavailable/);
 await assert.rejects(checkDraw({composition:{...composition(),source:html.replace('<head>','<head><script src="https://example.com/x.js"></script>')}}),/resources/);
});
for(const engine of ['hyperframes','remotion']){
 test('real '+engine+' renders frame-driven code with verified pixels and cache',{timeout:360000},async()=>{
  assert(process.env.VAC_TEST_OUTPUT,'Use TestWorkspace');
  const root=join(process.env.VAC_TEST_OUTPUT,'draw',engine);await mkdir(root,{recursive:true});const c=composition(engine);
  const report=await renderDraw({composition:c,output_dir:root});assert.equal(report.frames,30);assert.equal(report.width,320);assert.equal(report.height,180);assert.equal(report.engine,engine);assert.equal(report.duration,1);
  const movie=join(root,'program.mp4'),frame=async at=>{
   const text=await runTool('ffmpeg',['-v','error','-ss',String(at),'-i',movie,'-frames:v','1','-vf','crop=16:16:12:70,signalstats,metadata=print:file=-','-f','null','-']);return Number(text.match(/YAVG=([\d.]+)/)?.[1]);
  };
  const first=await frame(0),later=await frame(.8);assert(first>later+35,'the source program must move the orange box, not repeat a static frame');
  for(const [name,at] of [['first',0],['later',.8]])await runTool('ffmpeg',['-v','error','-y','-ss',String(at),'-i',movie,'-frames:v','1',join(root,name+'.png')]);
  assert.deepEqual(await renderDraw({composition:c,output_dir:root}),report);
  await writeFile(join(root,'verification.json'),JSON.stringify({report,pixels:{first,later},human_acceptance:'not_checked'},null,2));
  assert.equal(JSON.parse(await readFile(join(root,'program.json'),'utf8')).sha256,report.sha256);
 });
}

test('browser runtime blocks computed external requests and popups before source execution',{timeout:30000},async()=>{
 let externalHits=0,browser;
 const external=createServer((req,res)=>{externalHits++;res.setHeader('Access-Control-Allow-Origin','*');res.end('outside');});
 const owned=createServer((req,res)=>{res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><body>Owned render document</body></html>');});
 const listen=server=>new Promise((done,fail)=>{server.once('error',fail);server.listen(0,'127.0.0.1',done);});
 await listen(external);await listen(owned);
 try{
  browser=await puppeteer.launch({executablePath:drawBrowser(),headless:true});
  const page=await guardPage(await browser.newPage());
  const response=await page.goto('http://127.0.0.1:'+owned.address().port);
  assert.match(response.headers()['content-security-policy'],/sandbox allow-scripts allow-same-origin/);
  const outside='http://127.0.0.1:'+external.address().port+'/should-not-arrive';
  const blocked=await page.evaluate(async url=>{
   let request=false;try{await window['fet'+'ch'](url);request=true;}catch{}
   const popup=window.open(url,'_blank');
   return {request,popup:popup!==null};
  },outside);
  assert.deepEqual(blocked,{request:false,popup:false});
  assert.equal(externalHits,0);
 }finally{
  if(browser)await browser.close();
  await Promise.all([new Promise(done=>external.close(done)),new Promise(done=>owned.close(done))]);
 }
});


for(const engine of ['hyperframes','remotion']){
 test('imported image instances and source references render position, size and displacement in '+engine,{timeout:360000},async()=>{
  assert(process.env.VAC_TEST_OUTPUT,'Use TestWorkspace');
  const root=join(process.env.VAC_TEST_OUTPUT,'draw','images-'+engine);await mkdir(root,{recursive:true});
  const original=join(root,'imported.png');
  await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=lime:s=100x60','-frames:v','1','-threads','1',original]);
  const sha=createHash('sha256').update(await readFile(original)).digest('hex');
  for(const mode of ['layer','source']){
   const layer={id:'hero',resource_id:'image-current-project',resource_sha256:sha,x:10,y:120,width:80,height:40,dx:180,dy:0,from_frame:0,to_frame:29,fit:'fill',opacity:1,mode};
   const c={...composition(engine),images:[layer,{...layer,id:'stamp',mode:'layer',x:260,y:10,width:20,height:20,dx:0,dy:0,from_frame:0,to_frame:0}]};
   if(mode==='source'){
    c.source=engine==='remotion'
     ?'import React from "react";import {AbsoluteFill,Img,useCurrentFrame} from "remotion";export default function Scene({images}){const a=images.hero,frame=useCurrentFrame(),p=Math.max(0,Math.min(1,(frame-a.from_frame)/(a.to_frame-a.from_frame)));return <AbsoluteFill style={{background:"#101820"}}><Img src={a.src} style={{position:"absolute",left:a.x+a.dx*p,top:a.y+a.dy*p,width:a.width,height:a.height,objectFit:a.fit}}/></AbsoluteFill>}'
     :'<!doctype html><html><head><style>body{margin:0;background:#101820}#root{position:relative;width:320px;height:180px;overflow:hidden}</style></head><body><div id="root" data-composition-id="root" data-width="320" data-height="180" data-duration="1"><img id="photo" src="vac-image:hero"></div><script>const a=window.vacImages.hero;Object.assign(document.getElementById("photo").style,{position:"absolute",left:a.x+"px",top:a.y+"px",width:a.width+"px",height:a.height+"px",objectFit:a.fit});const tl=gsap.timeline({paused:true});tl.fromTo("#photo",{x:0},{x:a.dx,duration:(a.to_frame-a.from_frame)/30,ease:"none"},0);window.__timelines={root:tl};</script></body></html>';
   }
   c.content_version=createHash('sha256').update(JSON.stringify({source:c.source,images:c.images})).digest('hex');
   const output=join(root,mode),input={composition:c,output_dir:output,assets:[{resource_id:layer.resource_id,sha256:sha,path:original}]};
   const report=await renderDraw(input);
   assert.equal(report.frames,30);assert.equal(report.images.length,1);assert.equal(report.images[0].sha256,sha);
   const sample=async (at,crop='12:12:20:130')=>{
    const result=await runTool('ffmpeg',['-v','error','-ss',String(at),'-i',join(output,'program.mp4'),'-frames:v','1','-vf','crop='+crop+',signalstats,metadata=print:file=-','-f','null','-']);
    return Number(result.match(/YAVG=([\d.]+)/)?.[1]);
   };
   const first=await sample(0),later=await sample(.8);assert(first>later+50,mode+' must move the imported green picture out of its first bounds');
   const stampFirst=await sample(0,'12:12:264:14'),stampLater=await sample(.8,'12:12:264:14'),outside=await sample(0,'8:8:94:130'),below=await sample(0,'8:8:20:164');
   assert(stampFirst>120&&Math.abs(stampFirst-stampLater)<3,'another instance of the same original must remain static');
   assert(first>outside+50&&first>below+50,'requested image width and height must bound its pixels');

   for(const [name,at] of [['first',0],['later',.8]])await runTool('ffmpeg',['-v','error','-y','-ss',String(at),'-i',join(output,'program.mp4'),'-frames:v','1',join(output,name+'.png')]);
   await writeFile(join(output,'verification.json'),JSON.stringify({engine,mode,report,pixels:{first,later,stampFirst,stampLater,outside,below},human_acceptance:'not_checked'},null,2));
   if(mode==='layer'){
    const invalid={...c,images:[{...layer,resource_id:'outside-project'}]};
    await assert.rejects(renderDraw({...input,composition:invalid}),/missing or changed/);
   }
  }
 });
}

// Observe real GUI-browser windows and process exit, including source failure.
test('Remotion Chrome stays headless and closes its owned browser after success and source failure',{timeout:120000,skip:process.platform!=='win32'},async()=>{
 const chrome=process.env.CHROME||['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Google/Chrome/Application/chrome.exe'].find(existsSync);
 if(!chrome||!existsSync(chrome)||/msedge/i.test(chrome))throw Error('Installed Chrome required for Windows lifecycle acceptance; Edge is prohibited');
 const powershell='C:/Program Files/PowerShell/7/pwsh.exe';
 const observerFile=join(process.env.VAC_TEST_OUTPUT,'draw','lifecycle','observer-state.txt');await mkdir(join(process.env.VAC_TEST_OUTPUT,'draw','lifecycle'),{recursive:true});await writeFile(observerFile,'running');
 const watch=String.raw`
Add-Type 'using System;using System.Text;using System.Runtime.InteropServices;public static class DrawWindows {public delegate bool Callback(IntPtr w,IntPtr p);[DllImport("user32.dll")]public static extern bool EnumWindows(Callback c,IntPtr p);[DllImport("user32.dll")]public static extern bool IsWindowVisible(IntPtr w);[DllImport("user32.dll")]public static extern uint GetWindowThreadProcessId(IntPtr w,out uint p);[DllImport("user32.dll",CharSet=CharSet.Unicode)]public static extern int GetWindowText(IntPtr w,StringBuilder s,int n);}'
$roots=@{};$visible=[Collections.Generic.List[object]]::new();$clock=[Diagnostics.Stopwatch]::StartNew();$stateFile=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('__STATE_PATH__'))
while((Get-Content -LiteralPath $stateFile -Raw).Trim() -ne 'finished' -and $clock.Elapsed.TotalSeconds -lt 100){
 $owners=@(Get-CimInstance Win32_Process | Where-Object {$_.ParentProcessId -eq __NODE_PID__ -and $_.Name -eq 'chrome.exe'})
 foreach($owner in $owners){if($owner.CommandLine -or -not $roots.ContainsKey($owner.ProcessId)){$roots[$owner.ProcessId]=@{pid=$owner.ProcessId;command=$owner.CommandLine}}}
 [DrawWindows]::EnumWindows({param($w,$p) $id=[uint32]0;[void][DrawWindows]::GetWindowThreadProcessId($w,[ref]$id);if($roots.ContainsKey([uint32]$id) -and [DrawWindows]::IsWindowVisible($w)){$title=[Text.StringBuilder]::new(256);[void][DrawWindows]::GetWindowText($w,$title,256);$visible.Add(@{pid=$id;title=$title.ToString()})};return $true},[IntPtr]::Zero)|Out-Null
 Start-Sleep -Milliseconds 250
}
@{roots=@($roots.Values);visible=@($visible.ToArray())}|ConvertTo-Json -Depth 5 -Compress
`.replace('__NODE_PID__',String(process.pid)).replace('__STATE_PATH__',Buffer.from(observerFile).toString('base64'));
 const monitor=spawn(powershell,['-Command',watch],{windowsHide:true});let stdout='',stderr='';monitor.stdout.on('data',b=>stdout+=b);monitor.stderr.on('data',b=>stderr+=b);const monitorDone=new Promise((done,fail)=>{monitor.once('error',fail);monitor.once('close',code=>code===0?done():fail(Error(stderr)));});
 const previous=process.env.VAC_DRAW_BROWSER;process.env.VAC_DRAW_BROWSER=chrome;
 let failure;
 try{
  const base=join(process.env.VAC_TEST_OUTPUT,'draw','lifecycle');
  const c=composition('remotion');c.content_version=createHash('sha256').update('lifecycle-success-'+Date.now()).digest('hex');
  await renderDraw({composition:c,output_dir:join(base,'success')});
  const bad={...c,source:'export default function Scene(){throw new Error("intentional lifecycle failure");}',content_version:createHash('sha256').update('lifecycle-failure').digest('hex')};
  await assert.rejects(renderDraw({composition:bad,output_dir:join(base,'failure')}),/intentional lifecycle failure/);
 }catch(error){failure=error;}finally{if(previous===undefined)delete process.env.VAC_DRAW_BROWSER;else process.env.VAC_DRAW_BROWSER=previous;await writeFile(observerFile,"finished");await monitorDone;}
 const audit=JSON.parse(stdout.trim());await writeFile(join(process.env.VAC_TEST_OUTPUT,'draw','lifecycle','verification.json'),JSON.stringify({audit,stage:'observed_before_assertions',success_and_failure:!failure},null,2));assert(audit.roots.length>=2,'observer must witness both success and failure browser processes');assert.deepEqual(audit.visible,[],'background render must never create a visible task-switcher window');
 for(const root of audit.roots){assert.match(root.command,/--headless=new/);assert(!/--headless=old/.test(root.command));}
 const remaining=JSON.parse(execFileSync(powershell,['-Command',`$ids=@(${audit.roots.map(r=>r.pid).join(',')});ConvertTo-Json -InputObject @(Get-Process -Id $ids -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id) -Compress`],{encoding:'utf8',windowsHide:true}).trim());assert.deepEqual(remaining,[],'every witnessed browser must exit');
 await writeFile(join(process.env.VAC_TEST_OUTPUT,'draw','lifecycle','verification.json'),JSON.stringify({audit,remaining,success_and_failure:!failure},null,2));
 if(failure)throw failure;
});
