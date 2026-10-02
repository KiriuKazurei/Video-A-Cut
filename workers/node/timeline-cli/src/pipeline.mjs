import { createHash } from 'node:crypto';
import { publishNewDirectory } from './publish.mjs';
import { access, copyFile, mkdir, mkdtemp, readFile, realpath, readdir, rm, stat, writeFile } from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { validateEdl } from './edl.mjs';

export class PipelineError extends Error {
  constructor(message) {
    super(message);
    this.name = 'PipelineError';
  }
}

const requiredPorts = ['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles'];

function requirePorts(ports) {
  for (const name of requiredPorts) {
    if (typeof ports?.[name]?.run !== 'function') {
      throw new PipelineError(`missing ${name}.run adapter; no delivery files were produced`);
    }
  }
}

function isInside(root, path) {
  const rel = relative(root, path);
  return rel !== '' && rel !== '..' && !rel.startsWith(`..${sep}`) && !isAbsolute(rel);
}

async function verifyArtifacts(staging, artifacts) {
  if (!Array.isArray(artifacts) || artifacts.length === 0) {
    throw new PipelineError('export adapters must return at least one artifact');
  }
  const seen = new Set();
  const stagingReal = await realpath(staging);
  for (const artifact of artifacts) {
    if (typeof artifact?.kind !== 'string' || !artifact.kind || typeof artifact?.path !== 'string') {
      throw new PipelineError('each artifact needs kind and relative path strings');
    }
    const outputPath = resolve(staging, artifact.path);
    const key = process.platform === 'win32' ? outputPath.toLowerCase() : outputPath;
    if (isAbsolute(artifact.path) || !isInside(staging, outputPath) || seen.has(key)) {
      throw new PipelineError(`unsafe or duplicate artifact path: ${artifact.path}`);
    }
    seen.add(key);
    const actualPath = await realpath(outputPath);
    if (!isInside(stagingReal, actualPath) || !(await stat(actualPath)).isFile()) {
      throw new PipelineError(`artifact must be a regular file inside staging: ${artifact.path}`);
    }
  }
  return artifacts;
}

/**
 * Make the package self-contained: the stored edl.json must reference files
 * inside the package so the package can be re-imported and rebuilt. Video
 * sources already copied by an adapter (declared as source_video) are
 * reused; any other referenced source (game audio from a non-video file,
 * voice, music) is copied under media/. Timing fields are never touched.
 * Without an edlPath (unit tests with synthetic ports) the EDL is stored as is.
 */
async function packageSources({ edl, edlPath, staging, artifacts }) {
  const videoCopies = artifacts.filter((a) => a.kind === 'source_video').map((a) => a.path);
  // Only a real media build (formats copied probed video) has sources to
  // package; synthetic ports keep the EDL untouched.
  if (!edlPath || videoCopies.length === 0) return edl;
  const byOriginal = new Map();
  const videoSources = [...new Set(edl.video.map((item) => item.src))];
  videoSources.forEach((src, index) => {
    const copy = videoCopies.find((p) => p.startsWith(`media/video_${index + 1}_`));
    if (copy) byOriginal.set(src, copy);
  });
  const mediaDir = join(staging, 'media');
  await mkdir(mediaDir, { recursive: true });
  const used = new Set((await readdir(mediaDir)).map((n) => n.toLowerCase()));
  let n = 0;
  const pkg = async (src) => {
    if (byOriginal.has(src)) return byOriginal.get(src);
    const from = isAbsolute(src) ? src : resolve(dirname(edlPath), src);
    let name;
    do { n += 1; name = `src_${n}_${basename(from)}`; } while (used.has(name.toLowerCase()));
    used.add(name.toLowerCase());
    await copyFile(from, join(mediaDir, name));
    const rel = `media/${name}`;
    byOriginal.set(src, rel);
    artifacts.push({ kind: 'packaged_source', path: rel });
    return rel;
  };
  const out = structuredClone(edl);
  for (const track of ['video', 'game_audio', 'voice', 'music']) {
    for (const item of out[track]) item.src = await pkg(item.src);
  }
  return out;
}

async function carryEvidence({ edlPath, staging, artifacts, edl }) {
  if (!edlPath) return;
  const packageRoot = dirname(edlPath);
  const manifestPath = join(packageRoot, 'samples', 'evidence-manifest.json');
  let raw;
  try {
    raw = await readFile(manifestPath, 'utf8');
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    const scenes = Array.isArray(edl?.scenes) ? edl.scenes : [];
    if (scenes.some((scene) => Array.isArray(scene?.evidence_frames) && scene.evidence_frames.length > 0)) {
      throw new PipelineError('scene evidence has no evidence manifest in the source package');
    }
    return;
  }
  const parsed = JSON.parse(raw);
  const frames = Array.isArray(parsed) ? parsed : parsed?.frames;
  if (!Array.isArray(frames)) throw new PipelineError('evidence manifest has no frames list');
  const known = new Set();
  for (const frame of frames) {
    if (!frame || typeof frame.path !== 'string' || !frame.path.startsWith('samples/') || frame.path.includes('..') || frame.path.includes('\\')) {
      throw new PipelineError('evidence frame path is not inside samples/');
    }
    if (typeof frame.sha256 !== 'string' || !/^[0-9a-f]{64}$/.test(frame.sha256)) {
      throw new PipelineError('evidence frame hash is missing');
    }
    const from = resolve(packageRoot, ...frame.path.split('/'));
    if (!isInside(packageRoot, from)) throw new PipelineError('evidence frame escapes the source package');
    const dest = resolve(staging, ...frame.path.split('/'));
    if (!isInside(staging, dest)) throw new PipelineError('evidence frame escapes the delivery package');
    await mkdir(dirname(dest), { recursive: true });
    await copyFile(from, dest);
    const digest = createHash('sha256').update(await readFile(dest)).digest('hex');
    if (digest !== frame.sha256) throw new PipelineError(`evidence frame hash mismatch: ${frame.path}`);
    artifacts.push({ kind: 'sample', path: frame.path });
    known.add(frame.sha256);
    known.add(frame.path);
    if (typeof frame.source_name === 'string' && frame.source_name) known.add(frame.source_name);
    known.add(frame.path.split('/').pop());
  }
  const manifestRel = 'samples/evidence-manifest.json';
  await copyFile(manifestPath, join(staging, manifestRel));
  artifacts.push({ kind: 'evidence_manifest', path: manifestRel });
  for (const scene of edl?.scenes || []) {
    for (const ref of scene?.evidence_frames || []) {
      if (!known.has(ref)) throw new PipelineError(`scene evidence ${ref} is not in the package evidence manifest`);
    }
  }
}

const ingestFiles = [['source_map', 'source-map.json'], ['ingest_provenance', 'ingest-provenance.json']];

/**
 * Packages prepared from a raw recording carry source-map.json and
 * ingest-provenance.json. Copy them into the delivery and write the final
 * timeline mapping, joined by segment_id rather than array position.
 */
async function carryIngest({ edlPath, staging, artifacts, edl }) {
  if (!edlPath) return;
  const packageRoot = dirname(edlPath);
  const tagged = edl.video.filter((item) => item?.ingest && typeof item.ingest === 'object');
  let raw;
  try {
    raw = await readFile(join(packageRoot, 'source-map.json'), 'utf8');
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
    if (tagged.length) throw new PipelineError('video clips carry ingest metadata but the package has no source-map.json');
    return;
  }
  const map = JSON.parse(raw);
  const segments = new Map((map?.segments || []).map((seg) => [seg.segment_id, seg]));
  if (!Array.isArray(map?.segments) || segments.size !== map.segments.length) {
    throw new PipelineError('source-map.json is missing segments or repeats a segment id');
  }
  if (tagged.length !== edl.video.length) throw new PipelineError('every video clip of an ingested package needs its ingest segment_id');
  for (const [kind, name] of ingestFiles) {
    try {
      await copyFile(join(packageRoot, name), join(staging, name));
    } catch (error) {
      if (error.code === 'ENOENT') throw new PipelineError(`ingested package is missing ${name}`);
      throw error;
    }
    artifacts.push({ kind, path: name });
  }
  const rows = edl.video.map((clip) => {
    const seg = segments.get(clip.ingest.segment_id);
    if (!seg) throw new PipelineError(`segment ${clip.ingest.segment_id} is not in source-map.json`);
    // CFR conversion may repeat the last frame to round the output duration.
    // That padding belongs to the output timeline, never to additional source footage.
    const sourceStart = seg.source_start_us + Math.round(clip.in * 1e6);
    const sourceEnd = Math.min(seg.source_end_us, seg.source_start_us + Math.round(clip.out * 1e6));
    if (!Number.isSafeInteger(seg.source_start_us) || !Number.isSafeInteger(seg.source_end_us)
        || seg.source_start_us < 0 || sourceStart >= sourceEnd) {
      throw new PipelineError(`segment ${clip.ingest.segment_id} has an invalid source range`);
    }
    return {
      segment_id: seg.segment_id,
      src: clip.src,
      timeline_in: clip.timeline_in,
      timeline_out: Math.round((clip.timeline_in + clip.out - clip.in) * 1e6) / 1e6,
      source_start_us: sourceStart,
      source_end_us: sourceEnd
    };
  });
  const timeline = { schema_version: 1, source_id: map.source_id, source_sha256: map.source_sha256, fps: edl.timeline.fps, segments: rows };
  await writeFile(join(staging, 'ingest-timeline.json'), `${JSON.stringify(timeline, null, 2)}\n`, 'utf8');
  artifacts.push({ kind: 'ingest_timeline', path: 'ingest-timeline.json' });
}

/**
 * Pipeline contract. Ports own media/cut/frame/track/export semantics; this runner
 * owns ordering, fail-closed verification and atomic publication of a new package.
 */
export async function buildDelivery({ edl, edlPath, outputDir, ports }) {
  validateEdl(edl);
  requirePorts(ports);
  if (typeof outputDir !== 'string' || !outputDir.trim()) throw new PipelineError('outputDir is required');
  const target = resolve(outputDir);
  try {
    await access(target);
    throw new PipelineError(`output directory already exists: ${target}`);
  } catch (error) {
    if (error.code !== 'ENOENT') throw error;
  }

  const parent = dirname(target);
  await mkdir(parent, { recursive: true });
  const staging = await mkdtemp(join(parent, `.${basename(target)}-staging-`));
  try {
    const context = { edl, edlPath: edlPath ? resolve(edlPath) : undefined, stagingDir: staging, outputDir: target };
    const cuts = await ports.cuts.run(context);
    const timebase = await ports.timebase.run({ ...context, cuts });
    const timeline = await ports.tracks.run({ ...context, cuts, timebase });
    const verification = await ports.sync.run({ ...context, cuts, timebase, timeline });
    if (verification?.ok !== true || !Array.isArray(verification.issues)) {
      throw new PipelineError('sync verification did not pass or return issues array');
    }
    const exported = await ports.formats.run({ ...context, timeline, verification });
    const audio = await ports.audio.run({ ...context, timeline, verification });
    const subtitles = await ports.subtitles.run({ ...context, timeline, verification });
    if (![exported, audio, subtitles].every(Array.isArray)) {
      throw new PipelineError('export adapters must return artifact arrays');
    }
    const artifacts = await verifyArtifacts(staging, [...exported, ...audio, ...subtitles]);
    if (artifacts.some((item) => ['delivery-manifest.json', 'edl.json'].includes(item.path))) {
      throw new PipelineError('edl.json and delivery-manifest.json are reserved for the pipeline');
    }
    const packagedEdl = await packageSources({ edl, edlPath: context.edlPath, staging, artifacts });
    await carryEvidence({ edlPath: context.edlPath, staging, artifacts, edl: packagedEdl });
    await carryIngest({ edlPath: context.edlPath, staging, artifacts, edl: packagedEdl });
    await writeFile(join(staging, 'edl.json'), `${JSON.stringify(packagedEdl, null, 2)}\n`, 'utf8');
    const manifest = {
      schema_version: 1,
      source_edl: 'edl.json',
      source_name: edlPath ? basename(edlPath) : null,
      status: 'ready_for_import_validation',
      artifacts: [{ kind: 'edl', path: 'edl.json' }, ...artifacts],
      checks: verification
    };
    await writeFile(join(staging, 'delivery-manifest.json'), `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
    // A new target is required: never replace an existing delivery package.
    if ((await readdir(staging)).length < 2) throw new PipelineError('delivery package is empty');
    await publishNewDirectory(staging, target);
    return manifest;
  } catch (error) {
    await rm(staging, { recursive: true, force: true });
    throw error;
  }
}
