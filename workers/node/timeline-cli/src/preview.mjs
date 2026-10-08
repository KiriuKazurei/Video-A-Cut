import { createHash, randomUUID } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createReadStream } from 'node:fs';
import { access, copyFile, lstat, mkdir, mkdtemp, readFile, realpath, rm, stat, writeFile } from 'node:fs/promises';
import { basename, dirname, extname, isAbsolute, join, relative, resolve } from 'node:path';
import { validateEdl } from './edl.mjs';
import { probeMedia, runTool } from './media.mjs';
import {
  fingerprintPreviewInput,
  isPreviewRelativePath,
  loadPreviewPreset,
  PREVIEW_FILENAMES,
  PREVIEW_PRESET_ID
} from './preview-protocol.mjs';
import { publishNewDirectory } from './publish.mjs';
import { resolveFonts as defaultResolveFonts } from './fonts.mjs';
import { toUnits } from './time.mjs';
import { isDeepStrictEqual } from 'node:util';
import { audioIntervalFromStream, validateAudioInterval } from './preview-audio.mjs';

/** @typedef {import('../../../../contracts/mp4-preview/v1/contracts').PreviewInput} PreviewInput */
/** @typedef {import('../../../../contracts/mp4-preview/v1/contracts').PreviewPhase} PreviewPhase */
/** @typedef {import('../../../../contracts/mp4-preview/v1/contracts').PreviewManifest} PreviewManifest */
/** @typedef {import('../../../../contracts/mp4-preview/v1/contracts').PreviewMedia} PreviewMedia */
/** @typedef {import('../../../../contracts/mp4-preview/v1/contracts').PreviewReport} PreviewReport */
/** @typedef {import('../../../../contracts/mp4-preview/v1/contracts').PreviewReceipt} PreviewReceipt */

export class PreviewError extends Error {
  constructor(message, code = 'preview_error') {
    super(message);
    this.name = 'PreviewError';
    this.code = code;
  }
}

export async function packageFile(packageDir, relPath) {
  if (!isPreviewRelativePath(relPath)) throw new PreviewError(`unsafe package path: ${relPath}`, 'invalid_file_path');
  const abs = resolve(packageDir, relPath);
  const real = await realpath(abs);
  const within = relative(packageDir, real);
  if (!within || within.startsWith('..') || isAbsolute(within) || real.toLowerCase() !== abs.toLowerCase()) {
    throw new PreviewError(`package file escapes or redirects its path: ${relPath}`, 'file_escapes_package');
  }
  let cursor = packageDir;
  for (const part of relPath.split('/')) {
    cursor = join(cursor, part);
    if ((await lstat(cursor)).isSymbolicLink()) throw new PreviewError(`linked package file: ${relPath}`, 'file_escapes_package');
  }
  if (!(await stat(real)).isFile()) throw new PreviewError(`irregular package file: ${relPath}`, 'file_not_found');
  return real;
}

export async function verifyOutputParent(outputDir) {
  let cursor = dirname(outputDir);
  for (;;) {
    try {
      const info = await lstat(cursor);
      if (info.isSymbolicLink() || !info.isDirectory()) {
        throw new PreviewError(`output parent contains a redirected or invalid component: ${cursor}`, 'invalid_output_dir');
      }
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    const parent = dirname(cursor);
    if (parent === cursor) return;
    cursor = parent;
  }
}

export async function hashFile(filePath, signal) {
  const hash = createHash('sha256');
  try {
    for await (const chunk of createReadStream(filePath, { signal })) hash.update(chunk);
  } catch (error) {
    // Streams wrap cancellation in AbortError; preserve the caller's reason.
    signal?.throwIfAborted();
    throw error;
  }
  return hash.digest('hex');
}

export async function readPackageJSON(filePath) {
  if ((await stat(filePath)).size > 1024 * 1024) throw new PreviewError('package JSON exceeds 1 MiB', 'invalid_input');
  return readFile(filePath);
}

export class PreviewNotImplementedError extends PreviewError {
  constructor() {
    super('MP4 preview renderer is not implemented; implement Agent A in the framework handoff.', 'preview_not_implemented');
    this.name = 'PreviewNotImplementedError';
  }
}

function rate(value) {
  const [num, den = '1'] = String(value || '0/1').split('/').map(Number);
  return den ? num / den : 0;
}

/**
 * Locate font file on system to record font sha256 in PreviewReceipt.
 * Supports Windows Fonts, environment override PREVIEW_FONT_PATH, and Linux fallbacks.
 */
export async function findFontFile(family = 'Microsoft YaHei') {
  if (family === 'Microsoft YaHei') {
    const windir = process.env.WINDIR || process.env.SYSTEMROOT || 'C:\\Windows';
    const candidates = [
      process.env.PREVIEW_FONT_PATH,
      join(windir, 'Fonts', 'msyh.ttc'),
      join(windir, 'Fonts', 'msyh.ttf'),
      join(windir, 'Fonts', 'msyhl.ttc'),
      join(windir, 'Fonts', 'msyhbd.ttc'),
      process.env.LOCALAPPDATA ? join(process.env.LOCALAPPDATA, 'Microsoft', 'Windows', 'Fonts', 'msyh.ttc') : null,
      process.env.LOCALAPPDATA ? join(process.env.LOCALAPPDATA, 'Microsoft', 'Windows', 'Fonts', 'msyh.ttf') : null,
      '/usr/share/fonts/truetype/msyh.ttc',
      '/usr/share/fonts/msyh.ttc'
    ].filter(Boolean);

    for (const candidate of candidates) {
      try {
        await access(candidate);
        const bytes = await readFile(candidate);
        const sha256 = createHash('sha256').update(bytes).digest('hex');
        return { path: candidate, sha256, family };
      } catch (_) {}
    }
  }
  return null;
}

/**
 * Query FFmpeg version string for receipt metadata.
 */
export async function getFfmpegVersion(options) {
  const raw = await runTool('ffmpeg', ['-version'], { timeoutMs: 10_000, ...options });
  const firstLine = (raw.split('\n')[0] || '').trim();
  const match = firstLine.match(/ffmpeg version\s+(\S+)/);
  return match ? match[1] : firstLine;
}

/**
 * Probe rendered MP4 for validation and PreviewReport media metadata.
 */
export async function probePreviewMedia(filePath, options) {
  const raw = await runTool('ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', filePath], { timeoutMs: 30_000, ...options });
  const data = JSON.parse(raw);
  const video = data.streams?.find((s) => s.codec_type === 'video');
  const audio = data.streams?.find((s) => s.codec_type === 'audio');
  const duration = Number(data.format?.duration);
  if (!video) throw new PreviewError('rendered output has no video stream', 'missing_video_stream');
  if (!audio) throw new PreviewError('rendered output has no audio stream', 'missing_audio_stream');
  const fps = rate(video.avg_frame_rate) || rate(video.r_frame_rate);
  const videoDuration = Number(video.duration) || duration;
  if (!Number.isFinite(fps) || fps <= 0 || !Number.isFinite(videoDuration) || videoDuration <= 0) {
    throw new PreviewError('rendered output has invalid video timing', 'invalid_media_timing');
  }
  let frames = Number(video.nb_frames);
  if (!Number.isFinite(frames) || frames <= 0) {
    frames = Math.round((Number(video.duration) || duration) * fps);
  }
  return {
    width: Number(video.width),
    height: Number(video.height),
    fps,
    frame_count: frames,
    duration_seconds: videoDuration,
    video_codec: video.codec_name,
    audio_codec: audio.codec_name,
    sample_rate: Number(audio.sample_rate),
    channels: Number(audio.channels),
    audio_interval: audioIntervalFromStream(audio)
  };
}

async function loadFrozenAss(input, subtitlePackageDir) {
  if (typeof subtitlePackageDir !== 'string' || !subtitlePackageDir.trim()) {
    throw new PreviewError('subtitlePackageDir string is required', 'package_not_found');
  }
  const root = resolve(subtitlePackageDir);
  try {
    const info = await stat(root);
    if (!info.isDirectory()) throw new Error('not a directory');
    if ((await realpath(root)).toLowerCase() !== root.toLowerCase()) throw new Error('redirected');
  } catch (error) {
    if (error instanceof PreviewError) throw error;
    throw new PreviewError('subtitle package directory is missing or redirected', 'package_not_found');
  }
  const file = input.subtitle_variant.ass;
  const absPath = await packageFile(root, file.path);
  const info = await stat(absPath);
  if (!info.isFile() || info.size !== file.size) {
    throw new PreviewError(`ASS size mismatch for ${file.path}`, 'size_mismatch');
  }
  if (await hashFile(absPath) !== file.sha256) {
    throw new PreviewError(`ASS hash mismatch for ${file.path}`, 'hash_mismatch');
  }
  return absPath;
}

async function resolveAssFonts(families, resolveFontsImpl) {
  let resolved;
  try {
    resolved = await resolveFontsImpl(families);
  } catch (error) {
    throw new PreviewError(error instanceof Error ? error.message : '字体不可用', 'font_not_found');
  }
  const files = new Map();
  for (const font of resolved) {
    const current = files.get(font.path);
    if (current && current.sha256 !== font.sha256) throw new PreviewError(`字体文件哈希冲突：${font.family}`, 'font_hash_mismatch');
    const entry = current ?? { sha256: font.sha256, families: new Set() };
    entry.families.add(font.family);
    files.set(font.path, entry);
  }
  for (const [fontPath, font] of files) {
    const actual = createHash('sha256').update(await readFile(fontPath)).digest('hex');
    if (actual !== font.sha256) throw new PreviewError(`字体文件哈希不符：${[...font.families].join(',')}`, 'font_hash_mismatch');
  }
  const receipt = [];
  const seen = new Set();
  for (const family of families) {
    let found = false;
    for (const font of files.values()) {
      if (!font.families.has(family)) continue;
      found = true;
      const key = `${family}\0${font.sha256}`;
      if (seen.has(key)) continue;
      seen.add(key);
      receipt.push({ family, sha256: font.sha256 });
    }
    if (!found) throw new PreviewError(`字体不可用：${family}`, 'font_not_found');
  }
  return { files, receipt };
}

/**
 * Construct compliant PreviewInput from an exported package directory.
 * Useful for CLI execution and automated testing.
 */
export async function buildPreviewInputFromPackage({ packageDir, presetId = PREVIEW_PRESET_ID }) {
  const resolvedPkg = resolve(packageDir);
  const edlPath = join(resolvedPkg, 'edl.json');
  const manifestPath = join(resolvedPkg, 'delivery-manifest.json');

  let edlBytes;
  let manifestBytes;
  try {
    edlBytes = await readPackageJSON(await packageFile(resolvedPkg, 'edl.json'));
  } catch (err) {
    throw new PreviewError(`cannot read edl.json from package: ${err.message}`, 'missing_edl');
  }
  try {
    manifestBytes = await readPackageJSON(await packageFile(resolvedPkg, 'delivery-manifest.json'));
  } catch (err) {
    throw new PreviewError(`cannot read delivery-manifest.json from package: ${err.message}`, 'missing_manifest');
  }

  const edlSha = createHash('sha256').update(edlBytes).digest('hex');
  const manifestSha = createHash('sha256').update(manifestBytes).digest('hex');
  const { preset, sha256: presetSha } = await loadPreviewPreset(presetId);
  const deliveryManifest = JSON.parse(manifestBytes.toString('utf8'));

  const files = [];
  const seenPaths = new Set();
  const addFile = async (kind, relPath) => {
    const normalized = relPath.replaceAll('\\', '/');
    if (seenPaths.has(normalized)) return;
    const abs = await packageFile(resolvedPkg, normalized);
    const sha = await hashFile(abs);
    files.push({
      kind,
      path: normalized,
      sha256: sha,
      size: (await stat(abs)).size
    });
    seenPaths.add(normalized);
  };

  await addFile('edl', 'edl.json');
  await addFile('delivery_manifest', 'delivery-manifest.json');

  for (const item of deliveryManifest.artifacts || []) {
    let mappedKind = null;
    if (item.kind === 'source_video') mappedKind = 'source_video';
    else if (item.kind === 'packaged_source') mappedKind = 'packaged_source';
    else if (item.kind === 'game_audio') mappedKind = 'game_audio';
    else if (item.kind === 'voice') mappedKind = 'voice';
    else if (item.kind === 'music') mappedKind = 'music';
    else if (item.kind === 'subtitles' || item.kind === 'srt') mappedKind = 'srt';

    if (mappedKind) {
      await addFile(mappedKind, item.path);
    }
  }

  // Ensure stems and subtitles are captured if named conventionally
  for (const [kind, name] of [['game_audio', 'audio_a1.wav'], ['voice', 'audio_a2.wav'], ['music', 'audio_a3.wav']]) {
    if (!files.some((f) => f.kind === kind)) {
      try {
        await access(join(resolvedPkg, name));
        await addFile(kind, name);
      } catch (_) {}
    }
  }
  if (!files.some((f) => f.kind === 'srt')) {
    try {
      await access(join(resolvedPkg, 'subtitles.srt'));
      await addFile('srt', 'subtitles.srt');
    } catch (_) {}
  }

  const previewInput = {
    schema_version: 1,
    preview_id: `preview_${randomUUID().slice(0, 8)}`,
    task_id: `task_${randomUUID().slice(0, 8)}`,
    asset_id: `asset_${randomUUID().slice(0, 8)}`,
    run_id: `run_${randomUUID().slice(0, 8)}`,
    revision_id: `rev_${randomUUID().slice(0, 8)}`,
    package_ref: 'package',
    edl_sha256: edlSha,
    manifest_sha256: manifestSha,
    preset_id: preset.preset_id,
    preset_sha256: presetSha,
    preset,
    files,
    input_sha256: ''
  };
  previewInput.input_sha256 = fingerprintPreviewInput(previewInput);
  return { input: previewInput, packageDir: resolvedPkg };
}

/**
 * Main MP4 preview renderer implementation for Agent A.
 *
 * @param {{
 *   input: PreviewInput,
 *   packageDir: string,
 *   outputDir: string,
 *   signal?: AbortSignal,
 *   onProgress?: (event: { phase: PreviewPhase, progress: number, message: string }) => void
 * }} options
 * @returns {Promise<PreviewManifest>}
 */
export async function renderPreview(options) {
  options?.signal?.throwIfAborted();

  const { input, packageDir, subtitlePackageDir, outputDir, signal, onProgress, resolveFontsImpl = defaultResolveFonts } = options || {};

  // 1. Validate input structure and identity
  if (!input || typeof input !== 'object') {
    throw new PreviewError('valid PreviewInput is required', 'invalid_input');
  }
  const burnAss = input.schema_version === 2;
  if (![1, 2].includes(input.schema_version) || input.preset_id !== PREVIEW_PRESET_ID || burnAss !== Boolean(input.subtitle_variant)) {
    throw new PreviewError('unsupported preview protocol or preset', 'unsupported_preset');
  }

  // Preset verification against canonical file
  const canonicalPreset = await loadPreviewPreset(input.preset_id);
  if (canonicalPreset.sha256 !== input.preset_sha256) {
    throw new PreviewError('preset sha256 mismatch with canonical preset', 'preset_mismatch');
  }
  if (!isDeepStrictEqual(input.preset, canonicalPreset.preset)) {
    throw new PreviewError('preset content does not match canonical preset', 'preset_mismatch');
  }

  // Input fingerprint verification
  const computedFingerprint = fingerprintPreviewInput(input);
  if (computedFingerprint !== input.input_sha256) {
    throw new PreviewError(
      `input fingerprint mismatch: expected ${input.input_sha256}, got ${computedFingerprint}`,
      'fingerprint_mismatch'
    );
  }

  // 2. Validate packageDir and outputDir
  if (typeof packageDir !== 'string' || !packageDir.trim()) {
    throw new PreviewError('packageDir string is required', 'package_not_found');
  }
  const resolvedPkg = resolve(packageDir);
  try {
    const pkgStat = await stat(resolvedPkg);
    if (!pkgStat.isDirectory()) throw new Error();
    if ((await realpath(resolvedPkg)).toLowerCase() !== resolvedPkg.toLowerCase()) throw new Error();
  } catch (_) {
    throw new PreviewError(`package directory does not exist or is not a directory: ${packageDir}`, 'package_not_found');
  }

  if (typeof outputDir !== 'string' || !outputDir.trim()) {
    throw new PreviewError('outputDir string is required', 'invalid_output_dir');
  }
  const resolvedOutput = resolve(outputDir);
  await verifyOutputParent(resolvedOutput);
  try {
    await access(resolvedOutput);
    throw Object.assign(new PreviewError(`output directory already exists: ${resolvedOutput}`, 'EEXIST'), { code: 'EEXIST' });
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }

  // 3. Verify all input.files on disk
  onProgress?.({ phase: 'preparing', progress: 0.02, message: 'Verifying package input files' });
  signal?.throwIfAborted();

  const filesByKind = new Map();
  const filesByPath = new Map();
  const aliases = new Set();
  for (const file of input.files) {
    if (!isPreviewRelativePath(file.path)) {
      throw new PreviewError(`unsafe or invalid relative file path in input: ${file.path}`, 'invalid_file_path');
    }
    const alias = file.path.toLowerCase();
    if (aliases.has(alias)) throw new PreviewError(`ambiguous package path: ${file.path}`, 'invalid_file_path');
    aliases.add(alias);
    const absPath = await packageFile(resolvedPkg, file.path);
    const rel = relative(resolvedPkg, absPath);
    if (rel.startsWith('..') || isAbsolute(rel)) {
      throw new PreviewError(`input file path escapes package root: ${file.path}`, 'file_escapes_package');
    }

    let fileStat;
    try {
      fileStat = await stat(absPath);
      if (!fileStat.isFile()) throw new Error();
    } catch (_) {
      throw new PreviewError(`required package file not found: ${file.path}`, 'file_not_found');
    }

    if (fileStat.size !== file.size) {
      throw new PreviewError(`file size mismatch for ${file.path}: expected ${file.size}, got ${fileStat.size}`, 'size_mismatch');
    }

    const fileSha = await hashFile(absPath, signal);
    if (fileSha !== file.sha256) {
      throw new PreviewError(`file hash mismatch for ${file.path}: expected ${file.sha256}, got ${fileSha}`, 'hash_mismatch');
    }

    filesByKind.set(file.kind, (filesByKind.get(file.kind) || []).concat({ ...file, absPath }));
    filesByPath.set(file.path, { ...file, absPath });
  }

  const edlList = filesByKind.get('edl') || [];
  if (edlList.length !== 1) throw new PreviewError('exactly one edl file required', 'invalid_input_files');
  if (edlList[0].sha256 !== input.edl_sha256) {
    throw new PreviewError('edl sha256 mismatch with input.edl_sha256', 'edl_hash_mismatch');
  }

  const manifestList = filesByKind.get('delivery_manifest') || [];
  if (manifestList.length !== 1) throw new PreviewError('exactly one delivery_manifest file required', 'invalid_input_files');
  if (manifestList[0].sha256 !== input.manifest_sha256) {
    throw new PreviewError('manifest sha256 mismatch with input.manifest_sha256', 'manifest_hash_mismatch');
  }

  const gaList = filesByKind.get('game_audio') || [];
  const voList = filesByKind.get('voice') || [];
  const muList = filesByKind.get('music') || [];
  if (gaList.length !== 1 || voList.length !== 1 || muList.length !== 1) {
    throw new PreviewError('exactly 3 audio stems (game_audio, voice, music) required in input', 'missing_audio_stems');
  }

  const srtList = filesByKind.get('srt') || [];
  if (srtList.length > 1) {
    throw new PreviewError('at most one subtitle (srt) file allowed in input', 'duplicate_subtitles');
  }
  const burnSrt = !burnAss && srtList.length === 1;

  let fontInfo = null;
  if (burnSrt) {
    fontInfo = await findFontFile(input.preset.subtitles.font_family);
    if (!fontInfo) {
      throw new PreviewError(`required subtitle font not found: ${input.preset.subtitles.font_family}`, 'font_not_found');
    }
  }
  const frozenAss = burnAss ? await loadFrozenAss(input, subtitlePackageDir) : null;
  const assFonts = burnAss ? await resolveAssFonts(input.subtitle_variant.font_families, resolveFontsImpl) : null;

  // 4. Parse EDL and construct timeline
  const edlRaw = (await readPackageJSON(edlList[0].absPath)).toString('utf8');
  let edl;
  try {
    edl = JSON.parse(edlRaw);
    validateEdl(edl);
  } catch (err) {
    throw new PreviewError(`invalid EDL structure: ${err.message}`, 'invalid_edl');
  }

  const fps = edl.timeline.fps;
  const sampleRate = input.preset.audio.sample_rate;
  if (!Number.isSafeInteger(fps) || fps <= 0) {
    throw new PreviewError('project fps must be positive integer', 'invalid_fps');
  }
  if (!Number.isSafeInteger(sampleRate) || sampleRate <= 0) {
    throw new PreviewError('project sample_rate must be positive integer', 'invalid_sample_rate');
  }

  const toFrame = (sec) => toUnits(sec, fps);
  const videoCuts = edl.video.map((clip, index) => {
    const in_frame = toFrame(clip.in);
    const out_frame = toFrame(clip.out);
    const frames = out_frame - in_frame;
    if (frames <= 0) {
      throw new PreviewError(`video clip ${index} has non-positive frame count (${frames})`, 'invalid_clip_frames');
    }
    const timeline_in_frame = toFrame(clip.timeline_in);
    const timeline_out_frame = timeline_in_frame + frames;
    return {
      index,
      src: clip.src,
      in: clip.in,
      out: clip.out,
      in_frame,
      out_frame,
      frames,
      timeline_in: clip.timeline_in,
      timeline_out: clip.timeline_in + (clip.out - clip.in),
      timeline_in_frame,
      timeline_out_frame
    };
  }).sort((a, b) => a.timeline_in_frame - b.timeline_in_frame);

  // Validate no overlapping video cuts
  for (let i = 0; i < videoCuts.length - 1; i++) {
    if (videoCuts[i + 1].timeline_in_frame < videoCuts[i].timeline_out_frame) {
      throw new PreviewError(
        `overlapping video cuts at timeline frame ${videoCuts[i + 1].timeline_in_frame}`,
        'overlapping_clips'
      );
    }
  }

  // Construct segments with leading and intermediate gaps
  const segments = [];
  let cursor = 0;
  for (const cut of videoCuts) {
    if (cut.timeline_in_frame > cursor) {
      segments.push({ kind: 'gap', frames: cut.timeline_in_frame - cursor });
    }
    segments.push({ kind: 'clip', cut });
    cursor = cut.timeline_out_frame;
  }
  const total_frames = cursor;
  if (!Number.isSafeInteger(total_frames) || total_frames <= 0) {
    throw new PreviewError('timeline total frames must be positive', 'empty_timeline');
  }
  const target_samples = Math.round((total_frames * sampleRate) / fps);
  if (!Number.isSafeInteger(target_samples)) throw new PreviewError('target audio samples exceed safe integer range', 'invalid_timeline');

  // Probe all referenced video sources
  const uniqueVideoSrcs = [...new Set(videoCuts.map((c) => c.src))];
  const probedSources = new Map();
  for (const src of uniqueVideoSrcs) {
    const frozen = filesByPath.get(src);
    if (!isPreviewRelativePath(src) || !frozen || !['source_video', 'packaged_source'].includes(frozen.kind)) {
      throw new PreviewError(`video source is not a frozen package input: ${src}`, 'invalid_video_source');
    }
    const abs = frozen.absPath;
    try {
      await access(abs);
    } catch (_) {
      throw new PreviewError(`referenced video source missing from package: ${src}`, 'source_missing');
    }
    const probe = await probeMedia(abs, { signal });
    if (!probe?.video) {
      throw new PreviewError(`video stream missing in source: ${src}`, 'source_has_no_video');
    }
    // Reject frame rate mismatch or variable frame rate (VFR)
    if (Math.abs(probe.video.fps - fps) > 0.01) {
      throw new PreviewError(
        `source frame rate (${probe.video.fps}) does not match project fps (${fps}): ${src}`,
        'fps_mismatch'
      );
    }
    if (Math.abs(probe.video.fps - probe.video.nominalFps) > 0.01) {
      throw new PreviewError(`variable frame rate (VFR) source rejected: ${src}`, 'vfr_rejected');
    }
    probedSources.set(src, probe.video);
  }

  // 5. Canvas adaptation: preserve SAR, fit contain within 1280x720, no upscale for small assets
  const primaryVideo = probedSources.get(videoCuts[0].src);
  const baseW = primaryVideo.width;
  const baseH = primaryVideo.height;
  const maxWidth = input.preset.video.max_width;
  const maxHeight = input.preset.video.max_height;

  let canvasW = baseW;
  let canvasH = baseH;
  if (baseW > maxWidth || baseH > maxHeight) {
    const scale = Math.min(maxWidth / baseW, maxHeight / baseH);
    canvasW = Math.round(baseW * scale);
    canvasH = Math.round(baseH * scale);
  }
  canvasW = canvasW - (canvasW % 2);
  canvasH = canvasH - (canvasH % 2);

  onProgress?.({ phase: 'preparing', progress: 0.05, message: 'Canvas and timeline prepared' });
  signal?.throwIfAborted();

  // 6. Setup staging directory
  const parent = dirname(resolvedOutput);
  await mkdir(parent, { recursive: true });
  await verifyOutputParent(resolvedOutput);
  const stagingDir = await mkdtemp(join(parent, `.${basename(resolvedOutput)}-staging-`));

  let childProcess = null;
  let killed = false;

  try {
    // Copy subtitles into the staging directory and reference them relatively.
    // An absolute Windows path would be escaped by the subtitles filter.
    if (burnSrt) {
      await copyFile(srtList[0].absPath, join(stagingDir, 'subtitles.srt'));
    }
    if (burnAss) {
      await copyFile(frozenAss, join(stagingDir, 'subtitles.ass'));
      await mkdir(join(stagingDir, 'fonts'));
      let fontIndex = 0;
      for (const [fontPath, font] of assFonts.files) {
        const ext = extname(fontPath).toLowerCase();
        if (!['.ttf', '.otf', '.ttc'].includes(ext)) throw new PreviewError(`字体文件类型无效：${[...font.families].join(',')}`, 'font_not_found');
        await copyFile(fontPath, join(stagingDir, 'fonts', `${String(fontIndex).padStart(2, '0')}-${font.sha256.slice(0, 16)}${ext}`));
        fontIndex += 1;
      }
    }

    // Build FFmpeg arguments and filter graph
    const ffmpegArgs = ['-hide_banner', '-y'];

    const videoInputIndexMap = new Map();
    uniqueVideoSrcs.forEach((src, idx) => {
      videoInputIndexMap.set(src, idx);
      ffmpegArgs.push('-i', filesByPath.get(src).absPath);
    });

    const gaInputIdx = uniqueVideoSrcs.length;
    const voInputIdx = gaInputIdx + 1;
    const muInputIdx = voInputIdx + 1;
    ffmpegArgs.push('-i', gaList[0].absPath);
    ffmpegArgs.push('-i', voList[0].absPath);
    ffmpegArgs.push('-i', muList[0].absPath);

    const vFilters = [];
    const vSegmentLabels = [];
    segments.forEach((seg, i) => {
      const label = `vseg_${i}`;
      vSegmentLabels.push(`[${label}]`);
      if (seg.kind === 'gap') {
        vFilters.push(
          `color=c=${input.preset.video.gap_color}:s=${canvasW}x${canvasH}:r=${fps},` +
          `trim=end_frame=${seg.frames},setpts=PTS-STARTPTS,format=yuv420p[${label}]`
        );
      } else {
        const inIdx = videoInputIndexMap.get(seg.cut.src);
        vFilters.push(
          `[${inIdx}:v]fps=${fps},trim=start_frame=${seg.cut.in_frame}:end_frame=${seg.cut.out_frame},setpts=PTS-STARTPTS,` +
          `scale=w='min(${canvasW},iw)':h='min(${canvasH},ih)':force_original_aspect_ratio=decrease,` +
          `pad=${canvasW}:${canvasH}:(ow-iw)/2:(oh-ih)/2:${input.preset.video.gap_color},` +
          `setsar=1,format=yuv420p[${label}]`
        );
      }
    });

    vFilters.push(`${vSegmentLabels.join('')}concat=n=${segments.length}:v=1:a=0[vconcat]`);

    if (burnAss) {
      vFilters.push('[vconcat]subtitles=filename=subtitles.ass:fontsdir=fonts[vfinal]');
    } else if (burnSrt) {
      vFilters.push(`[vconcat]subtitles=filename=subtitles.srt:force_style='FontName=${input.preset.subtitles.font_family}'[vfinal]`);
    } else {
      vFilters.push(`[vconcat]null[vfinal]`);
    }

    // Audio filters: resample stems, mix without normalization, apply alimiter and target sample trim/pad
    const aFilters = [
      `[${gaInputIdx}:a]aresample=${sampleRate},aformat=channel_layouts=stereo[a_ga]`,
      `[${voInputIdx}:a]aresample=${sampleRate},aformat=channel_layouts=stereo[a_vo]`,
      `[${muInputIdx}:a]aresample=${sampleRate},aformat=channel_layouts=stereo[a_mu]`,
      `[a_ga][a_vo][a_mu]amix=inputs=3:duration=longest:normalize=0[a_mixed]`,
      `[a_mixed]alimiter=limit=${input.preset.audio.limiter.limit}:attack=${input.preset.audio.limiter.attack_ms}:release=${input.preset.audio.limiter.release_ms}:level=false:latency=true[a_lim]`,
      `[a_lim]apad=whole_len=${target_samples},atrim=end_sample=${target_samples},asetpts=PTS-STARTPTS[afinal]`
    ];

    const filterComplex = [...vFilters, ...aFilters].join(';');
    if (Buffer.byteLength(filterComplex, 'utf8') > 8 * 1024 * 1024) {
      throw new PreviewError('preview filter graph exceeds 8 MiB', 'filter_graph_too_large');
    }
    const graphFile = 'filtergraph.txt';
    signal?.throwIfAborted();
    await writeFile(join(stagingDir, graphFile), filterComplex, {encoding:'utf8', signal});
    // FFmpeg 7+ accepts the generic file-valued option syntax. Older stable
    // releases use the legacy script option; no graph text goes on the argv.
    const ffmpegVersion = await getFfmpegVersion({signal});
    const ffmpegMajor = Number(/^(\d+)\./.exec(ffmpegVersion)?.[1]);
    ffmpegArgs.push(
      Number.isFinite(ffmpegMajor) && ffmpegMajor >= 7 ? '-/filter_complex' : '-filter_complex_script', graphFile,
      '-map', '[vfinal]',
      '-c:v', input.preset.video.encoder,
      '-pix_fmt', input.preset.video.pixel_format,
      '-crf', String(input.preset.video.crf),
      '-preset', input.preset.video.speed,
      '-map', '[afinal]',
      '-c:a', input.preset.audio.encoder,
      '-b:a', `${input.preset.audio.bitrate_kbps}k`,
      '-ar', String(sampleRate),
      '-ac', '2'
    );
    if (input.preset.container.faststart) {
      ffmpegArgs.push('-movflags', '+faststart');
    }
    ffmpegArgs.push('-r', String(fps), '-fps_mode', 'cfr', '-progress', 'pipe:1', 'final.mp4');
    // Reserve executable space and conservatively allow Windows quoting and
    // doubled backslashes. Many distinct source paths can still exceed argv.
    const commandUnits = 1024 + ffmpegArgs.reduce((units, arg) => units + 2 * arg.length + 3, 0);
    if (process.platform === 'win32' && commandUnits > 30_000) {
      throw new PreviewError('too many input paths for the Windows command line', 'command_line_too_large');
    }
    signal?.throwIfAborted();

    // 7. Spawn FFmpeg child process
    childProcess = spawn('ffmpeg', ffmpegArgs, { cwd: stagingDir, windowsHide: true });

    let killTimer, processError;
    const stopProcess = () => {
      if (killTimer) return;
      try { childProcess.kill('SIGTERM'); } catch (error) { processError ??= error; }
      killTimer = setTimeout(() => {
        try { childProcess.kill('SIGKILL'); } catch (_) {}
      }, 1000);
      killTimer.unref();
    };
    const abortHandler = () => {
      killed = true;
      stopProcess();
    };
    signal?.addEventListener('abort', abortHandler, { once: true });
    if (signal?.aborted) abortHandler();

    let stderrTail = '';
    childProcess.stderr.on('data', (chunk) => {
      stderrTail += chunk.toString('utf8');
      if (stderrTail.length > 50000) {
        stderrTail = stderrTail.slice(-50000);
      }
    });

    let stdoutBuf = '';
    childProcess.stdout.on('data', (chunk) => {
      if (processError || killed) return;
      // Progress is a sequence of short ASCII lines. Bound an unterminated
      // line before retaining it, and still wait for close before cleanup.
      if (stdoutBuf.length + chunk.length > 128 * 1024) {
        processError = new PreviewError('ffmpeg progress exceeds 128 KiB', 'progress_output_limit');
        stdoutBuf = '';
        stopProcess();
        return;
      }
      stdoutBuf += chunk.toString('utf8');
      const lines = stdoutBuf.split('\n');
      stdoutBuf = lines.pop() || '';
      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed.startsWith('frame=')) {
          const currentFrame = parseInt(trimmed.slice(6).trim(), 10);
          if (Number.isFinite(currentFrame) && total_frames > 0) {
            const p = Math.min(0.85, 0.10 + 0.75 * Math.min(1, currentFrame / total_frames));
            try {
              onProgress?.({
                phase: 'rendering',
                progress: p,
                message: `Rendering frame ${currentFrame}/${total_frames}`
              });
            } catch (error) {
              processError = error instanceof Error ? error : new PreviewError(String(error), 'progress_callback_failed');
              stopProcess();
              break;
            }
          }
        }
      }
    });

    const exitCode = await new Promise(resolveClose => {
      childProcess.on('error', error => { processError ??= error; });
      childProcess.on('close', code => resolveClose(code));
    });
    clearTimeout(killTimer);
    signal?.removeEventListener('abort', abortHandler);

    if (killed || signal?.aborted) {
      signal?.throwIfAborted();
      throw new PreviewError('preview rendering aborted by signal', 'aborted');
    }
    if (processError) throw processError;

    if (exitCode !== 0) {
      throw new PreviewError(`ffmpeg exited with code ${exitCode}: ${stderrTail.trim()}`, 'ffmpeg_failed');
    }

    // 8. Verifying rendered MP4 media
    onProgress?.({ phase: 'verifying', progress: 0.90, message: 'Verifying rendered media streams' });
    signal?.throwIfAborted();

    const finalMp4Path = join(stagingDir, 'final.mp4');
    const mediaInfo = await probePreviewMedia(finalMp4Path, { signal });

    if (mediaInfo.video_codec !== 'h264' || mediaInfo.audio_codec !== 'aac') {
      throw new PreviewError('rendered output codecs mismatch H.264/AAC preset', 'codec_mismatch');
    }
    if (mediaInfo.width !== canvasW || mediaInfo.height !== canvasH) {
      throw new PreviewError(
        `rendered video dimensions ${mediaInfo.width}x${mediaInfo.height} mismatch canvas ${canvasW}x${canvasH}`,
        'dimension_mismatch'
      );
    }
    if (Math.abs(mediaInfo.fps - fps) > 0.01) {
      throw new PreviewError(`rendered video fps ${mediaInfo.fps} mismatch project fps ${fps}`, 'fps_mismatch');
    }
    if (mediaInfo.frame_count !== total_frames) {
      throw new PreviewError(
        `rendered video frame count ${mediaInfo.frame_count} mismatch target frames ${total_frames}`,
        'frame_count_mismatch'
      );
    }
    if (Math.abs(mediaInfo.duration_seconds - total_frames / fps) > 0.025) {
      throw new PreviewError('rendered video duration mismatches timeline', 'duration_mismatch');
    }
    if (mediaInfo.sample_rate !== sampleRate) {
      throw new PreviewError(
        `rendered audio sample rate ${mediaInfo.sample_rate} mismatch target ${sampleRate}`,
        'sample_rate_mismatch'
      );
    }
    if (mediaInfo.channels !== 2) {
      throw new PreviewError(`rendered audio channels ${mediaInfo.channels} mismatch 2`, 'channel_mismatch');
    }
    validateAudioInterval(mediaInfo.audio_interval, target_samples, sampleRate, fps);

    const checks = [
      { code: 'source_fingerprint', status: 'passed', message: 'Input identity and package files verified' },
      { code: 'timeline_frames', status: 'passed', message: `Rendered ${mediaInfo.frame_count} frames matches target ${total_frames}` },
      { code: 'audio_alignment', status: 'passed', message: `Audio stream starts at ${mediaInfo.audio_interval.start_seconds}s and covers ${mediaInfo.audio_interval.duration_seconds}s; target ${target_samples}/${sampleRate}s` },
      {
        code: 'subtitles',
        status: 'passed',
        message: burnAss
          ? `ASS burned with fonts ${assFonts.receipt.map(font => font.family).join(', ')}`
          : burnSrt ? `Subtitles burned with font ${input.preset.subtitles.font_family}` : 'No subtitles present; burn omitted'
      },
      {
        code: 'media_streams',
        status: 'passed',
        message: `Verified video stream (${mediaInfo.video_codec}, ${mediaInfo.width}x${mediaInfo.height}, ${mediaInfo.fps}fps) and audio stream (${mediaInfo.audio_codec}, ${mediaInfo.sample_rate}Hz, ${mediaInfo.channels}ch)`
      }
    ];

    const report = {
      schema_version: 1,
      input_sha256: input.input_sha256,
      target_frames: total_frames,
      target_samples: target_samples,
      media: mediaInfo,
      checks
    };
    const reportContent = `${JSON.stringify(report, null, 2)}\n`;
    await writeFile(join(stagingDir, PREVIEW_FILENAMES.report), reportContent, 'utf8');

    const receipt = {
      schema_version: 1,
      preview_id: input.preview_id,
      task_id: input.task_id,
      input_sha256: input.input_sha256,
      preset_sha256: input.preset_sha256,
      renderer_version: '@video-auto-cut/timeline-cli 1.0.0',
      ffmpeg_version: ffmpegVersion,
      ...(burnAss ? { fonts: assFonts.receipt } : {}),
      ...(burnSrt && fontInfo ? { font: { family: fontInfo.family, sha256: fontInfo.sha256 } } : {})
    };
    const receiptContent = `${JSON.stringify(receipt, null, 2)}\n`;
    await writeFile(join(stagingDir, PREVIEW_FILENAMES.receipt), receiptContent, 'utf8');

    const mp4Stat = await stat(finalMp4Path);
    const mp4Hash = await hashFile(finalMp4Path, signal);

    const reportBytes = Buffer.from(reportContent, 'utf8');
    const reportHash = createHash('sha256').update(reportBytes).digest('hex');

    const receiptBytes = Buffer.from(receiptContent, 'utf8');
    const receiptHash = createHash('sha256').update(receiptBytes).digest('hex');

    const artifacts = [
      { key: 'mp4', kind: 'preview', path: PREVIEW_FILENAMES.media, mime: 'video/mp4', size: mp4Stat.size, sha256: mp4Hash },
      { key: 'report', kind: 'render_report', path: PREVIEW_FILENAMES.report, mime: 'application/json', size: reportBytes.length, sha256: reportHash },
      { key: 'receipt', kind: 'receipt', path: PREVIEW_FILENAMES.receipt, mime: 'application/json', size: receiptBytes.length, sha256: receiptHash }
    ];

    const manifest = {
      schema_version: 1,
      preview_id: input.preview_id,
      task_id: input.task_id,
      input_sha256: input.input_sha256,
      preset_sha256: input.preset_sha256,
      artifacts
    };
    await writeFile(join(stagingDir, PREVIEW_FILENAMES.manifest), `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');

    // Temporary subtitle and font files stay out of the published preview package.
    await rm(join(stagingDir, graphFile));
    if (burnSrt) await rm(join(stagingDir, 'subtitles.srt'), { force: true });
    if (burnAss) {
      await rm(join(stagingDir, 'subtitles.ass'), { force: true });
      await rm(join(stagingDir, 'fonts'), { recursive: true, force: true });
    }

    // 9. Atomic publication to new directory
    onProgress?.({ phase: 'publishing', progress: 0.95, message: 'Publishing preview package' });
    signal?.throwIfAborted();

    await verifyOutputParent(resolvedOutput);
    await publishNewDirectory(stagingDir, resolvedOutput, {signal});
    return manifest;
  } catch (error) {
    await rm(stagingDir, { recursive: true, force: true });
    throw error;
  }
}
