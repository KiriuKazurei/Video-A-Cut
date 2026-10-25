import {request} from './api.ts';
import {apiURL} from './runtimeUrl.ts';
import type {ProjectSession} from './projectContracts';
import type {TimelineView} from './timelineContracts';
import type {DrawComposition,DrawRender,DrawSaveRequest,DrawActionRequest,DrawImportRequest} from './drawContracts';
const key=encodeURIComponent;
const path=(project:string,id?:string)=>'/api/projects/'+key(project)+'/draw-compositions'+(id?'/'+key(id):'');
const json=(value:unknown)=>({method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(value)});
export const drawApi={
 list:(project:string,signal?:AbortSignal)=>request<DrawComposition[]>(path(project),{signal}),
 save:(project:string,value:DrawSaveRequest)=>request<ProjectSession>(path(project),json(value)),
 check:(project:string,id:string,value:DrawActionRequest)=>request<{valid:boolean;checks:string[];engine:string;runtime_version:string}>(path(project,id)+'/check',json(value)),
 render:(project:string,id:string,value:DrawActionRequest)=>request<DrawRender>(path(project,id)+'/render',json(value)),
 import:(project:string,id:string,value:DrawImportRequest)=>request<TimelineView>(path(project,id)+'/import',json(value)),
 renderURL:(project:string,id:string,version:string)=>apiURL(path(project,id)+'/render?version='+key(version))
};
