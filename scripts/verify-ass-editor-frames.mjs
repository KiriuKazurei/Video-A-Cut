import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
const run=promisify(execFile),[root]=process.argv.slice(2);
if(!root)throw Error('acceptance directory required');
const mp4=path.join(root,'styled.mp4');
const {stdout:probeRaw}=await run('ffprobe',['-v','error','-show_streams','-show_format','-of','json',mp4]);
const probe=JSON.parse(probeRaw),video=probe.streams.find(s=>s.codec_type==='video'),audio=probe.streams.find(s=>s.codec_type==='audio');
assert.equal(video.width,640);assert.equal(video.height,360);assert.equal(video.codec_name,'h264');assert.equal(audio.codec_name,'aac');assert.equal(Number(audio.sample_rate),48000);
const width=video.width,height=video.height;
const frame=async(seconds,name)=>{
 const {stdout}=await run('ffmpeg',['-v','error','-i',mp4,'-ss',String(seconds),'-frames:v','1','-pix_fmt','rgb24','-f','rawvideo','pipe:1'],{encoding:'buffer',maxBuffer:8*1024*1024});
 assert.equal(stdout.length,width*height*3);
 await run('ffmpeg',['-v','error','-y','-i',mp4,'-ss',String(seconds),'-frames:v','1',path.join(root,name)]);
 return stdout;
};
const stats=(bytes,y0,y1)=>{
 let red=0,blue=0,yellow=0,bright=0,redEnergy=0,minX=width,maxX=0,minY=height,maxY=0;
 for(let y=y0;y<y1;y++)for(let x=0;x<width;x++){
  const i=(y*width+x)*3,[r,g,b]=bytes.subarray(i,i+3);
  if(r>100&&r-g>50&&r-b>50)red++;
  if(b>100&&b-r>60&&b-g>35)blue++;
  if(r>140&&g>140&&b<100)yellow++;
  redEnergy+=Math.max(0,r-Math.max(g,b));
  if(Math.max(r,g,b)>60){bright++;minX=Math.min(minX,x);maxX=Math.max(maxX,x);minY=Math.min(minY,y);maxY=Math.max(maxY,y);}
 }
 return {red,blue,yellow,bright,redEnergy,bounds:[minX,minY,maxX,maxY]};
};
const before=stats(await frame(1.1,'before-start.png'),0,height);
const faded=stats(await frame(1.3,'fade-in.png'),170,height);
const opaqueBytes=await frame(2,'bottom-red-blue-outline.png'),opaque=stats(opaqueBytes,170,height),topDuringFirst=stats(opaqueBytes,0,150);
const fadedOut=stats(await frame(3.1,'fade-out.png'),170,height);
const topBytes=await frame(5,'top-blue-yellow-outline.png'),top=stats(topBytes,0,150),bottomDuringTop=stats(topBytes,170,height);
assert.equal(before.bright,0,'source SRT must not be burned before the adjusted ASS start');
assert(opaque.red>300&&opaque.blue>80,'first cue must show red text and blue outline at bottom');
assert.equal(topDuringFirst.bright,0,'literal ASS-looking text must not reposition the cue');
assert(faded.redEnergy<opaque.redEnergy*0.8&&fadedOut.redEnergy<opaque.redEnergy*0.8,'both fade-in and fade-out must change decoded pixels');
assert(top.blue>200&&top.yellow>80,'second cue must show blue text and yellow outline at top');
assert.equal(bottomDuringTop.bright,0,'per-cue alignment must not retain an extra bottom SRT layer');
assert(opaque.bounds[0]>=25&&opaque.bounds[2]<615&&opaque.bounds[3]<350,'Chinese wrapping must fit subtitle safe area');
const style=JSON.parse(await fs.readFile(path.join(root,'ass-style.json'),'utf8'));
assert.equal(style.play_res_y,720);assert.equal(style.play_res_x,1280);assert.equal(style.style.font_size,64);
const receipt=JSON.parse(await fs.readFile(path.join(root,'preview-receipt.json'),'utf8'));
assert(receipt.fonts?.length>0&&receipt.fonts.every(font=>/^[0-9a-f]{64}$/.test(font.sha256)));
const result={status:'passed',mode:'actual_decoded_rgb_frames',media:{width,height,duration:Number(probe.format.duration),video_codec:video.codec_name,audio_codec:audio.codec_name},
 before,faded,opaque,fadedOut,top,topDuringFirst,bottomDuringTop,fonts:receipt.fonts,human_font_wrapping_timing_confirmation:'pending'};
await fs.writeFile(path.join(root,'frames.json'),JSON.stringify(result,null,2));
console.log(JSON.stringify({frame_checks:'passed',first_text_red:opaque.red,first_outline_blue:opaque.blue,second_text_blue:top.blue,second_outline_yellow:top.yellow}));
