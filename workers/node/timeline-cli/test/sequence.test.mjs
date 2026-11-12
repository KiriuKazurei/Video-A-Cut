import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { sequenceGraph, selectedTracks, renderSequence, renderSequenceFrame, sequenceFileHash, waveform } from '../src/sequence.mjs';
import { probeMedia, runTool } from '../src/media.mjs';
import { inspectBasicQuality, sequenceExpectations } from '../src/basic-quality.mjs';

const effects = () => ({scale:1,x:0,y:0,crop_left:0,crop_right:0,crop_top:0,crop_bottom:0,fade_in:0,fade_out:0,transition:0,gain_db:0});
const clip = (id,stream=0) => ({id,resource_id:'r',name:id,stream_index:stream,at:0,duration:24,source_in:{value:0,scale:48000},source_out:{value:38400,scale:48000},speed:1,effects:effects()});
const seq = () => ({id:'s',width:320,height:180,fps:{num:30,den:1},sample_rate:48000,tracks:[],markers:[],groups:[]});

test('code tracks compose pixels and parameter animation using video streams',()=>{
 const s=seq();s.tracks=[{id:'code',kind:'code',clips:[{...clip('c'),effects:{...effects(),parameter_keyframes:[{kind:'移动',frame:0,duration:12,x:100,y:0},{kind:'旋转',frame:0,duration:12,value:90}]}}]}];
 const resources=[{id:'r',kind:'video',path:join(process.cwd(),'drawing.png'),metadata:{duration:2,streams:[{index:0,codec_type:'video'}]}}];
 const graph=sequenceGraph(s,resources,{frame:6});
 assert.equal(graph.frameLayers[0].x,50);assert.equal(graph.frameLayers[0].rotation,45);
 assert.match(graph.filters.join(';'),/rotate=/);
});

test('code scale opacity and timed captions share the still/movie graph',()=>{
 const s=seq();s.tracks=[{id:'code',kind:'code',clips:[{...clip('c'),effects:{...effects(),parameter_keyframes:[{kind:'缩放',frame:0,duration:12,value:.5},{kind:'透明度',frame:0,duration:12,start_value:0,value:.5}],subtitles:[{text:'定时字幕',frame:3,duration:9,size_px:24}]}}]}];
 const resources=[{id:'r',kind:'video',path:join(process.cwd(),'drawing.png'),metadata:{duration:2,streams:[{index:0,codec_type:'video'}]}}];
 const graph=sequenceGraph(s,resources,{frame:6});
 assert.equal(graph.frameLayers[0].width,240);assert.equal(graph.frameLayers[0].opacity,.25);
 assert.equal(graph.cues[0].text,'定时字幕');assert.equal(graph.cues[0].start,.1);assert.equal(graph.cues[0].end,.4);
 assert.equal(sequenceGraph(s,resources,{frame:13}).cues.length,0,'caption cache must not retain a cue after its end');
 assert.match(sequenceGraph(s,resources).filters.join(';'),/geq=/);
});

test('code audio follows its envelope independently of hidden pixels and mute',()=>{
 const s=seq();s.tracks=[{id:'code',kind:'code',hidden:true,gain_db:-2,clips:[{...clip('c'),effects:{...effects(),gain_db:-6}}]}];
 const resources=[{id:'r',kind:'video',path:join(process.cwd(),'drawing.png'),draw_document:{canvas:{fps:{num:30,den:1}},audio_bindings:[{resource_id:'sound',at_frame:0,duration_frames:24,source_in:0,stream_index:2,gain_db:-3}]},metadata:{streams:[{index:0,codec_type:'video'}]}},{id:'sound',kind:'audio',path:join(process.cwd(),'sound.wav'),metadata:{duration:2,streams:[{index:2,codec_type:'audio'}]}}];
 const graph=sequenceGraph(s,resources);
 assert.match(graph.filters.join(';'),/volume=-5dB,volume=-6dB/);assert.equal(graph.frameLayers.length,0);
 s.tracks[0].muted=true;
 assert.throws(()=>sequenceGraph(s,resources),/没有可渲染内容/);
});
let fixturePromise;
function realFixture() {
 return fixturePromise ??= (async()=>{
  assert.ok(process.env.VAC_TEST_OUTPUT);const root=join(process.env.VAC_TEST_OUTPUT,'sequence');await mkdir(root,{recursive:true});
  const path=join(root,'multi.mp4'),blue=join(root,'blue.mp4');
  await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=red:s=320x180:r=30:d=2','-f','lavfi','-i','anullsrc=r=48000:cl=stereo:d=2','-f','lavfi','-i','sine=frequency=880:sample_rate=48000:duration=2','-map','0:v','-map','1:a','-map','2:a','-c:v','libx264','-threads','1','-c:a','aac',path]);
  await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=blue:s=320x180:r=30:d=2','-c:v','libx264','-threads','1',blue]);
  return {root,path,resources:[{id:'r',kind:'video',name:'multi',path,sha256:await sequenceFileHash(path),metadata:{duration:2,streams:[{index:0,codec_type:'video'},{index:1,codec_type:'audio'},{index:2,codec_type:'audio'}]}},{id:'blue',kind:'video',name:'blue',path:blue,sha256:await sequenceFileHash(blue),metadata:{duration:2,streams:[{index:0,codec_type:'video'}]}}]};
 })();
}
async function audioStat(path,key,at=0,duration=.7) {
 const text=await runTool('ffmpeg',['-v','error','-ss',String(at),'-i',path,'-t',String(duration),'-af',`astats=metadata=1:reset=0,ametadata=print:key=${key}:file=-`,'-vn','-f','null','-']);
 return Number([...text.matchAll(new RegExp(key.replaceAll('.','\\.')+'=(-?inf|[-+\\d.]+)','g'))].at(-1)?.[1]);
}
async function pixelStat(path,at,x=150,y=80) {
 const text=await runTool('ffmpeg',['-v','error','-ss',String(at),'-i',path,'-frames:v','1','-vf',`crop=16:16:${x}:${y},signalstats,metadata=print:file=-`,'-f','null','-']);
 return Object.fromEntries(['YAVG','UAVG','VAVG'].map(key=>[key,Number(text.match(new RegExp(key+'=([\\d.]+)'))?.[1])]));
}

test('all code engines share still/movie transforms and declared source audio',{timeout:180000},async()=>{
 const fixture=await realFixture();
 const sources={
  hyperframes:'<!doctype html><html><head><style>body{margin:0}#root{position:relative;width:320px;height:180px}#box{position:absolute;left:140px;top:70px;width:40px;height:40px;background:#ff8800}</style></head><body><div id="root" data-composition-id="root" data-width="320" data-height="180" data-duration="0.4"><div id="box"></div></div><script>const tl=gsap.timeline({paused:true});tl.to("#box",{x:0,duration:0.4});window.__timelines={root:tl};</script></body></html>',
  remotion:'import React from "react";export default ()=> <div style={{position:"absolute",left:140,top:70,width:40,height:40,background:"#ff8800"}}/>;',
  motion_canvas:'import {makeScene2D,Rect} from "@motion-canvas/2d";import {waitFor} from "@motion-canvas/core";export default makeScene2D(function* (view){view.add(<Rect width={40} height={40} fill="#ff8800"/>);yield* waitFor(0.4);});'
 };
 for(const [engine,source] of Object.entries(sources)){
  const document={schema_version:1,canvas:{width:320,height:180,fps:{num:30,den:1}},duration_frames:12,sources:[{id:'source',engine,source}],nodes:[{id:'scene',kind:'source',source_id:'source',from:0,duration:12,transform:{x:0,y:0}}],audio_bindings:[{resource_id:'r',at_frame:0,duration_frames:12,source_in:0,stream_index:2,gain_db:0}]};
  const sequence=seq(),visual={...clip('picture'),resource_id:'picture',duration:12,source_out:{value:400000000,scale:1e9},effects:{...effects(),gain_db:-12,parameter_keyframes:[{kind:'移动',frame:0,duration:12,x:80,y:0},{kind:'旋转',frame:0,duration:12,value:90},{kind:'缩放',frame:0,duration:12,value:.75},{kind:'透明度',frame:0,duration:12,start_value:0,value:1}],subtitles:[{text:'定时字幕',frame:0,duration:3,size_px:18}]}};
  sequence.tracks=[{id:'base',kind:'video',clips:[{...clip('base'),duration:12,source_out:visual.source_out}]},{id:'code',kind:'code',clips:[visual]}];
  const resources=[...fixture.resources,{id:'picture',kind:'video',name:engine,path:join(fixture.root,engine+'.source'),sha256:engine,draw_document:document,metadata:{duration:.4,streams:[{index:0,codec_type:'video'}]}}];
  const output=join(fixture.root,'code-'+engine);
  await renderSequence({sequence,resources,output_dir:output,content_version:'code-'+engine});
  await renderSequenceFrame({sequence,resources,frame:6,output_dir:join(output,'still'),content_version:'code-'+engine});
  const movie=join(output,'program.mp4'),a=await pixelStat(movie,.2,195,80),b=await pixelStat(join(output,'still','frame.png'),0,195,80);
  for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(a[key]-b[key])<8,`${engine} ${key} still/movie mismatch: ${a[key]} ${b[key]}`);
  assert.match(await readFile(join(output,'subtitles.ass'),'utf8'),/Dialogue:.*0:00:00.00,0:00:00.10.*定时字幕/);
  const after=await audioStat(movie,'lavfi.astats.Overall.RMS_level',.1,.15);
  // An audible sine at about -21 dBFS with the code clip at -12 dB must remain audible near -33 dBFS.
  assert(Number.isFinite(after)&&after< -28&&after> -39,`${engine} code audio missing or ignored gain: ${after}`);
 }
});
test('composition scopes mute/solo and keeps actual stream indices and tempo',()=>{
 const s=seq();s.tracks=[{id:'v',kind:'video',clips:[clip('v')]},{id:'a',kind:'audio',muted:true,clips:[clip('a',1)]},{id:'b',kind:'audio',clips:[{...clip('b',4),speed:2}]}];
 const graph=sequenceGraph(s,[{id:'r',kind:'video',path:join(process.cwd(),'example.mp4'),metadata:{duration:4,streams:[{index:0,codec_type:'video'},{index:1,codec_type:'audio'},{index:4,codec_type:'audio'}]}}]);
 assert.match(graph.filters.join(';'),/\[1:4\]atrim/);assert.match(graph.filters.join(';'),/atempo=2/);assert.equal(graph.args.filter(v=>v==='-i').length,2);
 s.tracks.push({id:'agent',kind:'video',group_id:'g',clips:[clip('agent')]});s.scope_group='g';assert.deepEqual(selectedTracks(s).map(t=>t.id),['agent']);
 s.scope_group='original';assert.deepEqual(selectedTracks(s).map(t=>t.id),['v','b']);
 s.tracks[2].solo=true;assert.equal(selectedTracks(s).filter(t=>t.kind==='audio').length,1);
});
test('real multitrack program maps second audio stream, overlays, waveforms and pitch-preserving speed',{timeout:120000},async()=>{
 const {root,path,resources}=await realFixture();
 const s=seq();s.tracks=[{id:'v1',kind:'video',clips:[clip('v')]},{id:'a2',kind:'audio',clips:[{...clip('a',2),speed:2,source_out:{value:76800,scale:48000}}]}];
 const output=join(root,'program');const report=await renderSequence({sequence:s,resources,output_dir:output,content_version:'verified'});assert.equal(report.frames,24);assert.equal(report.content_version,'verified');assert.equal(report.sha256,await sequenceFileHash(join(output,'program.mp4')));
 const stats=await runTool('ffmpeg',['-v','error','-i',join(output,'program.mp4'),'-af','astats=metadata=1:reset=0,ametadata=print:key=lavfi.astats.Overall.RMS_level:file=-','-vn','-f','null','-']);const levels=[...stats.matchAll(/RMS_level=(-?[\d.]+)/g)].map(m=>Number(m[1]));assert.ok(levels.at(-1)>-32,'actual second stream is audible, first stream is silent');
 const pixels=await runTool('ffmpeg',['-v','error','-i',join(output,'program.mp4'),'-frames:v','1','-vf','crop=16:16:0:0,signalstats,metadata=print:file=-','-f','null','-']);assert.match(pixels,/VAVG=2[234]\d/,'red source survives composition');
 const peaks=await waveform({path,stream_index:2,duration:2});assert.ok(peaks.peaks.length>100);assert.ok(peaks.peaks.some(p=>p>.05));
 const saved=JSON.parse(await readFile(join(output,'sequence.json'),'utf8'));assert.equal(saved.sequence.tracks[1].clips[0].stream_index,2);
 const crossing=await audioStat(join(output,'program.mp4'),'lavfi.astats.1.Zero_crossings_rate',.1,.5),frequency=crossing*48000/2;assert.ok(Math.abs(frequency-880)<35,`speed changed pitch to ${frequency}`);
 await writeFile(join(root,'verification.json'),JSON.stringify({report,rms:levels.at(-1),waveform_bins:peaks.peaks.length,measured_frequency:frequency},null,2));
});
test('real effects preserve scoped audio, composite transforms, transition colors and subtitle cues',{timeout:120000},async()=>{
 const {root,resources}=await realFixture();const s=seq();
 const full=(id,resource='r')=>({...clip(id),resource_id:resource,duration:60,source_out:{value:96000,scale:48000}});
 s.tracks=[{id:'base',kind:'video',clips:[full('red')]},{id:'overlay',kind:'video',clips:[{...full('overlay','blue'),effects:{...effects(),scale:.5,x:20,crop_left:.1}}]},{id:'tone',kind:'audio',gain_db:-6,clips:[{...full('tone'),stream_index:2,effects:{...effects(),gain_db:-6,fade_in:6,fade_out:6,from_frame:15,to_frame:45}}]}];
 const transformed=join(root,'transformed');await renderSequence({sequence:s,resources,output_dir:transformed,content_version:'transform'});
 const center=await pixelStat(join(transformed,'program.mp4'),.4),corner=await pixelStat(join(transformed,'program.mp4'),.4,0,0);assert(center.UAVG>220&&corner.VAVG>220,'overlay must be blue centrally with visible red behind it');
 const before=await audioStat(join(transformed,'program.mp4'),'lavfi.astats.Overall.RMS_level',.1,.2),after=await audioStat(join(transformed,'program.mp4'),'lavfi.astats.Overall.RMS_level',1.7,.2);assert(before>-40&&after>-40,'scoped fades silenced audio outside their range');
 const dissolve=seq();dissolve.tracks=[{id:'v',kind:'video',clips:[{...full('red'),duration:30,source_out:{value:48000,scale:48000}},{...full('blue','blue'),at:30,duration:30,source_out:{value:48000,scale:48000},effects:{...effects(),transition:12}}]}];
 const blended=join(root,'dissolve');await renderSequence({sequence:dissolve,resources,output_dir:blended,content_version:'blend'});const middle=await pixelStat(join(blended,'program.mp4'),1.2),end=await pixelStat(join(blended,'program.mp4'),1.6);assert(middle.VAVG>130&&middle.VAVG<220&&end.UAVG>220,'dissolve did not blend the actual adjacent pictures');
 const fade=seq();fade.tracks=[{id:'v',kind:'video',clips:[{...full('blue','blue'),effects:{...effects(),fade_in:9,fade_color:'white',fade_out:9}}]}];const white=join(root,'white');await renderSequence({sequence:fade,resources,output_dir:white,content_version:'white'});assert((await pixelStat(join(white,'program.mp4'),0)).YAVG>220);assert((await pixelStat(join(white,'program.mp4'),1)).UAVG>220);
 fade.tracks[0].clips[0].effects.fade_color='black';fade.tracks.push({id:'s',kind:'subtitle',clips:[{...full('subtitle'),resource_id:'r',text:'测试字幕 Overlay'}]});const caption=join(root,'subtitle');const report=await renderSequence({sequence:fade,resources,output_dir:caption,content_version:'subtitle'});assert(report.fonts.length>0);assert((await pixelStat(join(caption,'program.mp4'),0,0,0)).YAVG<25);assert.match(await readFile(join(caption,'subtitles.ass'),'utf8'),/测试字幕 Overlay/);
 await writeFile(join(root,'effects-verification.json'),JSON.stringify({center,corner,scoped_audio:{before,after},dissolve:{middle,end},fonts:report.fonts},null,2));
});
test('cross dissolve refuses missing handles instead of silently changing edit duration',()=>{
 const s=seq(),a=clip('a'),b={...clip('b'),at:24,effects:{...effects(),transition:6}};s.tracks=[{id:'v',kind:'video',clips:[a,b]}];
 const resources=[{id:'r',kind:'video',path:join(process.cwd(),'example.mp4'),metadata:{duration:.8,streams:[{index:0,codec_type:'video'}]}}];assert.throws(()=>sequenceGraph(s,resources),/源尾部/);
});

test('current frame matches movie effects without rendering preceding frames',{timeout:120000},async()=>{
 const {renderSequenceFrame}=await import('../src/sequence.mjs');
 assert.equal(typeof renderSequenceFrame,'function','current-frame renderer must exist');
 const {root,resources}=await realFixture(),s=seq();
 s.tracks=[{id:'v',kind:'video',clips:[{...clip('red'),duration:30,source_out:{value:48000,scale:48000}},{...clip('blue'),resource_id:'blue',at:30,duration:30,source_out:{value:48000,scale:48000},effects:{...effects(),transition:12,fade_out:9}}]}];
 const movie=join(root,'frame-ref');await renderSequence({sequence:s,resources,output_dir:movie,content_version:'frame-ref'});
 for(const frame of [0,30,36,45,57]){
  const output=join(root,'still-'+frame);const report=await renderSequenceFrame({sequence:s,resources,frame,output_dir:output,content_version:'frame-ref'});
  assert.equal(report.frame,frame);assert.equal(report.content_version,'frame-ref');
  const actual=await pixelStat(join(output,'frame.png'),0),expected=await pixelStat(join(movie,'program.mp4'),frame/30);
  for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(actual[key]-expected[key])<9,`frame ${frame} ${key}: ${actual[key]} vs ${expected[key]}`);
 }
 const graph=sequenceGraph(s,resources,{frame:57});assert.equal(graph.frames,1);assert(graph.args.includes('-ss'));assert(!graph.filters.some(f=>f.includes('amix')));
 await assert.rejects(()=>renderSequenceFrame({sequence:s,resources,frame:60,output_dir:join(root,'invalid')}),/帧/);
 const moving=join(root,'moving.mp4');await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','testsrc2=s=320x180:r=30:d=2','-c:v','libx264','-threads','1',moving]);
 const source={...resources[0],path:moving,sha256:await sequenceFileHash(moving)};
 const timed=seq();timed.tracks=[{id:'v',kind:'video',clips:[{...clip('moving'),duration:30,speed:1.5,source_in:{value:2,scale:30},source_out:{value:47,scale:30},effects:{...effects(),from_frame:5,to_frame:25,scale:.8,fade_in:5,fade_out:5}}]},{id:'s',kind:'subtitle',clips:[{...clip('caption'),at:9,duration:12,text:'Current frame'}]}];
 const reference=join(root,'moving-ref');await renderSequence({sequence:timed,resources:[source],output_dir:reference,content_version:'moving'});
 for(const frame of [3,9,17,29]){
  const output=join(root,'moving-'+frame);await renderSequenceFrame({sequence:timed,resources:[source],frame,output_dir:output,content_version:'moving'});
  const actual=await pixelStat(join(output,'frame.png'),0),expected=await pixelStat(join(reference,'program.mp4'),frame/30);
  for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(actual[key]-expected[key])<12,`moving frame ${frame} ${key}: ${actual[key]} vs ${expected[key]}`);
 }
});

test('bounded preview segments preserve absolute effects, transitions, audio and captions',{timeout:120000},async()=>{
 const {root,resources}=await realFixture(),s=seq();
 s.tracks=[{id:'v',kind:'video',clips:[{...clip('red'),duration:30,source_out:{value:1,scale:1}},{...clip('blue'),resource_id:'blue',at:30,duration:30,source_out:{value:1,scale:1},effects:{...effects(),transition:12,fade_out:9}}]},{id:'a',kind:'audio',clips:[{...clip('audio',2),duration:60,source_out:{value:2,scale:1}}]}];
 const reference=join(root,'segment-reference');await renderSequence({sequence:s,resources,output_dir:reference,content_version:'reference'});
 const output=join(root,'segment-window');const result=await renderSequence({sequence:s,resources,output_dir:output,content_version:'window',window:{start:36,end:54},preview:true});
 assert.equal(result.frames,18);assert(Math.abs(result.duration-.6)<.001);
 for(const frame of [0,5,15]){const actual=await pixelStat(join(output,'program.mp4'),frame/30),expected=await pixelStat(join(reference,'program.mp4'),(frame+36)/30);for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(actual[key]-expected[key])<12,`${frame} ${key}: ${actual[key]} vs ${expected[key]}`);}
 assert(await audioStat(join(output,'program.mp4'),'lavfi.astats.Overall.RMS_level',.05,.4)>-32);
 await assert.rejects(()=>renderSequence({sequence:s,resources,output_dir:join(root,'invalid-window'),window:{start:60,end:61},preview:true}),/范围|window/);
});

test('portrait segments limit the longest edge while keeping source timing and full export',{timeout:120000},async()=>{
 const {root,resources}=await realFixture(),sequence={...seq(),width:1080,height:1920};
 sequence.tracks=[{id:'v',kind:'video',clips:[clip('video')]},{id:'a',kind:'audio',clips:[clip('audio',2)]}];
 const window={start:0,end:12},output=join(root,'portrait-segment');
 const segment=await renderSequence({sequence,resources,output_dir:output,content_version:'portrait',window,preview:true});
 const file=join(output,'program.mp4');
 const {video}=await probeMedia(file);
 assert.deepEqual([video.width,video.height,segment.frames],[540,960,12]);
 assert.deepEqual(segment.fps,sequence.fps);
 assert((await pixelStat(file,.1,262,472)).VAVG>200,'the centre contains the red source');
 assert(await audioStat(file,'lavfi.astats.Overall.RMS_level',.05,.25)>-32);
 const full=join(root,'portrait-export');
 await renderSequence({sequence,resources,output_dir:full,content_version:'portrait-full'});
 const exported=(await probeMedia(join(full,'program.mp4'))).video;
 assert.deepEqual([exported.width,exported.height,exported.frames],[1080,1920,24]);
});

test('persistent preview reuses source frames across effect edits and recompiles only changed drawing source',{timeout:120000},async()=>{
 const module=await import('../src/sequence-frame-session.mjs').catch(()=>({}));assert.equal(typeof module.createSequenceFrameSession,'function','persistent frame session is required');
 const {root,resources}=await realFixture(),s=seq();
 s.width=1920;s.height=1080;
 const drawing={id:'draw',kind:'video',sha256:'drawing-v1',metadata:{duration:60,streams:[{index:0,codec_type:'video'}]},draw_document:{schema_version:1,canvas:{width:320,height:180,fps:{num:30,den:1}},duration_frames:1800,sources:[{id:'r',engine:'remotion',source:'import React from "react";import {useCurrentFrame} from "remotion";export default ()=> <div style={{position:"absolute",left:useCurrentFrame()%120,top:20,width:60,height:60,background:"blue"}}/>;'}],nodes:[{id:'r',kind:'source',source_id:'r',from:0,duration:1800}]},assets:[]};
 s.tracks=[{id:'v',kind:'video',clips:[{...clip('drawing'),resource_id:'draw',duration:1800,source_out:{value:60,scale:1}}]}];
 drawing.draw_document.canvas.width=1920;drawing.draw_document.canvas.height=1080;
 const session=await module.createSequenceFrameSession({output_dir:join(root,'persistent'),cache_bytes:2*1024*1024,prefetch_frames:2});
 try{
  const request={sequence:s,resources:[...resources,drawing],frame:900,content_version:'v1'};
  const cold=await session.render(request),warm=await session.render({...request,frame:901});
  assert.equal(cold.frame,900);assert.equal(warm.frame,901);assert.equal(warm.diagnostics.host_loads,1);assert.equal(warm.diagnostics.source_compiles,1);
  const repeat=await session.render({...request,frame:901});assert(repeat.diagnostics.frame_cache_hits>0);
  assert.equal(typeof session.renderSegment,'function','segments must reuse the owned drawing host');
  const segment=await session.renderSegment({...request,window:{start:900,end:906},preview:true,output_dir:join(root,'persistent-segment')});
  assert.equal(segment.frames,6);assert.equal(session.diagnostics().source_compiles,1);assert.equal(session.diagnostics().host_loads,1);
  const changed=structuredClone(s);changed.tracks[0].clips[0].effects.x=20;
  const edited=await session.render({...request,sequence:changed,frame:901,content_version:'v2'});assert(edited.diagnostics.source_cache_hits>0);assert.equal(edited.diagnostics.source_compiles,1);
  const updated=structuredClone(drawing);updated.sha256='drawing-v2';updated.draw_document.sources[0].source=updated.draw_document.sources[0].source.replace('blue','red');
  const revised=await session.render({...request,resources:[...resources,updated],frame:901,content_version:'v3'});assert.equal(revised.diagnostics.source_compiles,2);assert.notEqual(revised.image,edited.image);
  assert(revised.diagnostics.cache_bytes<=2*1024*1024);
  const changedRequest={...request,resources:[...resources,updated],frame:901,content_version:'v3'};
  await session.prefetch(changedRequest);const renders=session.diagnostics().source_renders;
  const prefetched=await session.render({...changedRequest,frame:902});assert(prefetched.diagnostics.frame_cache_hits>revised.diagnostics.frame_cache_hits);assert.equal(prefetched.diagnostics.source_renders,renders);
  const obsolete=session.prefetch(changedRequest),foreground=session.render({...changedRequest,frame:200});await obsolete;assert.equal((await foreground).frame,200);
  assert(session.diagnostics().prefetched_frames>=4);
  const html=structuredClone(updated);html.sha256='html-v1';html.draw_document.sources[0]={id:'r',engine:'hyperframes',source:'<!doctype html><html><head><style>body{margin:0;background:transparent}#root{position:relative;width:1920px;height:1080px}#box{position:absolute;top:20px;width:60px;height:60px;background:lime}</style></head><body><div id="root" data-composition-id="r" data-width="1920" data-height="1080" data-duration="60"><div id="box"></div></div><script>const tl=gsap.timeline({paused:true});tl.to("#box",{x:120,duration:60,ease:"none"});window.__timelines={r:tl};</script></body></html>'};
  const native=await session.render({...request,resources:[...resources,html],frame:900,content_version:'html'});assert.equal(native.diagnostics.host_loads,1);
  const htmlPath=join(root,'persistent-hyperframes.png');await writeFile(htmlPath,Buffer.from(native.image.split(',')[1],'base64'));const green=await pixelStat(htmlPath,0,75,35);assert(green.YAVG>100&&green.UAVG<90&&green.VAVG<90,'native HyperFrames clock must share the persistent GPU browser');
  await writeFile(join(root,'persistent-verification.json'),JSON.stringify({cold:cold.diagnostics,warm:warm.diagnostics,edited:edited.diagnostics,revised:revised.diagnostics},null,2));
 }finally{await session.close();}
 await assert.rejects(()=>session.render({sequence:s,resources:[],frame:0}),/closed|关闭/i);
});

test('WebGL preview agrees with CPU transforms fades dissolves and captions',{timeout:120000},async()=>{
 const {createSequenceFrameSession}=await import('../src/sequence-frame-session.mjs'),{root,resources}=await realFixture();
 const gpu=await createSequenceFrameSession({output_dir:join(root,'gpu-effects'),prefetch_frames:0});
 const cpu=await createSequenceFrameSession({output_dir:join(root,'cpu-effects'),prefetch_frames:0,backend:'cpu'});
 const s=seq(),full=(id,resource='r')=>({...clip(id),resource_id:resource,duration:30,source_out:{value:1,scale:1}});
 s.tracks=[{id:'v',kind:'video',clips:[full('red'),{...full('blue','blue'),at:30,effects:{...effects(),transition:12,fade_out:9,scale:.7,x:10,crop_left:.1}}]},{id:'s',kind:'subtitle',clips:[{...full('caption'),at:30,text:'GPU / CPU'}]}];
 try{
  const reference=join(root,'gpu-export-ref');await renderSequence({sequence:s,resources,output_dir:reference,content_version:'gpu-compare'});
  for(const frame of [0,30,36,45,57]){
   const input={sequence:s,resources,frame,content_version:'gpu-compare'};
   const actual=await gpu.render(input),expected=await cpu.render(input);assert.equal(actual.diagnostics.backend,'webgl2',actual.diagnostics.fallback_reason);assert.equal(expected.diagnostics.backend,'cpu-ffmpeg');
   const g=join(root,'gpu-'+frame+'.png'),c=join(root,'cpu-'+frame+'.png');await writeFile(g,Buffer.from(actual.image.split(',')[1],'base64'));await writeFile(c,Buffer.from(expected.image.split(',')[1],'base64'));
   for(const [x,y] of [[150,50],[0,0],[150,120]]){const a=await pixelStat(g,0,x,y),b=await pixelStat(c,0,x,y),movie=await pixelStat(join(reference,'program.mp4'),frame/30,x,y);for(const key of ['YAVG','UAVG','VAVG']){assert(Math.abs(a[key]-b[key])<12,`frame ${frame} (${x},${y}) ${key}: GPU ${a[key]} CPU ${b[key]}`);assert(Math.abs(a[key]-movie[key])<12,`GPU frame ${frame} differs from exported ${key}`);}}
  }
  const white=structuredClone(s);white.tracks[0].clips[1].effects.fade_color='white';
  const w=await gpu.render({sequence:white,resources,frame:57,content_version:'white'}),wCPU=await cpu.render({sequence:white,resources,frame:57,content_version:'white'});
  const wp=join(root,'gpu-white.png'),cp=join(root,'cpu-white.png');await writeFile(wp,Buffer.from(w.image.split(',')[1],'base64'));await writeFile(cp,Buffer.from(wCPU.image.split(',')[1],'base64'));
  const whiteGPU=await pixelStat(wp,0,150,50),whiteCPU=await pixelStat(cp,0,150,50);for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(whiteGPU[key]-whiteCPU[key])<12,'white fade must preserve the CPU alpha rule');
  const animated=structuredClone(s);animated.tracks=animated.tracks.slice(0,1);animated.tracks[0].clips[1].effects={...effects(),opacity:.7,parameter_keyframes:[{kind:'缩放',frame:0,duration:30,value:.5},{kind:'透明度',frame:0,duration:30,start_value:0,value:1}],subtitles:[{text:'定时字幕',frame:0,duration:10,size_px:18}]};
  const animatedExport=join(root,'animated-export');await renderSequence({sequence:animated,resources,output_dir:animatedExport,content_version:'animated'});
  for(const frame of [36,45,57]){const input={sequence:animated,resources,frame,content_version:'animated'},g=await gpu.render(input),c=await cpu.render(input),gp=join(root,'animated-gpu.png'),cp=join(root,'animated-cpu.png');await writeFile(gp,Buffer.from(g.image.split(',')[1],'base64'));await writeFile(cp,Buffer.from(c.image.split(',')[1],'base64'));for(const [x,y] of [[150,50],[0,0],[150,120]]){const a=await pixelStat(gp,0,x,y),b=await pixelStat(cp,0,x,y),movie=await pixelStat(join(animatedExport,'program.mp4'),frame/30,x,y);for(const key of ['YAVG','UAVG','VAVG']){assert(Math.abs(a[key]-b[key])<12,`animated frame ${frame} ${key} GPU/CPU`);assert(Math.abs(a[key]-movie[key])<12,`animated frame ${frame} ${key} GPU/movie`);}}}
  await writeFile(join(root,'gpu-verification.json'),JSON.stringify(gpu.diagnostics(),null,2));
 }finally{await gpu.close();await cpu.close();}
});

test('editable drawing FrameSource composites alpha with footage and sound on the shared sequence clock',{timeout:120000},async()=>{
 const {root,resources}=await realFixture(),s=seq();
 const drawing={id:'draw',name:'Editable source',kind:'video',sha256:'snapshot-1',metadata:{duration:1,streams:[{index:0,codec_type:'video'}]},draw_document:{schema_version:1,canvas:{width:320,height:180,fps:{num:24,den:1}},duration_frames:24,preset:'auto',sources:[{id:'react',engine:'remotion',canvas:{width:320,height:180,fps:{num:12,den:1}},duration_frames:12,source:'import React from "react";import {useCurrentFrame,useVideoConfig} from "remotion";export default function Scene(){const f=useCurrentFrame(),c=useVideoConfig();return <div style={{position:"absolute",left:50+f*4,top:40,width:80,height:50,background:c.fps===12?"rgba(0,0,255,.5)":"lime"}}/>;}'}],nodes:[{id:'react',kind:'source',source_id:'react',from:0,duration:24}]},assets:[]};
 s.tracks=[{id:'base',kind:'video',clips:[clip('base')]},{id:'drawing',kind:'video',clips:[{...clip('drawing'),resource_id:'draw'}]},{id:'tone',kind:'audio',clips:[clip('audio',2)]}];
 const {renderSequenceFrame}=await import('../src/sequence.mjs'),{renderDrawSourceFrame}=await import('../src/unified-draw.mjs');
 const stillOutput=join(root,'drawing-still'),requested=[],started=performance.now();
 await renderSequenceFrame({sequence:s,resources:[...resources,drawing],frame:12,output_dir:stillOutput,content_version:'drawing-source'},{drawFrame:async input=>{requested.push(input.frame);return renderDrawSourceFrame(input);}});
 const stillMs=performance.now()-started;assert.deepEqual(requested,[9],'seek a single rational-clock drawing frame');
 const {prepareDrawFrameSource}=await import('../src/unified-draw.mjs');
 const proxyOutput=join(root,'drawing-proxy'),proxy=await renderSequence({sequence:s,resources:[...resources,drawing],window:{start:1,end:23},preview:true,output_dir:proxyOutput,content_version:'proxy'}, {prepareDrawFrames:options=>{assert.equal(options.frame_step,3,'preview source samples at most 8 fps without changing its clock');assert.equal(options.from_frame%options.frame_step,0);return prepareDrawFrameSource(options);}});
 assert.equal(proxy.frames,22);assert.deepEqual(proxy.fps,s.fps);const proxyPixels=await pixelStat(join(proxyOutput,'program.mp4'),.3,75,55);assert(proxyPixels.UAVG>165&&proxyPixels.VAVG>165,'proxy must preserve composited source alpha');
 const movieStarted=performance.now(),output=join(root,'drawing-source'),report=await renderSequence({sequence:s,resources:[...resources,drawing],output_dir:output,content_version:'drawing-source'}),movieMs=performance.now()-movieStarted;
 const center=await pixelStat(join(output,'program.mp4'),.4,75,55),outside=await pixelStat(join(output,'program.mp4'),.4,230,55),audio=await audioStat(join(output,'program.mp4'),'lavfi.astats.Overall.RMS_level',.1,.4);
 assert(center.UAVG>165&&center.VAVG>165,'half-alpha blue source must blend with red footage');assert(outside.VAVG>220&&outside.UAVG<110,'transparent drawing area must preserve footage');assert(audio>-45,'existing sound must survive the drawing FrameSource');assert.equal(report.frames,24);
 const snapshot=JSON.parse(await readFile(join(output,'sequence.json'),'utf8'));assert(snapshot.resources.some(r=>r.draw_document),'editable source snapshot must remain distinguishable from flattened video');
 const still=await pixelStat(join(stillOutput,'frame.png'),0,75,55);for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(still[key]-center[key])<9,`drawing ${key} differs from movie`);
 const {createSequenceFrameSession}=await import('../src/sequence-frame-session.mjs');const gpu=await createSequenceFrameSession({output_dir:join(root,'drawing-gpu'),prefetch_frames:0});
 try{
  const result=await gpu.render({sequence:s,resources:[...resources,drawing],frame:12,content_version:'drawing-source'}),path=join(root,'drawing-gpu.png');await writeFile(path,Buffer.from(result.image.split(',')[1],'base64'));
  const pixels=await pixelStat(path,0,75,55);for(const key of ['YAVG','UAVG','VAVG'])assert(Math.abs(pixels[key]-center[key])<9,'GPU must blend real drawing alpha with footage');
  await gpu.render({sequence:s,resources:[...resources,drawing],frame:10,content_version:'drawing-source'});const repeated=await gpu.render({sequence:s,resources:[...resources,drawing],frame:11,content_version:'drawing-source'});assert(repeated.diagnostics.source_cache_hits>0,'rational source clocks must reuse the same source frame');
 }finally{await gpu.close();}
 await writeFile(join(output,'verification.json'),JSON.stringify({report,center,outside,audio,still,still_ms:stillMs,movie_ms:movieMs,independent_source_fps:12,drawing_fps:24,sequence_fps:30},null,2));
});

test('program exposes FFmpeg basic inspection for an audible visible sequence',{timeout:120000},async()=>{
 const {root,resources}=await realFixture();
 const s=seq(); const v={...clip('v'),duration:60,source_out:{value:96000,scale:48000}};
 const a={...clip('a',2),duration:60,source_out:{value:96000,scale:48000}};
 s.tracks=[{id:'v',kind:'video',clips:[v]},{id:'a',kind:'audio',clips:[a]}];
 const report=await renderSequence({sequence:s,resources,output_dir:join(root,'quality'),content_version:'quality'});
 assert.ok(report.basic_quality,'the rendered program must include a basic inspection result');
 assert.equal(report.basic_quality.status,'passed');
 assert.equal(report.basic_quality.sha256,report.sha256);
 assert.deepEqual(report.basic_quality.issues,[]);
});


test('basic inspection identifies unexpected black and silence while skipping silent sources',{timeout:120000},async()=>{
 const {root,path}=await realFixture();const bad=join(root,'quality-bad.mp4');
 await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=black:s=320x180:r=30:d=2','-f','lavfi','-i','anullsrc=r=48000:cl=stereo:d=2','-c:v','libx264','-threads','1','-c:a','aac',bad]);
 const args={path:bad,sha256:await sequenceFileHash(bad),duration:2,videoWindows:[{start:0,end:2}],audioSources:[{path,stream_index:2,source_start:0,source_end:2,timeline_start:0,timeline_end:2,speed:1}]};
 const failed=await inspectBasicQuality(args);assert.equal(failed.status,'failed');assert.deepEqual(failed.issues.map(x=>x.kind).sort(),['black','silence']);
 for(const issue of failed.issues){assert.ok(issue.start<.1);assert.ok(issue.end>1.9)}
 const ignored=await inspectBasicQuality({...args,videoWindows:[],audioSources:[{...args.audioSources[0],stream_index:1}]});assert.equal(ignored.status,'not_checked');assert.equal(ignored.audio_status,'skipped');
 const unavailable=await inspectBasicQuality(args,{run:async()=>{throw Error('ffmpeg unavailable')}});assert.equal(unavailable.status,'not_checked');
});


test('basic inspection honors intentional track attenuation',{timeout:120000},async()=>{
 const {root,resources}=await realFixture();const silent=join(root,'quality-bad.mp4');
 await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=blue:s=320x180:r=30:d=2','-f','lavfi','-i','anullsrc=r=48000:cl=stereo:d=2','-c:v','libx264','-threads','1','-c:a','aac',silent]);
 const s=seq(),v={...clip('v'),duration:60,source_out:{value:96000,scale:48000}},a={...clip('a',2),duration:60,source_out:{value:96000,scale:48000}};
 s.tracks=[{id:'v',kind:'video',clips:[v]},{id:'a',kind:'audio',gain_db:-80,clips:[a]}];
 const result=await inspectBasicQuality({path:silent,sha256:await sequenceFileHash(silent),duration:2,...sequenceExpectations(s,resources,selectedTracks(s))});
 assert.equal(result.status,'passed');assert.equal(result.audio_status,'skipped');
});


test('basic inspection maps source speed, position and explicit black fades',{timeout:120000},async()=>{
 const {root,path,resources}=await realFixture();const bad=join(root,'quality-offset.mp4');
 await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=black:s=320x180:r=30:d=4','-f','lavfi','-i','anullsrc=r=48000:cl=stereo:d=4','-c:v','libx264','-threads','1','-c:a','aac',bad]);
 const args={path:bad,sha256:await sequenceFileHash(bad),duration:4,videoWindows:[],audioSources:[{path,stream_index:2,source_start:0,source_end:2,timeline_start:1,timeline_end:2,speed:2}]};
 const q=await inspectBasicQuality(args);assert.equal(q.status,'failed');assert.equal(q.issues.length,1);assert.equal(q.issues[0].kind,'silence');assert.ok(Math.abs(q.issues[0].start-1)<.05);assert.ok(Math.abs(q.issues[0].end-2)<.05);
 const s=seq();s.tracks=[{id:'v',kind:'video',clips:[{...clip('v'),duration:120,effects:{...effects(),fade_in:105,fade_color:'black'}}]}];
 const expected=sequenceExpectations(s,resources,selectedTracks(s));
 const fade=await inspectBasicQuality({...args,...expected});assert.equal(fade.status,'passed');
 s.tracks[0].clips[0].effects={...effects(),opacity:0,parameter_keyframes:[{kind:'透明度',frame:60,duration:0,value:1}]};
 assert.deepEqual(sequenceExpectations(s,resources,selectedTracks(s)).videoWindows,[{start:2,end:4}],'intentional zero opacity is not missing video');
 s.tracks[0].clips[0].effects.from_frame=30;s.tracks[0].clips[0].effects.to_frame=90;
 assert.deepEqual(sequenceExpectations(s,resources,selectedTracks(s)).videoWindows,[{start:0,end:1},{start:2,end:4}],'opacity exclusion honors the clip effect scope');
});


test('source pauses are not treated as missing program audio',{timeout:120000},async()=>{
 const {root}=await realFixture(),src=join(root,'quality-pauses.mp4');
 await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=red:s=320x180:r=30:d=4','-f','lavfi','-i','sine=frequency=880:sample_rate=48000:duration=4','-af',"volume=0:enable='between(t,1,3)'",'-c:v','libx264','-threads','1','-c:a','aac',src]);
 const q=await inspectBasicQuality({path:src,sha256:await sequenceFileHash(src),duration:4,videoWindows:[{start:0,end:4}],audioSources:[{path:src,stream_index:1,source_start:0,source_end:4,timeline_start:0,timeline_end:4,speed:1}]});
 assert.equal(q.status,'passed');assert.deepEqual(q.issues,[]);
});

test('quality detector failure does not prevent the encoded program being returned',{timeout:120000},async()=>{
 const {root,resources}=await realFixture(),s=seq(),v={...clip('v'),duration:60,source_out:{value:96000,scale:48000}};
 s.tracks=[{id:'v',kind:'video',clips:[v]}];
 const checkingFails=(cmd,args,options)=>args.some(a=>a.includes('blackdetect'))?Promise.reject(Error('detector unavailable')):runTool(cmd,args,options);
 const out=join(root,'quality-unavailable'),report=await renderSequence({sequence:s,resources,output_dir:out,content_version:'quality-unavailable'},{run:checkingFails});
 assert.equal(report.basic_quality.status,'not_checked');assert.equal(await sequenceFileHash(join(out,'program.mp4')),report.sha256);
});


test('muted tracks and an intentional empty timeline gap are skipped',{timeout:120000},async()=>{
 const {root,resources}=await realFixture(),s=seq();
 const v={...clip('v'),at:30,duration:60,source_out:{value:96000,scale:48000}},a={...clip('a',2),at:30,duration:60,source_out:{value:96000,scale:48000}};
 s.tracks=[{id:'v',kind:'video',clips:[v]},{id:'a',kind:'audio',muted:true,clips:[a]}];
 const report=await renderSequence({sequence:s,resources,output_dir:join(root,'quality-muted'),content_version:'quality-muted'});
 assert.equal(report.basic_quality.status,'passed');assert.equal(report.basic_quality.audio_status,'skipped');assert.deepEqual(report.basic_quality.issues,[]);
});

test('overlapping source audio expectations merge on the timeline',{timeout:120000},async()=>{
 const {root,path}=await realFixture(),bad=join(root,'quality-overlap.mp4');
 await runTool('ffmpeg',['-v','error','-y','-f','lavfi','-i','color=c=red:s=320x180:r=30:d=2','-f','lavfi','-i','anullsrc=r=48000:cl=stereo:d=2','-c:v','libx264','-threads','1','-c:a','aac',bad]);
 const source={path,stream_index:2,source_start:0,source_end:1.2,speed:1};
 const q=await inspectBasicQuality({path:bad,sha256:await sequenceFileHash(bad),duration:2,videoWindows:[{start:0,end:2}],audioSources:[{...source,timeline_start:0,timeline_end:1.2},{...source,timeline_start:.8,timeline_end:2}]});
 assert.equal(q.status,'failed');const issue=q.issues.find(i=>i.kind==='silence');assert.ok(issue);assert.ok(issue.start<.05);assert.ok(issue.end>1.95);
});
