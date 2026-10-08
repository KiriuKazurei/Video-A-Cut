import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, writeFile, readdir, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { writeFileSync } from 'node:fs';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { parseSrt, serializeAss, serializeStyledAss, assText, wrapAssText } from '../adapters/ass.mjs';
import { canonicalJSON, defaultStyle } from '../../../../contracts/delivery-variant/v1/style.mjs';
import { buildVariantInputFromPackage, buildDeliveryVariant, loadVariantSpec, variantFingerprint } from '../src/delivery-variant.mjs';

const srt = '1\r\n00:00:00,005 --> 00:00:00,995\r\n中文, 😀 {\\an8} \\N \\h\r\n第二行  \r\n\r\n';
async function fixture(t, text = srt) {
  const root = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'vac-ass-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const pkg = join(root, 'parent'); await mkdir(pkg);
  const contents = {
    'edl.json': JSON.stringify({ timeline: { fps: 30 }, video: [{ in: 0, out: 1, timeline_in: 0 }] }),
    'delivery-manifest.json': JSON.stringify({ schema_version: 1, artifacts: [{ kind: 'edl', path: 'edl.json' }, { kind: 'subtitles', path: 'subtitles.srt' }, { kind: 'voice', path: 'voice.wav' }] }),
    'subtitles.srt': text, 'voice.wav': 'fixed audio passthrough fixture'
  };
  for (const [name, data] of Object.entries(contents)) await writeFile(join(pkg, name), data);
  return { root, pkg, output: join(root, 'variant'), contents, ...(await buildVariantInputFromPackage({ packageDir: pkg })) };
}
test('ASS preserves Unicode, comma, multiline, literal overrides and trailing spaces', async () => {
  const { spec } = await loadVariantSpec(); const cues = parseSrt(srt, 1000);
  assert.deepEqual(cues, [{ start: 1, end: 100, text: '中文, 😀 {\\an8} \\N \\h\n第二行  ' }]);
  const ass = serializeAss(cues, spec);
  assert.match(ass, /PlayResX: 1280/); assert.match(ass, /Microsoft YaHei,36/);
  assert.ok(ass.endsWith(`Dialogue: 0,0:00:00.01,0:00:01.00,Default,,0,0,0,,${assText(cues[0].text)}\n`));
  assert.equal(assText('{\\an8}\\N'), '\\{\\\u2060an8\\}\\\u2060N');
});
test('ASS rejects malformed, outside-duration, unsafe and collapsed centisecond cues', () => {
  for (const bad of ['2\n00:00:00,000 --> 00:00:00,100\nx', '1\n00:00:60,000 --> 00:00:61,000\nx', '1\n00:00:00,001 --> 00:00:00,004\nx', '1\n00:00:00,000 --> 00:00:02,000\nx', '1\n00:00:00,000 --> 00:00:00,100\na\u0000b']) assert.throws(() => parseSrt(bad, 1000));
  assert.deepEqual(parseSrt('', 1000), []);
});
test('ASS output retains every original file byte and fixed style bytes without changing parent', async t => {
  const f = await fixture(t); const result = await buildDeliveryVariant({ input: f.input, packageDir: f.pkg, outputDir: f.output });
  for (const [name, content] of Object.entries(f.contents)) {
    assert.deepEqual(await readFile(join(f.output, name)), Buffer.from(content));
    assert.deepEqual(await readFile(join(f.pkg, name)), Buffer.from(content));
  }
  const style = await loadVariantSpec(); assert.deepEqual(await readFile(join(f.output, 'ass-style.json')), style.bytes);
  assert.equal(result.artifacts.length, Object.keys(f.contents).length + 4);
  assert.equal(result.artifacts.find(a => a.key === 'srt').sha256, createHash('sha256').update(srt).digest('hex'));
  await assert.rejects(buildDeliveryVariant({ input: f.input, packageDir: f.pkg, outputDir: f.output }), /already exists/);
});
test('empty SRT creates a valid zero-event ASS and preserves its empty file', async t => {
  const f = await fixture(t, ''); await buildDeliveryVariant({ input: f.input, packageDir: f.pkg, outputDir: f.output });
  assert.equal((await readFile(join(f.output, 'subtitles.srt'))).length, 0);
  assert.ok(!(await readFile(join(f.output, 'subtitles.ass'), 'utf8')).includes('Dialogue:'));
});
test('ASS rejects changed parent, wrong spec and invalid UTF-8 before publication', async t => {
  const f = await fixture(t);
  await assert.rejects(buildDeliveryVariant({ input: { ...f.input, spec: { ...f.input.spec, font_size: 72 } }, packageDir: f.pkg, outputDir: f.output }), /style fingerprint/);
  await writeFile(join(f.pkg, 'subtitles.srt'), 'changed');
  await assert.rejects(buildDeliveryVariant({ input: f.input, packageDir: f.pkg, outputDir: f.output }), /parent input changed/);
  await writeFile(join(f.pkg, 'subtitles.srt'), Buffer.from([0xff]));
  const { input } = await buildVariantInputFromPackage({ packageDir: f.pkg });
  await assert.rejects(buildDeliveryVariant({ input, packageDir: f.pkg, outputDir: f.output }), /encoded data|encoding/i);
  assert.ok(!(await readdir(f.root)).includes('variant'));
});
test('ASS cancellation waits for copy cleanup and never publishes output', async t => {
  const f = await fixture(t); const controller = new AbortController();
  await assert.rejects(buildDeliveryVariant({ input: f.input, packageDir: f.pkg, outputDir: f.output, signal: controller.signal, onProgress: () => controller.abort(new Error('cancelled ASS')) }), /cancelled ASS/);
  assert.deepEqual((await readdir(f.root)).sort(), ['parent']);
});
test('variant fingerprint matches independent UTF-8 golden and excludes attempt IDs', async () => {
  const golden = JSON.parse(await readFile(new URL('../../../../contracts/delivery-variant/v1/fixtures/input.json', import.meta.url)));
  assert.equal(variantFingerprint(golden.input), golden.expected_sha256);
  assert.equal(variantFingerprint({ ...golden.input, variant_id: 'different', task_id: 'different', source_files: [...golden.input.source_files].reverse() }), golden.expected_sha256);
  assert.notEqual(variantFingerprint({ ...golden.input, revision_id: 'another' }), golden.expected_sha256);
  assert.throws(() => variantFingerprint({ ...golden.input, source_files: [...golden.input.source_files, { ...golden.input.source_files[0], path: golden.input.source_files[0].path.toUpperCase() }] }));
});
test('ASS detects parent changes after copying and cleans unpublished staging', async t => {
  const f = await fixture(t); let changed = false;
  await assert.rejects(buildDeliveryVariant({ input: f.input, packageDir: f.pkg, outputDir: f.output, onProgress: () => {
    if (!changed) { changed = true; writeFileSync(join(f.pkg, 'subtitles.srt'), 'changed while copying'); }
  } }), /parent changed/);
  assert.deepEqual((await readdir(f.root)).sort(), ['parent']);
});
test('ASS CLI produces a new bundle and rejects SRT absent from declared inputs', async t => {
  const f = await fixture(t);
  await promisify(execFile)(process.execPath, [fileURLToPath(new URL('../src/cli.mjs', import.meta.url)), 'build-ass', '--package', f.pkg, '--output', f.output]);
  assert.deepEqual(await readFile(join(f.output, 'subtitles.srt')), Buffer.from(srt));
  await writeFile(join(f.pkg, 'delivery-manifest.json'), '{"schema_version":1,"artifacts":[]}');
  await assert.rejects(buildVariantInputFromPackage({ packageDir: f.pkg }), /must declare exported SRT/);
});
test('ASS fixed wrapping preserves all characters and explicit newlines within layout budget', async () => {
  const long='长句中文字幕'.repeat(12); const wrapped=wrapAssText(long,56);
  assert.equal(wrapped.replaceAll('\n',''),long);
  for(const line of wrapped.split('\n')) assert([...line].length<=28);
  assert.equal(wrapAssText('a'.repeat(28)+'\u0301\n中文',56),'a'.repeat(28)+'\u0301\n中文');
  assert([...wrapAssText('W'.repeat(56),56).split('\n')].every(line=>line.length<=28));
  const { spec } = await loadVariantSpec();
  assert.throws(() => serializeAss([{start:0,end:100,text:'长'.repeat(28*9)}],spec),/line limit/);
});

async function makeEditable(f, overrides = [], changes = {}) {
  const {spec,sha256}=await loadVariantSpec('ass-basic-v2');
  const document={source_srt_sha256:f.input.source_files.find(file=>file.kind==='srt').sha256,
    play_res_x:1280,play_res_y:720,style:{...defaultStyle(),...changes},overrides};
  const input={...f.input,schema_version:2,spec_id:spec.spec_id,spec_sha256:sha256,spec,style_document:document,
    style_sha256:createHash('sha256').update(canonicalJSON(document)).digest('hex')};
  input.input_sha256=variantFingerprint(input);return input;
}

test('editable ASS writes colors, flags, fractional borders, fades and all nine alignments', () => {
  const cues=Array.from({length:9},(_,i)=>({start:i*100,end:(i+1)*100,text:'字 {\\an1}\\N\n第二行'}));
  const document={source_srt_sha256:'a'.repeat(64),play_res_x:1280,play_res_y:720,
    style:{...defaultStyle(),font_family:'示例 Serif',font_size:48,bold:true,italic:true,underline:true,
      outline:0,shadow:1.25,primary_color:'#12abEF',outline_color:'#987654',margin_l:10,margin_r:20,margin_v:30,fade_in_ms:100,fade_out_ms:200},
    overrides:cues.map((_,i)=>({cue_id:i+1,style:{alignment:i+1}}))};
  const ass=serializeStyledAss(cues,document,9000);
  for(let i=1;i<=9;i++) assert(ass.includes(`Style: Cue_${i},示例 Serif,48,&H00EFAB12,&H00EFAB12,&H00547698,&H80000000,-1,-1,-1,0,100,100,0,0,1,0,1.25,${i},10,20,30,1\n`));
  assert.equal((ass.match(/\\fad\(100,200\)/g)||[]).length,9);
  assert(ass.includes('\\{\\\u2060an1\\}\\\u2060N\\N第二行'));
  document.style.fade_in_ms=0;document.style.fade_out_ms=0;
  assert(!serializeStyledAss(cues,document,9000).includes('\\fad'));
});

test('editable ASS inherits fields, applies millisecond time overrides and scales wrapping', () => {
  const text='长'.repeat(100)+'\n显式换行';
  const cues=[{start:0,end:100,text}];
  const document={source_srt_sha256:'a'.repeat(64),play_res_x:720,play_res_y:720,style:defaultStyle(),
    overrides:[{cue_id:1,start_ms:15,end_ms:995,style:{font_size:120,primary_color:'#FF0000'}}]};
  const ass=serializeStyledAss(cues,document,1000);
  assert(ass.includes('0:00:00.02,0:00:01.00,Cue_1'));
  assert(ass.includes(',120,&H000000FF,&H000000FF,&H00000000,'));
  const last=ass.trimEnd().split('\n').at(-1).split(',,0,0,0,,')[1].split('\\N');
  assert.equal(last.join(''),text.replaceAll('\n',''));assert(last.slice(0,-1).every(line=>[...line].length<=4));
  document.overrides[0].end_ms=15;assert.throws(()=>serializeStyledAss(cues,document,1000),/时间/);
  document.overrides[0].end_ms=25;document.style.fade_in_ms=11;
  assert.throws(()=>serializeStyledAss(cues,document,1000),/渐入渐出/);
  document.style.fade_in_ms=10;serializeStyledAss(cues,document,1000);
});

test('editable bundle freezes canonical style, timing and original file bytes', async t => {
  const f=await fixture(t);const input=await makeEditable(f,[{cue_id:1,start_ms:20,end_ms:980,style:{primary_color:'#ABCDEF',alignment:8}}],{bold:true,fade_in_ms:50});
  const manifest=await buildDeliveryVariant({input,packageDir:f.pkg,outputDir:f.output});
  assert.equal(manifest.schema_version,2);
  assert.equal(await readFile(join(f.output,'ass-style.json'),'utf8'),canonicalJSON(input.style_document));
  assert.equal(JSON.parse(await readFile(join(f.output,'variant-receipt.json'))).converter_version,'vac.ass-basic-v2/1');
  const ass=await readFile(join(f.output,'subtitles.ass'),'utf8');assert(ass.includes('0:00:00.02,0:00:00.98'));assert(ass.includes('&H00EFCDAB'));assert(ass.includes('\\fad(50,0)'));
  for(const [path,bytes] of Object.entries(f.contents)) assert.deepEqual(await readFile(join(f.output,path)),Buffer.from(bytes));
  const previous=input.input_sha256;
  input.style_document.style.font_size=72;
  assert.throws(()=>variantFingerprint(input),/style snapshot/);
  input.style_sha256=createHash('sha256').update(canonicalJSON(input.style_document)).digest('hex');
  assert.notEqual(variantFingerprint(input),previous);
});

test('editable inputs reject a forged source hash and v1 style injection before publication', async t => {
  const f=await fixture(t);const input=await makeEditable(f);
  input.style_document.source_srt_sha256='0'.repeat(64);input.style_sha256=createHash('sha256').update(canonicalJSON(input.style_document)).digest('hex');
  await assert.rejects(buildDeliveryVariant({input,packageDir:f.pkg,outputDir:f.output}),/source SRT/);
  assert.throws(()=>variantFingerprint({...f.input,style_document:input.style_document}),/fixed ASS/);
  assert.deepEqual(await readdir(f.root),['parent']);
});

test('editable ASS supports long source text beyond the v1 limit and rejects empty input', async t => {
  const long=await fixture(t,'1\n00:00:00,000 --> 00:00:01,000\n'+'中'.repeat(400)+'\n');
  await buildDeliveryVariant({input:await makeEditable(long),packageDir:long.pkg,outputDir:long.output});
  assert((await readFile(join(long.output,'subtitles.ass'),'utf8')).includes('Dialogue:'));
  const empty=await fixture(t,'');
  await assert.rejects(buildDeliveryVariant({input:await makeEditable(empty),packageDir:empty.pkg,outputDir:empty.output}),/nonempty/);
});

test('editable ASS cancellation cleans unpublished output and retry succeeds', async t => {
  const f=await fixture(t),input=await makeEditable(f),controller=new AbortController();
  await assert.rejects(buildDeliveryVariant({input,packageDir:f.pkg,outputDir:f.output,signal:controller.signal,
    onProgress:()=>controller.abort(new Error('cancelled editable ASS'))}),/cancelled editable/);
  assert.deepEqual(await readdir(f.root),['parent']);
  await buildDeliveryVariant({input,packageDir:f.pkg,outputDir:f.output});
  assert.equal(JSON.parse(await readFile(join(f.output,'variant-manifest.json'))).input_sha256,input.input_sha256);
});
