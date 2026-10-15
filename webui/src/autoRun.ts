/**
 * 自动模式（一句话剪辑）的前端读模型：把后端的 auto 视图翻成给普通用户看的话。
 * 纯函数，不依赖 React，便于单元测试。后端字段见 docs/自动模式设计-2026-10-16.md §7 与 §6.1。
 * 约束：不向普通用户暴露内部术语（actor、auto_version、阶段英文名、原因码）。
 */

export interface AutoNotice {
  profile_id: string;
  revision: number;
  profile_sha256: string;
  external: boolean;
  notice: string;
  notice_sha256: string;
}

export interface AutoScopeInfo {
  external_egress: boolean;
  max_rounds: number;
  max_total_rounds: number;
  thresholds: Record<string, number>;
  relaxed_thresholds: boolean;
}

export interface AutoReviewReason { code: string; severity?: string; item?: string; value?: number; threshold?: number; improvable?: boolean }
export interface AutoReviewInfo { review_id: string; round: number; score: number; band: 'low' | 'medium' | 'high'; reasons: AutoReviewReason[] | null }

export interface AutoDeliveryInfo {
  delivered: boolean;
  package_ready: boolean;
  preview_id?: string;
  preview_status?: string;
  preview_is_current: boolean;
  spec: string;
}
export interface AutoDecisionPrompt { reason: string; can_export: boolean; can_revise: boolean; deadline_at?: string }

export interface AutoView {
  run_id: string;
  auto_version: number;
  state: 'active' | 'awaiting_user_decision' | 'completed' | 'halted' | 'revoked' | 'cancelled';
  halt_reason?: string;
  brief?: string;
  scope: AutoScopeInfo;
  round: number;
  max_rounds: number;
  total_rounds: number;
  max_total_rounds: number;
  delivery_hold: boolean;
  consent_granted_by_run?: boolean;
  latest_review: AutoReviewInfo | null;
  delivery: AutoDeliveryInfo;
  decision: AutoDecisionPrompt | null;
  delivery_hold_enforced: boolean;
}

/** 进度面板的七个阶段（人话）。 */
export const AUTO_PHASES = ['识别画面', '确认场景', '撰写解说', '配音', '字幕', '混音', '导出成片'] as const;

const STAGE_PHASE: Record<string, number> = {
  recognize: 0, scene_review: 1, sort: 1, narrate: 2, draft_review: 2, tts: 3, subtitle: 4, mix: 5, export: 6
};

/** 当前运行所在的阶段下标；到了验收（成片包已出）则是 AUTO_PHASES.length，代表七步都已完成。 */
export function phaseIndex(stage: string | undefined): number {
  if (!stage) return 0;
  if (stage === 'acceptance') return AUTO_PHASES.length;
  return STAGE_PHASE[stage] ?? 0;
}

const BAND_LABEL = { high: '高', medium: '中', low: '低' } as const;
export const bandLabel = (band?: string) => BAND_LABEL[band as keyof typeof BAND_LABEL] ?? '未评';

const REASON_TEXT: Record<string, string> = {
  low_confidence: '部分画面的识别把握偏低',
  tts_incomplete: '有解说句子没有成功配音',
  av_misaligned: '画面、声音与字幕有对不齐的地方',
  pacing_out_of_range: '解说语速偏快或偏慢',
  item_below_floor: '某一项指标低于要求',
  brief_low_coverage: '成片内容与你的要求关联较弱',
  brief_unparsable: '你的要求太短，无法核对成片是否贴合',
  input_missing: '缺少可用于自检的数据',
  no_scenes: '没有识别出可用的场景',
  preview_failed: '成片预览没有生成成功',
  preview_check_failed: '成片预览的检查没有通过',
  duration_mismatch: '成片时长与剪辑单不一致',
  score_below_low: '综合得分偏低'
};

/** 自评原因（去重，最多 n 条）；未知原因码一律不显示，避免暴露内部术语。 */
export function reasonLines(review: AutoReviewInfo | null | undefined, n = 4): string[] {
  const out: string[] = [];
  for (const r of review?.reasons ?? []) {
    const text = REASON_TEXT[r.code];
    if (text && !out.includes(text)) out.push(text);
    if (out.length >= n) break;
  }
  return out;
}

const HALT_TEXT: Record<string, string> = {
  human_takeover: '你手动改动了内容，自动模式已停止，后续请你接着处理。',
  human_decision_conflict: '你的手动确认与自动检查冲突，自动模式已停止，请你接着处理。',
  deadline: '自动剪辑用时超过了授权的时间上限，已停止。',
  decision_timeout: '超过了等你决定的期限，已停止。没有导出任何内容。',
  not_improvable: '自检发现的问题不是重剪能解决的（例如成片渲染失败），已停止，避免白白消耗。',
  stage_failed: '某一步处理失败，自动重试后仍未成功，已停止。',
  max_tts_chars: '配音字数超过了授权上限，已停止。',
  budget: '自动操作次数用完了授权的上限，已停止。',
  preview_unavailable: '无法生成成片预览（导出内容与清单不符），已停止。',
  narration_refused: '解说内容没有通过自动检查，已停止，请你审阅解说。',
  user_discarded: '你已放弃这次自动结果。'
};

export type AutoPanelKind = 'running' | 'rerunning' | 'revising' | 'decision' | 'done' | 'halted' | 'revoked' | 'cancelled';

export interface AutoPanelModel {
  kind: AutoPanelKind;
  alert: 'info' | 'success' | 'warning' | 'error';
  title: string;
  description: string;
  /** 进度条是否还在走（active 才在走）。 */
  live: boolean;
  /** 能否撤销授权：只有还没结束的才有意义。 */
  canRevoke: boolean;
  /** 是否提供「改为手动」。 */
  canTakeOver: boolean;
}

export function decisionHeadline(reason: string | undefined): { title: string; description: string } {
  switch (reason) {
    case 'exhausted': return { title: '已自动重剪多轮，仍未达到高档', description: '你可以直接导出最近一版，或写下修改意见让 Agent 再剪一次。' };
    case 'asset_busy': return { title: '这个素材已有另一个进行中的任务', description: '先处理那个任务，再回来决定。' };
    default: return { title: '成片已生成，AI 自评为中档，请你决定', description: '直接导出当前这版，或写下修改意见让 Agent 再剪一次。' };
  }
}

/** 把后端视图翻成面板文案。run 用来识别「步骤失败」。 */
export function autoPanelModel(a: AutoView, run?: { status?: string; stage?: string }): AutoPanelModel {
  const base = { canRevoke: a.state === 'active' || a.state === 'awaiting_user_decision', canTakeOver: false };
  switch (a.state) {
    case 'active': {
      if (run?.status === 'failed') {
        return { ...base, kind: 'running', alert: 'warning', live: false, canTakeOver: true, title: '处理中遇到问题', description: 'Agent 会自动重试一次；仍失败会停下来交给你。也可以现在改为手动处理。' };
      }
      if (a.total_rounds > a.round) {
        return { ...base, kind: 'revising', alert: 'info', live: true, title: '正在按你的修改意见重新剪辑', description: '完成后会再做一次自检。' };
      }
      if (a.round > 0) {
        return { ...base, kind: 'rerunning', alert: 'warning', live: true, title: `自评偏低，正在自动重剪第 ${a.round} 轮`, description: `最多自动重剪 ${a.max_rounds} 轮；重剪后会再次自检。` };
      }
      return { ...base, kind: 'running', alert: 'info', live: true, title: 'Agent 正在自动剪辑', description: '识别、撰稿、配音、字幕和导出都会自动完成，你可以先去做别的。' };
    }
    case 'awaiting_user_decision': {
      const h = decisionHeadline(a.decision?.reason ?? a.halt_reason);
      return { ...base, kind: 'decision', alert: 'warning', live: false, title: h.title, description: h.description };
    }
    case 'completed':
      return { ...base, kind: 'done', alert: 'success', live: false, title: '自动剪辑完成', description: 'AI 自评不等于人工验收，请在审查页确认后再正式使用。' };
    case 'halted':
      return { ...base, kind: 'halted', alert: 'error', live: false, canTakeOver: true, title: '自动剪辑已停止', description: HALT_TEXT[a.halt_reason ?? ''] ?? '自动模式已停止，请你接着处理。' };
    case 'revoked':
      return { ...base, kind: 'revoked', alert: 'info', live: false, canTakeOver: true, title: '你已撤销自动授权', description: '不会再有任何自动动作；已经在进行的步骤不会被强行中断，后续请你手动确认。' };
    case 'cancelled':
      return { ...base, kind: 'cancelled', alert: 'info', live: false, title: '任务已取消', description: '这次自动剪辑已被取消。' };
    default:
      return { ...base, kind: 'running', alert: 'info', live: false, title: '状态未知', description: '请刷新后再看。' };
  }
}

/** 是否还需要轮询：进行中、等待决定。已结束的状态靠事件/手动刷新。 */
export const autoPolling = (state?: string) => state === 'active' || state === 'awaiting_user_decision';

/** 授权阈值：只开放 high_score_min 一项放宽；默认 0.85，硬下限 0.80（后端 CheckOverrides）。 */
export const HIGH_SCORE_DEFAULT = 0.85;
export const HIGH_SCORE_FLOOR = 0.8;

export interface AutoStartInput {
  profileId: string;
  revision: number;
  expectedAssetVersion: string;
  brief: string;
  maxRounds: number;
  relaxHighScore?: number;
  relaxConfirmed: boolean;
  noticeSha: string;
  external: boolean;
  idempotencyKey: string;
}

/** 组装 POST /api/assets/{id}/auto-runs 的请求体。只有明确确认并真的更宽松时才带 relax_thresholds。 */
export function buildAutoRunBody(i: AutoStartInput) {
  const relaxed = i.relaxConfirmed && i.relaxHighScore !== undefined && i.relaxHighScore < HIGH_SCORE_DEFAULT;
  const scope: Record<string, unknown> = { max_rounds: i.maxRounds };
  if (relaxed) { scope.thresholds = { high_score_min: i.relaxHighScore }; scope.relax_thresholds = true; }
  return {
    profile_id: i.profileId,
    revision: i.revision,
    expected_asset_version: i.expectedAssetVersion,
    brief: i.brief.trim(),
    scope,
    authorization: { accept_external_egress: i.external, notice_sha256: i.noticeSha },
    idempotency_key: i.idempotencyKey
  };
}

/** 启动按钮能否点：授权已勾选、预设可用、预检通过；放宽阈值时必须再勾选确认。 */
export function canStartAuto(s: { authorized: boolean; profileOk: boolean; noticeReady: boolean; preflightOk: boolean; busy: boolean; relaxOn: boolean; relaxValue?: number; relaxConfirmed: boolean }): boolean {
  if (!s.authorized || !s.profileOk || !s.noticeReady || !s.preflightOk || s.busy) return false;
  const relaxed = s.relaxOn && s.relaxValue !== undefined && s.relaxValue < HIGH_SCORE_DEFAULT;
  if (relaxed && !s.relaxConfirmed) return false;
  if (s.relaxOn && (s.relaxValue === undefined || s.relaxValue < HIGH_SCORE_FLOOR || s.relaxValue > HIGH_SCORE_DEFAULT)) return false;
  return true;
}

/** 后端错误 → 一句人话。不回显后端的英文原句（含内部术语）。 */
export function autoErrorText(e: unknown): string {
  const { status, code } = (e ?? {}) as { status?: number; code?: string };
  const msg = e instanceof Error ? e.message : '';
  if (status === 409 && code === 'invalid_state') return '当前状态下无法这样操作，已刷新，请看看最新状态。';
  if (status === 409) return '状态刚刚发生了变化（可能是你或 Agent 刚做了别的操作），已刷新，请再试一次。';
  if (status === 400 && /relax_thresholds|looser/.test(msg)) return '放宽的自评标准需要你明确确认。';
  if (status === 400 && /notice_sha256/.test(msg)) return '授权说明已更新，请重新阅读并勾选授权。';
  if (status === 400 && /at least|at most/.test(msg)) return '放宽的自评标准超出了允许范围。';
  if (status === 400 && /accept_external_egress/.test(msg)) return '需要先勾选授权，同意向外部端点发送数据。';
  if (status === 403) return '没有权限执行这个操作。';
  if (status === 404) return '没有找到对应的内容，请刷新后再试。';
  return '操作没有成功，请稍后再试。';
}

/**
 * 自动模式的预检：外发授权（external_authorization）由「全局授权」勾选本身在启动时一并授予，
 * 所以它不应拦住自动启动；其他受阻项照常拦。
 */
export function autoPreflightBlockers(checks: { code: string; status: string; message: string }[] | undefined): string[] {
  return (checks ?? []).filter(c => c.status === 'blocked' && c.code !== 'external_authorization').map(c => c.message);
}
