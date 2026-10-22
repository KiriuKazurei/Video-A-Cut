import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { App, Checkbox, Input, Space } from 'antd';
import { useProjectWorkspace } from './projectWorkspace';
import { timelineApi } from './timelineApi';
import { projectApi } from './projectApi';
import { desktopCommand, isDesktop } from './desktop';
import { errorText } from './components/ui';
import { activeTracks, frameLabel, fpsValue, linkedSelection, mediaTime, type TimelineOperation, type TimelineClip, type TimelineTrack, type TimelineView, type SequencePreview, type TimelineMarker } from './timelineContracts';

interface EditorContext {
 active:boolean; projectId:string; sequenceId:string; view?:TimelineView; loading:boolean; error:string; pending:boolean;
 selected:string[]; select:(ids:string[],separate?:boolean)=>void; selectedTrack:string; setSelectedTrack:(id:string)=>void;
 head:number; setHead:(frame:number,seek?:boolean)=>void; zoom:number; setZoom:(value:number)=>void; snap:boolean; setSnap:(value:boolean)=>void; tool:'select'|'razor'; setTool:(tool:'select'|'razor')=>void;
 command:(operations:TimelineOperation[],label:string)=>Promise<void>; switchSequence:(id:string)=>void; manageSequence:(action:string,id?:string)=>Promise<void>;
 marker:(kind?:'point'|'in'|'out',source?:boolean)=>void; addResource:(id:string,track?:string,at?:number,mode?:string)=>Promise<void>;
 copy:(cut?:boolean)=>void; paste:()=>Promise<void>; remove:(ripple?:boolean)=>Promise<void>; split:()=>Promise<void>;
 program:RefObject<HTMLVideoElement|null>; source:RefObject<HTMLVideoElement|null>; play:()=>void;
 preview?:SequencePreview; previewURL?:string; previewLoading:boolean; previewError:string; refreshPreview:()=>void; exportVideo:()=>Promise<void>;
}
const Context=createContext<EditorContext|null>(null);
type ClipboardTrack={kind:TimelineTrack['kind'];offset:number;clips:TimelineClip[]};
type SequenceUI={head:number;zoom:number};
export function timelineKeyAllowed(target:EventTarget|null) {return !(target instanceof HTMLElement)||!target.closest('input,textarea,select,[contenteditable=true],[role=combobox],[role=listbox],[role=menu],[role=slider],[role=tab],.ant-modal');}

export function SequenceEditorProvider({active,children}:{active:boolean;children:ReactNode}) {
 const project=useProjectWorkspace(),client=useQueryClient(),{message,modal}=App.useApp();
 const session=project.session,doc=session?.document,projectId=doc?.project.id ?? '';
 const [wantedSequence,setWantedSequence]=useState('');
 const sequenceId=doc?.sequences?.some(s=>s.id===wantedSequence)?wantedSequence:doc?.sequences?.find(s=>s.id===doc.workspace.active_sequence)?.id ?? doc?.sequences?.[0]?.id ?? '';
 const key=['timeline',session?.id,projectId,sequenceId];
 const query=useQuery({queryKey:key,queryFn:({signal})=>timelineApi.get(projectId,sequenceId,signal),enabled:!!sequenceId && active,refetchInterval:active?1500:false});
 const view=query.data,sequence=view?.sequence;
 const [selected,setSelected]=useState<string[]>([]),[selectedTrack,setSelectedTrack]=useState('');
 const [localUI,setLocalUI]=useState<{session:string;states:Record<string,SequenceUI>}>({session:'',states:{}}),[snap,setSnap]=useState(true),[tool,setTool]=useState<'select'|'razor'>('select');
 const states=localUI.session===session?.id?localUI.states:{},savedStates=doc?.workspace.sequence_states as Record<string,SequenceUI>|undefined;
 const position=states[sequenceId] ?? savedStates?.[sequenceId] ?? {head:0,zoom:60},head=position.head,zoom=position.zoom;
 const updatePosition=(patch:Partial<SequenceUI>)=>setLocalUI(previous=>{const current=previous.session===session?.id?previous.states:{};return {session:session?.id??'',states:{...current,[sequenceId]:{...(current[sequenceId]??savedStates?.[sequenceId]??{head:0,zoom:60}),...patch}}};});
 const setZoom=(value:number)=>updatePosition({zoom:value});
 const [pending,setPending]=useState(false),[error,setError]=useState(''),[settledVersion,setSettledVersion]=useState('');
 const busy=useRef(false),currentSequence=useRef(sequenceId),clipboard=useRef<ClipboardTrack[]>([]),clipboardRate=useRef(30);
 const program=useRef<HTMLVideoElement>(null),source=useRef<HTMLVideoElement>(null),focused=useRef(false);
 const stopEditorPlayers=()=>{program.current?.pause();source.current?.pause();};
 const headRate=useRef({session:session?.id,id:sequenceId,rate:sequence?fpsValue(sequence.fps):0});
 useEffect(()=>{const rate=sequence?fpsValue(sequence.fps):0,previous=headRate.current;if(previous.session===session?.id&&previous.id===sequenceId&&previous.rate>0&&rate>0&&rate!==previous.rate)updatePosition({head:Math.round(head*rate/previous.rate)});headRate.current={session:session?.id,id:sequenceId,rate};},[session?.id,sequenceId,sequence?.fps.num,sequence?.fps.den]);
 useEffect(()=>{clipboard.current=[];setWantedSequence('');},[session?.id]);
 const setHead=(value:number,seek=true)=>{const frame=Math.max(0,Math.round(value));updatePosition({head:frame});if(seek && program.current && sequence)program.current.currentTime=frame/fpsValue(sequence.fps);};
 useEffect(()=>{
  stopEditorPlayers();setSelected([]);setSelectedTrack('');setError('');currentSequence.current=sequenceId;
  return ()=>{stopEditorPlayers();void client.cancelQueries({queryKey:['timeline',session?.id,projectId,sequenceId]});void client.cancelQueries({queryKey:['sequence-preview',session?.id,projectId,sequenceId]});};
 },[session?.id,sequenceId,active]);
 useEffect(()=>{const focus=(event:FocusEvent)=>{const target=event.target;if(target instanceof HTMLElement && !target.closest('.project-menu-bar,[role=menu]')) focused.current=!!target.closest('.sequence-timeline,.editor-monitor,.timeline-inspector');};document.addEventListener('focusin',focus);return()=>document.removeEventListener('focusin',focus);},[]);
 useEffect(()=>{setSettledVersion('');const timer=setTimeout(()=>setSettledVersion(view?.content_version ?? ''),350);return()=>clearTimeout(timer);},[view?.content_version]);
 const renderedTracks=sequence?activeTracks(sequence):[];
 const nonempty=renderedTracks.some(t=>t.clips.length);
 const previewKey=['sequence-preview',session?.id,projectId,sequenceId,view?.content_version];
 const previewQuery=useQuery({queryKey:previewKey,queryFn:({signal})=>timelineApi.preview(projectId,sequenceId,{session:session!.id,expected_revision:view!.sequence.revision,content_version:view!.content_version},signal),enabled:active && nonempty && !!settledVersion && settledVersion===view?.content_version,retry:1,staleTime:Infinity});
 const sourceUnavailable=renderedTracks.some(t=>t.clips.some(c=>!doc?.resources.some(r=>r.id===c.resource_id&&!r.changed&&!r.missing&&!r.removed)));
 const preview=!sourceUnavailable&&previewQuery.data?.content_version===view?.content_version?previewQuery.data:undefined;
 const select=(ids:string[],separate=false)=>{setSelected(sequence?linkedSelection(sequence,ids,separate):ids);const resource=sequence?.tracks.flatMap(t=>t.clips).find(c=>c.id===ids[0])?.resource_id;if(resource&&resource!==project.selectedId)void project.selectResource(resource);};
 const command=async(operations:TimelineOperation[],label:string)=>{
  if(!view || !session || busy.current)return;busy.current=true;setPending(true);setError('');const owner=sequenceId;
  try{await project.flushEditors();program.current?.pause();const next=await timelineApi.command(projectId,owner,{session:session.id,expected_revision:view.sequence.revision,idempotency_key:crypto.randomUUID(),label,operations});if(currentSequence.current===owner){client.setQueryData(key,next);await project.refresh();void client.invalidateQueries({queryKey:['script-workbench']});void client.invalidateQueries({queryKey:['script-view']});void client.invalidateQueries({queryKey:['reference-directory']});}}
  catch(reason){setError(errorText(reason));void query.refetch();throw reason;}
  finally{busy.current=false;setPending(false);}
 };
 const persistUI=()=>project.setWorkspaceState({active_sequence:sequenceId,sequence_states:{...savedStates,...states}});
 const persisted=(doc?.workspace.sequence_states as Record<string,{head:number;zoom:number}> | undefined)?.[sequenceId];
 useEffect(()=>project.registerEditor('sequence-ui',{dirty:!!sequenceId&&active&&((persisted?.head??0)!==head||(persisted?.zoom??60)!==zoom),flush:persistUI}),[sequenceId,head,zoom,persisted?.head,persisted?.zoom,session?.id,active]);
 const switchSequence=(id:string)=>{if(id===sequenceId)return;void project.flushEditors().then(async()=>{await persistUI();stopEditorPlayers();setWantedSequence(id);await project.setWorkspaceState({active_sequence:id,sequence_states:{...savedStates,...states}});}).catch(reason=>setError(errorText(reason)));};
 const manageSequence=async(action:string,id?:string)=>{
  if(!session)return;try{await project.flushEditors();const current=await projectApi.workspace();
  const next=await timelineApi.manage(projectId,{session:current.id,expected_revision:current.revision,idempotency_key:crypto.randomUUID(),action,sequence_id:id});
  await project.refresh();if(action!=='delete')setWantedSequence(next.document?.sequences.at(-1)?.id ?? '');
  }catch(reason){setError(errorText(reason));throw reason;}
 };
 const addResource=async(id:string,track?:string,at=head,mode='overwrite')=>{
  if(!sequence)return;const resource=doc?.resources.find(r=>r.id===id);if(!resource)throw Error('资源不属于当前工程');
  const kind=resource.kind==='audio'?'audio':resource.kind==='subtitle'?'subtitle':'video';
  const target=track?sequence.tracks.find(t=>t.id===track):sequence.tracks.find(t=>t.id===selectedTrack&&t.kind===kind&&!t.locked) ?? sequence.tracks.find(t=>t.kind===kind && !t.locked && !t.group_id);
  if((!target&&kind!=='subtitle') || (target&&(target.kind!==kind||target.locked))) {const reason=Error('请选择匹配的未锁定轨道');setError(reason.message);throw reason;}
  await command([{type:'clip.insert',resource_id:id,track_id:target?.id,at,mode}],'拖入资源');
 };
 const marker=(kind:'point'|'in'|'out'='point',isSource=false)=>{
  if(!sequence)return;let notes=`${kind==='in'?'入点':kind==='out'?'出点':'标记'} ${frameLabel(head,sequence.fps)}`,locked=false;
  modal.confirm({title:isSource?'添加素材标记':'添加时间线标记',content:<Space direction="vertical" style={{width:'100%'}}><span>备注就是脚本识别名，可用 {'{@识别名}'} 引用。</span><Input aria-label="标记备注" defaultValue={notes} onChange={event=>{notes=event.target.value;}}/>{!isSource&&<Checkbox onChange={event=>{locked=event.target.checked;}}>锁定绝对时码</Checkbox>}</Space>,onOk:async()=>{
   const selectedClip=sequence.tracks.flatMap(t=>t.clips).find(c=>selected.includes(c.id));const resource=isSource ? doc?.resources.find(r=>r.id===(selectedClip?.resource_id ?? project.resource?.id)):undefined;
   const time=isSource?(source.current&&resource&&source.current.dataset.resourceId===resource.id ? source.current.currentTime : (selectedClip ? selectedClip.source_in.value/selectedClip.source_in.scale + (head-selectedClip.at)/fpsValue(sequence.fps)*selectedClip.speed : 0)):undefined;
   if(isSource && !resource)throw Error('请选择源素材');
   const value:TimelineMarker={id:'',notes,kind,frame:head,locked,...(time!==undefined?{source_time:mediaTime(time)}:{})};
   await command([{type:'marker.add',marker:value,resource_id:resource?.id}],'添加标记');
  }});
 };
 const copy=(cut=false)=>{
  if(!sequence || !selected.length)return;const chosen=sequence.tracks.flatMap(t=>t.clips).filter(c=>selected.includes(c.id)),start=Math.min(...chosen.map(c=>c.at));
  const first:Partial<Record<TimelineTrack['kind'],number>>={},counts:Partial<Record<TimelineTrack['kind'],number>>={};clipboard.current=[];clipboardRate.current=fpsValue(sequence.fps);
  sequence.tracks.forEach(t=>{const i=counts[t.kind]??0;counts[t.kind]=i+1;const clips=t.clips.filter(c=>selected.includes(c.id));if(!clips.length)return;first[t.kind]??=i;clipboard.current.push({kind:t.kind,offset:i-first[t.kind]!,clips:clips.map(c=>({...structuredClone(c),at:c.at-start}))});});
  if(cut)void command([{type:'clip.delete',clip_ids:selected}],'剪切片段').catch(()=>{});else void message.info('已复制所选片段');
 };
 const paste=async()=>{
  if(!sequence || !clipboard.current.length)return;const operations:TimelineOperation[]=[],links=new Map<string,string>();
  const ratio=fpsValue(sequence.fps)/clipboardRate.current,frame=(v:number)=>Math.round(v*ratio);
  for(const row of clipboard.current){const tracks=sequence.tracks.filter(t=>t.kind===row.kind && !t.group_id),target=tracks[row.offset];if(!target)throw Error('请先创建足够的对应轨道，粘贴未执行');operations.push({type:'clip.paste',track_id:target.id,at:head,clips:row.clips.map(c=>{if(c.link_id&&!links.has(c.link_id))links.set(c.link_id,crypto.randomUUID());return {...c,at:frame(c.at),duration:Math.max(1,frame(c.duration)),effects:{...c.effects,fade_in:frame(c.effects.fade_in),fade_out:frame(c.effects.fade_out),transition:frame(c.effects.transition),from_frame:c.effects.from_frame===undefined?undefined:frame(c.effects.from_frame),to_frame:c.effects.to_frame===undefined?undefined:frame(c.effects.to_frame)},link_id:c.link_id?links.get(c.link_id):undefined};})});}
  await command(operations,'粘贴片段');
 };
 const remove=(ripple=false)=>command([{type:'clip.delete',clip_ids:selected,mode:ripple?'ripple':'delete'}],ripple?'波纹删除':'删除片段');
 const split=()=>command([{type:'clip.split',clip_ids:selected.length?selected:sequence?.tracks.flatMap(t=>t.clips).filter(c=>c.at<head&&c.at+c.duration>head).map(c=>c.id),at:head}],'分割片段');
 const play=()=>{const media=program.current;if(!media || !preview)return;if(media.paused)void media.play().catch(reason=>setError(errorText(reason)));else media.pause();};
 const exportVideo=async()=>{try{if(!view || !session)return;await project.flushEditors();let path:string|undefined;if(isDesktop())path=(await desktopCommand<string[]>('project_dialog',{mode:'save-video',defaultPath:`${sequence?.name ?? '序列'}.mp4` }))[0];else path=window.prompt('导出 MP4 的绝对路径') ?? undefined;if(!path)return;await timelineApi.export(projectId,sequenceId,{session:session.id,expected_revision:view.sequence.revision,content_version:view.content_version,path});void message.success('已导出当前轨道范围');}catch(reason){setError(errorText(reason));throw reason;}};
 useEffect(()=>{
  if(!active)return;
  const dispatch=(commandName:string)=>{const actions:Record<string,()=>unknown>={undo:()=>command([{type:'history.undo'}],'撤销'),redo:()=>command([{type:'history.redo'}],'重做'),copy:()=>copy(),cut:()=>copy(true),paste};const result=actions[commandName]?.();if(result instanceof Promise)void result.catch(()=>{});};
  const onMenu=(event:Event)=>{if(event.defaultPrevented||!focused.current)return;event.preventDefault();dispatch((event as CustomEvent<string>).detail);};
  const onKey=(event:KeyboardEvent)=>{
   if(event.defaultPrevented || event.isComposing || !timelineKeyAllowed(event.target) || !focused.current || [...document.querySelectorAll<HTMLElement>('.ant-modal-wrap,.script-completion')].some(el=>el.getClientRects().length>0))return;
   const key=event.key.toLowerCase(),ctrl=event.ctrlKey||event.metaKey;let action:(()=>unknown)|undefined;
   if(!ctrl&&[' ','arrowleft','arrowright'].includes(key)&&event.target instanceof HTMLElement&&event.target.closest('button,a[href]'))return;
   if(ctrl){if(key==='k')action=split;else if(['c','v','x','z','y'].includes(key))action=()=>dispatch(key==='c'?'copy':key==='v'?'paste':key==='x'?'cut':key==='y'||event.shiftKey?'redo':'undo');}
   else if(key==='v')action=()=>setTool('select');else if(key==='c')action=()=>setTool('razor');else if(key==='m')action=()=>marker();else if(key==='i'||key==='o')action=()=>marker(key==='i'?'in':'out');else if(key===' ')action=play;else if(key==='arrowleft'||key==='arrowright')action=()=>setHead(head+(key==='arrowright'?1:-1));else if(key==='delete')action=()=>remove(event.shiftKey);
   if(action){event.preventDefault();const result=action();if(result instanceof Promise)void result.catch(()=>{});}
  };
  window.addEventListener('keydown',onKey);window.addEventListener('vac:edit-command',onMenu);return()=>{window.removeEventListener('keydown',onKey);window.removeEventListener('vac:edit-command',onMenu);};
 });
 const value:EditorContext=useMemo(()=>({active,projectId,sequenceId,view,loading:query.isPending,error:error || (query.error?errorText(query.error):''),pending,selected,select,selectedTrack,setSelectedTrack,head,setHead,zoom,setZoom,snap,setSnap,tool,setTool,command,switchSequence,manageSequence,marker,addResource,copy,paste,remove,split,program,source,play,preview,previewURL:preview?timelineApi.previewURL(projectId,sequenceId,preview.content_version):undefined,previewLoading:nonempty && (!settledVersion || previewQuery.isFetching),previewError:sourceUnavailable?'源素材缺失或发生变化，请在资源管理重新定位或确认后更新预览。':previewQuery.error?errorText(previewQuery.error):'',refreshPreview:()=>{void client.invalidateQueries({queryKey:previewKey});},exportVideo}),[active,projectId,sequenceId,view,query.isPending,query.error,error,pending,selected,selectedTrack,head,zoom,snap,tool,preview,previewQuery.isFetching,previewQuery.error,settledVersion,sourceUnavailable,project]);
 return <Context.Provider value={value}>{children}</Context.Provider>;
}
export function useSequenceEditor(){const context=useContext(Context);if(!context)throw Error('序列编辑器尚未初始化');return context;}
