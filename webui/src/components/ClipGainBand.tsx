import { useRef, useState, type PointerEvent } from 'react';
import { useSequenceEditor } from '../sequenceEditor';
import {
  GAIN_HANDLE, GAIN_LINE, dbToY, yToDb, effectiveKeyframes, interpolateGain,
  insertKeyframe, deleteKeyframe, moveKeyframe, shiftGain, formatGainDb, constantGainEffects, type GainKeyframe
} from '../gainEnvelope';
import type { ClipEffects, TimelineClip, TimelineTrack } from '../timelineContracts';

type Drag =
  | { kind:'line'; y0:number; db0:number; points:GainKeyframe[] }
  | { kind:'point'; index:number; points:GainKeyframe[] };

/** Premiere-style gain rubber-band drawn over an audio clip. */
export function ClipGainBand({clip,track,px,height}:{clip:TimelineClip;track:TimelineTrack;px:number;height:number}) {
  const e=useSequenceEditor();
  const width=Math.max(3,clip.duration*px);
  const bandH=Math.max(12,height-20);
  const points=effectiveKeyframes(clip.effects);
  const [draft,setDraft]=useState<GainKeyframe[]|null>(null);
  const [readout,setReadout]=useState<{x:number;y:number;text:string}|null>(null);
  const drag=useRef<Drag|null>(null);
  const active=draft??points;
  // Handles only when the user has (or is editing) a real envelope — not the synthetic flat endpoints.
  const showHandles=Boolean(draft||(Array.isArray(clip.effects.gain_keyframes)&&clip.effects.gain_keyframes.length));
  const editable=!track.locked&&e.tool==='select'&&!e.pending;

  const commit=(next:GainKeyframe[], label:string)=>{
    const normalized=next.length?next:[{t:0,db:clip.effects.gain_db||0},{t:1,db:clip.effects.gain_db||0}];
    const effects:ClipEffects={...clip.effects,gain_db:normalized[0].db,gain_keyframes:normalized};
    void e.command([{type:'clip.update',clip_ids:[clip.id],effects,unlinked:true}],label).catch(()=>{});
  };
  const commitConstant=(db:number, label:string)=>{
    void e.command([{type:'clip.update',clip_ids:[clip.id],effects:constantGainEffects(clip.effects,db),unlinked:true}],label).catch(()=>{});
  };

  const onLineDown=(event:PointerEvent<SVGElement>)=>{
    if(!editable||event.button!==0)return;
    event.stopPropagation();event.preventDefault();
    (event.currentTarget as Element).setPointerCapture?.(event.pointerId);
    drag.current={kind:'line',y0:event.clientY,db0:interpolateGain(active,0.5),points:active};
    setDraft(active);setReadout({x:event.clientX,y:event.clientY,text:formatGainDb(interpolateGain(active,0.5))});
  };
  const onPointDown=(event:PointerEvent<SVGElement>,index:number)=>{
    if(!editable)return;
    event.stopPropagation();event.preventDefault();
    if(event.altKey){
      const next=deleteKeyframe(active,index);
      if(next.length<=1){
        const db=next[0]?.db ?? (clip.effects.gain_db||0);
        void e.command([{type:'clip.update',clip_ids:[clip.id],effects:{...clip.effects,gain_db:db,gain_keyframes:undefined},unlinked:true}],'删除音量关键帧').catch(()=>{});
      } else commit(next,'删除音量关键帧');
      return;
    }
    (event.currentTarget as Element).setPointerCapture?.(event.pointerId);
    drag.current={kind:'point',index,points:active};
    setDraft(active);setReadout({x:event.clientX,y:event.clientY,text:formatGainDb(active[index].db)});
  };
  const onMove=(event:PointerEvent<SVGElement>)=>{
    const d=drag.current;if(!d)return;event.stopPropagation();
    const svg=(event.currentTarget.ownerSVGElement??event.currentTarget as SVGSVGElement) as SVGSVGElement;
    const rect=svg.getBoundingClientRect();
    if(d.kind==='line'){
      const dy=event.clientY-d.y0, mid=bandH/2;
      // Asymmetric: dragging up toward +12, down toward -24.
      const deltaDb=dy<=0?(-dy/mid)*12:-(dy/mid)*24;
      const next=shiftGain(d.points, deltaDb);
      setDraft(next);setReadout({x:event.clientX-rect.left,y:Math.max(0,dbToY(interpolateGain(next,0.5),bandH)-14),text:formatGainDb(interpolateGain(next,0.5))});
    } else {
      const t=(event.clientX-rect.left)/Math.max(1,rect.width);
      const db=yToDb(event.clientY-rect.top, bandH);
      const next=moveKeyframe(d.points,d.index,t,db);
      const p=next[Math.min(d.index,next.length-1)];
      setDraft(next);setReadout({x:p.t*width,y:Math.max(0,dbToY(p.db,bandH)-14),text:formatGainDb(p.db)});
    }
  };
  const onUp=(event:PointerEvent<SVGElement>)=>{
    const d=drag.current;drag.current=null;setReadout(null);
    if(!d||!draft){setDraft(null);return;}
    event.stopPropagation();
    // Whole-line drag with no stored keyframes stays a constant (straight line), not a 2-point envelope.
    if(d.kind==='line'&&!(Array.isArray(clip.effects.gain_keyframes)&&clip.effects.gain_keyframes.length)){
      commitConstant(interpolateGain(draft,0.5),'调节音量');
    } else {
      commit(draft, d.kind==='line'?'调节音量':'调节音量关键帧');
    }
    setDraft(null);
  };
  const onDouble=(event:PointerEvent<SVGElement>)=>{
    if(!editable||event.button!==0)return;
    event.stopPropagation();event.preventDefault();
    const rect=(event.currentTarget as SVGElement).getBoundingClientRect();
    const t=(event.clientX-rect.left)/Math.max(1,rect.width);
    commit(insertKeyframe(active,t),'添加音量关键帧');
  };

  return <>
    <svg className="clip-gain-band" width={width} height={bandH} viewBox={`0 0 ${width} ${bandH}`} preserveAspectRatio="none"
      aria-label={`音量增益线 ${formatGainDb(interpolateGain(active,0))}`}
      onPointerDown={onLineDown} onPointerMove={onMove} onPointerUp={onUp} onPointerCancel={()=>{drag.current=null;setDraft(null);setReadout(null);}}>
      <polyline points={active.map(p=>`${p.t*width},${dbToY(p.db,bandH)}`).join(' ')} fill="none" stroke={GAIN_LINE} strokeWidth={2} vectorEffect="non-scaling-stroke" style={{pointerEvents:'stroke'}}/>
      {/* Invisible wider stroke for easier grabbing */}
      <polyline points={active.map(p=>`${p.t*width},${dbToY(p.db,bandH)}`).join(' ')} fill="none" stroke="transparent" strokeWidth={10} vectorEffect="non-scaling-stroke" style={{pointerEvents:'stroke',cursor:editable?'ns-resize':'default'}} onDoubleClick={onDouble as never}/>
      {showHandles&&active.map((p,i)=><rect key={i} x={p.t*width-GAIN_HANDLE/2} y={dbToY(p.db,bandH)-GAIN_HANDLE/2} width={GAIN_HANDLE} height={GAIN_HANDLE} rx={2} ry={2} fill={GAIN_LINE}
        style={{cursor:editable?'move':'default',pointerEvents:editable?'all':'none'}}
        onPointerDown={ev=>onPointDown(ev,i)} onPointerMove={onMove} onPointerUp={onUp}/>)}
    </svg>
    {readout&&<div className="clip-gain-readout" style={{left:Math.min(width-56,Math.max(0,readout.x)),top:readout.y}}>{readout.text}</div>}
  </>;
}
