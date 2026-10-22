import type { ScriptReference, ReferenceCandidate } from './timelineContracts.ts';
export function rebaseReferences(before:string,after:string,references:ScriptReference[]):ScriptReference[] {
 let start=0;while(start<before.length&&start<after.length&&before[start]===after[start])start++;
 let oldEnd=before.length,newEnd=after.length;while(oldEnd>start&&newEnd>start&&before[oldEnd-1]===after[newEnd-1]){oldEnd--;newEnd--;}
 const delta=newEnd-oldEnd;
 return references.flatMap(ref=>{if(ref.end<=start)return [{...ref}];if(ref.start>=oldEnd)return [{...ref,start:ref.start+delta,end:ref.end+delta}];return [];});
}
export const REFERENCE_TYPES:Record<string,string>={video:'视频',audio:'音频',image:'图片',subtitle:'字幕',document:'文稿',script:'脚本',source_marker:'素材标记点',sequence_marker:'时间线标记点'};
/** Merge an authoritative rename into a local draft without replacing prose. */
export function synchronizeReferenceNames(body:string,references:ScriptReference[],directory:ReferenceCandidate[]) {
 let nextBody=body,nextReferences=references.map(ref=>({...ref}));
 const names=references.map(ref=>({ref,candidate:directory.find(c=>['kind','target_id','resource_id','sequence_id','clip_id'].every(key=>(c[key as keyof ReferenceCandidate]??'')===(ref[key as keyof ScriptReference]??'')))})).sort((a,b)=>b.ref.start-a.ref.start);
 for(const {ref,candidate} of names){
  if(ref.unresolved||!candidate||candidate.name===ref.name||nextBody.slice(ref.start,ref.end)!==`{@${ref.name}}`)continue;
  const token=`{@${candidate.name}}`,delta=token.length-(ref.end-ref.start);
  nextBody=nextBody.slice(0,ref.start)+token+nextBody.slice(ref.end);
  nextReferences=nextReferences.map(value=>value.id===ref.id?{...value,name:candidate.name,end:ref.start+token.length}:value.start>=ref.end?{...value,start:value.start+delta,end:value.end+delta}:value);
 }
 return {body:nextBody,references:nextReferences};
}
export function candidateLabel(candidate:ReferenceCandidate){const time=candidate.source_time?`${(candidate.source_time.value/candidate.source_time.scale).toFixed(3)}s`:candidate.frame!==undefined?`第 ${candidate.frame} 帧`:'';return `${candidate.name} · ${REFERENCE_TYPES[candidate.kind] ?? candidate.kind} · ${candidate.owner}${time?` · ${time}`:''}${candidate.clip_id?` · ${candidate.clip_id.slice(-6)}`:''}`;}
export function insertReference(body:string,references:ScriptReference[],start:number,end:number,candidate:ReferenceCandidate){const token=`{@${candidate.name}}`,next=body.slice(0,start)+token+body.slice(end),retained=references.filter(ref=>!(ref.start<end&&ref.end>start));return {body:next,references:[...rebaseReferences(body,next,retained),{id:crypto.randomUUID(),name:candidate.name,kind:candidate.kind,target_id:candidate.target_id,resource_id:candidate.resource_id,sequence_id:candidate.sequence_id,clip_id:candidate.clip_id,start,end:start+token.length,unresolved:false}],caret:start+token.length};}
