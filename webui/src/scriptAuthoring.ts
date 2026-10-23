import type { ReferenceCandidate, ScriptReference } from './timelineContracts';
import { insertReference, rebaseReferences } from './scriptReferences.ts';

/** Editor helpers for vac-script-syntax v2. They do not add directives. */
export interface AuthoringInsert {
  text: string;
  selectStart: number;
  selectEnd: number;
  openReference?: boolean;
}

export interface SnippetChoice extends AuthoringInsert {
  key: string;
  label: string;
  prefixes: string[];
}

export const SNIPPETS: SnippetChoice[] = [
  { key: 'reference', label: '@ 素材或标记引用', text: '{@', prefixes: ['@'], selectStart: 2, selectEnd: 2, openReference: true },
  { key: 'subtitle', label: '字幕 · 指定文字与字号', text: '{"字幕文字"}', prefixes: ['"', '字幕'], selectStart: 2, selectEnd: 6 },
  { key: 'black', label: '黑场淡变', text: '{黑屏}', prefixes: ['黑屏', '黑场淡变'], selectStart: 4, selectEnd: 4 },
  { key: 'white', label: '白场淡变', text: '{白屏}', prefixes: ['白屏', '白场淡变'], selectStart: 4, selectEnd: 4 },
  { key: 'dissolve', label: '交叉溶解', text: '{交叉溶解}', prefixes: ['交叉溶解'], selectStart: 6, selectEnd: 6 },
];

export const SUBTITLE_INSERT: AuthoringInsert = { text: '{"字幕文字"}', selectStart: 2, selectEnd: 6 };
export const EFFECT_INSERTS = {
  黑屏: { text: '{黑屏}', selectStart: 4, selectEnd: 4 },
  白屏: { text: '{白屏}', selectStart: 4, selectEnd: 4 },
  溶解: { text: '{交叉溶解}', selectStart: 6, selectEnd: 6 },
} as const;
export const PARAGRAPH_BREAK: AuthoringInsert = { text: '\n\n', selectStart: 2, selectEnd: 2 };

export const CHEATSHEET: { label: string; hint: string; insert: AuthoringInsert }[] = [
  { label: '{@识别名}', hint: '引用素材或标记', insert: { text: '{@}', selectStart: 2, selectEnd: 2, openReference: true } },
  { label: '{"字幕"}', hint: '按原文显示，可再补字号、字体', insert: SUBTITLE_INSERT },
  { label: '{黑屏}', hint: '本段入点黑场淡变', insert: EFFECT_INSERTS.黑屏 },
  { label: '{白屏}', hint: '本段入点白场淡变', insert: EFFECT_INSERTS.白屏 },
  { label: '{交叉溶解}', hint: '与上一段叠化', insert: EFFECT_INSERTS.溶解 },
];

export interface ParamChoice { key: 'size' | 'font'; label: string; text: string }
export const SUBTITLE_PARAMS: ParamChoice[] = [
  { key: 'size', label: '字号:', text: '字号:' },
  { key: 'font', label: '字体:"', text: '字体:"' },
];

export type ParagraphTemplateKind = 'open' | 'result' | 'transition';

export function paragraphTemplate(kind: ParagraphTemplateKind): AuthoringInsert {
  if (kind === 'result') {
    const text = '{"重点说明", 字号:32px}{白屏}在此写结果要求\n\n';
    const at = text.indexOf('在此写结果要求');
    return { text, selectStart: at, selectEnd: at + '在此写结果要求'.length };
  }
  const text = kind === 'transition'
    ? '{交叉溶解}{@}在此写转场要求\n\n'
    : '{黑屏}{@}在此写开场要求\n\n';
  const at = text.indexOf('{@}') + 2;
  return { text, selectStart: at, selectEnd: at, openReference: true };
}

export function filterSnippets(query: string): SnippetChoice[] {
  const q = query.trim().toLocaleLowerCase();
  if (!q) return SNIPPETS;
  return SNIPPETS.filter(snippet =>
    snippet.prefixes.some(prefix => prefix.toLocaleLowerCase().startsWith(q)) ||
    snippet.label.toLocaleLowerCase().startsWith(q));
}

export function filterParams(query: string, omit: ParamChoice['key'][] = []): ParamChoice[] {
  const q = query.replace(/\s+/g, '').toLocaleLowerCase();
  return SUBTITLE_PARAMS.filter(param => {
    if (omit.includes(param.key)) return false;
    if (!q) return true;
    const text = param.text.replace(/\s+/g, '').toLocaleLowerCase();
    return text.startsWith(q) || q.startsWith(text);
  });
}

export function filterFonts(fonts: string[], query: string): string[] {
  const q = query.toLocaleLowerCase();
  return fonts.filter(font => font.toLocaleLowerCase().startsWith(q)).slice(0, 80);
}

export interface CompletionQuery {
  kind: 'snippet' | 'reference' | 'param' | 'font';
  start: number;
  end: number;
  query: string;
  needsComma?: boolean;
  pendingQuote?: boolean;
  omit?: ParamChoice['key'][];
}

export function completionContext(source: string, caret: number, force = false): CompletionQuery | null {
  const region = directiveRegion(source, caret);
  if (!region) return force && nearDirective(source, caret) ? { kind: 'snippet', start: caret, end: caret, query: '' } : null;
  const token = source.slice(region.brace + 1, caret);
  if (token.includes('\n')) return force && nearDirective(source, caret) ? { kind: 'snippet', start: caret, end: caret, query: '' } : null;
  if (token.startsWith('@')) return { kind: 'reference', start: region.brace, end: region.end, query: token.slice(1) };
  if (token.startsWith('"')) {
    const param = subtitleQuery(source, region.brace, caret);
    if (param) return param;
    return force ? { kind: 'param', start: caret, end: caret, query: '', pendingQuote: true } : null;
  }
  return { kind: 'snippet', start: region.brace, end: region.end, query: token };
}

/** Mirrors the Go subtitle scanner closely enough to flag broken blocks. The parser stays authoritative. */
export function subtitleWellFormed(body: string): boolean {
  const parts: string[] = [];
  let start = 0, quoted = false, escaped = false;
  for (let i = 0; i < body.length; i++) {
    const c = body[i];
    if (quoted) {
      if (escaped) escaped = false;
      else if (c === '\\') escaped = true;
      else if (c === '"') quoted = false;
    } else if (c === '"') quoted = true;
    else if (c === ',') { parts.push(body.slice(start, i).trim()); start = i + 1; }
  }
  if (quoted) return false;
  parts.push(body.slice(start).trim());
  if (parts.length > 1 && parts[parts.length - 1] === '') parts.pop();
  if (!parts.length) return false;
  let text: unknown;
  try { text = JSON.parse(parts[0]); } catch { return false; }
  if (typeof text !== 'string' || !text.trim() || [...text].length > 2000) return false;
  const seen = new Set<string>();
  for (const part of parts.slice(1)) {
    const cut = part.indexOf(':');
    if (cut < 0) return false;
    const key = part.slice(0, cut).trim();
    const value = part.slice(cut + 1).trim();
    if (!key || seen.has(key)) return false;
    seen.add(key);
    if (key === '字号') {
      const size = /^([0-9]+(?:\.[0-9]+)?)px$/.exec(value);
      const n = size ? Number(size[1]) : NaN;
      if (!size || n < 1 || n > 512) return false;
    } else if (key === '字体') {
      try {
        const font = JSON.parse(value);
        if (typeof font !== 'string' || !font.trim() || [...font].length > 100 || /[\r\n\0]/.test(font)) return false;
      } catch { return false; }
    } else return false;
  }
  return true;
}

export interface TokenReplacement extends AuthoringInsert { label: string }

export function invalidReplacements(tokenText: string): TokenReplacement[] {
  const closed = tokenText.startsWith('{') && tokenText.endsWith('}');
  const inner = (tokenText.startsWith('{') ? tokenText.slice(1, closed ? -1 : undefined) : tokenText).trim();
  if (inner.startsWith('@')) {
    const name = inner.slice(1).trim();
    const text = `{@${name}}`;
    const caret = 2 + name.length;
    return [{ label: '补全引用', text, selectStart: caret, selectEnd: caret, openReference: true }];
  }
  if (inner.startsWith('"') || inner.includes('"')) {
    return [{ label: '换成字幕模板', text: '{"字幕文字", 字号:32px}', selectStart: 2, selectEnd: 6 }];
  }
  const effects = ['黑屏', '白屏', '交叉溶解'] as const;
  const matches = effects.filter(name => !inner || name.startsWith(inner) || inner.startsWith(name));
  return (matches.length ? matches : effects).map(name => ({
    label: name, text: `{${name}}`, selectStart: name.length + 2, selectEnd: name.length + 2,
  }));
}

export function applyAuthoringInsert(body: string, references: ScriptReference[], start: number, end: number, insert: AuthoringInsert, candidate?: ReferenceCandidate) {
  if (candidate && insert.text.includes('{@}')) {
    const drafted = body.slice(0, start) + insert.text + body.slice(end);
    const draftedRefs = rebaseReferences(body, drafted, references);
    const at = start + insert.text.indexOf('{@}');
    const filled = insertReference(drafted, draftedRefs, at, at + 3, candidate);
    return { body: filled.body, references: filled.references, selectStart: filled.caret, selectEnd: filled.caret };
  }
  const next = body.slice(0, start) + insert.text + body.slice(end);
  return {
    body: next,
    references: rebaseReferences(body, next, references),
    selectStart: start + insert.selectStart,
    selectEnd: start + insert.selectEnd,
  };
}

function directiveRegion(source: string, caret: number): { brace: number; end: number } | null {
  const prefix = source.slice(0, caret);
  let brace = -1;
  for (let i = prefix.length - 1; i >= 0; i--) {
    if (prefix[i] !== '{' || escaped(prefix, i)) continue;
    brace = i;
    break;
  }
  if (brace < 0 || unquotedIndex(prefix.slice(brace + 1), '}') >= 0) return null;
  const close = unquotedBoundary(source.slice(caret));
  return { brace, end: close >= 0 ? caret + close + 1 : caret };
}

function nearDirective(source: string, caret: number): boolean {
  const lineStart = source.lastIndexOf('\n', Math.max(0, caret - 1)) + 1;
  const lineEnd = source.indexOf('\n', caret);
  return /[{}]/.test(source.slice(lineStart, lineEnd < 0 ? source.length : lineEnd));
}

function escaped(source: string, index: number): boolean {
  let slashes = 0;
  for (let i = index - 1; i >= 0 && source[i] === '\\'; i--) slashes++;
  return slashes % 2 === 1;
}

function unquotedIndex(text: string, target: string): number {
  let quoted = false, slash = false;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (slash) slash = false;
      else if (c === '\\') slash = true;
      else if (c === '"') quoted = false;
      continue;
    }
    if (slash) { slash = false; continue; }
    if (c === '\\') slash = true;
    else if (c === '"') quoted = true;
    else if (c === target) return i;
  }
  return -1;
}

function unquotedBoundary(tail: string): number {
  let quoted = false, slash = false;
  for (let i = 0; i < tail.length; i++) {
    const c = tail[i];
    if (quoted) {
      if (slash) slash = false;
      else if (c === '\\') slash = true;
      else if (c === '"') quoted = false;
      continue;
    }
    if (slash) { slash = false; continue; }
    if (c === '\\') slash = true;
    else if (c === '"') quoted = true;
    else if (c === '{' || c === '\n') return -1;
    else if (c === '}') return i;
  }
  return -1;
}

function finishedParam(fragment: string): boolean {
  return /^字号\s*:\s*(?:[0-9]+(?:\.[0-9]+)?)px$/.test(fragment) || /^字体\s*:\s*"(?:[^"\\]|\\.)*"$/.test(fragment);
}

function subtitleQuery(source: string, brace: number, caret: number): CompletionQuery | null {
  const token = source.slice(brace + 1, caret);
  const quoted = scanQuote(token);
  if (!quoted.closed) return null;
  const rest = token.slice(quoted.end);
  const font = /字体\s*:\s*"([^"]*)$/.exec(rest);
  if (font) {
    const query = font[1];
    const start = brace + 1 + quoted.end + font.index + font[0].length - query.length;
    return { kind: 'font', start, end: caret, query };
  }
  const comma = rest.lastIndexOf(',');
  const raw = comma >= 0 ? rest.slice(comma + 1) : rest;
  const lead = raw.length - raw.trimStart().length;
  const frag = raw.trim();
  const head = comma >= 0 ? rest.slice(0, comma) : '';
  const omit: ParamChoice['key'][] = [];
  if (/字号\s*:/.test(comma >= 0 && finishedParam(frag) ? rest : head)) omit.push('size');
  if (/字体\s*:/.test(comma >= 0 && finishedParam(frag) ? rest : head)) omit.push('font');
  if (/^字号\s*:\s*\d/.test(frag) && !finishedParam(frag)) return null;
  if (finishedParam(frag)) return { kind: 'param', start: caret, end: caret, query: '', needsComma: !/,\s*$/.test(rest), omit };
  const fragStart = brace + 1 + quoted.end + (comma >= 0 ? comma + 1 : 0) + lead;
  return {
    kind: 'param',
    start: frag ? fragStart : caret,
    end: caret,
    query: frag,
    needsComma: comma < 0,
    omit,
  };
}

function scanQuote(text: string): { end: number; closed: boolean } {
  let slash = false;
  for (let i = 1; i < text.length; i++) {
    const c = text[i];
    if (slash) { slash = false; continue; }
    if (c === '\\') slash = true;
    else if (c === '"') return { end: i + 1, closed: true };
  }
  return { end: text.length, closed: false };
}
