// The script control plane supplies only frozen, validated local source windows.
// Effects are baked into the media referenced by Premiere XML, not merely labels.
import { writeFile, mkdir, copyFile, unlink, readdir, lstat } from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import { resolve, join, basename } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createHash } from 'node:crypto';
import { runTool, probeMedia } from './media.mjs';
import { renderSequence, waveform } from './sequence.mjs';
import { loudness } from './loudness.mjs';
import { installedFonts, resolveFonts } from './fonts.mjs';
import { assText, assTime, wrapAssText } from '../adapters/ass.mjs';
import { formatsAdapter } from '../adapters/formats.mjs';

const frameRate = 30;
async function fileSHA256(file) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(file)) hash.update(chunk);
  return hash.digest('hex');
}
const overlapDuration = (previous, current) => Math.floor(Math.min(.35, previous / 3, current / 3) * frameRate) / frameRate;
const filterPath = p => resolve(p).replaceAll('\\', '/').replaceAll(':', '\\:').replaceAll("'", "'\\''").replaceAll('[', '\\[').replaceAll(']', '\\]');

export async function scriptFonts() {
  return [...new Set((await installedFonts()).map(f => f.family))].sort();
}

export async function renderScript(input, { run = runTool, probe = probeMedia, fonts = scriptFonts, resolveFont = resolveFonts } = {}) {
  const { segments, document, output_dir: output } = input;
  if (!Array.isArray(segments) || !segments.length || segments.length > 20 || segments.length !== document?.paragraphs?.length) throw new Error('首版每次交付支持 1～20 个段落');
  await mkdir(output, { recursive: true });
  const available = await fonts();
  const defaultFont = ['Microsoft YaHei', 'Microsoft YaHei UI', 'Noto Sans CJK SC', 'SimHei', 'Arial'].find(f => available.includes(f)) ?? available[0];
  const paths = [], durations = [], cues = [], fontRecords = [];
  let cursor = 0;
  for (let index = 0; index < segments.length; index++) {
    const seg = segments[index], p = document.paragraphs[index];
    if (![seg.start, seg.end].every(Number.isFinite) || seg.start < 0 || seg.end <= seg.start || !seg.path) throw new Error('视频时间窗无效');
    const info = await probe(seg.path);
    if (!info.video || seg.end > info.duration + 1 / frameRate) throw new Error('视频窗口超出源文件');
    const duration = Math.round((seg.end - seg.start) * frameRate) / frameRate;
    if (duration < .1 || duration > 120) throw new Error('单段需为 0.1～120 秒');
    const vf = ['scale=1280:720:force_original_aspect_ratio=decrease', 'pad=1280:720:(ow-iw)/2:(oh-ih)/2', 'setsar=1', 'fps=30', 'format=yuv420p'];
    const incoming = p.effect === '黑屏' ? 'black' : p.effect === '白屏' ? 'white' : null;
    if (incoming) vf.push(`fade=t=in:st=0:d=${Math.min(.175, duration / 3)}:color=${incoming}`);
    const next = document.paragraphs[index + 1]?.effect;
    if (next === '黑屏' || next === '白屏') {
      const fade = Math.min(.175, duration / 3);
      vf.push(`fade=t=out:st=${duration - fade}:d=${fade}:color=${next === '黑屏' ? 'black' : 'white'}`);
    }
    const cut = join(output, `paragraph-${index + 1}.mp4`);
    const voice = seg.voice_file;
    const audio = info.audio ? '0:a:0' : '1:a:0';
    const inputs = ['-ss', String(seg.start), '-i', seg.path, '-f', 'lavfi', '-i', 'anullsrc=r=48000:cl=stereo'];
    const fades = `afade=t=in:st=0:d=0.03,afade=t=out:st=${Math.max(0,duration-.03)}:d=0.03`;
    let audioArgs = ['-map', audio, '-af', `aresample=48000,apad,${fades}`];
    if (voice) {
      const offset = seg.voice_offset ?? 0;
      const spoken = await probe(voice);
      if (!Number.isFinite(offset) || offset < 0 || !spoken.audio || spoken.duration + offset > duration + 1 / frameRate) throw new Error('配音超出片段时间窗');
      inputs.push('-i', voice);
      const delay = Math.round(offset * 1000);
      audioArgs = ['-filter_complex', `[${audio}]aresample=48000,volume=0.25,apad[game];[2:a]aresample=48000,adelay=${delay}|${delay},apad[voice];[game][voice]amix=inputs=2:duration=first:normalize=0,alimiter=limit=0.95,${fades}[mixed]`, '-map', '[mixed]'];
    }
    await run('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', ...inputs, '-map', '0:v:0', ...audioArgs,
      '-t', String(duration), '-vf', vf.join(','), '-c:v', 'libx264', '-preset', 'ultrafast',
      '-crf', '20', '-threads', '1', '-c:a', 'aac', '-ac', '2', '-ar', '48000', '-movflags', '+faststart', cut], { timeoutMs: 120_000 });
    const overlap = index > 0 && p.effect === '交叉溶解' ? overlapDuration(durations[index - 1], duration) : 0;
    cursor -= overlap;
	if (overlap && cues.length) cues[cues.length - 1].end = Math.min(cues[cues.length - 1].end, cursor);
    const sub = p.subtitle;
    if (sub || (seg.subtitles_enabled ?? input.subtitles_enabled)) {
      const font = sub?.font || defaultFont;
      if (!font || !available.includes(font)) throw new Error(`字体不可用：${font || '没有已安装字体'}`);
      const resolved = await resolveFont([font]);
      for (const f of resolved) if (!fontRecords.some(r => r.sha256 === f.sha256 && r.family === f.family)) fontRecords.push({ family: f.family, sha256: f.sha256 });
      cues.push({ start: cursor, end: cursor + duration, text: sub?.text ?? seg.narration, font, size: sub?.size_px || 36 });
    }
    paths.push(cut); durations.push(duration); cursor += duration;
  }
  const joined = join(output, 'joined.mp4');
  if (paths.length === 1) await copyFile(paths[0], joined);
  else {
    const args = paths.flatMap(p => ['-i', p]);
    const filters = paths.flatMap((_, i) => [`[${i}:v]setpts=PTS-STARTPTS,fps=30[v${i}]`, `[${i}:a]atrim=duration=${durations[i]},asetpts=PTS-STARTPTS[a${i}]`]);
    let v = 'v0', a = 'a0', length = durations[0];
    for (let i = 1; i < paths.length; i++) {
      const vn = `mixv${i}`, an = `mixa${i}`;
      if (document.paragraphs[i].effect === '交叉溶解') {
        const d = overlapDuration(durations[i - 1], durations[i]);
        filters.push(`[${v}][v${i}]xfade=transition=fade:duration=${d}:offset=${length - d}[raw${vn}]`, `[${a}][a${i}]acrossfade=d=${d}:c1=tri:c2=tri[${an}]`);
        length += durations[i] - d;
      } else {
        filters.push(`[${v}][${a}][v${i}][a${i}]concat=n=2:v=1:a=1[raw${vn}][${an}]`);
        length += durations[i];
      }
      filters.push(`[raw${vn}]setpts=PTS-STARTPTS,fps=30[${vn}]`);
      v = vn; a = an;
    }
    await run('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', ...args, '-filter_complex_threads', '1', '-filter_complex', filters.join(';'),
      '-map', `[${v}]`, '-map', `[${a}]`, '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '20', '-threads', '1', '-c:a', 'aac', '-r', '30', joined], { timeoutMs: 240_000 });
  }
  const final = join(output, 'final.mp4');
  if (cues.length) {
    let ass = '[Script Info]\nScriptType: v4.00+\nPlayResX: 1280\nPlayResY: 720\nWrapStyle: 0\n\n[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n';
    for (const [i, c] of cues.entries()) ass += `Style: P${i},${c.font},${c.size},&H00FFFFFF,&H00FFFFFF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2,0,2,40,40,36,1\n`;
    ass += '\n[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\n';
    for (const [i, c] of cues.entries()) ass += `Dialogue: ${i},${assTime(Math.round(c.start * 100))},${assTime(Math.round(c.end * 100))},P${i},,0,0,0,,${assText(wrapAssText(c.text, Math.max(2, Math.floor(1180 / c.size) * 2)))}\n`;
    const file = join(output, 'subtitles.ass'); await writeFile(file, ass, 'utf8');
    await run('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-i', joined, '-vf', `ass=filename='${filterPath(file)}'`,
      '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '20', '-threads', '1', '-c:a', 'copy', '-movflags', '+faststart', final], { timeoutMs: 240_000 });
  } else await copyFile(joined, final);
  const media = await probe(final), frames = media.video.frames || Math.round(media.video.duration * frameRate);
  await run('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', '-i', final, '-vn', '-c:a', 'pcm_s16le', '-ar', '48000', '-ac', '2', join(output, 'audio_Game.wav')], { timeoutMs: 60_000 });
  const timeline = { fps: frameRate, sample_rate: 48000, total_frames: frames, media: { baked: media }, tracks: [
    { id: 'Video', name: '脚本画面（效果已渲染）', kind: 'video', items: [{ id: 'script-film', src: 'baked', in_frame: 0, out_frame: frames, timeline_in_frame: 0, timeline_out_frame: frames, frames }] },
    { id: 'Game', name: segments.some(s => s.voice_file) ? '原声与旁白混音' : '原视频声音', kind: 'audio', items: [] }
  ] };
  const artifacts = await formatsAdapter.run({ stagingDir: output, outputDir: output, timeline });
  await writeFile(join(output, 'script.json'), JSON.stringify({ schema_version: 1, script: input.script, document: input.document,
    segments: segments.map(({ path, voice_file, ...segment }) => ({ ...segment, ...(voice_file ? { voice_file: basename(voice_file) } : {}) })) }, null, 2));
  await writeFile(join(output, 'narration.txt'), segments.map((s, i) => `${i + 1}. ${s.narration}`).join('\n\n'), 'utf8');
  await writeFile(join(output, 'knowledge-sources.json'), JSON.stringify(input.sources ?? [], null, 2));
  await writeFile(join(output, 'IMPORT.txt'), '在 Premiere 中导入 edit.xml。画面效果和强制字幕已渲染进 media/ 中的视频；不是原生可编辑转场。\n若移动了交付目录，请将素材重新链接到 media/。final.mp4 是同一画面的站内预览。旁白文本在 narration.txt，启用配音的段落已按声音设置与段内起点合成、混音；audio_Game.wav 是与成片对应的混音轨。\n', 'utf8');
  const hash = await fileSHA256(final);
  for (const file of [...paths, joined]) await unlink(file);
  // The shared renderer also supports the old workflow's package importer.
  const bakedEDL = { timeline: { fps: frameRate, sample_rate: 48000 },
    video: [{ src: 'final.mp4', in: 0, out: media.duration, timeline_in: 0 }],
    game_audio: [{ src: 'audio_Game.wav', in: 0, out: media.duration, timeline_in: 0, gain_db: 0 }],
    voice: [], subtitle: [], music: [], script_binding: input.script };
  await writeFile(join(output,'edl.json'),JSON.stringify(bakedEDL,null,2));
  await writeFile(join(output,'delivery-manifest.json'),JSON.stringify({ schema_version:1,source_edl:'edl.json',
    artifacts:[...artifacts,{kind:'preview_mp4',path:'final.mp4'},{kind:'audio',path:'audio_Game.wav'},
      {kind:'script',path:'script.json'},{kind:'instructions',path:'IMPORT.txt'}] },null,2));
  const files = [];
  const collect = async (directory, prefix = '') => {
    for (const name of (await readdir(directory)).sort()) {
      const full = join(directory, name), stat = await lstat(full), relative = prefix + name;
      if (stat.isSymbolicLink()) throw new Error('交付目录不能包含符号链接');
      if (stat.isDirectory()) await collect(full, relative + '/');
      else if (stat.isFile() && relative !== 'script-delivery.json') {
        files.push({ path: relative, size_bytes: stat.size, sha256: await fileSHA256(full) });
      } else if (!stat.isFile()) throw new Error('交付文件类型无效');
      if (files.length > 500) throw new Error('交付文件数量超出限制');
    }
  };
  await collect(output);
  const report = { schema_version: 2, script_id: input.script?.script_id, version: input.script?.version,
    visible_set_sha: input.script?.visible_set_sha, tts_enabled: segments.some(s => Boolean(s.voice_file)),
    baked_effects: true, width: media.video.width, height: media.video.height, duration: media.duration, frames,
    fonts: fontRecords, sha256: hash, artifacts, files };
  await writeFile(join(output, 'script-delivery.json'), JSON.stringify(report, null, 2));
  return report;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    if (process.argv[2] === 'fonts') process.stdout.write(JSON.stringify({ ok: true, fonts: await scriptFonts() }));
    else {
      const chunks = []; let bytes = 0;
      for await (const chunk of process.stdin) { bytes += chunk.length; if (bytes > 4 * 1024 * 1024) throw new Error('request too large'); chunks.push(chunk); }
      const raw = Buffer.concat(chunks).toString('utf8');
      const operation=process.argv[2],input=JSON.parse(raw);
      process.stdout.write(JSON.stringify({ ok: true, ...await (operation==='sequence' ? renderSequence(input) : operation==='waveform' ? waveform(input) : operation==='loudness' ? loudness(input) : renderScript(input)) }));
    }
  } catch (e) { process.stdout.write(JSON.stringify({ ok: false, message: e.message })); process.exitCode = 1; }
}
