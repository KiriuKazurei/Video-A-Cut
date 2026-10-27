import { createHash } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { mkdir, readFile, rename, stat, writeFile } from 'node:fs/promises';
import { isAbsolute, join } from 'node:path';
import { runTool, probeMedia } from './media.mjs';
import { installedFonts, resolveFonts } from './fonts.mjs';
import { assText, assTime } from '../adapters/ass.mjs';

const seconds = time => time.value / time.scale;
const n = value => { if (!Number.isFinite(value)) throw Error('无效时间或效果参数'); return String(Number(value.toFixed(9))); };

/** Build an ffmpeg volume expression for clip-local keyframes (t in seconds of the trimmed clip).
 *  Returns null when keyframes are absent so callers keep the constant gain_db path. */
export function gainEnvelopeExpression(effects, durationSeconds) {
  const keys=[...(effects?.gain_keyframes||[])].filter(k=>Number.isFinite(k?.t)&&Number.isFinite(k?.db))
    .map(k=>({t:Math.min(1,Math.max(0,k.t)),db:k.db})).sort((a,b)=>a.t-b.t);
  if(!keys.length||!(durationSeconds>0)) return null;
  // Piecewise linear dB envelope in clip-local seconds (after asetpts).
  const esc=v=>n(v);
  let expr=esc(keys[keys.length-1].db);
  for(let i=keys.length-2;i>=0;i--){
    const a=keys[i],b=keys[i+1], ta=a.t*durationSeconds, tb=b.t*durationSeconds, d=Math.max(1e-12,tb-ta);
    const seg=`${esc(a.db)}+(${esc(b.db)}-(${esc(a.db)}))*(t-${esc(ta)})/${esc(d)}`;
    expr=`if(lt(t\\,${esc(ta)})\\,${esc(a.db)}\\,if(lt(t\\,${esc(tb)})\\,${seg}\\,${expr}))`;
  }
  return expr;
}

export function audioGainFilters(track, effects, durationSeconds, from, to) {
  const trackGain=`volume=${n(track.gain_db||0)}dB`;
  const env=gainEnvelopeExpression(effects, durationSeconds);
  if(!env) return `${trackGain},volume=${n(effects.gain_db||0)}dB:enable='gte(t,${n(from)})*lt(t,${n(to)})'`;
  // Expression returns dB; convert to linear multiplier inside the effect scope.
  return `${trackGain},volume='if(gte(t\\,${n(from)})*lt(t\\,${n(to)})\\,pow(10\\,(${env})/20)\\,1)':eval=frame`;
}

const filterPath = path => path.replaceAll('\\', '/').replaceAll(':', '\\:').replaceAll("'", "'\\''");
export async function sequenceFileHash(path) { const hash = createHash('sha256'); for await (const bytes of createReadStream(path)) hash.update(bytes); return hash.digest('hex'); }
export function selectedTracks(sequence) {
 const group = sequence.scope_group;
 const tracks = sequence.tracks.filter(t => !group || (group === 'original' ? !t.group_id : t.group_id === group));
 const solo = new Set(tracks.filter(t => t.solo).map(t => t.kind));
 return tracks.filter(t => !t.hidden && !(t.kind === 'audio' && t.muted) && (!solo.has(t.kind) || t.solo));
}
const tempo = speed => { const filters = []; while (speed > 2) { filters.push('atempo=2'); speed /= 2; } while (speed < .5) { filters.push('atempo=0.5'); speed *= 2; } filters.push(`atempo=${n(speed)}`); return filters.join(','); };

// The same immutable graph is used for the program monitor and final export.
// Inputs contain only current-project resources resolved by Go.
export function sequenceGraph(sequence, resources) {
 const fps = sequence.fps.num / sequence.fps.den, rate = `${sequence.fps.num}/${sequence.fps.den}`;
 if (!(fps > 0 && fps <= 240) || !(sequence.width > 0 && sequence.height > 0)) throw Error('序列参数无效');
 const tracks = selectedTracks(sequence), clips = tracks.flatMap(t => t.clips);
 const frames = Math.max(0, ...clips.map(c => c.at + c.duration));
 if (frames < 1) throw Error('当前轨道范围没有可渲染内容');
 const duration = frames / fps, args = [], filters = [], cues = [], audio = [];
 let canvas = 'base', inputIndex = 0, layer = 0;
 filters.push(`color=c=black:s=${sequence.width}x${sequence.height}:r=${rate}:d=${n(duration)},format=rgba[base]`);
 for (const track of tracks) {
  const ordered = [...track.clips].sort((a,b) => a.at - b.at);
  for (let ci = 0; ci < ordered.length; ci++) {
   const clip = ordered[ci], resource = resources.find(r => r.id === clip.resource_id);
   if (!resource || !isAbsolute(resource.path) || /^[a-z]+:\/\//i.test(resource.path)) throw Error('片段资源缺失或不属于当前工程');
   if (!(clip.duration > 0 && clip.speed > 0) || clip.at < 0) throw Error('片段时间无效');
   const e = clip.effects, len = clip.duration / fps, at = clip.at / fps;
   if (track.kind === 'subtitle') { cues.push({start: at,end: at + len,text: clip.text,font: clip.font,size: clip.font_size || 36}); continue; }
   const available = resource.metadata?.streams ?? [];
   if (resource.kind !== 'image' && !available.some(s => s.index === clip.stream_index && s.codec_type === track.kind)) throw Error(`源流 ${clip.stream_index} 不属于 ${track.kind}`);
   const next = ordered[ci + 1], transition = e.transition / fps;
   if (transition && (!ci || ordered[ci - 1].at + ordered[ci - 1].duration !== clip.at)) throw Error('交叉溶解需要前一个相邻片段');
   const tail = next && next.at === clip.at + clip.duration ? (next.effects.transition || 0) / fps : 0;
   if (tail && resource.kind !== 'image' && seconds(clip.source_out) + tail * clip.speed > Number(resource.metadata.duration) + 1e-6) throw Error('交叉溶解的前片段没有足够的源尾部，请缩短源出点');
   if (resource.draw_document) args.push('-framerate',`${resource.draw_document.canvas.fps.num}/${resource.draw_document.canvas.fps.den}`,'-start_number','0');
   else if (resource.kind === 'image') args.push('-loop','1','-framerate',rate);
   args.push('-i',resource.path); const source = `${inputIndex++}:${clip.stream_index}`;
   if (track.kind === 'audio') {
    const label = `audio${audio.length}`;
    let chain = `[${source}]atrim=start=${n(seconds(clip.source_in))}:end=${n(seconds(clip.source_out) + tail * clip.speed)},asetpts=PTS-STARTPTS,aresample=${sequence.sample_rate},${tempo(clip.speed)},atrim=duration=${n(len+tail)}`;
    const from = (e.from_frame ?? 0) / fps, to = (e.to_frame ?? clip.duration) / fps;
    chain += ','+audioGainFilters(track, e, len, from, to);
    if (e.fade_in) chain += `,afade=t=in:st=${n(from)}:d=${n(Math.min(e.fade_in / fps,to-from))}:enable='gte(t,${n(from)})*lt(t,${n(to)})'`;
    if (e.fade_out) chain += `,afade=t=out:st=${n(Math.max(from,to-e.fade_out/fps))}:d=${n(Math.min(e.fade_out/fps,to-from))}:enable='gte(t,${n(from)})*lt(t,${n(to)})'`;
    if (transition) chain += `,afade=t=in:d=${n(transition)}`;
    if (tail) chain += `,afade=t=out:st=${n(len)}:d=${n(tail)}`;
    chain += `,adelay=${Math.round(at*sequence.sample_rate)}S:all=1[${label}]`; filters.push(chain);audio.push(label);continue;
   }
   const from = e.from_frame ?? 0, to = e.to_frame ?? clip.duration;
   const parts = [[0,from,false],[from,to,true],[to,clip.duration + Math.round(tail*fps),false]].filter(([a,b]) => b > a);
   for (const [start,end,affected] of parts) {
    const fx = affected ? e : {scale:1,x:0,y:0,crop_left:0,crop_right:0,crop_top:0,crop_bottom:0};
    const name = `video${layer}`, output = `composite${layer++}`;
    const left = fx.crop_left || 0, right = fx.crop_right || 0, top = fx.crop_top || 0, bottom = fx.crop_bottom || 0;
    const width = Math.max(2,Math.round(sequence.width * (fx.scale || 1) / 2) * 2), height = Math.max(2,Math.round(sequence.height * (fx.scale || 1) / 2) * 2);
    if (width > 16384 || height > 16384 || left+right >= 1 || top+bottom >= 1) throw Error('缩放或裁切参数超出范围');
    let chain = `[${source}]trim=start=${n(seconds(clip.source_in) + start/fps*clip.speed)}:end=${n(seconds(clip.source_in) + end/fps*clip.speed)},setpts=(PTS-STARTPTS)/${n(clip.speed)},fps=${rate},crop=w='iw*${n(1-left-right)}':h='ih*${n(1-top-bottom)}':x='iw*${n(left)}':y='ih*${n(top)}',scale=w=${width}:h=${height}:force_original_aspect_ratio=decrease,setsar=1,format=rgba`;
    if (affected && e.fade_in) chain += `,fade=t=in:st=0:d=${n(Math.min(e.fade_in,end-start)/fps)}:color=${e.fade_color || 'black'}`;
    if (affected && e.fade_out) chain += `,fade=t=out:st=${n(Math.max(0,(end-start-e.fade_out)/fps))}:d=${n(Math.min(e.fade_out,end-start)/fps)}:color=${e.fade_color || 'black'}`;
    if (transition && start === 0) chain += `,fade=t=in:st=0:d=${n(transition)}:alpha=1`;
    chain += `,setpts=PTS+${n(at+start/fps)}/TB[${name}]`;
    filters.push(chain,`[${canvas}][${name}]overlay=x='(W-w)/2+${n(fx.x || 0)}':y='(H-h)/2+${n(fx.y || 0)}':eof_action=pass:enable='gte(t,${n(at+start/fps)})*lt(t,${n(at+end/fps)})'[${output}]`);canvas=output;
   }
  }
 }
 filters.push(`anullsrc=r=${sequence.sample_rate}:cl=stereo,atrim=duration=${n(duration)}[silence]`);
 filters.push(`[silence]${audio.map(a=>`[${a}]`).join('')}amix=inputs=${audio.length+1}:normalize=0:duration=first,alimiter=limit=0.95:level=0,atrim=duration=${n(duration)}[mix]`);
 return {args,filters,canvas,cues,frames,duration,rate,tracks};
}

async function subtitleFile(output, cues, sequence) {
 if (!cues.length) return undefined;
 const catalogue = await installedFonts(), fonts = catalogue.map(f=>f.family), defaultFont = ['Microsoft YaHei','Noto Sans CJK SC','Arial'].find(f=>fonts.includes(f)) || fonts[0];
 if (!defaultFont) throw Error('没有可用的本地字幕字体');
 let ass = `[Script Info]\nScriptType: v4.00+\nPlayResX: ${sequence.width}\nPlayResY: ${sequence.height}\n\n[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n`;
 const records=[];
 for (const [i,c] of cues.entries()) {const font=c.font || defaultFont;if (!fonts.includes(font)) throw Error(`字体不可用：${font}`);records.push(...await resolveFonts([font]));ass += `Style: P${i},${font},${c.size},&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2,0,2,40,40,36,1\n`;}
 ass+='\n[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\n';
 cues.forEach((c,i)=>{ass+=`Dialogue: ${i},${assTime(Math.round(c.start*100))},${assTime(Math.round(c.end*100))},P${i},,0,0,0,,${assText(c.text)}\n`;});
 const path=join(output,'subtitles.ass');await writeFile(path,ass,'utf8');return {path,fonts:records.map(({family,sha256})=>({family,sha256}))};
}

export async function renderSequence(input,{run=runTool,probe=probeMedia}={}) {
 const {sequence,output_dir:output,content_version:version}=input;
 const resources=[];
 for(const r of input.resources){
  if(r.draw_document){const {prepareDrawFrameSource}=await import('./unified-draw.mjs');const directory=join(output,'draw-sources',createHash('sha256').update(r.id+':'+r.sha256).digest('hex'));const path=await prepareDrawFrameSource({document:r.draw_document,assets:r.assets,output_dir:directory});resources.push({...r,path});}
  else resources.push(r);
 }
 const graph=sequenceGraph(sequence,resources);await mkdir(output,{recursive:true});
 for (const r of resources) {if (!r.draw_document && await sequenceFileHash(r.path)!==r.sha256) throw Error(`源素材发生变化：${r.name}`);}
 const subtitles=await subtitleFile(output,graph.cues,sequence);
 const filters=[...graph.filters];let video=graph.canvas;
 if (subtitles){filters.push(`[${video}]ass=filename='${filterPath(subtitles.path)}'[subtitled]`);video='subtitled';}
 const temporary=join(output,'rendering.mp4'),final=join(output,'program.mp4');
 await run('ffmpeg',['-hide_banner','-loglevel','error','-nostdin','-y',...graph.args,'-filter_complex_threads','1','-filter_complex',filters.join(';'),'-map',`[${video}]`,'-map','[mix]','-frames:v',String(graph.frames),'-t',n(graph.duration),'-c:v','libx264','-preset','veryfast','-crf','20','-threads','2','-pix_fmt','yuv420p','-c:a','aac','-ar',String(sequence.sample_rate),'-movflags','+faststart',temporary],{timeoutMs:360_000});
 const media=await probe(temporary);if (!media.video || !media.audio || Math.abs(media.duration-graph.duration)>2/(sequence.fps.num/sequence.fps.den)) throw Error('节目输出验证失败');
 const sha256=await sequenceFileHash(temporary);
 for (const r of resources) {if (!r.draw_document && await sequenceFileHash(r.path)!==r.sha256) throw Error(`渲染期间源素材发生变化：${r.name}`);}
 await rename(temporary,final);
 const report={schema_version:2,content_version:version,sha256,frames:graph.frames,duration:graph.duration,width:sequence.width,height:sequence.height,fps:sequence.fps,scope_group:sequence.scope_group || '',fonts:subtitles?.fonts || []};
 await writeFile(join(output,'sequence.json'),JSON.stringify({schema_version:2,sequence,resources:resources.map(({path,assets,...r})=>({...r,...(assets?{assets:assets.map(({path,...asset})=>asset)}:{})}))},null,2));
 await writeFile(join(output,'program.json'),JSON.stringify(report,null,2));return report;
}

export async function waveform(input,{run=runTool}={}) {
 const info=await stat(input.path);if (!info.isFile() || !(input.duration>0) || input.duration>24*3600) throw Error('波形源无效');
 // astats emits one peak per bounded time bucket instead of copying PCM hours
 // into the process. This is a display cache, not a transcription request.
 const bins=1000, samples=Math.max(1,Math.ceil(input.duration*8000/bins));
 const output=await run('ffmpeg',['-hide_banner','-loglevel','info','-nostdin','-i',input.path,'-map',`0:${input.stream_index}`,'-vn','-af',`aresample=8000,aformat=sample_fmts=flt:channel_layouts=mono,asetnsamples=n=${samples}:p=1,astats=metadata=1:reset=1,ametadata=print:key=lavfi.astats.Overall.Peak_level:file=-`,'-f','null','-'],{timeoutMs:60_000,maxOutputBytes:1024*1024});
 const peaks=[...output.matchAll(/lavfi\.astats\.Overall\.Peak_level=(-?inf|[-+\d.]+)/g)].map(m=>m[1]==='-inf'?0:Math.min(1,10**(Number(m[1])/20)));
 return {peaks,duration:input.duration,stream_index:input.stream_index};
}
