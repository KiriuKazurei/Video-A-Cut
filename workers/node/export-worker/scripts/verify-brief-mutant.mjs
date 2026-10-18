import assert from 'node:assert/strict';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

assert.ok(process.env.VAC_TEST_OUTPUT, 'Run through TestWorkspace.ps1');
const workerURL = new URL('../src/worker.mjs', import.meta.url);
const testURL = new URL('../test/worker.test.mjs', import.meta.url);
const original = await readFile(workerURL, 'utf8');
const guard = "if (typeof taskInput.auto.brief !== 'string' || declared !== createHash('sha256').update(taskInput.auto.brief, 'utf8').digest('hex')) {";
assert.equal(original.split(guard).length, 2, 'Expected exactly one brief validation guard');
const absoluteImports = (source, base) => source.replace(/(from\s+['"])(\.[^'"]+)(['"])/g,
  (_match, left, specifier, right) => `${left}${new URL(specifier, base).href}${right}`);
const mutant = absoluteImports(original.replace(guard, 'if (false) {'), workerURL);
const mutantURL = `data:text/javascript;base64,${Buffer.from(mutant).toString('base64')}`;
const test = absoluteImports(await readFile(testURL, 'utf8'), testURL).replace(workerURL.href, mutantURL);
const output = path.join(process.env.VAC_TEST_OUTPUT, 'brief-mutant');
await mkdir(output, { recursive: true });
const testPath = path.join(output, 'worker.test.mjs');
await writeFile(testPath, test);
const result = spawnSync(process.execPath, ['--test', '--test-reporter=tap', '--test-name-pattern=^a brief whose hash does not match', testPath],
  { encoding: 'utf8', windowsHide: true, cwd: path.dirname(fileURLToPath(testURL)) });
await writeFile(path.join(output, 'result.log'), `${result.stdout ?? ''}${result.stderr ?? ''}`);
assert.ifError(result.error);
assert.equal(result.status, 1, 'Removing the guard must fail the negative test');
assert.match(result.stdout, /not ok \d+ - a brief whose hash does not match/);
assert.match(result.stdout, /succeeded/, 'Failure must expose the incorrectly accepted delivery');
console.log('PASS: the brief-hash negative test kills the missing-validation mutant');
