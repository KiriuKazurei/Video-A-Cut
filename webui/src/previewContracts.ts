/** Shared protocol and preview client. */
import type { PreviewStartRequest, PreviewView, PreviewList } from '../../contracts/mp4-preview/v1/contracts';
export type * from '../../contracts/mp4-preview/v1/contracts';

export interface PreviewClient {
  start(runID: string, body: PreviewStartRequest): Promise<PreviewView>;
  list(runID: string): Promise<PreviewList>;
  get(previewID: string): Promise<PreviewView>;
  cancel(previewID: string): Promise<PreviewView>;
}

/** Context and URL builders are provided for integration. */
export const previewQueryKey = (assetID: string, runID: string, revisionID: string) =>
  ['mp4Previews', assetID, runID, revisionID] as const;

export const previewFileURL = (previewID: string, fileKey: string, download = false) =>
  `/api/previews/${encodeURIComponent(previewID)}/files/${encodeURIComponent(fileKey)}${download ? '?download=1' : ''}`;

/** Original SRT burns and each ASS style hash are different products, even on one revision. */
export function previewProductKey(view: { subtitle_variant_id?: string; style_sha256?: string }): string {
  if (!view.subtitle_variant_id && !view.style_sha256) return 'srt';
  return `style:${view.subtitle_variant_id ?? ''}:${view.style_sha256 ?? ''}`;
}

export function previewStyleLabel(view: { subtitle_variant_id?: string; style_sha256?: string }): string {
  if (previewProductKey(view) === 'srt') return '原 SRT';
  return `样式 ${view.style_sha256 ? view.style_sha256.slice(0, 8) : '未标记'}`;
}

/**
 * The delivery panel may auto-play the current original-SRT MP4.
 * A styled MP4 already on screen is left alone, including when the SRT result is newer in the list.
 */
export function shouldAutoSelectPreview(
  candidate: { subtitle_variant_id?: string; style_sha256?: string },
  activeSrc: string | undefined,
  previews: ReadonlyArray<{ preview_id: string; subtitle_variant_id?: string; style_sha256?: string }>
): boolean {
  if (previewProductKey(candidate) !== 'srt') return false;
  if (!activeSrc) return true;
  const active = previews.find(preview => previewFileURL(preview.preview_id, 'mp4') === activeSrc);
  if (!active) return false;
  return previewProductKey(active) === 'srt';
}

export function isCurrentMp4(view: PreviewView, owner: { assetId: string; runId: string; revisionId: string }): boolean {
  return view.status === 'succeeded' && !view.invalidated && view.is_current
    && view.asset_id === owner.assetId && view.run_id === owner.runId && view.revision_id === owner.revisionId;
}

export function formatSeconds(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '00:00:00.000';
  const ms = Math.floor((value % 1) * 1000);
  const total = Math.floor(value);
  const h = Math.floor(total / 3600), m = Math.floor(total / 60) % 60, s = total % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(ms).padStart(3, '0')}`;
}

export function createPreviewSource(view: PreviewView): {
  kind: 'video';
  src: string;
  title: string;
  meta: string;
  note: string;
  workflowContext: { assetId: string; runId: string; revisionId: string };
} {
  const media = view.media;
  const styled = previewProductKey(view) !== 'srt';
  return {
    kind: 'video',
    src: previewFileURL(view.preview_id, 'mp4'),
    title: styled
      ? `样式成片 MP4 (${view.revision_id} · ${previewStyleLabel(view)})`
      : `成片 MP4 预览 (${view.revision_id})`,
    meta: media
      ? `${media.width}×${media.height} · ${media.fps}fps · ${formatSeconds(media.duration_seconds)}`
      : 'MP4 720p',
    note: styled
      ? '按已冻结 ASS 样式烧录的成片。编辑器中的字幕位置只是示意，不是这支成片。'
      : '合成成片 MP4 预览（中文字幕烧录与最终三轨混合）',
    workflowContext: {
      assetId: view.asset_id,
      runId: view.run_id,
      revisionId: view.revision_id
    }
  };
}

async function previewFetch<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { Accept: 'application/json', ...init.headers }
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const error = body as { error?: { code?: string; message?: string } } | null;
    throw new Error(error?.error?.message ?? `请求失败（HTTP ${response.status}）`);
  }
  return body as T;
}

export const previewApi: PreviewClient = {
  start: (runID: string, body: PreviewStartRequest) =>
    previewFetch<PreviewView>(`/api/workflows/${encodeURIComponent(runID)}/previews`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body)
    }),
  list: (runID: string) =>
    previewFetch<PreviewList>(`/api/workflows/${encodeURIComponent(runID)}/previews`),
  get: (previewID: string) =>
    previewFetch<PreviewView>(`/api/previews/${encodeURIComponent(previewID)}`),
  cancel: (previewID: string) =>
    previewFetch<PreviewView>(`/api/previews/${encodeURIComponent(previewID)}/cancel`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({})
    })
};
