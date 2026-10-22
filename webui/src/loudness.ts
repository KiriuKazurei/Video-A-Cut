/** Timeline loudness envelope: decoding, zoom pyramid and per-pixel drawing.
 *  Pure logic is kept free of React/DOM so it is unit-testable. */
export interface LoudnessEnvelope { version:number; stream_index:number; channels?:number; duration:number; bucket_seconds:number; buckets:number; floor_db:number; clip_db:number; encoding:'u8-dbfs'; peak:string; rms:string; clipped_buckets:number }
export interface LoudnessLevel { bucketSeconds:number; peak:Uint8Array; rms:Uint8Array }
export interface LoudnessData { floorDb:number; clipDb:number; levels:LoudnessLevel[] }

export function decodeBase64(text:string):Uint8Array {
 const bin=atob(text),out=new Uint8Array(bin.length);
 for(let i=0;i<bin.length;i++)out[i]=bin.charCodeAt(i);
 return out;
}
const toLinear=(byte:number,floor:number)=>byte<=0?0:10**((floor+byte/255*-floor)/20);
const toByte=(linear:number,floor:number)=>linear<=0?0:Math.round(Math.min(1,Math.max(0,(20*Math.log10(linear)-floor)/-floor))*255);

/** Build a 2× pyramid once per asset: peak = max, RMS = power mean.
 *  Drawing then reads ~1 bucket per pixel regardless of zoom. */
export function buildLevels(env:LoudnessEnvelope):LoudnessData {
 let peak=decodeBase64(env.peak),rms=decodeBase64(env.rms),bucketSeconds=env.bucket_seconds;
 const levels:LoudnessLevel[]=[{bucketSeconds,peak,rms}],floor=env.floor_db;
 while(peak.length>64){
  const n=Math.ceil(peak.length/2),p=new Uint8Array(n),r=new Uint8Array(n);
  for(let i=0;i<n;i++){
   const a=2*i,b=Math.min(a+1,peak.length-1);
   p[i]=Math.max(peak[a],peak[b]);
   const la=toLinear(rms[a],floor),lb=toLinear(rms[b],floor);
   r[i]=toByte(Math.sqrt(b===a?la*la:(la*la+lb*lb)/2),floor);
  }
  peak=p;rms=r;bucketSeconds*=2;levels.push({bucketSeconds,peak,rms});
 }
 return {floorDb:floor,clipDb:env.clip_db,levels};
}

/** Coarsest level whose bucket is no wider than one device pixel. */
export function pickLevel(levels:LoudnessLevel[],secondsPerPixel:number):number {
 let best=0;
 for(let i=0;i<levels.length;i++)if(levels[i].bucketSeconds<=secondsPerPixel)best=i;
 return best;
}

export interface Column { peakDb:number; rmsDb:number; clipped:boolean }
/** Per-pixel columns for a source range [from,to) seconds. gainDb is a constant or one value per pixel. */
export function columns(data:LoudnessData,from:number,to:number,pixels:number,gainDb:number|number[]=0):Column[] {
 const out:Column[]=[];if(pixels<=0||!(to>from))return out;
 const spp=(to-from)/pixels,level=data.levels[pickLevel(data.levels,spp)],floor=data.floorDb;
 const gainAt=typeof gainDb==='number'?(x:number)=>gainDb:(x:number)=>gainDb[x]??0;
 for(let x=0;x<pixels;x++){
  const t0=from+x*spp,t1=t0+spp,g=gainAt(x);
  let i0=Math.floor(t0/level.bucketSeconds),i1=Math.max(i0+1,Math.ceil(t1/level.bucketSeconds));
  i0=Math.max(0,i0);i1=Math.min(level.peak.length,i1);
  let p=0,r=0;for(let i=i0;i<i1;i++){if(level.peak[i]>p)p=level.peak[i];if(level.rms[i]>r)r=level.rms[i];}
  const peakDb=p?floor+p/255*-floor+g:-Infinity,rmsDb=r?floor+r/255*-floor+g:-Infinity;
  out.push({peakDb,rmsDb,clipped:peakDb>=data.clipDb});
 }
 return out;
}
/** dB → 0..1 bar height on a log-ish display scale (floor → 0, 0 dBFS → 1). */
export const barHeight=(db:number,floor:number)=>!Number.isFinite(db)?0:Math.min(1,Math.max(0,(db-floor)/-floor));

export const LOUDNESS_COLORS={rms:'#E6E6E6',peak:'#9A9A9A',clip:'#DC4446',center:'#3A3A3A'};
/** Bottom-aligned envelope: bars rise from the bottom edge; clip ticks sit at the top. */
export function drawColumns(ctx:Pick<CanvasRenderingContext2D,'clearRect'|'fillRect'|'fillStyle'|'globalAlpha'>,cols:Column[],height:number,floor:number) {
 ctx.clearRect(0,0,cols.length,height);
 ctx.globalAlpha=.55;ctx.fillStyle=LOUDNESS_COLORS.peak;
 cols.forEach((c,x)=>{const h=barHeight(c.peakDb,floor)*height;if(h>0)ctx.fillRect(x,height-h,1,h);});
 ctx.globalAlpha=.85;ctx.fillStyle=LOUDNESS_COLORS.rms;
 cols.forEach((c,x)=>{const h=barHeight(c.rmsDb,floor)*height;if(h>0)ctx.fillRect(x,height-h,1,Math.max(1,h));});
 ctx.globalAlpha=1;ctx.fillStyle=LOUDNESS_COLORS.clip;
 cols.forEach((c,x)=>{if(c.clipped)ctx.fillRect(x,0,1,2);});
}

/** 409 means the same envelope is being computed (another tab/request);
 *  back off and retry several times instead of failing permanently. */
export const LOUDNESS_CONFLICT_RETRIES=8;
export function loudnessRetry(failureCount:number,error:unknown):boolean {
 const status=(error as {status?:number}|null)?.status;
 return status===409?failureCount<LOUDNESS_CONFLICT_RETRIES:failureCount<1;
}
export const loudnessRetryDelay=(attempt:number)=>Math.min(15000,500*2**attempt);
/** Errors are not terminal: a failed envelope is retried periodically while shown. */
export const LOUDNESS_ERROR_REFETCH_MS=30000;
export const loudnessRefetchInterval=(status:string)=>status==='error'?LOUDNESS_ERROR_REFETCH_MS:false;

/** Timeline header width; lanes start after it inside the scroller. */
export const TRACK_HEADING_PX=148,LOUDNESS_OVERSCAN_PX=256;
/** Clip-local [x0,x1) pixel span that is visible in the scroller (+overscan). */
export function visibleSpan(scrollLeft:number,clientWidth:number,left:number,width:number,overscan=LOUDNESS_OVERSCAN_PX):[number,number] {
 const x0=Math.min(width,Math.max(0,Math.floor(scrollLeft-left-overscan))),x1=Math.min(width,Math.ceil(scrollLeft+clientWidth-TRACK_HEADING_PX-left+overscan));
 return [x0,Math.max(x0,x1)];
}
/** Source seconds covered by a clip-local pixel span (respects trim in/out). */
export function sourceWindow(from:number,to:number,width:number,span:[number,number]):[number,number] {
 return [from+(to-from)*span[0]/width,from+(to-from)*span[1]/width];
}
export const totalGain=(clip:{effects:{gain_db?:number}},track:{gain_db?:number})=>(clip.effects.gain_db||0)+(track.gain_db||0);
/** Spectrum scaling ignores keyframe envelopes — only constant clip+track gain (UX: rubber-band stays independent). */
export const loudnessDisplayGain=(effects:{gain_db?:number;gain_keyframes?:unknown},trackGain=0)=>(effects.gain_db||0)+trackGain;

