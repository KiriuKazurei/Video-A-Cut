import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { processDeliveryVariantTask, probeDeliveryVariantCapability } from '../src/delivery-variant.mjs';
import { ToolError } from '../src/mcp.mjs';
import { createHash } from 'node:crypto';
import { loadVariantSpec, variantFingerprint } from '../../timeline-cli/src/delivery-variant.mjs';
import { canonicalJSON, defaultStyle } from '../../../../contracts/delivery-variant/v1/style.mjs';
import { runLoop } from '../src/worker.mjs';
const golden = JSON.parse(await readFile(new URL('../../../../contracts/delivery-variant/v1/fixtures/input.json', import.meta.url)));
const input = { ...golden.input, asset_id: 'asset_fixture', parent_package_ref: 'exports/fixture' };
input.input_sha256 = variantFingerprint(input);
const task = { task_id: input.task_id, asset_id: input.asset_id, type: 'delivery_variant' };
async function setup(t, handlers = {}, taskInput = input) {
  const root = await mkdtemp(join(process.env.TMPDIR || tmpdir(), 'vac-ass-worker-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(join(root, 'exports/fixture'), { recursive: true });
  const calls = []; const client = { async call(tool, args, options) { calls.push([tool, args]); return handlers[tool] ? handlers[tool](args, options) : tool === 'get_task_input' ? { delivery_variant: taskInput } : { ok: true }; } };
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
  const { bytes } = await loadVariantSpec();
  await writeFile(join(output, 'ass-style.json'), bytes);
  const artifacts = [
    {key:'ass',path:'subtitles.ass',size:13,sha256:createHash('sha256').update('[Script Info]').digest('hex')},
    {key:'style',path:'ass-style.json',size:bytes.length,sha256:createHash('sha256').update(bytes).digest('hex')}
  ];
  const receipt = { ...binding, converter_version:'vac.ass-basic-v1/1' };
  await writeFile(join(output, 'variant-manifest.json'), JSON.stringify({...binding, artifacts}));
  await writeFile(join(output, 'variant-receipt.json'), JSON.stringify({ ...receipt, task_id: 'foreign' }));
  const bad = await processDeliveryVariantTask({ ...f, buildVariantImpl: async () => assert.fail('rerendered foreign output') });
  assert.equal(bad.outcome, 'failed'); assert(!f.calls.some(([n]) => n === 'submit_delivery_variant'));
  await writeFile(join(output, 'variant-receipt.json'), JSON.stringify(receipt));
  const recovered = await processDeliveryVariantTask({ ...f, buildVariantImpl: async () => assert.fail('rerendered recoverable output') });
  assert.equal(recovered.outcome, 'succeeded'); assert.equal(recovered.recovered, true);
});

async function editableInput() {
  const registered = await loadVariantSpec('ass-basic-v2');
  const document = { source_srt_sha256: input.source_files.find(f => f.kind === 'srt').sha256,
    play_res_x:1280,play_res_y:720,style:defaultStyle(),overrides:[{cue_id:1,style:{font_family:'Fixture Serif'}}] };
  const result = { ...input, schema_version:2, spec_id:'ass-basic-v2',spec:registered.spec,spec_sha256:registered.sha256,
    style_document:document, style_sha256:createHash('sha256').update(canonicalJSON(document)).digest('hex') };
  result.input_sha256 = variantFingerprint(result); return result;
}

test('v2 worker passes the frozen per-cue style to its converter without a renderer dependency', async t => {
  const value = await editableInput(), f = await setup(t, {}, value);
  let invoked=false;
  const ok = await processDeliveryVariantTask({ ...f, buildVariantImpl:async args=>{
    invoked=true;assert.deepEqual(args.input.style_document,value.style_document);
    assert.equal(args.input.style_sha256,value.style_sha256);
  } });
  assert(invoked);
  assert.equal(ok.outcome,'succeeded');
});

test('v2 recovery reuses the matching style snapshot and rejects changed ASS bytes', async t => {
  const value = await editableInput(), f = await setup(t, {}, value);
  const output = join(f.root, `delivery-variants/${task.asset_id}/${value.variant_id}`); await mkdir(output,{recursive:true});
  const binding = Object.fromEntries(['schema_version','variant_id','task_id','asset_id','run_id','revision_id','input_sha256','spec_sha256'].map(k=>[k,value[k]]));
  const files = [['ass','subtitles.ass','[Script Info]'],['style','ass-style.json',canonicalJSON(value.style_document)]];
  const artifacts = [];
  for (const [key,path,bytes] of files) {await writeFile(join(output,path),bytes);artifacts.push({key,path,size:Buffer.byteLength(bytes),sha256:createHash('sha256').update(bytes).digest('hex')});}
  await writeFile(join(output,'variant-manifest.json'),JSON.stringify({...binding,artifacts}));
  await writeFile(join(output,'variant-receipt.json'),JSON.stringify({...binding,converter_version:'vac.ass-basic-v2/1'}));
  const options = { ...f, buildVariantImpl:async()=>assert.fail('re-rendered a complete v2 bundle') };
  const recovered = await processDeliveryVariantTask(options);
  assert.equal(recovered.outcome,'succeeded');assert.equal(recovered.recovered,true);
  await writeFile(join(output,'subtitles.ass'),'changed bytes');
  const changed = await processDeliveryVariantTask(options);
  assert.equal(changed.outcome,'failed');assert.match(changed.reason,/file changed/);
});

test('font capability refresh publishes families and hashes without local paths', async () => {
  let generation=0;
  const probe = () => probeDeliveryVariantCapability({refresh:true,installedFontsImpl:async refresh=>{
    assert.equal(refresh,true);
    const family=++generation===1?'Before':'After';
    return [{family,sha256:'a'.repeat(64),path:'private.ttf'},{family,sha256:'b'.repeat(64),path:'private-bold.ttf'}];
  }});
  assert.deepEqual((await probe()).fonts,[{family:'Before',sha256:'a'.repeat(64)}]);
  assert.deepEqual((await probe()).fonts,[{family:'After',sha256:'a'.repeat(64)}]);
  const unavailable=await probeDeliveryVariantCapability({installedFontsImpl:async()=>{throw Error('fontkit unavailable');}});
  assert.equal(unavailable.ready,true);assert.deepEqual(unavailable.fonts,[]);
});

test('idle worker heartbeats refresh an already-ready font catalogue', async () => {
  const original=Date.now;let clock=0,probes=0;const beats=[];
  const client={listTools:async()=>['claim_task','heartbeat','report_progress','get_asset','get_task_input','submit_delivery','submit_delivery_variant','fail_task'],
    call:async(name,args)=>{if(name==='heartbeat'){beats.push(args);return {}}return {claimed:false}}};
  try {
    Date.now=()=>{clock+=6000;return clock;};
    const result=await runLoop({client,cfg:{pollIntervalMs:1},signal:new AbortController().signal,once:true,log:()=>{},
      probeDeliveryVariantCapabilityImpl:async()=>({protocol_version:1,ready:true,specs:[],fonts:[{family:++probes===1?'Before':'After',sha256:'a'.repeat(64)}]})});
    assert.equal(result.outcome,'idle');assert.equal(probes,2);
    assert.equal(beats[0].delivery_variant_capability.fonts[0].family,'Before');
    assert.equal(beats[1].delivery_variant_capability.fonts[0].family,'After');
  } finally {Date.now=original;}
});

test('ASS task heartbeats publish a refreshed catalogue while building', async t => {
  const f=await setup(t);
  const result=await processDeliveryVariantTask({...f,
    deliveryVariantCapability:{protocol_version:1,ready:true,specs:[],fonts:[{family:'Before',sha256:'a'.repeat(64)}]},
    probeCapabilityImpl:async()=>({protocol_version:1,ready:true,specs:[],fonts:[{family:'After',sha256:'b'.repeat(64)}]}),
    buildVariantImpl:async()=>{await new Promise(resolve=>setTimeout(resolve,40));}});
  assert.equal(result.outcome,'succeeded');
  const beats=f.calls.filter(([name])=>name==='heartbeat');assert(beats.length>0);
  assert(beats.every(([,args])=>args.delivery_variant_capability.fonts[0].family==='After'));
});
