export interface ScriptSubtitle { text: string; size_px?: number; font?: string }
export interface ScriptParagraph { index: number; source: string; instruction: string; video?: string; subtitle?: ScriptSubtitle; effect?: string }
export interface ScriptDocument { version: number; paragraphs: ScriptParagraph[]; diagnostics: { paragraph: number; column: number; message: string }[] }
export interface ScriptMedia { id: string; asset_id: string; name: string; in: number; out: number }
export interface ScriptSegment { index: number; narration: string; media_id: string; start: number; end: number; source_urls?: string[] }
export interface ScriptSource { url: string; title: string; excerpt?: string }
export interface ScriptRecord { script_id: string; asset_id: string; status: string; visible_set_sha: string; title?: string; body?: string; stale_reason?: string }
export interface ScriptWorkbenchView {
  script: ScriptRecord; version: number; document: ScriptDocument; segments: ScriptSegment[];
  sources: ScriptSource[]; questions: string[]; tts_enabled: boolean; subtitles_enabled: boolean;
  allow_web_research: boolean; rejected: boolean; profile_id?: string; profile_revision?: number;
  delivery?: ScriptDelivery;
}
export interface ScriptSave {
  expected_version: number; visible_set_sha: string; title: string; body: string; segments?: ScriptSegment[];
  tts_enabled: boolean; subtitles_enabled: boolean; allow_web_research: boolean;
}
export interface ScriptRevision { version: number; action: string; actor: string; snapshot: string; created_at: string }
export interface ScriptDelivery { version: number; visible_set_sha: string; created_at: string }

/** Lightweight lexical highlighting; the Go parser remains authoritative. */
export function scriptTokens(source: string): { text: string; kind: 'text' | 'directive' | 'invalid' }[] {
  const out: ReturnType<typeof scriptTokens> = [];
  let ordinary = 0;
  for (let i = 0; i < source.length; i++) {
    if (source[i] === '\\' && '{}\\'.includes(source[i + 1] ?? ' ')) { i++; continue; }
    if (source[i] !== '{') continue;
    if (i > ordinary) out.push({ text: source.slice(ordinary, i), kind: 'text' });
    let end = i + 1, quoted = false, escaped = false, nested = false;
    for (; end < source.length; end++) {
      const c = source[end];
      if (quoted) { if (escaped) escaped = false; else if (c === '\\') escaped = true; else if (c === '"') quoted = false; }
      else if (c === '"') quoted = true;
      else if (c === '{') nested = true;
      else if (c === '}') break;
    }
    const body = source.slice(i + 1, end).trim();
    const known = body.startsWith('@') || body.startsWith('"') || ['黑屏', '白屏', '交叉溶解'].includes(body);
    const stop = Math.min(end + 1, source.length);
    out.push({ text: source.slice(i, stop), kind: end < source.length && !nested && known ? 'directive' : 'invalid' });
    i = stop - 1; ordinary = stop;
  }
  if (ordinary < source.length) out.push({ text: source.slice(ordinary), kind: 'text' });
  return out;
}

export function initialScriptSegment(p: ScriptParagraph, media: ScriptMedia[]): ScriptSegment {
  const matches = p.video ? media.filter(m => m.name === p.video) : [];
  const m = matches.length === 1 ? matches[0] : undefined;
  return { index: p.index, narration: '', media_id: m?.id ?? '', start: m?.in ?? 0, end: m ? Math.min(m.out, m.in + 8) : 0 };
}
