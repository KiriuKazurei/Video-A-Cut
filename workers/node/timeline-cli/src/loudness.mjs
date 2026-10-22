import { spawn } from 'node:child_process';
import { stat } from 'node:fs/promises';
import { runTool } from './media.mjs';

// Loudness envelope for timeline display. PCM is streamed from FFmpeg and
// folded into fixed buckets as it arrives, so an hour-long recording never
// sits in memory; only the encoded buckets (a few hundred KB) are returned.
export const LOUDNESS_VERSION = 2;
export const SAMPLE_RATE = 8000;
export const FLOOR_DB = -60;
export const MAX_BUCKETS = 200_000;
export const MIN_BUCKET_SECONDS = 0.02;
export const CLIP_DB = -0.1;
export const MAX_CHANNELS = 8;

export function bucketPlan(duration) {
  if (!(duration > 0) || duration > 24 * 3600) throw Error('响度源无效');
  const seconds = Math.max(MIN_BUCKET_SECONDS, duration / MAX_BUCKETS);
  const samples = Math.max(1, Math.round(seconds * SAMPLE_RATE));
  return { samples, bucket_seconds: samples / SAMPLE_RATE };
}

/** Linear amplitude -> byte over [FLOOR_DB, 0] dBFS; 0 means at/below the floor. */
export function encodeLevel(linear) {
  if (!(linear > 0)) return 0;
  const db = 20 * Math.log10(linear);
  return Math.round(Math.min(1, Math.max(0, (db - FLOOR_DB) / -FLOOR_DB)) * 255);
}
export const decodeLevelDb = byte => byte <= 0 ? -Infinity : FLOOR_DB + byte / 255 * -FLOOR_DB;

export class LoudnessAccumulator {
  /** samplesPerBucket counts frames; channels are interleaved. Peak is the
   *  max over all channels (so one full-scale channel still flags clipping),
   *  RMS is the power mean over channels. */
  constructor(samplesPerBucket, channels = 1) { this.n = samplesPerBucket; this.ch = Math.max(1, channels | 0); this.peak = []; this.rms = []; this.clipped = 0; this.count = 0; this.max = 0; this.sum = 0; this.partial = []; }
  push(samples) {
    const ch = this.ch;
    if (this.partial.length) { const merged = new Float32Array(this.partial.length + samples.length); merged.set(this.partial); merged.set(samples, this.partial.length); samples = merged; this.partial = []; }
    const frames = Math.floor(samples.length / ch);
    for (let f = 0; f < frames; f++) {
      let power = 0;
      for (let c = 0; c < ch; c++) { const v = Math.abs(samples[f * ch + c]); if (v > this.max) this.max = v; power += v * v; }
      this.sum += power / ch;
      if (++this.count === this.n) this.flush();
    }
    if (frames * ch < samples.length) this.partial = Array.from(samples.subarray(frames * ch));
  }
  flush() {
    if (!this.count) return;
    if (this.max >= 10 ** (CLIP_DB / 20)) this.clipped++;
    this.peak.push(encodeLevel(this.max)); this.rms.push(encodeLevel(Math.sqrt(this.sum / this.count)));
    this.count = 0; this.max = 0; this.sum = 0;
  }
  finish() { this.flush(); return { peak: Buffer.from(Uint8Array.from(this.peak)).toString('base64'), rms: Buffer.from(Uint8Array.from(this.rms)).toString('base64'), buckets: this.peak.length, clipped_buckets: this.clipped }; }
}

export async function probeChannels(path, stream, { run = runTool } = {}) {
  const out = await run('ffprobe', ['-v', 'error', '-show_entries', 'stream=index,channels', '-of', 'json', path], { timeoutMs: 30_000 });
  const found = JSON.parse(out).streams?.find(s => s.index === stream);
  const channels = Number(found?.channels);
  return Number.isInteger(channels) && channels > 0 ? Math.min(channels, MAX_CHANNELS) : 1;
}

// No -ac downmix: averaging L/R hides a full-scale single channel (about -6 dB
// after downmix). Sources with more than MAX_CHANNELS are reduced to that many.
export function decodeArgs(path, stream, channels) {
  return ['-hide_banner', '-loglevel', 'error', '-nostdin', '-protocol_whitelist', 'file,pipe', '-i', path, '-map', `0:${stream}`, '-vn', '-sn', '-dn', ...(channels === 1 ? ['-ac', '1'] : ['-ac', String(channels)]), '-ar', String(SAMPLE_RATE), '-f', 'f32le', '-'];
}

function decodePCM(path, stream, onSamples, { timeoutMs = 5 * 60_000, command = 'ffmpeg', channels = 1 } = {}) {
  const args = decodeArgs(path, stream, channels);
  return new Promise((done, fail) => {
    const child = spawn(command, args, { windowsHide: true });
    let rest = Buffer.alloc(0), stderr = '', failure;
    const timer = setTimeout(() => { failure = Error('响度计算超时'); child.kill('SIGKILL'); }, timeoutMs); timer.unref();
    child.stdout.on('data', chunk => {
      const buf = rest.length ? Buffer.concat([rest, chunk]) : chunk;
      const usable = buf.length - buf.length % 4;
      // Copy into an aligned buffer; Buffer slices may not be 4-byte aligned.
      const aligned = new Float32Array(usable / 4);
      Buffer.from(aligned.buffer).set(buf.subarray(0, usable));
      onSamples(aligned);
      rest = buf.subarray(usable);
    });
    child.stderr.on('data', c => { if (stderr.length < 4096) stderr += c; });
    child.on('error', e => { clearTimeout(timer); fail(e); });
    child.on('close', code => { clearTimeout(timer); if (failure) fail(failure); else if (code) fail(Error(`ffmpeg 解码音频失败: ${stderr.trim().slice(0, 300)}`)); else done(); });
  });
}

export async function loudness(input, { decode = decodePCM, channels: probe = probeChannels } = {}) {
  const info = await stat(input.path); if (!info.isFile()) throw Error('响度源无效');
  if (!Number.isInteger(input.stream_index) || input.stream_index < 0) throw Error('音频流无效');
  const plan = bucketPlan(Number(input.duration));
  const channels = await probe(input.path, input.stream_index);
  const acc = new LoudnessAccumulator(plan.samples, channels);
  await decode(input.path, input.stream_index, s => acc.push(s), { channels });
  return { version: LOUDNESS_VERSION, stream_index: input.stream_index, duration: Number(input.duration), sample_rate: SAMPLE_RATE, bucket_seconds: plan.bucket_seconds, floor_db: FLOOR_DB, clip_db: CLIP_DB, encoding: 'u8-dbfs', channels, ...acc.finish() };
}
