import type {ProjectResource} from './projectContracts';
import type { FrameRate } from './timelineContracts';
export const DRAW_ENGINES=['hyperframes','remotion'] as const;
export type DrawEngine=(typeof DRAW_ENGINES)[number];
export const DRAW_FOCUSES=['canvas','timeline','source','inspector','lint','render-queue'] as const;
export type DrawFocus=(typeof DRAW_FOCUSES)[number];
export const DRAW_ZONES=[{id:'source',label:'构图、图层与源码'},{id:'canvas',label:'构图预览'},{id:'timeline',label:'构图时间轴'},{id:'inspector',label:'Inspector、Lint 与渲染队列'}] as const;
export type DrawZoneId=(typeof DRAW_ZONES)[number]['id'];
export const DRAW_ENTRY_SKILLS=['video-use','vac-draw-hyperframes','vac-draw-remotion','vac-draw-pipeline'] as const;
export const DRAW_HANDOFF_ACTIONS=['import_into_editing','composite_preview','merge_render'] as const;
export type DrawHandoffAction=(typeof DRAW_HANDOFF_ACTIONS)[number];
export const DRAW_HANDOFF_COPY={
 import_into_editing:{label:'导入编辑时间线',detail:'将当前源码对应的已验证成片作为独立轨组导入，可继续剪辑和添加效果。'},
 composite_preview:{label:'未渲源码拼合',detail:'尚未开放。当前可将已渲资源在编辑时间线中组合预览。'},
 merge_render:{label:'未渲源码一并渲染',detail:'尚未开放。当前通过编辑时间线导出已渲资源与原素材。'}
};
export interface DrawImageLayer {
 id:string;resource_id:string;resource_sha256?:string;x:number;y:number;width:number;height:number;dx:number;dy:number;from_frame:number;to_frame:number;
 fit?:'contain'|'cover'|'fill';mode?:'layer'|'source';opacity?:number;
}
export interface DrawImageResource {id:string;name:string;sha256:string;metadata:Record<string,unknown>;missing:boolean;changed:boolean}
export interface DrawComposition {
 images?:DrawImageLayer[];id:string;name:string;engine:DrawEngine;source:string;width:number;height:number;fps:FrameRate;duration_frames:number;revision:number;content_version:string;
}
export interface DrawRender {
 content_version:string;sha256:string;size_bytes:number;width:number;height:number;frames:number;duration:number;engine:DrawEngine;runtime_version:string;
}
export interface DrawSaveRequest {
 session:string;expected_revision:number;idempotency_key:string;composition:DrawComposition;
}
export interface DrawActionRequest {session:string;content_version:string}
export interface DrawImportRequest extends DrawActionRequest {sequence_id:string;expected_revision:number;idempotency_key:string}
export interface DrawDeepLink {project?:string;run?:string;slot?:string;engine?:DrawEngine;focus?:DrawFocus}
export function isDrawEngine(value:string|null|undefined):value is DrawEngine{return value==='hyperframes'||value==='remotion';}
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
export function newDrawComposition(engine:DrawEngine):DrawComposition{
 const source=engine==='remotion'
 ?'import React from "react";\nimport {AbsoluteFill, useCurrentFrame} from "remotion";\nexport default function Scene(){\n const frame=useCurrentFrame();\n return <AbsoluteFill style={{background:"#1e1e1e",color:"#ffffff",fontFamily:"sans-serif",justifyContent:"center",alignItems:"center"}}>\n  <h1 style={{transform:"translateX("+(frame*2-90)+"px)"}}>VAC</h1>\n </AbsoluteFill>;\n}\n'
 :'<!doctype html>\n<html><head><meta charset="utf-8"><style>\nbody{margin:0;background:#1e1e1e;color:#ffffff;font-family:sans-serif}\n#root{position:relative;width:640px;height:360px;overflow:hidden;display:grid;place-items:center}\n</style></head><body>\n<div id="root" data-composition-id="root" data-width="640" data-height="360" data-duration="3"><h1 id="title">VAC</h1></div>\n<script>\nconst tl=gsap.timeline({paused:true});\ntl.fromTo("#title",{x:-90,opacity:0},{x:90,opacity:1,duration:3,ease:"none"},0);\nwindow.__timelines={root:tl};\n</script></body></html>\n';
 return {id:'',name:engine==='remotion'?'React 构图':'HTML 构图',engine,source,width:640,height:360,fps:{num:30,den:1},duration_frames:90,revision:0,content_version:''};
}
export function drawDraftChanged(draft:DrawComposition|undefined,saved:DrawComposition|undefined){
 if(!draft||!saved)return false;
 return ['name','source','width','height','duration_frames'].some(key=>draft[key as keyof DrawComposition]!==saved[key as keyof DrawComposition])||draft.fps.num!==saved.fps.num||draft.fps.den!==saved.fps.den||JSON.stringify(draft.images??[])!==JSON.stringify(saved.images??[]);
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
