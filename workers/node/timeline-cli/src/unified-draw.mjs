import {readFile, writeFile, mkdir, mkdtemp, rm, stat, rename} from 'node:fs/promises';
import {createHash} from 'node:crypto';
import {createRequire} from 'node:module';
import {dirname, join, resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {createServer} from 'node:http';
import {build} from 'esbuild';
import puppeteer from 'puppeteer-core';
import {guardPage} from './draw-network.mjs';
import {prepareImages} from './draw-images.mjs';
import {runTool,probeMedia} from './media.mjs';
import {drawFrameContext} from './draw-time.mjs';
const require=createRequire(import.meta.url), directory=dirname(fileURLToPath(import.meta.url));
const hash=value=>createHash('sha256').update(value).digest('hex');
const script=value=>value.replace(/<\/script/gi,'<\\/script');
function sourceBindings(document,source) {
  const bound=new Map((source.image_bindings??[]).map(im=>[im.id,im]));
  for(const n of document.nodes.filter(n=>n.kind==='image')) {
    const first=n.keyframes?.[0],last=n.keyframes?.at(-1);
    bound.set(n.id,{...bound.get(n.id),id:n.id,resource_id:n.resource_id,resource_sha256:n.resource_sha256??'0'.repeat(64),x:n.transform?.x??0,y:n.transform?.y??0,width:n.transform?.width||document.canvas.width,height:n.transform?.height||document.canvas.height,dx:first&&last?last.x-first.x:0,dy:first&&last?last.y-first.y:0,from_frame:first?.frame??0,to_frame:last?.frame??0,fit:n.fit??'contain',opacity:n.opacity??1,mode:n.hidden?'source':'layer'});
  }
  return [...bound.values()];
}
export async function validateUnified(document) {
  if(document?.schema_version!==1 || !Array.isArray(document.nodes) || !Array.isArray(document.sources) || document.nodes.length>128 || document.sources.length>32)throw Error('Invalid DrawDocument');
  const {validateDraw,checkDraw}=await import('./draw-runtime.mjs');
  const spec={...document.canvas,source:' ',engine:'remotion',duration_frames:document.duration_frames};validateDraw(spec);
  const ids=new Set();
  for(const source of document.sources){
    if(!source.id || ids.has(source.id))throw Error('Duplicate or missing source ID');ids.add(source.id);
    await checkDraw({composition:{...spec,...source,...source.canvas,duration_frames:source.duration_frames??document.duration_frames,images:sourceBindings(document,source)},deterministic:true});
  }
  const nodes=new Map();
  for(const node of document.nodes){
    if(!node.id || nodes.has(node.id) || !['source','image','text','group','mask','shape'].includes(node.kind) || !Number.isInteger(node.from) || node.from<0 || !Number.isInteger(node.duration) || node.duration<1)throw Error('Invalid drawing layer');
    if(node.kind==='source'&&!ids.has(node.source_id))throw Error('Missing drawing source');
    if(node.speed && (![node.speed.num,node.speed.den].every(n=>Number.isInteger(n)&&n>0)))throw Error('Invalid layer speed');
    if(node.opacity!==undefined && (!Number.isFinite(node.opacity)||node.opacity<0||node.opacity>1))throw Error('Invalid opacity');
    for(const number of Object.values(node.transform??{}))if(!Number.isFinite(number))throw Error('Invalid transform');
    nodes.set(node.id,node);
  }
  for(const node of nodes.values()){
    const seen=new Set([node.id]);let parent=node.parent_id;
    while(parent){if(seen.has(parent))throw Error('Drawing graph contains a cycle');seen.add(parent);const target=nodes.get(parent);if(!target||!['group','mask'].includes(target.kind))throw Error('Missing layer group');parent=target.parent_id;}
  }
  return spec;
}
function htmlSource(source,gsap,runtime,bindings,images,globals,origin) {
  const byID=Object.fromEntries((bindings??[]).map(im=>[im.id,{...im,src:images[im.resource_id]}]));
  source=source.replace(/(\b(?:src|href|poster)\s*=\s*["'])vac-image:([^"']+)(["'])/gi,(_,start,id,end)=>{
    const image=byID[decodeURIComponent(id)];if(!image?.src)throw Error('Missing bound source image');return start+image.src+end;
  });
  const bootstrap=`<meta http-equiv="Content-Security-Policy" content="connect-src 'none'; img-src data: blob: ${origin??''}; media-src 'none'; font-src data:; frame-src 'none'; object-src 'none'; form-action 'none'"><script>window.__timelines={};window.vacImages=${JSON.stringify(byID).replaceAll('<','\\u003c')};window.vacImages=new Proxy(window.vacImages,{get(t,k,r){if(typeof k!=='string'||k==='then'||k==='toJSON'||k in t)return Reflect.get(t,k,r);window.__vacImageError='Image alias '+String(k)+' is not bound to an imported resource';throw Error(window.__vacImageError)}});window.vac=JSON.parse(${JSON.stringify(JSON.stringify(globals)).replaceAll('<','\\u003c')});window.vac.random=function(key){let h=2166136261;for(const c of String(window.vac.seed)+':'+String(key))h=Math.imul(h^c.charCodeAt(0),16777619);return (h>>>0)/4294967296;};</script><script>${script(gsap)}</script>`;
  const bridge=`<script>${script(runtime)}</script><script>
addEventListener('message',async event=>{
 if(event.source!==parent||event.data?.vac!=='seek')return;
 const {key,frame,fps}=event.data;
 try{
  if(window.__vacImageError)throw Error(window.__vacImageError);
  const started=performance.now();while(!window.__renderReady||!window.__player){if(performance.now()-started>10000)throw Error('HyperFrames runtime not ready');await new Promise(r=>setTimeout(r,10));}
  // The pinned native runtime otherwise snaps to its default 30 fps grid.
  await Promise.resolve(window.__player.renderSeek(frame/fps,{exact:true}));
  if(typeof window.vacFrame==='function')await window.vacFrame({frame,fps,variables:window.vac.variables,theme:window.vac.theme,seed:window.vac.seed});
  await document.fonts.ready;await Promise.all([...document.images].map(i=>i.decode()));
  await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
  parent.postMessage({vac:'frame-ready',key},'*');
 }catch(e){parent.postMessage({vac:'frame-ready',key,error:String(e)},'*');}
});</script>`;
  // Runtime bundles contain replacement tokens such as $&; never interpolate
  // them through String.replace's replacement-string grammar.
  return source.replace(/<head([^>]*)>/i,(_,attributes)=>`<head${attributes}>${bootstrap}`).replace(/<\/body>/i,()=>bridge+'</body>');
}
/** Owned, persistent frame adapter. Caller must close; it never downloads a browser. */
export async function openUnifiedDraw({document,assets=[],output_dir,browser_path,on_state=()=>{}}) {
  on_state('compiling');
  await validateUnified(document);
  const root=resolve(output_dir);await mkdir(root,{recursive:true});
  on_state('loading');
  const images={},imageFiles=new Map();
  const digest=async path=>hash(await readFile(path));
  for(const node of document.nodes.filter(n=>n.kind==='image')){
    const asset=assets.find(a=>a.resource_id===node.resource_id);
    if(!asset || node.resource_sha256 && node.resource_sha256!==(asset.binding_sha256??asset.sha256))throw Error('Image binding changed: '+node.resource_id);
  }
  // Full-resolution RAW images can exceed the DevTools message limit when
  // duplicated as data URIs in HTML plus React inputs. Serve only prepared,
  // hash-checked owned files and send short URLs through the frame protocol.
  for(const asset of await prepareImages(assets,root,digest)){const url='/images/'+hash(asset.resource_id+':'+asset.prepared_sha256)+'.png';images[asset.resource_id]=url;imageFiles.set(url,asset.path);}
  for(const node of document.nodes)if(node.kind==='image'&&!images[node.resource_id])throw Error('Missing project image '+node.resource_id);
  let gsap='',runtime='';
  let seekSerial=0,currentDocument=structuredClone(document),html={},htmlHash='',resourcesDirty=true,browser,page,server,origin='',closed=false,serial=Promise.resolve();
  const reactSources=new Map(),compileCounts=new Map();
  const lifecycle=[];const trace=(type,message='')=>{lifecycle.push({type,message:String(message),at:Date.now()});if(lifecycle.length>12)lifecycle.shift();};
  const install=async(id,code)=>{await page.addScriptTag({content:code});await page.evaluate(id=>window.vacInstallSource(id),id);};
  async function compile(d){
    const needed=new Set(d.nodes.filter(n=>n.kind==='source').map(n=>n.source_id));
    const htmlSources=d.sources.filter(s=>s.engine==='hyperframes'&&needed.has(s.id));
    if(htmlSources.length&&!runtime){gsap=await readFile(require.resolve('gsap/dist/gsap.min.js'),'utf8');runtime=await readFile(join(dirname(require.resolve('hyperframes/package.json')),'dist/hyperframe.runtime.iife.js'),'utf8');}
    html=Object.fromEntries(htmlSources.map(s=>[s.id,htmlSource(s.source,gsap,runtime,sourceBindings(d,s),images,{variables:d.variables??{},theme:d.theme??{},seed:d.seed??0},origin)]));
    const generated=hash(JSON.stringify(html));if(generated!==htmlHash){htmlHash=generated;resourcesDirty=true;}
    const react=d.sources.filter(s=>s.engine==='remotion');
    for(const id of reactSources.keys())if(!react.some(s=>s.id===id)){reactSources.delete(id);if(page)await page.evaluate(id=>window.vacRemoveSource(id),id);}
    for(const source of react){
      const signature=hash(source.source);if(reactSources.get(source.id)?.signature===signature)continue;
      on_state('compiling');
      const bundle=await build({stdin:{contents:source.source,loader:'tsx',resolveDir:join(directory,'..')},bundle:true,write:false,format:'iife',globalName:'VacSourceBuild',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"production"'},plugins:[{name:'shared-drawing-runtime',setup(b){
        b.onResolve({filter:/^(react(?:\/jsx-runtime)?|remotion)$/},args=>({path:args.path,namespace:'shared'}));
        b.onLoad({filter:/.*/,namespace:'shared'},args=>({contents:'module.exports=window.'+({'react':'__vacReact','react/jsx-runtime':'__vacJSX',remotion:'__vacRemotion'}[args.path]),loader:'js'}));
      }}]});
      const code=bundle.outputFiles[0].text;reactSources.set(source.id,{signature,code});compileCounts.set(source.id,(compileCounts.get(source.id)??0)+1);if(page)await install(source.id,code);
    }
  }
  try{
    await compile(document);
    const bundle=await build({entryPoints:[join(directory,'unified-scene.jsx')],bundle:true,write:false,format:'iife',platform:'browser',jsx:'automatic',define:{'process.env.NODE_ENV':'"production"'}});
    await writeFile(join(root,'scene.js'),bundle.outputFiles[0].contents);
    server=createServer(async(req,res)=>{try{if(req.method!=='GET'){res.statusCode=405;res.end();return;}if(imageFiles.has(req.url)){res.setHeader('Content-Type','image/png');res.setHeader('Access-Control-Allow-Origin','*');res.setHeader('Cache-Control','public,max-age=31536000,immutable');res.end(await readFile(imageFiles.get(req.url)));}else if(req.url==='/scene.js'){res.setHeader('Content-Type','application/javascript');res.end(await readFile(join(root,'scene.js')));}else if(req.url==='/'){res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><head><style>html,body{margin:0;background:transparent;overflow:hidden}</style></head><body><div id="root"></div><script src="/scene.js"></script></body></html>');}else{res.statusCode=404;res.end();}}catch{res.statusCode=500;res.end();}});
    await new Promise((done,fail)=>{server.once('error',fail);server.listen(0,'127.0.0.1',done);});
    origin='http://127.0.0.1:'+server.address().port;
    for(const id of Object.keys(images))images[id]=origin+images[id];await compile(document);
    const {drawBrowser}=await import('./draw-runtime.mjs');
    browser=await puppeteer.launch({executablePath:browser_path??drawBrowser('unified'),headless:true,args:['--disable-background-timer-throttling','--disable-renderer-backgrounding','--force-color-profile=srgb']});
    browser.on('disconnected',()=>trace('disconnected'));
    browser.process()?.on('exit',(code,signal)=>trace('browser-exit',JSON.stringify({code,signal})));
    browser.process()?.stderr?.on('data',chunk=>trace('browser-stderr',String(chunk).slice(-1600)));
    page=await guardPage(await browser.newPage(),{origin});
    page.on('close',()=>trace('page-closed'));page.on('error',error=>trace('page-crashed',error.message));
    page.on('pageerror',error=>process.stderr.write('[unified browser] '+error.message+'\n'));
    page.on('console',message=>{if(message.type()==='error')process.stderr.write('[unified console] '+message.text()+'\n');});
    await page.setViewport({width:document.canvas.width,height:document.canvas.height,deviceScaleFactor:1});
    await page.goto(origin,{waitUntil:'load'});await page.waitForFunction('window.vacReady===true');
    for(const [id,source] of reactSources)await install(id,source.code);
  }catch(error){try{await browser?.close();}finally{server?.close();}throw error;}
  const enqueue=action=>{const result=serial.then(()=>{if(closed)throw Error('Frame adapter disposed');return action();});serial=result.catch(()=>{});return result;};
  return {
    diagnostics:()=>({source_compiles:Object.fromEntries(compileCounts),host_loads:1,asset_transport:'owned-http',resource_manifest_bytes:Buffer.byteLength(JSON.stringify({assets:images,html})),browser_connected:browser.connected,page_closed:page.isClosed(),lifecycle}),
    seek:request=>enqueue(async()=>{
      on_state('updating');
      if(!Number.isInteger(request.frame)||request.frame<0||request.frame>=currentDocument.duration_frames)throw Error('Frame outside drawing');
      const key=(request.request_id??'frame')+':'+(++seekSerial);
      const scale=Math.min(1,Math.max(.1,request.scale??1));
      await page.setViewport({width:Math.ceil(currentDocument.canvas.width*scale),height:Math.ceil(currentDocument.canvas.height*scale),deviceScaleFactor:1});
      await page.evaluate(scale=>{const root=document.getElementById('root');root.style.transform=`scale(${scale})`;root.style.transformOrigin='top left';},scale);
      const displayDocument={...currentDocument,sources:currentDocument.sources.map(({source,...s})=>s),asset_bindings:undefined};
      const bounds=await page.evaluate(snapshot=>window.vacSeek(snapshot),{document:displayDocument,frame:request.frame,key,...(resourcesDirty?{resources:{assets:images,html}}:{})});
      resourcesDirty=false;
      const geometry=bounds.map(b=>({...b,x:b.x/scale,y:b.y/scale,width:b.width/scale,height:b.height/scale}));
      const png=Buffer.from(await page.screenshot({type:'png',omitBackground:true}));
      const times=new Map(),active=n=>drawFrameContext(currentDocument,n.id,request.frame,times).active;
      const used=new Set(currentDocument.nodes.filter(n=>active(n)&&n.kind==='source').map(n=>n.source_id));
      const engines=[...new Set(currentDocument.sources.filter(s=>used.has(s.id)).map(s=>s.engine))].sort();
      return {frame:request.frame,revision:request.revision,request_id:request.request_id,session:request.session,content_version:request.content_version,png,geometry,engines,...(engines.includes('hyperframes')?{hyperframes_protocol:'native-player-renderSeek'}:{})};
    }),
    update:d=>enqueue(async()=>{await validateUnified(d);await compile(d);await page.setViewport({width:d.canvas.width,height:d.canvas.height,deviceScaleFactor:1});currentDocument=structuredClone(d);}),
    close:async()=>{await serial;closed=true;trace('dispose');try{await browser.close();}finally{await new Promise(done=>server.close(done));}}
  };
}

// The encoder sees one final RGBA frame sequence. No per-engine H.264 intermediates.
export async function renderUnifiedDraw(input) {
  const c=input.composition,d=c.document;await validateUnified(d);
  const output=resolve(input.output_dir);await mkdir(output,{recursive:true});
  const format=input.format??'mp4';
  if(!['mp4','webm-alpha','prores4444'].includes(format))throw Error('Unsupported drawing output format');
  const filename=format==='mp4'?'program.mp4':format==='webm-alpha'?'program.webm':'program.mov';
  const stage=await mkdtemp(join(output,'.unified-'));let host;
  try{
    host=await openUnifiedDraw({document:d,assets:input.assets,output_dir:stage});
    for(let frame=0;frame<d.duration_frames;frame++){
      const result=await host.seek({frame,revision:c.revision,content_version:c.content_version,request_id:'export-'+frame});
      await writeFile(join(stage,String(frame).padStart(6,'0')+'.png'),result.png);
    }
    await host.close();host=null;
    const codec=format==='mp4'?['-c:v','libx264','-crf','18','-pix_fmt','yuv420p','-movflags','+faststart']:format==='webm-alpha'?['-c:v','libvpx-vp9','-pix_fmt','yuva420p','-auto-alt-ref','0','-crf','18','-b:v','0']:['-c:v','prores_ks','-profile:v','4','-pix_fmt','yuva444p10le'];
    await runTool('ffmpeg',['-v','error','-nostdin','-y','-framerate',`${d.canvas.fps.num}/${d.canvas.fps.den}`,'-i',join(stage,'%06d.png'),'-frames:v',String(d.duration_frames),...codec,'-threads','1',join(stage,filename)],{timeoutMs:300000});
    const media=await probeMedia(join(stage,filename));
    // WebM and ProRes often omit nb_frames; count decoded frames rather than
    // interpreting missing container bookkeeping as a successful/failed render.
    const counted=JSON.parse(await runTool('ffprobe',['-v','error','-count_frames','-select_streams','v:0','-show_entries','stream=nb_read_frames','-of','json',join(stage,filename)]));
    if(media.video?.width!==d.canvas.width||media.video?.height!==d.canvas.height||Number(counted.streams?.[0]?.nb_read_frames)!==d.duration_frames)throw Error('Unified output frame validation failed');
    const bytes=await readFile(join(stage,filename));
    const report={environment_version:input.environment_version??'',engine:'unified',runtime_version:'vac-unified-1',content_version:c.content_version,sha256:hash(bytes),size_bytes:(await stat(join(stage,filename))).size,width:d.canvas.width,height:d.canvas.height,frames:d.duration_frames,duration:d.duration_frames*d.canvas.fps.den/d.canvas.fps.num,format,alpha:format==='mp4'?'opaque':'straight',color_space:'srgb',file:filename};
    await rename(join(stage,filename),join(output,filename));await writeFile(join(output,'program.json.partial'),JSON.stringify(report));await rename(join(output,'program.json.partial'),join(output,'program.json'));
    return report;
  }finally{try{await host?.close();}finally{await rm(stage,{recursive:true,force:true});}}
}

/** FrameSource used by sequence composition: editable source -> lossless RGBA
 * frames, with its own rational source clock. Final sequence encoding happens
 * once after footage, audio, subtitles and drawing pixels are composed. */
export async function prepareDrawFrameSource({document,assets=[],output_dir}) {
  await mkdir(output_dir,{recursive:true});
  const host=await openUnifiedDraw({document,assets,output_dir});
  try{
    for(let frame=0;frame<document.duration_frames;frame++){
      const result=await host.seek({frame,request_id:'source-'+frame});
      await writeFile(join(output_dir,String(frame).padStart(6,'0')+'.png'),result.png);
    }
  }finally{await host.close();}
  return join(output_dir,'%06d.png');
}

export async function renderDrawSourceFrame(input) {
  const host=await openUnifiedDraw(input);
  try{const frame=await host.seek({frame:input.frame,scale:input.scale,request_id:'thumbnail'});await writeFile(join(input.output_dir,'frame.png'),frame.png);return {frame:input.frame,sha256:hash(frame.png)};}
  finally{await host.close();}
}
