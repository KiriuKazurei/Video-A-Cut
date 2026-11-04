import {frameLabel,fpsValue,parseTimecode,type TimelineClip,type TimelineSequence,type FrameRate} from './timelineContracts.ts';
import {scriptTokens} from './scriptWorkbench.ts';

export function selectedCodeClips(sequence:TimelineSequence,ids:string[]){
 const selected=new Set(ids);
 return sequence.tracks.flatMap((track,order)=>track.kind==='code'?track.clips.filter(c=>selected.has(c.id)).map(clip=>({clip,track,order})):[])
  .sort((a,b)=>a.clip.at-b.clip.at||a.order-b.order||a.clip.id.localeCompare(b.clip.id));
}
export function codeClipColors(sequence:TimelineSequence){
 const palette=['--vac-accent','--vac-status-cyan','--vac-warning-text','--vac-success-text','--vac-clip-effect','--vac-danger-text'];
 const colors:Record<string,string>={};
 const ids=sequence.tracks.filter(t=>t.kind==='code').flatMap(t=>t.clips.map(c=>c.id)).sort();
 for(const id of ids){let hash=2166136261;for(const c of id)hash=Math.imul(hash^c.charCodeAt(0),16777619)>>>0;
  const base=`var(${palette[hash%palette.length]})`,weight=60+(hash>>>8)%4000/100;
  colors[id]=`color-mix(in srgb, ${base} ${weight}%, var(--vac-text))`;
 }
 return colors;
}

export function rebaseCodeDescription(value:string,delta:number,rate:FrameRate){
 return scriptTokens(value,true).map(token=>token.kind==='directive'&&/^\{\s*(音量|移动|旋转)\s*[,，]/.test(token.text)
  ?token.text.replace(/(时间\s*:\s*)(\d+:\d+:\d+(?::\d+)?)/,(_,prefix,time)=>{const frame=parseTimecode(time,rate);return frame===null?prefix+time:prefix+frameLabel(frame+delta,rate);})
  :token.text).join('');
}
export function codeDescription(clip:TimelineClip,rate:FrameRate){
 const lines=[clip.code?.description??''],fx=clip.effects;
 const time=(frame:number)=>frameLabel(clip.at+frame,rate);
 const duration=(frames:number)=>`${Number((frames/fpsValue(rate)).toFixed(9))}s`;
 const keys=fx.gain_keyframes??[];
 if(keys.length>1){for(let i=1;i<keys.length;i++){
  const a=keys[i-1],b=keys[i],start=Math.round(a.t*clip.duration),end=Math.round(b.t*clip.duration);
  if(end>start)lines.push(`{音量, 时间:${time(start)}, 起始增益:${a.db}dB, 增益:${b.db}dB, 持续:${duration(end-start)}}`);
 }}else lines.push(`{音量, 时间:${time(0)}, 增益:${keys[0]?.db??fx.gain_db??0}dB}`);
 if((fx.x||fx.y)&&!fx.parameter_keyframes?.some(k=>k.kind==='移动'&&k.frame===0))lines.push(`{移动, 时间:${time(0)}, x:${fx.x||0}px, y:${fx.y||0}px}`);
 if(fx.rotation&&!fx.parameter_keyframes?.some(k=>k.kind==='旋转'&&k.frame===0))lines.push(`{旋转, 时间:${time(0)}, 角度:${fx.rotation}}`);
 for(const key of fx.parameter_keyframes??[]){
  const value=key.kind==='移动'?`x:${key.x??0}px, y:${key.y??0}px`:`角度:${key.value??0}`;
  lines.push(`{${key.kind}, 时间:${time(key.frame)}, ${value}${key.duration?`, 持续:${duration(key.duration)}`:''}}`);
 }
 return lines.filter(Boolean).join('\n');
}
