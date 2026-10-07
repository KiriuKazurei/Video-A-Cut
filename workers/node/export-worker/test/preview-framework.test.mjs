import test from 'node:test';
import assert from 'node:assert/strict';
import { probePreviewCapability, processPreviewTask } from '../src/preview.mjs';
import { WorkerFailure } from '../src/worker.mjs';

test('Worker advertises compliant capability structure and handles missing tools', async () => {
  const capability = await probePreviewCapability();
  assert.equal(capability.protocol_version, 1);
  assert.equal(typeof capability.ready, 'boolean');
  assert.ok(Array.isArray(capability.presets));
  assert.ok(Array.isArray(capability.checks));

  const blockedCap = await probePreviewCapability({ ffmpegCheck: async () => null });
  assert.equal(blockedCap.protocol_version, 1);
  assert.equal(blockedCap.ready, false);
  assert.deepEqual(blockedCap.presets, []);
  const ffmpegCheck = blockedCap.checks.find((c) => c.code === 'ffmpeg');
  assert.ok(ffmpegCheck);
  assert.equal(ffmpegCheck.status, 'blocked');
});

test('processPreviewTask rejects missing task and preserves caller abort', async () => {
  await assert.rejects(processPreviewTask({}), WorkerFailure);
  const controller = new AbortController(); const reason = new Error('worker stopped'); controller.abort(reason);
  await assert.rejects(processPreviewTask({signal:controller.signal}), (error) => error === reason);
});
