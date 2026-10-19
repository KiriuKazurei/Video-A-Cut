import { spawn } from 'node:child_process';
import { basename, dirname, isAbsolute, resolve } from 'node:path';

export function runTool(command, args, { signal, timeoutMs, maxOutputBytes = 8 * 1024 * 1024, cwd } = {}) {
  // Local footage must not cause FFmpeg to fetch URLs hidden in a playlist.
  const name = basename(command).replace(/\.exe$/i, '').toLowerCase();
  if (name === 'ffprobe') args = ['-protocol_whitelist', 'file,pipe', '-format_whitelist', 'mov,matroska,webm,avi,mpegts,mpegvideo,flv,ogg,wav,mp3,aac,flac', ...args];
  if (name === 'ffmpeg') args = args.flatMap(value => value === '-i' ? ['-protocol_whitelist', 'file,pipe', value] : [value]);
  signal?.throwIfAborted();
  return new Promise((done, fail) => {
    const child = spawn(command, args, { windowsHide: true, cwd });
    const stdout = [];
    let stdoutBytes = 0, stderr = '', failure, escalation;
    const stop = error => {
      if (failure) return;
      failure = error;
      child.kill('SIGTERM');
      escalation = setTimeout(() => child.kill('SIGKILL'), 1000);
      escalation.unref();
    };
    const abort = () => stop(signal.reason);
    const timeout = timeoutMs > 0 ? setTimeout(() => stop(Object.assign(new Error(`${command} timed out`), { code: 'media_tool_timeout' })), timeoutMs) : undefined;
    timeout?.unref();
    signal?.addEventListener('abort', abort, { once: true });
    if (signal?.aborted) abort();
    child.stdout.on('data', part => {
      stdoutBytes += part.length;
      if (stdoutBytes > maxOutputBytes) {
        stop(Object.assign(new Error(`${command} output exceeds limit`), { code: 'media_tool_output_limit' }));
      } else if (!failure) stdout.push(part);
    });
    child.stderr.on('data', part => { stderr = (stderr + part.toString()).slice(-50_000); });
    child.on('error', error => { failure ??= error; });
    // Settle after close so callers can safely clean the process's working directory.
    child.on('close', code => {
      clearTimeout(timeout); clearTimeout(escalation);
      signal?.removeEventListener('abort', abort);
      if (failure) fail(failure);
      else if (code !== 0) fail(new Error(`${command} exited ${code}: ${stderr.trim()}`));
      else done(Buffer.concat(stdout).toString('utf8'));
    });
  });
}

export function sourcePath(edlPath, src) {
  if (!edlPath) throw new Error('real media export requires an EDL file path');
  return isAbsolute(src) ? src : resolve(dirname(edlPath), src);
}

function rate(value) {
  const [num, den = '1'] = String(value || '0/1').split('/').map(Number);
  return den ? num / den : 0;
}

export async function probeMedia(path, options) {
  const raw = await runTool('ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', path], { timeoutMs: 30_000, ...options });
  const data = JSON.parse(raw);
  const video = data.streams?.find((stream) => stream.codec_type === 'video');
  const audio = data.streams?.find((stream) => stream.codec_type === 'audio');
  const duration = Number(data.format?.duration);
  if (!Number.isFinite(duration) || duration <= 0) throw new Error(`cannot determine media duration: ${path}`);
  return {
    path,
    duration,
    video: video ? {
      fps: rate(video.avg_frame_rate), nominalFps: rate(video.r_frame_rate),
      width: video.width, height: video.height,
      frames: Number(video.nb_frames) || null,
      duration: Number(video.duration) || duration
    } : null,
    audio: audio ? {
      sampleRate: Number(audio.sample_rate), channels: audio.channels,
      duration: Number(audio.duration) || duration
    } : null
  };
}
