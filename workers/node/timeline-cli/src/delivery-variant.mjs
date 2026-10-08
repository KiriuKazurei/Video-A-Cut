import { createHash, randomUUID } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { pipeline } from 'node:stream/promises';
import { access, mkdir, mkdtemp, realpath, rm, stat, writeFile } from 'node:fs/promises';
import { basename, dirname, join, resolve } from 'node:path';
import { isDeepStrictEqual } from 'node:util';
import { packageFile, hashFile, readPackageJSON, verifyOutputParent } from './preview.mjs';
import { isPreviewRelativePath } from './preview-protocol.mjs';
import { publishNewDirectory } from './publish.mjs';
import { parseSrt, serializeAss, serializeStyledAss } from '../adapters/ass.mjs';
import { canonicalJSON } from '../../../../contracts/delivery-variant/v1/style.mjs';
export const VARIANT_SPEC_ID = 'ass-basic-v1';
const specURL = new URL('../../../../contracts/delivery-variant/v1/specs/ass-basic-v1.json', import.meta.url);
export const EDITABLE_VARIANT_SPEC_ID = 'ass-basic-v2';
export async function loadVariantSpec(id = VARIANT_SPEC_ID) {
  if (![VARIANT_SPEC_ID, EDITABLE_VARIANT_SPEC_ID].includes(id)) throw new Error('unsupported ASS spec');
  const bytes = await readPackageJSON(id === VARIANT_SPEC_ID ? specURL : new URL('../../../../contracts/delivery-variant/v1/specs/ass-basic-v2.json', import.meta.url));
  return { bytes, spec: JSON.parse(bytes), sha256: createHash('sha256').update(bytes).digest('hex') };
}
export function variantFingerprint(input) {
  const hash = /^[0-9a-f]{64}$/;
  if (!((input.schema_version === 1 && input.spec_id === VARIANT_SPEC_ID) || (input.schema_version === 2 && input.spec_id === EDITABLE_VARIANT_SPEC_ID)) || input.format !== 'ass' || !input.asset_id || !input.run_id || !input.revision_id || !isPreviewRelativePath(input.parent_package_ref) || !Array.isArray(input.source_files) || !input.source_files.length) throw new Error('invalid variant input');
  if (input.schema_version === 1 && (input.style_document !== undefined || input.style_sha256 !== undefined)) throw new Error('fixed ASS input cannot carry an editable style');
  if (![input.edl_sha256, input.parent_manifest_sha256, input.spec_sha256].every(h => typeof h === 'string' && hash.test(h))) throw new Error('invalid variant hash');
  const atoms = ['vac.delivery-variant.input.v1', '1', input.asset_id, input.run_id, input.revision_id, input.parent_package_ref, input.edl_sha256, input.parent_manifest_sha256, input.format, input.spec_id, input.spec_sha256, String(input.source_files.length)];
  const seen = new Set();
  for (const f of [...input.source_files].sort((a, b) => Buffer.compare(Buffer.from(a.path), Buffer.from(b.path)))) {
    const alias = f.path.toLowerCase();
    if (!isPreviewRelativePath(f.path) || seen.has(alias) || typeof f.kind !== 'string' || !f.kind || !hash.test(f.sha256) || !Number.isSafeInteger(f.size) || f.size < 0) throw new Error('invalid variant file');
    seen.add(alias); atoms.push(f.kind, f.path, f.sha256, String(f.size));
  }
  if (input.schema_version === 2) {
    if (!input.style_document || !hash.test(input.style_sha256) || createHash('sha256').update(canonicalJSON(input.style_document)).digest('hex') !== input.style_sha256) throw new Error('invalid style snapshot');
    const srts = input.source_files.filter(file => file.kind === 'srt');
    if (srts.length !== 1 || input.style_document.source_srt_sha256 !== srts[0].sha256) throw new Error('style source SRT fingerprint mismatch');
    atoms.push(EDITABLE_VARIANT_SPEC_ID, input.style_sha256);
  }
  const digest = createHash('sha256');
  for (const atom of atoms) { digest.update(`${Buffer.byteLength(atom)}:`); digest.update(atom); }
  return digest.digest('hex');
}
export function timelineDuration(edl) {
  const fps = edl.timeline?.fps;
  if (!Number.isSafeInteger(fps) || fps <= 0 || !edl.video?.length) throw new Error('invalid timeline');
  const frames = Math.max(...edl.video.map(v => {
    const n = Math.round(v.out * fps) - Math.round(v.in * fps), end = Math.round(v.timeline_in * fps) + n;
    if (!Number.isFinite(v.in) || v.in < 0 || !Number.isFinite(v.timeline_in) || v.timeline_in < 0 || n <= 0 || !Number.isSafeInteger(end)) throw new Error('invalid video span');
    return end;
  }));
  const ms = Math.round(frames * 1000 / fps);
  if (!Number.isSafeInteger(ms) || ms <= 0) throw new Error('invalid timeline duration');
  return ms;
}
const mime = path => path.endsWith('.json') ? 'application/json' : path.endsWith('.srt') ? 'application/x-subrip' : path.endsWith('.ass') ? 'text/x-ssa; charset=utf-8' : path.endsWith('.mp4') ? 'video/mp4' : path.endsWith('.wav') ? 'audio/wav' : 'application/octet-stream';
const reserved = new Set(['subtitles.ass', 'ass-style.json', 'variant-manifest.json', 'variant-report.json', 'variant-receipt.json']);
export async function buildVariantInputFromPackage({ packageDir }) {
  const root = await realpath(resolve(packageDir));
  const manifest = JSON.parse(await readPackageJSON(await packageFile(root, 'delivery-manifest.json')));
  const paths = new Map([['edl.json', 'edl'], ['delivery-manifest.json', 'delivery_manifest']]);
  for (const artifact of manifest.artifacts || []) {
    if (!isPreviewRelativePath(artifact.path) || paths.has(artifact.path) && paths.get(artifact.path) !== artifact.kind && artifact.path !== 'edl.json') throw new Error('invalid parent manifest');
    paths.set(artifact.path, artifact.kind === 'subtitles' ? 'srt' : artifact.kind);
  }
  if (![...paths.values()].includes('srt')) throw new Error('parent manifest must declare exported SRT');
  const source_files = [];
  for (const [path, kind] of paths) { const absolute = await packageFile(root, path); source_files.push({ path, kind, size: (await stat(absolute)).size, sha256: await hashFile(absolute) }); }
  const { spec, sha256 } = await loadVariantSpec();
  const input = { schema_version: 1, variant_id: `dv_${randomUUID()}`, task_id: `tkdv_${randomUUID()}`, asset_id: 'cli', run_id: 'cli', revision_id: 'cli', parent_package_ref: 'cli', edl_sha256: source_files.find(f => f.kind === 'edl').sha256, parent_manifest_sha256: source_files.find(f => f.kind === 'delivery_manifest').sha256, format: 'ass', spec_id: VARIANT_SPEC_ID, spec_sha256: sha256, spec, source_files };
  input.input_sha256 = variantFingerprint(input);
  return { input };
}
export async function buildDeliveryVariant({ input, packageDir, outputDir, signal, onProgress }) {
  signal?.throwIfAborted();
  if (variantFingerprint(input) !== input.input_sha256) throw new Error('variant input fingerprint mismatch');
  const registered = await loadVariantSpec(input.spec_id);
  if (registered.sha256 !== input.spec_sha256 || !isDeepStrictEqual(registered.spec, input.spec)) throw new Error('variant style fingerprint mismatch');
  const root = resolve(packageDir);
  if ((await realpath(root)).toLowerCase() !== root.toLowerCase()) throw new Error('redirected parent package');
  const output = resolve(outputDir);
  await verifyOutputParent(output);
  try { await access(output); throw Object.assign(new Error('output already exists'), { code: 'EEXIST' }); } catch (error) { if (error.code !== 'ENOENT') throw error; }
  const frozen = new Map();
  for (const f of input.source_files) {
    signal?.throwIfAborted();
    if (reserved.has(f.path.toLowerCase())) throw new Error('parent file collides with variant output');
    const absolute = await packageFile(root, f.path);
    if ((await stat(absolute)).size !== f.size || await hashFile(absolute, signal) !== f.sha256) throw new Error('parent input changed');
    frozen.set(f.path, { ...f, absolute });
  }
  const edlFile = frozen.get('edl.json'), manifestFile = frozen.get('delivery-manifest.json');
  const srts = [...frozen.values()].filter(f => f.kind === 'srt');
  if (!edlFile || edlFile.sha256 !== input.edl_sha256 || !manifestFile || manifestFile.sha256 !== input.parent_manifest_sha256 || srts.length !== 1) throw new Error('missing or mismatched EDL/manifest/SRT');
  const edlBytes = await readPackageJSON(edlFile.absolute), srtBytes = await readPackageJSON(srts[0].absolute);
  if (createHash('sha256').update(edlBytes).digest('hex') !== edlFile.sha256 || createHash('sha256').update(srtBytes).digest('hex') !== srts[0].sha256) throw new Error('parent input changed before subtitle parsing');
  const durationMs = timelineDuration(JSON.parse(edlBytes));
  const cues = parseSrt(new TextDecoder('utf-8', { fatal: true }).decode(srtBytes), durationMs);
  const editable = input.spec_id === EDITABLE_VARIANT_SPEC_ID;
  if (editable && !cues.length) throw new Error('editable ASS requires nonempty source subtitles');
  const ass = editable ? serializeStyledAss(cues, input.style_document, durationMs) : serializeAss(cues, input.spec);
  await mkdir(dirname(output), { recursive: true }); await verifyOutputParent(output);
  const staging = await mkdtemp(join(dirname(output), `.${basename(output)}-staging-`));
  try {
    const artifacts = [];
    const entry = async (key, kind, path) => { const absolute = await packageFile(staging, path); artifacts.push({ key, kind, path, mime: mime(path), size: (await stat(absolute)).size, sha256: await hashFile(absolute, signal) }); };
    let n = 0;
    for (const f of frozen.values()) {
      signal?.throwIfAborted();
      const destination = join(staging, ...f.path.split('/')); await mkdir(dirname(destination), { recursive: true });
      await pipeline(createReadStream(f.absolute), createWriteStream(destination, { flags: 'wx' }), { signal });
      if (await hashFile(destination, signal) !== f.sha256) throw new Error('parent changed during copying');
      await entry(f.kind === 'srt' ? 'srt' : `source_${n}`, f.kind, f.path); n++;
      onProgress?.({ phase: 'copying', progress: 0.1 + 0.65 * n / frozen.size });
    }
    await writeFile(join(staging, 'subtitles.ass'), ass, 'utf8'); await entry('ass', 'ass', 'subtitles.ass');
    await writeFile(join(staging, 'ass-style.json'), editable ? canonicalJSON(input.style_document) : registered.bytes); await entry('style', 'ass_style', 'ass-style.json');
    const binding = { schema_version: input.schema_version, variant_id: input.variant_id, task_id: input.task_id, asset_id: input.asset_id, run_id: input.run_id, revision_id: input.revision_id, input_sha256: input.input_sha256, spec_sha256: input.spec_sha256 };
    const checks = ['source_fingerprint', 'subtitle_text', 'subtitle_timing', 'ass_style', 'self_contained'].map(code => ({ code, status: 'passed', message: code === 'subtitle_timing' ? 'nearest centisecond; at most 5ms quantization' : 'verified' }));
    await writeFile(join(staging, 'variant-report.json'), JSON.stringify({ ...binding, subtitle_count: cues.length, duration_centiseconds: Math.floor((durationMs + 5) / 10), checks }, null, 2) + '\n'); await entry('report', 'variant_report', 'variant-report.json');
    await writeFile(join(staging, 'variant-receipt.json'), JSON.stringify({ ...binding, converter_version: `vac.${input.spec_id}/1` }, null, 2) + '\n'); await entry('receipt', 'variant_receipt', 'variant-receipt.json');
    const manifest = { ...binding, artifacts };
    const manifestJSON = JSON.stringify(manifest, null, 2) + '\n';
    if (Buffer.byteLength(manifestJSON) > 1024 * 1024) throw new Error('variant manifest exceeds 1 MiB');
    await writeFile(join(staging, 'variant-manifest.json'), manifestJSON);
    // Detect source changes after the copy, before immutable publication.
    for (const f of frozen.values()) if (await hashFile(f.absolute, signal) !== f.sha256) throw new Error('parent changed before publication');
    signal?.throwIfAborted(); await verifyOutputParent(output); await publishNewDirectory(staging, output);
    return manifest;
  } catch (error) { await rm(staging, { recursive: true, force: true }); signal?.throwIfAborted(); throw error; }
}
