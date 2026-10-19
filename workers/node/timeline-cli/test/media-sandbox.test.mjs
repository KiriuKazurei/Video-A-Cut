import test from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { mkdtemp, writeFile, rm, realpath, lstat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, dirname, resolve, sep } from 'node:path';
import { probeMedia } from '../src/media.mjs';

test('media probing refuses a disguised network playlist before making HTTP requests', async () => {
  let requests = 0;
  const server = createServer((_, response) => { requests++; response.writeHead(404); response.end(); });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const root = await mkdtemp(join(tmpdir(), 'vac-media-boundary-'));
  try {
    const path = join(root, 'untrusted.mp4');
    await writeFile(path, `#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nhttp://127.0.0.1:${server.address().port}/hidden.ts\n#EXT-X-ENDLIST\n`);
    await assert.rejects(() => probeMedia(path));
    assert.equal(requests, 0);
  } finally {
    await new Promise(resolve => server.close(resolve));
    const actual = await realpath(root);
    assert.equal(dirname(actual), await realpath(tmpdir()));
    assert.equal((await lstat(root)).isSymbolicLink(), false);
    assert(process.env.VAC_TEST_ROOT && actual.toLowerCase().startsWith((resolve(process.env.VAC_TEST_ROOT) + sep).toLowerCase()));
    await rm(root, { recursive: true, force: true });
  }
});
