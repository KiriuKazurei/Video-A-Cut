import {readFile,writeFile,mkdir,mkdtemp,rename,stat,lstat,realpath,rm} from 'node:fs/promises';
import {existsSync,createReadStream,readdirSync} from 'node:fs';
import {dirname,join,resolve,relative,isAbsolute,sep} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {createRequire} from 'node:module';
import {createHash} from 'node:crypto';
import {createServer} from 'node:http';
import {spawn} from 'node:child_process';
import {homedir} from 'node:os';
import {transform} from 'esbuild';
import {parse} from 'acorn';
import {runTool,probeMedia} from './media.mjs';
import {guardRemotion} from './draw-network.mjs';
import {imageLayers,isImageReference,checkedImageAssets,prepareImages,installImages,hyperframesImageSource,remotionImageEntry} from './draw-images.mjs';
const require=createRequire(import.meta.url), runtimeRoot=resolve(dirname(fileURLToPath(import.meta.url)),'..');
export const drawVersions={hyperframes:'0.8.193',remotion:'4.0.538'};
export function drawBrowser(engine){
 const explicit=process.env.VAC_DRAW_BROWSER;
 if(explicit){if(!existsSync(explicit))throw Error('Configured draw browser does not exist: '+explicit);return explicit;}
 // Windows GUI browsers do not reliably exit for --version; HyperFrames'
 // preflight requires a console headless shell. Reuse an existing installation.
 if(process.platform==='win32'){
  const cache=process.env.PLAYWRIGHT_BROWSERS_PATH||join(process.env.LOCALAPPDATA||join(homedir(),'AppData','Local'),'ms-playwright');
  let entries=[];try{entries=readdirSync(cache,{withFileTypes:true});}catch{}
  for(const entry of entries.filter(e=>e.isDirectory()&&!e.isSymbolicLink()&&/^chromium_headless_shell-/.test(e.name)).sort((a,b)=>b.name.localeCompare(a.name,undefined,{numeric:true}))){
   const file=join(cache,entry.name,'chrome-headless-shell-win64','chrome-headless-shell.exe');if(existsSync(file))return file;
  }
  if(engine==='hyperframes')throw Error('HyperFrames on Windows requires an installed chrome-headless-shell. Set VAC_DRAW_BROWSER to its executable. No browser is downloaded.');
 }
 const paths=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Google/Chrome/Application/chrome.exe','/usr/bin/chromium','/usr/bin/google-chrome'];
 const path=paths.find(existsSync);if(!path)throw Error('Draw browser missing: configure VAC_DRAW_BROWSER to an installed Chrome. No browser is downloaded.');
 return path;
}
export function validateDraw(c){
 const fps=c.fps?.num/c.fps?.den;
 if(!['hyperframes','remotion'].includes(c.engine)||typeof c.source!=='string'||!c.source.length||Buffer.byteLength(c.source)>96*1024||
 ![c.fps?.num,c.fps?.den].every(n=>Number.isInteger(n)&&n>0)||![c.width,c.height,c.duration_frames].every(Number.isInteger)||c.width<16||c.width>3840||c.height<16||c.height>2160||c.width%2||c.height%2||
 !Number.isFinite(fps)||fps<1||fps>120||c.duration_frames<1||c.duration_frames>18000||c.duration_frames/fps>300)throw Error('Invalid draw specification');
 return fps;
}
function auditScript(code,deterministic=false,imageIDs=[]){
 const ast=parse(code,{ecmaVersion:'latest',sourceType:'module'});
 const names=new Set(),allowed=new Set(imageIDs);
 const findImageParams=node=>{
  if(!node||typeof node!=='object')return;
  if(Array.isArray(node.params))for(const p of node.params){
   if(p.type==='ObjectPattern')for(const item of p.properties||[]){
    if(item.key?.name==='images'&&item.value?.type==='Identifier')names.add(item.value.name);
    if(item.key?.name==='images'&&item.value?.type==='AssignmentPattern'&&item.value.left?.type==='Identifier')names.add(item.value.left.name);
   }
  }
  for(const value of Object.values(node)){if(Array.isArray(value))value.forEach(findImageParams);else if(value&&typeof value==='object')findImageParams(value);}
 };
 findImageParams(ast);
 const visit=node=>{
  if(!node||typeof node!=='object')return;
  if(node.type==='MemberExpression'){
   const obj=node.object,root=names.has(obj?.name)||obj?.type==='MemberExpression'&&((obj.object?.name==='window'&&obj.property?.name==='vacImages')||(obj.property?.name==='images'&&obj.object?.name==='props'));
   const alias=node.computed?(typeof node.property?.value==='string'?node.property.value:null):node.property?.name;
   if(root&&alias&&!allowed.has(alias))throw Error('Image alias '+alias+' is not bound to an imported resource');
  }
  if(node.type==='VariableDeclarator'&&node.id?.type==='ObjectPattern'&&names.has(node.init?.name))for(const item of node.id.properties||[]){
   const alias=item.key?.name??item.key?.value;if(alias&&!allowed.has(alias))throw Error('Image alias '+alias+' is not bound to an imported resource');
  }
  if(deterministic&&node.type==='Identifier'&&['Date','performance','setTimeout','setInterval','requestAnimationFrame','cancelAnimationFrame'].includes(node.name))throw Error('Use the deterministic frame clock instead of '+node.name);
  if(deterministic&&node.type==='MemberExpression'&&node.object?.name==='Math'&&(node.property?.name==='random'||node.property?.value==='random'))throw Error('Use deterministic seeded randomness instead of Math.random');
  if(node.type==='ImportExpression')throw Error('Dynamic imports are not allowed in draw source');
  if(node.type==='ImportDeclaration'){
   if(!['react','remotion'].includes(node.source.value))throw Error('Only react and remotion imports are available');
   if(node.source.value==='remotion'&&node.specifiers.some(s=>s.type==='ImportNamespaceSpecifier'||['Audio','Video','OffthreadVideo'].includes(s.imported?.name)))throw Error('Use the editing timeline for external audio/video media');
  }
  if(node.type==='ExportAllDeclaration'||node.type==='ExportNamedDeclaration'&&node.source)throw Error('Source re-exports are not supported');
  if(node.type==='Identifier'&&['process','require','global','globalThis','eval','Function','Deno','Bun','fetch','WebSocket','XMLHttpRequest','importScripts'].includes(node.name))throw Error('Unavailable draw API: '+node.name);
  for(const value of Object.values(node)){if(Array.isArray(value))value.forEach(visit);else if(value&&typeof value==='object')visit(value);}
 };
 visit(ast);
}
export async function checkDraw({composition:c,deterministic=false}){
 if(c.document){const {validateUnified}=await import('./unified-draw.mjs');await validateUnified(c.document);return {valid:true,engine:'unified',runtime_version:'vac-unified-1',content_version:c.content_version,checks:['source','graph','timebase','local-resources','shared-alpha']};}
 validateDraw(c);const imageIDs=imageLayers(c).map(image=>image.id);
 if(c.engine==='remotion'){
  const js=await transform(c.source,{loader:'tsx',jsx:'automatic',target:'es2022',tsconfigRaw:{compilerOptions:{verbatimModuleSyntax:true}}});auditScript(js.code.replace(/from\s*["']react\/jsx-runtime["']/g,'from "react"'),deterministic,imageIDs);
  if(!/export\s+default\b/.test(c.source))throw Error('Export one default React composition component');
 }else{
  if(!/<html\b/i.test(c.source)||!/<head\b/i.test(c.source)||!/<body\b/i.test(c.source))throw Error('HyperFrames requires a complete HTML document');
  if(/<base\b|<iframe\b|<object\b|<embed\b|<link\b|<form\b|data-composition-src\s*=/i.test(c.source))throw Error('Only self-contained HTML compositions are supported');
  for(const m of c.source.matchAll(/\b(?:src|href|poster|action)\s*=\s*["']([^"']*)["']/gi))
   if(!m[1].startsWith('data:image/')&&!m[1].startsWith('#')&&!isImageReference(m[1],c))throw Error('Use inline graphics; external resources and paths are unavailable');
  if(/url\s*\(\s*["']?(?!data:|#)/i.test(c.source))throw Error('External CSS resources are unavailable');
  const root=c.source.match(/<[^>]+\bdata-composition-id=["']([^"']+)["'][^>]*>/i);
  if(!root)throw Error('HyperFrames root requires data-composition-id');
  const attributes={width:c.width,height:c.height,duration:c.duration_frames/validateDraw(c)};
  for(const [key,value] of Object.entries(attributes)){
   const m=root[0].match(new RegExp('data-'+key+'=["\\x27]([^"\\x27]+)["\\x27]','i'));
   if(!m||!Number.isFinite(Number(m[1]))||Math.abs(Number(m[1])-value)>1e-6)throw Error('Root data-'+key+' must match the draw specification');
  }
  for(const script of c.source.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/gi))auditScript(script[1],deterministic,imageIDs);
 }
 return {valid:true,engine:c.engine,runtime_version:drawVersions[c.engine],content_version:c.content_version,checks:['source','dimensions','timebase','local-resources']};
}
async function digest(path){const h=createHash('sha256');for await(const b of createReadStream(path))h.update(b);return h.digest('hex');}
async function ensureDirectory(path){
 const absolute=resolve(path);if(!isAbsolute(path))throw Error('Owned output directory must be absolute');
 await mkdir(absolute,{recursive:true});
 for(let p=absolute;;p=dirname(p)){if((await lstat(p)).isSymbolicLink())throw Error('Draw output must not use symbolic links');if(p===dirname(p))break;}
 return absolute;
}
async function staticServer(directory){
 const root=await realpath(directory);
 const server=createServer(async(req,res)=>{
  try{
   const url=new URL(req.url,'http://localhost'),name=decodeURIComponent(url.pathname)==='/'?'index.html':decodeURIComponent(url.pathname).slice(1);
   const target=await realpath(resolve(root,name)),rel=relative(root,target);
   if(rel.startsWith('..'+sep)||isAbsolute(rel)||req.method!=='GET')throw Error('outside bundle');
   const data=await readFile(target),extension=target.split('.').at(-1);
   res.setHeader('Content-Type',({html:'text/html',js:'application/javascript',css:'text/css',json:'application/json',woff2:'font/woff2',png:'image/png'})[extension]??'application/octet-stream');res.end(data);
  }catch{res.statusCode=404;res.end();}
 });
 await new Promise((done,fail)=>{server.once('error',fail);server.listen(0,'127.0.0.1',done);});
 return {url:'http://127.0.0.1:'+server.address().port,close:()=>new Promise(done=>server.close(done))};
}
async function renderRemotion(c,stage,browserPath,prepared){
 const {bundle}=await import('@remotion/bundler'),{openBrowser,selectComposition,renderMedia}=await import('@remotion/renderer');
 const component=join(stage,'Composition.tsx'),entry=join(stage,'entry.tsx');
 await writeFile(component,c.source);
 await writeFile(entry,c.images?.length?remotionImageEntry(c):'import React from "react";import {Composition,registerRoot} from "remotion";import Scene from "./Composition";registerRoot(()=>React.createElement(Composition,{id:"VAC",component:Scene,width:'+c.width+',height:'+c.height+',fps:'+validateDraw(c)+',durationInFrames:'+c.duration_frames+'}));');
 const modules=join(runtimeRoot,'node_modules');
 const bundled=await bundle({entryPoint:entry,rootDir:runtimeRoot,outDir:join(stage,'bundle'),publicDir:null,enableCaching:false,webpackOverride:config=>({...config,resolve:{...config.resolve,modules:[modules,...(config.resolve?.modules??[])],alias:{...config.resolve?.alias,react:join(modules,'react'),remotion:join(modules,'remotion'),'react-dom':join(modules,'react-dom')}}})});
 if(prepared.length)await installImages(prepared,bundled);
 const server=await staticServer(bundled);let browser;
 try{
  browser=await openBrowser('chrome',{browserExecutable:browserPath,logLevel:'error',chromeMode:/headless.shell/i.test(browserPath)?'headless-shell':'chrome-for-testing',chromiumOptions:{headless:true}});
  guardRemotion(browser,server.url);
  const composition=await selectComposition({serveUrl:server.url,id:'VAC',puppeteerInstance:browser,logLevel:'error'});
  await renderMedia({serveUrl:server.url,composition,codec:'h264',outputLocation:join(stage,'program.mp4'),puppeteerInstance:browser,concurrency:1,logLevel:'error',crf:18});
 }finally{try{if(browser)await browser.close({silent:true});}finally{await server.close();}}
}
async function renderHyperframes(c,stage,browserPath,prepared){
 const gsap=await readFile(require.resolve('gsap/dist/gsap.min.js'),'utf8');
 const source=hyperframesImageSource(c,gsap);
 const project=join(stage,'html');await mkdir(project);await writeFile(join(project,'index.html'),source);if(prepared.length)await installImages(prepared,project);
 const cli=join(dirname(require.resolve('hyperframes/package.json')),'bin','hyperframes.mjs'),guard=join(dirname(fileURLToPath(import.meta.url)),'draw-network.mjs');
 const env={...process.env,VAC_DRAW_NETWORK_POLICY:'1',PRODUCER_HEADLESS_SHELL_PATH:browserPath,HYPERFRAMES_CHROME_PATH:browserPath,DO_NOT_TRACK:'1',HYPERFRAMES_TELEMETRY_DISABLED:'1',NODE_OPTIONS:'--import='+JSON.stringify(pathToFileURL(guard).href)};
 await new Promise((done,fail)=>{
  const child=spawn(process.execPath,[cli,'render',project,'--output',join(stage,'program.mp4'),'--fps',String(validateDraw(c)),'--workers','1','--quality','draft','--no-browser-gpu','--no-best-effort','--quiet'],{cwd:project,env,windowsHide:true,stdio:['ignore','pipe','pipe']});
  let message='';const collect=b=>{message=(message+b.toString()).slice(-12000);};child.stdout.on('data',collect);child.stderr.on('data',collect);child.on('error',fail);
  child.once('exit',code=>code===0?done():fail(Error('HyperFrames render failed: '+message)));
 });
}
export async function renderDraw(input){
 if(input.composition.document){const {renderUnifiedDraw}=await import('./unified-draw.mjs');return renderUnifiedDraw(input);}
 const c=input.composition;await checkDraw(input);
 const assets=await checkedImageAssets(input,digest);
 const output=await ensureDirectory(input.output_dir),movie=join(output,'program.mp4'),reportFile=join(output,'program.json');
 try{const old=JSON.parse(await readFile(reportFile,'utf8'));if((old.environment_version??'')===(input.environment_version??'')&&old.content_version===c.content_version&&old.runtime_version===drawVersions[c.engine]&&(!assets.length||old.image_adapter_version===2)&&old.sha256===await digest(movie))return old;}catch{}
 const browser=drawBrowser(c.engine),stage=await mkdtemp(join(output,'.stage-'));
 try{
  const prepared=await prepareImages(assets,stage,digest);
  if(c.engine==='remotion')await renderRemotion(c,stage,browser,prepared);else await renderHyperframes(c,stage,browser,prepared);
  const media=await probeMedia(join(stage,'program.mp4')),fps=validateDraw(c);
  if(media.video?.width!==c.width||media.video?.height!==c.height||Math.abs(media.video.fps-fps)>1e-5||Math.abs(media.duration-c.duration_frames/fps)>1/fps+.001||media.video.frames!==c.duration_frames)throw Error('Rendered video does not match the requested frame count, size or timebase');
  const report={environment_version:input.environment_version??'',content_version:c.content_version,sha256:await digest(join(stage,'program.mp4')),size_bytes:(await stat(join(stage,'program.mp4'))).size,width:c.width,height:c.height,frames:media.video.frames,duration:media.duration,engine:c.engine,runtime_version:drawVersions[c.engine],...(assets.length?{image_adapter_version:2,images:prepared.map(({resource_id,sha256,prepared_sha256})=>({resource_id,sha256,prepared_sha256}))}:{})};
  await rename(join(stage,'program.mp4'),movie);await writeFile(reportFile+'.partial',JSON.stringify(report));await rename(reportFile+'.partial',reportFile);
  return report;
 }finally{await rm(stage,{recursive:true,force:true});}
}
