import {request} from './api.ts';
import {apiURL} from './runtimeUrl.ts';
import type {ProjectSession} from './projectContracts';
import type {TimelineView} from './timelineContracts';
import type {DrawComposition,DrawRender,DrawSaveRequest,DrawActionRequest,DrawImportRequest,DrawDocument,DrawPreviewFrame,DrawOutputFormat} from './drawContracts';
const key=encodeURIComponent;
const path=(project:string,id?:string)=>'/api/projects/'+key(project)+'/draw-compositions'+(id?'/'+key(id):'');
const json=(value:unknown)=>({method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(value)});
export const drawApi={
 history:(project:string,id:string)=>request<{can_undo:boolean;can_redo:boolean}>(path(project,id)+'/history'),
 moveHistory:(project:string,id:string,value:{session:string;expected_revision:number;composition_revision:number;idempotency_key:string;action:'undo'|'redo'})=>request<ProjectSession>(path(project,id)+'/history',json(value)),
 preview:(project:string,id:string,value:DrawActionRequest&{request_id:string;frame:number;document:DrawDocument},signal?:AbortSignal)=>request<DrawPreviewFrame>(path(project,id)+'/preview',{...json(value),signal}),
 closePreview:(project:string,id:string,session:string)=>request(path(project,id)+'/preview?session='+key(session),{method:'DELETE'}),
 capabilities:(project:string)=>request<Record<string,unknown>>('/api/projects/'+key(project)+'/draw-capabilities'),
 list:(project:string,signal?:AbortSignal)=>request<DrawComposition[]>(path(project),{signal}),
 save:(project:string,value:DrawSaveRequest)=>request<ProjectSession>(path(project),json(value)),
 check:(project:string,id:string,value:DrawActionRequest)=>request<{valid:boolean;checks:string[];engine:string;runtime_version:string}>(path(project,id)+'/check',json(value)),
 render:(project:string,id:string,value:DrawActionRequest&{format?:DrawOutputFormat})=>request<DrawRender>(path(project,id)+'/render',json(value)),
 export:(project:string,id:string,value:DrawActionRequest&{format?:DrawOutputFormat;path:string})=>request<DrawRender>(path(project,id)+'/export',json(value)),
 import:(project:string,id:string,value:DrawImportRequest)=>request<TimelineView>(path(project,id)+'/import',json(value)),
 renderURL:(project:string,id:string,version:string,format?:DrawOutputFormat)=>apiURL(path(project,id)+'/render?version='+key(version)+(format?'&format='+key(format):''))
};
