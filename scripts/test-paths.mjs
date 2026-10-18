import { readFileSync, mkdirSync, readdirSync, existsSync, lstatSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const root = path.join(repo, '.run-data', 'tests');
const version = JSON.parse(readFileSync(path.join(repo, 'desktop', 'tauri.conf.json'), 'utf8')).version;
if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(version)) throw new Error('Invalid test version');
export const testStorageRoot = root;

export function testDirectory(name = '') {
  const base = path.resolve(process.env.VAC_TEST_ROOT || path.join(root, version, 'artifacts'));
  const output = path.resolve(base, name);
  if (!base.startsWith(root + path.sep) || (output !== base && !output.startsWith(base + path.sep))) throw new Error('Test output must remain in versioned test storage');
  for (let cursor = output; cursor !== path.dirname(cursor); cursor = path.dirname(cursor)) {
    let entry;
    try { entry = lstatSync(cursor); } catch (error) { if (error.code !== 'ENOENT') throw error; }
    if (entry?.isSymbolicLink()) throw new Error('Linked test output is not allowed');
  }
  const versionRoot = path.join(root, path.relative(root, base).split(path.sep)[0]);
  const marker = path.join(versionRoot, '.test-workspace.json');
  const currentVersion = path.basename(versionRoot);
  if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(currentVersion)) throw new Error('Invalid output version');
  if (existsSync(marker)) {
    const owner = JSON.parse(readFileSync(marker, 'utf8'));
    if (owner.schema !== 1 || owner.version !== currentVersion) throw new Error('Invalid test workspace marker');
  } else {
    if (existsSync(versionRoot) && readdirSync(versionRoot).length) throw new Error('Unmarked test workspace');
    mkdirSync(versionRoot, { recursive: true });
    writeFileSync(marker, JSON.stringify({ schema: 1, version: currentVersion }));
  }
  mkdirSync(output, { recursive: true });
  return output;
}
