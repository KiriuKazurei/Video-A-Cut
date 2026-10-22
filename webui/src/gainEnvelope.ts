/** Premiere-style clip gain rubber-band: keyframes on 0..1 of clip duration. */
export interface GainKeyframe { t:number; db:number }
export const GAIN_TOP_DB=12, GAIN_BOTTOM_DB=-24, GAIN_LINE='#4C8DFF', GAIN_HANDLE=7;

/** Asymmetric map: top=+12 dB, middle=0 dB, bottom=−24 dB. */
export function dbToY(db:number, height:number):number {
 const mid=height/2, c=Math.min(GAIN_TOP_DB, Math.max(GAIN_BOTTOM_DB, db));
 return c>=0 ? mid*(1-c/GAIN_TOP_DB) : mid+mid*(c/GAIN_BOTTOM_DB);
}
export function yToDb(y:number, height:number):number {
 const mid=height/2, yy=Math.min(height, Math.max(0, y));
 return yy<=mid ? GAIN_TOP_DB*(1-yy/mid) : GAIN_BOTTOM_DB*((yy-mid)/mid);
}

/** Normalize: finite values, t clamped to [0,1], sorted, merge duplicate t (last wins). */
export function normalizeKeyframes(points:GainKeyframe[]):GainKeyframe[] {
 const cleaned=points
  .filter(p=>Number.isFinite(p.t)&&Number.isFinite(p.db))
  .map(p=>({t:Math.min(1,Math.max(0,p.t)),db:p.db}))
  .sort((a,b)=>a.t-b.t||0);
 const out:GainKeyframe[]=[];
 for(const p of cleaned){if(out.length&&Math.abs(out[out.length-1].t-p.t)<1e-9)out[out.length-1]=p;else out.push(p);}
 return out;
}

/** Constant gain when keyframes are absent/empty. */
export function effectiveKeyframes(effects:{gain_db?:number;gain_keyframes?:GainKeyframe[]|null}):GainKeyframe[] {
 const k=effects.gain_keyframes;
 if(Array.isArray(k)&&k.length)return normalizeKeyframes(k);
 return [{t:0,db:effects.gain_db||0},{t:1,db:effects.gain_db||0}];
}

/** Linear interpolate clip-local gain at u ∈ [0,1]. */
export function interpolateGain(points:GainKeyframe[], u:number):number {
 const k=normalizeKeyframes(points); if(!k.length)return 0;
 const t=Math.min(1,Math.max(0,u));
 if(t<=k[0].t)return k[0].db; if(t>=k[k.length-1].t)return k[k.length-1].db;
 for(let i=0;i<k.length-1;i++){
  const a=k[i],b=k[i+1];
  if(t>=a.t&&t<=b.t){const d=b.t-a.t;return d<=1e-12?b.db:a.db+(b.db-a.db)*((t-a.t)/d);}
 }
 return k[k.length-1].db;
}

export function insertKeyframe(points:GainKeyframe[], t:number, db?:number):GainKeyframe[] {
 const k=normalizeKeyframes(points), u=Math.min(1,Math.max(0,t));
 const value=db??interpolateGain(k,u);
 return normalizeKeyframes([...k.filter(p=>Math.abs(p.t-u)>=1e-9),{t:u,db:value}]);
}
export function deleteKeyframe(points:GainKeyframe[], index:number):GainKeyframe[] {
 const k=normalizeKeyframes(points); if(index<0||index>=k.length)return k;
 if(k.length<=1)return k;
 return k.filter((_,i)=>i!==index);
}
/** Move keyframe; t clamped between neighbors (or 0..1 for ends). */
export function moveKeyframe(points:GainKeyframe[], index:number, t:number, db:number):GainKeyframe[] {
 const k=normalizeKeyframes(points); if(index<0||index>=k.length)return k;
 const lo=index===0?0:k[index-1].t, hi=index===k.length-1?1:k[index+1].t;
 const next=k.map((p,i)=>i===index?{t:Math.min(hi,Math.max(lo,t)),db}:p);
 return normalizeKeyframes(next);
}
/** Shift every keyframe (or the implied constant) by ΔdB. */
export function shiftGain(points:GainKeyframe[], delta:number):GainKeyframe[] {
 return normalizeKeyframes(points.map(p=>({t:p.t,db:p.db+delta})));
}

/** Per-pixel clip+track gain for a canvas covering clip-local [x0,x1) of width. */
export function pixelGains(effects:{gain_db?:number;gain_keyframes?:GainKeyframe[]|null}, trackGain:number, width:number, x0:number, x1:number, pixels:number):number[] {
 const k=effectiveKeyframes(effects), out=new Array(pixels);
 if(pixels<=0||!(x1>x0)||!(width>0))return out.fill?.(trackGain+(effects.gain_db||0))??[];
 for(let i=0;i<pixels;i++){
  const x=x0+(i+.5)*(x1-x0)/pixels;
  out[i]=interpolateGain(k,x/width)+trackGain;
 }
 return out;
}

export const formatGainDb=(db:number)=>`${db>=0?'+':''}${db.toFixed(1)} dB`;

/** Replace envelope with a single constant (inspector behavior). */
export function constantGainEffects<T extends {gain_db?:number;gain_keyframes?:GainKeyframe[]|null}>(effects:T, db:number):T {
 const {gain_keyframes:_, ...rest}=effects as T & {gain_keyframes?:unknown};
 return {...rest, gain_db:db, gain_keyframes:undefined} as T;
}

/** True when the rubber-band should show keyframe handles (stored envelope or live draft). */
export function gainBandShowsHandles(effects:{gain_keyframes?:GainKeyframe[]|null}, draft:GainKeyframe[]|null|undefined):boolean {
  return Boolean(draft||(Array.isArray(effects.gain_keyframes)&&effects.gain_keyframes.length));
}
