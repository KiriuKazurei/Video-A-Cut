import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Empty, Flex, Progress, Space, Spin, Tabs, Tooltip, Typography } from 'antd';
import { PlayCircleOutlined, StepBackwardOutlined, StepForwardOutlined, PlusOutlined, ExportOutlined, CloseOutlined } from '@ant-design/icons';
import type { Asset, WorkflowRun } from '../types';
import type { RouteState } from '../navigation';
import type { RunSelection } from '../components/WorkflowPanel';
import { SequenceTimeline } from '../components/SequenceTimeline';
import { ProgramVu } from '../components/ProgramVu';
import { useSequenceEditor } from '../sequenceEditor';
import { useProjectWorkspace } from '../projectWorkspace';
import { projectApi } from '../projectApi';
import { fpsValue, frameLabel } from '../timelineContracts';
import { TimecodeField } from '../components/TimecodeField';
import { ACCENT, CONTROL_BG } from '../theme';
import '../timeline.css';
import { drawRoute } from '../drawContracts';

interface EditingWorkspaceProps { route:RouteState;navigate:(state:Partial<RouteState>)=>void;asset?:Asset;selection:RunSelection;onSelect:(id:string)=>void;onPreparedStarted:(run:WorkflowRun)=>void }

function SourceMonitor({ onCollapse, seekTo, onSeeked }:{ onCollapse:()=>void; seekTo?:number|null; onSeeked?:()=>void }) {
 const p=useProjectWorkspace(),e=useSequenceEditor(),r=p.resource,[proxy,setProxy]=useState(false),[proxyError,setProxyError]=useState(''),[sourceTime,setSourceTime]=useState(0),request=useRef<AbortController|null>(null);
 useEffect(()=>{const media=e.source.current;media?.pause();request.current?.abort();setProxy(false);setProxyError('');setSourceTime(0);return()=>{media?.pause();request.current?.abort();};},[e.active,p.session?.id,r?.id,r?.source.sha256,JSON.stringify(r?.raw_develop)]);
 useEffect(()=>{
  if(seekTo==null)return;
  const media=e.source.current;
  if(!media)return;
  const apply=()=>{try{media.currentTime=seekTo;}catch{/* ignore */}onSeeked?.();};
  if(media.readyState>=1)apply();else media.addEventListener('loadedmetadata',apply,{once:true});
 },[seekTo,r?.id,proxy]);
 const textual=r?.kind==='script'||r?.kind==='document'||r?.kind==='subtitle';
 const text=useQuery({queryKey:['source-text',p.session?.id,r?.id],queryFn:async({signal})=>{const response=await fetch(projectApi.contentURL(e.projectId,r!.id),{signal});if(!response.ok)throw Error('内容加载失败');return (await response.text()).slice(0,65536);},enabled:e.active&&!!r&&textual});
 const url=r?projectApi.contentURL(e.projectId,r.id,proxy)+(r.raw_develop?(proxy?"&":"?")+"recipe="+encodeURIComponent(JSON.stringify(r.raw_develop)):""):'';
 const fallback=()=>{if(!e.active||request.current&&!request.current.signal.aborted)return;if(!r||proxy){setProxyError('源预览加载失败，请检查资源或重新定位');return;}const controller=new AbortController();request.current=controller;void projectApi.createPreview(e.projectId,r.id,controller.signal).then(()=>{if(!controller.signal.aborted)setProxy(true);}).catch(reason=>{if(!controller.signal.aborted)setProxyError(String(reason));}).finally(()=>{if(request.current===controller)request.current=null;});};
 if(!e.active)return null;
 return <section className="editor-monitor source-monitor" aria-label="源监视器">
  <div className="editor-monitor-title"><strong>源</strong><Typography.Text ellipsis>{r?.name ?? '选择工程资源'}</Typography.Text>
   <Tooltip title="收起源监视器"><Button type="text" size="small" icon={<CloseOutlined/>} aria-label="收起源监视器" onClick={onCollapse}/></Tooltip>
  </div>
  <div className="editor-monitor-screen">{!r?<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="从左侧资源管理选择素材"/>:r.missing||r.removed?<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="源文件待重新关联"/>:r.kind==='video'||r.kind==='audio'?<video key={`${r.id}-${proxy}`} ref={e.source} data-resource-id={r.id} controls preload="metadata" src={url} aria-label="源素材播放器" onError={fallback} onTimeUpdate={event=>setSourceTime(event.currentTarget.currentTime)} onLoadedMetadata={event=>setSourceTime(event.currentTarget.currentTime)}/>:r.kind==='image'?<img alt={r.name} src={url} onError={fallback}/>:<pre className="source-text-preview">{text.data ?? (text.isError?'内容读取失败':'正在读取…')}</pre>}</div>
  {proxyError&&<Alert type="warning" message={proxyError}/>}
  <Flex gap={6} className="editor-monitor-controls" wrap align="center">
   {e.view?.sequence&&(r?.kind==='video'||r?.kind==='audio')&&<TimecodeField label="源时间码" frame={Math.max(0,Math.round(sourceTime*fpsValue(e.view.sequence.fps)))} rate={e.view.sequence.fps} onSeek={frame=>{const rate=e.view?.sequence?.fps;if(!rate)return;const media=e.source.current;const seconds=frame/fpsValue(rate);if(media){try{media.currentTime=seconds;}catch{/* ignore */}}setSourceTime(seconds);}}/>}
   <Button size="small" onClick={()=>e.marker('point',true)} disabled={!r||!['video','audio','image'].includes(r.kind)}>源标记</Button>
   <Button size="small" onClick={()=>void e.addResource(r!.id).catch(()=>{})} disabled={!r||r.missing||r.removed||!['video','audio','image','subtitle'].includes(r.kind)}>加入序列</Button>
  </Flex>
 </section>;
}

export function EditingWorkspace({route,navigate}:EditingWorkspaceProps) {
 const e=useSequenceEditor(),p=useProjectWorkspace(),s=e.view?.sequence;
 const [sourceOpen,setSourceOpen]=useState(false);
 const [sourceSeek,setSourceSeek]=useState<number|null>(null);
 useEffect(()=>{
  if(route.editingSection==='content'||route.scriptId)window.dispatchEvent(new Event('vac:open-script-dock'));
 },[route.editingSection,route.scriptId]);
 useEffect(()=>{
  const onOpen=(event:Event)=>{
   const detail=(event as CustomEvent<{resourceId?:string;time?:number}>).detail;
   setSourceOpen(true);
   if(typeof detail?.time==='number')setSourceSeek(detail.time);
  };
  window.addEventListener('vac:open-source',onOpen);
  return()=>window.removeEventListener('vac:open-source',onOpen);
 },[]);
 const items=p.session?.document?.sequences?.map(seq=>({key:seq.id,label:seq.name,closable:(p.session?.document?.sequences.length ?? 0)>1}));
 const sourceLabel=p.resource?.name ?? '源';
 const inFrame=[...(s?.markers??[])].reverse().find(m=>m.kind==='in')?.frame;
 const outFrame=[...(s?.markers??[])].reverse().find(m=>m.kind==='out')?.frame;
 const hasRange=inFrame!==undefined&&outFrame!==undefined&&outFrame>inFrame;
 return <div className="unified-editor" data-panel="timeline" tabIndex={-1} aria-label="统一多轨编辑器">
  <Tabs className="sequence-tabs" type="editable-card" size="small" activeKey={e.sequenceId} items={items} onChange={e.switchSequence} onEdit={(key,action)=>void e.manageSequence(action==='add'?'create':'delete',typeof key==='string'?key:undefined).catch(()=>{})}/>
  {e.error&&<Alert type="error" showIcon message={e.error}/>}
  <div className={`editor-monitors${sourceOpen?' is-source-open':' is-primary-only'}`}>
   {sourceOpen?<SourceMonitor onCollapse={()=>setSourceOpen(false)} seekTo={sourceSeek} onSeeked={()=>setSourceSeek(null)}/>:null}
   <section className="editor-monitor program-monitor" aria-label="节目监视器">
    <div className="editor-monitor-title">
     <strong>节目</strong>
     <Typography.Text ellipsis>{s?.name ?? '当前序列'}</Typography.Text>
     <Typography.Text type="secondary">{s?`${s.width}×${s.height} · ${fpsValue(s.fps).toFixed(3)}fps`:''}</Typography.Text>
     {hasRange&&<Typography.Text type="secondary" className="program-inout-label">导出 {frameLabel(inFrame!,s!.fps)}–{frameLabel(outFrame!,s!.fps)}</Typography.Text>}
     {!sourceOpen&&<button type="button" className="source-monitor-badge" onClick={()=>setSourceOpen(true)} title="打开源监视器" aria-label={`打开源监视器：${sourceLabel}`}>源 · {sourceLabel}</button>}
    </div>
    <div className="program-monitor-row">
     <div className="editor-monitor-screen">{e.previewURL?<video key={e.previewURL} ref={e.program} controls preload="metadata" src={e.previewURL} aria-label="节目播放器" onLoadedMetadata={event=>{if(s)event.currentTarget.currentTime=e.head/fpsValue(s.fps);}} onTimeUpdate={event=>{if(s&&event.currentTarget.readyState>=1&&!event.currentTarget.seeking)e.setHead(Math.round(event.currentTarget.currentTime*fpsValue(s.fps)),false);}}/>:<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={e.previewLoading?'正在更新当前轨道合成…':'把资源拖入时间线开始编辑'}/>}{e.previewLoading&&<div className="program-updating" role="status"><Spin size="small"/> 更新节目预览</div>}
      {hasRange&&s&&e.previewURL&&<div className="program-inout-overlay" aria-hidden="true"><span className="program-inout-in" style={{left:`${Math.min(100,Math.max(0,inFrame!/Math.max(1,outFrame!))*100)}%`}}/><span className="program-inout-out" style={{left:`${Math.min(100,Math.max(0,outFrame!/Math.max(outFrame!,inFrame!+1))*100)}%`}}/></div>}
     </div>
     <ProgramVu media={e.program}/>
    </div>
    <Flex gap={6} align="center" className="editor-monitor-controls" wrap>
     <Button size="small" icon={<StepBackwardOutlined/>} aria-label="节目后退一帧" onClick={()=>e.setHead(e.head-1)}/>
     <Button size="small" icon={<PlayCircleOutlined/>} onClick={e.play} disabled={!e.preview}>播放 / 暂停</Button>
     <Button size="small" icon={<StepForwardOutlined/>} aria-label="节目前进一帧" onClick={()=>e.setHead(e.head+1)}/>
     {s?<TimecodeField label="节目时间码" frame={e.head} rate={s.fps} onSeek={frame=>e.setHead(frame)}/>:<Typography.Text code>--:--:--:--</Typography.Text>}
     <Button size="small" onClick={()=>e.marker('in')}>入点 I</Button>
     <Button size="small" onClick={()=>e.marker('out')}>出点 O</Button>
     <Button size="small" icon={<ExportOutlined/>} disabled={!e.preview||e.pending||e.exportStatus.active} onClick={()=>void e.exportVideo().catch(()=>{})}>导出当前范围</Button>
    </Flex>
    {e.previewError&&<Alert type="warning" message={e.previewError} action={<Button size="small" onClick={e.refreshPreview}>重试预览</Button>}/>}
   </section>
  </div>
  {(e.exportStatus.active||e.exportStatus.error)&&(
   <div className="editor-export-bar" role="status" aria-label="导出进度">
    <span>{e.exportStatus.label}</span>
    <Progress percent={e.exportStatus.percent} size="small" showInfo={false} strokeColor={ACCENT} trailColor={CONTROL_BG} status={e.exportStatus.error?'exception':undefined}/>
    {e.exportStatus.error?<Typography.Text type="danger">{e.exportStatus.error}</Typography.Text>:null}
    <Button type="text" size="small" aria-label="关闭导出进度" onClick={e.dismissExport}>关闭</Button>
   </div>
  )}
  <Flex className="editor-panels-bar" align="center" gap={8} wrap>
   <Typography.Text type="secondary">时间线</Typography.Text>
   <Button size="small" type="link" onClick={()=>window.dispatchEvent(new Event('vac:open-script-dock'))}>脚本工作台</Button>
   <Button size="small" type="link" onClick={()=>navigate(drawRoute({ project: p.session?.document?.project.id, engine: 'hyperframes', focus: 'inspector' }))}>绘制</Button>
   <Space><Button size="small" icon={<PlusOutlined/>} onClick={()=>void e.manageSequence('duplicate',e.sequenceId).catch(()=>{})}>复制序列</Button>{e.pending&&<Typography.Text type="secondary">正在提交编辑…</Typography.Text>}</Space>
  </Flex>
  {e.loading?<Spin/>:<SequenceTimeline onOpenDraw={()=>navigate(drawRoute({ project: p.session?.document?.project.id, engine: 'hyperframes', focus: 'timeline' }))}/>}
 </div>;
}
