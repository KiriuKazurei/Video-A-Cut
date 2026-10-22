import { request } from './api';
import { apiURL } from './runtimeUrl';
import type { ProjectSession } from './projectContracts';
import type { ReferenceCandidate, ScriptReference, TimelineOperation, TimelineView, SequencePreview } from './timelineContracts';
const id = encodeURIComponent;
const body = (value: unknown): RequestInit => ({method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(value)});
const base = (project: string,sequence: string) => `/api/projects/${id(project)}/sequences/${id(sequence)}`;
export const timelineApi = {
 get: (project: string,sequence: string,signal?: AbortSignal) => request<TimelineView>(base(project,sequence),{signal}),
 manage: (project: string,value: {session:string;expected_revision:number;idempotency_key:string;action:string;sequence_id?:string;name?:string}) => request<ProjectSession>(`/api/projects/${id(project)}/sequences`,body(value)),
 command: (project:string,sequence:string,value:{session:string;expected_revision:number;idempotency_key:string;label:string;operations:TimelineOperation[]}) => request<TimelineView>(`${base(project,sequence)}/commands`,body(value)),
 preview: (project:string,sequence:string,value:{session:string;expected_revision:number;content_version:string},signal?:AbortSignal) => request<SequencePreview>(`${base(project,sequence)}/preview`,{...body(value),signal}),
 export: (project:string,sequence:string,value:{session:string;expected_revision:number;content_version:string;path:string}) => request<SequencePreview>(`${base(project,sequence)}/export`,body(value)),
 importResult:(project:string,sequence:string,value:{session:string;expected_revision:number;idempotency_key:string;run_id:string;artifact_id:string;checkpoint_id?:string})=>request<TimelineView>(`${base(project,sequence)}/skill-results`,body(value)),
 previewURL: (project:string,sequence:string,version:string) => apiURL(`${base(project,sequence)}/preview?version=${id(version)}`),
 references: (project:string,sequence?:string,signal?:AbortSignal) => request<ReferenceCandidate[]>(`/api/projects/${id(project)}/references${sequence ? `?sequence=${id(sequence)}` : ''}`,{signal}),
 resolve: (project:string,references:ScriptReference[]) => request<ReferenceCandidate[]>(`/api/projects/${id(project)}/references/resolve`,body({references})),
 waveform: (project:string,resource:string,stream:number,signal?:AbortSignal) => request<{peaks:number[];duration:number;stream_index:number}>(`/api/projects/${id(project)}/resources/${id(resource)}/waveform?stream=${stream}`,{signal}),
 thumbnailURL: (project:string,resource:string,at:number) => apiURL(`/api/projects/${id(project)}/resources/${id(resource)}/thumbnail?at=${at}`)
};
