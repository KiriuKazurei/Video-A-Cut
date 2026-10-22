/** Timeline loudness envelope: decoding, zoom pyramid and per-pixel drawing.
 *  Pure logic is kept free of React/DOM so it is unit-testable. */
export interface LoudnessEnvelope { version:number; stream_index:number; duration:number; bucket_seconds:number; buckets:number; floor_db:number; clip_db:number; encoding:'u8-dbfs'; peak:string; rms:string; clipped_buckets:number }
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
/** Per-pixel columns for a source range [from,to) seconds, with gain in dB. */
export function columns(data:LoudnessData,from:number,to:number,pixels:number,gainDb=0):Column[] {
 const out:Column[]=[];if(pixels<=0||!(to>from))return out;
 const spp=(to-from)/pixels,level=data.levels[pickLevel(data.levels,spp)],floor=data.floorDb;
 for(let x=0;x<pixels;x++){
  const t0=from+x*spp,t1=t0+spp;
  let i0=Math.floor(t0/level.bucketSeconds),i1=Math.max(i0+1,Math.ceil(t1/level.bucketSeconds));
  i0=Math.max(0,i0);i1=Math.min(level.peak.length,i1);
  let p=0,r=0;for(let i=i0;i<i1;i++){if(level.peak[i]>p)p=level.peak[i];if(level.rms[i]>r)r=level.rms[i];}
  const peakDb=p?floor+p/255*-floor+gainDb:-Infinity,rmsDb=r?floor+r/255*-floor+gainDb:-Infinity;
  out.push({peakDb,rmsDb,clipped:peakDb>=data.clipDb});
 }
 return out;
}
/** dB → 0..1 bar height on a log-ish display scale (floor → 0, 0 dBFS → 1). */
export const barHeight=(db:number,floor:number)=>!Number.isFinite(db)?0:Math.min(1,Math.max(0,(db-floor)/-floor));

export const LOUDNESS_COLORS={rms:'#E6E6E6',peak:'#9A9A9A',clip:'#DC4446',center:'#3A3A3A'};
/** Mirrored envelope: peak behind, RMS in front, clipped columns tinted at the top. */
export function drawColumns(ctx:Pick<CanvasRenderingContext2D,'clearRect'|'fillRect'|'fillStyle'|'globalAlpha'>,cols:Column[],height:number,floor:number) {
 ctx.clearRect(0,0,cols.length,height);
 const mid=height/2;
 ctx.globalAlpha=.55;ctx.fillStyle=LOUDNESS_COLORS.peak;
 cols.forEach((c,x)=>{const h=barHeight(c.peakDb,floor)*mid;if(h>0)ctx.fillRect(x,mid-h,1,h*2);});
 ctx.globalAlpha=.85;ctx.fillStyle=LOUDNESS_COLORS.rms;
 cols.forEach((c,x)=>{const h=barHeight(c.rmsDb,floor)*mid;if(h>0)ctx.fillRect(x,mid-h,1,Math.max(1,h*2));});
 ctx.globalAlpha=1;ctx.fillStyle=LOUDNESS_COLORS.clip;
 cols.forEach((c,x)=>{if(c.clipped){ctx.fillRect(x,0,1,2);ctx.fillRect(x,height-2,1,2);}});
}
