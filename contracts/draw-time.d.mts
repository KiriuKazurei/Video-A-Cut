export interface DrawClockRate {num:number;den:number}
export interface DrawClockNode {id:string;kind:string;parent_id?:string;source_id?:string;from:number;duration:number;source_offset?:number;speed?:DrawClockRate;hidden?:boolean}
export interface DrawClockDocument {canvas:{fps:DrawClockRate};duration_frames?:number;sources?:{id:string;canvas?:{fps:DrawClockRate}}[];nodes:DrawClockNode[]}
export interface DrawFrameContext {active:boolean;parent_frame:number;frame:number;fps:DrawClockRate}
export function drawFrameContext(document:DrawClockDocument,id:string,globalFrame:number,memo?:Map<string,DrawFrameContext>,visiting?:Set<string>):DrawFrameContext;
export function drawNodeRange(document:DrawClockDocument&{duration_frames:number},id:string,memo?:Map<string,{from:number;to:number}>):{from:number;to:number};
