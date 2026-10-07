#!/usr/bin/env node
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { readEdl } from './edl.mjs';
import { buildDelivery } from './pipeline.mjs';
import { buildPreviewInputFromPackage, renderPreview } from './preview.mjs';
import { buildVariantInputFromPackage, buildDeliveryVariant } from './delivery-variant.mjs';

const usage = `Usage:
  node src/cli.mjs validate --edl <edl.json>
  node src/cli.mjs build --edl <edl.json> --output <new-dir> --adapters <module.mjs>
  node src/cli.mjs render-preview --package <exported-dir> --output <new-dir> [--preset mp4-720p-v1]
  node src/cli.mjs build-ass --package <exported-dir> --output <new-dir>

The build command requires an adapter module exporting createAdapters().`;

function parse(argv) {
  const [command, ...rest] = argv;
  if (!['validate', 'build', 'render-preview', 'build-ass'].includes(command)) throw new Error(usage);
  const options = {};
  for (let i = 0; i < rest.length; i += 2) {
    const key = rest[i];
    if (!['--edl', '--output', '--adapters', '--package', '--preset'].includes(key) || !rest[i + 1] || options[key]) throw new Error(usage);
    options[key] = rest[i + 1];
  }
  if (command === 'validate' && !options['--edl']) throw new Error(usage);
  if (command === 'build' && (!options['--edl'] || !options['--output'] || !options['--adapters'])) throw new Error(usage);
  if (['render-preview', 'build-ass'].includes(command) && (!options['--package'] || !options['--output'])) throw new Error(usage);
  return { command, options };
}

export async function main(argv) {
  const { command, options } = parse(argv);
  if (command === 'build-ass') {
    const packageDir = resolve(options['--package']), outputDir = resolve(options['--output']);
    const { input } = await buildVariantInputFromPackage({ packageDir });
    const manifest = await buildDeliveryVariant({ input, packageDir, outputDir });
    process.stdout.write(`ASS package created: ${outputDir}\n${manifest.artifacts.length} artifacts declared.\n`);
    return;
  }
  if (command === 'render-preview') {
    const packageDir = resolve(options['--package']);
    const outputDir = resolve(options['--output']);
    const presetId = options['--preset'] || 'mp4-720p-v1';

    const { input } = await buildPreviewInputFromPackage({ packageDir, presetId });
    const manifest = await renderPreview({
      input,
      packageDir,
      outputDir,
      onProgress: (evt) => {
        process.stdout.write(`[preview:${evt.phase}] ${Math.round(evt.progress * 100)}% - ${evt.message}\n`);
      }
    });

    process.stdout.write(`Preview rendered: ${resolve(outputDir, 'final.mp4')}\n`);
    process.stdout.write(`${manifest.artifacts.length} artifacts declared in ${resolve(outputDir, 'preview-manifest.json')}\n`);
    return;
  }
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
