import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, readFile, realpath } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ToolError, createClient } from '../src/mcp.mjs';
import { loadConfig, processTask, runLoop, resolveUnderRoot, WorkerFailure } from '../src/worker.mjs';

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

test('prepared export rejects mismatched Worker before CLI or submission', async () => {
  const dir=await root();
  const client=fakeClient({get_task_input:()=>({processing_profile:{export_target:'premiere'},profile_sha256:'a'.repeat(64)})});
  const result=await processTask({client,cfg:{...baseCfg(dir),profileSHA256:'b'.repeat(64)},task,runCliImpl:async()=>assert.fail('wrong Worker executed')});
  assert.equal(result.outcome,'failed');
  assert.match(result.reason,/fingerprint/);
  assert.ok(!client.calls.some(([name])=>name==='submit_delivery'));
});

test('prepared recovery restores provenance and does not duplicate manifest entry', async () => {
  const dir=await root();const output=join(dir,'deliveries','clip','t1');await mkdir(output,{recursive:true});
  const sha='a'.repeat(64);const profile={profile_id:'fixed',revision:1,export_target:'premiere'};
  await writeFile(join(output,'worker-receipt.json'),JSON.stringify({task_id:'t1',revision_id:'rev1',content_mode:'configured',profile_sha256:sha}));
  await writeFile(join(output,'delivery-manifest.json'),JSON.stringify({artifacts:[]}));
  const client=fakeClient({get_task_input:()=>({versioned:true,revision_id:'rev1',content_mode:'configured',edl_path:'src/edl.json',processing_profile:profile,profile_sha256:sha})});
  for(let i=0;i<2;i++){
    const result=await processTask({client,cfg:{...baseCfg(dir),profileSHA256:sha},task,runCliImpl:async()=>assert.fail('published CLI reran')});
    assert.equal(result.recovered,true);
  }
  assert.deepEqual(JSON.parse(await readFile(join(output,'processing-profile.json'),'utf8')),{profile,profile_sha256:sha});
  const manifest=JSON.parse(await readFile(join(output,'delivery-manifest.json'),'utf8'));
  assert.equal(manifest.artifacts.filter(a=>a.kind==='processing_profile').length,1);
});

test('versioned output receipt recovers publish-before-register crash', async () => {
  const dir=await root();const output=join(dir,'deliveries','clip','t1');await mkdir(output,{recursive:true});
  await writeFile(join(output,'worker-receipt.json'),JSON.stringify({task_id:'t1',revision_id:'rev1',content_mode:'configured'}));
  const client=fakeClient({get_task_input:()=>({versioned:true,revision_id:'rev1',content_mode:'configured',edl_path:'src/edl.json'})});
  const result=await processTask({client,cfg:baseCfg(dir),task,runCliImpl:async()=>assert.fail('published CLI reran')});
  assert.equal(result.recovered,true);assert.ok(client.calls.some(([name])=>name==='submit_delivery'));
});

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


test('idle capability heartbeat retries a transient disconnect', async () => {
  const original=Date.now;let clock=0,beats=0,claims=0;
  const client={listTools:async()=>['claim_task','heartbeat','report_progress','get_asset','get_task_input','submit_delivery','fail_task'],call:async(name)=>{if(name==='heartbeat'){if(++beats===2)throw Error('temporary disconnect');return {}}claims++;return {claimed:false}}};
  try{
    Date.now=()=>{clock+=6000;return clock};
    const result=await runLoop({client,cfg:{...baseCfg(await root()),profileSHA256:'a'.repeat(64),token:'test',pollIntervalMs:1},signal:new AbortController().signal,log:()=>{},once:true});
    assert.equal(result.outcome,'idle');assert.equal(beats,3);assert.equal(claims,1);
  }finally{Date.now=original}
});
