import type { PlaybackClip, SequencePlayback, SequenceSegment } from './timelineContracts.ts';

export interface ProgramPlayer { seek:(seconds:number)=>void; toggle:()=>void; pause:()=>void; close:()=>void }
export interface PlayerEvents {
 frame:(seconds:number)=>void; ready:(ready:boolean)=>void;
 media:(video:HTMLVideoElement|null,audio:HTMLMediaElement|null)=>void;
 error:(error:unknown)=>void;
}
export function playbackClipAt(clips:PlaybackClip[],frame:number) {return clips.find(c=>frame>=c.at&&frame<c.at+c.duration);}
export function playbackSourceTime(clip:PlaybackClip,seconds:number,fps:number) {return clip.source_in+seconds-clip.at/fps;}
export function setSegmentWindow(buffer:Pick<SourceBuffer,'timestampOffset'|'appendWindowStart'|'appendWindowEnd'>,start:number,end:number) {
 buffer.appendWindowEnd=Infinity;buffer.appendWindowStart=start;buffer.appendWindowEnd=end;buffer.timestampOffset=start;
}

function loaded(media:HTMLMediaElement,signal:AbortSignal) {
 return new Promise<void>((resolve,reject)=>{
  const finish=(error?:unknown)=>{clearTimeout(timer);for(const event of ['loadeddata','canplay','seeked'])media.removeEventListener(event,check);media.removeEventListener('error',failed);signal.removeEventListener('abort',aborted);error?reject(error):resolve();};
  const check=()=>{if(media.readyState>=2&&!media.seeking)finish();};
  const failed=()=>finish(Error(media.error?.message||'当前媒体无法解码'));
  const aborted=()=>finish(new DOMException('Superseded','AbortError'));
  const timer=setTimeout(()=>finish(Error('媒体定位超时')),20000);
  for(const event of ['loadeddata','canplay','seeked'])media.addEventListener(event,check);
  media.addEventListener('error',failed);signal.addEventListener('abort',aborted,{once:true});
  if(signal.aborted)aborted();else if(media.error)failed();else check();
 });
}
function disposeMedia(media:HTMLMediaElement) {media.pause();media.removeAttribute('src');media.load();media.remove();}

// Two source slots per media type keep the following cut preloaded. The active
// audio element supplies the master clock; a silent gap uses a monotonic clock.
export function directProgramPlayer(container:HTMLElement,plan:SequencePlayback,url:(id:string)=>string,events:PlayerEvents,initial:number):ProgramPlayer {
 const fps=plan.fps.num/plan.fps.den,duration=plan.frames/fps;
 const videos=[document.createElement('video'),document.createElement('video')],audios=[document.createElement('audio'),document.createElement('audio')];
 for(const v of videos){v.crossOrigin='anonymous';v.muted=true;v.preload='auto';v.playsInline=true;v.className='program-preload';container.append(v);}
 for(const a of audios){a.crossOrigin='anonymous';a.preload='auto';container.append(a);}
 type Slot={el:HTMLMediaElement;resource:string;clip?:PlaybackClip};
 const videoSlots=videos.map(el=>({el,resource:''} as Slot)),audioSlots=audios.map(el=>({el,resource:''} as Slot));
 let video:Slot|undefined,audio:Slot|undefined,time=initial,epoch=performance.now(),epochTime=initial,intent=false,busy=false,closed=false,generation=0,raf=0,lastNotify=0,lastHardSeek=0,abort=new AbortController();
 container.dataset.mode='direct';
 const stop=()=>{for(const slot of [...videoSlots,...audioSlots])slot.el.pause();};
 const current=()=>audio?.clip&&!audio.el.paused&&!audio.el.seeking?audio.clip.at/fps+audio.el.currentTime-audio.clip.source_in:epochTime+(intent&&!busy?(performance.now()-epoch)/1000:0);
 const assign=(slots:Slot[],previous:Slot|undefined,clip:PlaybackClip|undefined,seconds:number,force:boolean)=>{
  if(!clip)return undefined;
  const slot=(previous?.resource===clip.resource_id?previous:slots.find(s=>s!==previous&&s.resource===clip.resource_id))??slots.find(s=>s!==previous)??slots[0];
  const target=playbackSourceTime(clip,seconds,fps);slot.clip=clip;
  if(slot.resource!==clip.resource_id){slot.el.src=url(clip.resource_id);slot.resource=clip.resource_id;slot.el.load();}
  if(force||Math.abs(slot.el.currentTime-target)>1/fps){
   if(slot.el.readyState>=1)slot.el.currentTime=target;
   else slot.el.addEventListener('loadedmetadata',()=>{if(!closed&&slot.clip?.id===clip.id)slot.el.currentTime=target;},{once:true});
  }
  return slot;
 };
 const preload=(slots:Slot[],active:Slot|undefined,clips:PlaybackClip[])=>{
  const next=clips.find(c=>c.at>time*fps&&c.id!==active?.clip?.id);if(!next||next.at/fps-time>4||next.resource_id===active?.resource)return;
  assign(slots,active,next,next.at/fps,false);
 };
 const publish=()=>{container.dataset.playhead=String(time);container.dataset.playing=String(intent&&!busy);if(video?.clip)container.dataset.avDrift=String(video.el.currentTime-playbackSourceTime(video.clip,time,fps));};
 const resume=async()=>{if(!intent||closed||busy)return;try{await Promise.all([video?.el.play(),audio?.el.play()]);epoch=performance.now();epochTime=time;}catch(error){intent=false;stop();events.error(error);}};
 const position=async(seconds:number,force=true)=>{
  const own=++generation;abort.abort();abort=new AbortController();busy=true;time=Math.min(Math.max(0,seconds),Math.max(0,duration-1/fps));epochTime=time;stop();events.ready(false);
  const v=assign(videoSlots,video,playbackClipAt(plan.video,Math.floor(time*fps+1e-6)),time,force),a=assign(audioSlots,audio,playbackClipAt(plan.audio,Math.floor(time*fps+1e-6)),time,force);
  try{
   await Promise.all([v&&loaded(v.el,abort.signal),a&&loaded(a.el,abort.signal)]);
   if(closed||own!==generation)return;
   video=v;audio=a;for(const slot of videoSlots){slot.el.className=slot===video?'program-video':'program-preload';slot.el.setAttribute('aria-label',slot===video?'节目播放器':'预加载画面');}
   events.media((video?.el as HTMLVideoElement)??null,audio?.el??video?.el??null);busy=false;epoch=performance.now();epochTime=time;events.frame(time);events.ready(true);publish();
   preload(videoSlots,video,plan.video);preload(audioSlots,audio,plan.audio);await resume();
  }catch(error){if(!closed&&own===generation){busy=false;intent=false;events.error(error);}}
 };
 const tick=()=>{
  if(closed)return;raf=requestAnimationFrame(tick);
  if(!intent||busy)return;
  time=Math.min(duration,current());
  if(time>=duration){intent=false;stop();time=Math.max(0,duration-1/fps);events.frame(time);publish();return;}
  const v=playbackClipAt(plan.video,Math.floor(time*fps+1e-6)),a=playbackClipAt(plan.audio,Math.floor(time*fps+1e-6));
  if(v?.id!==video?.clip?.id||a?.id!==audio?.clip?.id){void position(time,false);return;}
  if((video&&video.el.readyState<2&&!video.el.seeking)||(audio&&(audio.el.readyState<2||audio.el.seeking))){void position(time,false);return;}
  if(video?.clip&&!video.el.seeking){
   const target=playbackSourceTime(video.clip,time,fps),drift=video.el.currentTime-target;
   // Decoders expose quantized clocks. Small differences must not pause the
   // audio clock or restart every source: that slows the entire programme.
   video.el.playbackRate=Math.abs(drift)<.015?1:Math.max(.97,Math.min(1.03,1-drift*.25));
   if(Math.abs(drift)>.2&&performance.now()-lastHardSeek>1000){lastHardSeek=performance.now();video.el.currentTime=target;}
  }
  if(performance.now()-lastNotify>50){lastNotify=performance.now();events.frame(time);preload(videoSlots,video,plan.video);preload(audioSlots,audio,plan.audio);publish();}
 };
 raf=requestAnimationFrame(tick);void position(initial);
 return {seek:seconds=>{void position(seconds);},toggle:()=>{if(intent){time=current();intent=false;epochTime=time;stop();publish();}else{intent=true;if(time>=duration-1/fps)void position(0);else void resume();}},pause:()=>{if(intent)time=current();intent=false;epochTime=time;stop();publish();},close:()=>{closed=true;intent=false;generation++;abort.abort();cancelAnimationFrame(raf);for(const el of [...videos,...audios])disposeMedia(el);events.media(null,null);}};
}

// fMP4 fragments are appended to one media element, retaining one audio clock
// across segment boundaries. Only the seek neighbourhood is requested.
export function segmentedProgramPlayer(container:HTMLElement,plan:SequencePlayback,segment:(frame:number,signal:AbortSignal)=>Promise<{report:SequenceSegment;bytes:ArrayBuffer}>,events:PlayerEvents,initial:number):ProgramPlayer {
 const fps=plan.fps.num/plan.fps.den,duration=plan.frames/fps,mime='video/mp4; codecs="avc1.4D401F, mp4a.40.2"';
 if(!('MediaSource' in window)||!MediaSource.isTypeSupported(mime))throw Error('当前桌面媒体组件不支持分段预览');
 const video=document.createElement('video');video.className='program-video';video.preload='auto';video.playsInline=true;video.setAttribute('aria-label','节目播放器');container.append(video);container.dataset.mode='segments';events.media(video,video);
 let media:MediaSource,buffer:SourceBuffer|undefined,objectURL='',abort=new AbortController(),generation=0,closed=false,intent=false,pumping=false,desired=initial,raf=0,lastNotify=0;
 const appended=new Set<number>();
 const wait=(target:EventTarget,event:string,signal:AbortSignal)=>new Promise<void>((resolve,reject)=>{const done=()=>{clearTimeout(timer);target.removeEventListener(event,ok);target.removeEventListener('error',bad);signal.removeEventListener('abort',bad);};const ok=()=>{done();resolve();};const bad=()=>{done();reject(signal.aborted?new DOMException('Superseded','AbortError'):Error('分段缓冲失败'));};const timer=setTimeout(bad,30000);target.addEventListener(event,ok,{once:true});target.addEventListener('error',bad,{once:true});signal.addEventListener('abort',bad,{once:true});if(signal.aborted)bad();});
 const contains=(seconds:number)=>{for(let i=0;i<video.buffered.length;i++)if(video.buffered.start(i)<=seconds+.02&&video.buffered.end(i)>seconds+.02)return true;return false;};
 const pump=async()=>{
  if(pumping||closed||!buffer)return;pumping=true;const own=generation,signal=abort.signal;
  try{
   const first=Math.floor(desired*fps/plan.segment_frames)*plan.segment_frames;
   for(let frame=first;frame<Math.min(plan.frames,first+plan.segment_frames*4);frame+=plan.segment_frames){
    if(closed||own!==generation)break;if(appended.has(frame))continue;
    const {report,bytes}=await segment(frame,signal);if(closed||own!==generation)return;
    if(buffer.buffered.length&&buffer.buffered.start(0)<desired-14){const removed=wait(buffer,'updateend',signal);buffer.remove(0,desired-10);await removed;for(const old of appended)if((old+plan.segment_frames)/fps<desired-10)appended.delete(old);}
    setSegmentWindow(buffer,report.start_frame/fps,Math.min(duration,(report.start_frame+report.frames)/fps));
    const updated=wait(buffer,'updateend',signal);buffer.appendBuffer(bytes);await updated;appended.add(frame);
    // SourceBuffer completion can precede the media element's metadata and
    // buffered ranges. At a nonzero restore point, checking too early leaves
    // currentTime at zero outside the appended window with no later retry.
    if(video.readyState<1)await wait(video,'loadedmetadata',signal);
    if(closed||own!==generation)return;
    // The media element can still expose stale ranges after metadata; the
    // SourceBuffer is authoritative for the append just acknowledged above.
    let appendedContains=false;for(let i=0;i<buffer.buffered.length;i++)if(buffer.buffered.start(i)<=desired+.02&&buffer.buffered.end(i)>desired)appendedContains=true;
    if(appendedContains){if(Math.abs(video.currentTime-desired)>.1)video.currentTime=desired;await loaded(video,signal);if(closed||own!==generation)return;events.ready(true);if(intent&&video.paused)await video.play();}
    if(frame+report.frames>=plan.frames&&media.readyState==='open')media.endOfStream();
   }
  }catch(error){if(!closed&&own===generation){intent=false;events.error(error);}}
  finally{if(own===generation)pumping=false;}
 };
 const open=async(seconds:number)=>{
  generation++;abort.abort();abort=new AbortController();pumping=false;appended.clear();desired=Math.min(Math.max(0,seconds),Math.max(0,duration-1/fps));events.ready(false);video.pause();if(objectURL)URL.revokeObjectURL(objectURL);media=new MediaSource();objectURL=URL.createObjectURL(media);const own=generation,signal=abort.signal;
  const opened=wait(media,'sourceopen',signal);video.src=objectURL;
  try{await opened;if(closed||own!==generation)return;media.duration=duration;buffer=media.addSourceBuffer(mime);void pump();}catch(error){if(!closed&&own===generation)events.error(error);}
 };
 const tick=()=>{if(closed)return;raf=requestAnimationFrame(tick);if(!video.paused){desired=video.currentTime;if(performance.now()-lastNotify>50){lastNotify=performance.now();events.frame(desired);container.dataset.playhead=String(desired);container.dataset.playing='true';void pump();}}};
 video.addEventListener('ended',()=>{intent=false;events.frame(Math.max(0,duration-1/fps));});
 video.addEventListener('error',()=>{if(!closed)events.error(Error(video.error?.message||'分段媒体解码失败'));});
 raf=requestAnimationFrame(tick);void open(initial);
 return {seek:seconds=>{if(contains(seconds)){desired=seconds;video.currentTime=seconds;}else void open(seconds);},toggle:()=>{intent=!intent;if(intent){if(video.ended||video.currentTime>=duration-1/fps)void open(0);else void video.play().catch(events.error);}else video.pause();},pause:()=>{intent=false;video.pause();},close:()=>{closed=true;intent=false;generation++;abort.abort();cancelAnimationFrame(raf);disposeMedia(video);if(objectURL)URL.revokeObjectURL(objectURL);events.media(null,null);}};
}
