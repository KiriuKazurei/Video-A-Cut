import { useEffect, useRef } from 'react';
import { useSequenceEditor } from '../sequenceEditor';
import { projectApi } from '../projectApi';
import { timelineApi } from '../timelineApi';
import { directProgramPlayer, segmentedProgramPlayer } from '../programPlayback';
import { fpsValue } from '../timelineContracts';

export function ProgramPlayback() {
 const e=useSequenceEditor(),container=useRef<HTMLDivElement>(null),latest=useRef(e);latest.current=e;
 const plan=e.playbackPlan;
 useEffect(()=>{
  if(!e.active||!plan||!container.current||plan.mode==='cached')return;
  const version=plan.content_version,root=container.current;
  const current=()=>latest.current.view?.content_version===version;
  const events={frame:(seconds:number)=>{if(current())latest.current.setHead(Math.round(seconds*fpsValue(plan.fps)),false);},ready:(ready:boolean)=>{if(current())latest.current.setPlaybackReady(ready);},media:(video:HTMLVideoElement|null,audio:HTMLMediaElement|null)=>{e.program.current=video;if(current())latest.current.setPlaybackMeter(audio);},error:(error:unknown)=>{if(current())latest.current.playbackError(error);}};
  const initial=latest.current.head/fpsValue(plan.fps);
  try{
   const player=plan.mode==='direct'?directProgramPlayer(root,plan,id=>projectApi.contentURL(e.projectId,id),events,initial):segmentedProgramPlayer(root,plan,async(frame,signal)=>{
    const view=latest.current.view!;const report=await timelineApi.segment(e.projectId,e.sequenceId,{session:view.session,expected_revision:view.sequence.revision,content_version:version,start_frame:frame},signal);
    const response=await fetch(timelineApi.segmentURL(e.projectId,e.sequenceId,version,report.key),{signal});if(!response.ok)throw Error(`分段读取失败（${response.status}）`);return {report,bytes:await response.arrayBuffer()};
   },events,initial);
   e.playback.current=player;
   return()=>{if(e.playback.current===player)e.playback.current=null;player.close();};
  }catch(error){events.error(error);}
 },[plan?.content_version,plan?.mode,e.playbackAttempt,e.active]);
 return <div className="program-playback" ref={container}/>;
}
