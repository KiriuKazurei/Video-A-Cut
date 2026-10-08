#!/usr/bin/env node
// Export worker entry: node src/main.mjs --config <worker.json> [--once]
import { createClient } from './mcp.mjs';
import { loadConfig, runLoop } from './worker.mjs';
import { existsSync } from 'node:fs';

const args = process.argv.slice(2);
const configIdx = args.indexOf('--config');
if (configIdx < 0 || !args[configIdx + 1]) {
  process.stderr.write('Usage: node src/main.mjs --config <worker.json> [--once]\n');
  process.exit(2);
}
const once = args.includes('--once');
const log = (m) => process.stdout.write(`[export-worker ${new Date().toISOString()}] ${m}\n`);

const controller = new AbortController();
const stopPath = process.env.VAC_SESSION_STOP;
if (stopPath && existsSync(stopPath)) controller.abort();
const stopWatcher = stopPath ? setInterval(() => {
  if (existsSync(stopPath) && !controller.signal.aborted) {
    log('session stop requested; cancelling owned export work');
    controller.abort();
  }
}, 200) : null;
stopWatcher?.unref();
for (const sig of ['SIGINT', 'SIGTERM']) {
  process.on(sig, () => {
    log(`${sig} received; finishing current step and exiting`);
    controller.abort();
  });
}

try {
  if (controller.signal.aborted) {
    log('session was stopped before worker startup');
  } else {
    const cfg = await loadConfig(args[configIdx + 1]);
    const client = createClient({ url: cfg.mcpUrl, token: cfg.token });
    const result = await runLoop({ client, cfg, log, signal: controller.signal, once });
    log(`exit: ${result.outcome}`);
    process.exitCode = result.outcome === 'failed' || result.outcome === 'fail_report_error' ? 1 : 0;
  }
} catch (err) {
  log(`fatal: ${err.message}`);
  process.exitCode = 2;
} finally {
  if (stopWatcher) clearInterval(stopWatcher);
}
