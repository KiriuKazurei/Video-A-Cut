import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash, randomUUID } from 'node:crypto';
import { spawn } from 'node:child_process';
import { access, mkdir, mkdtemp, readFile, readdir, rm, symlink, unlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  renderPreview,
  buildPreviewInputFromPackage,
  findFontFile,
  probePreviewMedia,
  PreviewError
} from '../src/preview.mjs';
import { loadPreviewPreset, fingerprintPreviewInput } from '../src/preview-protocol.mjs';
import { resolveFonts } from '../src/fonts.mjs';

function runProcess(cmd, args, options = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, { windowsHide: true, ...options });
    const stdoutParts = [];
    let stderr = '';
    child.stdout?.on('data', (d) => { stdoutParts.push(d); });
    child.stderr?.on('data', (d) => { stderr += d; });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        const stdoutBytes = Buffer.concat(stdoutParts);
        resolve({ stdout: stdoutBytes.toString('utf8'), stdoutBytes, stderr });
      }
      else reject(new Error(`${cmd} exited with code ${code}: ${stderr.trim()}`));
    });
  });
}

/**
 * Creates a minimal synthetic delivery package (<500KB total) for fast, real FFmpeg testing.
 */
async function createTestPackage(dir, options = {}) {
  await mkdir(dir, { recursive: true });

  // 1. Generate 1.0s video clips (320x180, 30fps)
  const clip1Path = join(dir, 'clip_01.mp4');
  const clip2Path = join(dir, 'clip_02.mp4');
  await runProcess('ffmpeg', [
    '-hide_banner', '-y',
    '-f', 'lavfi', '-i', options.videoLavfi || 'smptebars=size=320x180:rate=30:duration=1.0',
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-r', '30',
    clip1Path
  ]);
  await runProcess('ffmpeg', [
    '-hide_banner', '-y',
    '-f', 'lavfi', '-i', options.clip2Lavfi || `testsrc2=size=${options.clip2Size || '320x180'}:rate=30:duration=1.0`,
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-r', '30',
    clip2Path
  ]);

  // 2. Generate three 1.0s audio WAV stems (48kHz stereo)
  const gaPath = join(dir, 'audio_a1.wav');
  const voPath = join(dir, 'audio_a2.wav');
  const muPath = join(dir, 'audio_a3.wav');
  await runProcess('ffmpeg', [
    '-hide_banner', '-y',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=1.0',
    '-ac', '2', '-c:a', 'pcm_s16le', gaPath
  ]);
  await runProcess('ffmpeg', [
    '-hide_banner', '-y',
    '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000:duration=1.0',
    '-ac', '2', '-c:a', 'pcm_s16le', voPath
  ]);
  await runProcess('ffmpeg', [
    '-hide_banner', '-y',
    '-f', 'lavfi', '-i', 'sine=frequency=220:sample_rate=48000:duration=1.0',
    '-ac', '2', '-c:a', 'pcm_s16le', muPath
  ]);

  // 3. Subtitles (optional)
  const artifacts = [
    { kind: 'edl', path: 'edl.json' },
    { kind: 'source_video', path: 'clip_01.mp4' },
    { kind: 'source_video', path: 'clip_02.mp4' },
    { kind: 'game_audio', path: 'audio_a1.wav' },
    { kind: 'voice', path: 'audio_a2.wav' },
    { kind: 'music', path: 'audio_a3.wav' }
  ];

  if (options.subtitles) {
    const srtContent = '1\n00:00:00,100 --> 00:00:00,800\n测试中文字幕\n\n';
    await writeFile(join(dir, 'subtitles.srt'), srtContent, 'utf8');
    artifacts.push({ kind: 'subtitles', path: 'subtitles.srt' });
  }

  // 4. EDL
  const edl = {
    timeline: { fps: 30, sample_rate: 48000 },
    video: options.video || [
      { src: 'clip_01.mp4', in: 0, out: 0.5, timeline_in: 0 },
      { src: 'clip_02.mp4', in: 0.1, out: 0.6, timeline_in: 0.8 } // Intermediate gap: 0.5s to 0.8s
    ],
    game_audio: [{ src: 'clip_01.mp4', in: 0, out: 0.5, timeline_in: 0, gain_db: 0 }],
    voice: [{ id: 'vo_001', src: 'voice.wav', start: 0.2, end: 0.8, gain_db: 0 }],
    subtitle: options.subtitles ? [{ id: 'sub_001', text: '测试中文字幕', start: 0.1, end: 0.8 }] : [],
    music: [{ id: 'bgm_001', src: 'music.wav', start: 0, end: 1.0, gain_db: -10, duck: false }]
  };
  await writeFile(join(dir, 'edl.json'), `${JSON.stringify(edl, null, 2)}\n`, 'utf8');

  // 5. Delivery manifest
  const deliveryManifest = {
    schema_version: 1,
    source_edl: 'edl.json',
    status: 'ready_for_import_validation',
    artifacts,
    checks: { ok: true, issues: [] }
  };
  await writeFile(join(dir, 'delivery-manifest.json'), `${JSON.stringify(deliveryManifest, null, 2)}\n`, 'utf8');

  return { edl, deliveryManifest };
}

test('real FFmpeg preview rendering without subtitles creates compliant MP4, report and receipt', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-render-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });

  const packageDir = join(root, 'package');
  const outputDir = join(root, 'preview_out');

  // Clip 1: 0 to 0.5s = 15 frames
  // Intermediate gap: 0.5s to 0.8s = 9 frames
  // Clip 2: in 0.1 to 0.6 = 15 frames, timeline 0.8 to 1.3s = frames 24 to 39
  // Total frames = 39 frames
  await createTestPackage(packageDir, { subtitles: false });

  const { input } = await buildPreviewInputFromPackage({ packageDir });
  const progressEvents = [];
  const manifest = await renderPreview({
    input,
    packageDir,
    outputDir,
    onProgress: (evt) => { progressEvents.push({ ...evt }); }
  });

  // Verify progress callbacks passed through all phases
  const phases = progressEvents.map((e) => e.phase);
  assert.ok(phases.includes('preparing'));
  assert.ok(phases.includes('rendering'));
  assert.ok(phases.includes('verifying'));
  assert.ok(phases.includes('publishing'));

  // Verify manifest
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.preview_id, input.preview_id);
  assert.equal(manifest.task_id, input.task_id);
  assert.equal(manifest.input_sha256, input.input_sha256);
  assert.equal(manifest.preset_sha256, input.preset_sha256);
  assert.equal(manifest.artifacts.length, 3);

  const keys = manifest.artifacts.map((a) => a.key).sort();
  assert.deepEqual(keys, ['mp4', 'receipt', 'report']);

  // Verify output files on disk
  const finalMp4 = join(outputDir, 'final.mp4');
  const reportPath = join(outputDir, 'render-report.json');
  const receiptPath = join(outputDir, 'preview-receipt.json');
  const manifestPath = join(outputDir, 'preview-manifest.json');

  await access(finalMp4);
  await access(reportPath);
  await access(receiptPath);
  await access(manifestPath);

  // Probe rendered MP4
  const media = await probePreviewMedia(finalMp4);
  assert.equal(media.video_codec, 'h264');
  assert.equal(media.audio_codec, 'aac');
  assert.equal(media.width, 320);
  assert.equal(media.height, 180);
  assert.equal(media.fps, 30);
  assert.equal(media.frame_count, 39);
  assert.equal(media.sample_rate, 48000);
  assert.equal(media.channels, 2);

  // Decode pixels independently: both cuts contain imagery, while frames 15–23 are black.
  const { stdoutBytes: samples } = await runProcess('ffmpeg', [
    '-v', 'error', '-i', finalMp4,
    '-vf', 'select=eq(n\\,5)+eq(n\\,18)+eq(n\\,30),scale=1:1,format=gray',
    '-fps_mode', 'passthrough', '-frames:v', '3', '-f', 'rawvideo', 'pipe:1'
  ]);
  assert.equal(samples.length, 3);
  assert.ok(samples[0] > 20 && samples[2] > 20, 'cut frames must contain source imagery');
  assert.ok(samples[1] <= 10, 'intermediate gap frame must be black');

  // Verify render-report.json
  const report = JSON.parse(await readFile(reportPath, 'utf8'));
  assert.equal(report.schema_version, 1);
  assert.equal(report.input_sha256, input.input_sha256);
  assert.equal(report.target_frames, 39);
  assert.equal(report.target_samples, Math.round((39 * 48000) / 30));
  assert.equal(report.checks.length, 5);
  for (const c of report.checks) {
    assert.equal(c.status, 'passed', `Check ${c.code} should pass`);
  }
  const subCheck = report.checks.find((c) => c.code === 'subtitles');
  assert.match(subCheck.message, /No subtitles present/);

  // Verify preview-receipt.json
  const receipt = JSON.parse(await readFile(receiptPath, 'utf8'));
  assert.equal(receipt.schema_version, 1);
  assert.equal(receipt.preview_id, input.preview_id);
  assert.equal(receipt.input_sha256, input.input_sha256);
  assert.equal(receipt.preset_sha256, input.preset_sha256);
  assert.ok(receipt.renderer_version);
  assert.ok(receipt.ffmpeg_version);
  assert.equal(receipt.font, undefined, 'receipt should omit font when subtitles not present');
});

test('real FFmpeg preview rendering WITH subtitles burns Chinese font and records font metadata', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-subtitles-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });

  const packageDir = join(root, 'package');
  const outputDir = join(root, 'preview_out');

  await createTestPackage(packageDir, { subtitles: true });

  const { input } = await buildPreviewInputFromPackage({ packageDir });
  assert.ok(input.files.some((f) => f.kind === 'srt'));

  const manifest = await renderPreview({
    input,
    packageDir,
    outputDir
  });

  assert.equal(manifest.artifacts.length, 3);
  const receiptPath = join(outputDir, 'preview-receipt.json');
  const receipt = JSON.parse(await readFile(receiptPath, 'utf8'));

  // Font must be recorded
  assert.ok(receipt.font);
  assert.equal(receipt.font.family, 'Microsoft YaHei');
  assert.match(receipt.font.sha256, /^[0-9a-f]{64}$/);

  const reportPath = join(outputDir, 'render-report.json');
  const report = JSON.parse(await readFile(reportPath, 'utf8'));
  const subCheck = report.checks.find((c) => c.code === 'subtitles');
  assert.equal(subCheck.status, 'passed');
  assert.match(subCheck.message, /Microsoft YaHei/);

  // Temporary subtitles.srt must have been cleaned up before publish
  await assert.rejects(access(join(outputDir, 'subtitles.srt')), { code: 'ENOENT' });
});

test('timeline with leading gap correctly pads black frames from frame 0', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-leading-gap-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });

  const packageDir = join(root, 'package');
  const outputDir = join(root, 'preview_out');

  // Leading gap: timeline_in = 0.4s (12 frames gap from 0 to 12)
  // Clip: in = 0, out = 0.5 (15 frames) -> ends at frame 27
  // Total frames: 27
  await createTestPackage(packageDir, {
    subtitles: false,
    video: [
      { src: 'clip_01.mp4', in: 0, out: 0.5, timeline_in: 0.4 }
    ]
  });

  const { input } = await buildPreviewInputFromPackage({ packageDir });
  await renderPreview({ input, packageDir, outputDir });

  const media = await probePreviewMedia(join(outputDir, 'final.mp4'));
  assert.equal(media.frame_count, 27);
  assert.equal(media.fps, 30);

  const report = JSON.parse(await readFile(join(outputDir, 'render-report.json'), 'utf8'));
  assert.equal(report.target_frames, 27);
  assert.equal(report.target_samples, Math.round((27 * 48000) / 30));
});

test('abort signal immediately stops rendering and cleans up staging without publishing', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-abort-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });

  const packageDir = join(root, 'package');
  const outputDir = join(root, 'preview_out');
  await createTestPackage(packageDir, { subtitles: false });

  const { input } = await buildPreviewInputFromPackage({ packageDir });
  const controller = new AbortController();
  const cancelReason = new Error('cancelled by test');

  // Trigger abort shortly after render starts
  setTimeout(() => {
    controller.abort(cancelReason);
  }, 30);

  await assert.rejects(
    renderPreview({ input, packageDir, outputDir, signal: controller.signal }),
    (err) => err === cancelReason || err?.code === 'aborted'
  );

  // Destination directory was never published
  await assert.rejects(access(outputDir), { code: 'ENOENT' });
});

test('cancelling active FFmpeg encoding waits for exit and removes staging', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-encoding-abort-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const packageDir = join(root, 'package'), outputDir = join(root, 'preview_out');
  await createTestPackage(packageDir);
  const { input } = await buildPreviewInputFromPackage({ packageDir });
  const controller = new AbortController(), reason = new Error('cancel during encoding');
  let encodingStarted = false;
  await assert.rejects(renderPreview({ input, packageDir, outputDir, signal: controller.signal,
    onProgress: event => {
      if (event.phase === 'rendering') { encodingStarted = true; controller.abort(reason); }
    }
  }), error => error === reason);
  assert.equal(encodingStarted, true);
  await assert.rejects(access(outputDir), { code: 'ENOENT' });
  assert.deepEqual(await readdir(root), ['package']);
});

test('validation rejects tampered input, missing files, overlapping clips, or existing output directory', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-validate-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });

  const packageDir = join(root, 'package');
  const outputDir = join(root, 'preview_out');
  await createTestPackage(packageDir, { subtitles: false });

  const { input } = await buildPreviewInputFromPackage({ packageDir });

  // 1. Input fingerprint mismatch
  const tamperedInput = structuredClone(input);
  tamperedInput.input_sha256 = 'f'.repeat(64);
  await assert.rejects(
    renderPreview({ input: tamperedInput, packageDir, outputDir }),
    (err) => err instanceof PreviewError && err.code === 'fingerprint_mismatch'
  );

  // 2. Preset sha256 mismatch
  const tamperedPreset = structuredClone(input);
  tamperedPreset.preset_sha256 = 'e'.repeat(64);
  await assert.rejects(
    renderPreview({ input: tamperedPreset, packageDir, outputDir }),
    (err) => err instanceof PreviewError && err.code === 'preset_mismatch'
  );

  // 3. Non-existent package directory
  await assert.rejects(
    renderPreview({ input, packageDir: join(root, 'nonexistent'), outputDir }),
    (err) => err instanceof PreviewError && err.code === 'package_not_found'
  );

  // 4. Missing required file
  const missingFileInput = structuredClone(input);
  missingFileInput.files = input.files.filter((f) => f.kind !== 'game_audio');
  missingFileInput.input_sha256 = fingerprintPreviewInput(missingFileInput);
  await assert.rejects(
    renderPreview({ input: missingFileInput, packageDir, outputDir }),
    (err) => err instanceof PreviewError && err.code === 'missing_audio_stems'
  );

  // 5. Tampered file hash on disk
  const tamperedHashInput = structuredClone(input);
  tamperedHashInput.files[0].sha256 = 'a'.repeat(64);
  tamperedHashInput.input_sha256 = fingerprintPreviewInput(tamperedHashInput);
  await assert.rejects(
    renderPreview({ input: tamperedHashInput, packageDir, outputDir }),
    (err) => err instanceof PreviewError && err.code === 'hash_mismatch'
  );

  // 6. Overlapping clips
  const overlapDir = join(root, 'overlap_package');
  await createTestPackage(overlapDir, {
    video: [
      { src: 'clip_01.mp4', in: 0, out: 0.5, timeline_in: 0 },
      { src: 'clip_02.mp4', in: 0, out: 0.5, timeline_in: 0.3 } // Overlaps clip 1 (which ends at 0.5)
    ]
  });
  const { input: overlapInput } = await buildPreviewInputFromPackage({ packageDir: overlapDir });
  await assert.rejects(
    renderPreview({ input: overlapInput, packageDir: overlapDir, outputDir }),
    (err) => err instanceof PreviewError && err.code === 'overlapping_clips'
  );

  // 7. Output directory already exists
  await mkdir(outputDir, { recursive: true });
  await assert.rejects(
    renderPreview({ input, packageDir, outputDir }),
    (err) => err?.code === 'EEXIST'
  );
});

test('preview rejects video sources outside the frozen package before probing them', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-source-boundary-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const packageDir = join(root, 'package');
  await createTestPackage(packageDir, { video: [{ src: '../foreign.mp4', in: 0, out: 0.5, timeline_in: 0 }] });
  await writeFile(join(root, 'foreign.mp4'), await readFile(join(packageDir, 'clip_01.mp4')));
  const { input } = await buildPreviewInputFromPackage({ packageDir });
  await assert.rejects(renderPreview({input, packageDir, outputDir:join(root,'out')}), { code: 'invalid_video_source' });
});

test('unsorted mixed-size clips use the earliest clip canvas in real encoding', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-unsorted-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const packageDir = join(root, 'package'), outputDir = join(root, 'out');
  await createTestPackage(packageDir, { clip2Size: '640x360', video: [
    { src: 'clip_02.mp4', in: 0, out: 0.5, timeline_in: 0.5 },
    { src: 'clip_01.mp4', in: 0, out: 0.5, timeline_in: 0 }
  ] });
  const { input } = await buildPreviewInputFromPackage({ packageDir });
  await renderPreview({ input, packageDir, outputDir });
  const media = await probePreviewMedia(join(outputDir, 'final.mp4'));
  assert.equal(media.width, 320); assert.equal(media.height, 180);
  assert.equal(media.frame_count, 30);
});

test('renderer rejects a redirected output parent without writing into its target', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-output-boundary-'));
  const link = join(root, 'redirected');
  t.after(async () => {
    await unlink(link).catch(error => { if (error.code !== 'ENOENT') throw error; });
    await rm(root, { recursive: true, force: true });
  });
  const packageDir = join(root, 'package'), target = join(root, 'target');
  await createTestPackage(packageDir); await mkdir(target);
  await symlink(target, link, process.platform === 'win32' ? 'junction' : 'dir');
  const { input } = await buildPreviewInputFromPackage({ packageDir });
  await assert.rejects(renderPreview({ input, packageDir, outputDir: join(link, 'preview') }), { code: 'invalid_output_dir' });
  assert.deepEqual(await readdir(target), []);
});

test('CLI render-preview command renders preview end-to-end', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-cli-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });

  const packageDir = join(root, 'package');
  const cliOutputDir = join(root, 'cli_preview_out');
  await createTestPackage(packageDir, { subtitles: false });

  const cliPath = fileURLToPath(new URL('../src/cli.mjs', import.meta.url));
  const { stdout } = await runProcess(process.execPath, [
    cliPath, 'render-preview',
    '--package', packageDir,
    '--output', cliOutputDir,
    '--preset', 'mp4-720p-v1'
  ]);

  assert.match(stdout, /Preview rendered:/);
  assert.match(stdout, /final\.mp4/);
  assert.match(stdout, /3 artifacts declared/);

  await access(join(cliOutputDir, 'final.mp4'));
  await access(join(cliOutputDir, 'preview-manifest.json'));
  await access(join(cliOutputDir, 'render-report.json'));
  await access(join(cliOutputDir, 'preview-receipt.json'));
});

test('schema 2 burns only the frozen ASS, keeps cue color, placement, fade and font hashes', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-preview-ass-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });
  const packageDir = join(root, 'pkg files');
  const subtitleDir = join(root, 'sub pack');
  const outputDir = join(root, 'preview out');
  await createTestPackage(packageDir, {
    subtitles: true,
    videoLavfi: 'color=c=black:s=320x180:r=30:d=1',
    clip2Lavfi: 'color=c=black:s=320x180:r=30:d=1'
  });
  await writeFile(join(packageDir, 'subtitles.srt'), '1\n00:00:00,050 --> 00:00:00,300\nSHOULD_NOT_BURN\n\n', 'utf8');
  let families = ['Microsoft YaHei'];
  try {
    await resolveFonts(['Microsoft YaHei', 'Arial']);
    families = ['Microsoft YaHei', 'Arial'];
  } catch { /* The second cue uses the same installed family when Arial is absent. */ }
  const [bottomFamily, topFamily] = [families[0], families[1] ?? families[0]];
  const ass = [
    '[Script Info]',
    'ScriptType: v4.00+',
    'PlayResX: 320',
    'PlayResY: 180',
    'WrapStyle: 2',
    'ScaledBorderAndShadow: yes',
    '',
    '[V4+ Styles]',
    'Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding',
    `Style: Bottom,${bottomFamily},64,&H000000FF,&H000000FF,&H00000000,&H00000000,-1,-1,-1,0,100,100,0,0,1,0,0,2,8,8,2,1`,
    `Style: Top,${topFamily},64,&H00FF0000,&H00FF0000,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,8,8,2,1`,
    '',
    '[Events]',
    'Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text',
    'Dialogue: 0,0:00:00.40,0:00:00.95,Bottom,,0,0,0,,{\\fad(300,0)}中中中中',
    'Dialogue: 0,0:00:00.40,0:00:00.95,Top,,0,0,0,,中中中中',
    ''
  ].join('\n');
  await mkdir(subtitleDir);
  await writeFile(join(subtitleDir, 'styled.ass'), ass, 'utf8');
  const { input: base } = await buildPreviewInputFromPackage({ packageDir });
  const assBytes = Buffer.from(ass, 'utf8');
  const styled = {
    ...base,
    schema_version: 2,
    subtitle_variant: {
      variant_id: 'dv_styled',
      package_ref: 'delivery-variants/asset/dv_styled',
      ass: { kind: 'ass', path: 'styled.ass', size: assBytes.length, sha256: createHash('sha256').update(assBytes).digest('hex') },
      style_sha256: 'b'.repeat(64),
      font_families: families
    }
  };
  styled.input_sha256 = fingerprintPreviewInput(styled);
  await renderPreview({ input: styled, packageDir, subtitlePackageDir: subtitleDir, outputDir });

  const names = (await readdir(outputDir)).sort();
  assert.deepEqual(names, ['final.mp4', 'preview-manifest.json', 'preview-receipt.json', 'render-report.json']);
  const receipt = JSON.parse(await readFile(join(outputDir, 'preview-receipt.json'), 'utf8'));
  assert.equal(receipt.schema_version, 1);
  assert.equal(receipt.input_sha256, styled.input_sha256);
  assert.equal(receipt.font, undefined);
  for (const family of families) assert.ok(receipt.fonts.some(font => font.family === family));
  assert.ok(receipt.fonts.every(font => families.includes(font.family)));
  for (const font of receipt.fonts) assert.match(font.sha256, /^[0-9a-f]{64}$/);
  assert.doesNotMatch(JSON.stringify(receipt), /[A-Za-z]:\\|\\\\|sub pack|styled\.ass/);
  const report = JSON.parse(await readFile(join(outputDir, 'render-report.json'), 'utf8'));
  assert.equal(report.schema_version, 1);
  assert.match(report.checks.find(check => check.code === 'subtitles').message, /ASS burned/);
  assert.doesNotMatch(report.checks.find(check => check.code === 'subtitles').message, /SHOULD_NOT_BURN|subtitles\.srt/);

  const frame = async (seconds) => {
    const { stdoutBytes } = await runProcess('ffmpeg', [
      '-hide_banner', '-i', join(outputDir, 'final.mp4'), '-ss', String(seconds),
      '-frames:v', '1', '-f', 'rawvideo', '-pix_fmt', 'rgb24', 'pipe:1'
    ]);
    assert.equal(stdoutBytes.length, 320 * 180 * 3);
    return stdoutBytes;
  };
  const mean = (buf, y0, y1, channel) => {
    let sum = 0;
    let count = 0;
    for (let y = y0; y < y1; y += 1) {
      for (let x = 40; x < 280; x += 1) {
        sum += buf[(y * 320 + x) * 3 + channel];
        count += 1;
      }
    }
    return sum / count;
  };
  const quiet = await frame(0.12);
  assert.ok(mean(quiet, 140, 175, 0) < 8, 'SRT window must stay empty when only ASS is burned');
  const fading = await frame(0.48);
  const opaque = await frame(0.82);
  const bottomFade = mean(fading, 140, 175, 0);
  const bottomOpaque = mean(opaque, 140, 175, 0);
  const topBlue = mean(opaque, 4, 40, 2);
  assert.ok(bottomOpaque > 25 && bottomOpaque > mean(opaque, 140, 175, 1) + 10, 'bottom cue is red');
  assert.ok(topBlue > 25 && topBlue > mean(opaque, 4, 40, 0) + 10, 'top cue is blue');
  assert.ok(bottomFade < bottomOpaque * 0.75, 'fade-in is darker than the settled cue');

  const mismatch = { ...styled, subtitle_variant: { ...styled.subtitle_variant, ass: { ...styled.subtitle_variant.ass, sha256: 'c'.repeat(64) } } };
  mismatch.input_sha256 = fingerprintPreviewInput(mismatch);
  await assert.rejects(renderPreview({ input: mismatch, packageDir, subtitlePackageDir: subtitleDir, outputDir: join(root, 'bad-hash') }), { code: 'hash_mismatch' });
  const missing = { ...styled, subtitle_variant: { ...styled.subtitle_variant, font_families: ['Unavailable Preview Font'] } };
  missing.input_sha256 = fingerprintPreviewInput(missing);
  await assert.rejects(
    renderPreview({ input: missing, packageDir, subtitlePackageDir: subtitleDir, outputDir: join(root, 'missing-font') }),
    (error) => error.code === 'font_not_found' && /字体不可用：Unavailable Preview Font/.test(error.message)
  );
});
