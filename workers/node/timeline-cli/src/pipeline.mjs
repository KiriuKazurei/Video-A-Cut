import { access, copyFile, mkdir, mkdtemp, realpath, readdir, rename, rm, stat, writeFile } from 'node:fs/promises';
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
    await rename(staging, target);
    return manifest;
  } catch (error) {
    await rm(staging, { recursive: true, force: true });
    throw error;
  }
}
