/**
 * 硬性样式检查。规则见 docs/ui-style-spec.md。
 * 扫描 webui/src：渐变、背景模糊、发光阴影、色板以外的颜色、过大圆角。
 */
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const webuiRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const srcRoot = join(webuiRoot, 'src');
const contentFiles = new Set([
  'assEditorModel.ts',
  'components/AssStyleEditor.tsx'
]);

const errors = [];

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    const stat = statSync(path);
    if (stat.isDirectory()) walk(path, out);
    else if (/\.(css|tsx|ts)$/.test(name)) out.push(path);
  }
  return out;
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

function visible(text, mask) {
  let out = '';
  for (let i = 0; i < text.length; i += 1) out += mask[i] ? (text[i] === '\n' ? '\n' : ' ') : text[i];
  return out;
}

function normHex(raw) {
  let h = raw.slice(1).toUpperCase();
  if (h.length === 3 || h.length === 4) h = [...h].map((ch) => ch + ch).join('');
  if (h.length === 8 && h.endsWith('FF')) h = h.slice(0, 6);
  return `#${h}`;
}

function loadTokens() {
  const css = readFileSync(join(srcRoot, 'styles.css'), 'utf8');
  const theme = readFileSync(join(srcRoot, 'theme.ts'), 'utf8');
  const tokens = new Set();
  const root = css.match(/:root\s*\{([\s\S]*?)\n\}/);
  const rootBody = root ? root[1] : '';
  for (const match of rootBody.matchAll(/#[0-9a-fA-F]{3,8}/g)) tokens.add(normHex(match[0]));
  for (const match of theme.matchAll(/export const [A-Z0-9_]+ = '(#[0-9a-fA-F]{3,8})'/g)) tokens.add(normHex(match[1]));
  if (tokens.size < 7) {
    errors.push('styles.css / theme.ts 没有声明完整色板，无法作为检查基准');
  }
  return tokens;
}

function report(file, index, text, message) {
  const rel = relative(webuiRoot, file);
  errors.push(`${rel}:${lineOf(text, index)} ${message}`);
}

function checkHex(file, text, tokens) {
  const rel = relative(srcRoot, file);
  if (contentFiles.has(rel)) return;
  const mask = commentMask(text);
  const hex = /#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{4}|[0-9a-fA-F]{3})\b/g;
  for (const match of text.matchAll(hex)) {
    if (mask[match.index]) continue;
    const value = normHex(match[0]);
    if (!tokens.has(value)) report(file, match.index, text, `色板以外的颜色 ${match[0]}`);
  }
}

function checkFunctions(file, text) {
  const mask = commentMask(text);
  const patterns = [
    [/rgba?\s*\(/g, '使用了 rgb/rgba，应改为 token 或 color-mix'],
    [/hsla?\s*\(/g, '使用了 hsl/hsla，应改为 token 或 color-mix'],
    [/(?:repeating-linear|repeating-radial|linear|radial|conic)-gradient\s*\(/g, '禁止渐变'],
    [/backdrop-filter\s*:\s*(?!none\b)[^;]*/g, '禁止 backdrop-filter'],
    [/filter\s*:[^;]*blur\s*\(/g, '禁止模糊'],
    [/strokeColor=\{\{[^}]*\bfrom\s*:/g, '进度条禁止双色渐变']
  ];
  for (const [re, message] of patterns) {
    for (const match of text.matchAll(re)) {
      if (mask[match.index]) continue;
      report(file, match.index, text, message);
    }
  }
}

function checkShadow(file, text) {
  const mask = commentMask(text);
  const patterns = [
    [/(?<![\w-])box-shadow\s*:\s*([^;]+)/g, 1],
    [/(?<![\w-])boxShadow\s*:\s*([^,;\n]+)/g, 1]
  ];
  for (const [re] of patterns) {
    for (const match of text.matchAll(re)) {
      if (mask[match.index]) continue;
      const value = match[1].replace(/!important/gi, '').trim().replace(/['"`]/g, '');
      if (value !== 'none') report(file, match.index, text, `box-shadow 只允许 none，当前为 ${match[1].trim()}`);
    }
  }
  if (relative(srcRoot, file) === 'components/AssStyleEditor.tsx') return;
  const shadow = /(?<![\w-])text-shadow\s*:|(?<![\w-])textShadow\s*:/g;
  for (const match of text.matchAll(shadow)) {
    if (mask[match.index]) continue;
    report(file, match.index, text, '禁止 text-shadow');
  }
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

function checkRadius(file, text) {
  if (!file.endsWith('.css')) {
    const mask = commentMask(text);
    const re = /\bborderRadius(?:LG|SM|XS)?\s*:\s*(\d+)/g;
    for (const match of text.matchAll(re)) {
      if (mask[match.index]) continue;
      const n = Number(match[1]);
      if (n !== 0 && (n < 2 || n > 4)) report(file, match.index, text, `圆角 ${n}px 超出 2–4px`);
    }
    return;
  }
  const mask = commentMask(text);
  const re = /border-radius\s*:\s*([^;]+)/g;
  for (const match of text.matchAll(re)) {
    if (mask[match.index]) continue;
    const selector = selectorAt(text, match.index);
    for (const part of match[1].split(/\s+/)) {
      const value = part.replace(/!important/gi, '').trim();
      if (!value || value.startsWith('var(') || value === '0' || value === '0px') continue;
      if (/^(2|3|4)px$/.test(value)) continue;
      if (value === '50%' && /dot/.test(selector)) continue;
      if ((value === '9px' || value === '12px') && selector.includes('seg-nav')) continue;
      report(file, match.index, text, `圆角 ${value} 超出规范（选择器 ${selector.trim().slice(0, 80)}）`);
    }
  }
}

const tokens = loadTokens();
for (const file of walk(srcRoot)) {
  const text = readFileSync(file, 'utf8');
  checkHex(file, text, tokens);
  checkFunctions(file, text);
  checkShadow(file, text);
  checkRadius(file, text);
}

if (errors.length) {
  console.error(`lint:style 未通过（${errors.length}）`);
  for (const error of errors) console.error(`  ${error}`);
  process.exit(1);
}
console.log(`lint:style 通过（${tokens.size} 个色板色值）`);
