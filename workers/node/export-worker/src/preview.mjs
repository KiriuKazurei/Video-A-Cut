import { spawn } from 'node:child_process';
import { lstat, open, realpath } from 'node:fs/promises';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { ToolError } from './mcp.mjs';
import { WorkerFailure } from './worker.mjs';
import {
  PREVIEW_PROTOCOL_VERSION,
  PREVIEW_PRESET_ID,
  PREVIEW_FILENAMES,
  isPreviewRelativePath,
  loadPreviewPreset,
} from '../../timeline-cli/src/preview-protocol.mjs';
import { findFontFile, renderPreview } from '../../timeline-cli/src/preview.mjs';

/** Legacy placeholder error class kept for backward compatibility. */
export class PreviewWorkerNotImplementedError extends Error {
  constructor() {
    super('MP4 preview Worker is not implemented; implement Agent C in the framework handoff.');
    this.name = 'PreviewWorkerNotImplementedError';
    this.code = 'preview_worker_not_implemented';
  }
}

/** Check execution of a CLI tool with a timeout. */
function checkTool(cmd, args = ['-version'], timeoutMs = 3000) {
  return new Promise((resolve) => {
    let stdout = '';
    const child = spawn(cmd, args, { windowsHide: true, stdio: ['ignore', 'pipe', 'ignore'] });
    const timer = setTimeout(() => {
      child.kill();
      resolve(null);
    }, timeoutMs);
    child.stdout?.on('data', (d) => { stdout += d; });
    child.on('error', () => {
      clearTimeout(timer);
      resolve(null);
    });
    child.on('close', (code) => {
      clearTimeout(timer);
      if (code === 0) {
        resolve(stdout.trim().split('\n')[0] || 'available');
      } else {
        resolve(null);
      }
    });
  });
}

/**
 * Probe host capabilities for rendering previews.
 * Checks FFmpeg, ffprobe, mp4-720p-v1 preset, and subtitle font.
 * Supports dependency injection options for testing.
 * @param {{
 *   hasSubtitles?: boolean,
 *   ffmpegCheck?: () => Promise<string | boolean | null>,
 *   ffprobeCheck?: () => Promise<string | boolean | null>,
 *   loadPresetImpl?: typeof loadPreviewPreset,
 *   findFontImpl?: typeof findFontFile,
 * }} [options]
 * @returns {Promise<import('../../../../contracts/mp4-preview/v1/contracts').PreviewCapability>}
 */
export async function probePreviewCapability(options = {}) {
  const {
    hasSubtitles = true,
    ffmpegCheck,
    ffprobeCheck,
    loadPresetImpl = loadPreviewPreset,
    findFontImpl = findFontFile,
  } = options;

  const checks = [];
  const presets = [];

  // 1. Check FFmpeg
  let ffmpegVersion = null;
  if (ffmpegCheck) {
    try {
      const res = await ffmpegCheck();
      if (res) ffmpegVersion = typeof res === 'string' ? res : 'available';
    } catch (_) {}
  } else {
    ffmpegVersion = await checkTool('ffmpeg');
  }

  if (ffmpegVersion) {
    checks.push({ code: 'ffmpeg', status: 'passed', message: `FFmpeg is available (${ffmpegVersion.trim()})` });
  } else {
    checks.push({ code: 'ffmpeg', status: 'blocked', message: 'FFmpeg executable is not available or timed out' });
  }

  // 2. Check ffprobe
  let ffprobeVersion = null;
  if (ffprobeCheck) {
    try {
      const res = await ffprobeCheck();
      if (res) ffprobeVersion = typeof res === 'string' ? res : 'available';
    } catch (_) {}
  } else {
    ffprobeVersion = await checkTool('ffprobe');
  }

  if (ffprobeVersion) {
    checks.push({ code: 'ffprobe', status: 'passed', message: `ffprobe is available (${ffprobeVersion.trim()})` });
  } else {
    checks.push({ code: 'ffprobe', status: 'blocked', message: 'ffprobe executable is not available or timed out' });
  }

  // 3. Check preset mp4-720p-v1
  try {
    const loaded = await loadPresetImpl(PREVIEW_PRESET_ID);
    if (loaded?.sha256 && loaded.preset) {
      presets.push({ preset_id: PREVIEW_PRESET_ID, sha256: loaded.sha256 });
      checks.push({ code: 'preset', status: 'passed', message: `Preset ${PREVIEW_PRESET_ID} verified` });
    } else {
      checks.push({ code: 'preset', status: 'blocked', message: `Preset ${PREVIEW_PRESET_ID} invalid` });
    }
  } catch (err) {
    checks.push({ code: 'preset', status: 'blocked', message: `Failed to load preset ${PREVIEW_PRESET_ID}: ${err.message}` });
  }

  // 4. Check subtitle font
  if (hasSubtitles === false) {
    checks.push({ code: 'font', status: 'passed', message: 'No subtitles required; font check bypassed' });
  } else {
    try {
      const font = await findFontImpl('Microsoft YaHei');
      if (font?.path) {
        checks.push({ code: 'font', status: 'passed', message: `Subtitle font available: ${font.family || 'Microsoft YaHei'}` });
      } else {
        checks.push({ code: 'font', status: 'blocked', message: 'Required subtitle font (Microsoft YaHei) not found' });
      }
    } catch (err) {
      checks.push({ code: 'font', status: 'blocked', message: `Error checking subtitle font: ${err.message}` });
    }
  }

  const ready = checks.every((c) => c.status === 'passed');
  return {
    subtitle_formats: ['srt', 'ass'],
    protocol_version: PREVIEW_PROTOCOL_VERSION,
    execution_protocol: 2,
    ready,
    presets: ready ? presets : [],
    checks
  };
}

// Existing package/output components must be directories without redirection.
export async function previewDirectory(root, rel, allowMissing = false) {
  if (!isPreviewRelativePath(rel)) throw new WorkerFailure(`invalid preview directory: ${rel}`);
  let cursor = await realpath(root);
  for (const part of rel.split('/')) {
    cursor = join(cursor, part);
    try {
      const info = await lstat(cursor);
      if (info.isSymbolicLink() || !info.isDirectory()) {
        throw new WorkerFailure(`preview directory contains a redirected or invalid component: ${rel}`);
      }
    } catch (err) {
      if (!allowMissing || err.code !== 'ENOENT') throw err;
    }
  }
  return cursor;
}

export async function recoveryJSON(path) {
  const info = await lstat(path);
  if (info.isSymbolicLink() || !info.isFile()) throw new WorkerFailure('preview recovery JSON is not a regular file');
  const limit = 1024 * 1024;
  if (info.size > limit) throw new WorkerFailure('preview recovery JSON exceeds limit');
  const file = await open(path, 'r');
  try {
    const buffer = Buffer.alloc(limit + 1);
    let size = 0;
    while (size < buffer.length) {
      const { bytesRead } = await file.read(buffer, size, buffer.length - size, null);
      if (!bytesRead) break;
      size += bytesRead;
    }
    if (size > limit) throw new WorkerFailure('preview recovery JSON exceeds limit');
    return JSON.parse(buffer.subarray(0, size).toString('utf8'));
  } finally { await file.close(); }
}

const safeDirectoryID = value => typeof value === 'string' && /^[A-Za-z0-9._-]+$/.test(value) && !/^\.+$/.test(value);
const leaseLoss = err => err instanceof ToolError && ['lease_expired', 'not_found', 'invalid_state', 'conflict', 'stale_execution', 'instance_conflict', 'obsolete', 'forbidden'].includes(err.code);
const transientRegistrationError = err => !(err instanceof ToolError) || err.code === 'internal';

/** Process a claimed preview task, including publish-before-register recovery. */
export async function processPreviewTask(options = {}) {
  const {
    client,
    cfg,
    task,
    executionScope,
    log = () => {},
    signal,
    renderPreviewImpl = renderPreview,
    previewCapability,
    deliveryVariantCapability,
  } = options;

  signal?.throwIfAborted();

  if (!task || typeof task !== 'object') {
    throw new WorkerFailure('task is required');
  }

  const id = task.task_id;
  if (!executionScope || executionScope.task_id !== id || !safeDirectoryID(executionScope.runtime_instance_id) ||
      !safeDirectoryID(executionScope.execution_id) || !Number.isSafeInteger(executionScope.generation) || executionScope.generation < 1) {
    throw new WorkerFailure('preview claim is missing execution scope; upgrade the control plane');
  }
  const scoped = args => ({ ...args, runtime_instance_id: executionScope.runtime_instance_id,
    execution_id: executionScope.execution_id, generation: executionScope.generation });
  let leaseLost = false;
  let remoteStatus = '';
  let publishedPackage = '';
  let recovered = false;
  let inputSHA = '';
  const taskAbort = new AbortController();
  const deadline = Date.now() + (cfg?.taskTimeoutMs ?? 600_000);
  const timeout = setTimeout(() => taskAbort.abort(new WorkerFailure('preview task timed out')), Math.max(1, deadline - Date.now()));
  const abort = () => taskAbort.abort(signal.reason);
  if (signal?.aborted) abort(); else signal?.addEventListener('abort', abort, { once: true });

  const taskCall = async (tool, args, timeoutMs) => {
    taskAbort.signal.throwIfAborted();
    const result = await client.call(tool, scoped(args), { signal: taskAbort.signal, ...(timeoutMs ? { timeoutMs } : {}) });
    taskAbort.signal.throwIfAborted();
    return result;
  };
  const progressReports = new Set();

  let heartbeatInFlight = false;
  const beat = setInterval(async () => {
    if (taskAbort.signal.aborted || heartbeatInFlight) return;
    heartbeatInFlight = true;
    try {
      const result = await taskCall('heartbeat', { task_id: id, ...(previewCapability ? { preview_capability: previewCapability } : {}), ...(deliveryVariantCapability ? { delivery_variant_capability: deliveryVariantCapability } : {}) }, 3000);
      if (result?.stopped || (result?.task_status && !['claimed', 'running'].includes(result.task_status)) ||
          (result?.task_status && (!result.lease_expires_at || result.lease_remaining_ms <= 0))) {
        remoteStatus = result.task_status || 'stopped';
        leaseLost = true;
        taskAbort.abort(new WorkerFailure(`preview task stopped (${remoteStatus})`));
      }
    } catch (err) {
      if (leaseLoss(err)) {
        leaseLost = true;
        taskAbort.abort();
        log(`task ${id}: heartbeat lost lease (${err.code})`);
      } else {
        if (!taskAbort.signal.aborted) log(`task ${id}: heartbeat transport error: ${err.message}`);
      }
    } finally {
      heartbeatInFlight = false;
    }
  }, Math.min(cfg?.heartbeatIntervalMs ?? 2000, 2000));

  const fail = async (reason) => {
    if (leaseLost) return { outcome: 'abandoned', reason };
    try {
      await client.call('fail_task', scoped({ task_id: id, reason: String(reason).slice(0, 1900) }), { signal, timeoutMs: 3000 });
      return { outcome: 'failed', reason };
    } catch (err) {
      return { outcome: 'fail_report_error', reason: `${reason}; report: ${err.message}` };
    }
  };

  const reconcile = async (useTaskSignal = true) => {
    const result = await client.call('get_task_status', scoped({ task_id: id }),
      { signal: useTaskSignal ? taskAbort.signal : signal, timeoutMs: 3000 });
    remoteStatus = result?.task?.status || '';
    if (remoteStatus === 'succeeded') return true;
    if (remoteStatus && !['claimed', 'running'].includes(remoteStatus)) {
      leaseLost = true;
      taskAbort.abort(new WorkerFailure(`preview task stopped (${remoteStatus})`));
      taskAbort.signal.throwIfAborted();
    }
    return false;
  };

  const registerPublished = async previewRel => {
    for (let attempt = 0; attempt < 5; attempt++) {
      taskAbort.signal.throwIfAborted();
      try {
        // Registration hashes/probes large files. Its deadline is the task's
        // remaining budget, independent of the short heartbeat/status calls.
        await taskCall('submit_preview', { task_id: id, preview_dir: previewRel, input_sha256: inputSHA }, Math.max(1, deadline - Date.now()));
        return { outcome: 'succeeded', package: previewRel, recovered };
      } catch (err) {
        if (!transientRegistrationError(err)) throw err;
        taskAbort.signal.throwIfAborted();
        try {
          if (await reconcile()) return { outcome: 'succeeded', package: previewRel, recovered };
        } catch (statusError) {
          if (leaseLoss(statusError) || taskAbort.signal.aborted) throw statusError;
          log(`task ${id}: registration status unavailable: ${statusError.message}`);
        }
        log(`task ${id}: registration transport error; retry ${attempt + 1}/5: ${err.message}`);
        if (attempt < 4) await delay(Math.min(1000 * 2 ** attempt, 5000), undefined, { signal: taskAbort.signal });
      }
    }
    return { outcome: 'registration_pending', package: previewRel, recovered, reason: 'registration result unknown; preserve package for lease recovery' };
  };

  const publicationProgress = async message => {
    try {
      await taskCall('report_progress', { task_id: id, progress: 0.95, message });
    } catch (err) {
      if (leaseLoss(err) || taskAbort.signal.aborted) throw err;
      log(`task ${id}: publication progress unavailable: ${err.message}`);
    }
  };

  try {
    if (task.type !== 'preview') {
      throw new WorkerFailure(`unsupported task type ${task.type}`);
    }
    if (
      !/^[A-Za-z0-9._-]+$/.test(task.asset_id) ||
      !/^[A-Za-z0-9._-]+$/.test(id) ||
      [task.asset_id, id].some((s) => /^\.+$/.test(s))
    ) {
      throw new WorkerFailure('asset_id or task_id contains characters unsafe for a directory name');
    }

    await taskCall('report_progress', { task_id: id, progress: 0.05, message: 'fetching task input' });
    const rawInput = await taskCall('get_task_input', { task_id: id });
    if (rawInput?.cancelled === true) {
      throw new WorkerFailure('workflow cancelled this task');
    }
    const input = rawInput?.preview ?? rawInput;
    if (!input || typeof input !== 'object') {
      throw new WorkerFailure('missing preview task input');
    }
    if (![1, 2].includes(input.schema_version) || (input.schema_version === 2) !== Boolean(input.subtitle_variant)) {
      throw new WorkerFailure(`unsupported preview schema_version ${input.schema_version}`);
    }
    if (input.preset_id !== PREVIEW_PRESET_ID) {
      throw new WorkerFailure(`unsupported preview preset ${input.preset_id}`);
    }
    if (input.asset_id && input.asset_id !== task.asset_id) {
      throw new WorkerFailure('task asset_id mismatch with input');
    }
    if (input.task_id && input.task_id !== id) {
      throw new WorkerFailure('task_id mismatch with input');
    }
    if (!isPreviewRelativePath(input.package_ref)) {
      throw new WorkerFailure(`invalid package_ref: ${input.package_ref}`);
    }
    if (!safeDirectoryID(input.revision_id)) {
      throw new WorkerFailure('revision_id is missing or unsafe for a directory name');
    }
    if (!safeDirectoryID(input.preview_id)) {
      throw new WorkerFailure('preview_id is missing or unsafe for a directory name');
    }
    if (typeof input.input_sha256 !== 'string' || !input.input_sha256) {
      throw new WorkerFailure('missing input_sha256 in input');
    }
    if (input.input_sha256 !== executionScope.input_sha256) throw new WorkerFailure('claim input fingerprint mismatch');
    inputSHA = input.input_sha256;

    // Resolve package directory. ASS lives in its own delivery package, not in packageDir.
    const packageRel = input.package_ref;
    const packageDir = await previewDirectory(cfg.deliveryRoot, packageRel);
    const subtitlePackageDir = input.schema_version === 2
      ? await previewDirectory(cfg.deliveryRoot, input.subtitle_variant.package_ref)
      : undefined;

    // Calculate destination preview directory
    const previewRel = ['previews', task.asset_id, input.revision_id, input.preview_id].join('/');
    const previewDir = await previewDirectory(cfg.deliveryRoot, previewRel, true);

    // Breakpoint recovery check
    let canRecover = false;
    try {
      const manifestPath = join(previewDir, PREVIEW_FILENAMES.manifest);
      const receiptPath = join(previewDir, PREVIEW_FILENAMES.receipt);
      const mp4Path = join(previewDir, PREVIEW_FILENAMES.media);

      const [manifest, receipt, mp4Stat] = await Promise.all([
        recoveryJSON(manifestPath),
        recoveryJSON(receiptPath),
        lstat(mp4Path)
      ]);

      if (mp4Stat.isSymbolicLink() || !mp4Stat.isFile() || mp4Stat.size <= 0) {
        throw new WorkerFailure('preview recovery media is not a nonempty regular file');
      }
      if (
        manifest.schema_version === 1 &&
        receipt.schema_version === 1 &&
        receipt.input_sha256 === input.input_sha256 &&
        manifest.input_sha256 === input.input_sha256 &&
        receipt.preview_id === input.preview_id &&
        manifest.preview_id === input.preview_id &&
        receipt.task_id === id && manifest.task_id === id &&
        receipt.preset_sha256 === input.preset_sha256 && manifest.preset_sha256 === input.preset_sha256
      ) {
        canRecover = true;
      } else {
        throw new WorkerFailure('existing preview directory belongs to a different task input');
      }
    } catch (err) {
      if (err.code !== 'ENOENT') throw err;
      canRecover = false;
    }

    if (canRecover) {
      publishedPackage = previewRel;
      recovered = true;
      if (leaseLost) return { outcome: 'abandoned', reason: 'lease lost before recovery submit' };
      await publicationProgress('recovering existing preview package');
      return await registerPublished(previewRel);
    }

    // Execute renderPreview
    await taskCall('report_progress', { task_id: id, progress: 0.1, message: 'rendering preview' });
    await renderPreviewImpl({
      input,
      packageDir,
      subtitlePackageDir,
      outputDir: previewDir,
      signal: taskAbort.signal,
      onProgress: (p) => {
        const pending = taskCall('report_progress', {
          task_id: id,
          progress: typeof p.progress === 'number' ? p.progress : 0.5,
          message: p.message || p.phase || 'rendering preview'
        }).catch((err) => {
          if (leaseLoss(err)) {
            leaseLost = true;
            taskAbort.abort();
          } else {
            if (!taskAbort.signal.aborted) log(`task ${id}: progress report error: ${err.message}`);
          }
        });
        progressReports.add(pending);
        pending.finally(() => progressReports.delete(pending));
      }
    });
    publishedPackage = previewRel;

    if (leaseLost) return { outcome: 'abandoned', reason: 'lease lost during render' };
    await Promise.all(progressReports);
    taskAbort.signal.throwIfAborted();

    await publicationProgress('registering preview');
    return await registerPublished(previewRel);
  } catch (err) {
    if (signal?.aborted) {
      throw signal.reason;
    }
    if (publishedPackage) {
      try {
        if (remoteStatus === 'succeeded' || await reconcile(false)) {
          return { outcome: 'succeeded', package: publishedPackage, recovered };
        }
      } catch (statusError) {
        if (leaseLoss(statusError)) leaseLost = true;
      }
      if (!leaseLost && !leaseLoss(err) && transientRegistrationError(err)) {
        return { outcome: 'registration_pending', package: publishedPackage, recovered, reason: err.message };
      }
    }
    if (leaseLost) {
      return { outcome: 'abandoned', reason: err.message };
    }
    if (leaseLoss(err)) {
      return { outcome: 'abandoned', reason: err.message };
    }
    return fail(err.message);
  } finally {
    clearTimeout(timeout);
    clearInterval(beat);
    taskAbort.abort(new Error('preview task finished'));
    signal?.removeEventListener('abort', abort);
  }
}
