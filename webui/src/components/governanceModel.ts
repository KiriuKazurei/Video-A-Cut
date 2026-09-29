/**
 * 二阶段「资产治理与审计观测」的纯逻辑模块。
 *
 * 这里不放 React：类别推断、草稿校验、审计过滤都是可单测的纯函数，组件只负责
 * 取数和渲染。服务端契约见 docs/二阶段开发文档.md「已有 HTTP 契约」——本模块
 * 只使用其中真实存在的端点，不虚构接口：
 *   GET    /api/assets        资产列表
 *   GET    /api/assets/{id}   资产详情
 *   PATCH  /api/assets/{id}   治理字段（agent_visible / locked / human_approved / allowed_agents）
 *   GET    /api/tasks/{id}    按 ID 查任务
 *   GET    /api/audit?limit=N 审计列表（唯一参数是 limit）
 */

import { ApiError } from '../api.ts';
import type {
  AssetCategory,
  AssetDraft,
  AssetTaskIndex,
  AssetTaskSummary,
  AuditActionOption,
  AuditActorKind,
  AuditFilters,
  AuditLog
} from '../types';

/* ------------------------------------------------------------------ *
 * 通用展示
 * ------------------------------------------------------------------ */

/** RFC3339 字符串转本地时间；空值或无法解析时原样回退，不静默变成「1970」。 */
export function stamp(value: string | undefined | null): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString('zh-CN');
}

/** 展示服务端可读 message，同时保留 code 供行为分支（不得按 message 文本判断错误）。 */
export function describeError(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

/** 进度归一化到 0–1 后取整；NaN/Infinity 不会渲染成 "NaN%"。 */
export function percent(progress: number): string {
  if (!Number.isFinite(progress)) return '—';
  const clamped = Math.min(1, Math.max(0, progress));
  return `${Math.round(clamped * 100)}%`;
}

/* ------------------------------------------------------------------ *
 * 素材类别
 *
 * 控制面的 Asset 没有 type/category 字段（control-plane/internal/model/asset.go），
 * 唯一可用的类别信号是 artifacts 的键集合，因此这里做「产物类别」推断，并在界面上
 * 明确标注为推断值，绝不冒充服务端字段。
 * ------------------------------------------------------------------ */

export const ASSET_CATEGORIES: readonly AssetCategory[] = ['video', 'audio', 'narration', 'bgm', 'uncategorized'];

export const ASSET_CATEGORY_LABELS: Record<AssetCategory, string> = {
  video: '视频',
  audio: '音频',
  narration: '旁白',
  bgm: 'BGM',
  uncategorized: '其他产物'
};

interface ArtifactGroup {
  readonly category: AssetCategory;
  readonly tokens: readonly string[];
}

/**
 * 匹配优先级即数组顺序：一个产物键只归入第一个命中的类别。
 * 否则 "voice.wav" 会同时算旁白和音频，类别过滤就退化成「全都显示」。
 */
const ARTIFACT_GROUPS: readonly ArtifactGroup[] = [
  { category: 'narration', tokens: ['voice', 'narration', 'tts', 'voiceover', 'dub', 'speech'] },
  { category: 'bgm', tokens: ['bgm', 'music', 'ost'] },
  { category: 'video', tokens: ['preview', 'video', 'source', 'clip', 'frame', 'mp4', 'mov', 'mkv', 'webm'] },
  { category: 'audio', tokens: ['game', 'gameplay', 'audio', 'wav', 'mix', 'sound', 'stem', 'sfx'] }
];

export interface AssetClassification {
  /** 命中的媒体类别，不含 uncategorized。 */
  categories: AssetCategory[];
  /** 每个类别命中的产物键，用作界面上的推断依据。 */
  evidence: Partial<Record<AssetCategory, string[]>>;
  /** 未归类的产物键（edl/xml/otio/subtitles 等工程产物）。 */
  other: string[];
}

/**
 * 按 artifacts 键/值推断素材包含哪些媒体类别。
 *
 * 用「非字母数字切分后的整段相等」而不是子串包含：子串会让 "remove.wav" 命中
 * 视频 token "mov"，也会让 "voice.wav" 同时命中旁白和音频。整段相等意味着
 * "voice.wav"→['voice','wav']、'voice' 精确命中旁白，行为可预测。
 */
export function classifyAsset(artifacts: Record<string, string> | undefined): AssetClassification {
  const evidence: Partial<Record<AssetCategory, string[]>> = {};
  const other: string[] = [];

  for (const [key, value] of Object.entries(artifacts ?? {})) {
    const segments = `${key} ${value ?? ''}`
      .toLowerCase()
      .split(/[^a-z0-9]+/)
      .filter((segment) => segment.length > 0);
    const hit = ARTIFACT_GROUPS.find((group) =>
      group.tokens.some((token) => segments.includes(token)));
    if (!hit) {
      other.push(key);
      continue;
    }
    (evidence[hit.category] ??= []).push(key);
  }

  const categories = ASSET_CATEGORIES.filter(
    (category) => category !== 'uncategorized' && (evidence[category]?.length ?? 0) > 0);

  return { categories, evidence, other };
}

/** 素材类别标签，多个类别时用「、」连接；空集合显示为未分类。 */
export function categoryText(categories: readonly AssetCategory[]): string {
  const labels = categories.map((category) => ASSET_CATEGORY_LABELS[category]);
  return labels.length > 0 ? labels.join('、') : '未分类';
}

/** 秒数转「mm:ss」；空值、非有限值、负数都显示占位，不显示 NaN 或负数时长。 */
export function durationText(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds) || seconds < 0) return '—';
  const total = Math.round(seconds);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
}

/* ------------------------------------------------------------------ *
 * 资产状态（docs/项目开发文档.md §7.1）
 * ------------------------------------------------------------------ */

export const ASSET_STATUS_LABELS: Record<string, string> = {
  ingested: '已入库',
  recognized: '已识别',
  narrated: '已旁白',
  exported: '已导出'
};

export function assetStatusLabel(status: string): string {
  return ASSET_STATUS_LABELS[status] ?? status;
}

/** 资产状态机的展示顺序（服务端 status 不是可排序的枚举，这里只影响下拉框排序）。 */
export const ASSET_STATUS_ORDER: readonly string[] = [
  'ingested', 'recognized', 'narrated', 'exported'
];

/* ------------------------------------------------------------------ *
 * 手动补充导入的本地草稿
 *
 * 控制面当前只暴露 GET/PATCH /api/assets，没有创建或删除资产的路由
 * （control-plane/internal/api/api.go routes()）。因此「补充导入」在这里落地为
 * 带显著标识的本地草稿：绝不覆盖服务端快照、不参与治理 PATCH，且界面必须写明
 * 它未写入 Go 控制面。等服务端补上写入口，只需把草稿提交接到真实路由上。
 * ------------------------------------------------------------------ */

export const DRAFT_STORAGE_KEY = 'vac.asset-drafts.v1';
const DRAFT_ID_PATTERN = /^[A-Za-z0-9._-]{1,128}$/;
const RESOLUTION_PATTERN = /^\d{2,5}\s*[xX×]\s*\d{2,5}$/;

/**
 * 从 localStorage 读回草稿。读取失败（隐私模式、JSON 损坏、结构不符）一律返回空数组：
 * 草稿是可再生的展示层数据，为它弹一个阻塞错误不值得。
 */
export function loadDrafts(): AssetDraft[] {
  try {
    const raw = window.localStorage.getItem(DRAFT_STORAGE_KEY);
    if (raw === null) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(isAssetDraft);
  } catch {
    return [];
  }
}

/** 落盘草稿。写失败（配额、隐私模式）由调用方忽略：本地草稿不是治理数据。 */
export function saveDrafts(drafts: readonly AssetDraft[]): void {
  try {
    window.localStorage.setItem(DRAFT_STORAGE_KEY, JSON.stringify(drafts));
  } catch {
    /* 忽略：草稿写不进去时本次会话仍然可用，只是刷新后丢失。 */
  }
}

export function isAssetDraft(value: unknown): value is AssetDraft {
  if (typeof value !== 'object' || value === null) return false;
  const draft = value as Partial<AssetDraft>;
  return typeof draft.asset_id === 'string' &&
    Array.isArray(draft.categories) &&
    typeof draft.status === 'string' &&
    typeof draft.note === 'string' &&
    typeof draft.source === 'string' &&
    typeof draft.imported_at === 'string' &&
    (draft.duration_seconds === null || typeof draft.duration_seconds === 'number') &&
    (draft.resolution === null || typeof draft.resolution === 'string');
}

/** 草稿表单的原始输入态（字符串，便于受控输入与校验）。 */
export interface DraftInput {
  asset_id: string;
  categories: AssetCategory[];
  status: string;
  duration: string;
  resolution: string;
  note: string;
}

export const EMPTY_DRAFT_INPUT: DraftInput = {
  asset_id: '',
  categories: [],
  status: 'ingested',
  duration: '',
  resolution: '',
  note: ''
};

export type DraftParseResult = { ok: true; draft: AssetDraft } | { ok: false; error: string };

/** 单条草稿校验与构造，表单与 JSON 批量导入共用同一套规则。 */
export function buildDraft(input: DraftInput, takenIds: ReadonlySet<string>, source: AssetDraft['source']): DraftParseResult {
  const assetId = input.asset_id.trim();
  if (!assetId) return { ok: false, error: '资产 ID 不能为空' };
  if (!DRAFT_ID_PATTERN.test(assetId)) {
    return { ok: false, error: `资产 ID 只允许字母、数字与 . _ - ：${assetId}` };
  }
  if (takenIds.has(assetId)) return { ok: false, error: `资产 ID 已被占用：${assetId}` };

  let duration: number | null = null;
  if (input.duration.trim() !== '') {
    const seconds = Number(input.duration);
    if (!Number.isFinite(seconds) || seconds < 0) {
      return { ok: false, error: '时长必须是不小于 0 的秒数' };
    }
    duration = seconds;
  }

  const resolution = input.resolution.trim();
  if (resolution && !RESOLUTION_PATTERN.test(resolution)) {
    return { ok: false, error: '分辨率格式应为 1920x1080' };
  }

  const categories = ASSET_CATEGORIES.filter(
    (category) => category !== 'uncategorized' && input.categories.includes(category));
  const status = input.status.trim();

  return {
    ok: true,
    draft: {
      asset_id: assetId,
      categories,
      status: status === '' ? 'ingested' : status,
      duration_seconds: duration,
      resolution: resolution === '' ? null : resolution,
      note: input.note.trim(),
      source,
      imported_at: new Date().toISOString()
    }
  };
}

function draftInputFromRecord(record: unknown): DraftInput | string {
  if (typeof record !== 'object' || record === null || Array.isArray(record)) return '每条记录必须是 JSON 对象';
  const row = record as Record<string, unknown>;
  const readString = (key: string): string =>
    typeof row[key] === 'string' ? (row[key] as string) : typeof row[key] === 'number' ? String(row[key]) : '';
  const readCategories = (): AssetCategory[] => {
    const raw = row.categories ?? row.category;
    if (typeof raw === 'string') {
      const single = raw.trim().toLowerCase();
      return (ASSET_CATEGORIES as readonly AssetCategory[]).includes(single as AssetCategory)
        ? [single as AssetCategory]
        : [];
    }
    if (!Array.isArray(raw)) return [];
    return raw.flatMap((item) => {
      if (typeof item !== 'string') return [];
      const value = item.trim().toLowerCase();
      return (ASSET_CATEGORIES as readonly AssetCategory[]).includes(value as AssetCategory)
        ? [value as AssetCategory]
        : [];
    });
  };
  return {
    asset_id: readString('asset_id').trim(),
    categories: readCategories(),
    status: readString('status').trim(),
    duration: readString('duration_seconds'),
    resolution: readString('resolution'),
    note: readString('note')
  };
}

export interface DraftImportReport {
  drafts: AssetDraft[];
  errors: string[];
}

/**
 * 解析粘贴的 JSON 数组并逐条校验。已占用的 ID 跳过而不是整批失败，让一次导入
 * 能带上多条有效记录；错误按序号回报，便于定位。
 */
export function parseDraftImport(text: string, takenIds: ReadonlySet<string>): DraftImportReport {
  const errors: string[] = [];
  const drafts: AssetDraft[] = [];
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch (error) {
    return { drafts, errors: [`JSON 解析失败：${describeError(error)}`] };
  }
  if (!Array.isArray(parsed)) {
    return { drafts, errors: ['JSON 顶层必须是数组，例如 [{"asset_id":"clip_002", …}]'] };
  }

  const claimed = new Set(takenIds);
  parsed.forEach((row, index) => {
    const input = draftInputFromRecord(row);
    if (typeof input === 'string') {
      errors.push(`第 ${index + 1} 条：${input}`);
      return;
    }
    const result = buildDraft(input, claimed, 'json');
    if (result.ok) {
      claimed.add(result.draft.asset_id);
      drafts.push(result.draft);
      return;
    }
    errors.push(`第 ${index + 1} 条：${result.error}`);
  });
  return { drafts, errors };
}

/* ------------------------------------------------------------------ *
 * 资产 → 任务关联
 *
 * 服务端没有「按资产列任务」的接口（docs/二阶段开发文档.md 明确写了这一点），
 * 但审计日志里每条 task.create 的 target 就是 task_id，而 Task 自带 asset_id。
 * 因此关联是这样推导出来的：读审计 → 取 task_id → 逐个 GET /api/tasks/{id} →
 * 按 asset_id 归组。全部来自真实端点，没有臆造的数据。
 * ------------------------------------------------------------------ */

/** 推导任务的查询上限。审计一页可能不止这些 task.create，超出部分明确告知用户。 */
export const MAX_TASK_LOOKUPS = 120;
const TASK_LINK_AUDIT_LIMIT = 1000;

export function taskIdsFromAudit(logs: readonly AuditLog[]): string[] {
  const ids: string[] = [];
  for (const row of logs) {
    if (row.action !== 'task.create') continue;
    const id = row.target?.trim();
    if (id && !ids.includes(id)) ids.push(id);
  }
  return ids;
}

export function assetTaskIndex(taskIds: readonly string[], tasks: readonly (AssetTaskSummary | null)[]): AssetTaskIndex {
  const index: AssetTaskIndex = {};
  for (const task of tasks) {
    if (!task || !task.asset_id) continue;
    (index[task.asset_id] ??= []).push(task);
  }
  for (const list of Object.values(index)) {
    list.sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at));
  }
  return index;
}

/** 审计页大小：服务端 maxAuditLimit 为 1000（control-plane/internal/api/audit.go）。 */
export const AUDIT_PAGE_LIMITS: readonly number[] = [100, 200, 500, TASK_LINK_AUDIT_LIMIT];
export const AUDIT_DEFAULT_LIMIT = 200;

/** 任务状态到中文的映射（服务端状态机：queued → claimed → running → succeeded/failed/cancelled）。 */
export const TASK_STATUS_LABELS: Record<string, string> = {
  queued: '排队中',
  claimed: '已领取',
  running: '执行中',
  paused: '已暂停',
  succeeded: '已成功',
  failed: '已失败',
  cancelled: '已取消'
};

export function taskStatusLabel(status: string): string {
  return TASK_STATUS_LABELS[status] ?? status;
}

/* ------------------------------------------------------------------ *
 * 审计过滤
 *
 * GET /api/audit 只认 limit，时间范围与操作类型过滤在浏览器内完成。这意味着过滤
 * 只作用于已加载的这一页，界面必须显示「已加载 N 条 / 命中 M 条」而不是让用户
 * 以为筛的是全表。
 * ------------------------------------------------------------------ */

export const AUDIT_ACTOR_KINDS: readonly AuditActorKind[] = ['human', 'system', 'agent', 'queue', 'other'];

export const AUDIT_ACTOR_KIND_LABELS: Record<AuditActorKind, string> = {
  human: '人工',
  system: '系统',
  agent: 'Agent',
  queue: '队列',
  other: '其他'
};

/** actor 前缀是服务端区分人机动作的依据（"system" / "human:webui" / "agent:<id>"）。 */
export function auditActorKind(actor: string): AuditActorKind {
  const value = actor.trim().toLowerCase();
  if (value.startsWith('human:')) return 'human';
  if (value === 'system' || value.startsWith('system')) return 'system';
  if (value.startsWith('agent:')) return 'agent';
  if (value === 'queue' || value.startsWith('queue')) return 'queue';
  return 'other';
}

/** 服务端当前真实写出的动作（internal/service/*.go 的 s.audit 调用）。 */
const KNOWN_ACTIONS: readonly AuditActionOption[] = [
  { value: 'asset.create', label: '资产入库', group: '资产' },
  { value: 'asset.governance', label: '治理变更', group: '资产' },
  { value: 'asset.approve', label: '人工批准', group: '资产' },
  { value: 'asset.unapprove', label: '撤销批准', group: '资产' },
  { value: 'task.create', label: '创建任务', group: '任务' },
  { value: 'task.claim', label: '领取任务', group: '任务' },
  { value: 'task.submit', label: '提交结果', group: '任务' },
  { value: 'task.fail', label: '任务失败', group: '任务' },
  { value: 'task.requeue', label: '租约到期重排', group: '任务' },
  { value: 'task.cascade_error', label: '依赖级联异常', group: '任务' },
  { value: 'delivery.import', label: '交付包导入', group: '资产' }
];

/**
 * 操作类型候选项 = 已知动作 ∪ 本页实际出现过的动作。
 * 服务端以后新增动作时，界面不必改代码就能在过滤器里看到它。
 */
export function auditActionOptions(logs: readonly AuditLog[]): AuditActionOption[] {
  const byValue = new Map<string, AuditActionOption>();
  for (const option of KNOWN_ACTIONS) byValue.set(option.value, option);
  for (const log of logs) {
    const action = log.action?.trim();
    if (action && !byValue.has(action)) {
      byValue.set(action, { value: action, label: action, group: '其他' });
    }
  }
  return [...byValue.values()].sort((left, right) =>
    left.group.localeCompare(right.group) || left.value.localeCompare(right.value));
}

/**
 * 需求方要求按 `CREATE_TASK, TASK_REQUEUE, SYSTEM_EVENT` 等过滤（控制面未开放取消/重试，故无对应别名）。
 * 服务端并不写这些字面量——它写的是 `task.create`、`asset.governance` 等
 * （见 internal/service/*.go 的 s.audit 调用）。这里提供别名词表，把需求方
 * 的意图映射到真实 action，界面标签同时显示中文名与真实 action 值，
 * 让「筛的是什么」始终可核对，而不是让用户对着一个不存在的字符串猜。
 */
export const AUDIT_ACTION_ALIASES: Readonly<Record<string, readonly string[]>> = {
  CREATE_TASK: ['task.create'],
  TASK_REQUEUE: ['task.requeue'],
  SYSTEM_EVENT: ['asset.create']
};

/** 默认勾选的操作别名。 */
export const DEFAULT_ACTION_ALIASES: readonly string[] = ['CREATE_TASK', 'TASK_REQUEUE', 'SYSTEM_EVENT'];

/** 别名词表对应的真实 action 集合（去重、保序），用于把 UI 选择翻译成过滤条件。 */
export function expandActionAliases(aliases: readonly string[], logs: readonly AuditLog[]): string[] {
  const available = new Set(logs.map((log) => log.action));
  const expanded: string[] = [];
  for (const alias of aliases) {
    const mapped = AUDIT_ACTION_ALIASES[alias];
    if (!mapped) continue;
    for (const action of mapped) {
      // 只加入本页真实出现过的 action：把一条从未发生的动作塞进过滤条件，
      // 会让界面显示「0 条命中」却不说原因。
      if (available.has(action) && !expanded.includes(action)) expanded.push(action);
    }
  }
  return expanded;
}

export function auditActionLabel(action: string): string {
  return KNOWN_ACTIONS.find((option) => option.value === action)?.label ?? action;
}

/** target 是什么取决于动作前缀，详情展开时用它把 ID 解释成人能读的一句话。 */
export function auditTargetHint(action: string): string {
  if (action.startsWith('asset.')) return 'target 是资产 ID';
  if (action.startsWith('task.')) return 'target 是任务 ID';
  return 'target 是服务端记录的对象标识';
}

/** datetime-local 的边界值转毫秒；未填的一端取开区间，解析失败按未填处理。 */
function localBound(value: string, edge: 'start' | 'end'): number {
  if (value.trim() === '') return edge === 'start' ? 0 : Number.POSITIVE_INFINITY;
  const parsed = Date.parse(value);
  if (Number.isNaN(parsed)) return edge === 'start' ? 0 : Number.POSITIVE_INFINITY;
  // 结束时间按「含当分钟」处理，否则 14:30 的筛选看不到 14:30:05 的记录。
  return edge === 'start' ? parsed : parsed + 60_000;
}

export function filterAuditLogs(logs: readonly AuditLog[], filters: AuditFilters): AuditLog[] {
  const from = localBound(filters.from, 'start');
  const to = localBound(filters.to, 'end');
  const actions = new Set(filters.actions);
  const kinds = new Set(filters.actorKinds);
  const keyword = filters.keyword.trim().toLowerCase();

  return logs.filter((log) => {
    if (from !== 0 || to !== Number.POSITIVE_INFINITY) {
      const at = Date.parse(log.created_at);
      if (Number.isNaN(at)) return false;
      if (at < from || at > to) return false;
    }
    if (actions.size > 0 && !actions.has(log.action)) return false;
    if (kinds.size > 0 && !kinds.has(auditActorKind(log.actor))) return false;
    if (keyword !== '') {
      const haystack = [log.actor, log.action, log.target, log.detail ?? '', String(log.id)]
        .join(' ')
        .toLowerCase();
      if (!haystack.includes(keyword)) return false;
    }
    return true;
  });
}

export interface AuditActionCount {
  value: string;
  label: string;
  count: number;
}

/** 观测视图用：当前命中集合里各动作的出现次数，多的排在前面。 */
export function auditActionCounts(logs: readonly AuditLog[]): AuditActionCount[] {
  const counts = new Map<string, number>();
  for (const log of logs) counts.set(log.action, (counts.get(log.action) ?? 0) + 1);
  return [...counts.entries()]
    .map(([value, count]) => ({ value, label: auditActionLabel(value), count }))
    .sort((left, right) => right.count - left.count || left.value.localeCompare(right.value));
}
