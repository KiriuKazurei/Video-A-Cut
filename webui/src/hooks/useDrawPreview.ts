import {useEffect,useRef,useState} from 'react';
import type {DrawComposition,DrawPreviewFrame} from '../drawContracts';
import {drawApi} from '../drawApi';
import {errorText} from '../components/ui';

// One in-flight request and one newest target: dragging never builds a backlog
// or repeatedly kills/recreates the persistent browser.
export function useDrawPreview(project:string,session:string,composition:DrawComposition|undefined,frame:number,enabled:boolean){
 const [picture,setPicture]=useState<DrawPreviewFrame>(),[state,setState]=useState<'empty'|'compiling'|'loading'|'updating'|'current'|'error'>('empty'),[error,setError]=useState('');
 const latest=useRef<{c:DrawComposition;frame:number;serial:number;signature:string}|undefined>(undefined),serial=useRef(0),wake=useRef<()=>void>(()=>{});
 useEffect(()=>{
  const id=composition?.id;if(!project||!session||!id||!enabled){setState('empty');return;}
  let disposed=false,running=false,last=-1,timer:ReturnType<typeof setTimeout>;
  const controller=new AbortController();let activeRequest='';setPicture(undefined);
  const progress=(event:Event)=>{const d=(event as CustomEvent).detail;if(d?.project_id===project&&d.session_id===session&&d.composition_id===id&&d.request_id===activeRequest&&['compiling','loading','updating'].includes(d.phase))setState(d.phase);};
  window.addEventListener('vac:draw-preview-state',progress);
  const pump=async()=>{
   const target=latest.current;
   if(disposed||running||!target?.c.document||target.c.id!==id||target.serial===last)return;
   running=true;last=target.serial;setState('updating');setError('');
   try{
    activeRequest=crypto.randomUUID();
    const result=await drawApi.preview(project,id,{session,content_version:target.c.content_version,request_id:activeRequest,frame:target.frame,document:target.c.document},controller.signal);
    if(!disposed&&latest.current?.signature===target.signature){setPicture(result);setState(latest.current.serial===target.serial?'current':'updating');}
   }catch(e){if(!disposed&&latest.current?.signature===target.signature){setError(errorText(e));setState('error');}}
   finally{running=false;if(!disposed&&latest.current?.serial!==last)timer=setTimeout(()=>void pump(),0);}
  };
  wake.current=()=>{clearTimeout(timer);timer=setTimeout(()=>void pump(),100);};wake.current();
  return ()=>{disposed=true;window.removeEventListener('vac:draw-preview-state',progress);clearTimeout(timer);controller.abort();wake.current=()=>{};void drawApi.closePreview(project,id,session).catch(()=>{});};
 },[project,session,composition?.id,enabled]);
 useEffect(()=>{
  if(!composition)return;
  latest.current={c:composition,frame,serial:++serial.current,signature:composition.content_version+JSON.stringify(composition.document)};
  setState('updating');wake.current();
 },[composition,frame]);
 return {picture,state,error};
}
