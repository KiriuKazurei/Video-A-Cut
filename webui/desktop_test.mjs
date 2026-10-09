import test from 'node:test';
import assert from 'node:assert/strict';
import { setApiBase, apiURL } from './src/runtimeUrl.ts';
import { previewFileURL } from './src/previewContracts.ts';
import { variantFileURL, variantZIPURL } from './src/deliveryVariantContracts.ts';
import { createWorkflowPreview } from './src/previewContext.ts';

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
