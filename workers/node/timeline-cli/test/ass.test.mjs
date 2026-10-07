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
import { parseSrt, serializeAss, assText, wrapAssText } from '../adapters/ass.mjs';
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
