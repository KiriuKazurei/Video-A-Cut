import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { fingerprintPreviewInput, isPreviewRelativePath, loadPreviewPreset } from '../src/preview-protocol.mjs';
import { renderPreview, PreviewError, PreviewNotImplementedError } from '../src/preview.mjs';

const fixture = JSON.parse(await readFile(new URL('../../../../contracts/mp4-preview/v1/fixtures/input.json', import.meta.url), 'utf8'));

test('shared UTF-8 golden fingerprint and preset bytes agree', async () => {
  const loaded = await loadPreviewPreset();
  assert.equal(loaded.sha256, fixture.input.preset_sha256);
  assert.deepEqual(loaded.preset, fixture.input.preset);
  assert.equal(fingerprintPreviewInput(fixture.input), fixture.input.input_sha256);
});

test('input identity survives file ordering and new attempt IDs; revision/media changes invalidate it', () => {
  const changed = structuredClone(fixture.input);
  changed.preview_id = 'another-attempt'; changed.task_id = 'another-task'; changed.files.reverse();
  assert.equal(fingerprintPreviewInput(changed), fixture.input.input_sha256);
  changed.revision_id = 'another-revision';
  assert.notEqual(fingerprintPreviewInput(changed), fixture.input.input_sha256);
  changed.revision_id = fixture.input.revision_id;
  changed.files[0].sha256 = '0'.repeat(64);
  assert.notEqual(fingerprintPreviewInput(changed), fixture.input.input_sha256);
});

test('wire path/hash/size guards reject unsafe or ambiguous fingerprint input', async () => {
  for (const path of ['/outside', '../outside', 'a/../b', 'a//b', 'C:/outside', 'a\\b', 'a\0b', 'a/']) {
    assert.equal(isPreviewRelativePath(path), false, path);
    const changed = structuredClone(fixture.input); changed.files[0].path = path;
    assert.throws(() => fingerprintPreviewInput(changed));
  }
  const duplicate = structuredClone(fixture.input); duplicate.files.push({...duplicate.files[0]});
  assert.throws(() => fingerprintPreviewInput(duplicate));
  for (const size of [-1, 0.5, Number.MAX_SAFE_INTEGER + 1]) {
    const changed = structuredClone(fixture.input); changed.files[0].size = size;
    assert.throws(() => fingerprintPreviewInput(changed));
  }
  const badHash = structuredClone(fixture.input); badHash.edl_sha256 = 'not-a-sha';
  assert.throws(() => fingerprintPreviewInput(badHash));
  await assert.rejects(loadPreviewPreset('../arbitrary'), /unsupported preview preset/);
});

test('renderer fails closed on missing package and preserves caller abort', async () => {
  await assert.rejects(renderPreview({input:fixture.input, packageDir:'', outputDir:''}),
    (error) => error instanceof PreviewError && error.code === 'package_not_found');
  const controller = new AbortController(); const reason = new Error('caller cancelled'); controller.abort(reason);
  await assert.rejects(renderPreview({signal:controller.signal}), (error) => error === reason);
});
