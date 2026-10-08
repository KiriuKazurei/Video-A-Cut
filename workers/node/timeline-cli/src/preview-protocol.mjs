import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';

export const PREVIEW_PROTOCOL_VERSION = 1;
export const PREVIEW_PRESET_ID = 'mp4-720p-v1';
export const PREVIEW_FILENAMES = Object.freeze({
  media: 'final.mp4', manifest: 'preview-manifest.json',
  report: 'render-report.json', receipt: 'preview-receipt.json'
});
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex');
const inputKinds = new Set(['edl', 'delivery_manifest', 'source_video', 'packaged_source', 'game_audio', 'voice', 'music', 'srt']);
const sha = /^[0-9a-f]{64}$/;

/** This is wire/path validation only; actual containment, hashes and media require implementation. */
export function isPreviewRelativePath(value) {
  return typeof value === 'string' && value.length > 0 && !/[\\:\x00-\x1f\x7f]/u.test(value)
    && value.split('/').every((part) => part !== '' && part !== '.' && part !== '..');
}

/** Hash length-prefixed UTF-8 atoms; independent of JSON serialization and key order. */
export function fingerprintPreviewInput(input) {
  if (![1,2].includes(input?.schema_version) || (input.schema_version === 2) !== Boolean(input.subtitle_variant) || input.preset_id !== PREVIEW_PRESET_ID) throw new Error('unsupported preview protocol or preset');
  const atoms = ['vac.mp4-preview.input.v1'];
  for (const key of ['asset_id', 'run_id', 'revision_id']) {
    if (typeof input[key] !== 'string' || !input[key]) throw new Error(`missing ${key}`);
    atoms.push(input[key]);
  }
  if (!isPreviewRelativePath(input.package_ref)) throw new Error('invalid package_ref');
  atoms.push(input.package_ref);
  for (const key of ['edl_sha256', 'manifest_sha256', 'preset_sha256']) {
    if (!sha.test(input[key] ?? '')) throw new Error(`invalid ${key}`);
    atoms.push(input[key]);
  }
  atoms.push(input.preset_id);
  if (!Array.isArray(input.files) || input.files.length === 0) throw new Error('preview input files required');
  const files = [...input.files].sort((a, b) => Buffer.compare(Buffer.from(a.path), Buffer.from(b.path)));
  atoms.push(String(files.length));
  const paths = new Set();
  for (const file of files) {
    if (!isPreviewRelativePath(file.path) || paths.has(file.path) || !inputKinds.has(file.kind)
      || !sha.test(file.sha256 ?? '') || !Number.isSafeInteger(file.size) || file.size < 0) throw new Error('invalid or duplicate preview input file');
    paths.add(file.path);
    atoms.push(file.kind, file.path, file.sha256, String(file.size));
  }
  if (input.subtitle_variant) {
 const sub=input.subtitle_variant, f=sub.ass;
 if (!isPreviewRelativePath(sub.variant_id)||sub.variant_id.includes('/')||!isPreviewRelativePath(sub.package_ref)||f?.kind!=='ass'||!isPreviewRelativePath(f.path)||!sha.test(f.sha256)||!Number.isSafeInteger(f.size)||f.size<=0||f.size>1024*1024||!sha.test(sub.style_sha256)||!Array.isArray(sub.font_families)||!sub.font_families.length||sub.font_families.some(f=>typeof f!=='string'||!f)||new Set(sub.font_families).size!==sub.font_families.length) throw new Error('invalid subtitle variant snapshot');
 atoms.push('subtitle-variant-v1',sub.variant_id,sub.package_ref,f.kind,f.path,f.sha256,String(f.size),sub.style_sha256,String(sub.font_families.length),...sub.font_families);
 }
 const hash = createHash('sha256');
  for (const atom of atoms) {
    const bytes = Buffer.from(atom, 'utf8');
    hash.update(`${bytes.length}:`, 'ascii');
    hash.update(bytes);
  }
  return hash.digest('hex');
}

/** The preset SHA-256 covers exact file bytes, including its final LF. */
export async function loadPreviewPreset(id = PREVIEW_PRESET_ID) {
  if (id !== PREVIEW_PRESET_ID) throw new Error(`unsupported preview preset: ${id}`);
  const bytes = await readFile(new URL('../../../../contracts/mp4-preview/v1/presets/mp4-720p-v1.json', import.meta.url));
  return { preset: JSON.parse(bytes.toString('utf8')), sha256: digest(bytes) };
}
