import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { renderScript, scriptFonts } from '../src/script-workbench.mjs';
import { runTool, probeMedia } from '../src/media.mjs';

test('real script render bakes Chinese subtitles, black/white dips and cross dissolve into Premiere media', { timeout: 180000 }, async () => {
  assert.ok(process.env.VAC_TEST_OUTPUT, 'run through TestWorkspace.ps1');
  const root = join(process.env.VAC_TEST_OUTPUT, 'script-workbench-20261018', 'render');
  await mkdir(root, { recursive: true });
  const red = join(root, 'red.mp4'), blue = join(root, 'blue.mp4');
  for (const [path, color] of [[red,'red'],[blue,'blue']]) await runTool('ffmpeg', ['-v','error','-y','-f','lavfi','-i',`color=c=${color}:s=320x180:r=30:d=1`,'-c:v','libx264','-preset','ultrafast','-threads','1','-pix_fmt','yuv420p',path], { timeoutMs: 15000 });
  const fonts = await scriptFonts();
  const font = ['Microsoft YaHei','Microsoft YaHei UI','Noto Sans CJK SC'].find(f => fonts.includes(f));
  assert.ok(font, 'Chinese system font required for real acceptance');
  const segments = [red, blue, red, blue].map(path => ({ path, start: 0, end: 1, narration: '实际旁白文稿' }));
  const document = { version: 1, paragraphs: [{ effect: '黑屏', subtitle: { text: '抽卡机制 {文字}', size_px: 40, font } }, { effect: '白屏' }, {}, { effect: '交叉溶解' }] };
  const output = join(root, 'delivery');
  const result = await renderScript({ segments, document, subtitles_enabled: false, output_dir: output, sources: [] });
  assert.equal(result.width, 1280); assert.equal(result.height, 720);
  assert.ok(result.frames >= 109 && result.frames <= 112, `frames: ${result.frames}`);
  const final = await readFile(join(output,'final.mp4'));
  const xml = await readFile(join(output,'edit.xml'),'utf8');
  assert.match(xml, /<xmeml version="4">/);
  const files = [...xml.matchAll(/<pathurl>([^<]+\.mp4)<\/pathurl>/g)]; assert.ok(files.length);
  const copiedName = files[0][1].split('/').at(-1);
  const copied = await readFile(join(output,'media',decodeURIComponent(copiedName)));
  assert.equal(createHash('sha256').update(copied).digest('hex'), result.sha256);
  assert.equal(createHash('sha256').update(final).digest('hex'), result.sha256);
  const ass = await readFile(join(output,'subtitles.ass'),'utf8');
  assert.match(ass, /Style: P0,[^,]+,40,/); assert.match(ass, /抽卡机制/); assert.equal((ass.match(/Dialogue:/g)||[]).length,1);
  assert.ok(result.fonts.some(f => f.family === font && f.sha256.length === 64));
  // A cropped pixel average proves effects exist in the encoded file, independently of filter arguments.
  const stats = async at => {
    const raw = await runTool('ffmpeg', ['-v','error','-ss',String(at),'-i',join(output,'final.mp4'),'-frames:v','1','-vf','crop=16:16:0:0,signalstats,metadata=print:file=-','-f','null','-']);
    return Object.fromEntries([...raw.matchAll(/lavfi.signalstats.([YUV])AVG=([\d.]+)/g)].slice(0,3).map(m => [m[1],Number(m[2])]));
  };
  assert.ok((await stats(0)).Y < 25, 'first frame is black');
  assert.ok((await stats(1)).Y > 220, 'second paragraph starts white');
  const mix = await stats(2.83);
  assert.ok(mix.U > 110 && mix.U < 220 && mix.V > 130 && mix.V < 220, `dissolve mixes red and blue: ${JSON.stringify(mix)}`);
  const mid = join(root, 'dissolve.png');
  await runTool('ffmpeg',['-v','error','-y','-ss','2.83','-i',join(output,'final.mp4'),'-frames:v','1',mid]);
  await runTool('ffmpeg',['-v','error','-y','-ss','0.5','-i',join(output,'final.mp4'),'-frames:v','1',join(root,'subtitle.png')]);
  const probe = await probeMedia(join(output,'final.mp4')); assert.ok(probe.audio); assert.equal(probe.video.frames,result.frames);
  await writeFile(join(root,'verification.json'),JSON.stringify({ ...result, black: await stats(0), white: await stats(1), dissolve: mix },null,2));
});
