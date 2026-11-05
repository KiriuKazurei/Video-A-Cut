import test from 'node:test';
import assert from 'node:assert/strict';
import { setApiBase, apiURL } from './src/runtimeUrl.ts';
import { previewFileURL } from './src/previewContracts.ts';
import { variantFileURL, variantZIPURL } from './src/deliveryVariantContracts.ts';
import { createWorkflowPreview } from './src/previewContext.ts';
import { currentMcpURL, mcpConnectionConfig, mcpInstallCommand } from './src/mcpConnection.ts';
import * as presence from './src/externalAgentStatus.ts';
import { EXPORT_SECTIONS, parseWorkspaceHash } from './src/navigation.ts';
import { playbackClipAt, playbackSourceTime, setSegmentWindow, segmentedProgramPlayer } from './src/programPlayback.ts';

for(const earlyMetadata of [false,true])test(`cold segment restoration seeks when media ranges lag append (${earlyMetadata?'early':'late'} metadata)`,async t=>{
 const globals=['window','document','MediaSource','requestAnimationFrame','cancelAnimationFrame'];
 const saved=new Map(globals.map(key=>[key,Object.getOwnPropertyDescriptor(globalThis,key)]));
 const createURL=URL.createObjectURL,revokeURL=URL.revokeObjectURL;let player;
 t.after(()=>{player?.close();for(const [key,descriptor] of saved){if(descriptor)Object.defineProperty(globalThis,key,descriptor);else delete globalThis[key];}URL.createObjectURL=createURL;URL.revokeObjectURL=revokeURL;});
 const ranges=()=>({length:1,start:()=>2,end:()=>3.98802});
 class Video extends EventTarget{
  readyState=0;paused=true;buffered={length:0};time=0;
  get currentTime(){return this.time;}set currentTime(value){this.time=value;queueMicrotask(()=>{if(this.buffered.length){this.readyState=2;this.dispatchEvent(new Event('seeked'));}});}
  pause(){this.paused=true;}play(){this.paused=false;return Promise.resolve();}setAttribute(){}removeAttribute(){}load(){}remove(){}
 }
 const video=new Video();
 class Buffer extends EventTarget{
  buffered={length:0};
  appendBuffer(){queueMicrotask(()=>{this.buffered=ranges();if(earlyMetadata){video.readyState=1;video.dispatchEvent(new Event('loadedmetadata'));}this.dispatchEvent(new Event('updateend'));setTimeout(()=>{video.buffered=ranges();video.readyState=1;if(!earlyMetadata)video.dispatchEvent(new Event('loadedmetadata'));if(video.currentTime>0){video.readyState=2;video.dispatchEvent(new Event('seeked'));}},0);});}
 }
 class Source extends EventTarget{
  static isTypeSupported(){return true;}readyState='closed';
  constructor(){super();queueMicrotask(()=>{this.readyState='open';this.dispatchEvent(new Event('sourceopen'));});}
  addSourceBuffer(){return new Buffer();}endOfStream(){this.readyState='ended';}
 }
 Object.assign(globalThis,{window:{MediaSource:Source},MediaSource:Source,document:{createElement:()=>video},requestAnimationFrame:()=>1,cancelAnimationFrame:()=>{}});
 URL.createObjectURL=()=> 'blob:cold-segment';URL.revokeObjectURL=()=>{};
 const readiness=[],errors=[];
 player=segmentedProgramPlayer({append(){},dataset:{}},{frames:120,fps:{num:30,den:1},segment_frames:60},async()=>({report:{start_frame:60,frames:60},bytes:new ArrayBuffer(0)}),{media(){},frame(){},ready:value=>readiness.push(value),error:error=>errors.push(error)},119/30);
 await new Promise(resolve=>setTimeout(resolve,40));
 assert.deepEqual(errors,[]);assert.equal(video.currentTime,119/30,'restored playhead must seek into the buffered final segment');assert.equal(readiness.at(-1),true);
});

test('segment append windows can advance to the preceding end and seek backwards',()=>{
 let start=0,end=2,offset=0;
 const buffer={get timestampOffset(){return offset;},set timestampOffset(v){offset=v;},get appendWindowStart(){return start;},set appendWindowStart(v){assert(v<end);start=v;},get appendWindowEnd(){return end;},set appendWindowEnd(v){assert(v>start);end=v;}};
 setSegmentWindow(buffer,2,4);assert.equal(start,2);assert.equal(end,4);assert.equal(offset,2);
 setSegmentWindow(buffer,0,2);assert.equal(start,0);assert.equal(end,2);
});

test('direct playback uses half-open timeline cuts and exact source offsets',()=>{
 const clips=[{id:'a',resource_id:'a',at:0,duration:1748,source_in:0},{id:'b',resource_id:'a',at:1748,duration:52,source_in:1748/30},{id:'c',resource_id:'b',at:1800,duration:1800,source_in:0}];
 assert.equal(playbackClipAt(clips,1747)?.id,'a');assert.equal(playbackClipAt(clips,1748)?.id,'b');assert.equal(playbackClipAt(clips,1800)?.id,'c');assert.equal(playbackClipAt(clips,3600),undefined);
 assert(Math.abs(playbackSourceTime(clips[1],59,30)-59)<1e-9);assert.equal(playbackSourceTime(clips[2],61,30),1);
});

test('retired acceptance page resolves to the editing workspace',()=>{
  assert.equal(EXPORT_SECTIONS.some(section=>section.id==='acceptance'),false);
  assert.equal(parseWorkspaceHash('#/export/acceptance').workspace,'editing');
});

test('external Agent state never treats backend readiness or stale data as a live Agent', () => {
  assert.equal(typeof presence.externalAgentSummary, 'function');
  assert.equal(presence.externalAgentSummary(undefined, false).label, '正在读取 Agent 状态');
  assert.equal(presence.externalAgentSummary({ agents: [] }, false).label, '尚无 Agent 活动');
  assert.equal(presence.externalAgentSummary({ agents: [{ state: 'busy' }] }, false).label, '1 个 Agent 正在调用工具');
  assert.equal(presence.externalAgentSummary({ agents: [{ state: 'connected' }, { state: 'stale' }] }, false).label, '1 个 Agent 最近有活动');
  assert.equal(presence.externalAgentSummary({ agents: [{ state: 'stale' }] }, false).label, 'Agent 连接待确认');
  assert.equal(presence.externalAgentSummary({ agents: [{ state: 'disconnected' }] }, false).label, 'Agent 已断开');
  assert.equal(presence.externalAgentSummary({ agents: [{ state: 'busy' }] }, true).label, 'Agent 状态读取失败');
});

test('MCP install command follows the ready endpoint and rejects unavailable or unsafe URLs', () => {
  const ready = { status: 'ready', mcp_url: 'http://127.0.0.1:43210/mcp' };
  assert.equal(mcpInstallCommand(ready), 'codex mcp add vac --url http://127.0.0.1:43210/mcp');
  assert.equal(mcpInstallCommand({ ...ready, mcp_url: 'http://127.0.0.1:54321/mcp' }), 'codex mcp add vac --url http://127.0.0.1:54321/mcp');
  for (const runtime of [undefined, { ...ready, status: 'stopped' }, { ...ready, mcp_url: 'http://evil.test/mcp' }, { ...ready, mcp_url: 'http://127.0.0.1:43210/mcp?x=$(bad)' }]) assert.equal(mcpInstallCommand(runtime), '');
});

test('MCP configuration uses the current ready desktop endpoint without credentials', () => {
  const ready = { status: 'ready', mcp_url: 'http://127.0.0.1:43210/mcp' };
  assert.equal(JSON.parse(mcpConnectionConfig(ready, 'json')).mcpServers.vac.url, ready.mcp_url);
  assert.equal(mcpConnectionConfig(ready, 'codex'), '[mcp_servers.vac]\nurl = "http://127.0.0.1:43210/mcp"\n');
  const restarted = { ...ready, mcp_url: 'http://127.0.0.1:54321/mcp' };
  assert.match(mcpConnectionConfig(restarted, 'codex'), /54321/);
  assert.doesNotMatch(mcpConnectionConfig(restarted, 'json'), /43210|token|Authorization/i);
  for (const status of ['starting', 'stopping', 'stopped', 'fault']) assert.equal(mcpConnectionConfig({ ...ready, status }, 'json'), '');
  for (const mcp_url of ['http://remote.example:8787/mcp', 'http://user:pass@127.0.0.1:8787/mcp', 'http://127.0.0.1:8787/api', 'http://127.0.0.1:8787/mcp?token=secret', 'https://127.0.0.1:8787/mcp']) assert.equal(currentMcpURL({ ...ready, mcp_url }), '');
});

test('desktop URLs use one validated dynamic loopback base and preserve escaping', () => {
  try {
    setApiBase('http://127.0.0.1:43210');
    assert.equal(apiURL('/api/events'),'http://127.0.0.1:43210/api/events');
    assert.equal(previewFileURL('p/1','mp4',true),'http://127.0.0.1:43210/api/previews/p%2F1/files/mp4?download=1');
    assert.equal(variantFileURL('v 1','audio'),'http://127.0.0.1:43210/api/delivery-variants/v%201/files/audio');
    assert.equal(variantZIPURL('v'),'http://127.0.0.1:43210/api/delivery-variants/v/delivery.zip');
    for (const value of ['http://evil.test:1234','https://127.0.0.1:1234','http://127.0.0.1:1234/path','http://user@127.0.0.1:1234']) assert.throws(()=>setApiBase(value));
    assert.throws(()=>apiURL('https://evil.test'));
  } finally { setApiBase(''); }
  assert.equal(apiURL('/api/assets'),'/api/assets');
});
