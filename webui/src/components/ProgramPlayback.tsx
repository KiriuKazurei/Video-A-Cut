import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useSequenceEditor } from '../sequenceEditor';
import { projectApi } from '../projectApi';
import { timelineApi } from '../timelineApi';
import { directProgramPlayer, segmentedProgramPlayer } from '../programPlayback';
import { fpsValue, type PlaybackClip, type SequencePlayback } from '../timelineContracts';

export function ProgramPlayback() {
 const e=useSequenceEditor(),container=useRef<HTMLDivElement>(null),latest=useRef(e);latest.current=e;
 const client=useQueryClient(),plan=e.playbackPlan;
 useEffect(()=>{
  if(!e.active||!plan||!container.current||plan.mode==='cached')return;
  const version=plan.content_version,session=e.view?.session,root=container.current;
  const current=()=>latest.current.view?.session===session&&latest.current.view?.content_version===version;
  const events={frame:(seconds:number)=>{if(current())latest.current.setHead(Math.round(seconds*fpsValue(plan.fps)),false);},ready:(ready:boolean)=>{if(current())latest.current.setPlaybackReady(ready);},media:(video:HTMLVideoElement|null,audio:HTMLMediaElement|null)=>{e.program.current=video;if(current())latest.current.setPlaybackMeter(audio);},error:(error:unknown)=>{if(current())latest.current.playbackError(error);}};
  const initial=latest.current.head/fpsValue(plan.fps);
  const ready=new Set(plan.video.filter(clip=>clip.proxy_ready).map(clip=>clip.proxy_version));
  const failed=new Set<string>();
  let warming:AbortController|undefined;
  let warmTimer:ReturnType<typeof setTimeout>|undefined;
  let warmTarget='';
  const cancelWarm=()=>{
   clearTimeout(warmTimer);
   warming?.abort();
  };
  const proxy={
   url:(clip:PlaybackClip)=>clip.proxy_version&&ready.has(clip.proxy_version)
    ?projectApi.timelineProxyURL(e.projectId,clip.resource_id,clip.proxy_version):undefined,
   warm:(currentClip:PlaybackClip|undefined,nextClip:PlaybackClip|undefined)=>{
    const clips=[currentClip,nextClip];
    const target=clips.map(clip=>clip?.proxy_version??'').join(':');
    if(target===warmTarget)return;
    warmTarget=target;
    cancelWarm();
    if(!clips.some(clip=>clip?.proxy_version&&!ready.has(clip.proxy_version)&&!failed.has(clip.proxy_version)))return;
    const controller=new AbortController();
    warming=controller;
    warmTimer=setTimeout(async()=>{
     for(const clip of clips){
      const version=clip?.proxy_version;
      if(controller.signal.aborted||!current())return;
      if(!clip||!version||ready.has(version)||failed.has(version))continue;
      try{
       const result=await projectApi.createTimelineProxy(e.projectId,clip.resource_id,version,controller.signal);
       if(result.ready&&!controller.signal.aborted&&current()){
        ready.add(version);
        client.setQueryData<SequencePlayback>(['sequence-playback',session,e.projectId,e.sequenceId,plan.content_version],previous=>{
         if(!previous)return previous;
         return {...previous,video:previous.video.map(item=>item.proxy_version===version?{...item,proxy_ready:true}:item)};
        });
       }
      }catch{
       if(!controller.signal.aborted)failed.add(version);
      }
     }
    },650);
   }
  };
  try{
   const player=plan.mode==='direct'?directProgramPlayer(root,plan,id=>projectApi.contentURL(e.projectId,id),events,initial,proxy):segmentedProgramPlayer(root,plan,async(frame,signal)=>{
    const view=latest.current.view!;const report=await timelineApi.segment(e.projectId,e.sequenceId,{session:view.session,expected_revision:view.sequence.revision,content_version:version,start_frame:frame},signal);
    const response=await fetch(timelineApi.segmentURL(e.projectId,e.sequenceId,version,report.key),{signal});if(!response.ok)throw Error(`分段读取失败（${response.status}）`);return {report,bytes:await response.arrayBuffer()};
   },events,initial);
   e.playback.current=player;
   return()=>{cancelWarm();if(e.playback.current===player)e.playback.current=null;player.close();};
  }catch(error){events.error(error);}
 },[plan?.content_version,plan?.mode,e.view?.session,e.projectId,e.sequenceId,e.playbackAttempt,e.active,client]);
 return <div className="program-playback" ref={container}/>;
}
