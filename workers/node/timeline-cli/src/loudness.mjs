import { spawn } from 'node:child_process';
import { stat } from 'node:fs/promises';

// Loudness envelope for timeline display. PCM is streamed from FFmpeg and
// folded into fixed buckets as it arrives, so an hour-long recording never
// sits in memory; only the encoded buckets (a few hundred KB) are returned.
export const LOUDNESS_VERSION = 1;
export const SAMPLE_RATE = 8000;
export const FLOOR_DB = -60;
export const MAX_BUCKETS = 200_000;
export const MIN_BUCKET_SECONDS = 0.02;
export const CLIP_DB = -0.1;

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
  constructor(samplesPerBucket) { this.n = samplesPerBucket; this.peak = []; this.rms = []; this.clipped = 0; this.count = 0; this.max = 0; this.sum = 0; }
  push(samples) {
    for (let i = 0; i < samples.length; i++) {
      const v = Math.abs(samples[i]);
      if (v > this.max) this.max = v;
      this.sum += v * v;
      if (++this.count === this.n) this.flush();
    }
  }
  flush() {
    if (!this.count) return;
    if (this.max >= 10 ** (CLIP_DB / 20)) this.clipped++;
    this.peak.push(encodeLevel(this.max)); this.rms.push(encodeLevel(Math.sqrt(this.sum / this.count)));
    this.count = 0; this.max = 0; this.sum = 0;
  }
  finish() { this.flush(); return { peak: Buffer.from(Uint8Array.from(this.peak)).toString('base64'), rms: Buffer.from(Uint8Array.from(this.rms)).toString('base64'), buckets: this.peak.length, clipped_buckets: this.clipped }; }
}

function decodePCM(path, stream, onSamples, { timeoutMs = 5 * 60_000, command = 'ffmpeg' } = {}) {
  const args = ['-hide_banner', '-loglevel', 'error', '-nostdin', '-protocol_whitelist', 'file,pipe', '-i', path, '-map', `0:${stream}`, '-vn', '-sn', '-dn', '-ac', '1', '-ar', String(SAMPLE_RATE), '-f', 'f32le', '-'];
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

export async function loudness(input, { decode = decodePCM } = {}) {
  const info = await stat(input.path); if (!info.isFile()) throw Error('响度源无效');
  if (!Number.isInteger(input.stream_index) || input.stream_index < 0) throw Error('音频流无效');
  const plan = bucketPlan(Number(input.duration));
  const acc = new LoudnessAccumulator(plan.samples);
  await decode(input.path, input.stream_index, s => acc.push(s));
  return { version: LOUDNESS_VERSION, stream_index: input.stream_index, duration: Number(input.duration), sample_rate: SAMPLE_RATE, bucket_seconds: plan.bucket_seconds, floor_db: FLOOR_DB, clip_db: CLIP_DB, encoding: 'u8-dbfs', ...acc.finish() };
}
