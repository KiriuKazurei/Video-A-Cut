import { ApiError } from './api.ts';
import { skillQueryKeys, type SkillArtifact, type SkillEditWindow, type SkillFlow, type SkillHistory, type SkillPlan, type VacProject, type ProjectSave } from './skillContracts.ts';

export const FLOW_LABEL: Record<SkillFlow, string> = {
  lightweight: '流程 A · 轻量口播',
  visual_heavy: '流程 B · 画面摘要',
  custom: '自定义组合'
};

export const RUN_STATE_LABEL = {
  queued: '排队',
  running: '运行中',
  waiting_human: '等待人工',
  succeeded: '已完成',
  failed: '失败',
  cancelled: '已取消'
} as const;

export const PAUSE_REASON_LABEL: Record<string, string> = {
  plan_confirmation: '计划待人工确认',
  frame_budget_exceeded: '抽帧预算已用尽',
  tool_failed: '工具失败',
  context_changed: '上下文已变化',
  authorization_revoked: '授权已撤销',
  runtime_restarted: '运行时已重启',
  agent_clarification: 'Agent 需要补充说明'
};

/** Named read-only tools. Not a free-form argument editor. */
export const READONLY_TOOL_ACTIONS = [
  { name: 'media.list' as const, label: '读取冻结素材' },
  { name: 'artifacts.list' as const, label: '读取已登记产物' },
  { name: 'summaries.get' as const, label: '读取内容摘要' }
];

export function isHumanUnlockRequired(error: unknown): boolean {
  return error instanceof ApiError && error.status === 403;
}

export function isVersionConflict(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409;
}

export function humanUnlockMessage(): string {
  return '需要人工会话解锁后才能继续。请使用宿主配置的人工访问凭证；403 不会改走其它接口。';
}

export function versionConflictMessage(): string {
  return '版本已变化，已重新读取最新快照，请核对后再提交。';
}

export function protectedQueryRetry(failureCount: number, error: Error): boolean {
  if (isHumanUnlockRequired(error) || isVersionConflict(error)) return false;
  return failureCount < 2;
}

export function projectDraft(project: VacProject): ProjectSave {
  return {
    name: project.name,
    flow: project.flow,
    expected_version: project.version,
    assets: [...project.assets],
    visible: [...project.visible],
    default_skills: { ...(project.default_skills ?? {}) },
    disabled_skills: [...(project.disabled_skills ?? [])],
    accept_external: project.external_authorized,
    accept_transcription: project.transcription_authorized,
    ...(project.profile_id ? { profile_id: project.profile_id, profile_revision: project.profile_revision } : {})
  };
}

export function sameIds(left: readonly string[], right: readonly string[]): boolean {
  if (left.length !== right.length) return false;
  const a = [...left].sort();
  const b = [...right].sort();
  return a.every((id, index) => id === b[index]);
}

export function consentAfterVisibility(hidden: boolean, current: { external: boolean; transcription: boolean }) {
  if (hidden) return { external: false, transcription: false };
  return current;
}

export function historyMoves(history: SkillHistory | undefined): { undo: boolean; redo: boolean } {
  if (!history) return { undo: false, redo: false };
  const current = history.checkpoints.find((item) => item.id === history.cursor.checkpoint_id);
  return { undo: Boolean(current?.parent_id), redo: history.cursor.redo.length > 0 };
}

export function sameRevisionDelivery(artifacts: readonly SkillArtifact[], planVersion: number | undefined) {
  if (planVersion == null) return { mp4: undefined, premiere: undefined, aligned: false };
  const mp4 = artifacts.find((item) => item.kind === 'mp4' && item.plan_version === planVersion);
  const premiere = artifacts.find((item) => item.kind === 'premiere_package' && item.plan_version === planVersion);
  return { mp4, premiere, aligned: Boolean(mp4 && premiere) };
}

export function splitPlanSegment(plan: SkillPlan, index: number): SkillPlan {
  if (!Number.isInteger(index) || index < 0 || index >= plan.segments.length) throw new Error('请选择要拆开的片段');
  const target = plan.segments[index];
  const mid = (target.start + target.end) / 2;
  if (!(mid > target.start && mid < target.end)) throw new Error('片段时长不足以拆成两段');
  const paragraphs = plan.body.split(/\n\s*\n/);
  if (paragraphs.length !== plan.segments.length) throw new Error('正文空行段落数必须与片段数一致后才能拆段');
  const segments = plan.segments.slice();
  segments.splice(index, 1, { ...target, end: mid }, { ...target, start: mid, voice_consent: false });
  const nextParagraphs = paragraphs.slice();
  nextParagraphs.splice(index, 1, paragraphs[index], paragraphs[index]);
  return { ...plan, body: nextParagraphs.join('\n\n'), segments, finalized_by: undefined, finalized_at: undefined };
}

export function rangeFromSegments(segments: readonly SkillEditWindow[], startIndex: number, endIndex: number) {
  const from = Math.min(startIndex, endIndex);
  const to = Math.max(startIndex, endIndex);
  const ordered = [...segments].sort((a, b) => a.segment_index - b.segment_index);
  const slice = ordered.filter((item) => item.segment_index >= from && item.segment_index <= to);
  if (slice.length === 0 || slice.length !== to - from + 1) throw new Error('区间必须对齐连续片段边界');
  return { timeline_start: slice[0].timeline_start, timeline_end: slice[slice.length - 1].timeline_end };
}

export function runDetailKey(projectId: string, runId: string, part: 'workbench' | 'history' | 'steps' | 'revisions' | 'operations') {
  if (!projectId || !runId) throw new Error('查询键必须同时包含项目与运行');
  return [...skillQueryKeys.run(projectId, runId), part] as const;
}

export function revisionLabel(value: unknown, index: number): string {
  if (!value || typeof value !== 'object') return `修订 ${index + 1}`;
  const row = value as Record<string, unknown>;
  const version = row.version ?? row.run_version ?? row.plan_version;
  return version != null ? `修订版本 ${String(version)}` : `修订 ${index + 1}`;
}

export function newIdempotencyKey(prefix: string): string {
  return `${prefix}-${crypto.randomUUID()}`;
}
