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
import {prepareImages} from '../src/draw-images.mjs';
const html='<!doctype html><html><head><style>body{margin:0;background:#101820}#root{position:relative;width:320px;height:180px;overflow:hidden}#box{position:absolute;top:60px;width:40px;height:40px;background:#ff8800}</style></head><body><div id="root" data-composition-id="root" data-width="320" data-height="180" data-duration="1"><div id="box"></div><span style="color:white">VAC draw</span></div><script>const tl=gsap.timeline({paused:true});tl.to("#box",{x:200,duration:1,ease:"none"},0);window.__timelines={root:tl};</script></body></html>';
const react='import React from "react";import {AbsoluteFill,useCurrentFrame} from "remotion";export default function Scene(){const frame=useCurrentFrame();return <AbsoluteFill style={{background:"#101820"}}><div style={{position:"absolute",top:60,left:frame*6,width:40,height:40,background:"#ff8800"}}/><span style={{color:"white"}}>VAC draw</span></AbsoluteFill>}';
export const composition=(engine='hyperframes')=>{const source=engine==='hyperframes'?html:react;return {id:'drawing',name:'Draw test',engine,source,width:320,height:180,fps:{num:30,den:1},duration_frames:30,revision:1,content_version:createHash('sha256').update(source).digest('hex')};};

test('imported JPEG prepares without transcoding the original and preserves literal percent filenames',async()=>{
 assert(process.env.VAC_TEST_OUTPUT,'Use TestWorkspace');
 const root=join(process.env.VAC_TEST_OUTPUT,'draw','image-compat');await mkdir(root,{recursive:true});
 for(const extension of ['jpg','jpeg','jpe','jepg','png','webp','bmp','tif','tiff']){
  const path=join(root,'photo%03d.'+extension);
  await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=lime:s=32x24','-frames:v','1','-threads','1',...(extension==='jepg'?['-c:v','mjpeg']:[]),'-f','image2','-update','1',path]);
  const digest=async file=>createHash('sha256').update(await readFile(file)).digest('hex'),sha256=await digest(path);
  const [image]=await prepareImages([{resource_id:'photo-'+extension,path,sha256}],join(root,extension),digest);
  assert.equal(await digest(path),sha256,'preparing must preserve the original');
  const stats=await runTool('ffmpeg',['-v','error','-i',image.path,'-frames:v','1','-vf','signalstats,metadata=print:file=-','-f','null','-']);
  assert(Number(stats.match(/YAVG=([\d.]+)/)?.[1])>120,'prepared image must retain the bright green pixels');
 }
});
test('draw check validates source, dimensions, explicit modules and timebase without running code',async()=>{
 for(const engine of ['hyperframes','remotion'])assert.equal((await checkDraw({composition:composition(engine)})).valid,true);
 await assert.rejects(checkDraw({composition:{...composition(),width:321}}),/specification/);
 await assert.rejects(checkDraw({composition:{...composition(),fps:{num:-30,den:-1}}}),/specification/);
 await assert.rejects(checkDraw({composition:{...composition(),source:html.replace('data-duration="1"','data-duration="invalid"')}}),/data-duration/);

 await assert.rejects(checkDraw({composition:{...composition('remotion'),source:'import fs from "node:fs";export default ()=>null;'}}),/Only react/);
 await assert.rejects(checkDraw({composition:{...composition('remotion'),source:'export default ()=>{fetch("http://127.0.0.1");return null;}'}}),/Unavailable/);
 await assert.rejects(checkDraw({composition:{...composition(),source:html.replace('<head>','<head><script src="https://example.com/x.js"></script>')}}),/resources/);
});

test('image source fallbacks cannot conceal an unbound photo alias',async()=>{
 const c={...composition('remotion'),source:'import React from "react";export default function Scene({images}){return <div>{images?.water?.src||"gradient fallback"}</div>}'};
 await assert.rejects(checkDraw({composition:c}),/water.*not bound|not bound.*water/i);
 c.images=[{id:'water',resource_id:'photo',resource_sha256:'a'.repeat(64),x:0,y:0,width:320,height:180,dx:0,dy:0,from_frame:0,to_frame:0,mode:'source'}];
 assert.equal((await checkDraw({composition:c})).valid,true);
});

for(const engine of ['remotion','hyperframes'])test('dynamic image alias cannot silently fall back when previewing '+engine, {timeout:60000}, async()=>{
 const {openUnifiedDraw}=await import('../src/unified-draw.mjs');
 const root=join(process.env.VAC_TEST_OUTPUT,'draw','dynamic-image-alias',engine);await mkdir(root,{recursive:true});
 const source=engine==='remotion'?'import React from "react";export default function Scene({images}){const name="missing";return <div>{images[name]?.src||"fallback"}</div>}':'<!doctype html><html><head></head><body><div id="root" data-composition-id="scene" data-width="320" data-height="180" data-duration="0.03333333333333333"></div><script>const name="missing";window.vacImages[name]?.src;const tl=gsap.timeline({paused:true});tl.to({},{duration:1/30});window.__timelines={scene:tl};</script></body></html>';
 const document={schema_version:1,canvas:{width:320,height:180,fps:{num:30,den:1}},duration_frames:1,preset:'auto',
  sources:[{id:'source',engine,source}],nodes:[{id:'scene',kind:'source',source_id:'source',from:0,duration:1}]};
 const host=await openUnifiedDraw({document,assets:[],output_dir:root});
 try{await assert.rejects(host.seek({frame:0,revision:'missing',request_id:'missing'}),/missing.*not bound|not bound.*missing/i);}finally{await host.close();}
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

test('unified frame host seeks native HyperFrames, React and imported images together, including alpha and reverse seeks',{timeout:180000},async()=>{
 const {openUnifiedDraw}=await import('../src/unified-draw.mjs');
 const output=join(process.env.VAC_TEST_OUTPUT,'draw','unified');await mkdir(output,{recursive:true});
 const image=join(output,'background.png');
 await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=0x204020:s=320x180','-frames:v','1','-pix_fmt','rgba',image]);
 const bytes=await readFile(image),sha=createHash('sha256').update(bytes).digest('hex');
 const document={schema_version:1,canvas:{width:320,height:180,fps:{num:30,den:1}},duration_frames:30,preset:'auto',sources:[
  {id:'html',engine:'hyperframes',source:'<!doctype html><html><head><style>body{margin:0;background:transparent}#root{position:relative;width:320px;height:180px}#box{position:absolute;left:0;top:30px;width:40px;height:40px;background:#ff0000}#badge{position:absolute;left:280px;top:10px;width:20px;height:20px;background:white}</style></head><body><div id="root" data-composition-id="html" data-width="320" data-height="180" data-duration="1"><div id="box" class="clip" data-start="0" data-duration="1"></div><div id="badge" class="clip" data-start="0.2" data-duration="0.4"></div></div><script>const tl=gsap.timeline({paused:true});tl.to("#box",{x:200,duration:1,ease:"none"});window.__timelines.html=tl;</script></body></html>'},
  {id:'react',engine:'remotion',source:'import React from "react";import {useCurrentFrame} from "remotion";export default function Chart(){const f=useCurrentFrame();return <div style={{position:"absolute",left:40,top:100,width:10+f*2,height:30,background:"rgba(0,0,255,0.5)"}}/>;}'}
 ],nodes:[
  {id:'image',kind:'image',resource_id:'source-image',from:0,duration:30,transform:{x:0,y:0,width:320,height:180}},
  {id:'web',kind:'source',source_id:'html',from:0,duration:30},
  {id:'chart',kind:'source',source_id:'react',from:0,duration:30}
 ]};
 const host=await openUnifiedDraw({document,assets:[{resource_id:'source-image',path:image,sha256:sha}],output_dir:output});
 const hashes=new Map(),frames=[];
 const rgba=png=>execFileSync('ffmpeg',['-v','error','-i','pipe:0','-f','rawvideo','-pix_fmt','rgba','pipe:1'],{input:png,windowsHide:true,maxBuffer:320*180*4+1024});
 const pixel=(data,x,y)=>[...data.subarray((y*320+x)*4,(y*320+x)*4+4)];
 try{
  for(const frame of [0,15,29,2,15,0]){
   const result=await host.seek({frame,revision:'first',request_id:'seek-'+frames.length});
   assert.equal(result.frame,frame);assert.equal(result.revision,'first');assert.equal(result.request_id,'seek-'+frames.length);
   assert.deepEqual(result.engines,['hyperframes','remotion']);
   assert.equal(result.hyperframes_protocol,'native-player-renderSeek');
   const digest=createHash('sha256').update(result.png).digest('hex');if(hashes.has(frame))assert.equal(digest,hashes.get(frame),'same requested frame must reproduce after reverse/random seeks');hashes.set(frame,digest);
   const data=rgba(result.png);frames.push({frame,sha256:digest});
   if(frame===0){assert(pixel(data,10,40)[0]>220);assert(pixel(data,65,110)[2]<80);assert(pixel(data,285,15)[0]<80);}
   if(frame===15){assert(pixel(data,110,40)[0]>220);assert(pixel(data,10,40)[0]<80);assert(pixel(data,65,110)[2]>120);assert(pixel(data,285,15)[0]>220);}
   if(frame===29)assert(pixel(data,285,15)[0]<80);
   await writeFile(join(output,'frame-'+frame+'.png'),result.png);
  }
  await host.update({...document,nodes:document.nodes.filter(n=>n.id!=='image')});
  const transparent=await host.seek({frame:15,revision:'transparent',request_id:'alpha'}),data=rgba(transparent.png);
  assert.equal(pixel(data,5,170)[3],0,'background must remain transparent');
  assert(Math.abs(pixel(data,65,110)[3]-128)<=1,'React foreground alpha must survive the shared canvas');
  await writeFile(join(output,'transparent.png'),transparent.png);
  await writeFile(join(output,'verification.json'),JSON.stringify({frames,alpha:{clear:pixel(data,5,170),foreground:pixel(data,65,110)},engines:transparent.engines,native_hyperframes:true,human_acceptance:'not_checked'},null,2));
  const native24={...document,sources:[{...document.sources[0],canvas:{width:320,height:180,fps:{num:24,den:1}},duration_frames:24}],nodes:[{id:'native24',kind:'source',source_id:'html',from:0,duration:30}]};
  await host.update(native24);const exact=await host.seek({frame:2,request_id:'native24'}),mapped=rgba(exact.png);
  assert(pixel(mapped,47,40)[0]>220,'native 24fps source must seek exact 1/24 second without snapping to its runtime default 30fps');
  await host.update({...document,nodes:[{id:'group',kind:'group',from:2,duration:10,speed:{num:2,den:1},transform:{x:10,y:0,width:320,height:180}},{id:'child',kind:'source',source_id:'html',parent_id:'group',from:0,duration:20}]});
  const grouped=rgba((await host.seek({frame:3,request_id:'group'})).png);assert(pixel(grouped,26,40)[0]>220,'source must seek its nested group local frame, not the global frame');
  await host.update({...document,nodes:[{id:'mask',kind:'mask',shape:'ellipse',from:0,duration:30,transform:{x:0,y:20,width:60,height:60}},{id:'masked-html',kind:'source',source_id:'html',parent_id:'mask',from:0,duration:30},{id:'masked-react',kind:'source',source_id:'react',parent_id:'mask',from:0,duration:30,transform:{x:0,y:-80}}]});
  const maskFrame=await host.seek({frame:0,request_id:'cross-mask'}),masked=rgba(maskFrame.png);assert(pixel(masked,20,55)[0]>220);assert.equal(pixel(masked,2,76)[3],0,'mask must clip native HTML pixels across its document boundary');assert(Math.abs(pixel(masked,45,50)[3]-128)<=1,'React alpha must survive the shared cross-source mask');await writeFile(join(output,'cross-source-mask.png'),maskFrame.png);
  const initial=host.diagnostics();
  const changed=structuredClone(document);changed.sources.push({id:'unused-react',engine:'remotion',source:'export default ()=>null;'});
  await host.update(changed);const added=host.diagnostics();assert.equal(added.source_compiles.react,initial.source_compiles.react);
  changed.sources[1].source=changed.sources[1].source.replace('0,0,255','0,255,255');await host.update(changed);
  const updated=host.diagnostics();assert.equal(updated.source_compiles.react,added.source_compiles.react+1);assert.equal(updated.source_compiles['unused-react'],added.source_compiles['unused-react']);assert.equal(updated.host_loads,1,'source edits must not reload unaffected native iframe/runtime contexts');
  const edited=rgba((await host.seek({frame:15,request_id:'one-source-edit'})).png);assert(pixel(edited,65,110)[1]>120,'changed React source must update actual pixels');
  changed.nodes[0].hidden=true;changed.sources[0].source=changed.sources[0].source.replace('</body>','<img src="vac-image:image" style="position:absolute;left:0;top:150px;width:20px;height:20px"/></body>');await host.update(changed);
  const referenced=rgba((await host.seek({frame:15,request_id:'owned-image'})).png);assert.equal(pixel(referenced,10,160)[3],255,'opaque-origin HTML must resolve an owned image URL');assert(pixel(referenced,10,160)[1]>50);
  assert.equal(host.diagnostics().asset_transport,'owned-http');assert(host.diagnostics().resource_manifest_bytes<2_000_000,'full image bytes must never be duplicated into CDP frame messages');

 }finally{await host.close();}
});

test('unified renderer shares preview pixels and exports actual alpha, while pure source scenes keep their native appearance',{timeout:180000},async()=>{
 const {openUnifiedDraw,renderUnifiedDraw}=await import('../src/unified-draw.mjs');
 const base=join(process.env.VAC_TEST_OUTPUT,'draw','unified-export');await mkdir(base,{recursive:true});
 const document={schema_version:1,canvas:{width:320,height:180,fps:{num:30,den:1}},duration_frames:6,preset:'auto',sources:[{id:'box',engine:'remotion',source:'import React from "react";import {useCurrentFrame} from "remotion";export default function Scene(){return <div style={{position:"absolute",left:20+useCurrentFrame()*10,top:40,width:50,height:50,background:"rgba(0,80,255,.5)"}}/>;}'}],nodes:[{id:'box',kind:'source',source_id:'box',from:0,duration:6}]};
 const c={id:'export',name:'Export',engine:'unified',document,revision:1,content_version:createHash('sha256').update(JSON.stringify(document)).digest('hex')};
 const report=await renderUnifiedDraw({composition:c,output_dir:base,format:'webm-alpha'});assert.equal(report.frames,6);assert.equal(report.alpha,'straight');
 const decoded=execFileSync('ffmpeg',['-v','error','-c:v','libvpx-vp9','-i',join(base,'program.webm'),'-frames:v','1','-f','rawvideo','-pix_fmt','rgba','pipe:1'],{windowsHide:true,maxBuffer:320*180*4+1024});
 assert.equal(decoded[(170*320+5)*4+3],0);assert(Math.abs(decoded[(50*320+30)*4+3]-128)<=2);
 const prores=await renderUnifiedDraw({composition:c,output_dir:base,format:'prores4444'});assert.equal(prores.frames,6);assert.equal(prores.alpha,'straight');
 const rgbaProres=execFileSync('ffmpeg',['-v','error','-i',join(base,'program.mov'),'-frames:v','1','-f','rawvideo','-pix_fmt','rgba','pipe:1'],{windowsHide:true,maxBuffer:320*180*4+1024});
 assert.equal(rgbaProres[(170*320+5)*4+3],0);assert(Math.abs(rgbaProres[(50*320+30)*4+3]-128)<=2,'ProRes must preserve actual half-alpha pixels');
 for(const engine of ['hyperframes','remotion']){
  const legacy=composition(engine),d={schema_version:1,canvas:{width:320,height:180,fps:legacy.fps},duration_frames:30,preset:'auto',sources:[{id:'legacy',engine,source:legacy.source}],nodes:[{id:'legacy',kind:'source',source_id:'legacy',from:0,duration:30}]};
  const host=await openUnifiedDraw({document:d,output_dir:join(base,engine)});
  try{const frame=await host.seek({frame:0,request_id:engine,revision:'legacy'});const rgba=execFileSync('ffmpeg',['-v','error','-i','pipe:0','-f','rawvideo','-pix_fmt','rgba','pipe:1'],{input:frame.png,windowsHide:true,maxBuffer:320*180*4+1024});const at=(70*320+10)*4;assert(rgba[at]>220&&rgba[at+1]>90&&rgba[at+2]<30,'native orange scene must survive '+engine+' adapter');await writeFile(join(base,engine+'.png'),frame.png);}finally{await host.close();}
 }
 await writeFile(join(base,'verification.json'),JSON.stringify({report,alpha_clear:decoded[(170*320+5)*4+3],alpha_edge:decoded[(50*320+30)*4+3],prores,prores_alpha:rgbaProres[(50*320+30)*4+3],pure_engines:['hyperframes','remotion']},null,2));
});

test('unified source rejects independent clocks instead of silently accepting non-repeatable frames',async()=>{
 const {validateUnified}=await import('../src/unified-draw.mjs');
 const d={schema_version:1,canvas:{width:320,height:180,fps:{num:30,den:1}},duration_frames:30,preset:'auto',nodes:[],sources:[]};
 for(const expression of ['Date.now()','Math.random()','setTimeout(()=>{},10)','performance.now()']){
  const source={id:'clock',engine:'remotion',source:'import React from "react";export default function Scene(){const value='+expression+';return <div>{String(value)}</div>;}'};
  await assert.rejects(validateUnified({...d,sources:[source]}),/deterministic|clock|Unavailable draw API/);
 }
});

test('common shape masks preserve alpha without starting a source engine',{timeout:60000},async()=>{
 const {openUnifiedDraw}=await import('../src/unified-draw.mjs');
 const d={schema_version:1,canvas:{width:100,height:100,fps:{num:30,den:1}},duration_frames:30,preset:'auto',sources:[],nodes:[{id:'mask',kind:'mask',shape:'ellipse',from:0,duration:30,transform:{x:10,y:10,width:80,height:80}},{id:'fill',kind:'shape',shape:'rect',parent_id:'mask',color:'lime',from:0,duration:30,transform:{x:0,y:0,width:80,height:80}}]};
 const host=await openUnifiedDraw({document:d,output_dir:join(process.env.VAC_TEST_OUTPUT,'draw','mask')});
 try{const frame=await host.seek({frame:0,request_id:'mask'}),data=execFileSync('ffmpeg',['-v','error','-i','pipe:0','-f','rawvideo','-pix_fmt','rgba','pipe:1'],{input:frame.png,windowsHide:true,maxBuffer:41024});assert.equal(data[(12*100+12)*4+3],0);assert.equal(data[(50*100+50)*4+1],255);assert.deepEqual(frame.engines,[]);await writeFile(join(process.env.VAC_TEST_OUTPUT,'draw','mask','frame.png'),frame.png);}finally{await host.close();}
});

test('drawing instances use nested group clocks, rational source rates and half-open ranges',async()=>{
 const {drawFrameContext}=await import('../src/draw-time.mjs');
 const d={canvas:{fps:{num:30,den:1}},sources:[{id:'s',canvas:{fps:{num:24,den:1}}}],nodes:[{id:'g',kind:'group',from:2,duration:6,source_offset:1,speed:{num:2,den:1}},{id:'m',kind:'mask',parent_id:'g',from:3,duration:6,speed:{num:1,den:2}},{id:'v',kind:'source',parent_id:'m',source_id:'s',from:0,duration:6,source_offset:2,speed:{num:3,den:2}}]};
 assert.deepEqual(drawFrameContext(d,'v',4),{active:true,parent_frame:1,frame:3,fps:{num:24,den:1}});
 assert.equal(drawFrameContext(d,'v',1).active,false);assert.equal(drawFrameContext(d,'v',7).active,false);d.nodes[0].hidden=true;assert.equal(drawFrameContext(d,'v',4).active,false);
});
