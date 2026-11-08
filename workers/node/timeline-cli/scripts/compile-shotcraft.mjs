import {readFile, writeFile} from 'node:fs/promises';
import {dirname, extname, resolve, basename} from 'node:path';
import {fileURLToPath} from 'node:url';
import {build} from 'esbuild';
import {checkDraw} from '../src/draw-runtime.mjs';

const catalogRoot = fileURLToPath(new URL('../../../../control-plane/internal/shotcraft/', import.meta.url));
const upstream = resolve(catalogRoot, 'upstream');
const catalog = JSON.parse(await readFile(resolve(catalogRoot, 'catalog.json'), 'utf8'));
const templates = {};
const projectCards = Array.from({length: 10}, (_, i) => 'card' + (i + 1));
const imagesByStyle = {
  'crash-zoom-punch': ['projects-full', 'card4-hires'],
  'deck-deal-flyin': ['projects-empty', ...projectCards],
  multiplane: ['projects-full', 'card4-hires', ...projectCards.filter(id => id !== 'card4')],
  'document-typewriter-reveal': ['wbr-full'],
  'list-stack-press': ['papers-full', 'paper1', 'paper2', 'paper3', 'paper4', 'paper5'],
  'outro-group-photo-launch': ['projects-full', 'nav', 'card4-hires', 'card1', 'paper1', 'card7', 'paper3', 'float-search', 'float-stats', 'wbr-full'],
  'page-waterfall-wall': projectCards,
  'row-embed': ['detail-full'],
  'whip-pan': ['card4-hires', ...projectCards],
  'freeze-annotate': projectCards,
  'spotlight-hero-card': ['projects-full', 'card4', 'card4-hires'],
  'type-and-filter': ['projects-empty', ...projectCards],
};

for (const card of catalog) {
  for (const style of card.styles) {
    const key = card.id + '/' + style.id;
    try {
      templates[key] = await compile(style);
    } catch (error) {
      templates[key] = {needs_adapter: error.errors?.[0]?.text ?? error.message.split('\n')[0]};
    }
  }
}

await writeFile(resolve(catalogRoot, 'compiled.json'), JSON.stringify(templates, null, 2) + '\n');
const ready = Object.values(templates).filter(template => template.source).length;
console.log(`${ready} source-checked variants, ${Object.keys(templates).length - ready} require adaptation`);

async function compile(style) {
  const path = resolve(upstream, style.source);
  const entry = await readFile(path, 'utf8');
  const component = basename(path, extname(path));
  const duration = /export const \w+_(?:DURATION|DUR) = (\d+)\s*;/.exec(entry);
  const localDuration = /export const \w+_DURATION = DUR\s*;/.test(entry) && /const DUR = (\d+)\s*;/.exec(entry);
  const fixedDurations = {FlashCut: 10, PillSlotCycle: 200};
  const durationFrames = Number(duration?.[1] ?? localDuration?.[1] ?? fixedDurations[component]);
  if (!durationFrames) throw Error('需要明确原实现的帧长');

  const result = await build({
    absWorkingDir: upstream,
    stdin: {contents: `export { ${component} as default } from ${JSON.stringify('./' + basename(path))};`, resolveDir: dirname(path), loader: 'tsx'},
    bundle: true,
    write: false,
    format: 'esm',
    platform: 'browser',
    jsx: 'automatic',
    target: 'es2022',
    external: ['react', 'remotion'],
    legalComments: 'inline',
    logLevel: 'silent',
    plugins: [{
      name: 'vac-shotcraft-inputs',
      setup(builder) {
        builder.onResolve({filter: /^@remotion\/motion-blur$/}, () => ({path: 'vac-camera-blur', namespace: 'vac'}));
        builder.onLoad({filter: /^vac-camera-blur$/, namespace: 'vac'}, () => ({
          loader: 'jsx',
          contents: `
import React from "react";
import {AbsoluteFill, Freeze, useCurrentFrame} from "remotion";
export function CameraMotionBlur({children, shutterAngle = 180, samples = 8}) {
  const frame = useCurrentFrame();
  const span = shutterAngle / 360;
  return <AbsoluteFill style={{isolation: "isolate"}}>
    {Array.from({length: samples}, (_, index) => {
      const sample = Math.max(0, frame - span * (index + 0.5) / samples);
      return <AbsoluteFill key={index} style={{opacity: 1 / samples, mixBlendMode: "plus-lighter"}}>
        <Freeze frame={sample}>{children}</Freeze>
      </AbsoluteFill>;
    })}
  </AbsoluteFill>;
}`
        }));
        builder.onLoad({filter: /[\\/]_fixtures[\\/]Brand\.tsx$/}, async ({path}) => ({
          loader: 'tsx',
          contents: (await readFile(path, 'utf8'))
            .replace("name: 'video-shotcraft'", "name: 'Your product'")
            .replace("short: 'Shotcraft'", "short: 'Product'")
            .replace('M16 16h96v26H42v44h70v26H16V16Z', 'M16 16h96v96H16z M32 32v64h64V32z')
            .replace('M92 48h28L84 84H56l36-36Z', 'M82 48l18 18-18 18-18-18z')
        }));
      }
    }]
  });
  const bundled = result.outputFiles[0].text;
  if (/\b(?:src|href|poster):\s*["'](?:https?:)?\/\//.test(bundled)) throw Error('需要替换外链素材');
  const {code, needsImages} = bindProjectImages(bundled);
  // esbuild emits an export alias, while VAC accepts an explicit default.
  const source = '// Adapted by VAC from Vincentwei1021/video-shotcraft (Apache-2.0).\n' +
    code.replace(/export\s*\{\s*(\w+) as default\s*\};\s*$/, 'export default $1;\n');
  await checkDraw({composition: {engine: 'remotion', width: 1920, height: 1080, fps: {num: 30, den: 1}, duration_frames: durationFrames, source}, deterministic: true});
  const imageAliases = imagesByStyle[style.id];
  if (needsImages !== Boolean(imageAliases?.length)) throw Error('需要核对模板的项目图片别名');
  return {source, duration_frames: durationFrames, ...(needsImages ? {image_aliases: imageAliases} : {})};
}

// Some upstream resources are selected dynamically and some are resolved at
// module initialization. Instantiate the bundled module inside the component
// so both use the same project-owned image bindings.
function bindProjectImages(bundled) {
  const imports = [];
  const aliases = [];
  const body = bundled.replace(/^import (.+) from "(react(?:\/jsx-runtime)?|remotion)";$/gm, (_, names, from) => {
    const bindings = names.replace(/\{([^}]+)\}/, (_, specifiers) => {
      const kept = specifiers.split(',').map(name => name.trim()).filter(name => {
        const alias = /^staticFile(?: as (\w+))?$/.exec(name);
        if (from !== 'remotion' || !alias) return true;
        aliases.push(alias[1] ?? 'staticFile');
        return false;
      });
      return kept.length ? '{ ' + kept.join(', ') + ' }' : '';
    }).replace(/,\s*$/, '');
    if (bindings) imports.push(`import ${bindings} from "${from}";`);
    return '';
  });
  if (!aliases.length) return {code: bundled, needsImages: false};
  const exported = /export\s*\{\s*(\w+) as default\s*\};\s*$/.exec(body);
  if (!exported) throw Error('找不到上游镜头组件出口');
  const code = `${imports.join('\n')}
import {useMemo as vacShotMemo, createElement as vacShotElement} from "react";
export default function VacShot({images: bindings = {}}) {
  const Scene = vacShotMemo(() => {
    const vacShotImage = (path) => {
      const name = path.split('/').at(-1).replace(/\.[^.]+$/, '');
      const alias = name.replace(/[^a-zA-Z0-9_-]/g, '-');
      const image = bindings[alias];
      if (!image?.src) throw new Error('Image alias ' + alias + ' is not bound to an imported resource');
      return image.src;
    };
    ${[...new Set(aliases)].map(name => 'const ' + name + ' = vacShotImage;').join('\n')}
    ${body.slice(0, exported.index)}
    return ${exported[1]};
  }, [bindings]);
  return vacShotElement(Scene);
}`;
  return {code, needsImages: true};
}
