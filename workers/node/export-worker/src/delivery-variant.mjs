import { lstat } from 'node:fs/promises';
import { join } from 'node:path';
import { ToolError } from './mcp.mjs';
import { previewDirectory, recoveryJSON } from './preview.mjs';
import { buildDeliveryVariant, loadVariantSpec } from '../../timeline-cli/src/delivery-variant.mjs';
import { isPreviewRelativePath } from '../../timeline-cli/src/preview-protocol.mjs';
export async function probeDeliveryVariantCapability() {
  try { const { spec, sha256 } = await loadVariantSpec(); return { protocol_version: 1, ready: true, specs: [{ spec_id: spec.spec_id, sha256 }] }; }
  catch { return { protocol_version: 1, ready: false, specs: [] }; }
}
const safeID = value => isPreviewRelativePath(value) && !value.includes('/');
const lostLease = error => error instanceof ToolError && ['lease_expired', 'invalid_state', 'not_found', 'conflict', 'forbidden'].includes(error.code);
export async function processDeliveryVariantTask({ client, cfg, task, signal, log = () => {}, buildVariantImpl = buildDeliveryVariant, deliveryVariantCapability }) {
  signal?.throwIfAborted();
  const controller = new AbortController();
  const abort = () => controller.abort(signal.reason);
  signal?.addEventListener('abort', abort, { once: true }); if (signal?.aborted) abort();
  const timer = setTimeout(() => controller.abort(new Error('ASS delivery task timed out')), cfg.taskTimeoutMs ?? 600_000);
  let leaseLost = false; const reports = new Set();
  const call = async (tool, args) => { controller.signal.throwIfAborted(); const result = await client.call(tool, args, { signal: controller.signal }); controller.signal.throwIfAborted(); return result; };
  const heartbeat = setInterval(async () => {
    if (controller.signal.aborted) return;
    try { await call('heartbeat', { task_id: task.task_id, ...(deliveryVariantCapability ? { delivery_variant_capability: deliveryVariantCapability } : {}) }); }
    catch (error) { if (lostLease(error)) { leaseLost = true; controller.abort(error); } else if (!controller.signal.aborted) log(error.message); }
  }, cfg.heartbeatIntervalMs ?? 5000);
  try {
    if (task.type !== 'delivery_variant' || !safeID(task.asset_id) || !safeID(task.task_id)) throw new Error('unsafe delivery variant task');
    const raw = await call('get_task_input', { task_id: task.task_id });
    if (raw.cancelled) throw new Error('workflow cancelled delivery variant');
    const input = raw.delivery_variant;
    if (!input || input.asset_id !== task.asset_id || input.task_id !== task.task_id || !safeID(input.variant_id) || !safeID(input.revision_id)) throw new Error('delivery variant input binding mismatch');
    const packageDir = await previewDirectory(cfg.deliveryRoot, input.parent_package_ref);
    const relative = `delivery-variants/${task.asset_id}/${input.variant_id}`;
    const outputDir = await previewDirectory(cfg.deliveryRoot, relative, true);
    let recovered = false;
    try {
      const [manifest, receipt, ass] = await Promise.all([recoveryJSON(join(outputDir, 'variant-manifest.json')), recoveryJSON(join(outputDir, 'variant-receipt.json')), lstat(join(outputDir, 'subtitles.ass'))]);
      if (ass.isSymbolicLink() || !ass.isFile() || ass.size <= 0) throw new Error('invalid recovered ASS file');
      for (const record of [manifest, receipt]) if (record.schema_version !== 1 || record.variant_id !== input.variant_id || record.task_id !== task.task_id || record.asset_id !== input.asset_id || record.run_id !== input.run_id || record.revision_id !== input.revision_id || record.input_sha256 !== input.input_sha256 || record.spec_sha256 !== input.spec_sha256) throw new Error('recovery belongs to different variant input');
      recovered = true;
    } catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (!recovered) await buildVariantImpl({ input, packageDir, outputDir, signal: controller.signal, onProgress: event => {
      const pending = call('report_progress', { task_id: task.task_id, progress: event.progress, message: '生成 ASS 字幕交付包' }).catch(error => {
        if (lostLease(error)) { leaseLost = true; controller.abort(error); } else if (!controller.signal.aborted) log(error.message);
      });
      reports.add(pending); pending.finally(() => reports.delete(pending));
    } });
    await Promise.all(reports); controller.signal.throwIfAborted();
    await call('submit_delivery_variant', { task_id: task.task_id, variant_dir: relative });
    return { outcome: 'succeeded', package: relative, recovered };
  } catch (error) {
    if (signal?.aborted) throw signal.reason;
    if (leaseLost || lostLease(error)) return { outcome: 'abandoned', reason: error.message };
    try { await client.call('fail_task', { task_id: task.task_id, reason: String(error.message).slice(0, 1900) }); return { outcome: 'failed', reason: error.message }; }
    catch (reportError) { return { outcome: 'fail_report_error', reason: `${error.message}; ${reportError.message}` }; }
  } finally { clearTimeout(timer); clearInterval(heartbeat); controller.abort(); signal?.removeEventListener('abort', abort); }
}
