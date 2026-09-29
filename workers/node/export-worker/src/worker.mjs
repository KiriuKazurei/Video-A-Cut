import { spawn } from 'node:child_process';
import { readFile, realpath, stat } from 'node:fs/promises';
import { isAbsolute, join, relative, resolve, sep } from 'node:path';
import { ToolError } from './mcp.mjs';

/**
 * Worker configuration. Paths only; the bearer token is read from the
 * environment variable named by token_env so it never lands in a file that
 * might be committed or logged.
 */
export async function loadConfig(path, env = process.env) {
  const raw = JSON.parse(await readFile(path, 'utf8'));
  const cfg = {
    mcpUrl: raw.mcp_url,
    tokenEnv: raw.token_env ?? 'VAC_WORKER_TOKEN',
    deliveryRoot: raw.delivery_root,
    cliPath: raw.cli_path,
    adaptersPath: raw.adapters_path,
    outputPrefix: raw.output_prefix ?? 'deliveries',
    pollIntervalMs: raw.poll_interval_ms ?? 2000,
    heartbeatIntervalMs: raw.heartbeat_interval_ms ?? 5000,
    taskTimeoutMs: raw.task_timeout_ms ?? 10 * 60_000,
    nodePath: raw.node_path ?? process.execPath
  };
  for (const key of ['mcpUrl', 'deliveryRoot', 'cliPath', 'adaptersPath']) {
    if (typeof cfg[key] !== 'string' || !cfg[key]) throw new Error(`config: ${key} is required`);
  }
  for (const key of ['deliveryRoot', 'cliPath', 'adaptersPath']) {
    if (!isAbsolute(cfg[key])) throw new Error(`config: ${key} must be absolute`);
  }
  if (!/^[A-Za-z0-9_-]+(\/[A-Za-z0-9_-]+)*$/.test(cfg.outputPrefix)) {
    throw new Error('config: output_prefix must be a relative slash path of [A-Za-z0-9_-]');
  }
  for (const key of ['pollIntervalMs', 'heartbeatIntervalMs', 'taskTimeoutMs']) {
    if (!Number.isInteger(cfg[key]) || cfg[key] <= 0) throw new Error(`config: ${key} must be a positive integer`);
  }
  cfg.token = env[cfg.tokenEnv];
  if (!cfg.token) throw new Error(`config: environment variable ${cfg.tokenEnv} is not set`);
  cfg.deliveryRoot = await realpath(cfg.deliveryRoot);
  return cfg;
}

function inside(root, p) {
  const rel = relative(root, p);
  return rel !== '' && rel !== '..' && !rel.startsWith(`..${sep}`) && !isAbsolute(rel);
}

/** Resolve a root-relative artifact path the control plane handed out. */
export async function resolveUnderRoot(root, rel) {
  if (typeof rel !== 'string' || !rel || isAbsolute(rel) || rel.includes('\\')) {
    throw new WorkerFailure('asset artifact path is not a root-relative slash path');
  }
  const real = await realpath(join(root, ...rel.split('/'))).catch(() => {
    throw new WorkerFailure(`asset artifact is missing: ${rel}`);
  });
  if (!inside(root, real) || !(await stat(real)).isFile()) {
    throw new WorkerFailure(`asset artifact escapes delivery root or is not a file: ${rel}`);
  }
  return real;
}

/** A failure the worker reports via fail_task (vs. a transport problem). */
export class WorkerFailure extends Error {
  constructor(message) {
    super(message);
    this.name = 'WorkerFailure';
  }
}

/** Run the Phase 1 CLI as a child process with a hard timeout. */
export function runCli({ nodePath, cliPath, edlPath, outputDir, adaptersPath, timeoutMs, signal }) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(nodePath, [cliPath, 'build', '--edl', edlPath, '--output', outputDir, '--adapters', adaptersPath], {
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsHide: true
    });
    let stdout = '';
    let stderr = '';
    const cap = (s, chunk) => (s + chunk).slice(-8000);
    child.stdout.on('data', (d) => { stdout = cap(stdout, d); });
    child.stderr.on('data', (d) => { stderr = cap(stderr, d); });
    let reason = null;
    const kill = (why) => {
      if (reason) return;
      reason = why;
      child.kill('SIGKILL');
    };
    const timer = setTimeout(() => kill(`timed out after ${timeoutMs} ms`), timeoutMs);
    const onAbort = () => kill('worker shutting down');
    signal?.addEventListener('abort', onAbort, { once: true });
    child.on('error', (err) => {
      clearTimeout(timer);
      signal?.removeEventListener('abort', onAbort);
      reject(new WorkerFailure(`cli failed to start: ${err.message}`));
    });
    child.on('close', (code) => {
      clearTimeout(timer);
      signal?.removeEventListener('abort', onAbort);
      if (reason) return reject(new WorkerFailure(`cli ${reason}`));
      if (code !== 0) return reject(new WorkerFailure(`cli exited ${code}: ${stderr.trim().slice(-500)}`));
      resolvePromise({ stdout, stderr });
    });
  });
}

const sleep = (ms, signal) => new Promise((r) => {
  const t = setTimeout(r, ms);
  signal?.addEventListener('abort', () => { clearTimeout(t); r(); }, { once: true });
});

/**
 * Process one claimed export task end to end. Returns the final outcome for
 * logging. Never throws for task-level problems; those become fail_task.
 */
export async function processTask({ client, cfg, task, log, signal, runCliImpl = runCli }) {
  const id = task.task_id;
  let leaseLost = false;
  const beat = setInterval(async () => {
    try {
      await client.call('heartbeat', { task_id: id });
    } catch (err) {
      if (err instanceof ToolError && ['lease_expired', 'not_found', 'invalid_state'].includes(err.code)) {
        leaseLost = true;
        log(`task ${id}: heartbeat lost lease (${err.code})`);
      } else {
        log(`task ${id}: heartbeat transport error: ${err.message}`);
      }
    }
  }, cfg.heartbeatIntervalMs);

  const fail = async (reason) => {
    if (leaseLost) return { outcome: 'abandoned', reason };
    try {
      await client.call('fail_task', { task_id: id, reason: reason.slice(0, 1900) });
      return { outcome: 'failed', reason };
    } catch (err) {
      return { outcome: 'fail_report_error', reason: `${reason}; report: ${err.message}` };
    }
  };

  try {
    if (task.type !== 'export') throw new WorkerFailure(`unsupported task type ${task.type}`);
    if (!/^[A-Za-z0-9._-]+$/.test(task.asset_id) || !/^[A-Za-z0-9._-]+$/.test(id) || [task.asset_id, id].some((s) => /^\.+$/.test(s))) {
      throw new WorkerFailure('asset_id or task_id contains characters unsafe for a directory name');
    }
    await client.call('report_progress', { task_id: id, progress: 0.05, message: 'resolving edl' });
    const { asset } = await client.call('get_asset', { asset_id: task.asset_id });
    // The control plane holds exports on exported assets (typeReady); this
    // is a fallback if a claim ever races a governance change.
    if (asset.status === 'exported') {
      throw new WorkerFailure('asset already exported; reopen it in the WebUI before exporting again');
    }
    const edlPath = await resolveUnderRoot(cfg.deliveryRoot, asset.artifacts?.edl);
    // One package per task id: the CLI refuses to overwrite, so a retried
    // task on a fresh claim must not collide with a leftover directory.
    const packageRel = `${cfg.outputPrefix}/${task.asset_id}/${id}`;
    const outputDir = resolve(cfg.deliveryRoot, ...packageRel.split('/'));
    await client.call('report_progress', { task_id: id, progress: 0.2, message: 'running timeline cli' });
    await runCliImpl({
      nodePath: cfg.nodePath, cliPath: cfg.cliPath, edlPath, outputDir,
      adaptersPath: cfg.adaptersPath, timeoutMs: cfg.taskTimeoutMs, signal
    });
    if (leaseLost) return { outcome: 'abandoned', reason: 'lease lost during build' };
    await client.call('report_progress', { task_id: id, progress: 0.9, message: 'registering delivery' });
    await client.call('submit_delivery', { task_id: id, package_dir: packageRel });
    return { outcome: 'succeeded', package: packageRel };
  } catch (err) {
    if (err instanceof ToolError && err.code === 'lease_expired') {
      return { outcome: 'abandoned', reason: err.message };
    }
    return fail(err.message);
  } finally {
    clearInterval(beat);
  }
}

/** Poll-claim-process loop until signal aborts. */
export async function runLoop({ client, cfg, log, signal, once = false }) {
  const tools = await client.listTools();
  for (const needed of ['claim_task', 'heartbeat', 'report_progress', 'get_asset', 'submit_delivery', 'fail_task']) {
    if (!tools.includes(needed)) throw new Error(`control plane does not expose ${needed}`);
  }
  await client.call('heartbeat', {});
  log('worker registered; polling');
  while (!signal.aborted) {
    let claim;
    try {
      claim = await client.call('claim_task', {});
    } catch (err) {
      log(`claim error: ${err.message}`);
      await sleep(cfg.pollIntervalMs, signal);
      continue;
    }
    if (!claim.claimed) {
      if (once) return { outcome: 'idle' };
      await sleep(cfg.pollIntervalMs, signal);
      continue;
    }
    log(`claimed ${claim.task.task_id} (${claim.task.type}) for ${claim.task.asset_id}`);
    const result = await processTask({ client, cfg, task: claim.task, log, signal });
    log(`task ${claim.task.task_id}: ${result.outcome}${result.reason ? ` - ${result.reason}` : ''}`);
    if (once) return result;
  }
  return { outcome: 'stopped' };
}
