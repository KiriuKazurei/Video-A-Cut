import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { processDeliveryVariantTask, probeDeliveryVariantCapability } from '../src/delivery-variant.mjs';
import { ToolError } from '../src/mcp.mjs';
const golden = JSON.parse(await readFile(new URL('../../../../contracts/delivery-variant/v1/fixtures/input.json', import.meta.url)));
const input = { ...golden.input, asset_id: 'asset_fixture', parent_package_ref: 'exports/fixture' };
const task = { task_id: input.task_id, asset_id: input.asset_id, type: 'delivery_variant' };
async function setup(t, handlers = {}) {
  const root = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'vac-ass-worker-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, 'exports/fixture'), { recursive: true });
  const calls = []; const client = { async call(tool, args, options) { calls.push([tool, args]); return handlers[tool] ? handlers[tool](args, options) : tool === 'get_task_input' ? { delivery_variant: input } : { ok: true }; } };
  return { root, calls, client, cfg: { deliveryRoot: root, heartbeatIntervalMs: 10, taskTimeoutMs: 2000 }, task };
}
test('ASS capability binds the registered style without FFmpeg or font dependency', async () => {
  const cap = await probeDeliveryVariantCapability(); assert.equal(cap.ready, true);
  assert.equal(cap.specs[0].spec_id, input.spec_id); assert.equal(cap.specs[0].sha256, input.spec_sha256);
});
test('ASS worker submits only controlled variant path and reports progress', async t => {
  const f = await setup(t); let invoked = false;
  const result = await processDeliveryVariantTask({ ...f, buildVariantImpl: async args => { invoked = true; args.onProgress({ progress: 0.5 }); } });
  assert(invoked); assert.equal(result.outcome, 'succeeded');
  assert.deepEqual(f.calls.find(([n]) => n === 'submit_delivery_variant')[1], { task_id: task.task_id, variant_dir: `delivery-variants/${task.asset_id}/${input.variant_id}` });
  assert(f.calls.some(([n]) => n === 'report_progress')); assert(!f.calls.some(([n]) => n === 'submit_delivery'));
});
test('ASS deadline prevents late builder submission', async t => {
  const f = await setup(t);
  const result = await processDeliveryVariantTask({ ...f, cfg: { ...f.cfg, taskTimeoutMs: 20 }, buildVariantImpl: async () => { await new Promise(r => setTimeout(r, 70)); } });
  assert.equal(result.outcome, 'failed'); assert.match(result.reason, /timed out/);
  assert(!f.calls.some(([n]) => n === 'submit_delivery_variant'));
});
test('ASS lost lease abandons output and avoids fail_task', async t => {
  const f = await setup(t, { heartbeat: () => { throw new ToolError('heartbeat', 'lease_expired: expired'); } });
  const result = await processDeliveryVariantTask({ ...f, buildVariantImpl: async () => { await new Promise(r => setTimeout(r, 60)); } });
  assert.equal(result.outcome, 'abandoned'); assert(!f.calls.some(([n]) => ['fail_task', 'submit_delivery_variant'].includes(n)));
});
test('ASS recovery requires exact task and style binding', async t => {
  const f = await setup(t); const output = join(f.root, `delivery-variants/${task.asset_id}/${input.variant_id}`); await mkdir(output, { recursive: true });
  const binding = { schema_version: 1, variant_id: input.variant_id, task_id: task.task_id, asset_id: input.asset_id, run_id: input.run_id, revision_id: input.revision_id, input_sha256: input.input_sha256, spec_sha256: input.spec_sha256 };
  await writeFile(join(output, 'subtitles.ass'), '[Script Info]');
  await writeFile(join(output, 'variant-manifest.json'), JSON.stringify(binding));
  await writeFile(join(output, 'variant-receipt.json'), JSON.stringify({ ...binding, task_id: 'foreign' }));
  const bad = await processDeliveryVariantTask({ ...f, buildVariantImpl: async () => assert.fail('rerendered foreign output') });
  assert.equal(bad.outcome, 'failed'); assert(!f.calls.some(([n]) => n === 'submit_delivery_variant'));
  await writeFile(join(output, 'variant-receipt.json'), JSON.stringify(binding));
  const recovered = await processDeliveryVariantTask({ ...f, buildVariantImpl: async () => assert.fail('rerendered recoverable output') });
  assert.equal(recovered.outcome, 'succeeded'); assert.equal(recovered.recovered, true);
});
