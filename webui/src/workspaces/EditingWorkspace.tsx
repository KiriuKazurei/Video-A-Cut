import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Empty, Flex, Segmented, Space, Spin, Table, Tabs, Typography } from 'antd';
import { PlayCircleOutlined, StepBackwardOutlined, StepForwardOutlined, PlusOutlined, ExportOutlined } from '@ant-design/icons';
import type { Asset, WorkflowRun } from '../types';
import type { RouteState } from '../navigation';
import type { RunSelection } from '../components/WorkflowPanel';
import { ScriptWorkbench } from '../components/ScriptWorkbench';
import { SkillRunModule } from '../components/SkillRunModule';
import { SequenceTimeline } from '../components/SequenceTimeline';
import { useSequenceEditor } from '../sequenceEditor';
import { useProjectWorkspace } from '../projectWorkspace';
import { projectApi } from '../projectApi';
import { fpsValue, frameLabel } from '../timelineContracts';
import '../timeline.css';

interface EditingWorkspaceProps { route:RouteState;navigate:(state:Partial<RouteState>)=>void;asset?:Asset;selection:RunSelection;onSelect:(id:string)=>void;onPreparedStarted:(run:WorkflowRun)=>void }
function SourceMonitor() {
 const p=useProjectWorkspace(),e=useSequenceEditor(),r=p.resource,[proxy,setProxy]=useState(false),[proxyError,setProxyError]=useState(''),request=useRef<AbortController|null>(null);
 useEffect(()=>{const media=e.source.current;media?.pause();request.current?.abort();setProxy(false);setProxyError('');return()=>{media?.pause();request.current?.abort();};},[e.active,p.session?.id,r?.id,r?.source.sha256]);
 const textual=r?.kind==='script'||r?.kind==='document'||r?.kind==='subtitle';
 const text=useQuery({queryKey:['source-text',p.session?.id,r?.id],queryFn:async({signal})=>{const response=await fetch(projectApi.contentURL(e.projectId,r!.id),{signal});if(!response.ok)throw Error('内容加载失败');return (await response.text()).slice(0,65536);},enabled:e.active&&!!r&&textual});
 const url=r?projectApi.contentURL(e.projectId,r.id,proxy):'';
 const fallback=()=>{if(!e.active||request.current&&!request.current.signal.aborted)return;if(!r||proxy){setProxyError('源预览加载失败，请检查资源或重新定位');return;}const controller=new AbortController();request.current=controller;void projectApi.createPreview(e.projectId,r.id,controller.signal).then(()=>{if(!controller.signal.aborted)setProxy(true);}).catch(reason=>{if(!controller.signal.aborted)setProxyError(String(reason));}).finally(()=>{if(request.current===controller)request.current=null;});};
 if(!e.active)return null;
 return <section className="editor-monitor source-monitor" aria-label="源监视器"><div className="editor-monitor-title"><strong>源</strong><Typography.Text ellipsis>{r?.name ?? '选择工程资源'}</Typography.Text></div>
  <div className="editor-monitor-screen">{!r?<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="从左侧资源管理选择素材"/>:r.missing||r.removed?<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="源文件待重新关联"/>:r.kind==='video'||r.kind==='audio'?<video key={`${r.id}-${proxy}`} ref={e.source} data-resource-id={r.id} controls preload="metadata" src={url} aria-label="源素材播放器" onError={fallback}/>:r.kind==='image'?<img alt={r.name} src={url} onError={fallback}/>:<pre className="source-text-preview">{text.data ?? (text.isError?'内容读取失败':'正在读取…')}</pre>}</div>
  {proxyError&&<Alert type="warning" message={proxyError}/>}<Flex gap={6} className="editor-monitor-controls" wrap><Button size="small" onClick={()=>e.marker('point',true)} disabled={!r||!['video','audio','image'].includes(r.kind)}>源标记</Button><Button size="small" onClick={()=>void e.addResource(r!.id).catch(()=>{})} disabled={!r||r.missing||r.removed||!['video','audio','image','subtitle'].includes(r.kind)}>加入序列</Button></Flex>
 </section>;
}
export function EditingWorkspace({route,navigate,asset}:EditingWorkspaceProps) {
 const e=useSequenceEditor(),p=useProjectWorkspace(),s=e.view?.sequence,[panel,setPanel]=useState('timeline'),[monitor,setMonitor]=useState('program'),[visited,setVisited]=useState<string[]>([]);
 useEffect(()=>{setVisited(old=>old.includes(panel)?old:[...old,panel]);},[panel]);
 useEffect(()=>{setPanel(route.editingSection==='content'?'script':route.editingSection==='review'?'skill':'timeline');},[route.editingSection,route.scriptId]);
 const scriptAsset=p.session?.document?.resources.find(r=>r.script_id===route.scriptId)?.asset_id ?? asset?.asset_id ?? p.session?.document?.resources.find(r=>r.asset_id)?.asset_id;
 const items=p.session?.document?.sequences?.map(seq=>({key:seq.id,label:seq.name,closable:(p.session?.document?.sequences.length ?? 0)>1}));
 return <div className="unified-editor" data-panel={panel} tabIndex={-1} aria-label="统一多轨编辑器">
  <Tabs className="sequence-tabs" type="editable-card" size="small" activeKey={e.sequenceId} items={items} onChange={e.switchSequence} onEdit={(key,action)=>void e.manageSequence(action==='add'?'create':'delete',typeof key==='string'?key:undefined).catch(()=>{})}/>
  {e.error&&<Alert type="error" showIcon message={e.error}/>}
  <Segmented className="compact-monitor-switch" aria-label="切换紧凑监视器" value={monitor} onChange={setMonitor} options={[{value:'source',label:'源素材'},{value:'program',label:'节目预览'}]}/>
  <div className={`editor-monitors compact-${monitor}`}><SourceMonitor/>
   <section className="editor-monitor program-monitor" aria-label="节目监视器"><div className="editor-monitor-title"><strong>节目</strong><Typography.Text ellipsis>{s?.name ?? '当前序列'}</Typography.Text><Typography.Text type="secondary">{s?`${s.width}×${s.height} · ${fpsValue(s.fps).toFixed(3)}fps`:''}</Typography.Text></div>
    <div className="editor-monitor-screen">{e.previewURL?<video key={e.previewURL} ref={e.program} controls preload="metadata" src={e.previewURL} aria-label="节目播放器" onLoadedMetadata={event=>{if(s)event.currentTarget.currentTime=e.head/fpsValue(s.fps);}} onTimeUpdate={event=>{if(s&&event.currentTarget.readyState>=1&&!event.currentTarget.seeking)e.setHead(Math.round(event.currentTarget.currentTime*fpsValue(s.fps)),false);}}/>:<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={e.previewLoading?'正在更新当前轨道合成…':'把资源拖入时间线开始编辑'}/>}{e.previewLoading&&<div className="program-updating" role="status"><Spin size="small"/> 更新节目预览</div>}</div>
    <Flex gap={6} align="center" className="editor-monitor-controls" wrap><Button size="small" icon={<StepBackwardOutlined/>} aria-label="节目后退一帧" onClick={()=>e.setHead(e.head-1)}/><Button size="small" icon={<PlayCircleOutlined/>} onClick={e.play} disabled={!e.preview}>播放 / 暂停</Button><Button size="small" icon={<StepForwardOutlined/>} aria-label="节目前进一帧" onClick={()=>e.setHead(e.head+1)}/><Typography.Text code>{s?frameLabel(e.head,s.fps):'--:--:--:--'}</Typography.Text><Button size="small" onClick={()=>e.marker('in')}>入点 I</Button><Button size="small" onClick={()=>e.marker('out')}>出点 O</Button><Button size="small" icon={<ExportOutlined/>} disabled={!e.preview||e.pending} onClick={()=>void e.exportVideo().catch(()=>{})}>导出当前范围</Button></Flex>
    {e.previewError&&<Alert type="warning" message={e.previewError} action={<Button size="small" onClick={e.refreshPreview}>重试预览</Button>}/>}
   </section>
  </div>
  {e.loading?<Spin/>:<SequenceTimeline/>}
  <Flex className="editor-panels-bar" align="center" gap={8} wrap><Segmented aria-label="编辑器面板" value={panel} onChange={value=>void p.flushEditors().then(()=>setPanel(value)).catch(()=>{})} options={[{value:'timeline',label:'时间线'},{value:'script',label:'脚本工作台'},{value:'skill',label:'Skill 任务'},{value:'history',label:'编辑历史'}]}/><Space><Button size="small" icon={<PlusOutlined/>} onClick={()=>void e.manageSequence('duplicate',e.sequenceId).catch(()=>{})}>复制序列</Button>{e.pending&&<Typography.Text type="secondary">正在提交编辑…</Typography.Text>}</Space></Flex>
  <div className="editor-panel-content" hidden={panel==='timeline'}>
   {visited.includes('script')&&<div hidden={panel!=='script'}>{scriptAsset?<ScriptWorkbench key={`script-${scriptAsset}`} assetId={scriptAsset} navigate={navigate} selectedScriptId={route.scriptId} active={route.workspace==='editing'&&panel==='script'}/>:<Empty description="导入素材或 .avs 脚本后开始编辑"/>}</div>}
   {visited.includes('skill')&&<div hidden={panel!=='skill'}><SkillRunModule navigate={navigate} active={route.workspace==='editing'&&panel==='skill'}/></div>}
   {panel==='history'&&<Table size="small" pagination={false} rowKey="id" dataSource={e.view?.history ?? []} columns={[{title:'操作',dataIndex:'label'},{title:'来源',dataIndex:'actor'},{title:'状态',dataIndex:'applied',render:value=>value?'已应用':'已撤销'}]}/>}
  </div>
 </div>;
}
