import test from 'node:test';
import assert from 'node:assert/strict';
import { runTool } from '../src/media.mjs';

test('media subprocess preserves cancellation and waits for exit', async () => {
  const controller = new AbortController();
  const reason = new Error('cancel media probe');
  const timer = setTimeout(() => controller.abort(reason), 100);
  try {
    await assert.rejects(runTool(process.execPath, ['-e', 'setTimeout(()=>{},1500)'], { signal: controller.signal }), error => error === reason);
  } finally { clearTimeout(timer); }
});

test('media subprocess has a deadline independent of caller cancellation', async () => {
  await assert.rejects(runTool(process.execPath, ['-e', 'setTimeout(()=>{},1500)'], { timeoutMs: 100 }), { code: 'media_tool_timeout' });
});

test('media subprocess rejects excessive stdout instead of buffering it all', async () => {
  await assert.rejects(runTool(process.execPath, ['-e', 'process.stdout.write("x".repeat(100000))'], { maxOutputBytes: 1024 }), { code: 'media_tool_output_limit' });
});
