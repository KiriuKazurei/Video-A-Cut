import { apiURL } from './runtimeUrl.ts';
import type { ReviewView, WorkflowRun } from './types';
import type { PreviewSource } from './components/Monitor';

export interface WorkflowPreviewContext {
  assetId: string;
  runId: string;
  revisionId: string;
}

export type PreviewTarget = { kind: 'scene' | 'line'; id: string };

/** Only compare versions within the resolved asset/run, including the last seen version. */
export function latestWorkflowRun(assetId: string, runId: string | undefined, candidates: (WorkflowRun | undefined)[]) {
  return candidates.filter((run): run is WorkflowRun => Boolean(run && run.asset_id === assetId && run.run_id === runId))
    .reduce<WorkflowRun | undefined>((latest, run) => !latest || run.version > latest.version ? run : latest, undefined);
}

export function workflowPreviewContext(run?: WorkflowRun): WorkflowPreviewContext | null {
  return run?.current_revision_id ? { assetId: run.asset_id, runId: run.run_id, revisionId: run.current_revision_id } : null;
}

export function samePreviewContext(a?: WorkflowPreviewContext | null, b?: WorkflowPreviewContext | null): boolean {
  return Boolean(a && b && a.assetId === b.assetId && a.runId === b.runId && a.revisionId === b.revisionId);
}

export function reviewMatchesCurrent(review: ReviewView | undefined, current: WorkflowRun | undefined): review is ReviewView {
  return Boolean(review && current && review.run.asset_id === current.asset_id && review.run.run_id === current.run_id
    && review.run.version === current.version && review.run.current_revision_id === current.current_revision_id
    && review.revision.revision_id === current.current_revision_id);
}

export function createWorkflowPreview(review: ReviewView | undefined, target: PreviewTarget, evidenceKey?: string): PreviewSource {
  if (!reviewMatchesCurrent(review, review?.run)) return null;
  const line = target.kind === 'line' ? review.narration.find(item => item.id === target.id) : undefined;
  const sceneId = target.kind === 'scene' ? target.id : line?.source_scene_id;
  const scene = review.scenes.find(item => item.scene_id === sceneId);
  if ((target.kind === 'line' && !line) || (target.kind === 'scene' && !scene)) return null;
  const file = review.evidence_files.find(item => item.scene_id === sceneId && (!evidenceKey || item.key === evidenceKey));
  if (!file) return null;
  const owner = workflowPreviewContext(review.run)!;
  return {
    kind: 'image',
    src: apiURL(`/api/workflows/${encodeURIComponent(owner.runId)}/evidence/${encodeURIComponent(file.key)}?revision_id=${encodeURIComponent(owner.revisionId)}`),
    workflowContext: owner, evidenceKey: file.key, evidenceSha: file.sha256, target,
    title: line ? `解说 ${line.id}` : scene?.label || sceneId || '场景',
    meta: line ? `${line.start ?? '?'}s – ${line.end ?? '?'}s · 来源 ${scene?.label || sceneId || '未提供'}`
      : `源时间 ${file.timestamp} 秒 · ${scene?.decision || '待确认'}`,
    caption: line?.text,
    note: line ? '字幕叠加仅为审查示意，不代表导出成片的字幕样式' : '识别取样证据帧（绑定当前审查修订，非成片）'
  };
}

/** Never rewrite an old revision URL; rebuild metadata only inside the same context. */
export function displayWorkflowPreview(preview: PreviewSource, owner: WorkflowPreviewContext | null, review?: ReviewView): PreviewSource {
  if (!preview?.target || !samePreviewContext(preview.workflowContext, owner) || !review
    || !samePreviewContext(workflowPreviewContext(review.run), owner)) return null;
  const refreshed = createWorkflowPreview(review, preview.target, preview.evidenceKey);
  return refreshed?.evidenceSha === preview.evidenceSha ? refreshed : null;
}
