import { skillApi } from './skillApi.ts';
import { apiURL } from './runtimeUrl.ts';
import type { SkillEditRequest, SkillWorkbench } from './skillContracts';

export type SkillSelection = { segment_index: number } | { at_seconds: number }
  | { timeline_start: number; timeline_end: number };

/** Bind each edit to the displayed current film; stale preview cannot authorize edits. */
export function createSkillEdit(view: SkillWorkbench, prompt: string, selection: SkillSelection,
  idempotencyKey: string): SkillEditRequest {
  const preview = [...view.previews].reverse().find(item => item.is_current && item.available && item.exportable);
  if (!preview || !view.run.plan) throw new Error('请先选择当前定稿成片；历史预览不能直接发起新修改');
  return { expected_version: view.run.version, plan_version: view.run.plan.version,
    artifact_id: preview.artifact.id, artifact_sha256: preview.artifact.sha256,
    prompt, ...selection, idempotency_key: idempotencyKey };
}

/** Use with the existing VAC player; historical preview remains visibly labelled. */
export function skillPreviewSource(run: string, preview: SkillWorkbench['previews'][number]) {
  if (!preview.available) return null;
  return { src: preview.preview_url ? apiURL(preview.preview_url) : preview.checkpoint_id
    ? skillApi.checkpointPreviewURL(run, preview.checkpoint_id, preview.artifact.id)
    : skillApi.artifactURL(run, preview.artifact.id),
  historical: preview.historical, exportable: preview.exportable, sha256: preview.artifact.sha256 };
}
