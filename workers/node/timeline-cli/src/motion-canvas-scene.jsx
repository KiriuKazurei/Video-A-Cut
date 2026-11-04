import React, {useLayoutEffect,useRef} from 'react';
import * as Core from '@motion-canvas/core';
import * as TwoD from '@motion-canvas/2d';
import * as JSX from '@motion-canvas/2d/lib/jsx-runtime';
import {ReadOnlyTimeEvents} from '@motion-canvas/core/lib/scenes/timeEvents';

window.__vacMotionCore=Core;window.__vacMotion2D=TwoD;window.__vacMotionJSX=JSX;
const scenes=new Map();
window.vacInstallMotion=async(id,description,spec)=>{
 const logger=new Core.Logger();
 logger.onLogged.subscribe(entry=>{if(entry.level==='error')console.error(entry.message);});
 const playback=new Core.PlaybackManager(),status=new Core.PlaybackStatus(playback);
 playback.fps=spec.fps.num/spec.fps.den;playback.state=Core.PlaybackState.Rendering;
 description.meta.seed.set(spec.seed??0);
 const context=new Core.SharedWebGLContext(logger);
 const scene=new description.klass({...description,name:id,logger,playback:status,size:new Core.Vector2(spec.width,spec.height),resolutionScale:1,timeEventsClass:ReadOnlyTimeEvents,sharedWebGLContext:context,experimentalFeatures:false});
 scene.variables.updateSignals(spec.variables??{});
 playback.setup([scene]);await playback.recalculate();await playback.reset();
 const stage=new Core.Stage();stage.configure({size:new Core.Vector2(spec.width,spec.height),resolutionScale:1,colorSpace:'srgb',background:null});
 scenes.get(id)?.context.dispose();scenes.set(id,{playback,stage,context});
};
window.vacRemoveMotion=id=>{scenes.get(id)?.context.dispose();scenes.delete(id);};
export function MotionSource({sourceId,frame,requestKey,id,acknowledge}){
 const canvas=useRef(null);
 useLayoutEffect(()=>{
  let live=true;
  void(async()=>{
   const host=scenes.get(sourceId);if(!host)throw Error('Motion Canvas source not installed');
   await host.playback.seek(frame);await host.stage.render(host.playback.currentScene,host.playback.previousScene);
   if(!live)return;
   const target=canvas.current;target.width=host.stage.finalBuffer.width;target.height=host.stage.finalBuffer.height;
   target.getContext('2d').drawImage(host.stage.finalBuffer,0,0);acknowledge(requestKey,id);
  })().catch(error=>acknowledge(requestKey,id,error.message));
  return()=>{live=false;};
 },[sourceId,frame,requestKey,id,acknowledge]);
 return <canvas ref={canvas} style={{width:'100%',height:'100%'}}/>;
}
