import { mkdir, writeFile } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

function ffmpeg(args) {
  return new Promise((done, fail) => {
    const child = spawn('ffmpeg', ['-hide_banner', '-loglevel', 'error', '-y', ...args], { windowsHide: true });
    let stderr = '';
    child.stderr.on('data', (data) => { stderr += data; });
    child.on('error', fail);
    child.on('close', (code) => code === 0 ? done() : fail(new Error(`ffmpeg failed (${code}): ${stderr}`)));
  });
}

/** Creates small, synthetic media. No copyrighted or user media is embedded. */
export async function generateMediaFixture(directory) {
  const root = resolve(directory);
  await mkdir(root, { recursive: true });
  await ffmpeg(['-f', 'lavfi', '-i', 'smptebars=size=320x180:rate=30:duration=3',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=3',
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-r', '30',
    '-c:a', 'aac', '-ar', '48000', '-shortest', join(root, 'clip_01.mp4')]);
  await ffmpeg(['-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=30:duration=3',
    '-f', 'lavfi', '-i', 'sine=frequency=660:sample_rate=48000:duration=3',
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-r', '30',
    '-c:a', 'aac', '-ar', '48000', '-shortest', join(root, 'clip_02.mp4')]);
  await ffmpeg(['-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000:duration=0.8',
    '-ac', '2', '-c:a', 'pcm_s16le', join(root, 'voice.wav')]);
  await ffmpeg(['-f', 'lavfi', '-i', 'sine=frequency=220:sample_rate=48000:duration=3.5',
    '-ac', '2', '-c:a', 'pcm_s16le', join(root, 'music.wav')]);

  const edl = {
    timeline: { fps: 30, sample_rate: 48000 },
    video: [
      { src: 'clip_01.mp4', in: 0, out: 2, timeline_in: 0 },
      { src: 'clip_02.mp4', in: 0.5, out: 2, timeline_in: 2 }
    ],
    game_audio: [
      { src: 'clip_01.mp4', in: 0, out: 2, timeline_in: 0, gain_db: -12 },
      { src: 'clip_02.mp4', in: 0.5, out: 2, timeline_in: 2, gain_db: -12 }
    ],
    voice: [{ id: 'vo_001', src: 'voice.wav', start: 0.7, end: 1.5, gain_db: 0 }],
    subtitle: [{ id: 'sub_001', text: 'Synthetic test voice', start: 0.7, end: 1.5 }],
    music: [{ src: 'music.wav', start: 0, end: 3.5, gain_db: -20, duck: true }]
  };
  const edlPath = join(root, 'edl.json');
  await writeFile(edlPath, `${JSON.stringify(edl, null, 2)}\n`, 'utf8');
  return { root, edlPath, edl, expectedFrames: 105, expectedSamples: 168000 };
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  generateMediaFixture(process.argv[2] || '.run-data/phase1-media').then(({ edlPath }) => {
    process.stdout.write(`Generated: ${edlPath}\n`);
  }).catch((error) => { process.stderr.write(`${error.message}\n`); process.exitCode = 1; });
}
