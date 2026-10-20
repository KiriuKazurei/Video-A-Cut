/**
 * 硬性样式检查。规则见 docs/架构与功能详解.md#style。
 * 扫描 webui/src：渐变、背景模糊、发光阴影、色板以外的颜色、过大圆角。
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const webuiRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const srcRoot = join(webuiRoot, 'src');

const NAMED_COLORS = new Set(`
aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet
brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan
darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen
darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey
darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite
forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew
hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue
lightcoral lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon
lightseagreen lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen
linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen
mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose
moccasin navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen
paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple
red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue
slateblue slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet
wheat white whitesmoke yellow yellowgreen
`.trim().split(/\s+/));

/** 圆角豁免必须整段选择器相等，不能用「包含 seg-nav / dot」。 */
const RADIUS_ALLOW = new Map([
  ['12px', new Set(['.app-nav.seg-nav'])],
  ['9px', new Set([
    '.app-nav.seg-nav > .ant-menu-item',
    '.app-nav.seg-nav > .ant-menu-submenu',
    '.app-nav.seg-nav::before'
  ])],
  ['50%', new Set(['.stage-dot', '.stage-next-dot'])]
]);

const CSS_LONGHAND = [
  'border-top-left-radius', 'border-top-right-radius', 'border-bottom-left-radius', 'border-bottom-right-radius',
  'border-start-start-radius', 'border-start-end-radius', 'border-end-start-radius', 'border-end-end-radius',
  'border-radius'
];
const JS_RADIUS = [
  'borderTopLeftRadius', 'borderTopRightRadius', 'borderBottomLeftRadius', 'borderBottomRightRadius',
  'borderStartStartRadius', 'borderStartEndRadius', 'borderEndStartRadius', 'borderEndEndRadius',
  'borderRadiusLG', 'borderRadiusSM', 'borderRadiusXS', 'borderRadius'
];

export function lintText(filename, text, tokens = new Set()) {
  const errors = [];
  const report = (index, message) => {
    errors.push(`${String(filename).replaceAll('\\', '/')}:${lineOf(text, index)} ${message}`);
  };
  const mask = commentMask(text);
  checkHex(text, mask, tokens, filename, report);
  checkColors(text, mask, report);
  checkEffects(text, mask, report);
  checkShadow(text, mask, filename, report);
  checkRadius(filename, text, mask, report);
  return errors;
}

function lineOf(text, index) {
  let line = 1;
  for (let i = 0; i < index; i += 1) if (text.charCodeAt(i) === 10) line += 1;
  return line;
}

function commentMask(text) {
  const mask = new Uint8Array(text.length);
  let i = 0;
  let quote = '';
  while (i < text.length) {
    const c = text[i];
    const n = text[i + 1];
    if (quote) {
      if (c === '\\') { i += 2; continue; }
      if (c === quote) quote = '';
      i += 1;
      continue;
    }
    if (c === '"' || c === "'" || c === '`') { quote = c; i += 1; continue; }
    if (c === '/' && n === '/') {
      while (i < text.length && text[i] !== '\n') { mask[i] = 1; i += 1; }
      continue;
    }
    if (c === '/' && n === '*') {
      mask[i] = 1;
      mask[i + 1] = 1;
      i += 2;
      while (i < text.length && !(text[i] === '*' && text[i + 1] === '/')) { mask[i] = 1; i += 1; }
      if (i < text.length) { mask[i] = 1; mask[i + 1] = 1; i += 2; }
      continue;
    }
    i += 1;
  }
  return mask;
}

function masked(mask, index) {
  return mask[index] === 1;
}

function isContentFile(filename) {
  const norm = String(filename).replaceAll('\\', '/');
  return norm.endsWith('assEditorModel.ts') || norm.endsWith('AssStyleEditor.tsx');
}

function normHex(raw) {
  let h = raw.slice(1).toUpperCase();
  if (h.length === 3 || h.length === 4) h = [...h].map((ch) => ch + ch).join('');
  if (h.length === 8 && h.endsWith('FF')) h = h.slice(0, 6);
  return `#${h}`;
}

function isIdentCont(ch) {
  return !!ch && /[A-Za-z0-9_-]/.test(ch);
}

function readValue(text, start) {
  let i = start;
  while (i < text.length && /\s/.test(text[i])) i += 1;
  const quote = text[i];
  if (quote === '"' || quote === "'" || quote === '`') {
    let end = i + 1;
    while (end < text.length && text[end] !== quote) {
      if (text[end] === '\\') end += 1;
      end += 1;
    }
    return text.slice(i, Math.min(text.length, end + 1));
  }
  let depth = 0;
  const begin = start;
  while (i < text.length) {
    const c = text[i];
    if (c === '(') depth += 1;
    else if (c === ')' && depth > 0) depth -= 1;
    else if (depth === 0 && (c === ';' || c === '}' || c === '\n')) break;
    else if (depth === 0 && c === ',') {
      const rest = text.slice(i + 1, i + 48);
      if (/^\s*[A-Za-z_][\w-]*\s*:/.test(rest)) break;
    }
    i += 1;
  }
  return text.slice(begin, i);
}

function cleanValue(raw) {
  let value = raw.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/!important/gi, '').trim();
  if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")) || (value.startsWith('`') && value.endsWith('`'))) {
    value = value.slice(1, -1).trim();
  }
  return value.replace(/\s+/g, ' ').trim();
}

function checkHex(text, mask, tokens, filename, report) {
  if (isContentFile(filename)) return;
  const consider = (index, hex, next) => {
    if (masked(mask, index) || isIdentCont(next)) return;
    const value = normHex(hex);
    if (!tokens.has(value)) report(index, `色板以外的颜色 ${hex}`);
  };
  for (let i = 0; i < text.length; i += 1) {
    if (text[i] !== '#') continue;
    let j = i + 1;
    while (j < text.length && /[0-9a-fA-F]/.test(text[j])) j += 1;
    const len = j - (i + 1);
    if (len === 3 || len === 4 || len === 6 || len === 8) consider(i, text.slice(i, j), text[j]);
  }
  const encoded = /%23([0-9a-fA-F]{3,8})/g;
  for (const match of text.matchAll(encoded)) {
    const len = match[1].length;
    if (len !== 3 && len !== 4 && len !== 6 && len !== 8) continue;
    consider(match.index, `#${match[1]}`, text[match.index + match[0].length]);
  }
}

function checkColors(text, mask, report) {
  const fn = /(?<![A-Za-z])(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb)\s*\(/gi;
  for (const match of text.matchAll(fn)) {
    if (masked(mask, match.index)) continue;
    report(match.index, `使用了 ${match[0].replace(/\s*\($/, '')}()，应改为 token 或 color-mix`);
  }
  const cssProp = /(?:^|[^-\w])(background-color|border-color|outline-color|caret-color|background|color|fill|stroke)\s*:\s*/gi;
  for (const match of text.matchAll(cssProp)) {
    const valueStart = match.index + match[0].length;
    if (masked(mask, valueStart)) continue;
    const token = cleanValue(readValue(text, valueStart)).split(/\s+/)[0]?.toLowerCase();
    if (token && NAMED_COLORS.has(token)) report(valueStart, `使用了命名色 ${token}`);
  }
  const jsProp = /(?:backgroundColor|borderColor|outlineColor|background|color|fill|stroke)\s*:\s*(['"`])([A-Za-z]+)\1/g;
  for (const match of text.matchAll(jsProp)) {
    if (masked(mask, match.index)) continue;
    const name = match[2].toLowerCase();
    if (NAMED_COLORS.has(name)) report(match.index, `使用了命名色 ${name}`);
  }
}

function checkEffects(text, mask, report) {
  const gradient = /(?:repeating-linear|repeating-radial|linear|radial|conic)-gradient\s*\(/g;
  for (const match of text.matchAll(gradient)) {
    if (!masked(mask, match.index)) report(match.index, '禁止渐变');
  }
  const drop = /(?<![\w-])drop-shadow\s*\(/g;
  for (const match of text.matchAll(drop)) {
    if (!masked(mask, match.index)) report(match.index, '禁止 drop-shadow');
  }
  const blur = /(?<![\w-])blur\s*\(/g;
  for (const match of text.matchAll(blur)) {
    if (!masked(mask, match.index)) report(match.index, '禁止模糊');
  }
  const backdrop = /(?<![\w])(?:-webkit-)?backdrop-filter\s*:|(?<![\w])(?:webkit)?backdropFilter\s*:/gi;
  for (const match of text.matchAll(backdrop)) {
    if (masked(mask, match.index)) continue;
    const raw = cleanValue(readValue(text, match.index + match[0].length));
    if (raw !== 'none') report(match.index, `backdrop-filter 只允许 none，当前为 ${raw || '空'}`);
  }
  const stroke = /strokeColor\s*[:=]\s*\{/g;
  for (const match of text.matchAll(stroke)) {
    if (masked(mask, match.index)) continue;
    const body = text.slice(match.index, match.index + 500);
    const percent = /['"]?\d+(?:\.\d+)?%['"]?\s*:/.test(body);
    const fromTo = /\bfrom\s*:/.test(body) && /\bto\s*:/.test(body);
    if (percent || fromTo) report(match.index, '进度条禁止渐变 strokeColor');
  }
}

function checkShadow(text, mask, filename, report) {
  const box = /(?<![\w-])box-?shadow\s*:/gi;
  for (const match of text.matchAll(box)) {
    if (masked(mask, match.index)) continue;
    const raw = cleanValue(readValue(text, match.index + match[0].length));
    if (raw !== 'none') report(match.index, `box-shadow 只允许 none，当前为 ${raw || '空'}`);
  }
  if (isContentFile(filename)) return;
  const textShadow = /(?<![\w-])text-?shadow\s*:/gi;
  for (const match of text.matchAll(textShadow)) {
    if (masked(mask, match.index)) continue;
    const raw = cleanValue(readValue(text, match.index + match[0].length));
    if (raw !== 'none') report(match.index, `text-shadow 只允许 none，当前为 ${raw || '空'}`);
  }
}

function normSelector(selector) {
  return selector.replace(/\/\*[\s\S]*?\*\//g, ' ').replace(/\s+/g, ' ').trim();
}

function splitSelectors(selector) {
  const parts = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < selector.length; i += 1) {
    const c = selector[i];
    if (c === '(') depth += 1;
    else if (c === ')' && depth > 0) depth -= 1;
    else if (c === ',' && depth === 0) {
      parts.push(selector.slice(start, i));
      start = i + 1;
    }
  }
  parts.push(selector.slice(start));
  return parts.map(normSelector).filter(Boolean);
}

function selectorAt(text, index) {
  let depth = 0;
  let end = index;
  for (let i = index; i >= 0; i -= 1) {
    if (text[i] === '}') depth += 1;
    else if (text[i] === '{') {
      if (depth === 0) { end = i; break; }
      depth -= 1;
    }
  }
  let start = end - 1;
  while (start >= 0 && text[start] !== '{' && text[start] !== '}') start -= 1;
  return text.slice(start + 1, end);
}

function radiusExempt(value, selectorText) {
  const allow = RADIUS_ALLOW.get(value);
  if (!allow || !selectorText) return false;
  const parts = splitSelectors(selectorText);
  return parts.length > 0 && parts.every((part) => allow.has(part));
}

function radiusPieceOk(piece, selectorText) {
  const value = cleanValue(piece);
  if (!value || value.startsWith('var(') || value === '0' || value === '0px') return true;
  if (!/^(?:\d+(?:\.\d+)?)(?:px|rem|em|%)?$/.test(value)) return true;
  if (/^(?:0|2|3|4)(?:px)?$/.test(value)) return true;
  return radiusExempt(value, selectorText);
}

function checkRadius(filename, text, mask, report) {
  const css = String(filename).endsWith('.css');
  if (css) {
    const re = new RegExp(`(?<![\\w-])(${CSS_LONGHAND.join('|')})\\s*:`, 'g');
    for (const match of text.matchAll(re)) {
      if (masked(mask, match.index)) continue;
      const raw = readValue(text, match.index + match[0].length);
      const selector = selectorAt(text, match.index);
      for (const piece of raw.split(/\s+/)) {
        if (!piece || piece.startsWith('/*')) continue;
        if (!radiusPieceOk(piece, selector)) {
          report(match.index, `圆角 ${cleanValue(piece)} 超出规范（选择器 ${normSelector(selector).slice(0, 80)}）`);
        }
      }
    }
    return;
  }
  const re = new RegExp(`(?<![\\w])(${JS_RADIUS.join('|')})\\s*:\\s*([^,;}\\n]+)`, 'g');
  for (const match of text.matchAll(re)) {
    if (masked(mask, match.index)) continue;
    const raw = cleanValue(match[2]);
    if (/^\d+(?:\.\d+)?$/.test(raw)) {
      const n = Number(raw);
      if (n !== 0 && (n < 2 || n > 4)) report(match.index, `圆角 ${n}px 超出 2–4px`);
      continue;
    }
    if (!radiusPieceOk(raw, '')) report(match.index, `圆角 ${raw} 超出规范`);
  }
}

function loadTokens() {
  const css = readFileSync(join(srcRoot, 'styles.css'), 'utf8');
  const theme = readFileSync(join(srcRoot, 'theme.ts'), 'utf8');
  const tokens = new Set();
  const root = css.match(/:root\s*\{([\s\S]*?)\n\}/);
  const rootBody = root ? root[1] : '';
  for (const match of rootBody.matchAll(/#([0-9a-fA-F]{3,8})/g)) {
    const len = match[1].length;
    if (len === 3 || len === 4 || len === 6 || len === 8) tokens.add(normHex(match[0]));
  }
  for (const match of theme.matchAll(/export const [A-Z0-9_]+ = '(#[0-9a-fA-F]{3,8})'/g)) tokens.add(normHex(match[1]));
  return tokens;
}

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.(css|tsx|ts)$/.test(name)) out.push(path);
  }
  return out;
}

function isMain() {
  const arg = process.argv[1];
  if (!arg) return false;
  return import.meta.url === pathToFileURL(resolve(arg)).href;
}

export function lintSources() {
  const tokens = loadTokens();
  const errors = [];
  if (tokens.size < 7) errors.push('styles.css / theme.ts 没有声明完整色板，无法作为检查基准');
  for (const file of walk(srcRoot)) {
    const text = readFileSync(file, 'utf8');
    errors.push(...lintText(relative(webuiRoot, file), text, tokens));
  }
  return { tokens, errors };
}

if (isMain()) {
  const { tokens, errors } = lintSources();
  if (errors.length) {
    console.error(`lint:style 未通过（${errors.length}）`);
    for (const error of errors) console.error(`  ${error}`);
    process.exit(1);
  }
  console.log(`lint:style 通过（${tokens.size} 个色板色值）`);
}
