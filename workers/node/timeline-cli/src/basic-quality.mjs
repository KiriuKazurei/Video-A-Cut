import { runTool } from './media.mjs';

const MIN = 1;
const number = v => String(Number(v.toFixed(9)));
function merge(items) {
 const ordered=items.filter(x=>Number.isFinite(x.start)&&Number.isFinite(x.end)&&x.end>x.start).sort((a,b)=>a.start-b.start), out=[];
 for(const x of ordered) { const last=out.at(-1); if(last && x.start<=last.end+0.001) last.end=Math.max(last.end,x.end); else out.push({...x}); }
 return out;
}
function subtract(items, excluded) {
 let current=merge(items);
 for(const e of merge(excluded)) current=current.flatMap(x=>e.end<=x.start||e.start>=x.end?[x]:[{start:x.start,end:Math.min(x.end,e.start)},{start:Math.max(x.start,e.end),end:x.end}].filter(y=>y.end>y.start));
 return current;
}
function intersect(a,b) { const result=[];for(const x of a)for(const y of b){const start=Math.max(x.start,y.start),end=Math.min(x.end,y.end);if(end-start>=MIN-0.001)result.push({start,end});}return merge(result); }
function intervals(text, prefix, duration) {
 const events=[...text.matchAll(new RegExp(`lavfi\\.${prefix}_(start|end)=(-?\\d+(?:\\.\\d+)?)`,'g'))];
 let start=null;const result=[];
 for(const event of events){const value=Math.max(0,Math.min(duration,Number(event[2])));if(event[1]==='start')start=value;else if(start!==null){result.push({start,end:value});start=null;}}
 if(start!==null)result.push({start,end:duration});return merge(result);
}
async function detect(path,kind,duration,{start=0,length=duration,stream,gainDB=0,fadeIn=0,fadeOut=0,run=runTool}={}) {
 const args=['-hide_banner','-v','error','-nostdin'];
 if(start>0) args.push('-ss',number(start));
 args.push('-i',path,'-t',number(length));
 if(kind==='silence') {
  const filters=[`volume=${number(gainDB)}dB`];
  if(fadeIn>0)filters.push(`afade=t=in:st=0:d=${number(Math.min(fadeIn,length))}`);
  if(fadeOut>0)filters.push(`afade=t=out:st=${number(Math.max(0,length-fadeOut))}:d=${number(Math.min(fadeOut,length))}`);
  filters.push('silencedetect=noise=-50dB:d=1','ametadata=mode=print:file=-');
  args.push('-map',stream===undefined?'0:a:0':`0:${stream}`,'-vn','-af',filters.join(','));
 }
 else args.push('-map','0:v:0','-an','-vf','blackdetect=d=1,metadata=mode=print:file=-');
 args.push('-f','null','-');
 return intervals(await run('ffmpeg',args,{timeoutMs:360000,maxOutputBytes:4*1024*1024}),kind,length);
}
// Expectations contain source audio windows mapped onto the rendered timeline.
export async function inspectBasicQuality({path,sha256,duration,videoWindows,audioSources}, {run=runTool}={}) {
 const result={sha256,status:'not_checked',audio_status:'skipped',video_status:'not_checked',issues:[]};
 const controller=new AbortController(), timer=setTimeout(()=>controller.abort(new Error('quality check timed out')),30000);timer.unref();
 const bounded=(command,args,options)=>run(command,args,{...options,signal:options?.signal?AbortSignal.any([options.signal,controller.signal]):controller.signal});
 try {
  const video=merge(videoWindows), expected=[];
  for(const a of audioSources) {
   const length=a.source_end-a.source_start;
   if(!(length>0 && a.speed>0)) continue;
   const quiet=await detect(a.path,'silence',length,{start:a.source_start,length,stream:a.stream_index,gainDB:a.gain_db||0,fadeIn:a.fade_in||0,fadeOut:a.fade_out||0,run:bounded});
   for(const x of subtract([{start:0,end:length}],quiet)) expected.push({start:a.timeline_start+x.start/a.speed,end:Math.min(a.timeline_end,a.timeline_start+x.end/a.speed)});
  }
  const black=await detect(path,'black',duration,{run:bounded});
  for(const x of intersect(black,video)) result.issues.push({kind:'black',...x});
  result.video_status=video.length?'passed':'skipped';
  if(expected.length) {
   const silence=await detect(path,'silence',duration,{run:bounded});
   for(const x of intersect(silence,merge(expected)))result.issues.push({kind:'silence',...x});
   result.audio_status=result.issues.some(x=>x.kind==='silence')?'failed':'passed';
  }
  if(result.issues.some(x=>x.kind==='black'))result.video_status='failed';
  result.status=result.issues.length?'failed':(result.video_status==='skipped' && result.audio_status==='skipped'?'not_checked':'passed');
 } catch { result.status='not_checked';result.audio_status='not_checked';result.video_status='not_checked';result.issues=[]; } finally { clearTimeout(timer); }
 return result;
}
export function sequenceExpectations(sequence, resources, tracks) {
 const fps=sequence.fps.num/sequence.fps.den, audioSources=[],videoWindows=[],videoExclusions=[];
 for(const track of tracks) for(const clip of track.clips) {
  const start=clip.at/fps,end=(clip.at+clip.duration)/fps,e=clip.effects||{};
  if(track.kind==='video') {
   const black=!e.fade_color || e.fade_color==='black', from=(e.from_frame??0)/fps,to=(e.to_frame??clip.duration)/fps;
   videoWindows.push({start,end});
   if(black && e.fade_in)videoExclusions.push({start:start+from,end:start+from+Math.min(e.fade_in/fps,to-from)});
   if(black && e.fade_out)videoExclusions.push({start:start+Math.max(from,to-e.fade_out/fps),end:start+to});
   if(e.transition)videoExclusions.push({start,end:Math.min(end,start+e.transition/fps)});
  }
  if(track.kind==='audio') {
   const resource=resources.find(r=>r.id===clip.resource_id);
   if(resource) {
    const duration=end-start,from=Math.max(0,Math.min(duration,(e.from_frame??0)/fps)),to=Math.max(from,Math.min(duration,(e.to_frame??clip.duration)/fps));
    const sourceStart=clip.source_in.value/clip.source_in.scale,sourceEnd=clip.source_out.value/clip.source_out.scale;
    for(const [a,b,gain] of [[0,from,0],[from,to,e.gain_db||0],[to,duration,0]]) if(b>a) audioSources.push({path:resource.path,stream_index:clip.stream_index,source_start:Math.min(sourceEnd,sourceStart+a*clip.speed),source_end:Math.min(sourceEnd,sourceStart+b*clip.speed),timeline_start:start+a,timeline_end:start+b,speed:clip.speed,gain_db:(track.gain_db||0)+gain,fade_in:a===from&&b===to?Math.min((e.fade_in||0)/fps,to-from)*clip.speed:0,fade_out:a===from&&b===to?Math.min((e.fade_out||0)/fps,to-from)*clip.speed:0});
   }
  }
 }
 return {videoWindows:subtract(videoWindows,videoExclusions),audioSources};
}
