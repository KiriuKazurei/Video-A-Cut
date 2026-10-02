import type { Asset, Task } from './types';

export type IngestState = 'queued' | 'processing' | 'awaiting_review' | 'ready' | 'failed' | 'cancelled';
export type IngestStage = 'probe' | 'segment' | 'segment_review' | 'prepare' | 'ready';

export interface IngestPolicy {
  policy_id: string;
  max_source_bytes: number;
  max_source_duration_us: number;
  max_candidates: number;
  max_selected: number;
  max_output_us: number;
  max_frames_per_segment: number;
}

export interface IngestRoots {
  configured: boolean;
  roots: { root_id: string; name: string }[];
  policy: IngestPolicy;
  ingesters: { agent_id?: string; online?: boolean }[];
}

export interface RecordingSource {
  source_id: string;
  asset_id: string;
  root_id: string;
  relative_path: string;
  source_version: string;
  size_bytes: number;
  has_snapshot: boolean;
  sha256?: string;
  created_at: string;
}

export interface IngestRun {
  run_id: string;
  asset_id: string;
  source_id: string;
  state: IngestState;
  stage: IngestStage;
  version: number;
  analysis_revision: number;
  selection_revision: number;
  current_task_id?: string;
  error_code?: string;
  error_message?: string;
  profile_id?: string;
  profile_revision?: number;
  created_at: string;
  updated_at: string;
}

export interface ProbeStream {
  index: number;
  type: string;
  codec: string;
  language: string;
  title: string;
  width: number;
  height: number;
  avg_frame_rate: string;
  frame_rate_mode: string;
  sample_rate: number;
  channels: number;
  disposition: Record<string, number>;
}

export interface Probe {
  container: string;
  duration_us: number;
  streams: ProbeStream[];
  limitations: string[];
}

export interface Segmentation {
  method: 'scene_change';
  threshold: number;
  min_segment_us: number;
  max_segment_us: number;
}

export interface SelectedSegment { segment_id: string; start_us: number; end_us: number }

export interface IngestPlanView {
  revision: number;
  kind: 'analysis' | 'selection';
  sha256: string;
  analysis?: { video_stream_index: number; game_audio_stream_index: number | null; source_range_us: [number, number]; segmentation: Segmentation };
  selection?: { base_plan_revision: number; selected_segments: SelectedSegment[]; output: { fps: 30 | 60; sample_rate: 48000 } };
}

export interface ExecutionStatus {
  status?: string;
  wait_reason?: string;
  connection?: string;
  checkpoints_done?: number;
  total_units?: number;
  recovery_from?: string;
  cleanup?: string;
}

export interface IngestRunView {
  run: IngestRun;
  source: RecordingSource;
  probe?: Probe;
  plans: IngestPlanView[];
  tasks: { task: Task; binding: { stage: string; execution_seq: number; attempt_of?: string; invalidated: boolean } }[];
  segments?: { count: number; cut_count: number; method_version: string; range_us: [number, number] };
  files: string[];
  asset: Asset;
  execution?: ExecutionStatus;
}

const WAITING = new Set(['allocated', 'waiting_resource', 'waiting_drain', 'queued', 'stop_requested', 'draining', 'suspended_connection']);

export function executionBanner(execution?: ExecutionStatus | null): string | null {
  if (!execution) return null;
  const state = execution.status ?? '';
  const cleanup = execution.cleanup ?? '';
  if (state === 'resource_busy' || execution.wait_reason === 'resource_busy') return '资源被有效执行占用';
  if (state === 'cleanup_blocked' || cleanup === 'blocked' || cleanup === 'cleanup_blocked') return '清理受阻';
  if (state === 'suspended_connection' || execution.connection === 'suspended') {
    return `连接中断，暂停；已确认 ${execution.checkpoints_done ?? 0} 个检查点`;
  }
  if (state === 'recovering') return '恢复中';
  if (state === 'waiting_resource' || state === 'waiting_drain' || state === 'draining' || state === 'stop_requested') return '等待旧执行退出';
  return null;
}

/** 未 begin 不显示进度。未成功的任务即使进度被写成 1 也不显示 100%。 */
export function displayProgress(task: Task | undefined, execution: ExecutionStatus | null | undefined, run?: IngestRun): { value: number; label: string } | null {
  if (!task) return null;
  if (execution) {
    const begun = execution.status === 'running' || execution.status === 'succeeded';
    if (!begun || WAITING.has(execution.status ?? '')) return null;
  } else if (task.status === 'queued' || task.status === 'pending') {
    return null;
  }
  const raw = typeof task.progress === 'number' && Number.isFinite(task.progress) ? task.progress : 0;
  const finished = task.status === 'succeeded' || run?.state === 'ready';
  const value = !finished && raw >= 1 ? 0.99 : Math.min(1, Math.max(0, raw));
  const percent = finished ? Math.round(Math.min(1, Math.max(0, raw)) * 100) : Math.round(value * 100);
  return { value: finished ? Math.min(1, Math.max(0, raw)) : value, label: `${task.type} · ${percent}% · ${finished ? '已完成' : '执行中'}` };
}

export interface SegmentItem {
  segment_id: string;
  start_us: number;
  end_us: number;
  score: number;
  reason: 'scene_change' | 'range_start' | 'duration_limit' | 'merged_short';
  thumbnail_key?: string;
}

export interface SegmentPage { total: number; limit: number; offset: number; analysis_revision: number; method_version: string; items: SegmentItem[] }

export const ACTIVE_STATES: readonly IngestState[] = ['queued', 'processing', 'awaiting_review'];

export const usToClock = (us: number): string => {
  const value = Math.max(0, Math.round(us));
  const seconds = Math.floor(value / 1e6);
  const h = Math.floor(seconds / 3600), m = Math.floor(seconds / 60) % 60;
  const fraction = String(value % 1e6).padStart(6, '0').replace(/0{1,3}$/, '');
  return `${h > 0 ? `${h}:` : ''}${String(m).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}.${fraction}`;
};

/** Preserve the selected source coverage; never include an unselected gap. */
export function splitSelection(segs: SelectedSegment[], id: string, at: number, newId: string): SelectedSegment[] {
  const index = segs.findIndex(s => s.segment_id === id);
  const s = segs[index];
  if (!s || !Number.isSafeInteger(at) || at <= s.start_us || at >= s.end_us || segs.some(s => s.segment_id === newId)) throw new Error('拆分点必须位于片段内部');
  return [...segs.slice(0, index), { ...s, end_us: at }, { segment_id: newId, start_us: at, end_us: s.end_us }, ...segs.slice(index + 1)];
}

export function mergeSelection(segs: SelectedSegment[], id: string): SelectedSegment[] {
  const index = segs.findIndex(s => s.segment_id === id);
  const a = segs[index], b = segs[index + 1];
  if (!a || !b || a.end_us !== b.start_us) throw new Error('只能合并首尾相接的已选片段');
  return [...segs.slice(0, index), { ...a, end_us: b.end_us }, ...segs.slice(index + 2)];
}

/** 解析 "mm:ss.sss" / "h:mm:ss.sss" / 纯秒为整数微秒；非法返回 null。 */
export const clockToUs = (text: string): number | null => {
  const parts = text.trim().split(':');
  if (parts.length === 0 || parts.length > 3 || parts.some((p) => !/^\d+(\.\d{1,6})?$/.test(p))) return null;
  const seconds = parts.reduce((acc, p) => acc * 60 + Number(p), 0);
  return Number.isFinite(seconds) ? Math.round(seconds * 1e6) : null;
};

/** 与 Go ValidateSelection 相同的顺序/重叠/边界检查，提交前给出可读提示。 */
export function selectionProblem(segs: SelectedSegment[], durationUs: number, fps: number, policy?: IngestPolicy): string | null {
  if (segs.length === 0) return '至少选择一个片段';
  if (policy && segs.length > policy.max_selected) return `最多选择 ${policy.max_selected} 个片段`;
  let prevEnd = 0, total = 0;
  for (const [i, s] of segs.entries()) {
    if (s.start_us < 0 || s.end_us > durationUs || s.end_us <= s.start_us) return `${s.segment_id} 超出录像范围或长度为零`;
    if (i > 0 && s.start_us < prevEnd) return `${s.segment_id} 与前一片段重叠或顺序颠倒`;
    if (Math.round(((s.end_us - s.start_us) * fps) / 1e6) < 1) return `${s.segment_id} 短于一帧`;
    prevEnd = s.end_us;
    total += s.end_us - s.start_us;
  }
  if (policy && total > policy.max_output_us) return `选中总时长 ${usToClock(total)} 超过上限 ${usToClock(policy.max_output_us)}`;
  return null;
}
