import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, readFile, realpath, rm, symlink, unlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ToolError } from '../src/mcp.mjs';
import { processPreviewTask, probePreviewCapability } from '../src/preview.mjs';
import { runLoop, processTask, WorkerFailure } from '../src/worker.mjs';
import { PREVIEW_FILENAMES } from '../../timeline-cli/src/preview-protocol.mjs';

const fixtureURL = new URL('../../../../contracts/mp4-preview/v1/fixtures/input.json', import.meta.url);
const fixture = JSON.parse(await readFile(fixtureURL, 'utf8'));

async function createTestRoot() {
  const dir = await realpath(await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'vac-preview-')));
  const pkgDir = join(dir, 'exports', 'fixture');
  await mkdir(pkgDir, { recursive: true });
  return {
    dir,
    async cleanup() {
      await rm(dir, { recursive: true, force: true }).catch(() => {});
    }
  };
}

function fakeClient(handlers) {
  const calls = [];
  return {
    calls,
    async call(tool, args) {
      calls.push([tool, args]);
      const h = handlers[tool];
      if (!h) return { ok: true };
      return h(args);
    }
  };
}

const baseCfg = (deliveryRoot) => ({
  deliveryRoot,
  outputPrefix: 'deliveries',
  heartbeatIntervalMs: 200,
  pollIntervalMs: 50,
  taskTimeoutMs: 1000,
  nodePath: process.execPath,
  cliPath: join(deliveryRoot, 'cli.mjs'),
  adaptersPath: join(deliveryRoot, 'adapters.mjs')
});

const defaultTask = {
  task_id: 'task_fixture',
  asset_id: 'asset_fixture',
  type: 'preview'
};
const defaultScope = {
  task_id: defaultTask.task_id, runtime_instance_id: 'preview-test-instance',
  execution_id: 'preview-test-execution', generation: 1, input_sha256: fixture.input.input_sha256
};

test('preview cannot submit after its deadline even if the renderer returns late', async () => {
  const {dir,cleanup}=await createTestRoot();
  try {
    const client=fakeClient({get_task_input:()=>({preview:fixture.input})});
    const result=await processPreviewTask({client,cfg:{...baseCfg(dir),taskTimeoutMs:30},task:defaultTask, executionScope: defaultScope,
      renderPreviewImpl:async()=>{await new Promise(resolve=>setTimeout(resolve,100));}});
    assert.equal(result.outcome,'registration_pending');
    assert.match(result.reason,/timed out/);
    assert.ok(!client.calls.some(([tool])=>tool==='submit_preview'));
  } finally {await cleanup();}
});

test('preview rejects path separators in revision and preview IDs', async () => {
  const {dir,cleanup}=await createTestRoot();
  try {
    for(const key of ['revision_id','preview_id']) {
      const input={...fixture.input,[key]:'../foreign'};
      const client=fakeClient({get_task_input:()=>({preview:input})});
      let rendered=false;
      const result=await processPreviewTask({client,cfg:baseCfg(dir),task:defaultTask, executionScope: defaultScope,renderPreviewImpl:async()=>{rendered=true}});
      assert.equal(result.outcome,'failed');assert.equal(rendered,false);
    }
  } finally {await cleanup();}
});

test('preview refuses a redirected output parent before rendering or recovery', async () => {
  const {dir,cleanup}=await createTestRoot();
  const link=join(dir,'previews');
  try {
    const target=join(dir,'redirected');await mkdir(target);
    await symlink(target,link,process.platform==='win32'?'junction':'dir');
    const client=fakeClient({get_task_input:()=>({preview:fixture.input})});let rendered=false;
    const result=await processPreviewTask({client,cfg:baseCfg(dir),task:defaultTask, executionScope: defaultScope,renderPreviewImpl:async()=>{rendered=true}});
    assert.equal(result.outcome,'failed');assert.equal(rendered,false);
    assert.ok(!client.calls.some(([tool])=>tool==='submit_preview'));
  } finally {await unlink(link).catch(error=>{if(error.code!=='ENOENT')throw error});await cleanup();}
});

test('preview recovery rejects foreign task or preset bindings and oversized JSON', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const output = join(dir, 'previews', fixture.input.asset_id, fixture.input.revision_id, fixture.input.preview_id);
    await mkdir(output, { recursive: true });
    await writeFile(join(output, PREVIEW_FILENAMES.media), 'dummy-mp4-data');
    const binding = { schema_version: 1, preview_id: fixture.input.preview_id, task_id: fixture.input.task_id,
      input_sha256: fixture.input.input_sha256, preset_sha256: fixture.input.preset_sha256 };
    for (const file of ['manifest', 'receipt']) {
      for (const field of ['task_id', 'preset_sha256']) {
        for (const item of ['manifest', 'receipt']) {
          await writeFile(join(output, PREVIEW_FILENAMES[item]), JSON.stringify({ ...binding, ...(file === item ? { [field]: 'foreign' } : {}) }));
        }
        const client = fakeClient({ get_task_input: () => ({ preview: fixture.input }) });
        const result = await processPreviewTask({ client, cfg: baseCfg(dir), task: defaultTask, executionScope: defaultScope,
          renderPreviewImpl: async () => assert.fail('foreign receipt caused a rerender') });
        assert.equal(result.outcome, 'failed');
        assert.match(result.reason, /different task input/);
        assert.ok(!client.calls.some(([tool]) => tool === 'submit_preview'));
      }
    }
    await writeFile(join(output, PREVIEW_FILENAMES.receipt), JSON.stringify(binding));
    await writeFile(join(output, PREVIEW_FILENAMES.manifest), ' '.repeat(1024 * 1024 + 1));
    const client = fakeClient({ get_task_input: () => ({ preview: fixture.input }) });
    const result = await processPreviewTask({ client, cfg: baseCfg(dir), task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => assert.fail('oversized receipt caused a rerender') });
    assert.equal(result.outcome, 'failed');
    assert.match(result.reason, /exceeds limit/);
    assert.ok(!client.calls.some(([tool]) => tool === 'submit_preview'));
  } finally { await cleanup(); }
});

test('preview deadline aborts fetching input before it can reach the renderer', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const calls = [];
    const client = { async call(tool, _args, { signal } = {}) {
      calls.push(tool);
      if (tool === 'get_task_input') return new Promise((_resolve, reject) => {
        signal.addEventListener('abort', () => reject(signal.reason), { once: true });
      });
      return {};
    } };
    const result = await processPreviewTask({ client, cfg: { ...baseCfg(dir), taskTimeoutMs: 30 }, task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => assert.fail('timed out input reached renderer') });
    assert.equal(result.outcome, 'failed');
    assert.match(result.reason, /timed out/);
    assert.ok(calls.includes('fail_task')); assert.ok(!calls.includes('submit_preview'));
  } finally { await cleanup(); }
});

test('late progress lease failure prevents registration after rendering returns', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const client = fakeClient({ get_task_input: () => ({ preview: fixture.input }),
      report_progress: async args => {
        if (args.progress === 0.5) {
          await new Promise(resolve => setTimeout(resolve, 30));
          throw new ToolError('report_progress', 'lease_expired: rendering lease expired');
        }
        return {};
      } });
    const result = await processPreviewTask({ client, cfg: baseCfg(dir), task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async ({ onProgress }) => onProgress({ progress: 0.5 }) });
    assert.equal(result.outcome, 'abandoned');
    assert.ok(!client.calls.some(([tool]) => ['submit_preview', 'fail_task'].includes(tool)));
  } finally { await cleanup(); }
});

// -------------------------------------------------------------
// 1. probePreviewCapability Tests
// -------------------------------------------------------------

test('probePreviewCapability detects environment and passes when tools and font available', async () => {
  const cap = await probePreviewCapability();
  assert.equal(cap.protocol_version, 1);
  assert.equal(typeof cap.ready, 'boolean');
  assert.ok(Array.isArray(cap.presets));
  assert.ok(Array.isArray(cap.checks));
  if (cap.ready) {
    assert.equal(cap.presets.length, 1);
    assert.equal(cap.presets[0].preset_id, 'mp4-720p-v1');
    assert.ok(cap.checks.every((c) => c.status === 'passed'));
  }
});

test('probePreviewCapability blocks ready when ffmpeg is missing', async () => {
  const cap = await probePreviewCapability({ ffmpegCheck: async () => null });
  assert.equal(cap.ready, false);
  assert.deepEqual(cap.presets, []);
  const ffmpegCheck = cap.checks.find((c) => c.code === 'ffmpeg');
  assert.ok(ffmpegCheck);
  assert.equal(ffmpegCheck.status, 'blocked');
});

test('probePreviewCapability blocks ready when ffprobe is missing', async () => {
  const cap = await probePreviewCapability({ ffprobeCheck: async () => null });
  assert.equal(cap.ready, false);
  assert.deepEqual(cap.presets, []);
  const probeCheck = cap.checks.find((c) => c.code === 'ffprobe');
  assert.ok(probeCheck);
  assert.equal(probeCheck.status, 'blocked');
});

test('probePreviewCapability blocks ready when preset fails to load', async () => {
  const cap = await probePreviewCapability({
    loadPresetImpl: async () => { throw new Error('corrupted preset'); }
  });
  assert.equal(cap.ready, false);
  assert.deepEqual(cap.presets, []);
  const presetCheck = cap.checks.find((c) => c.code === 'preset');
  assert.ok(presetCheck);
  assert.equal(presetCheck.status, 'blocked');
});

test('probePreviewCapability respects subtitle font requirement and bypasses when subtitles not required', async () => {
  const withoutSubtitles = await probePreviewCapability({
    hasSubtitles: false,
    findFontImpl: async () => null,
    ffmpegCheck: async () => 'ffmpeg-stub',
    ffprobeCheck: async () => 'ffprobe-stub'
  });
  assert.equal(withoutSubtitles.ready, true);
  const fontCheckNoSub = withoutSubtitles.checks.find((c) => c.code === 'font');
  assert.equal(fontCheckNoSub.status, 'passed');

  const withSubtitles = await probePreviewCapability({
    hasSubtitles: true,
    findFontImpl: async () => null,
    ffmpegCheck: async () => 'ffmpeg-stub',
    ffprobeCheck: async () => 'ffprobe-stub'
  });
  assert.equal(withSubtitles.ready, false);
  const fontCheckSub = withSubtitles.checks.find((c) => c.code === 'font');
  assert.equal(fontCheckSub.status, 'blocked');
});

// -------------------------------------------------------------
// 2. processPreviewTask Tests
// -------------------------------------------------------------

test('processPreviewTask happy path renders and submits preview', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    let rendered = false;
    let progressReported = [];
    const client = fakeClient({
      get_task_input: () => ({ preview: fixture.input }),
      report_progress: (args) => { progressReported.push(args); return { ok: true }; },
      submit_preview: (args) => ({ ok: true, submitted: true }),
      heartbeat: () => ({ ok: true })
    });

    const result = await processPreviewTask({
      client,
      cfg: { ...baseCfg(dir), heartbeatIntervalMs: 20 },
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async (opts) => {
        rendered = true;
        assert.equal(opts.input.preview_id, fixture.input.preview_id);
        opts.onProgress?.({ phase: 'rendering', progress: 0.5, message: 'encoding' });
        await new Promise((r) => setTimeout(r, 50));
      }
    });

    assert.equal(result.outcome, 'succeeded');
    assert.equal(result.package, `previews/${fixture.input.asset_id}/${fixture.input.revision_id}/${fixture.input.preview_id}`);
    assert.equal(rendered, true);

    const submitCall = client.calls.find(([tool]) => tool === 'submit_preview');
    assert.ok(submitCall);
    assert.deepEqual(submitCall[1], {
      task_id: defaultTask.task_id,
      runtime_instance_id: defaultScope.runtime_instance_id,
      execution_id: defaultScope.execution_id, generation: defaultScope.generation,
      input_sha256: defaultScope.input_sha256,
      preview_dir: `previews/${fixture.input.asset_id}/${fixture.input.revision_id}/${fixture.input.preview_id}`
    });

    assert.ok(client.calls.some(([tool]) => tool === 'heartbeat'));
    assert.ok(!client.calls.some(([tool]) => tool === 'fail_task'));
  } finally {
    await cleanup();
  }
});

test('processPreviewTask recovers existing completed preview without re-rendering', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const previewRel = `previews/${fixture.input.asset_id}/${fixture.input.revision_id}/${fixture.input.preview_id}`;
    const previewAbs = join(dir, ...previewRel.split('/'));
    await mkdir(previewAbs, { recursive: true });

    await writeFile(join(previewAbs, PREVIEW_FILENAMES.media), 'dummy-mp4-data');
    await writeFile(join(previewAbs, PREVIEW_FILENAMES.receipt), JSON.stringify({
      schema_version: 1,
      preview_id: fixture.input.preview_id,
      task_id: fixture.input.task_id,
      input_sha256: fixture.input.input_sha256,
      preset_sha256: fixture.input.preset_sha256,
      renderer_version: '1.0.0',
      ffmpeg_version: '7.1'
    }));
    await writeFile(join(previewAbs, PREVIEW_FILENAMES.manifest), JSON.stringify({
      schema_version: 1,
      preview_id: fixture.input.preview_id,
      task_id: fixture.input.task_id,
      input_sha256: fixture.input.input_sha256,
      preset_sha256: fixture.input.preset_sha256,
      artifacts: []
    }));

    let renderRan = false;
    const client = fakeClient({
      get_task_input: () => ({ preview: fixture.input }),
      submit_preview: () => ({ ok: true, submitted: true })
    });

    const result = await processPreviewTask({
      client,
      cfg: baseCfg(dir),
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => { renderRan = true; }
    });

    assert.equal(result.outcome, 'succeeded');
    assert.equal(result.recovered, true);
    assert.equal(renderRan, false);
    assert.ok(client.calls.some(([tool]) => tool === 'submit_preview'));
  } finally {
    await cleanup();
  }
});

test('processPreviewTask rejects mismatched existing preview directory', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const previewRel = `previews/${fixture.input.asset_id}/${fixture.input.revision_id}/${fixture.input.preview_id}`;
    const previewAbs = join(dir, ...previewRel.split('/'));
    await mkdir(previewAbs, { recursive: true });

    await writeFile(join(previewAbs, PREVIEW_FILENAMES.media), 'dummy-mp4-data');
    await writeFile(join(previewAbs, PREVIEW_FILENAMES.receipt), JSON.stringify({
      schema_version: 1,
      preview_id: fixture.input.preview_id,
      task_id: fixture.input.task_id,
      input_sha256: 'mismatched-sha256',
      preset_sha256: fixture.input.preset_sha256,
      renderer_version: '1.0.0',
      ffmpeg_version: '7.1'
    }));
    await writeFile(join(previewAbs, PREVIEW_FILENAMES.manifest), JSON.stringify({
      schema_version: 1,
      preview_id: fixture.input.preview_id,
      task_id: fixture.input.task_id,
      input_sha256: fixture.input.input_sha256,
      preset_sha256: fixture.input.preset_sha256,
      artifacts: []
    }));

    const client = fakeClient({
      get_task_input: () => ({ preview: fixture.input })
    });

    const result = await processPreviewTask({
      client,
      cfg: baseCfg(dir),
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => {}
    });

    assert.equal(result.outcome, 'failed');
    assert.match(result.reason, /existing preview directory belongs to a different task input/);
    assert.ok(client.calls.some(([tool]) => tool === 'fail_task'));
  } finally {
    await cleanup();
  }
});

test('processPreviewTask aborts without fail_task when lease is lost during heartbeat', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    let leaseLost = false;
    const client = fakeClient({
      get_task_input: () => ({ preview: fixture.input }),
      heartbeat: () => {
        throw new ToolError('heartbeat', 'lease_expired: lease lapsed');
      }
    });

    const result = await processPreviewTask({
      client,
      cfg: { ...baseCfg(dir), heartbeatIntervalMs: 20 },
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async ({ signal }) => {
        return new Promise((resolve, reject) => {
          signal.addEventListener('abort', () => {
            leaseLost = true;
            reject(new Error('render aborted'));
          });
        });
      }
    });

    assert.equal(result.outcome, 'abandoned');
    assert.equal(leaseLost, true);
    assert.ok(!client.calls.some(([tool]) => tool === 'fail_task'));
    assert.ok(!client.calls.some(([tool]) => tool === 'submit_preview'));
  } finally {
    await cleanup();
  }
});

test('processPreviewTask reports failure with fail_task when renderer fails', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const client = fakeClient({
      get_task_input: () => ({ preview: fixture.input })
    });

    const result = await processPreviewTask({
      client,
      cfg: baseCfg(dir),
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => {
        throw new WorkerFailure('FFmpeg exited 1: unsupported codec');
      }
    });

    assert.equal(result.outcome, 'failed');
    assert.match(result.reason, /unsupported codec/);
    const failCall = client.calls.find(([tool]) => tool === 'fail_task');
    assert.ok(failCall);
    assert.match(failCall[1].reason, /unsupported codec/);
  } finally {
    await cleanup();
  }
});

test('processPreviewTask validates input structure and rejects invalid attributes', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    // Escaping package_ref
    const badInput = structuredClone(fixture.input);
    badInput.package_ref = '../outside';
    const client = fakeClient({
      get_task_input: () => ({ preview: badInput })
    });

    const result = await processPreviewTask({
      client,
      cfg: baseCfg(dir),
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => {}
    });

    assert.equal(result.outcome, 'failed');
    assert.match(result.reason, /invalid package_ref/);
    assert.ok(client.calls.some(([tool]) => tool === 'fail_task'));
  } finally {
    await cleanup();
  }
});

test('processPreviewTask fails cleanly when workflow cancels task', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const client = fakeClient({
      get_task_input: () => ({ cancelled: true, preview: fixture.input })
    });

    const result = await processPreviewTask({
      client,
      cfg: baseCfg(dir),
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => {}
    });

    assert.equal(result.outcome, 'failed');
    assert.match(result.reason, /workflow cancelled/);
    assert.ok(client.calls.some(([tool]) => tool === 'fail_task'));
  } finally {
    await cleanup();
  }
});

// -------------------------------------------------------------
// 3. Worker processTask and runLoop Integration Tests
// -------------------------------------------------------------

test('processTask dispatches preview task to processPreviewTask', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    let previewInvoked = false;
    const client = fakeClient({
      get_task_input: () => ({ preview: fixture.input }),
      submit_preview: () => ({ ok: true, submitted: true })
    });

    const result = await processTask({
      client,
      cfg: baseCfg(dir),
      task: defaultTask, executionScope: defaultScope,
      renderPreviewImpl: async () => { previewInvoked = true; }
    });

    assert.equal(result.outcome, 'succeeded');
    assert.equal(previewInvoked, true);
  } finally {
    await cleanup();
  }
});

test('runLoop registers preview_capability when submit_preview tool is present', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const mockCap = {
      protocol_version: 1,
      execution_protocol: 2,
      ready: true,
      presets: [{ preset_id: 'mp4-720p-v1', sha256: 'abc' }],
      checks: [{ code: 'ffmpeg', status: 'passed', message: 'ok' }]
    };

    let claimed = false;
    const client = {
      listTools: async () => [
        'claim_task', 'heartbeat', 'report_progress', 'get_asset',
        'get_task_input', 'get_task_status', 'submit_delivery', 'fail_task', 'submit_preview'
      ],
      call: async (tool, args) => {
        if (tool === 'claim_task') {
          if (!claimed) {
            claimed = true;
            return { claimed: true, task: defaultTask, scope: defaultScope };
          }
          return { claimed: false };
        }
        if (tool === 'get_task_input') return { preview: fixture.input };
        if (tool === 'submit_preview') return { submitted: true };
        return { ok: true };
      }
    };

    let previewRan = false;
    const result = await runLoop({
      client,
      cfg: { ...baseCfg(dir), token: 'token-abc' },
      log: () => {},
      signal: new AbortController().signal,
      once: true,
      renderPreviewImpl: async () => { previewRan = true; },
      probePreviewCapabilityImpl: async () => mockCap
    });

    assert.equal(result.outcome, 'succeeded');
    assert.equal(previewRan, true);
  } finally {
    await cleanup();
  }
});

test('preview task heartbeats keep the advertised capability alive while rendering', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    const capability = { protocol_version: 1, ready: true, presets: [], checks: [] };
    let renewals = 0;
    const client = fakeClient({
      get_task_input: async () => ({ preview: fixture.input }),
      heartbeat: async args => { if (args.task_id) { assert.deepEqual(args.preview_capability, capability); renewals++; } }
    });
    const result = await processPreviewTask({client, cfg:{...baseCfg(dir), heartbeatIntervalMs:20}, task:defaultTask, executionScope: defaultScope,
      previewCapability:capability, renderPreviewImpl: async () => {await new Promise(resolve=>setTimeout(resolve,100));}});
    assert.equal(result.outcome,'succeeded');
    assert.ok(renewals>=1);
  } finally {await cleanup();}
});

test('runLoop does not include preview_capability when submit_preview tool is missing', async () => {
  const { dir, cleanup } = await createTestRoot();
  try {
    let hbCaptured = null;
    const client = {
      listTools: async () => [
        'claim_task', 'heartbeat', 'report_progress', 'get_asset',
        'get_task_input', 'submit_delivery', 'fail_task'
      ],
      call: async (tool, args) => {
        if (tool === 'heartbeat') {
          hbCaptured = args;
          return { ok: true };
        }
        if (tool === 'claim_task') return { claimed: false };
        return { ok: true };
      }
    };

    const result = await runLoop({
      client,
      cfg: { ...baseCfg(dir), token: 'token-abc' },
      log: () => {},
      signal: new AbortController().signal,
      once: true
    });

    assert.equal(result.outcome, 'idle');
    assert.ok(hbCaptured);
    assert.equal(hbCaptured.preview_capability, undefined);
  } finally {
    await cleanup();
  }
});
