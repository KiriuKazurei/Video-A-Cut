import {useEffect,useMemo,useRef,useState,type DragEvent,type PointerEvent} from 'react';
import {useQuery,useQueryClient} from '@tanstack/react-query';
import {Alert,Button,Collapse,Empty,Flex,Input,InputNumber,Select,Slider,Space,Tabs,Tag,Typography} from 'antd';
import {PlusOutlined,SaveOutlined,PlayCircleOutlined,PauseOutlined,UndoOutlined,RedoOutlined,FullscreenOutlined,FullscreenExitOutlined,StepBackwardOutlined,StepForwardOutlined} from '@ant-design/icons';
import {useProjectWorkspace} from '../projectWorkspace';
import {useSequenceEditor} from '../sequenceEditor';
import {useSkillWorkspace} from '../skillWorkspace';
import {drawNodeRange,drawFrameContext} from '../../../contracts/draw-time.mjs';
import {useDrawPreview} from '../hooks/useDrawPreview';
import {projectApi} from '../projectApi';
import {skillApi} from '../skillApi';
import {timelineApi} from '../timelineApi';
import {api} from '../api';
import {drawApi} from '../drawApi';
import {desktopCommand,isDesktop} from '../desktop';
import {errorText} from '../components/ui';
import type {RouteState} from '../navigation';
import {DRAW_ZONES,drawAgentStatus,drawDraftChanged,newDrawComposition,newDrawImage,unifiedComposition,reconcileDrawDraft,acceptDrawSave,type DrawComposition,type DrawDocument,type DrawNode,type DrawOutputFormat} from '../drawContracts';

interface Props{route:RouteState;navigate:(state:Partial<RouteState>)=>void}
const engineLabels={hyperframes:'HyperFrames',remotion:'Remotion'};
const kindLabels={source:'画面',image:'图片',text:'文字',group:'组',mask:'遮罩',shape:'形状'};
export function DrawWorkspace({route,navigate}:Props){
 const project=useProjectWorkspace(),editor=useSequenceEditor(),skills=useSkillWorkspace(),client=useQueryClient(),session=project.session;
 const id=session?.document?.project.id??'',active=route.workspace==='draw',blocked=!!route.drawProject&&route.drawProject!==id;
 const query=useQuery({queryKey:['draw-compositions',session?.id,id],queryFn:({signal})=>drawApi.list(id,signal),enabled:active&&!!id});
 const profiles=useQuery({queryKey:['processing-profiles'],queryFn:api.listProfiles,enabled:active});
 const list=useMemo(()=>(query.data??[]).map(unifiedComposition),[query.data]);
 const selected=list.find(c=>c.id===route.drawSlot)??list[0];
 const runID=skills.runId||route.drawRun||'';
 const agentRun=useQuery({queryKey:['vu-run',id,runID],queryFn:({signal})=>skillApi.run(runID,signal),enabled:active&&!!id&&!!runID,refetchInterval:q=>['succeeded','failed','cancelled'].includes(q.state.data?.state??'')?false:2000});
 const [statusClock,setStatusClock]=useState(Date.now());
 useEffect(()=>{if(!active||!runID||['succeeded','failed','cancelled'].includes(agentRun.data?.state??''))return;const timer=setInterval(()=>setStatusClock(Date.now()),1000);return()=>clearInterval(timer);},[active,runID,agentRun.data?.state]);
 const serverHistory=useQuery({queryKey:['draw-history',session?.id,id,selected?.id,selected?.revision],queryFn:()=>drawApi.history(id,selected!.id),enabled:active&&!!selected?.id});
 const [draft,setDraft]=useState<DrawComposition>(),[nodeID,setNodeID]=useState(''),[head,setHead]=useState(0),[playing,setPlaying]=useState(false);
 const [busy,setBusy]=useState(''),[failure,setFailure]=useState(''),[notice,setNotice]=useState(''),[goal,setGoal]=useState(''),[previewOnly,setPreviewOnly]=useState(false),[sourceID,setSourceID]=useState(''),[tab,setTab]=useState('layers');
 const [execution,setExecution]=useState('auto'),[historyVersion,setHistoryVersion]=useState(0);
 const [outputFormat,setOutputFormat]=useState<DrawOutputFormat>('mp4');
 type ViewState={head:number;node:string;source:string;tab:string;preview_only:boolean;output_format:DrawOutputFormat};
 const viewState:ViewState={head,node:nodeID,source:sourceID,tab,preview_only:previewOnly,output_format:outputFormat};
 const viewStates=(session?.document?.workspace.draw_states??{}) as Record<string,ViewState>;
 const latestDraft=useRef<DrawComposition|undefined>(undefined),saveFlight=useRef<Promise<DrawComposition>|undefined>(undefined);latestDraft.current=draft;
 const baseline=useRef<DrawComposition|undefined>(undefined);
 const drag=useRef<{id:string;pointer:number;x:number;y:number;ratio:number;resize:boolean;before:DrawComposition;moved:boolean}|undefined>(undefined);
 const history=useRef<{past:DrawComposition[];future:DrawComposition[]}>({past:[],future:[]}),base=useRef(''),busyRef=useRef(false),canvas=useRef<HTMLDivElement>(null);
 const document=draft?.document,dirty=drawDraftChanged(draft,baseline.current),node=document?.nodes.find(n=>n.id===nodeID),source=document?.sources.find(s=>s.id===(sourceID||node?.source_id))??document?.sources[0];
 const timeKey=JSON.stringify({duration:document?.duration_frames,nodes:document?.nodes.map(n=>({id:n.id,kind:n.kind,parent_id:n.parent_id,from:n.from,duration:n.duration,source_offset:n.source_offset,speed:n.speed,hidden:n.hidden}))});
 const ranges=useMemo(()=>document?new Map(document.nodes.map(n=>[n.id,drawNodeRange(document,n.id)])):new Map<string,{from:number;to:number}>(),[timeKey]);
 const resources=(session?.document?.resources??[]).filter(r=>r.kind==='image'&&!r.removed);
 const fps=draft?draft.fps.num/draft.fps.den:30,canUse=!!id&&!blocked&&!busy;
 useEffect(()=>{
  const scope=(session?.id??'')+':'+(selected?.id??'');
  if(base.current!==scope){const saved=selected?viewStates[selected.id]:undefined;base.current=scope;drag.current=undefined;baseline.current=selected?structuredClone(selected):undefined;setDraft(selected?structuredClone(selected):undefined);setNodeID(saved?.node??'');setHead(saved?.head??0);setSourceID(saved?.source??'');setTab(saved?.tab??'layers');setPreviewOnly(saved?.preview_only??false);setOutputFormat(saved?.output_format??'mp4');setPlaying(false);history.current={past:[],future:[]};setHistoryVersion(n=>n+1);}
  else if(selected&&draft&&baseline.current){const result=reconcileDrawDraft(draft,baseline.current,selected);baseline.current=result.baseline;if(result.draft!==draft){if(!result.conflict&&draft.revision!==selected.revision){history.current={past:[],future:[]};setHistoryVersion(n=>n+1);}setDraft(result.draft);}}

 },[selected?.id,selected?.revision,selected?.content_version,session?.id]);
 const preview=useDrawPreview(id,session?.id??'',draft,head,active&&!blocked);
 useEffect(()=>{if(preview.state==='error')setPlaying(false);},[preview.state]);
 useEffect(()=>{if(!active)setPlaying(false);},[active]);
 useEffect(()=>{setHead(n=>Math.min(n,Math.max(0,(draft?.duration_frames??1)-1)));},[draft?.duration_frames]);
 useEffect(()=>{
  if(!playing)return;
  const start=performance.now(),at=head;
  const timer=setInterval(()=>{const frame=at+Math.floor((performance.now()-start)*fps/1000);if(frame>=(draft?.duration_frames??1)){setPlaying(false);setHead((draft?.duration_frames??1)-1);}else setHead(frame);},1000/12);
  return ()=>clearInterval(timer);
 },[playing,fps,draft?.duration_frames]);
 const edit=(fn:(c:DrawComposition)=>DrawComposition)=>setDraft(old=>{
  if(!old)return old;const next=fn(old);
  history.current.past=[...history.current.past.slice(-99),old];history.current.future=[];setHistoryVersion(n=>n+1);return next;
 });
 const patchDocument=(fn:(d:DrawDocument)=>DrawDocument)=>edit(c=>({...c,document:fn(c.document!)}));
 const patchNode=(value:Partial<DrawNode>)=>patchDocument(d=>({...d,nodes:d.nodes.map(n=>n.id===nodeID?{...n,...value}:n)}));
 const historyMove=(redo:boolean)=>{if(busyRef.current)return;const from=redo?history.current.future:history.current.past,to=redo?history.current.past:history.current.future,next=from.pop();if(next&&draft){to.push(draft);setDraft({...next,revision:draft.revision,content_version:draft.content_version});setHistoryVersion(n=>n+1);}else if(draft&&!dirty&&(redo?serverHistory.data?.can_redo:serverHistory.data?.can_undo)){run(redo?'重做构图':'撤销构图',async()=>{const current=await projectApi.workspace();await drawApi.moveHistory(id,draft.id,{session:session!.id,expected_revision:current.revision,composition_revision:draft.revision,idempotency_key:crypto.randomUUID(),action:redo?'redo':'undo'});await project.refresh();await query.refetch();});}};
 useEffect(()=>{
  if(!active)return;
  const command=(event:Event)=>{const action=(event as CustomEvent).detail;if(action==='undo'||action==='redo'){event.preventDefault();historyMove(action==='redo');}};
  const keyboard=(event:KeyboardEvent)=>{
   if(event.key==='Escape'&&drag.current){const gesture=drag.current;drag.current=undefined;if(canvas.current?.hasPointerCapture(gesture.pointer))canvas.current.releasePointerCapture(gesture.pointer);setDraft(gesture.before);event.preventDefault();return;}
   if(event.isComposing||event.defaultPrevented||event.target instanceof HTMLElement&&event.target.closest('input,textarea,select,[contenteditable=true],[role=dialog],[role=menu],.ant-select'))return;
   if((event.ctrlKey||event.metaKey)&&['z','y'].includes(event.key.toLowerCase())){event.preventDefault();historyMove(event.shiftKey||event.key.toLowerCase()==='y');}
   else if(event.key===' '&&!(event.target instanceof HTMLElement&&event.target.closest('button,[role=slider]'))){event.preventDefault();setPlaying(p=>!p);}
  };
  window.addEventListener('vac:edit-command',command);window.addEventListener('keydown',keyboard);
  return ()=>{window.removeEventListener('vac:edit-command',command);window.removeEventListener('keydown',keyboard);};
 },[active,draft,historyVersion,serverHistory.data,dirty]);
 const execute=async(label:string,fn:()=>Promise<void>)=>{if(busyRef.current)return;busyRef.current=true;setBusy(label);setFailure('');try{await fn();}catch(e){setFailure(errorText(e));}finally{busyRef.current=false;setBusy('');}};
 const run=(label:string,fn:()=>Promise<void>)=>void execute(label,fn);
 const persist=async():Promise<DrawComposition>=>{
  while(saveFlight.current)await saveFlight.current;
  const snapshot=latestDraft.current?structuredClone(latestDraft.current):undefined;
  if(!snapshot||!session||blocked)throw Error('请先打开当前工程并选择构图');
  if(!drawDraftChanged(snapshot,baseline.current)&&snapshot.id)return snapshot;
  const job=(async()=>{
   const current=await projectApi.workspace();if(current.id!==session.id)throw Error('工程已切换');
   const result=await drawApi.save(id,{session:session.id,expected_revision:current.revision,idempotency_key:crypto.randomUUID(),composition:snapshot});
   const saved=result.document?.draw_compositions?.find(c=>c.id===snapshot.id)??result.document?.draw_compositions?.at(-1);if(!saved)throw Error('构图保存响应无效');
   if(base.current.startsWith(session.id+':')){baseline.current=structuredClone(saved);setDraft(now=>now?acceptDrawSave(now,snapshot,saved):now);}
   await project.refresh();await query.refetch();return saved;
  })();
  saveFlight.current=job;try{return await job;}finally{if(saveFlight.current===job)saveFlight.current=undefined;}
 };

 const persistView=async()=>{
  if(!draft||!session)return;const current=await projectApi.workspace();if(current.id!==session.id)throw Error('工程已切换');
  const states=(current.document?.workspace.draw_states??{}) as Record<string,ViewState>;
  await project.setWorkspaceState({draw_states:{...states,[draft.id]:viewState}});
 };
 const viewDirty=!!draft&&base.current===session?.id+':'+draft.id&&JSON.stringify(viewStates[draft.id]??{head:0,node:'',source:'',tab:'layers',preview_only:false,output_format:'mp4'})!==JSON.stringify(viewState);
 useEffect(()=>project.registerEditor('draw-source',{dirty:dirty||viewDirty,flush:async()=>{await persist();await persistView();}}),[dirty,viewDirty,draft,session?.id,JSON.stringify(viewState)]);
 const exportVideo=async()=>{
  const c=await persist(),extension=outputFormat==='mp4'?'mp4':outputFormat==='webm-alpha'?'webm':'mov';
  if(isDesktop()){
   const mode=outputFormat==='mp4'?'save-draw-mp4':outputFormat==='webm-alpha'?'save-draw-webm':'save-draw-prores';
   const [path]=await desktopCommand<string[]>('project_dialog',{mode,defaultPath:c.name+'.'+extension});if(!path)return;
   await drawApi.export(id,c.id,{session:session!.id,content_version:c.content_version,format:outputFormat,path});
  }else{
   await drawApi.render(id,c.id,{session:session!.id,content_version:c.content_version,format:outputFormat});
   const a=window.document.createElement('a');a.href=drawApi.renderURL(id,c.id,c.content_version,outputFormat);a.download=c.name+'.'+extension;a.click();
  }
  setNotice('视频已导出 · '+(outputFormat==='mp4'?'MP4':outputFormat==='webm-alpha'?'WebM 透明视频':'ProRes 4444 透明视频'));
 };
 const choose=async(c:DrawComposition)=>{await project.flushEditors();setPlaying(false);navigate({workspace:'draw',drawProject:id,drawSlot:c.id,drawEngine:undefined,drawFocus:'canvas'});};
 const create=async()=>{
  await project.flushEditors();const current=await projectApi.workspace();if(current.id!==session?.id)throw Error('工程已切换');
  const result=await drawApi.save(id,{session:current.id,expected_revision:current.revision,idempotency_key:crypto.randomUUID(),composition:newDrawComposition()});
  await project.refresh();await query.refetch();await choose(result.document!.draw_compositions!.at(-1)!);
 };
 const addImage=(resourceID:string)=>{
  const r=resources.find(r=>r.id===resourceID);if(!draft||!r||r.missing||r.changed)throw Error('图片不可用，请在资源管理中重新定位');
  const im=newDrawImage(r,draft),n:DrawNode={id:im.id,name:r.name,kind:'image',resource_id:r.id,from:0,duration:draft.duration_frames,transform:{x:im.x,y:im.y,width:im.width,height:im.height},fit:'contain',z:(document?.nodes.length??0)+1};
  patchDocument(d=>({...d,nodes:[...d.nodes,n]}));setNodeID(n.id);setTab('layers');
 };
 const drop=(event:DragEvent)=>{if(!event.dataTransfer.types.includes('application/x-vac-resource'))return;event.preventDefault();run('加入图片',async()=>{const data=JSON.parse(event.dataTransfer.getData('application/x-vac-resource'));if(data.project!==id||data.session!==session?.id)throw Error('图片来自其他工程或已失效会话');addImage(data.resource);});};
 const addText=()=>{const n:DrawNode={id:'text_'+crypto.randomUUID(),name:'文字',kind:'text',text:'输入文字',color:'#ffffff',font_size:48,from:0,duration:draft!.duration_frames,transform:{x:80,y:80,width:400,height:120},z:document!.nodes.length+1};patchDocument(d=>({...d,nodes:[...d.nodes,n]}));setNodeID(n.id);};
 const addShape=()=>{const n:DrawNode={id:'shape_'+crypto.randomUUID(),name:'形状',kind:'shape',shape:'rect',color:'#4C8DFF',from:0,duration:draft!.duration_frames,transform:{x:160,y:120,width:240,height:160},z:document!.nodes.length+1};patchDocument(d=>({...d,nodes:[...d.nodes,n]}));setNodeID(n.id);};
 const addSource=(engine:'hyperframes'|'remotion')=>{
  const c=newDrawComposition(engine),sid='source_'+crypto.randomUUID(),nid='scene_'+crypto.randomUUID();
  patchDocument(d=>({...d,sources:[...d.sources,{id:sid,name:engineLabels[engine]+' 画面',engine,source:c.source,canvas:{width:c.width,height:c.height,fps:c.fps},duration_frames:c.duration_frames}],nodes:[...d.nodes,{id:nid,name:'画面',kind:'source',source_id:sid,from:0,duration:Math.min(d.duration_frames,c.duration_frames),transform:{x:0,y:0,width:640,height:360},z:d.nodes.length+1}]}));setNodeID(nid);setSourceID(sid);
 };
 const createRun=async()=>{
  const c=await persist(),visible=await skillApi.visibleSet(id),profile=profiles.data?.find(p=>p.content_mode==='configured'&&p.narration.endpoint);
  const builtin=execution==='builtin'||execution==='auto'&&!!profile;
  if(builtin&&!profile)throw Error('请在设置中配置可用的 Agent 服务商，或使用外部 MCP Agent');
  const run=await skillApi.createRun({project_id:id,sequence_id:editor.sequenceId,flow:'custom',skill_id:'vac-draw-pipeline',strategy:'draw',execution_mode:builtin?'builtin_api':'external_mcp',visible_set_sha:visible.sha256,goal:(goal.trim()||'完善当前画面，保持现有素材和未选中图层。')+'\n构图 ID：'+c.id+'；用途：'+c.document?.preset+'；默认自动协同，按图层选用合适能力；只修改必要节点。',idempotency_key:crypto.randomUUID(),...(builtin&&profile?{profile_id:profile.profile_id,profile_revision:profile.revision}:{})});
  const current=await projectApi.workspace();if(current.id!==session?.id||current.document?.project.id!==id)return;
  skills.setRunId(run.id);navigate({workspace:'draw',drawProject:id,drawSlot:c.id,drawRun:run.id,drawFocus:'canvas'});setNotice('');
  if(builtin)void skillApi.execute(run.id,run.version).then(async()=>{await project.refresh();await query.refetch();}).catch(e=>setFailure(errorText(e)));
 };
 const importVideo=async(sourceMode:'snapshot'|'follow'|'rendered'='snapshot')=>{const c=await persist(),sequence=editor.sequenceId;if(!sequence)throw Error('当前工程没有可用序列');const view=await timelineApi.get(id,sequence);await drawApi.import(id,c.id,{session:session!.id,content_version:c.content_version,sequence_id:sequence,expected_revision:view.sequence.revision,idempotency_key:crypto.randomUUID(),source_mode:sourceMode});await project.refresh();await client.invalidateQueries({queryKey:['timeline']});navigate({workspace:'editing',editingSection:'import'});};
 const numeric=(label:string,value:number,change:(value:number)=>void,min=-100000,max=100000)=><label>{label}<InputNumber aria-label={label} value={value} min={min} max={max} precision={label.includes("帧")||label.includes("分子")||label.includes("分母")||label==="层级"?0:2} disabled={!canUse} onChange={v=>v!==null&&change(v)}/></label>;
 const image=preview.picture,selectedBounds=image?.geometry.find(g=>g.id===nodeID);
 const seek=(frame:number)=>{setPlaying(false);setHead(frame);};
 const hit=(event:PointerEvent<HTMLDivElement>)=>{
  if(!document||!canvas.current||!image||preview.state!=='current')return;
  const area=canvas.current.getBoundingClientRect(),ratio=Math.min(area.width/document.canvas.width,area.height/document.canvas.height),width=document.canvas.width*ratio,height=document.canvas.height*ratio;
  const x=(event.clientX-area.x-(area.width-width)/2)/ratio,y=(event.clientY-area.y-(area.height-height)/2)/ratio;
  const hit=[...image.geometry].reverse().find(b=>x>=b.x&&x<b.x+b.width&&y>=b.y&&y<b.y+b.height);if(hit){setNodeID(hit.id);beginDrag(event,hit.id,false);}
 };
 const beginDrag=(event:PointerEvent,id:string,resize:boolean)=>{
  if(!draft||!canvas.current||!canUse||preview.state!=='current')return;
  event.preventDefault();
  const b=canvas.current.getBoundingClientRect(),ratio=Math.min(b.width/draft.width,b.height/draft.height);
  drag.current={id,pointer:event.pointerId,x:event.clientX,y:event.clientY,ratio,resize,before:structuredClone(draft),moved:false};canvas.current.setPointerCapture(event.pointerId);
 };
 const moveDrag=(event:PointerEvent)=>{
  const gesture=drag.current;if(!gesture||gesture.pointer!==event.pointerId)return;
  const dx=(event.clientX-gesture.x)/gesture.ratio,dy=(event.clientY-gesture.y)/gesture.ratio;
  if(Math.abs(dx)+Math.abs(dy)<3&&!gesture.moved)return;gesture.moved=true;
  const original=gesture.before.document!.nodes.find(n=>n.id===gesture.id)!;
  setDraft(old=>old?{...old,document:{...old.document!,nodes:old.document!.nodes.map(n=>n.id===gesture.id?{...n,transform:gesture.resize?{...n.transform,width:Math.max(8,(original.transform.width??gesture.before.width)+dx),height:Math.max(8,(original.transform.height??gesture.before.height)+dy)}:{...n.transform,x:original.transform.x+dx,y:original.transform.y+dy}}:n)}}:old);
 };
 const endDrag=(event:PointerEvent,cancel=false)=>{
  const gesture=drag.current;if(!gesture||gesture.pointer!==event.pointerId)return;drag.current=undefined;
  if(canvas.current?.hasPointerCapture(event.pointerId))canvas.current.releasePointerCapture(event.pointerId);
  if(cancel){setDraft(gesture.before);return;}
  if(gesture.moved){history.current.past=[...history.current.past.slice(-99),gesture.before];history.current.future=[];setHistoryVersion(n=>n+1);}
 };
 const taskStatus=agentRun.isError?'无法读取任务状态，请检查连接':agentRun.data?.project_id===id&&agentRun.data.strategy==='draw'?drawAgentStatus(agentRun.data,statusClock):runID&&agentRun.isPending?'正在读取任务状态':notice||'由 Agent 选择画面实现方式。';
 const [statusSummary,...statusDetails]=taskStatus.split(' · ');
 return <div className={'draw-studio unified-draw'+(previewOnly?' is-preview-only':'')} data-testid="draw-workspace" data-history-version={historyVersion}>
  <div className="draw-toolbar">
   <div className="draw-toolbar-heading"><Typography.Text strong>绘制</Typography.Text><Tag>自动协同</Tag><Select aria-label="创作用途" value={document?.preset??'auto'} disabled={!canUse||!document} onChange={preset=>patchDocument(d=>({...d,preset}))} options={[{value:'auto',label:'自动方案'},{value:'interface',label:'界面演示'},{value:'data',label:'数据讲解'},{value:'media',label:'素材包装'}]}/></div>
   <Space wrap className="draw-toolbar-actions"><Button icon={<PlusOutlined aria-hidden="true"/>} disabled={!canUse} onClick={()=>run('新建构图',create)}>新建构图</Button><Button icon={<SaveOutlined aria-hidden="true"/>} disabled={!canUse||!(dirty||viewDirty)} onClick={()=>run('保存构图',async()=>{await persist();await persistView();})}>保存</Button><Button aria-label="撤销绘制修改" icon={<UndoOutlined aria-hidden="true"/>} disabled={!canUse||!(history.current.past.length||!dirty&&serverHistory.data?.can_undo)} onClick={()=>historyMove(false)}/><Button aria-label="重做绘制修改" icon={<RedoOutlined aria-hidden="true"/>} disabled={!canUse||!(history.current.future.length||!dirty&&serverHistory.data?.can_redo)} onClick={()=>historyMove(true)}/><Button disabled={!canUse||!draft} onClick={()=>run('导出视频',exportVideo)}>导出视频</Button></Space>
   <div className="draw-operation-status" role="status" aria-live="polite">{busy|| (dirty?'有未保存更改':selected?'构图已保存':'新建构图开始')}</div>
   {(failure||query.error)&&<Alert className="draw-toolbar-alert" type="error" showIcon message={failure||errorText(query.error)}/>}
   {dirty&&selected&&baseline.current&&selected.revision!==baseline.current.revision&&<Alert className="draw-toolbar-alert" type="warning" message="工程中的构图已更新；当前草稿完整保留。" action={<Button onClick={()=>run('保留草稿副本',async()=>{const current=await projectApi.workspace();const copy={...draft!,id:'',name:draft!.name+' · 草稿副本',revision:0,content_version:''};const saved=await drawApi.save(id,{session:session!.id,expected_revision:current.revision,idempotency_key:crypto.randomUUID(),composition:copy});await project.refresh();await query.refetch();const added=saved.document!.draw_compositions!.at(-1)!;base.current='';navigate({drawSlot:added.id});})}>保留为新构图</Button>}/>}
   {blocked&&<Alert className="draw-toolbar-alert" type="warning" message="当前窗口未打开深链指定工程。"/>}
  </div>
  <section className="draw-zone draw-zone-source" hidden={previewOnly} data-draw-zone="source" tabIndex={-1} aria-label={DRAW_ZONES[0].label}>
   <div className="draw-panel-title"><Typography.Text strong>构图与图层</Typography.Text></div>
   <Select aria-label="当前构图" value={selected?.id} disabled={!canUse} placeholder="新建构图" options={list.map(c=>({value:c.id,label:c.name}))} onChange={value=>{const c=list.find(c=>c.id===value);if(c)run('切换构图',()=>choose(c));}}/>
   <Tabs animated={false} activeKey={tab} onChange={setTab} items={[
    {key:'layers',label:'图层',children:<Flex vertical gap={6}><Space wrap><Button disabled={!canUse||!draft} onClick={addText}>添加文字</Button><Button disabled={!canUse||!draft} onClick={addShape}>添加形状</Button><Button disabled={!canUse||!document} onClick={()=>setNodeID('')}>画布属性</Button></Space>{[...(document?.nodes??[])].sort((a,b)=>(b.z??0)-(a.z??0)).map(n=><Button key={n.id} className="draw-layer-item" aria-pressed={nodeID===n.id} onClick={()=>setNodeID(n.id)}>{n.hidden?'隐藏 · ':''}{n.name||kindLabels[n.kind]} · {kindLabels[n.kind]}</Button>)}{!draft&&<Empty description="新建构图开始"/>}</Flex>},
    {key:'assets',label:'素材',children:<Flex vertical gap={8}>{resources.map(r=><Button className="draw-image-resource" key={r.id} disabled={!canUse||!draft||r.missing||r.changed} draggable={!r.missing&&!r.changed} onDragStart={event=>{event.dataTransfer.setData('application/x-vac-resource',JSON.stringify({project:id,session:session?.id,resource:r.id}));}} onClick={()=>run('加入图片',async()=>addImage(r.id))}><img alt="" src={timelineApi.thumbnailURL(id,r.id,0)}/><span>{r.name}</span></Button>)}{!resources.length&&<Empty description="从文件菜单导入图片"/>}</Flex>}
   ]}/>
   <div className="draw-goal"><label htmlFor="draw-goal">描述画面或修改目标</label><Input.TextArea id="draw-goal" aria-label="Agent 绘制目标" value={goal} rows={3} onChange={e=>setGoal(e.target.value)} placeholder="例如：图片作背景，叠加界面演示和动态数据卡"/><Button type="primary" block disabled={!canUse||!draft} onClick={()=>run('交给 Agent',createRun)}>交给 Agent 制作</Button><Typography.Text type="secondary" className="draw-agent-status"><span role="status" aria-live="polite">{statusSummary}</span>{statusDetails.length>0&&<span> · {statusDetails.join(' · ')}</span>}</Typography.Text></div>
  </section>
  <section className="draw-zone draw-zone-canvas" data-draw-zone="canvas" tabIndex={-1} aria-label={DRAW_ZONES[1].label} onDragOver={e=>{if(canUse&&e.dataTransfer.types.includes('application/x-vac-resource'))e.preventDefault();}} onDrop={drop}>
   <div className="draw-panel-title"><Typography.Text strong>画布</Typography.Text><Typography.Text className="draw-preview-metadata" type="secondary">{draft?draft.width+' × '+draft.height+' · '+fps.toFixed(2)+' fps':''}</Typography.Text><Button type="text" aria-label={previewOnly?'退出专注预览':'专注预览'} icon={previewOnly?<FullscreenExitOutlined aria-hidden="true"/>:<FullscreenOutlined aria-hidden="true"/>} onClick={()=>setPreviewOnly(v=>!v)}/></div>
   <div ref={canvas} className="draw-canvas-frame draw-live-canvas" onPointerDown={hit} onPointerMove={moveDrag} onPointerUp={e=>endDrag(e)} onPointerCancel={e=>endDrag(e,true)}>
    {image?<div className="draw-frame-fit" style={{aspectRatio:(draft?.width??1)/(draft?.height??1),width:"min(100cqw, "+((draft?.width??1)/(draft?.height??1)*100)+"cqh)"}}><img alt="当前构图合成帧" draggable={false} src={'data:image/png;base64,'+image.png}/>{selectedBounds&&draft&&<span className="draw-selection" style={{left:selectedBounds.x/draft.width*100+'%',top:selectedBounds.y/draft.height*100+'%',width:selectedBounds.width/draft.width*100+'%',height:selectedBounds.height/draft.height*100+'%'}}><button className="draw-resize-handle" aria-label="拖动调整图层大小；也可使用右侧宽高输入框" onPointerDown={event=>{event.stopPropagation();beginDrag(event,nodeID,true);}}/></span>}</div>:<Empty description={['compiling','loading','updating'].includes(preview.state)?'正在准备画面…':id?'新建构图开始绘制':'请先新建或打开工程'}/>}
   </div>
   <div className="draw-preview-status" role="status" aria-live="polite"><Typography.Text type={preview.state==='error'?'danger':'secondary'}>{preview.state==='current'?'当前画面 · 第 '+image?.frame+' 帧':preview.state==='compiling'?'正在编译画面 · 旧画面仅供参考':preview.state==='loading'?'正在准备素材 · 旧画面仅供参考':preview.state==='updating'?'正在更新 · 旧画面仅供参考':preview.state==='error'?preview.error:'预览等待构图'}</Typography.Text></div>
  </section>
  <section className="draw-zone draw-zone-timeline" data-draw-zone="timeline" tabIndex={-1} aria-label={DRAW_ZONES[2].label}>
   <div className="draw-timeline-heading"><Typography.Text strong>构图时间轴</Typography.Text><Typography.Text type="secondary">{head+' / '+(draft?.duration_frames??0)+' 帧'}</Typography.Text><Typography.Text type="secondary" className="draw-timeline-hint">画面与图层共享一个播放头</Typography.Text></div>
   <div className="draw-transport"><Button aria-label={playing?'暂停绘制预览':'播放绘制预览'} icon={playing?<PauseOutlined aria-hidden="true"/>:<PlayCircleOutlined aria-hidden="true"/>} disabled={!draft} onClick={()=>setPlaying(v=>!v)}/><Button aria-label="上一帧" icon={<StepBackwardOutlined aria-hidden="true"/>} disabled={!draft} onClick={()=>seek(Math.max(0,head-1))}/><Slider ariaLabelForHandle="构图播放帧" aria-label="构图播放帧" min={0} max={Math.max(0,(draft?.duration_frames??1)-1)} value={head} onChange={seek} disabled={!draft}/><Button aria-label="下一帧" icon={<StepForwardOutlined aria-hidden="true"/>} disabled={!draft} onClick={()=>seek(Math.min((draft?.duration_frames??1)-1,head+1))}/></div>
   <div className="draw-layer-rows">{document?.nodes.map(n=><div className="draw-layer-row" key={n.id}><span>{n.name||kindLabels[n.kind]}</span><div className="draw-layer-range"><button aria-label={"选择时间轴图层 "+(n.name||n.id)} aria-pressed={nodeID===n.id} style={{left:(ranges.get(n.id)?.from??0)/document.duration_frames*100+"%",width:Math.max(0,(ranges.get(n.id)?.to??0)-(ranges.get(n.id)?.from??0))/document.duration_frames*100+"%"}} onClick={()=>{setNodeID(n.id);seek(ranges.get(n.id)?.from??n.from);}}/><span className="draw-time-cursor" style={{left:head/document.duration_frames*100+"%"}}/></div></div>)}</div>
  </section>
  <section className="draw-zone draw-zone-inspector" hidden={previewOnly} data-draw-zone="inspector" tabIndex={-1} aria-label={DRAW_ZONES[3].label}>
   <div className="draw-panel-title"><Typography.Text strong>{node?'图层属性':'画布属性'}</Typography.Text></div>
   {draft&&document?<Flex vertical gap={10}>
    <label>{node?'图层名称':'构图名称'}<Input aria-label={node?'图层名称':'构图名称'} value={node?.name??draft.name} disabled={!canUse} onChange={e=>node?patchNode({name:e.target.value}):edit(c=>({...c,name:e.target.value}))}/></label>
    {node?<>
     <div className="draw-composition-fields">{numeric('位置 X',node.transform.x,v=>patchNode({transform:{...node.transform,x:v}}))}{numeric('位置 Y',node.transform.y,v=>patchNode({transform:{...node.transform,y:v}}))}{numeric('图层宽度',node.transform.width??draft.width,v=>patchNode({transform:{...node.transform,width:v}}),1,32768)}{numeric('图层高度',node.transform.height??draft.height,v=>patchNode({transform:{...node.transform,height:v}}),1,32768)}{numeric('旋转角度',node.transform.rotation??0,v=>patchNode({transform:{...node.transform,rotation:v}}))}{numeric('不透明度 %',(node.opacity??1)*100,v=>patchNode({opacity:v/100}),0,100)}{numeric('开始帧',node.from,v=>patchNode({from:Math.round(v)}),0,document.duration_frames-1)}{numeric('持续帧数',node.duration,v=>patchNode({duration:Math.round(v)}),1,document.duration_frames)}{numeric('层级',node.z??0,v=>patchNode({z:Math.round(v)}))}</div>
     {node.kind==='shape'&&<Select aria-label="形状" value={node.shape??'rect'} options={[{value:'rect',label:'矩形'},{value:'ellipse',label:'椭圆'}]} onChange={shape=>patchNode({shape})}/>}
     {node.kind==='text'&&<><label>文字内容<Input.TextArea aria-label="图层文字内容" value={node.text} onChange={e=>patchNode({text:e.target.value})}/></label>{numeric('字号',node.font_size??48,v=>patchNode({font_size:v}),8,500)}<label>文字颜色<Input aria-label="文字颜色" value={node.color??'#ffffff'} onChange={e=>patchNode({color:e.target.value})}/></label></>}
     {node.kind==='shape'&&<label>填充颜色<Input aria-label="填充颜色" value={node.color??'#4C8DFF'} onChange={e=>patchNode({color:e.target.value})}/></label>}
     <Collapse ghost items={[{key:'motion',label:'动画、范围与裁切',children:<Flex vertical gap={8}>
      {node.parent_id&&<Typography.Text type="secondary">范围与关键帧使用所属组的局部帧。</Typography.Text>}
      <div className="draw-composition-fields">{numeric('源偏移帧',node.source_offset??0,v=>patchNode({source_offset:Math.round(v)}),0,18000)}{numeric('速度分子',node.speed?.num??1,v=>patchNode({speed:{num:Math.round(v),den:node.speed?.den??1}}),1,1000)}{numeric('速度分母',node.speed?.den??1,v=>patchNode({speed:{num:node.speed?.num??1,den:Math.round(v)}}),1,1000)}</div>
      {['image','source','mask'].includes(node.kind)&&<><Select aria-label="画面适应方式" value={node.fit??'contain'} onChange={fit=>patchNode({fit})} options={[{value:'contain',label:'完整显示'},{value:'cover',label:'填满并裁切'},{value:'fill',label:'拉伸'}]}/><div className="draw-composition-fields">{(['top','right','bottom','left'] as const).map((side,i)=>numeric('裁切'+['上','右','下','左'][i]+' 像素',node.crop?.[side]??0,v=>patchNode({crop:{top:0,right:0,bottom:0,left:0,...node.crop,[side]:v}}),0,32768))}</div></>}
      <Button disabled={!canUse} onClick={()=>{const clock=drawFrameContext(document,node.id,head);const frame=Math.max(0,Math.min(node.duration-1,clock.parent_frame-node.from));patchNode({keyframes:[...(node.keyframes??[]).filter(k=>k.frame!==frame),{frame,x:node.transform.x,y:node.transform.y,opacity:node.opacity??1}].sort((a,b)=>a.frame-b.frame)});}}>在播放头添加位置关键帧</Button>
      {(node.keyframes??[]).map((k,index)=><Flex vertical gap={4} key={index}><Typography.Text type="secondary">关键帧 {index+1}</Typography.Text><div className="draw-composition-fields">{numeric('关键帧 '+(index+1)+' 帧',k.frame,v=>patchNode({keyframes:node.keyframes!.map((old,i)=>i===index?{...old,frame:Math.round(v)}:old).sort((a,b)=>a.frame-b.frame)}),0,node.duration-1)}{numeric('关键帧 '+(index+1)+' X',k.x,v=>patchNode({keyframes:node.keyframes!.map((old,i)=>i===index?{...old,x:v}:old)}))}{numeric('关键帧 '+(index+1)+' Y',k.y,v=>patchNode({keyframes:node.keyframes!.map((old,i)=>i===index?{...old,y:v}:old)}))}{numeric('关键帧 '+(index+1)+' 不透明度 %',(k.opacity??1)*100,v=>patchNode({keyframes:node.keyframes!.map((old,i)=>i===index?{...old,opacity:v/100}:old)}),0,100)}</div><Button size="small" disabled={!canUse} onClick={()=>patchNode({keyframes:node.keyframes!.filter((_,i)=>i!==index)})}>移除关键帧 {index+1}</Button></Flex>)}
      {node.kind==='source'&&<label>组件参数（JSON 对象）<Input.TextArea key={node.id+JSON.stringify(node.parameters)} aria-label="组件参数" rows={3} disabled={!canUse} defaultValue={JSON.stringify(node.parameters??{},null,2)} onBlur={e=>{try{const value=JSON.parse(e.target.value);if(!value||Array.isArray(value)||typeof value!=='object')throw Error('组件参数必须为 JSON 对象');if(JSON.stringify(value)!==JSON.stringify(node.parameters??{}))patchNode({parameters:value});}catch(error){setFailure(errorText(error));}}}/></label>}
     </Flex>}]}/>
     <Space wrap><Button onClick={()=>patchNode({hidden:!node.hidden})}>{node.hidden?'显示图层':'隐藏图层'}</Button><Button danger onClick={()=>{patchDocument(d=>{const removed=new Set([node.id]);for(let i=0;i<d.nodes.length;i++)for(const n of d.nodes)if(n.parent_id&&removed.has(n.parent_id))removed.add(n.id);return {...d,nodes:d.nodes.filter(n=>!removed.has(n.id))};});setNodeID('');}}>移除图层</Button></Space>
    </>:<div className="draw-composition-fields">{numeric('画布宽度',draft.width,v=>edit(c=>({...c,width:v,document:{...c.document!,canvas:{...c.document!.canvas,width:v}}})),16,3840)}{numeric('画布高度',draft.height,v=>edit(c=>({...c,height:v,document:{...c.document!,canvas:{...c.document!.canvas,height:v}}})),16,2160)}{numeric('总帧数',draft.duration_frames,v=>edit(c=>({...c,duration_frames:v,document:{...c.document!,duration_frames:v}})),1,18000)}</div>}
    <Collapse ghost items={[{key:'advanced',label:'高级 · 源码与实现策略',children:<Flex vertical gap={8}>
     <Select aria-label="视频导出格式" value={outputFormat} onChange={setOutputFormat} options={[{value:'mp4',label:'MP4 · 常规视频'},{value:'webm-alpha',label:'WebM · 透明背景'},{value:'prores4444',label:'ProRes 4444 · 透明背景'}]}/>
     <Typography.Text type="secondary">{document.explanation||'公共图层直接合成；HTML 动效与 React 组件按画面需要协作。'}</Typography.Text>
     <Select aria-label="Agent 执行方式" value={execution} onChange={setExecution} options={[{value:'auto',label:'自动使用已配置 Agent'},{value:'builtin',label:'内置服务商 Agent'},{value:'external',label:'外部 MCP Agent'}]}/>
     <Space wrap><Button disabled={!canUse} onClick={()=>addSource('hyperframes')}>添加 HTML 画面</Button><Button disabled={!canUse} onClick={()=>addSource('remotion')}>添加 React 画面</Button></Space>
     <Select aria-label="源码对象" value={source?.id} options={document.sources.map(s=>({value:s.id,label:(s.name||s.id)+' · '+engineLabels[s.engine]}))} onChange={setSourceID}/>
     {source&&<><Typography.Text type="secondary">{source.reason||engineLabels[source.engine]}</Typography.Text><Select aria-label="强制指定源码引擎" disabled={!canUse} value={source.engine} options={[{value:'hyperframes',label:'HyperFrames · HTML / GSAP'},{value:'remotion',label:'Remotion · React'}]} onChange={engine=>patchDocument(d=>({...d,sources:d.sources.map(s=>s.id===source.id?{...s,engine}:s)}))}/><Typography.Text type="secondary">指定后须提供对应语言的源码；检查失败会明确提示。</Typography.Text><Input.TextArea disabled={!canUse} className="draw-source-editor" rows={12} aria-label="构图源码" value={source.source} spellCheck={false} onChange={e=>patchDocument(d=>({...d,sources:d.sources.map(s=>s.id===source.id?{...s,source:e.target.value}:s)}))}/></>}
     <Space wrap><Button disabled={!canUse} onClick={()=>run("加入跟随源",()=>importVideo("follow"))}>入轨并跟随源更新</Button><Button disabled={!canUse} onClick={()=>run("导入已渲染成片",()=>importVideo("rendered"))}>导入已渲染成片</Button></Space>
     <Button disabled={!canUse} onClick={()=>run('检查构图',async()=>{const c=await persist();const result=await drawApi.check(id,c.id,{session:session!.id,content_version:c.content_version});setNotice('检查通过：'+result.checks.join(' / '));})}>检查构图</Button>
    </Flex>}]}/>
   </Flex>:<Empty description="没有选中构图"/>}
   <Flex vertical gap={8} className="draw-handoff"><Button block disabled={!canUse||!selected} onClick={()=>run('加入编辑时间线',()=>importVideo())}>加入编辑时间线</Button><Typography.Text type="secondary">{notice||'默认加入冻结源快照，后续修改不改变已入轨画面；可在高级选项中改为跟随源。'}</Typography.Text></Flex>
  </section>
 </div>;
}
