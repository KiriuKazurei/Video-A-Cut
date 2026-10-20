import test from 'node:test';
import assert from 'node:assert/strict';
import { setApiBase, apiURL } from './src/runtimeUrl.ts';
import { previewFileURL } from './src/previewContracts.ts';
import { variantFileURL, variantZIPURL } from './src/deliveryVariantContracts.ts';
import { createWorkflowPreview } from './src/previewContext.ts';
import { currentMcpURL, mcpConnectionConfig } from './src/mcpConnection.ts';

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
