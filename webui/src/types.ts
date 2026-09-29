export interface Asset {
  asset_id: string;
  status: string;
  agent_visible: boolean;
  human_approved: boolean;
  locked: boolean;
  allowed_agents: string[];
  artifacts: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface Task {
  task_id: string;
  asset_id: string;
  type: string;
  agent_role: string;
  agent_id?: string;
  status: string;
  progress: number;
  message?: string;
  lease_expires_at?: string;
  claimed_at?: string;
  updated_at: string;
  artifacts?: Record<string, string>;
  /** 同资产上必须先成功的任务 ID；创建后不可修改。 */
  depends_on?: string[];
  /** 租约到期被回收的次数；达到上限后服务端判定失败。 */
  attempts?: number;
}

export interface AuditLog {
  id: number;
  actor: string;
  action: string;
  target: string;
  detail?: string;
  created_at: string;
}

export type GovernancePatch = Partial<Pick<Asset,
  'agent_visible' | 'human_approved' | 'locked' | 'allowed_agents'>>;

export type CreateTask = Pick<Task, 'task_id' | 'asset_id' | 'type' | 'agent_role' | 'depends_on'>;

/** 把逗号/空白分隔的依赖输入整理成去重后的 ID 列表；空输入返回 undefined，不发送该字段。 */
export function parseDependsOn(input: string): string[] | undefined {
  const ids = [...new Set(input.split(/[\s,，]+/).map((s) => s.trim()).filter(Boolean))];
  return ids.length > 0 ? ids : undefined;
}

export interface DeliveryFile {
  key: string;
  name: string;
  mime: string;
  size: number;
  playable: boolean;
}

/* ------------------------------------------------------------------ *
 * 二阶段「资产治理与审计观测」追加类型。
 *
 * 下面全部是新增声明：上面的 Asset / Task / AuditLog 及其字段一个都没动，
 * 既有导入方（App.tsx、useEvents.ts）无需任何改动即可继续编译。
 * ------------------------------------------------------------------ */

/**
 * 素材媒体类别。
 *
 * 服务端 Asset 没有类别字段（control-plane/internal/model/asset.go），类别是
 * WebUI 依 artifacts 键名推断出来的展示层概念，任何字段都不得标称它来自服务端。
 */
export type AssetCategory = 'video' | 'audio' | 'narration' | 'bgm' | 'uncategorized';

/** 手动补充导入的本地草稿（控制面尚无资产创建/删除路由，见 governanceModel.ts）。 */
export interface AssetDraft {
  asset_id: string;
  categories: AssetCategory[];
  status: string;
  duration_seconds: number | null;
  resolution: string | null;
  note: string;
  /** 'manual' = 表单逐条录入，'json' = 粘贴 JSON 批量导入。 */
  source: 'manual' | 'json';
  imported_at: string;
}

/** asset_id → 关联任务的索引，由审计 task.create + GET /api/tasks/{id} 推导。 */
export type AssetTaskIndex = Record<string, AssetTaskSummary[]>;

/** 任务在资产面板里需要展示的最小子集。asset_id 是归组字段，缺了它无法按资产分组。 */
export type AssetTaskSummary = Pick<Task,
  'task_id' | 'asset_id' | 'status' | 'progress' | 'type' | 'agent_role' | 'updated_at'>;

/** 审计操作类型候选项；group 只用于界面分组。 */
export interface AuditActionOption {
  value: string;
  label: string;
  group: string;
}

/** actor 的分类，依据服务端写入审计时的 actor 前缀。 */
export type AuditActorKind = 'human' | 'system' | 'agent' | 'queue' | 'other';

/** 审计面板过滤条件。 */
export interface AuditFilters {
  /** datetime-local 值（本地时区），空串表示不限制。 */
  from: string;
  to: string;
  actions: string[];
  actorKinds: AuditActorKind[];
  keyword: string;
}

/** 任务查询上限（governanceModel.ts 的 MAX_TASK_LOOKUPS）。 */
export const MAX_TASK_LOOKUPS = 120;

/** 单个任务查询失败的原因，供界面列出而不是静默丢一行。 */
export interface TaskFetchFailure {
  task_id: string;
  message: string;
}

/** 批量任务查询的结果：成功的任务 + 逐条失败原因。 */
export interface TaskFetchReport {
  tasks: AssetTaskSummary[];
  failed: TaskFetchFailure[];
}

/** SSE 连接状态：useEvents 输出的详细连接生命周期。 */
export type ConnectionStatus =
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'disconnected'
  | 'error';

/**
 * 任务生命周期状态。与 control-plane 的任务状态机（queued -> claimed ->
 * running -> succeeded，running 可失败或被取消）对齐；`paused` 是编排控制台
 * 的预留状态，只有后端实现暂停后才可能出现。
 */
export type TaskLifecycleStatus =
  | 'queued'
  | 'claimed'
  | 'running'
  | 'paused'
  | 'succeeded'
  | 'failed'
  | 'cancelled';

/** 任务编排控制台可下发的控制动作（预留类型）。 */
export type TaskControlAction = 'pause' | 'resume';

/** SSE 事件名，必须与 Go events bus 的发布名一致。 */
export type StreamEventName =
  | 'asset_created'
  | 'asset_updated'
  | 'task_created'
  | 'task_updated';

/**
 * 任务控制端点的返回确认。后端可能返回完整 Task，也可能只回执
 * task_id/status，因此除 task_id 外全部按可选字段解析；UI 只把它当作
 * “已受理”的回执，真实状态始终以 GET /api/tasks/{id} 为准。
 */
export interface TaskControlAck extends Partial<Task> {
  task_id: string;
  action?: TaskControlAction;
  accepted?: boolean;
}

/** 非终态任务状态（仍在服务端生命周期中，界面仅观察）。 */
export const TASK_ACTIVE_STATUSES: readonly string[] = ['queued', 'claimed', 'running', 'paused'];

/** 终态任务状态：服务端不再推进。 */
export const TASK_TERMINAL_STATUSES: readonly string[] = ['succeeded', 'failed', 'cancelled'];

/**
 * 任务是否仍在生命周期中（用于观察态展示，不驱动任何控制请求）。
 *
 * 未识别的状态一律返回 false：状态机归 Go 所有，白名单未跟上时宁可按
 * 非活动展示，也不在客户端臆测状态语义。
 */
export function isTaskActiveStatus(status: string): boolean {
  return TASK_ACTIVE_STATUSES.includes(status);
}

/**
 * 任务是否已终结。
 *
 * 与 isTaskActiveStatus 都是白名单：两边都不认识的第三态交给 UI 显示原始
 * 状态字符串，而不是替服务端猜测能不能操作。
 */
export function isTaskTerminalStatus(status: string): boolean {
  return TASK_TERMINAL_STATUSES.includes(status);
}

/** 把 0..1 的任务进度收敛为 0..100 的整数百分比。 */
export function taskProgressPercent(progress: number | undefined): number {
  if (typeof progress !== 'number' || !Number.isFinite(progress)) return 0;
  return Math.min(100, Math.max(0, Math.round(progress * 100)));
}
