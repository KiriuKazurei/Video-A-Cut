import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { sequenceGraph, selectedTracks, renderSequence, sequenceFileHash, waveform } from '../src/sequence.mjs';
import { runTool } from '../src/media.mjs';

const effects = () => ({scale:1,x:0,y:0,crop_left:0,crop_right:0,crop_top:0,crop_bottom:0,fade_in:0,fade_out:0,transition:0,gain_db:0});
const clip = (id,stream=0) => ({id,resource_id:'r',name:id,stream_index:stream,at:0,duration:24,source_in:{value:0,scale:48000},source_out:{value:38400,scale:48000},speed:1,effects:effects()});
const seq = () => ({id:'s',width:320,height:180,fps:{num:30,den:1},sample_rate:48000,tracks:[],markers:[],groups:[]});
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

test('editable drawing FrameSource composites alpha with footage and sound on the shared sequence clock',{timeout:120000},async()=>{
 const {root,resources}=await realFixture(),s=seq();
 const drawing={id:'draw',name:'Editable source',kind:'video',sha256:'snapshot-1',metadata:{duration:1,streams:[{index:0,codec_type:'video'}]},draw_document:{schema_version:1,canvas:{width:320,height:180,fps:{num:24,den:1}},duration_frames:24,preset:'auto',sources:[{id:'react',engine:'remotion',canvas:{width:320,height:180,fps:{num:12,den:1}},duration_frames:12,source:'import React from "react";import {useCurrentFrame,useVideoConfig} from "remotion";export default function Scene(){const f=useCurrentFrame(),c=useVideoConfig();return <div style={{position:"absolute",left:50+f*4,top:40,width:80,height:50,background:c.fps===12?"rgba(0,0,255,.5)":"lime"}}/>;}'}],nodes:[{id:'react',kind:'source',source_id:'react',from:0,duration:24}]},assets:[]};
 s.tracks=[{id:'base',kind:'video',clips:[clip('base')]},{id:'drawing',kind:'video',clips:[{...clip('drawing'),resource_id:'draw'}]},{id:'tone',kind:'audio',clips:[clip('audio',2)]}];
 const output=join(root,'drawing-source'),report=await renderSequence({sequence:s,resources:[...resources,drawing],output_dir:output,content_version:'drawing-source'});
 const center=await pixelStat(join(output,'program.mp4'),.4,75,55),outside=await pixelStat(join(output,'program.mp4'),.4,230,55),audio=await audioStat(join(output,'program.mp4'),'lavfi.astats.Overall.RMS_level',.1,.4);
 assert(center.UAVG>165&&center.VAVG>165,'half-alpha blue source must blend with red footage');assert(outside.VAVG>220&&outside.UAVG<110,'transparent drawing area must preserve footage');assert(audio>-45,'existing sound must survive the drawing FrameSource');assert.equal(report.frames,24);
 const snapshot=JSON.parse(await readFile(join(output,'sequence.json'),'utf8'));assert(snapshot.resources.some(r=>r.draw_document),'editable source snapshot must remain distinguishable from flattened video');
 await writeFile(join(output,'verification.json'),JSON.stringify({report,center,outside,audio,independent_source_fps:12,drawing_fps:24,sequence_fps:30},null,2));
});
