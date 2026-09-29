import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, realpath } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ToolError, createClient } from '../src/mcp.mjs';
import { loadConfig, processTask, resolveUnderRoot, WorkerFailure } from '../src/worker.mjs';

async function root() {
  const dir = await realpath(await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'vac-worker-')));
  await mkdir(join(dir, 'src'), { recursive: true });
  await writeFile(join(dir, 'src', 'edl.json'), '{}');
  return dir;
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
  deliveryRoot, outputPrefix: 'deliveries', heartbeatIntervalMs: 10_000, taskTimeoutMs: 1000,
  nodePath: 'node', cliPath: '/cli.mjs', adaptersPath: '/a.mjs'
});
const task = { task_id: 't1', asset_id: 'clip', type: 'export' };

test('success path reports progress and submits delivery', async () => {
  const dir = await root();
  const client = fakeClient({ get_asset: () => ({ asset: { artifacts: { edl: 'src/edl.json' } } }) });
  let cliArgs;
  const r = await processTask({
    client, cfg: baseCfg(dir), task, log: () => {},
    runCliImpl: async (a) => { cliArgs = a; }
  });
  assert.equal(r.outcome, 'succeeded');
  assert.equal(cliArgs.edlPath, join(dir, 'src', 'edl.json'));
  assert.equal(cliArgs.outputDir, join(dir, 'deliveries', 'clip', 't1'));
  const submit = client.calls.find(([t]) => t === 'submit_delivery');
  assert.deepEqual(submit[1], { task_id: 't1', package_dir: 'deliveries/clip/t1' });
  assert.ok(!client.calls.some(([t]) => t === 'fail_task'));
});

test('cli failure is reported with fail_task', async () => {
  const dir = await root();
  const client = fakeClient({ get_asset: () => ({ asset: { artifacts: { edl: 'src/edl.json' } } }) });
  const r = await processTask({
    client, cfg: baseCfg(dir), task, log: () => {},
    runCliImpl: async () => { throw new WorkerFailure('cli exited 1: bad fps'); }
  });
  assert.equal(r.outcome, 'failed');
  const fail = client.calls.find(([t]) => t === 'fail_task');
  assert.match(fail[1].reason, /bad fps/);
});

test('escaping or missing edl paths fail the task without running cli', async () => {
  const dir = await root();
  for (const edl of ['../outside.json', 'C:/abs.json', 'src\\edl.json', 'nope.json', undefined]) {
    const client = fakeClient({ get_asset: () => ({ asset: { artifacts: { edl } } }) });
    let ran = false;
    const r = await processTask({ client, cfg: baseCfg(dir), task, log: () => {}, runCliImpl: async () => { ran = true; } });
    assert.equal(r.outcome, 'failed', String(edl));
    assert.equal(ran, false);
  }
});

test('already exported asset is refused before running cli', async () => {
  const dir = await root();
  const client = fakeClient({ get_asset: () => ({ asset: { status: 'exported', artifacts: { edl: 'src/edl.json' } } }) });
  let ran = false;
  const r = await processTask({ client, cfg: baseCfg(dir), task, log: () => {}, runCliImpl: async () => { ran = true; } });
  assert.equal(r.outcome, 'failed');
  assert.match(r.reason, /already exported/);
  assert.equal(ran, false);
});

test('unsafe ids and wrong type are refused', async () => {
  const dir = await root();
  for (const t of [{ ...task, type: 'tts' }, { ...task, asset_id: '..' }, { ...task, task_id: 'a/b' }]) {
    const client = fakeClient({});
    const r = await processTask({ client, cfg: baseCfg(dir), task: t, log: () => {}, runCliImpl: async () => {} });
    assert.equal(r.outcome, 'failed');
  }
});

test('expired lease abandons without fail_task', async () => {
  const dir = await root();
  const client = fakeClient({
    get_asset: () => ({ asset: { artifacts: { edl: 'src/edl.json' } } }),
    submit_delivery: () => { throw new ToolError('submit_delivery', 'lease_expired: lease lapsed'); }
  });
  const r = await processTask({ client, cfg: baseCfg(dir), task, log: () => {}, runCliImpl: async () => {} });
  assert.equal(r.outcome, 'abandoned');
  assert.ok(!client.calls.some(([t]) => t === 'fail_task'));
});

test('resolveUnderRoot rejects traversal', async () => {
  const dir = await root();
  await assert.rejects(resolveUnderRoot(dir, '../x'), WorkerFailure);
  assert.equal(await resolveUnderRoot(dir, 'src/edl.json'), join(dir, 'src', 'edl.json'));
});

test('config requires token from env and absolute paths', async () => {
  const dir = await root();
  const p = join(dir, 'w.json');
  const good = { mcp_url: 'http://127.0.0.1:1/mcp', delivery_root: dir, cli_path: join(dir, 'c.mjs'), adapters_path: join(dir, 'a.mjs') };
  await writeFile(p, JSON.stringify(good));
  await assert.rejects(loadConfig(p, {}), /VAC_WORKER_TOKEN/);
  const cfg = await loadConfig(p, { VAC_WORKER_TOKEN: 'x'.repeat(40) });
  assert.equal(cfg.deliveryRoot, dir);
  await writeFile(p, JSON.stringify({ ...good, cli_path: 'rel.mjs' }));
  await assert.rejects(loadConfig(p, { VAC_WORKER_TOKEN: 'x'.repeat(40) }), /absolute/);
  await writeFile(p, JSON.stringify({ ...good, output_prefix: '../up' }));
  await assert.rejects(loadConfig(p, { VAC_WORKER_TOKEN: 'x'.repeat(40) }), /output_prefix/);
});

test('mcp client maps isError to ToolError code', async () => {
  const fetchImpl = async () => new Response(JSON.stringify({
    jsonrpc: '2.0', id: 1, result: { isError: true, content: [{ type: 'text', text: 'not_found: task does not exist' }] }
  }), { status: 200 });
  const c = createClient({ url: 'http://x/mcp', token: 't'.repeat(40), fetchImpl });
  await assert.rejects(c.call('get_task_status', { task_id: 'x' }), (e) => e instanceof ToolError && e.code === 'not_found');
  assert.throws(() => createClient({ url: 'http://x', token: 'short' }), /token/);
});
