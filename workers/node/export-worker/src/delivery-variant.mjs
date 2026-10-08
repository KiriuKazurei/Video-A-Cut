import { lstat, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { ToolError } from './mcp.mjs';
import { previewDirectory, recoveryJSON } from './preview.mjs';
import { buildDeliveryVariant, loadVariantSpec, variantFingerprint, EDITABLE_VARIANT_SPEC_ID } from '../../timeline-cli/src/delivery-variant.mjs';
import { packageFile, hashFile } from '../../timeline-cli/src/preview.mjs';
import { isPreviewRelativePath } from '../../timeline-cli/src/preview-protocol.mjs';
import { installedFonts } from '../../timeline-cli/src/fonts.mjs';
import { canonicalJSON } from '../../../../contracts/delivery-variant/v1/style.mjs';

export async function probeDeliveryVariantCapability({ refresh = false, installedFontsImpl = installedFonts } = {}) {
  try {
    const specs = await Promise.all(['ass-basic-v1', EDITABLE_VARIANT_SPEC_ID].map(async id => {
      const { spec, sha256 } = await loadVariantSpec(id);
      return { spec_id: spec.spec_id, sha256 };
    }));
    const families = new Map();
    try {
      for (const font of await installedFontsImpl(refresh)) {
        if (!families.has(font.family)) families.set(font.family, { family: font.family, sha256: font.sha256 });
      }
    } catch { /* v1 conversion stays available when no local font catalogue can be read. */ }
    return { protocol_version: 1, ready: true, specs, fonts: [...families.values()] };
  } catch { return { protocol_version: 1, ready: false, specs: [], fonts: [] }; }
}
const safeID = value => isPreviewRelativePath(value) && !value.includes('/');
const lostLease = error => error instanceof ToolError && ['lease_expired', 'invalid_state', 'not_found', 'conflict', 'forbidden'].includes(error.code);
export async function processDeliveryVariantTask({ client, cfg, task, signal, log = () => {}, buildVariantImpl = buildDeliveryVariant, deliveryVariantCapability, probeCapabilityImpl = probeDeliveryVariantCapability }) {
  signal?.throwIfAborted();
  const controller = new AbortController();
  const abort = () => controller.abort(signal.reason);
  signal?.addEventListener('abort', abort, { once: true }); if (signal?.aborted) abort();
  const timer = setTimeout(() => controller.abort(new Error('ASS delivery task timed out')), cfg.taskTimeoutMs ?? 600_000);
  let leaseLost = false, heartbeatRunning = false; const reports = new Set();
  const call = async (tool, args) => { controller.signal.throwIfAborted(); const result = await client.call(tool, args, { signal: controller.signal }); controller.signal.throwIfAborted(); return result; };
  const heartbeat = setInterval(async () => {
    if (controller.signal.aborted || heartbeatRunning) return;
    heartbeatRunning = true;
    try {
      // The catalogue applies its own TTL; a later heartbeat sees installed/removed fonts.
      if (deliveryVariantCapability) deliveryVariantCapability = await probeCapabilityImpl();
      await call('heartbeat', { task_id: task.task_id, ...(deliveryVariantCapability ? { delivery_variant_capability: deliveryVariantCapability } : {}) });
    }
    catch (error) { if (lostLease(error)) { leaseLost = true; controller.abort(error); } else if (!controller.signal.aborted) log(error.message); }
    finally { heartbeatRunning = false; }
  }, cfg.heartbeatIntervalMs ?? 5000);
  try {
    if (task.type !== 'delivery_variant' || !safeID(task.asset_id) || !safeID(task.task_id)) throw new Error('unsafe delivery variant task');
    const raw = await call('get_task_input', { task_id: task.task_id });
    if (raw.cancelled) throw new Error('workflow cancelled delivery variant');
    const input = raw.delivery_variant;
    if (!input || input.asset_id !== task.asset_id || input.task_id !== task.task_id || !safeID(input.variant_id) || !safeID(input.revision_id)) throw new Error('delivery variant input binding mismatch');
    if (variantFingerprint(input) !== input.input_sha256) throw new Error('variant input fingerprint mismatch');
    const packageDir = await previewDirectory(cfg.deliveryRoot, input.parent_package_ref);
    const relative = `delivery-variants/${task.asset_id}/${input.variant_id}`;
    const outputDir = await previewDirectory(cfg.deliveryRoot, relative, true);
    let recovered = false;
    try {
      const [manifest, receipt, ass] = await Promise.all([recoveryJSON(join(outputDir, 'variant-manifest.json')), recoveryJSON(join(outputDir, 'variant-receipt.json')), lstat(join(outputDir, 'subtitles.ass'))]);
      if (ass.isSymbolicLink() || !ass.isFile() || ass.size <= 0) throw new Error('invalid recovered ASS file');
      for (const record of [manifest, receipt]) if (record.schema_version !== input.schema_version || record.variant_id !== input.variant_id || record.task_id !== task.task_id || record.asset_id !== input.asset_id || record.run_id !== input.run_id || record.revision_id !== input.revision_id || record.input_sha256 !== input.input_sha256 || record.spec_sha256 !== input.spec_sha256) throw new Error('recovery belongs to different variant input');
      if (receipt.converter_version !== `vac.${input.spec_id}/1` || !Array.isArray(manifest.artifacts)) throw new Error('invalid recovered variant metadata');
      for (const key of ['ass', 'style']) {
        const entries = manifest.artifacts.filter(entry => entry.key === key);
        if (entries.length !== 1 || entries[0].path !== (key === 'ass' ? 'subtitles.ass' : 'ass-style.json')) throw new Error('missing recovered ASS/style artifact');
        if (!Number.isSafeInteger(entries[0].size) || entries[0].size <= 0 || entries[0].size > 1024 * 1024 || !/^[0-9a-f]{64}$/.test(entries[0].sha256)) throw new Error('invalid recovered ASS/style artifact');
        const file = await packageFile(outputDir, entries[0].path);
        if ((await lstat(file)).size !== entries[0].size || await hashFile(file, controller.signal) !== entries[0].sha256) throw new Error('recovered variant file changed');
      }
      const styleBytes = await readFile(await packageFile(outputDir, 'ass-style.json'));
      const registered = await loadVariantSpec(input.spec_id);
      if (registered.sha256 !== input.spec_sha256) throw new Error('recovered ASS spec changed');
      const expectedStyle = input.schema_version === 2 ? Buffer.from(canonicalJSON(input.style_document)) : registered.bytes;
      if (!styleBytes.equals(expectedStyle)) throw new Error('recovered ASS style snapshot changed');
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
