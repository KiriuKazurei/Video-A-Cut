import {createHash} from 'node:crypto';
import {mkdir,readFile,writeFile,rm} from 'node:fs/promises';
import {join} from 'node:path';
import {sequenceGraph,renderSequence,renderSequenceFrame,renderSequenceCaptionFrame,selectedTracks} from './sequence.mjs';
import {runTool} from './media.mjs';
import {openUnifiedDraw} from './unified-draw.mjs';
import {openSequenceGPU} from './sequence-gpu.mjs';
const hash=value=>createHash('sha256').update(JSON.stringify(value)).digest('hex');

// One instance per active sequence. PNG byte budgets are shared by source and
// composite caches; host count is bounded independently. Exports stay unchanged.
export async function createSequenceFrameSession({output_dir:root,cache_bytes=64*1024*1024,prefetch_frames=60,backend='auto',browser_path}={}) {
 await mkdir(root,{recursive:true});
 const cache=new Map(),hosts=new Map();let bytes=0,closed=false,gpu,gpuStarted=false,serial=Promise.resolve(),generation=0;
 const stats={host_loads:0,source_compiles:0,source_cache_hits:0,frame_cache_hits:0,source_renders:0,prefetched_frames:0};
 const capabilities={available:false,renderer:'',hardware_accelerated:false,reason:'not initialized'};
 const get=key=>{const value=cache.get(key);if(value){cache.delete(key);cache.set(key,value);}return value;};
 const put=(key,png)=>{if(png.length>cache_bytes)return;const previous=cache.get(key);if(previous){bytes-=previous.length;cache.delete(key);}while(bytes+png.length>cache_bytes&&cache.size){const first=cache.keys().next().value;bytes-=cache.get(first).length;cache.delete(first);}cache.set(key,png);bytes+=png.length;};
 const ensureGPU=async()=>{if(gpuStarted)return;gpuStarted=true;try{gpu=await openSequenceGPU({backend,browser_path});Object.assign(capabilities,gpu.capabilities);}catch(error){capabilities.reason=error.message;}};
 const diagnostics=()=>({...stats,cache_bytes:bytes,cache_limit_bytes:cache_bytes,hosts:hosts.size,backend:capabilities.available?'webgl2':'cpu-ffmpeg',renderer:capabilities.renderer,hardware_accelerated:capabilities.available&&capabilities.hardware_accelerated,fallback_reason:capabilities.reason});
 const queue=action=>{const next=serial.then(()=>{if(closed)throw Error('Preview session closed');return action();});serial=next.catch(()=>{});return next;};
 const drawingHost=async r=>{
  const assets=hash(r.assets??[]),document=hash(r.draw_document);let entry=hosts.get(r.id);
  if(entry&&entry.assets!==assets){await entry.host.close();await rm(entry.directory,{recursive:true,force:true});hosts.delete(r.id);entry=undefined;}
  if(!entry){
   if(hosts.size>=4){const first=hosts.keys().next().value,old=hosts.get(first);await old.host.close();await rm(old.directory,{recursive:true,force:true});hosts.delete(first);}
   const directory=join(root,'host-'+hash(r.id).slice(0,8));
   entry={host:await openUnifiedDraw({document:r.draw_document,assets:r.assets,output_dir:directory,browser:gpu?.browser,browser_path}),assets,document,directory};hosts.set(r.id,entry);stats.host_loads++;
   stats.source_compiles+=Object.values(entry.host.diagnostics().source_compiles).reduce((a,b)=>a+b,0);
  }else{
   hosts.delete(r.id);hosts.set(r.id,entry);
   if(entry.document!==document){const before=Object.values(entry.host.diagnostics().source_compiles).reduce((a,b)=>a+b,0);await entry.host.update(r.draw_document);stats.source_compiles+=Object.values(entry.host.diagnostics().source_compiles).reduce((a,b)=>a+b,0)-before;entry.document=document;}
  }
  return entry;
 };
 const render=async input=>{
  const started=performance.now(),{sequence,frame,resources}=input;
  // Graph is also the authoritative selection/effect interpreter for export.
  const resolved=resources.map(r=>r.draw_document?{...r,path:join(root,'drawing-source.png')}:r);
  const graph=sequenceGraph(sequence,resolved,{frame});
  const keys=graph.frameLayers.map(layer=>{const r=resources.find(r=>r.id===layer.resource_id),d=r.draw_document;const time=d?Math.min(d.duration_frames-1,Math.max(0,Math.floor(layer.source_time*d.canvas.fps.num/d.canvas.fps.den+1e-7))):Number(layer.source_time.toFixed(9));return hash({id:r.id,sha:r.sha256,doc:d,assets:r.assets,stream:layer.stream_index,time});});
  const finalKey='frame:'+hash({width:sequence.width,height:sequence.height,layers:graph.frameLayers.map((l,i)=>({...l,key:keys[i]})),cues:graph.cues});
  let png=get(finalKey);
  if(png){stats.frame_cache_hits++;return {content_version:input.content_version,frame,image:'data:image/png;base64,'+png.toString('base64'),diagnostics:{...diagnostics(),elapsed_ms:performance.now()-started}};}
  await ensureGPU();
  const layers=[],prepared=structuredClone(sequence),flattened=[...resources];
  for(const [i,layer] of graph.frameLayers.entries()){
   const r=resources.find(r=>r.id===layer.resource_id),key='source:'+keys[i];let source=get(key);
   if(source)stats.source_cache_hits++;
   else{
    if(r.draw_document){
     const entry=await drawingHost(r);
     const d=r.draw_document,sourceFrame=Math.min(d.duration_frames-1,Math.max(0,Math.floor(layer.source_time*d.canvas.fps.num/d.canvas.fps.den+1e-7)));
     source=(await entry.host.seek({frame:sourceFrame,request_id:'sequence-frame'})).png;
    }else if(r.kind==='image')source=await readFile(r.path);
    else{
     const path=join(root,'decoded.png');await runTool('ffmpeg',['-v','error','-nostdin','-y','-ss',String(layer.source_time),'-i',r.path,'-map',`0:${layer.stream_index}`,'-frames:v','1','-threads','1',path],{timeoutMs:30_000});source=await readFile(path);
    }
    stats.source_renders++;put(key,source);
   }
   layers.push({...layer,png:source.toString('base64')});
   const id='frame:'+layer.clip_id,path=join(root,'input-'+i+'.png');await writeFile(path,source);
   flattened.push({...r,id,kind:'image',draw_document:undefined,path});
   for(const track of prepared.tracks){const clip=track.clips.find(c=>c.id===layer.clip_id);if(clip){clip.resource_id=id;clip.stream_index=0;}}
  }
  if(capabilities.available){
   try{
    if(graph.cues.length){const key='caption:'+hash({width:sequence.width,height:sequence.height,cues:graph.cues});let caption=get(key);if(!caption){caption=await renderSequenceCaptionFrame({sequence,cues:graph.cues,frame,output_dir:join(root,'captions')});put(key,caption);}layers.push({png:caption.toString('base64'),width:sequence.width,height:sequence.height,x:0,y:0,crop:[0,0,0,0],brightness:1,opacity:1,fade_color:0});}
    png=await gpu.render({width:sequence.width,height:sequence.height,layers});
   }catch(error){capabilities.available=false;capabilities.reason=error.message;}
  }
  if(!png){await renderSequenceFrame({sequence:prepared,resources:flattened,frame,output_dir:join(root,'cpu'),content_version:input.content_version});png=await readFile(join(root,'cpu','frame.png'));}
  put(finalKey,png);
  return {content_version:input.content_version,frame,image:'data:image/png;base64,'+png.toString('base64'),diagnostics:{...diagnostics(),elapsed_ms:performance.now()-started}};
 };
 return {
  render:input=>{generation++;return queue(()=>render(input));},diagnostics,
  renderSegment:input=>{generation++;return queue(async()=>{await ensureGPU();return renderSequence(input,{prepareDrawFrames:async options=>{
   const r=input.resources.find(r=>r.id===options.resource_id),entry=await drawingHost(r);await mkdir(options.output_dir,{recursive:true});
   for(let frame=options.from_frame;frame<options.to_frame;frame+=options.frame_step??1){
    const key='segment-source:'+hash({id:r.id,doc:r.draw_document,assets:r.assets,scale:options.scale,frame});let png=get(key);
    if(!png){png=(await entry.host.seek({frame,scale:options.scale,request_id:'segment-'+frame})).png;put(key,png);}
    await writeFile(join(options.output_dir,String(frame/(options.frame_step??1)).padStart(6,'0')+'.png'),png);
   }
   return join(options.output_dir,'%06d.png');
  }});});},
  cancelPrefetch:()=>{generation++;},
  prefetch:async input=>{
   const current=++generation,fps=input.sequence.fps.num/input.sequence.fps.den,end=Math.max(0,...selectedTracks(input.sequence).flatMap(t=>t.clips.map(c=>c.at+c.duration)));
   const radius=Math.min(prefetch_frames,Math.ceil(fps*2));
   for(let distance=1;distance<=radius&&current===generation&&!closed;distance++)for(const direction of [1,-1]){
    const frame=input.frame+distance*direction;if(frame<0||frame>=end||bytes>cache_bytes*.75)continue;
    await new Promise(done=>setTimeout(done,0));if(current!==generation||closed)return;
    // Foreground requests can enqueue between every prefetched frame.
    await queue(async()=>{if(current!==generation)return;await render({...input,frame});stats.prefetched_frames++;}).catch(()=>{});
   }
  },
  close:async()=>{generation++;closed=true;await serial;for(const entry of hosts.values())await entry.host.close().catch(()=>{});hosts.clear();await gpu?.close();cache.clear();bytes=0;await rm(root,{recursive:true,force:true});}
 };
}
