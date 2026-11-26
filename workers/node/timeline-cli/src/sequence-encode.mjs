import os from 'node:os';

/** Timeout for one sequence encode. Short programs keep the historical 6 minute floor; long or large programs scale up to 6 hours (30 minutes while previewing). */
export function sequenceEncodeTimeoutMs({ duration = 0, width = 1280, height = 720, filterCount = 1, preview = false } = {}) {
  const pixelFactor = Math.max(1, (width * height) / (1280 * 720));
  const filterFactor = Math.max(1, (filterCount || 1) / 6);
  const perSecond = preview ? 4 : 12;
  const estimated = Math.max(0, duration) * perSecond * pixelFactor * filterFactor * 1000;
  const floor = 360_000;
  const cap = preview ? 30 * 60_000 : 6 * 60 * 60_000;
  return Math.min(cap, Math.max(floor, Math.round(estimated)));
}

/** Video encoder threads. Preview stays at 2. A full encode uses the CPU count, clamped to 2–8, unless the caller sets 1–16. */
export function sequenceEncodeThreads(requested, { preview = false } = {}) {
  if (preview && !(Number.isInteger(requested) && requested > 0)) return 2;
  const cpus = typeof os.availableParallelism === 'function' ? os.availableParallelism() : (os.cpus()?.length || 2);
  const auto = Math.max(2, Math.min(8, cpus || 2));
  const chosen = Number.isInteger(requested) && requested > 0 ? requested : auto;
  return Math.max(1, Math.min(16, chosen));
}

/** Filter threads stay at 1 for preview and for the untouched program render. An explicit export encode may use up to 4. */
export function sequenceFilterThreads(videoThreads, { preview = false, explicit = false } = {}) {
  if (preview || !explicit) return 1;
  return Math.max(1, Math.min(4, videoThreads));
}

function evenBox(value, label) {
  if (!Number.isInteger(value) || value < 2 || value > 7680 || value % 2 !== 0) throw Error(`${label}须为 2–7680 的偶数`);
  return value;
}

/**
 * Turn an export encode request into ffmpeg pieces.
 * Absent input means the historical program encode (libx264 veryfast CRF 20, burned ASS, no loudnorm).
 */
export function normalizeSequenceEncode(raw) {
  if (raw == null) return { active: false };
  if (typeof raw !== 'object') throw Error('导出编码参数无效');
  const subtitles = raw.subtitles == null || raw.subtitles === '' || raw.subtitles === 'burn' ? 'burn' : raw.subtitles === 'sidecar' ? 'sidecar' : null;
  if (!subtitles) throw Error('字幕只支持烧录或外挂 SRT');
  let loudness = '';
  if (raw.loudness != null && raw.loudness !== '' && raw.loudness !== 'off') {
    const text = String(raw.loudness);
    if (text !== '-14' && text !== '-16') throw Error('响度目标只支持 -14 或 -16 LUFS');
    loudness = text;
  }
  const codec = raw.codec == null || raw.codec === '' || raw.codec === 'h264' ? 'h264' : raw.codec === 'hevc' ? 'hevc' : null;
  if (!codec) throw Error('编码只支持 H.264 或 HEVC');
  const bitrate = raw.bitrate || raw.video_bitrate || '';
  if (bitrate && !/^[1-9]\d{0,4}(\.\d)?[kKmM]$/.test(bitrate)) throw Error('视频码率格式无效');
  let crf = raw.crf;
  if (bitrate) {
    if (crf != null && crf !== '') throw Error('码率与 CRF 只能选一个');
    crf = null;
  } else if (crf == null || crf === '') crf = 20;
  if (crf != null && (!Number.isInteger(crf) || crf < 0 || crf > 51)) throw Error('CRF 范围为 0–51');
  let width = 0, height = 0, maxWidth = 0, maxHeight = 0;
  if (raw.width || raw.height) {
    width = evenBox(raw.width, '宽度');
    height = evenBox(raw.height, '高度');
  }
  if (raw.max_width || raw.max_height) {
    maxWidth = evenBox(raw.max_width, '最大宽度');
    maxHeight = evenBox(raw.max_height, '最大高度');
  }
  let fpsNum = 0, fpsDen = 0;
  if (raw.fps_num || raw.fps_den) {
    fpsNum = raw.fps_num;
    fpsDen = raw.fps_den;
    const fps = fpsDen ? fpsNum / fpsDen : 0;
    if (!Number.isInteger(fpsNum) || !Number.isInteger(fpsDen) || fpsDen < 1 || !(fps >= 1 && fps <= 120)) throw Error('导出帧率范围为 1–120');
  }
  const threads = raw.threads == null || raw.threads === '' || raw.threads === 0 ? 0 : raw.threads;
  if (threads && (!Number.isInteger(threads) || threads < 1 || threads > 16)) throw Error('编码线程范围为 1–16');
  let scale = '';
  if (width && height) scale = `scale=${width}:${height}:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=${width}:${height}:(ow-iw)/2:(oh-ih)/2:color=black`;
  else if (maxWidth && maxHeight) scale = `scale=w='min(${maxWidth}\\,iw)':h='min(${maxHeight}\\,ih)':force_original_aspect_ratio=decrease:force_divisible_by=2`;
  return {
    active: true, codec, crf, bitrate, subtitles, loudness, width, height, maxWidth, maxHeight,
    noUpscale: Boolean(raw.no_upscale), fpsNum, fpsDen, threads, scale
  };
}

export function sequenceVideoCodecArgs(encode, { preview = false, fps = 30 } = {}) {
  if (preview) {
    return ['-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '25', '-profile:v', 'main', '-level:v', '3.1', '-bf', '0', '-g', String(Math.ceil(fps * 2))];
  }
  const codec = encode?.codec === 'hevc' ? 'libx265' : 'libx264';
  const args = ['-c:v', codec, '-preset', 'veryfast'];
  if (encode?.bitrate) args.push('-b:v', encode.bitrate);
  else args.push('-crf', String(encode?.crf ?? 20));
  if (codec === 'libx265') args.push('-tag:v', 'hvc1');
  return args;
}

export function srtTimestamp(seconds) {
  const ms = Math.max(0, Math.round(Number(seconds) * 1000));
  const h = Math.floor(ms / 3_600_000);
  const m = Math.floor(ms / 60_000) % 60;
  const s = Math.floor(ms / 1000) % 60;
  const milli = ms % 1000;
  const pad = (value, size) => String(value).padStart(size, '0');
  return `${pad(h, 2)}:${pad(m, 2)}:${pad(s, 2)},${pad(milli, 3)}`;
}

export function srtDocument(cues) {
  return cues.map((cue, index) => `${index + 1}\n${srtTimestamp(cue.start)} --> ${srtTimestamp(cue.end)}\n${String(cue.text ?? '').replaceAll('\r', '')}\n`).join('\n');
}
