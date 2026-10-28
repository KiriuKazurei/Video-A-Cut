export interface ScriptSubtitle { text: string; size_px?: number; font?: string }
export interface ScriptParagraph { index: number; source: string; instruction: string; video?: string; references?: string[]; subtitle?: ScriptSubtitle; effect?: string }
export interface ScriptDocument { version: number; paragraphs: ScriptParagraph[]; diagnostics: { paragraph: number; column: number; message: string }[] }
export interface ScriptMedia { kind?: 'video' | 'audio'; id: string; asset_id: string; name: string; in: number; out: number }
import type { TtsSettings } from './ttsContracts';
import type { ScriptReference } from './timelineContracts';

export interface ScriptVoice { settings: TtsSettings; offset_seconds: number; accept_external: boolean }
export interface ScriptSegment { index: number; narration: string; media_id: string; start: number; end: number; source_urls?: string[]; tts_enabled?: boolean; subtitles_enabled?: boolean; voice?: ScriptVoice }
export interface ScriptSource { url: string; title: string; excerpt?: string }
export interface ScriptRecord { script_id: string; asset_id: string; status: string; visible_set_sha: string; title?: string; body?: string; stale_reason?: string }
export interface ScriptWorkbenchView {
  references?: ScriptReference[];
  script: ScriptRecord; version: number; document: ScriptDocument; segments: ScriptSegment[];
  sources: ScriptSource[]; questions: string[]; tts_enabled: boolean; subtitles_enabled: boolean;
  allow_web_research: boolean; rejected: boolean; profile_id?: string; profile_revision?: number;
  delivery?: ScriptDelivery;
}
export interface ScriptSave {
  references?: ScriptReference[]; sequence_id?: string;
  expected_version: number; visible_set_sha: string; title: string; body: string; segments?: ScriptSegment[];
  tts_enabled: boolean; subtitles_enabled: boolean; allow_web_research: boolean;
}
export interface ScriptRevision { version: number; action: string; actor: string; snapshot: string; created_at: string }
export interface ScriptDelivery { version: number; visible_set_sha: string; created_at: string }
export interface ScriptVoiceSave {
  expected_version: number; visible_set_sha: string; segment_index: number;
  narration: string; settings: TtsSettings; offset_seconds: number; enabled: boolean; accept_external: boolean;
}
export interface ScriptDeliveryInfo {
  basic_quality?: import('./timelineContracts').BasicQuality;
  script_id: string; version: number; visible_set_sha: string; width: number; height: number;
  duration: number; frames: number; sha256: string; tts_enabled: boolean;
  files: { path: string; size_bytes: number; sha256: string }[];
}

/** Preserve manual edits when a paragraph is changed or another paragraph is inserted. */
export function reconcileScriptSegments(before: ScriptDocument | undefined, after: ScriptDocument, segments: ScriptSegment[], media: ScriptMedia[], references: ScriptReference[] = []): ScriptSegment[] {
  const used = new Set<number>();
  return after.paragraphs.map((paragraph, index) => {
    let old = before?.paragraphs.findIndex((p, i) => !used.has(i) && p.source === paragraph.source) ?? -1;
    if (old < 0 && before?.paragraphs.length === after.paragraphs.length && !used.has(index)) old = index;
    if (old < 0 || !segments[old]) return initialScriptSegment(paragraph, media);
    used.add(old);
    const segment = { ...segments[old], index };
    if (!references.length && paragraph.video && media.find(m => m.id === segment.media_id)?.name !== paragraph.video) {
      const bound = initialScriptSegment(paragraph, media);
      segment.media_id = bound.media_id; segment.start = bound.start; segment.end = bound.end;
    }
    return segment;
  });
}

export function scriptTimelineStarts(w: Pick<ScriptWorkbenchView, 'segments' | 'document'>): number[] {
  let cursor = 0;
  return w.segments.map((segment, index) => {
    const duration = Math.max(0, Math.round((segment.end - segment.start) * 30) / 30);
    if (index > 0 && w.document.paragraphs[index]?.effect === '交叉溶解') {
      const previous = w.segments[index - 1];
      const previousDuration = Math.max(0, Math.round((previous.end - previous.start) * 30) / 30);
      cursor -= Math.floor(Math.min(.35, previousDuration / 3, duration / 3) * 30) / 30;
    }
    const start = cursor; cursor += duration; return start;
  });
}

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
