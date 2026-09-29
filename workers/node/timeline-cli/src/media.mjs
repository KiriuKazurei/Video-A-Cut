import { spawn } from 'node:child_process';
import { dirname, isAbsolute, resolve } from 'node:path';

export function runTool(command, args) {
  return new Promise((done, fail) => {
    const child = spawn(command, args, { windowsHide: true });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (part) => { stdout += part; });
    child.stderr.on('data', (part) => { stderr += part; });
    child.on('error', fail);
    child.on('close', (code) => code === 0 ? done(stdout) : fail(new Error(`${command} exited ${code}: ${stderr.trim()}`)));
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

export async function probeMedia(path) {
  const raw = await runTool('ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', path]);
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
