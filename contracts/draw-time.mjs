// Browser-safe time contract shared by the scene and frame adapter. Integer
// arithmetic prevents rate/speed drift; children are in the group's local clock.
export function drawFrameContext(document,id,globalFrame,memo=new Map(),visiting=new Set()) {
 if(memo.has(id))return memo.get(id);
 if(visiting.has(id))throw Error('Drawing time graph contains a cycle');
 const node=document.nodes.find(n=>n.id===id);if(!node)throw Error('Missing drawing time node');
 visiting.add(id);
 let parentFrame=globalFrame,parentActive=true;
 if(node.parent_id){const parent=drawFrameContext(document,node.parent_id,globalFrame,memo,visiting);parentFrame=parent.frame;parentActive=parent.active;}
 const fps=document.sources?.find(s=>s.id===node.source_id)?.canvas?.fps??document.canvas.fps;
 const active=parentActive&&!node.hidden&&parentFrame>=node.from&&parentFrame<node.from+node.duration;
 let frame=0;
 if(active){
  const rate=node.kind==='source'?fps:document.canvas.fps,base=document.canvas.fps;
  frame=(node.source_offset??0)+Number(BigInt(parentFrame-node.from)*BigInt(node.speed?.num??1)*BigInt(rate.num)*BigInt(base.den)/(BigInt(node.speed?.den??1)*BigInt(rate.den)*BigInt(base.num)));
 }
 const result={active,parent_frame:parentFrame,frame,fps};visiting.delete(id);memo.set(id,result);return result;
}


/** Visible global interval, accounting for all positive rational parent clocks.
 * Binary searches stay bounded even for long compositions and deep groups. */
export function drawNodeRange(document,id,memo=new Map()) {
 if(memo.has(id))return memo.get(id);
 const node=document.nodes.find(n=>n.id===id);if(!node)throw Error('Missing drawing time node');
 const parent=node.parent_id?drawNodeRange(document,node.parent_id,memo):{from:0,to:document.duration_frames};
 if(node.hidden||parent.to<=parent.from){const empty={from:parent.from,to:parent.from};memo.set(id,empty);return empty;}
 const lower=target=>{let lo=parent.from,hi=parent.to;while(lo<hi){const mid=Math.floor((lo+hi)/2),frame=drawFrameContext(document,id,mid).parent_frame;if(frame<target)lo=mid+1;else hi=mid;}return lo;};
 const from=lower(node.from),to=lower(node.from+node.duration),range={from,to:Math.max(from,to)};memo.set(id,range);return range;
}
