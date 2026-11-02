import test from 'node:test';
import assert from 'node:assert/strict';
import { setApiBase, apiURL } from './src/runtimeUrl.ts';
import { previewFileURL } from './src/previewContracts.ts';
import { variantFileURL, variantZIPURL } from './src/deliveryVariantContracts.ts';
import { createWorkflowPreview } from './src/previewContext.ts';
import { currentMcpURL, mcpConnectionConfig, mcpInstallCommand } from './src/mcpConnection.ts';
import * as presence from './src/externalAgentStatus.ts';
import { EXPORT_SECTIONS, parseWorkspaceHash } from './src/navigation.ts';

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
