import type {ProjectResource} from './projectContracts';
import type { FrameRate } from './timelineContracts';
import type { SkillRun } from './skillContracts';

/** Render observed state and Agent-reported progress without invented percentages. */
export function drawAgentStatus(run:SkillRun|undefined,now:number):string {
 if(!run)return '';
 if(run.state==='succeeded')return '制作完成';
 if(run.state==='cancelled')return '任务已取消';
 if(run.state==='failed')return '任务失败，请查看任务详情';
 if(run.state==='waiting_human')return run.reason==='model_output_truncated'?'模型输出额度不足，请在设置中调整':run.reason==='model_request_timeout'?'模型请求超时，请检查连接或任务时限':'任务已暂停，请查看任务详情';
 if(run.reason==='invalid_tool_arguments')return '参数有误，等待 Agent 修正';
 const isAgent=run.owner.startsWith('agent:');
 if(run.execution_mode==='external_mcp'&&isAgent&&run.lease_until&&Date.parse(run.lease_until)<now)return 'Agent 连接已超时，等待重新领取';
 if(run.execution_mode==='external_mcp'&&!isAgent)return '等待 Agent 领取';
 const phase=run.progress?.phase;
 const labels={working:'Agent 已连接，等待下一步',generating:'Agent 正在生成画面',checking:'Agent 正在检查画面',rendering:'Agent 正在渲染视频'};
 const text=run.state==='running'&&phase==='working'?'Agent 正在处理':phase?labels[phase]:'Agent 正在处理';
 const start=run.progress?.started_at?Date.parse(run.progress.started_at):NaN;
 const seconds=Number.isFinite(start)?Math.max(0,Math.floor((now-start)/1000)):0;
 const received=run.progress?.output_chars?` · 已接收 ${run.progress.output_chars} 字符`:'';
 const updated=run.progress?.updated_at?Date.parse(run.progress.updated_at):NaN;
 const age=Number.isFinite(updated)?Math.max(0,Math.floor((now-updated)/1000)):0;
 return text+(seconds?` · ${seconds} 秒`:'' )+received+(age>15?` · 上次更新 ${age} 秒前`:'');
}
export const DRAW_ENGINES=['hyperframes','remotion','unified'] as const;
export type DrawEngine=(typeof DRAW_ENGINES)[number];
export const DRAW_FOCUSES=['canvas','timeline','source','inspector','lint','render-queue'] as const;
export type DrawFocus=(typeof DRAW_FOCUSES)[number];
export const DRAW_ZONES=[{id:'source',label:'构图、图层与源码'},{id:'canvas',label:'构图预览'},{id:'timeline',label:'构图时间轴'},{id:'inspector',label:'Inspector、Lint 与渲染队列'}] as const;
export type DrawZoneId=(typeof DRAW_ZONES)[number]['id'];
export const DRAW_ENTRY_SKILLS=['video-use','vac-draw-hyperframes','vac-draw-remotion','vac-draw-pipeline'] as const;
export const DRAW_HANDOFF_ACTIONS=['import_into_editing','composite_preview','merge_render'] as const;
export type DrawHandoffAction=(typeof DRAW_HANDOFF_ACTIONS)[number];
export const DRAW_HANDOFF_COPY={
 import_into_editing:{label:'导入编辑时间线',detail:'默认加入可编辑源快照，与原轨道独立并存。'},
 composite_preview:{label:'未渲源码拼合',detail:'绘制源与素材轨共用帧合成，无需先导出 MP4。'},
 merge_render:{label:'未渲源码一并渲染',detail:'绘制源以无损帧参与序列合成，只做一次最终编码。'}
};
export interface DrawImageLayer {
 id:string;resource_id:string;resource_sha256?:string;x:number;y:number;width:number;height:number;dx:number;dy:number;from_frame:number;to_frame:number;
 fit?:'contain'|'cover'|'fill';mode?:'layer'|'source';opacity?:number;
}
export interface DrawImageResource {id:string;name:string;sha256:string;metadata:Record<string,unknown>;missing:boolean;changed:boolean}
export interface DrawComposition {
 plan?:DrawRenderPlan;
 document?:DrawDocument;
 images?:DrawImageLayer[];id:string;name:string;engine:DrawEngine;source:string;width:number;height:number;fps:FrameRate;duration_frames:number;revision:number;content_version:string;
}
export interface DrawRender {
 format?:DrawOutputFormat;alpha?:'opaque'|'straight';color_space?:'srgb';
 content_version:string;sha256:string;size_bytes:number;width:number;height:number;frames:number;duration:number;engine:DrawEngine;runtime_version:string;
}
export interface DrawSaveRequest {
 session:string;expected_revision:number;idempotency_key:string;composition:DrawComposition;
}
export interface DrawActionRequest {session:string;content_version:string}
export type DrawOutputFormat='mp4'|'webm-alpha'|'prores4444';
export interface DrawImportRequest extends DrawActionRequest {sequence_id:string;expected_revision:number;idempotency_key:string;source_mode?:'snapshot'|'follow'|'rendered'}
export interface DrawDeepLink {project?:string;run?:string;slot?:string;engine?:DrawEngine;focus?:DrawFocus}
export function isDrawEngine(value:string|null|undefined):value is DrawEngine{return value==='hyperframes'||value==='remotion'||value==='unified';}
export function isDrawFocus(value:string|null|undefined):value is DrawFocus{return !!value&&(DRAW_FOCUSES as readonly string[]).includes(value);}
export function isDrawEntrySkill(id:string|null|undefined){return !!id&&(DRAW_ENTRY_SKILLS as readonly string[]).includes(id);}
export function engineForSkill(id:string|null|undefined):DrawEngine{return id==='vac-draw-remotion'?'remotion':'hyperframes';}
export function zoneForFocus(focus:DrawFocus|undefined):DrawZoneId{return focus==='timeline'?'timeline':focus==='source'?'source':['inspector','lint','render-queue'].includes(focus??'')?'inspector':'canvas';}
export function drawRoute(link:DrawDeepLink={}){
 return {workspace:'draw' as const,drawProject:link.project??'',drawRun:link.run??'',drawSlot:link.slot??'',drawEngine:link.engine,drawFocus:link.focus};
}
export function drawAgentContext(input:{project_id?:string;skill_run_id?:string;slot_id?:string;sequence_id?:string;engine:DrawEngine;focus?:DrawFocus}){
 return ['VAC 绘制上下文','project='+(input.project_id??''),'run='+(input.skill_run_id??''),'slot='+(input.slot_id??''),'sequence='+(input.sequence_id??''),'engine='+input.engine,'strategy=draw','focus='+(input.focus??'canvas'),'shell=unavailable','transport=local-worker','tools=draw.list/save/check/render/get_frame/import; timeline.read/edit; artifacts.get; run.finish'].join('\n');
}
export function newDrawComposition(engine:DrawEngine='unified'):DrawComposition{
 if(engine==='unified')return {id:'',name:'新构图',engine,source:'',width:1280,height:720,fps:{num:30,den:1},duration_frames:150,revision:0,content_version:'',document:{schema_version:1,canvas:{width:1280,height:720,fps:{num:30,den:1},alpha:'straight',color_space:'srgb'},duration_frames:150,preset:'auto',sources:[],nodes:[{id:'title_'+crypto.randomUUID(),name:'标题',kind:'text',text:'开始创作',color:'#ffffff',font_size:64,from:0,duration:150,transform:{x:100,y:280,width:1080,height:150}}]}};
 const source=engine==='remotion'
 ?'import React from "react";\nimport {AbsoluteFill, useCurrentFrame} from "remotion";\nexport default function Scene(){\n const frame=useCurrentFrame();\n return <AbsoluteFill style={{background:"#1e1e1e",color:"#ffffff",fontFamily:"sans-serif",justifyContent:"center",alignItems:"center"}}>\n  <h1 style={{transform:"translateX("+(frame*2-90)+"px)"}}>VAC</h1>\n </AbsoluteFill>;\n}\n'
 :'<!doctype html>\n<html><head><meta charset="utf-8"><style>\nbody{margin:0;background:#1e1e1e;color:#ffffff;font-family:sans-serif}\n#root{position:relative;width:640px;height:360px;overflow:hidden;display:grid;place-items:center}\n</style></head><body>\n<div id="root" data-composition-id="root" data-width="640" data-height="360" data-duration="3"><h1 id="title">VAC</h1></div>\n<script>\nconst tl=gsap.timeline({paused:true});\ntl.fromTo("#title",{x:-90,opacity:0},{x:90,opacity:1,duration:3,ease:"none"},0);\nwindow.__timelines={root:tl};\n</script></body></html>\n';
 return {id:'',name:engine==='remotion'?'React 构图':'HTML 构图',engine,source,width:640,height:360,fps:{num:30,den:1},duration_frames:90,revision:0,content_version:''};
}
export function drawDraftChanged(draft:DrawComposition|undefined,saved:DrawComposition|undefined){
 if(!draft||!saved)return false;
 return ['name','source','width','height','duration_frames'].some(key=>draft[key as keyof DrawComposition]!==saved[key as keyof DrawComposition])||draft.fps.num!==saved.fps.num||draft.fps.den!==saved.fps.den||JSON.stringify(draft.images??[])!==JSON.stringify(saved.images??[])||JSON.stringify(draft.document)!==JSON.stringify(saved.document);
}

export function reconcileDrawDraft(draft:DrawComposition,baseline:DrawComposition,incoming:DrawComposition){
 if(!drawDraftChanged(draft,baseline))return {draft:structuredClone(incoming),baseline:structuredClone(incoming),conflict:false};
 if(draft.revision!==incoming.revision)return {draft,baseline,conflict:true};
 // A resource recipe/fingerprint update does not rewrite authored source.
 const next={...draft,content_version:incoming.content_version,document:draft.document?{...draft.document,asset_bindings:incoming.document?.asset_bindings,nodes:draft.document.nodes.map(n=>({...n,resource_sha256:incoming.document?.nodes.find(s=>s.id===n.id)?.resource_sha256??n.resource_sha256}))}:undefined};
 return {draft:next,baseline:structuredClone(incoming),conflict:false};
}
export function acceptDrawSave(current:DrawComposition,snapshot:DrawComposition,saved:DrawComposition):DrawComposition {
 if(current.id!==snapshot.id)return current;
 if(!drawDraftChanged(current,snapshot))return structuredClone(saved);
 return {...current,revision:saved.revision,content_version:saved.content_version,document:current.document?{...current.document,asset_bindings:saved.document?.asset_bindings,nodes:current.document.nodes.map(n=>({...n,resource_sha256:saved.document?.nodes.find(s=>s.id===n.id)?.resource_sha256??n.resource_sha256}))}:undefined};
}

export interface DrawTransform {x:number;y:number;width?:number;height?:number;rotation?:number;scale_x?:number;scale_y?:number}
export interface DrawSource {id:string;name?:string;engine:'hyperframes'|'remotion';source:string;reason?:string;capabilities?:string[];image_bindings?:DrawImageLayer[];canvas?:DrawDocument['canvas'];duration_frames?:number}
export interface DrawRenderPlan {schema_version:1;policy_version:string;nodes:{node_id:string;engine:string;reason:string;dependencies:string[];capabilities:string[]}[];engines:string[];alpha:'straight';color_space:'srgb';runtime_versions:Record<string,string>;frame_evaluations:number;fallback_policy:string}
export interface DrawNode {id:string;name?:string;kind:'source'|'image'|'text'|'group'|'mask'|'shape';shape?:'rect'|'ellipse';source_id?:string;resource_id?:string;resource_sha256?:string;parent_id?:string;from:number;duration:number;source_offset?:number;speed?:FrameRate;transform:DrawTransform;opacity?:number;z?:number;hidden?:boolean;fit?:'contain'|'cover'|'fill';crop?:{top:number;right:number;bottom:number;left:number};text?:string;color?:string;font_size?:number;parameters?:Record<string,unknown>;keyframes?:{frame:number;x:number;y:number;opacity?:number}[]}
export interface DrawDocument {asset_bindings?:{resource_id:string;sha256:string;original_sha256:string;raw_develop?:import('./projectContracts').RawDevelop}[];schema_version:1;canvas:{width:number;height:number;fps:FrameRate;background?:string;alpha?:'straight';color_space?:'srgb'};duration_frames:number;preset:'auto'|'interface'|'data'|'media';sources:DrawSource[];nodes:DrawNode[];variables?:Record<string,unknown>;theme?:Record<string,unknown>;seed?:number;goal?:string;explanation?:string}
export interface DrawPreviewFrame {session:string;content_version:string;revision:string;request_id:string;frame:number;png:string;geometry:{id:string;x:number;y:number;width:number;height:number}[]}
export function unifiedComposition(c:DrawComposition):DrawComposition {
 if(c.document)return c;
 const document:DrawDocument={schema_version:1,canvas:{width:c.width,height:c.height,fps:c.fps},duration_frames:c.duration_frames,preset:'auto',sources:[{id:'legacy-source',name:c.name,engine:c.engine as 'hyperframes'|'remotion',source:c.source,image_bindings:c.images}],nodes:[{id:'legacy-scene',kind:'source',name:c.name,source_id:'legacy-source',from:0,duration:c.duration_frames,transform:{x:0,y:0}}]};
 for(const im of c.images??[])document.nodes.push({id:im.id,name:'图片',kind:'image',resource_id:im.resource_id,resource_sha256:im.resource_sha256,from:0,duration:c.duration_frames,transform:{x:im.x,y:im.y,width:im.width,height:im.height},opacity:im.opacity,fit:im.fit,hidden:im.mode==='source',z:100+document.nodes.length,...(im.to_frame>im.from_frame?{keyframes:[{frame:im.from_frame,x:im.x,y:im.y},{frame:im.to_frame,x:im.x+im.dx,y:im.y+im.dy}]}:{})});
 return {...c,engine:'unified',source:'',images:undefined,document};
}

export function newDrawImage(resource:ProjectResource,composition:DrawComposition):DrawImageLayer{
 const naturalWidth=Number(resource.metadata.width),naturalHeight=Number(resource.metadata.height);
 const known=naturalWidth>0&&naturalHeight>0&&Number.isFinite(naturalWidth+naturalHeight);
 const ratio=known?Math.min(1,composition.width*.5/naturalWidth,composition.height*.5/naturalHeight):1;
 const width=known?naturalWidth*ratio:composition.width*.5,height=known?naturalHeight*ratio:composition.height*.5;
 return {id:'image_'+crypto.randomUUID(),resource_id:resource.id,resource_sha256:resource.source.sha256,x:(composition.width-width)/2,y:(composition.height-height)/2,width,height,dx:0,dy:0,from_frame:0,to_frame:Math.max(0,composition.duration_frames-1),fit:'contain',mode:'layer',opacity:1};
}
export function drawImagePosition(image:DrawImageLayer,frame:number){
 const progress=image.to_frame>image.from_frame?Math.max(0,Math.min(1,(frame-image.from_frame)/(image.to_frame-image.from_frame))):0;
 return {x:image.x+image.dx*progress,y:image.y+image.dy*progress};
}
export function drawImageReference(engine:DrawEngine,image:DrawImageLayer):string{
 return engine==='hyperframes'?'vac-image:'+encodeURIComponent(image.id):'images['+JSON.stringify(image.id)+'].src';
}
