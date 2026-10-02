import type {
  AcceptanceRecord,
  Asset,
  AssetTaskSummary,
  AuditLog,
  CreateTask,
  GovernancePatch,
  DeliveryFile,
  ReviewView,
  Task,
  TaskFetchReport,
  WorkflowRun,
  WorkflowSnapshot
} from './types';
import type { ProcessingProfile, ProcessingProvider, ProviderDiagnostic, PreparationReport } from './preparation';
import type { IngestRoots, IngestRun, IngestRunView, RecordingSource, SegmentPage, Segmentation, SelectedSegment } from './ingest';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { Accept: 'application/json', ...init.headers }
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const error = body as { error?: { code?: string; message?: string } } | null;
    throw new ApiError(response.status, error?.error?.code ?? 'http_error',
      error?.error?.message ?? `请求失败（HTTP ${response.status}）`);
  }
  return body as T;
}

function json(method: 'POST' | 'PATCH', body: object): RequestInit {
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

export const api = {
  diagnoseProvider: (provider: ProcessingProvider, operation: 'test' | 'models', apiKey: string, signal?: AbortSignal) =>
    request<ProviderDiagnostic>(`/api/providers/${operation}`, { ...json('POST', { provider, ...(apiKey ? { api_key: apiKey } : {}) }), signal }),
  listProfiles: () => request<ProcessingProfile[]>('/api/processing-profiles?limit=50'),
  getProfile: (id:string, revision:number) => request<{profile:ProcessingProfile;profile_sha256:string}>(`/api/processing-profiles/${encodeURIComponent(id)}/revisions/${revision}`),
  saveProfile: (profile:ProcessingProfile, expected:number) => request<ProcessingProfile>('/api/processing-profiles',json('POST',{profile,expected_revision:expected,idempotency_key:crypto.randomUUID()})),
  profileConsent: (id:string,revision:number,granted:boolean) => request<{granted:boolean}>(`/api/processing-profiles/${encodeURIComponent(id)}/revisions/${revision}/external-consent`,{method:granted?'POST':'DELETE'}),
  preparedPreflight: (asset:string,id:string,revision:number) => request<PreparationReport>(`/api/assets/${encodeURIComponent(asset)}/prepared-preflight`,json('POST',{profile_id:id,revision})),
  startPrepared: (asset:string,id:string,revision:number,version:string) => request<WorkflowRun>(`/api/assets/${encodeURIComponent(asset)}/prepared-workflows`,json('POST',{profile_id:id,revision,expected_asset_version:version,idempotency_key:crypto.randomUUID()})),
  preflightWorkflow: (assetId: string, profile: ProcessingProfile) =>
    request<PreparationReport>(`/api/assets/${encodeURIComponent(assetId)}/workflow-preflight`, json('POST', profile)),
  listAssets: () => request<Asset[]>('/api/assets'),
  getAsset: (id: string) => request<Asset>(`/api/assets/${encodeURIComponent(id)}`),
  patchAsset: (id: string, patch: GovernancePatch) =>
    request<Asset>(`/api/assets/${encodeURIComponent(id)}`, json('PATCH', patch)),
  createTask: (task: CreateTask) => request<Task>('/api/tasks', json('POST', task)),
  getTask: (id: string) => request<Task>(`/api/tasks/${encodeURIComponent(id)}`),
  listAudit: (limit = 100) => request<AuditLog[]>(`/api/audit?limit=${limit}`),
  importDelivery: (assetId: string, packageDir: string) =>
    request<Asset>('/api/deliveries/import', json('POST', { asset_id: assetId, package_dir: packageDir })),
  listDeliveryFiles: (id: string) => request<DeliveryFile[]>(`/api/assets/${encodeURIComponent(id)}/files`),
  reopenAsset: (id: string) => request<Asset>(`/api/assets/${encodeURIComponent(id)}/reopen`, { method: 'POST' }),
  listWorkflows: (id: string, offset = 0) => request<WorkflowRun[]>(`/api/assets/${encodeURIComponent(id)}/workflows?limit=20&offset=${offset}`),
  startWorkflow: (id: string, idempotencyKey: string, contentMode: 'builtin' | 'configured') =>
    request<WorkflowRun>(`/api/assets/${encodeURIComponent(id)}/workflows`, json('POST', {
      idempotency_key: idempotencyKey, content_mode: contentMode
    })),
  getWorkflow: (runId: string) => request<WorkflowSnapshot>(`/api/workflows/${encodeURIComponent(runId)}`),
  reviewWorkflow: (runId: string) => request<ReviewView>(`/api/workflows/${encodeURIComponent(runId)}/review`),
  confirmScene: (runId: string, body: { expected_version: number; revision_id: string; scene_id: string; decision: 'confirmed' | 'rejected'; note?: string }) =>
    request<WorkflowRun>(`/api/workflows/${encodeURIComponent(runId)}/scene-reviews`, json('POST', body)),
  approveNarration: (runId: string, body: { expected_version: number; revision_id: string; narration_id: string }) =>
    request<WorkflowRun>(`/api/workflows/${encodeURIComponent(runId)}/narration-reviews`, json('POST', body)),
  editWorkflow: (runId: string, body: Record<string, unknown>) =>
    request<WorkflowRun>(`/api/workflows/${encodeURIComponent(runId)}/revisions`, json('POST', body)),
  cancelWorkflow: (runId: string, expectedVersion: number, reason: string) =>
    request<WorkflowRun>(`/api/workflows/${encodeURIComponent(runId)}/cancel`, json('POST', { expected_version: expectedVersion, reason })),
  listAcceptance: (runId: string) => request<AcceptanceRecord[]>(`/api/workflows/${encodeURIComponent(runId)}/acceptance`),
  retryWorkflow: (runId: string, version: number, stage: string, key: string) => request<WorkflowRun>(`/api/workflows/${encodeURIComponent(runId)}/retry`, json('POST', { expected_version: version, failed_stage: stage, idempotency_key: key })),
  revokeWorkflowNarration: (runId: string, narrationId: string, version: number) => request<WorkflowRun>(`/api/workflows/${encodeURIComponent(runId)}/narration-reviews/${encodeURIComponent(narrationId)}?expected_version=${version}`, { method: 'DELETE' }),
  recordAcceptance: (runId: string, body: { expected_version: number; check_item: string; result: 'passed' | 'failed'; note?: string }) =>
    request<AcceptanceRecord>(`/api/workflows/${encodeURIComponent(runId)}/acceptance`, json('POST', body))
};

const runPath = (run: string) => `/api/ingest-runs/${encodeURIComponent(run)}`;
const key = () => crypto.randomUUID();

/** 七阶段原始录像导入。只提交根 ID + 相对路径，绝对路径从不经过浏览器。 */
export const ingestApi = {
  roots: () => request<IngestRoots>('/api/ingest-roots'),
  register: (assetId: string, rootId: string, relativePath: string) =>
    request<{ source: RecordingSource; source_version: string; asset: Asset }>('/api/recordings',
      json('POST', { asset_id: assetId, root_id: rootId, relative_path: relativePath, idempotency_key: key() })),
  sources: (assetId: string) => request<RecordingSource[]>(`/api/assets/${encodeURIComponent(assetId)}/recordings`),
  start: (assetId: string, source: RecordingSource) =>
    request<{ run: IngestRun; task: Task }>(`/api/assets/${encodeURIComponent(assetId)}/ingest-runs`,
      json('POST', { source_id: source.source_id, expected_source_version: source.source_version, idempotency_key: key() })),
  runs: (assetId: string, offset = 0) => request<IngestRun[]>(`/api/assets/${encodeURIComponent(assetId)}/ingest-runs?limit=20&offset=${offset}`),
  run: (run: string) => request<IngestRunView>(runPath(run)),
  segments: (run: string, offset: number, limit = 50) => request<SegmentPage>(`${runPath(run)}/segments?limit=${limit}&offset=${offset}`),
  analysis: (run: string, body: { expected_version: number; video_stream_index: number; game_audio_stream_index: number | null; source_range_us: [number, number]; segmentation: Segmentation }) =>
    request<{ run: IngestRun }>(`${runPath(run)}/analysis-plans`, json('POST', { ...body, idempotency_key: key() })),
  selection: (run: string, body: { expected_version: number; base_plan_revision: number; selected_segments: SelectedSegment[]; output: { fps: 30 | 60; sample_rate: 48000 } }) =>
    request<IngestRun>(`${runPath(run)}/selection-revisions`, json('POST', { ...body, idempotency_key: key() })),
  prepare: (run: string, body: { expected_version: number; plan_revision: number; profile_id: string; profile_revision: number }) =>
    request<{ run: IngestRun }>(`${runPath(run)}/prepare`, json('POST', { ...body, idempotency_key: key() })),
  cancel: (run: string, expectedVersion: number, reason: string) =>
    request<IngestRun>(`${runPath(run)}/cancel`, json('POST', { expected_version: expectedVersion, reason })),
  retry: (run: string, expectedVersion: number, stage: string) =>
    request<{ run: IngestRun }>(`${runPath(run)}/retry`, json('POST', { expected_version: expectedVersion, stage, idempotency_key: key() }))
};

export const ingestFileURL = (run: string, fileKey: string) => `${runPath(run)}/files/${encodeURIComponent(fileKey)}`;

export const deliveryFileURL = (id: string, key: string, download = false) =>
  `/api/assets/${encodeURIComponent(id)}/files/${encodeURIComponent(key)}${download ? '?download=1' : ''}`;

export const deliveryZipURL = (id: string) =>
  `/api/assets/${encodeURIComponent(id)}/delivery.zip`;

/* ------------------------------------------------------------------ *
 * 二阶段「资产治理与审计观测」追加接口。
 *
 * 只新增成员，上面的 API 一个都没动。
 * ------------------------------------------------------------------ */

/**
 * 审计列表端点候选，按优先级排列，解析结果在页面生命周期内缓存。
 *
 * 需求方指定 `/api/v1/audit/logs`，而当前 Go 控制面只注册了
 * `GET /api/audit?limit=N`（见 control-plane/internal/api/api.go 的 routes() 与
 * docs/二阶段开发文档.md「已有 HTTP 契约」）。版本化路径尚未实现时，Go 的 wrapJSONErrors
 * 会把未匹配路由转成 404 + `not_found`，因此这里用它作为「路径不存在」的信号并回落到
 * 已文档化的端点——面板今天读到的就是服务端真实数据，等版本化路径上线则自动改用它。
 *
 * 只有 `not_found` 会触发回落。500、跨源拒绝、参数错误都原样抛给调用方：把一次
 * 服务端故障当成「路由不存在」去换路径重试，会把真实错误掩盖成一次多余请求。
 */
const AUDIT_ENDPOINT_CANDIDATES: readonly string[] = ['/api/v1/audit/logs', '/api/audit'];

/** 已解析的审计端点；null 表示还没探测过。 */
let resolvedAuditEndpoint: string | null = null;

/** 把非数组负载收敛为空数组：契约是裸数组，防御性检查很便宜，渲染层永远不需要 nil guard。 */
function asAuditRows(rows: unknown): AuditLog[] {
  return Array.isArray(rows) ? (rows as AuditLog[]) : [];
}

/**
 * 读取审计日志（最新在前，服务端排序）。
 *
 * `limit` 是服务端唯一支持的分页参数（默认页 200，上限 1000，见 audit.go）。
 * 时间范围、操作类型、actor 过滤都由浏览器在已加载的这一页内完成，界面必须把
 * 「过滤范围 = 已加载 N 条」讲清楚，不能让用户以为筛的是全表。
 */
export async function fetchAuditLogs(limit: number): Promise<AuditLog[]> {
  const candidates = resolvedAuditEndpoint === null
    ? AUDIT_ENDPOINT_CANDIDATES
    : [resolvedAuditEndpoint];
  let lastNotFound: ApiError | null = null;

  for (const path of candidates) {
    try {
      const rows = await request<unknown>(`${path}?limit=${limit}`);
      resolvedAuditEndpoint = path;
      return asAuditRows(rows);
    } catch (error) {
      if (error instanceof ApiError && error.status === 404 && error.code === 'not_found') {
        lastNotFound = error;
        continue;
      }
      throw error;
    }
  }
  throw lastNotFound ?? new ApiError(404, 'not_found', '没有可用的审计端点');
}

/** 审计日志当前实际命中的端点，供界面显示真实来源（探测成功后才非空）。 */
export function auditEndpointInUse(): string | null {
  return resolvedAuditEndpoint;
}

/** 批量取任务的并发上限：审计页可能推导出上百个 task_id，一次全发会吃掉浏览器同源连接预算。 */
export const TASK_FETCH_CONCURRENCY = 8;

/**
 * 按 ID 批量读取任务，用于把审计里的 task.create 还原成资产面板的「关联任务」。
 *
 * 控制面没有「按资产列任务」的接口（docs/二阶段开发文档.md 明确写了没有），所以
 * 关联只能这样推导：审计 target 提供 task_id → GET /api/tasks/{id} → 按 Task.asset_id
 * 归组。全部来自真实端点。
 *
 * 单个 404 不影响整批：任务可能已被审计保留策略清掉，而其余关联仍然真实可用。
 */
export async function fetchTasksByIds(
  ids: readonly string[],
  concurrency: number = TASK_FETCH_CONCURRENCY
): Promise<TaskFetchReport> {
  const unique = [...new Set(ids.map((id) => id.trim()).filter((id) => id !== ''))];
  const report: TaskFetchReport = { tasks: [], failed: [] };
  const limit = Math.max(1, Math.min(concurrency, unique.length));

  let cursor = 0;
  const workers = Array.from({ length: limit }, async () => {
    for (;;) {
      const index = cursor;
      cursor += 1;
      if (index >= unique.length) return;
      const id = unique[index];
      try {
        const task = await request<Task>(`/api/tasks/${encodeURIComponent(id)}`);
        report.tasks.push({
          task_id: task.task_id,
          asset_id: task.asset_id,
          status: task.status,
          progress: task.progress,
          type: task.type,
          agent_role: task.agent_role,
          updated_at: task.updated_at
        });
      } catch (error) {
        report.failed.push({ task_id: id, message: apiErrorText(error) });
      }
    }
  });
  await Promise.all(workers);
  return report;
}

function apiErrorText(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}
