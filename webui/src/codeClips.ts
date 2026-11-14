import {frameLabel,fpsValue,type TimelineClip,type TimelineSequence,type FrameRate} from './timelineContracts.ts';
import {scriptTokens} from './scriptWorkbench.ts';
import {rewriteDirectiveTime} from './scriptGrammar.ts';

export function selectedCodeClips(sequence:TimelineSequence,ids:string[]){
 const selected=new Set(ids);
 return sequence.tracks.flatMap((track,order)=>track.kind==='code'?track.clips.filter(c=>selected.has(c.id)).map(clip=>({clip,track,order})):[])
  .sort((a,b)=>a.clip.at-b.clip.at||a.order-b.order||a.clip.id.localeCompare(b.clip.id));
}
export function codeClipColors(sequence:TimelineSequence){
 const colors:Record<string,string>={};
 for(const track of sequence.tracks) if(track.kind==='code') for(const clip of track.clips) colors[clip.id]='var(--vac-clip-code)';
 return colors;
}

export function rebaseCodeDescription(value:string,delta:number,rate:FrameRate){
 return scriptTokens(value,true).map(token=>token.kind==='directive'
  ?rewriteDirectiveTime(token.text,delta,rate)
  :token.text).join('');
}
export function codeDescription(clip:TimelineClip,rate:FrameRate){
 if(clip.code?.script!==undefined)return rebaseCodeDescription(clip.code.script,clip.at-(clip.code.script_at??0),rate);
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
  const value=key.kind==='移动'?`x:${key.x??0}px, y:${key.y??0}px`:key.kind==='旋转'?`角度:${key.value??0}`:key.kind==='缩放'?`比例:${(key.value??1)*100}%`:`${key.start_value!==undefined?`从:${key.start_value*100}%, `:''}到:${(key.value??1)*100}%`;
  lines.push(`{${key.kind}, 时间:${time(key.frame)}, ${value}${key.duration?`, 持续:${duration(key.duration)}`:''}}`);
 }
 if((fx.scale??1)!==1&&!fx.parameter_keyframes?.some(k=>k.kind==='缩放'&&k.frame===0))lines.push(`{缩放, 时间:${time(0)}, 比例:${(fx.scale??1)*100}%}`);
 if(fx.opacity!==undefined&&!fx.parameter_keyframes?.some(k=>k.kind==='透明度'&&k.frame===0))lines.push(`{透明度, 时间:${time(0)}, 到:${fx.opacity*100}%}`);
 for(const cue of fx.subtitles??[])lines.push(`{字幕, 文本:${JSON.stringify(cue.text)}, 时间:${time(cue.frame)}, 持续:${duration(cue.duration)}${cue.size_px?`, 字号:${cue.size_px}px`:''}${cue.font?`, 字体:${JSON.stringify(cue.font)}`:''}}`);
 return lines.filter(Boolean).join('\n');
}
