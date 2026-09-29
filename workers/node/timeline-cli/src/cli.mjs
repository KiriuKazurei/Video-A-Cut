#!/usr/bin/env node
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { readEdl } from './edl.mjs';
import { buildDelivery } from './pipeline.mjs';

const usage = `Usage:
  node src/cli.mjs validate --edl <edl.json>
  node src/cli.mjs build --edl <edl.json> --output <new-dir> --adapters <module.mjs>

The build command requires an adapter module exporting createAdapters().`;

function parse(argv) {
  const [command, ...rest] = argv;
  if (!['validate', 'build'].includes(command)) throw new Error(usage);
  const options = {};
  for (let i = 0; i < rest.length; i += 2) {
    const key = rest[i];
    if (!['--edl', '--output', '--adapters'].includes(key) || !rest[i + 1] || options[key]) throw new Error(usage);
    options[key] = rest[i + 1];
  }
  if (!options['--edl'] || (command === 'build' && (!options['--output'] || !options['--adapters']))) {
    throw new Error(usage);
  }
  return { command, options };
}

export async function main(argv) {
  const { command, options } = parse(argv);
  const edl = await readEdl(resolve(options['--edl']));
  if (command === 'validate') {
    process.stdout.write('EDL structure valid; media, frame and sync checks have not run.\n');
    return;
  }
  const adapterURL = pathToFileURL(resolve(options['--adapters'])).href;
  const module = await import(adapterURL);
  if (typeof module.createAdapters !== 'function') throw new Error('adapter module must export createAdapters()');
  const ports = await module.createAdapters();
  const manifest = await buildDelivery({
    edl, edlPath: options['--edl'], outputDir: options['--output'], ports
  });
  process.stdout.write(`Package staged for human import validation: ${resolve(options['--output'])}\n`);
  process.stdout.write(`${manifest.artifacts.length} artifacts declared.\n`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`${error.name || 'Error'}: ${error.message}\n`);
    process.exitCode = 1;
  });
}
