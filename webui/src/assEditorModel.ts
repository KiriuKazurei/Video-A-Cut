import {
  canonicalJSON,
  defaultStyle,
  effectiveCue,
  validateDocument
} from '../../contracts/delivery-variant/v1/style.mjs';
import type { PreviewList, PreviewStartRequest, PreviewView } from './previewContracts';
import type {
  SaveStyleRequest,
  SourceCue,
  StyleDocument,
  SubtitleCueOverride,
  SubtitleStyle,
  SubtitleStyleDraft,
  VariantList,
  VariantStartRequest,
  VariantView
} from './deliveryVariantContracts';

export { canonicalJSON, defaultStyle, effectiveCue, validateDocument };

export type StyleField = keyof SubtitleStyle;
export type EditorScope = 'global' | 'cues';

const ACTIVE = new Set(['queued', 'claimed', 'running']);

export interface LocalDraft {
  document: StyleDocument;
  undo: StyleDocument[];
  baseDraftVersion: number;
}

const localDrafts = new Map<string, LocalDraft>();
const LOCAL_DRAFT_PREFIX = 'vac.ass-style-draft.v1:';

function browserStorage(): Storage | undefined {
  try { return globalThis.localStorage; } catch { return undefined; }
}

export function styleCacheKey(asset: string, run: string, revision: string, sourceSha: string): string {
  return JSON.stringify([asset, run, revision, sourceSha]);
}

export function clearLocalDrafts(): void {
  localDrafts.clear();
  const storage = browserStorage();
  if (!storage) return;
  try {
    for (const key of Object.keys(storage)) if (key.startsWith(LOCAL_DRAFT_PREFIX)) storage.removeItem(key);
  } catch { /* Storage may be unavailable; the in-memory cache still works. */ }
}

export function rememberLocalDraft(key: string, draft: LocalDraft): void {
  localDrafts.set(key, structuredClone(draft));
  try {
    // Keep only the latest document on disk; undo remains available within this editing session.
    browserStorage()?.setItem(LOCAL_DRAFT_PREFIX + key, JSON.stringify({ schema_version: 1, key, ...draft, undo: [] }));
  } catch { /* A blocked/full browser store must not discard current input. */ }
}

export function recallLocalDraft(key: string): LocalDraft | undefined {
  let hit = localDrafts.get(key);
  if (!hit) {
    try {
      const raw = browserStorage()?.getItem(LOCAL_DRAFT_PREFIX + key);
      if (raw) {
        const saved = JSON.parse(raw);
        if (saved.schema_version === 1 && saved.key === key && Number.isSafeInteger(saved.baseDraftVersion) && saved.baseDraftVersion >= 0
          && saved.document?.style && Array.isArray(saved.document.overrides)
          && saved.document.overrides.every((item: SubtitleCueOverride) => item && Number.isInteger(item.cue_id))) {
          hit = { document: saved.document, baseDraftVersion: saved.baseDraftVersion, undo: [] };
          localDrafts.set(key, hit!);
        }
      }
    } catch { /* Ignore damaged/older cache records and use the server snapshot. */ }
  }
  return hit ? structuredClone(hit) : undefined;
}

/** Unsaved edits for the same asset, run, revision and source SRT win over a later server snapshot. */
export function adoptServerDraft(server: SubtitleStyleDraft, key: string): LocalDraft {
  return recallLocalDraft(key) ?? {
    document: structuredClone(server.document),
    undo: [],
    baseDraftVersion: server.draft_version
  };
}

/** A failed or conflicting save leaves the typed document in place. */
export function noteSaveOutcome(local: LocalDraft, outcome: { ok: true; server: SubtitleStyleDraft } | { ok: false }): LocalDraft {
  if (!outcome.ok) return local;
  return {
    document: structuredClone(outcome.server.document),
    undo: local.undo.map(item => structuredClone(item)),
    baseDraftVersion: outcome.server.draft_version
  };
}

export function commitEdit(undo: StyleDocument[], current: StyleDocument, next: StyleDocument): { undo: StyleDocument[]; current: StyleDocument } {
  if (canonicalJSON(current) === canonicalJSON(next)) return { undo, current };
  return { undo: [...undo, structuredClone(current)].slice(-40), current: next };
}

export function undoEdit(undo: StyleDocument[], current: StyleDocument): { undo: StyleDocument[]; current: StyleDocument } | null {
  const previous = undo[undo.length - 1];
  if (!previous) return null;
  return { undo: undo.slice(0, -1), current: structuredClone(previous) };
}

export function setGlobalField<K extends StyleField>(document: StyleDocument, key: K, value: SubtitleStyle[K]): StyleDocument {
  return { ...document, style: { ...document.style, [key]: value } };
}

function cleanOverride(override: SubtitleCueOverride | null | undefined): SubtitleCueOverride | null {
  if (!override) return null;
  const style = override.style && Object.keys(override.style).length > 0 ? override.style : undefined;
  const next: SubtitleCueOverride = { cue_id: override.cue_id };
  if (override.start_ms != null) next.start_ms = override.start_ms;
  if (override.end_ms != null) next.end_ms = override.end_ms;
  if (style) next.style = style;
  if (next.start_ms == null && next.end_ms == null && !next.style) return null;
  return next;
}

function mapOverrides(document: StyleDocument, cueIds: number[], mapper: (cueId: number, current: SubtitleCueOverride | undefined) => SubtitleCueOverride | null): StyleDocument {
  const wanted = new Set(cueIds);
  const seen = new Set<number>();
  const overrides: SubtitleCueOverride[] = [];
  for (const current of document.overrides) {
    if (!wanted.has(current.cue_id)) {
      overrides.push(current);
      continue;
    }
    seen.add(current.cue_id);
    const next = cleanOverride(mapper(current.cue_id, current));
    if (next) overrides.push(next);
  }
  for (const cueId of cueIds) {
    if (seen.has(cueId)) continue;
    const next = cleanOverride(mapper(cueId, undefined));
    if (next) overrides.push(next);
  }
  return { ...document, overrides };
}

export function setCueStyles(document: StyleDocument, cueIds: number[], patch: Partial<SubtitleStyle>): StyleDocument {
  return mapOverrides(document, cueIds, (cueId, current) => ({
    cue_id: cueId,
    start_ms: current?.start_ms,
    end_ms: current?.end_ms,
    style: { ...current?.style, ...patch }
  }));
}

/** Timing never fans out to a multi-selection; only the addressed cue changes. */
export function setCueTiming(document: StyleDocument, cueId: number, timing: { start_ms?: number; end_ms?: number }): StyleDocument {
  return mapOverrides(document, [cueId], (_id, current) => ({
    cue_id: cueId,
    start_ms: timing.start_ms ?? current?.start_ms,
    end_ms: timing.end_ms ?? current?.end_ms,
    style: current?.style
  }));
}

export function setCueDuration(document: StyleDocument, cues: SourceCue[], cueId: number, durationMs: number): StyleDocument {
  const cue = cues.find(item => item.cue_id === cueId);
  if (!cue) return document;
  const effective = effectiveCue(cue, document);
  return setCueTiming(document, cueId, { end_ms: effective.start_ms + durationMs });
}

export function setCueStart(document: StyleDocument, cues: SourceCue[], cueId: number, startMs: number): StyleDocument {
  const cue = cues.find(item => item.cue_id === cueId);
  if (!cue) return document;
  const current = effectiveCue(cue, document);
  return setCueTiming(document, cueId, { start_ms: startMs, end_ms: startMs + current.end_ms - current.start_ms });
}

export function restoreInheritance(document: StyleDocument, cueIds: number[]): StyleDocument {
  return mapOverrides(document, cueIds, (cueId, current) => {
    if (!current) return null;
    return { cue_id: cueId, start_ms: current.start_ms, end_ms: current.end_ms };
  });
}

export function applyStyleToAll(document: StyleDocument, style: SubtitleStyle): StyleDocument {
  return {
    ...document,
    style: { ...style },
    overrides: document.overrides.flatMap(override => {
      const kept = cleanOverride({ cue_id: override.cue_id, start_ms: override.start_ms, end_ms: override.end_ms });
      return kept ? [kept] : [];
    })
  };
}

export function applyPreset(document: StyleDocument, target: { kind: 'global' } | { kind: 'cues'; cueIds: number[] }, preset: SubtitleStyle): StyleDocument {
  if (target.kind === 'global') return { ...document, style: { ...preset } };
  if (target.cueIds.length === 0) return document;
  return setCueStyles(document, target.cueIds, { ...preset });
}

export function stylePresets(): Array<{ id: string; name: string; style: SubtitleStyle }> {
  return [
    { id: 'bottom-white', name: '白字黑边 · 底部', style: defaultStyle() },
    { id: 'bottom-yellow', name: '黄字黑边 · 底部', style: { ...defaultStyle(), primary_color: '#ffe14a' } },
    { id: 'top-white', name: '白字描边 · 顶部', style: { ...defaultStyle(), alignment: 8, margin_v: 48 } }
  ];
}

export function sharedStyleValue(document: StyleDocument, cues: SourceCue[], cueIds: number[], key: StyleField): SubtitleStyle[StyleField] | undefined {
  const values = cueIds.map(id => {
    const cue = cues.find(item => item.cue_id === id);
    return cue ? effectiveCue(cue, document).style[key] : undefined;
  });
  if (values.length === 0 || values.some(value => value === undefined)) return undefined;
  return values.every(value => value === values[0]) ? values[0] : undefined;
}

export function combinedCueStyle(document: StyleDocument, cues: SourceCue[], cueIds: number[]): SubtitleStyle | undefined {
  const styles = cueIds.map(id => {
    const cue = cues.find(item => item.cue_id === id);
    return cue ? effectiveCue(cue, document).style : undefined;
  });
  if (styles.length === 0 || styles.some(style => !style)) return undefined;
  const fingerprint = canonicalJSON(styles[0]);
  if (!styles.every(style => style && canonicalJSON(style) === fingerprint)) return undefined;
  return { ...styles[0]! };
}

export function formatCueMs(ms: number): string {
  if (!Number.isFinite(ms)) return '—';
  const sign = ms < 0 ? '-' : '';
  const value = Math.abs(Math.trunc(ms));
  const minutes = Math.floor(value / 60000);
  const seconds = Math.floor(value / 1000) % 60;
  const millis = value % 1000;
  return `${sign}${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}.${String(millis).padStart(3, '0')}`;
}

export function alignmentPlacement(alignment: number): { justifyContent: 'flex-start' | 'center' | 'flex-end'; alignItems: 'flex-start' | 'center' | 'flex-end' } {
  const column = (alignment - 1) % 3;
  const row = Math.floor((alignment - 1) / 3);
  return {
    justifyContent: column === 0 ? 'flex-start' : column === 1 ? 'center' : 'flex-end',
    alignItems: row === 0 ? 'flex-end' : row === 1 ? 'center' : 'flex-start'
  };
}

export async function styleSha256(document: StyleDocument): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(canonicalJSON(document)));
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

export function selectAssJob(variants: VariantView[], revisionId: string, styleSha: string, explicitRetry: boolean):
  { kind: 'ready' | 'wait'; variant: VariantView } | { kind: 'start' } {
  const matches = variants.filter(variant => variant.revision_id === revisionId && variant.spec_id === 'ass-basic-v2' && variant.style_sha256 === styleSha && !variant.invalidated);
  const ready = matches.find(variant => variant.status === 'succeeded');
  if (ready && !explicitRetry) return { kind: 'ready', variant: ready };
  const waiting = matches.find(variant => ACTIVE.has(variant.status));
  if (waiting) return { kind: 'wait', variant: waiting };
  return { kind: 'start' };
}

export function selectMp4Job(previews: PreviewView[], revisionId: string, variantId: string, styleSha: string, explicitRetry: boolean):
  { kind: 'ready' | 'wait'; preview: PreviewView } | { kind: 'start' } {
  const matches = previews.filter(preview => preview.revision_id === revisionId && preview.subtitle_variant_id === variantId && preview.style_sha256 === styleSha && !preview.invalidated);
  const ready = matches.find(preview => preview.status === 'succeeded');
  if (ready && !explicitRetry) return { kind: 'ready', preview: ready };
  const waiting = matches.find(preview => ACTIVE.has(preview.status));
  if (waiting) return { kind: 'wait', preview: waiting };
  return { kind: 'start' };
}

export function freezeStartRequest<T extends { request_id: string }>(reuse: boolean, previous: T | undefined, same: (previous: T) => boolean, create: () => T): T {
  if (reuse && previous && same(previous)) return previous;
  return create();
}

export interface ChainProgress {
  phase: 'idle' | 'saving' | 'ass' | 'mp4' | 'done' | 'stopped' | 'failed';
  message: string;
  progress: number;
  variantId?: string;
  previewId?: string;
  styleSha?: string;
  draftVersion?: number;
  variant?: VariantView;
  preview?: PreviewView;
  requests?: { ass?: VariantStartRequest; mp4?: PreviewStartRequest };
}

export const idleChain: ChainProgress = { phase: 'idle', message: '', progress: 0 };

class ChainInterrupt extends Error {
  phase: 'stopped' | 'failed';
  constructor(phase: 'stopped' | 'failed', message: string) {
    super(message);
    this.phase = phase;
  }
}

export interface StyledExportDeps {
  isCurrent: () => boolean;
  revisionId: string;
  draftVersion: number;
  document: StyleDocument;
  through: 'ass' | 'mp4';
  explicitRetry: boolean;
  reuseRequests: boolean;
  previousRequests?: { ass?: VariantStartRequest; mp4?: PreviewStartRequest };
  requestId: () => string;
  wait: (ms: number) => Promise<void>;
  save: (body: SaveStyleRequest) => Promise<SubtitleStyleDraft>;
  onDraftSaved: (draft: SubtitleStyleDraft) => void;
  listVariants: () => Promise<VariantList>;
  startVariant: (body: VariantStartRequest) => Promise<VariantView>;
  readVariant: (id: string) => Promise<VariantView>;
  listPreviews: () => Promise<PreviewList>;
  startPreview: (body: PreviewStartRequest) => Promise<PreviewView>;
  readPreview: (id: string) => Promise<PreviewView>;
  onProgress: (progress: ChainProgress) => void;
}

function publish(deps: StyledExportDeps, progress: ChainProgress): ChainProgress {
  if (deps.isCurrent()) deps.onProgress(progress);
  return progress;
}

async function pollUntil<T extends { status: string; progress: number; message?: string }>(
  deps: StyledExportDeps,
  read: () => Promise<T>,
  report: (item: T) => void
): Promise<T> {
  for (let attempt = 0; attempt < 600; attempt += 1) {
    if (!deps.isCurrent()) throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    const item = await read();
    if (!deps.isCurrent()) throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    report(item);
    if (item.status === 'succeeded') return item;
    if (item.status === 'failed' || item.status === 'cancelled') {
      throw new ChainInterrupt('failed', item.message || (item.status === 'cancelled' ? '任务已取消' : '任务失败'));
    }
    await deps.wait(400);
  }
  throw new ChainInterrupt('failed', '等待任务超时');
}

/** Save, reuse or create the matching ASS, then optionally burn that exact variant into an MP4. */
export async function runStyledExport(deps: StyledExportDeps): Promise<ChainProgress> {
  const requests: { ass?: VariantStartRequest; mp4?: PreviewStartRequest } = {};
  try {
    if (!deps.isCurrent()) throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    publish(deps, { phase: 'saving', message: '正在保存样式草稿', progress: 0 });
    const saved = await deps.save({
      revision_id: deps.revisionId,
      expected_draft_version: deps.draftVersion,
      document: deps.document
    });
    deps.onDraftSaved(saved);
    if (!deps.isCurrent()) throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    if (saved.revision_id !== deps.revisionId) throw new ChainInterrupt('stopped', '导出版本已变化，链式生成已停止');

    const variants = await deps.listVariants();
    if (!deps.isCurrent() || variants.revision_id !== deps.revisionId) {
      throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    }
    const assChoice = selectAssJob(variants.variants, deps.revisionId, saved.style_sha256, deps.explicitRetry);
    let variant: VariantView;
    if (assChoice.kind === 'start') {
      if (variants.can_start_editable !== true) {
        const reason = variants.checks.filter(check => check.status !== 'passed').map(check => check.message || check.code).join('；');
        throw new ChainInterrupt('failed', reason || '当前不能生成可编辑 ASS');
      }
      requests.ass = freezeStartRequest(
        deps.reuseRequests,
        deps.previousRequests?.ass,
        previous => previous.spec_id === 'ass-basic-v2' && previous.revision_id === deps.revisionId && previous.draft_version === saved.draft_version && previous.style_sha256 === saved.style_sha256 && previous.expected_version === variants.expected_version,
        (): VariantStartRequest => ({
          revision_id: deps.revisionId,
          expected_version: variants.expected_version,
          request_id: deps.requestId(),
          spec_id: 'ass-basic-v2',
          draft_version: saved.draft_version,
          style_sha256: saved.style_sha256
        })
      );
      publish(deps, { phase: 'ass', message: '正在创建 ASS', progress: 0, styleSha: saved.style_sha256, draftVersion: saved.draft_version, requests });
      variant = await deps.startVariant(requests.ass);
    } else {
      variant = assChoice.variant;
    }
    if (!deps.isCurrent()) throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    if (variant.status !== 'succeeded') {
      variant = await pollUntil(deps, () => deps.readVariant(variant.variant_id), item => {
        publish(deps, { phase: 'ass', message: item.message || item.status, progress: item.progress, variantId: variant.variant_id, styleSha: saved.style_sha256, draftVersion: saved.draft_version, requests });
      });
    }
    publish(deps, { phase: deps.through === 'ass' ? 'done' : 'ass', message: 'ASS 已完成', progress: 1, variantId: variant.variant_id, variant, styleSha: saved.style_sha256, draftVersion: saved.draft_version, requests });
    if (deps.through === 'ass') {
      return { phase: 'done', message: 'ASS 已完成', progress: 1, variantId: variant.variant_id, variant, styleSha: saved.style_sha256, draftVersion: saved.draft_version, requests };
    }

    const previews = await deps.listPreviews();
    if (!deps.isCurrent() || previews.revision_id !== deps.revisionId) {
      throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    }
    const mp4Choice = selectMp4Job(previews.previews, deps.revisionId, variant.variant_id, saved.style_sha256, deps.explicitRetry);
    let preview: PreviewView;
    if (mp4Choice.kind === 'start') {
      if (!previews.can_start) throw new ChainInterrupt('failed', '当前不能生成 MP4');
      requests.mp4 = freezeStartRequest(
        deps.reuseRequests,
        deps.previousRequests?.mp4,
        previous => previous.revision_id === deps.revisionId && previous.subtitle_variant_id === variant.variant_id && previous.expected_version === previews.expected_version,
        (): PreviewStartRequest => ({
          revision_id: deps.revisionId,
          expected_version: previews.expected_version,
          preset_id: 'mp4-720p-v1',
          request_id: deps.requestId(),
          subtitle_variant_id: variant.variant_id
        })
      );
      publish(deps, { phase: 'mp4', message: '正在创建样式 MP4', progress: 0, variantId: variant.variant_id, variant, styleSha: saved.style_sha256, requests });
      preview = await deps.startPreview(requests.mp4);
    } else {
      preview = mp4Choice.preview;
    }
    if (!deps.isCurrent()) throw new ChainInterrupt('stopped', '已切换资产或导出版本，链式生成已停止');
    if (preview.status !== 'succeeded') {
      preview = await pollUntil(deps, () => deps.readPreview(preview.preview_id), item => {
        publish(deps, { phase: 'mp4', message: item.message || item.status, progress: item.progress, variantId: variant.variant_id, previewId: preview.preview_id, styleSha: saved.style_sha256, requests });
      });
    }
    return publish(deps, { phase: 'done', message: '样式 MP4 已完成', progress: 1, variantId: variant.variant_id, previewId: preview.preview_id, variant, preview, styleSha: saved.style_sha256, draftVersion: saved.draft_version, requests });
  } catch (error) {
    if (error instanceof ChainInterrupt) {
      return publish(deps, { phase: error.phase, message: error.message, progress: 0, requests });
    }
    const message = error instanceof Error ? error.message : '样式生成失败';
    return publish(deps, { phase: 'failed', message, progress: 0, requests });
  }
}
