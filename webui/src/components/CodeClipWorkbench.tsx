import {useEffect,useMemo,useRef,useState} from 'react';
import {useQuery,useQueryClient} from '@tanstack/react-query';
import {Alert,Button,Card,Input,Select,Space,Typography} from 'antd';
import {api} from '../api';
import {skillApi} from '../skillApi';
import {timelineApi} from '../timelineApi';
import {useProjectWorkspace} from '../projectWorkspace';
import {useSequenceEditor} from '../sequenceEditor';
import {codeClipColors,codeDescription,rebaseCodeDescription,selectedCodeClips} from '../codeClips';
import {frameLabel,type TimelineClip,type FrameRate} from '../timelineContracts';
import {directiveCompletions,directiveTemplate,inspectCodeScript} from '../scriptGrammar';
import {scriptTokens} from '../scriptWorkbench';
import {ScriptAgentHandoff} from './ScriptAgentHandoff';
import {errorText} from './ui';
import type {RouteState} from '../navigation';

function CodeScriptField({label,value,disabled,clip,rate,onChange,onSeek}:{label:string;value:string;disabled:boolean;clip:TimelineClip;rate:FrameRate;onChange:(value:string)=>void;onSeek:(frame:number)=>void}){
 const overlay=useRef<HTMLPreElement>(null),input=useRef<HTMLTextAreaElement|null>(null),tokens=useMemo(()=>scriptTokens(value,true),[value]);
 const [caret,setCaret]=useState(0),[open,setOpen]=useState(false),[choice,setChoice]=useState(0);
 const suggestions=useMemo(()=>open?directiveCompletions(value,caret):[],[value,caret,open]);
 const inspection=useMemo(()=>inspectCodeScript(value,clip,rate),[value,clip.at,clip.duration,rate]);
 const insert=(item:(typeof suggestions)[number])=>{const next=value.slice(0,item.start)+item.text+value.slice(item.end),position=item.start+item.text.length-(item.text.endsWith('}')?1:0);onChange(next);setOpen(false);requestAnimationFrame(()=>{input.current?.focus();input.current?.setSelectionRange(position,position);setCaret(position);});};
 return <>
  <div className="script-code-field"><pre aria-hidden="true" ref={overlay} className="script-highlight">{tokens.map((token,i)=>token.kind==='text'?token.text:<mark key={i} className={`script-token-${token.kind}`}>{token.text}</mark>)}{'\n'}</pre><Input.TextArea aria-label={label} aria-invalid={inspection.issues.length>0} rows={7} value={value} disabled={disabled} spellCheck={false}
   onFocus={event=>{input.current=event.currentTarget;setCaret(event.currentTarget.selectionStart);}}
   onSelect={event=>{input.current=event.currentTarget;setCaret(event.currentTarget.selectionStart);}}
   onBlur={()=>setOpen(false)}
   onKeyDown={event=>{if(event.nativeEvent.isComposing)return;if((event.ctrlKey||event.metaKey)&&event.code==='Space'){event.preventDefault();setOpen(true);setChoice(0);}else if(event.key==='Escape'){setOpen(false);}else if(suggestions.length&&(event.key==='ArrowDown'||event.key==='ArrowUp')){event.preventDefault();setChoice(old=>(old+(event.key==='ArrowDown'?1:-1)+suggestions.length)%suggestions.length);}else if(suggestions.length&&(event.key==='Enter'||event.key==='Tab'&&!event.shiftKey&&!event.ctrlKey&&!event.altKey&&!event.metaKey)){event.preventDefault();event.stopPropagation();insert(suggestions[choice%suggestions.length]);}}}
   onScroll={event=>{if(overlay.current)overlay.current.scrollTop=event.currentTarget.scrollTop;}}
   onChange={event=>{input.current=event.currentTarget;setCaret(event.currentTarget.selectionStart);setOpen(true);setChoice(0);onChange(event.target.value);}}/>
  </div>
  {!!suggestions.length&&<Space wrap className="code-script-completions" role="group" aria-label="脚本语法补全">{suggestions.map((item,i)=><Button key={item.label} size="small" type={i===choice%suggestions.length?'primary':'default'} disabled={disabled} onMouseDown={event=>event.preventDefault()} onClick={()=>insert(item)}>{item.label}</Button>)}</Space>}
  {inspection.issues.map((issue,i)=><Typography.Paragraph key={i} type="danger" role="alert" className="code-script-diagnostic"><Button type="link" danger size="small" onClick={()=>{input.current?.focus();input.current?.setSelectionRange(issue.start,issue.end);}}>第{issue.line}行第{issue.column}列</Button>{issue.message}</Typography.Paragraph>)}
  {!!inspection.instructions.length&&<Space wrap className="code-script-instructions">{inspection.instructions.map((item,i)=><Button size="small" type="text" key={i} onClick={()=>onSeek(clip.at+item.frame)}>{item.label} · {frameLabel(clip.at+item.frame,rate)} · {item.mode==='generate'?'随画面生成':'直接应用'}</Button>)}</Space>}
 </>;
}

type Draft={text:string;base:string;revision:number;at:number};
export function CodeClipWorkbench({active,navigate}:{active:boolean;navigate:(route:Partial<RouteState>)=>void}){
 const p=useProjectWorkspace(),e=useSequenceEditor(),client=useQueryClient(),sequence=e.view?.sequence;
 const [drafts,setDrafts]=useState<Record<string,Draft>>({}),[busy,setBusy]=useState(false),[failure,setFailure]=useState(''),[execution,setExecution]=useState<'builtin'|'external'>('external');
 const draftRef=useRef(drafts);draftRef.current=drafts;
 const failedDraft=useRef('');
 const saveFlight=useRef<Promise<void>|null>(null),[autoSaving,setAutoSaving]=useState(false),identity=`${p.session?.id}:${e.sequenceId}`,current=useRef(identity);current.current=identity;
 const selected=useMemo(()=>sequence?selectedCodeClips(sequence,e.selected):[],[sequence,e.selected]);
 const all=useMemo(()=>sequence?sequence.tracks.filter(t=>t.kind==='code').flatMap(t=>t.clips):[],[sequence]);
 const profiles=useQuery({queryKey:['processing-profiles'],queryFn:api.listProfiles,enabled:active});
 const runs=useQuery({queryKey:['code-runs',e.projectId],queryFn:()=>skillApi.runs(e.projectId),enabled:active&&all.some(c=>!!c.code?.run_id),refetchInterval:3000});
 const colors=useMemo(()=>sequence?codeClipColors(sequence):{},[sequence]);
 const dirty=Object.values(drafts).some(d=>d.text!==d.base);
 const invalidDraft=!!sequence&&Object.entries(drafts).some(([id,d])=>{const clip=all.find(c=>c.id===id);return clip&&d.text!==d.base&&inspectCodeScript(d.text,clip,sequence.fps).issues.length>0;});
 useEffect(()=>{setDrafts({});setFailure('');},[identity]);
 useEffect(()=>{
  if(!sequence)return;
  setDrafts(old=>{let changed=false;const next={...old};for(const c of all){const d=old[c.id];if(!d)continue;const text=codeDescription(c,sequence.fps);
   if(d.text===d.base){if(text!==d.base||c.code?.revision!==d.revision){next[c.id]={text,base:text,revision:c.code?.revision??0,at:c.at};changed=true;}}
   else if(c.at!==d.at){const delta=c.at-d.at;next[c.id]={...d,text:rebaseCodeDescription(d.text,delta,sequence.fps),base:rebaseCodeDescription(d.base,delta,sequence.fps),at:c.at};changed=true;}
  }return changed?next:old;});
 },[all,sequence]);
 const commitDrafts=async()=>{
  if(!sequence)return;
  const owner=current.current;
  for(const [id,d] of Object.entries(draftRef.current)){
   if(d.text===d.base)continue;
   const view=await timelineApi.get(e.projectId,e.sequenceId),clip=view.sequence.tracks.flatMap(t=>t.clips).find(c=>c.id===id);
   if(!clip||clip.code?.revision!==d.revision)throw Error('代码条已修改或删除；草稿已保留，请核对后重试');
   const issues=inspectCodeScript(d.text,clip,view.sequence.fps).issues;if(issues.length)throw Error(`第${issues[0].line}行第${issues[0].column}列：${issues[0].message}`);
   const next=await timelineApi.command(e.projectId,e.sequenceId,{session:view.session,expected_revision:view.sequence.revision,idempotency_key:crypto.randomUUID(),label:'编辑代码条脚本',operations:[{type:'code.script',clip_ids:[id],description:d.text,code_revision:d.revision,unlinked:true}]});
   if(current.current!==owner)return;
   client.setQueryData(['timeline',view.session,e.projectId,e.sequenceId],next);
   const saved=next.sequence.tracks.flatMap(t=>t.clips).find(c=>c.id===id)!;
   const text=codeDescription(saved,next.sequence.fps),entry={text,base:text,revision:saved.code!.revision,at:saved.at};
   setDrafts(old=>{const next={...old,[id]:old[id]?.text===d.text?entry:{...old[id],base:text,revision:entry.revision,at:entry.at}};draftRef.current=next;return next;});
  }
  await p.refresh();await client.invalidateQueries({queryKey:['timeline']});
 };
 const save=async()=>{if(saveFlight.current)await saveFlight.current;const job=commitDrafts();saveFlight.current=job;try{await job;}finally{if(saveFlight.current===job)saveFlight.current=null;}};
 useEffect(()=>p.registerEditor(`code:${identity}`,{dirty,flush:save}),[identity,dirty,drafts,sequence]);
 useEffect(()=>{
  if(!active||!dirty||busy||autoSaving||invalidDraft||failedDraft.current===JSON.stringify(drafts))return;
  const timer=window.setTimeout(()=>{setAutoSaving(true);void save().then(()=>{failedDraft.current='';setFailure('');}).catch(error=>{failedDraft.current=JSON.stringify(draftRef.current);setFailure(errorText(error));}).finally(()=>{setAutoSaving(false);});},700);
  return()=>window.clearTimeout(timer);
 },[active,dirty,drafts,busy,autoSaving,invalidDraft]);
 const task=(clips:TimelineClip[],action:'describe'|'regenerate')=>JSON.stringify({action,sequence_id:e.sequenceId,targets:clips.map(c=>({clip_id:c.id,code_version:c.code?.content_version,code_revision:c.code?.revision,run_id:c.code?.run_id,description:c.code?.description,resource_id:c.resource_id}))},null,2)+'\n先调用timeline.read读取最新序列和目标源码。只允许修改以上代码条。'+(action==='describe'?'根据源码补写自然语言画面描述；使用timeline.edit，每次只提交一个code.script操作，mode=describe、description、code_revision及clip_ids。不得改变画面或参数。':'按原时间顺序分别生成，每条通过timeline.edit提交一个code.replace操作：clip_ids、code_version、code_segments:[{composition:完整DrawComposition,description:自然语言画面描述}]。每段必须独立可执行；参数动画不拆条。允许改变时长，由引擎执行轨组波纹。某条失败后继续其余条；不要调用draw.import或修改共享构图。')+'\n外部Agent先通过projects.list读取当前工程的排队任务，使用skill_runs.claim领取code_targets对应的运行；随后通过skill_runs.tool调用上述工具。完成后调用run.finish。';
 const launch=async(clips:TimelineClip[],action:'describe'|'regenerate',forceExternal=false)=>{
  if(!clips.length)return;
  const owner=current.current,profile=profiles.data?.find(x=>x.content_mode==='configured'&&x.narration.endpoint),builtin=!forceExternal&&execution==='builtin';
  if(builtin&&!profile)throw Error('请先配置内部Agent服务商，或选择外部MCP Agent');
  const visible=await skillApi.visibleSet(e.projectId);
  const targets=Object.fromEntries(clips.map(c=>[c.id,c.code!.content_version!])),existing=(await skillApi.runs(e.projectId)).find(r=>r.code_action===action&&r.execution_mode===(builtin?'builtin_api':'external_mcp')&&['queued','running'].includes(r.state)&&Object.keys(r.code_targets??{}).length===clips.length&&clips.every(c=>r.code_targets?.[c.id]===targets[c.id]));
  const run=existing??await skillApi.createRun({project_id:e.projectId,sequence_id:e.sequenceId,flow:'custom',skill_id:'vac-draw-pipeline',strategy:'draw',execution_mode:builtin?'builtin_api':'external_mcp',visible_set_sha:visible.sha256,goal:task(clips,action),code_targets:targets,code_action:action,idempotency_key:crypto.randomUUID(),...(builtin&&profile?{profile_id:profile.profile_id,profile_revision:profile.revision}:{})});
  if(current.current!==owner)return;
  const view=await timelineApi.get(e.projectId,e.sequenceId);
  await timelineApi.command(e.projectId,e.sequenceId,{session:view.session,expected_revision:view.sequence.revision,idempotency_key:crypto.randomUUID(),label:action==='describe'?'补齐画面描述':'生成代码画面',operations:clips.map(c=>({type:'code.job',clip_ids:[c.id],code_run_id:run.id,unlinked:true}))});
  await p.refresh();await client.invalidateQueries({queryKey:['timeline']});await runs.refetch();
  if(builtin&&!existing)void skillApi.execute(run.id,run.version).then(async()=>{if(current.current===owner){await p.refresh();await client.invalidateQueries({queryKey:['timeline']});await runs.refetch();}}).catch(error=>{if(current.current===owner)setFailure(errorText(error));});
 };
 const perform=(fn:()=>Promise<void>)=>{setBusy(true);setFailure('');void fn().catch(error=>setFailure(errorText(error))).finally(()=>setBusy(false));};
 const regenerate=async()=>{await save();const view=await timelineApi.get(e.projectId,e.sequenceId);const targets=selectedCodeClips(view.sequence,e.selected).map(x=>x.clip).filter(c=>c.code?.description!==c.code?.generated_description);await launch(targets,'regenerate');};
 return <div className="code-clip-workbench">
  <Space direction="vertical" size={12} className="code-workbench-content">
   <Typography.Title level={4}>代码条脚本</Typography.Title>
   <Typography.Text type="secondary">参数与字幕保存后直接应用；画面描述修改后点击重新生成。时间可写“条内1s”或“序列00:00:10:00”；省略时间表示本条起点。Ctrl+Space 打开补全，↑↓选择，Tab或Enter接受。</Typography.Text>
   {failure&&<Alert type="error" message={failure} closable onClose={()=>setFailure('')}/>}
   <Space wrap><Select aria-label="代码条Agent执行方式" value={execution} onChange={setExecution} options={[{value:'external',label:'外部 MCP Agent'},{value:'builtin',label:'内置 Agent（兼容）'}]}/><Button loading={busy} disabled={!dirty||e.pending} onClick={()=>perform(save)}>保存脚本与参数</Button><Button type="primary" loading={busy} disabled={!selected.length||e.pending} onClick={()=>perform(regenerate)}>重新生成画面</Button></Space>
   {selected.map(({clip,track},i)=>{
    const value=drafts[clip.id]?.text??codeDescription(clip,sequence!.fps),run=runs.data?.find(r=>r.id===clip.code?.run_id),working=run&&['queued','running'].includes(run.state);
    return <Card key={clip.id} size="small" className="code-script-card" style={{borderColor:colors[clip.id]}} title={`${i+1} · ${clip.name}`} extra={<Button size="small" onClick={()=>e.setHead(clip.at)}>定位</Button>}>
     <Typography.Text type="secondary">{frameLabel(clip.at,sequence!.fps)} · {track.name}{track.locked?' · 已锁定':''}</Typography.Text>
     {clip.code?.description_error&&<Alert type="warning" message={clip.code.description_error}/>}
     <CodeScriptField label={`代码条脚本 ${i+1} ${clip.name}`} value={value} disabled={track.locked||busy} clip={clip} rate={sequence!.fps} onSeek={e.setHead} onChange={text=>{setDrafts(old=>({...old,[clip.id]:{...(old[clip.id]??{base:codeDescription(clip,sequence!.fps),revision:clip.code?.revision??0,at:clip.at}),text}}));}}/>
     <Space wrap>{(['音量','移动','旋转','缩放','透明度','字幕'] as const).map(kind=><Button key={kind} size="small" disabled={track.locked||busy} onClick={()=>{const time=frameLabel(Math.max(clip.at,Math.min(e.head,clip.at+clip.duration-1)),sequence!.fps),block=directiveTemplate(kind).replace('时间:条内0f',`时间:${time}`);setDrafts(old=>({...old,[clip.id]:{...(old[clip.id]??{base:value,revision:clip.code?.revision??0,at:clip.at}),text:value+'\n'+block}}));}}>插入{kind}指令</Button>)}</Space>
     <Typography.Paragraph type="secondary" role="status">{drafts[clip.id]&&drafts[clip.id].text!==drafts[clip.id].base?'草稿待保存；无效指令不会改变当前画面':clip.code?.description!==clip.code?.generated_description?'描述已保存，待重新生成画面':'参数与字幕已应用'}</Typography.Paragraph>
     {run&&<Typography.Paragraph role="status">{working?(run.execution_mode==='external_mcp'?'等待外部Agent处理':'Agent处理中'):run.state==='succeeded'?(clip.code?.description===clip.code?.generated_description?'已应用':'本条未应用，草稿保留'):run.state==='cancelled'?'任务已取消':'生成未完成，可重试'}{run.reason?` · ${run.reason}`:''}</Typography.Paragraph>}
     <Space wrap>{!working&&run?.code_candidates?.[clip.id]&&<Button size="small" disabled={track.locked} onClick={()=>perform(async()=>{const operation=run.code_candidates![clip.id];if(operation.type!=='code.replace'||operation.clip_ids?.[0]!==clip.id)throw Error('保留结果与目标不匹配');await e.command([operation],'应用保留的代码画面');})}>应用保留结果</Button>}{working&&<Button size="small" onClick={()=>perform(async()=>{const latest=await skillApi.run(run.id);await skillApi.action(run.id,'cancel',latest.version);await runs.refetch();})}>停止生成</Button>}{!clip.code?.description&&!working&&<Button size="small" onClick={()=>perform(async()=>launch([clip],'describe'))}>重试补齐描述</Button>}</Space>
    </Card>;
   })}
   {!!selected.length&&<ScriptAgentHandoff active={active} navigate={navigate} task={task(selected.map(x=>x.clip),'regenerate')} prepareTask={async()=>{await save();let view=await timelineApi.get(e.projectId,e.sequenceId);const chosen=selectedCodeClips(view.sequence,e.selected).map(x=>x.clip),changed=chosen.filter(c=>c.code?.description!==c.code?.generated_description),pending=chosen.filter(c=>!c.code?.description),action=changed.length?'regenerate':'describe',targets=changed.length?changed:pending;if(!targets.length)throw Error('所选代码条描述未改变，无需重新生成');await launch(targets,action,true);view=await timelineApi.get(e.projectId,e.sequenceId);return task(view.sequence.tracks.flatMap(t=>t.clips).filter(c=>targets.some(t=>t.id===c.id)),action);}}/>}
  </Space>
 </div>;
}
