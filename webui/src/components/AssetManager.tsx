import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, fetchTasksByIds } from '../api';
import type {
  Asset,
  AssetCategory,
  AssetDraft,
  GovernancePatch,
  TaskFetchReport
} from '../types';
import {
  ASSET_CATEGORIES,
  ASSET_CATEGORY_LABELS,
  ASSET_STATUS_LABELS,
  ASSET_STATUS_ORDER,
  assetStatusLabel,
  categoryText,
  classifyAsset,
  describeError,
  durationText,
  EMPTY_DRAFT_INPUT,
  loadDrafts,
  MAX_TASK_LOOKUPS,
  percent,
  buildDraft,
  parseDraftImport,
  saveDrafts,
  stamp,
  taskStatusLabel,
  taskIdsFromAudit,
  type AssetClassification,
  type AuditActionCount,
  type DraftInput
} from './governanceModel';
import './governance.css';

/* ------------------------------------------------------------------ *
 * 资产治理面板
 *
 * 约束（docs/二阶段开发文档.md「架构与数据所有权」）：
 *  - 浏览器只缓存服务端快照，不在客户端重实现可见性/锁定/审批规则；
 *  - PATCH 只发送允许的治理字段，成功后用服务端返回的完整 Asset 更新缓存；
 *  - 不得把 artifacts 的本机路径当作可直接下载的 URL。
 *
 * 四块能力对应的服务端现状：
 *  - 列表 / 类型过滤 / 元数据：GET /api/assets（真实端点）
 *  - 治理：PATCH /api/assets/{id}（真实端点）
 *  - 关联任务：控制面没有「按资产列任务」接口，由审计 task.create +
 *    GET /api/tasks/{id} 推导（见 docs/二阶段开发文档.md）
 *  - 补充导入 / 删除：控制面没有资产创建或删除路由，落地为带显著标识的
 *    本地草稿，不覆盖服务端快照、不参与治理 PATCH，界面必须说明它未写入 Go。
 * ------------------------------------------------------------------ */

/** 类型过滤的候选项：四个媒体类别（「全部」在 select 里单独一项）。 */
const CATEGORY_FILTERS: readonly AssetCategory[] = ASSET_CATEGORIES;

/** 面板里一行素材的统一视图：服务端资产与本地草稿共用同一套渲染。 */
interface AssetRow {
  asset: Asset;
  /** 草稿补充的素材走这份元数据；服务端资产为 null。 */
  draft: AssetDraft | null;
  /** 用于展示与过滤的类别（服务端资产取推断值，草稿取登记值）。 */
  categories: AssetCategory[];
  classification: AssetClassification;
  statusLabel: string;
  duration: number | null;
  resolution: string | null;
}

export interface AssetManagerProps {
  /** 外部选中变化回调（例如与 App 的跟踪任务联动）。 */
  onSelect?: (assetId: string) => void;
}

export default function AssetManager({ onSelect }: AssetManagerProps) {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState('');
  const [categoryFilter, setCategoryFilter] = useState<AssetCategory | 'all'>('all');
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [drafts, setDrafts] = useState<AssetDraft[]>(() => loadDrafts());
  const [draftInput, setDraftInput] = useState<DraftInput>(EMPTY_DRAFT_INPUT);
  const [draftErrors, setDraftErrors] = useState<string[]>([]);
  const [importText, setImportText] = useState('');
  const [showImport, setShowImport] = useState(false);

  const assetsQuery = useQuery({ queryKey: ['assets'], queryFn: api.listAssets });
  const auditQuery = useQuery({ queryKey: ['audit'], queryFn: () => api.listAudit(100) });

  /* --- 治理 PATCH：只发允许字段，成功后以服务端返回的 Asset 更新缓存 --- */
  const patch = useMutation({
    mutationFn: ({ id, values }: { id: string; values: GovernancePatch }) => api.patchAsset(id, values),
    onSuccess(updated: Asset) {
      queryClient.setQueryData<Asset[]>(['assets'], (previous) =>
        previous?.map((item) => (item.asset_id === updated.asset_id ? updated : item)));
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
    }
  });

  /* --- 关联任务：审计 task.create + GET /api/tasks/{id} 推导 --- */
  const auditLogs = auditQuery.data ?? [];
  const taskIds = useMemo(
    () => taskIdsFromAudit(auditLogs).slice(0, MAX_TASK_LOOKUPS),
    [auditLogs]
  );
  const tasksQuery = useQuery({
    queryKey: ['asset-task-index', taskIds],
    queryFn: async (): Promise<TaskFetchReport> => {
      if (taskIds.length === 0) return { tasks: [], failed: [] };
      return fetchTasksByIds(taskIds);
    },
    staleTime: 15_000
  });
  const actionCounts: AuditActionCount[] = useMemo(() => countActions(auditLogs), [auditLogs]);

  /* --- 草稿持久化 --- */
  useEffect(() => {
    saveDrafts(drafts);
  }, [drafts]);

  const takenIds = useMemo(() => new Set([
    ...(assetsQuery.data ?? []).map((item) => item.asset_id),
    ...drafts.map((item) => item.asset_id)
  ]), [assetsQuery.data, drafts]);

  /* --- 行视图：服务端资产与本地草稿统一成一种结构 --- */
  const rows = useMemo<AssetRow[]>(() => {
    const server: AssetRow[] = (assetsQuery.data ?? []).map((asset) => {
      const classification = classifyAsset(asset.artifacts);
      return {
        asset,
        draft: null,
        categories: classification.categories,
        classification,
        statusLabel: assetStatusLabel(asset.status),
        duration: null,
        resolution: null
      };
    });
    const local: AssetRow[] = drafts.map((draft) => ({
      asset: {
        asset_id: draft.asset_id,
        status: draft.status,
        agent_visible: false,
        human_approved: false,
        locked: false,
        allowed_agents: [],
        artifacts: {},
        created_at: draft.imported_at,
        updated_at: draft.imported_at
      },
      draft,
      categories: draft.categories,
      classification: { categories: [], evidence: {}, other: [] },
      statusLabel: ASSET_STATUS_LABELS[draft.status] ?? draft.status,
      duration: draft.duration_seconds,
      resolution: draft.resolution
    }));
    return [...server, ...local];
  }, [assetsQuery.data, drafts]);

  const visibleRows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return rows.filter((row) => {
      if (needle && !row.asset.asset_id.toLowerCase().includes(needle)) return false;
      if (categoryFilter === 'all') return true;
      return row.categories.includes(categoryFilter);
    });
  }, [rows, search, categoryFilter]);

  const selected = visibleRows.find((row) => row.asset.asset_id === selectedId) ?? visibleRows[0] ?? null;

  // onSelect 只用来告知外部「选中项变了」，用 ref 持有最新的回调，
  // 这样调用方每次渲染传新闭包也不会让这里反复触发。
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;
  useEffect(() => {
    if (selected && onSelectRef.current) onSelectRef.current(selected.asset.asset_id);
  }, [selected?.asset.asset_id]);

  /* --- 补充导入 --- */
  const submitDraft = useCallback((event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const result = buildDraft(draftInput, takenIds, 'manual');
    if (!result.ok) {
      setDraftErrors([result.error]);
      return;
    }
    setDrafts((previous) => [...previous, result.draft]);
    setDraftErrors([]);
    setDraftInput(EMPTY_DRAFT_INPUT);
  }, [draftInput, takenIds]);

  const submitImport = useCallback(() => {
    const report = parseDraftImport(importText, takenIds);
    setDraftErrors(report.errors);
    if (report.drafts.length > 0) {
      setDrafts((previous) => [...previous, ...report.drafts]);
      setImportText('');
      setShowImport(false);
    }
  }, [importText, takenIds]);

  const removeDraft = useCallback((assetId: string) => {
    setDrafts((previous) => previous.filter((item) => item.asset_id !== assetId));
  }, []);

  /* --- 治理操作（仅对服务端资产有意义） --- */
  const runPatch = useCallback((asset: Asset, values: GovernancePatch) => {
    patch.mutate({ id: asset.asset_id, values });
  }, [patch]);

  const patchError = patch.isError ? describeError(patch.error) : null;

  return (
    <section className="panel asset-manager" aria-labelledby="asset-manager-title">
      <div className="panel-heading">
        <div>
          <p className="eyebrow">ASSET GOVERNANCE</p>
          <h2 id="asset-manager-title">资产治理面板</h2>
        </div>
        <span className="count">
          {assetsQuery.isPending ? '读取中' : `${rows.length} 项`}
        </span>
      </div>

      <div className="asset-toolbar">
        <div>
          <label className="field-label" htmlFor="asset-search">按资产 ID 筛选</label>
          <input id="asset-search" type="search" value={search}
            onChange={(event) => setSearch(event.target.value)} placeholder="输入资产 ID" />
        </div>
        <div>
          <label className="field-label" htmlFor="asset-category">素材类型</label>
          <select id="asset-category" value={categoryFilter}
            onChange={(event) => setCategoryFilter(event.target.value as AssetCategory | 'all')}>
            <option value="all">全部类型</option>
            {CATEGORY_FILTERS.map((value) => (
              <option key={value} value={value}>{ASSET_CATEGORY_LABELS[value]}</option>
            ))}
          </select>
        </div>
      </div>

      <p className="hint">
        服务端资产的类型由 <code>artifacts</code> 键名推断（键即文件名），<strong>不是</strong>服务端字段；
        每条推断依据都在「产物记录」表格里逐行列明，可自行核对。
      </p>

      {assetsQuery.isPending && <p className="state">正在读取资产…</p>}
      {assetsQuery.isError && (
        <div className="state state-error" role="alert">
          资产读取失败：{describeError(assetsQuery.error)}
          <button type="button" onClick={() => void assetsQuery.refetch()}>重试</button>
        </div>
      )}
      {assetsQuery.isSuccess && rows.length === 0 && <p className="state">暂无资产。素材入库后会出现在这里。</p>}
      {assetsQuery.isSuccess && rows.length > 0 && visibleRows.length === 0 && (
        <p className="state">没有匹配的素材。可清空搜索词或把类型改回「全部类型」。</p>
      )}

      {visibleRows.length > 0 && (
        <ul className="asset-manager-list">
          {visibleRows.map((row) => {
            const isSelected = selected?.asset.asset_id === row.asset.asset_id;
            return (
              <li key={row.asset.asset_id}>
                <button type="button"
                  className={`asset-row ${isSelected ? 'is-selected' : ''}`}
                  aria-pressed={isSelected}
                  onClick={() => setSelectedId(row.asset.asset_id)}>
                  <span className="asset-name">
                    {row.asset.asset_id}
                    {row.draft && <span className="pill pill-draft">草稿</span>}
                  </span>
                  <span className="asset-status">{row.statusLabel}</span>
                  <small>
                    {categoryText(row.categories)}
                    {row.duration !== null ? ` · ${durationText(row.duration)}` : ''}
                    {row.resolution !== null ? ` · ${row.resolution}` : ''}
                    {!row.draft && row.classification.categories.length === 0 ? ' · 无产物记录' : ''}
                  </small>
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {selected && <AssetDetail
        row={selected}
        taskReport={tasksQuery.data}
        tasksLoading={tasksQuery.isPending}
        tasksError={tasksQuery.isError ? describeError(tasksQuery.error) : null}
        actionCounts={actionCounts}
        onPatch={runPatch}
        patchPending={patch.isPending}
        patchError={patchError}
        onRemoveDraft={() => removeDraft(selected.asset.asset_id)}
      />}

      <div className="draft-section">
        <h3>手动补充导入</h3>
        <p className="hint">
          <strong>控制面尚未提供资产创建或删除路由</strong>——routes() 只注册了 GET/PATCH /api/assets
          （control-plane/internal/api/api.go）。因此这里补充的素材是<strong>本地草稿</strong>：
          用于把尚未入库的素材先登记到面板，不覆盖服务端快照、不参与治理 PATCH，
          也不会发送到 Go。服务端补上写入口后，同一份数据即可改为真实导入。
        </p>

        <form className="draft-form" onSubmit={submitDraft}>
          <div>
            <label className="field-label" htmlFor="draft-asset-id">资产 ID</label>
            <input id="draft-asset-id" value={draftInput.asset_id} required
              onChange={(event) => setDraftInput((prev) => ({ ...prev, asset_id: event.target.value }))}
              placeholder="clip_099" />
          </div>
          <div>
            <label className="field-label" htmlFor="draft-status">状态</label>
            <select id="draft-status" value={draftInput.status}
              onChange={(event) => setDraftInput((prev) => ({ ...prev, status: event.target.value }))}>
              {ASSET_STATUS_ORDER.map((value) => (
                <option key={value} value={value}>{ASSET_STATUS_LABELS[value] ?? value}</option>
              ))}
            </select>
          </div>
          <div>
            <label className="field-label" htmlFor="draft-duration">时长（秒）</label>
            <input id="draft-duration" type="number" min="0" step="0.1"
              value={draftInput.duration}
              onChange={(event) => setDraftInput((prev) => ({ ...prev, duration: event.target.value }))}
              placeholder="35" />
          </div>
          <div>
            <label className="field-label" htmlFor="draft-resolution">分辨率</label>
            <input id="draft-resolution" value={draftInput.resolution}
              onChange={(event) => setDraftInput((prev) => ({ ...prev, resolution: event.target.value }))}
              placeholder="1920x1080" />
          </div>
          <fieldset className="draft-categories">
            <legend className="field-label">素材类型</legend>
            <div className="checkbox-row">
              {CATEGORY_FILTERS.map((category) => (
                <label key={category} className="checkbox-item">
                  <input type="checkbox" checked={draftInput.categories.includes(category)}
                    onChange={(event) => setDraftInput((prev) => ({
                      ...prev,
                      categories: event.target.checked
                        ? [...prev.categories, category]
                        : prev.categories.filter((item) => item !== category)
                    }))} />
                  <span>{ASSET_CATEGORY_LABELS[category]}</span>
                </label>
              ))}
            </div>
          </fieldset>
          <div>
            <label className="field-label" htmlFor="draft-note">备注</label>
            <input id="draft-note" value={draftInput.note}
              onChange={(event) => setDraftInput((prev) => ({ ...prev, note: event.target.value }))}
              placeholder="素材来源、待办说明" />
          </div>
          <div className="draft-form-actions">
            <button type="submit">加入草稿</button>
            <button className="secondary-button" type="button"
              onClick={() => setShowImport((value) => !value)}
              aria-expanded={showImport} aria-controls="draft-import-box">
              {showImport ? '收起批量导入' : '批量导入 JSON'}
            </button>
            {drafts.length > 0 && (
              <button className="secondary-button" type="button" onClick={() => setDrafts([])}>清空草稿</button>
            )}
          </div>
        </form>

        {draftErrors.length > 0 && (
          <div className="state state-error" role="alert">
            <strong>未导入，请先修正：</strong>
            <ul>{draftErrors.map((message, index) => <li key={index}>{message}</li>)}</ul>
          </div>
        )}

        {showImport && (
          <div className="import-box" id="draft-import-box">
            <label className="field-label" htmlFor="draft-import">
              粘贴 JSON 数组，字段与表单一致：asset_id、status、duration_seconds、resolution、categories、note
            </label>
            <textarea id="draft-import" rows={6} value={importText}
              onChange={(event) => setImportText(event.target.value)}
              placeholder={'[{"asset_id":"clip_002","categories":["video","audio"],"duration_seconds":42}]'} />
            <div className="draft-form-actions">
              <button type="button" onClick={submitImport} disabled={importText.trim() === ''}>导入</button>
            </div>
          </div>
        )}

        {drafts.length > 0 && (
          <>
            <p className="hint">草稿 {drafts.length} 条，保存在本机浏览器，刷新后仍在。</p>
            <ul className="draft-list">
              {drafts.map((draft) => (
                <li key={draft.asset_id}>
                  <div>
                    <strong>{draft.asset_id}</strong>
                    <span className="pill pill-draft">草稿</span>
                    <small>
                      {categoryText(draft.categories)} · {ASSET_STATUS_LABELS[draft.status] ?? draft.status}
                      {draft.duration_seconds !== null ? ` · ${durationText(draft.duration_seconds)}` : ''}
                      {draft.resolution !== null ? ` · ${draft.resolution}` : ''}
                      {` · 登记于 ${stamp(draft.imported_at)}`}
                    </small>
                    {draft.note && <span className="draft-note">{draft.note}</span>}
                  </div>
                  <button className="secondary-button" type="button"
                    onClick={() => removeDraft(draft.asset_id)}>删除</button>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    </section>
  );
}

/* ------------------------------------------------------------------ *
 * 素材详情
 * ------------------------------------------------------------------ */

interface AssetDetailProps {
  row: AssetRow;
  taskReport: TaskFetchReport | undefined;
  tasksLoading: boolean;
  tasksError: string | null;
  actionCounts: AuditActionCount[];
  onPatch: (asset: Asset, values: GovernancePatch) => void;
  patchPending: boolean;
  patchError: string | null;
  onRemoveDraft: () => void;
}

function AssetDetail({
  row, taskReport, tasksLoading, tasksError, actionCounts,
  onPatch, patchPending, patchError, onRemoveDraft
}: AssetDetailProps) {
  const { asset, classification, draft } = row;
  const tasks = taskReport?.tasks ?? [];
  const failures = taskReport?.failed ?? [];
  const isDraft = draft !== null;

  return (
    <div className="asset-detail">
      <div className="detail-title">
        <strong>{asset.asset_id}</strong>
        <span className="pill">{row.statusLabel}</span>
        {isDraft && <span className="pill pill-draft">本地草稿 · 未写入控制面</span>}
      </div>

      {isDraft ? (
        <>
          <dl className="detail-grid">
            <div><dt>素材类型</dt><dd>{categoryText(draft.categories)}</dd></div>
            <div><dt>时长</dt><dd>{durationText(draft.duration_seconds)}</dd></div>
            <div><dt>分辨率</dt><dd>{draft.resolution ?? '未填写'}</dd></div>
            <div><dt>状态</dt><dd>{ASSET_STATUS_LABELS[draft.status] ?? draft.status}</dd></div>
            <div><dt>登记时间</dt><dd>{stamp(draft.imported_at)}</dd></div>
            <div><dt>来源</dt><dd>{draft.source === 'manual' ? '逐条表单' : 'JSON 批量'}</dd></div>
            <div><dt>备注</dt><dd>{draft.note || '—'}</dd></div>
          </dl>
          <div className="action-row">
            <button className="secondary-button" type="button" onClick={onRemoveDraft}>删除草稿</button>
          </div>
          <p className="hint">草稿只存在于浏览器，治理与任务能力以服务端资产为准。</p>
        </>
      ) : (
        <>
          <dl className="detail-grid">
            <div><dt>素材类型（推断）</dt><dd>{categoryText(classification.categories)}</dd></div>
            <div><dt>服务端状态</dt><dd>{assetStatusLabel(asset.status)}（{asset.status}）</dd></div>
            <div><dt>Agent 可见</dt><dd>{asset.agent_visible ? '是' : '否'}</dd></div>
            <div><dt>锁定</dt><dd>{asset.locked ? '是' : '否'}</dd></div>
            <div><dt>人工批准</dt><dd>{asset.human_approved ? '是' : '否'}</dd></div>
            <div><dt>允许的角色</dt><dd>{asset.allowed_agents?.join('、') || '未指定'}</dd></div>
            <div><dt>创建时间</dt><dd>{stamp(asset.created_at)}</dd></div>
            <div><dt>更新时间</dt><dd>{stamp(asset.updated_at)}</dd></div>
          </dl>

          <div className="detail-block">
            <h4>产物记录（artifacts）</h4>
            {Object.keys(asset.artifacts ?? {}).length === 0 ? (
              <p className="state">该资产没有产物记录。</p>
            ) : (
              <table className="artifact-table">
                <thead>
                  <tr><th scope="col">键</th><th scope="col">值</th><th scope="col">推断类别</th></tr>
                </thead>
                <tbody>
                  {Object.entries(asset.artifacts ?? {}).map(([key, value]) => (
                    <tr key={key}>
                      <td><code>{key}</code></td>
                      <td><span className="artifact-path" title={value}>{value}</span></td>
                      <td>{categoryOfArtifact(key, classification)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            <p className="hint">
              产物值是服务端记录的本机路径，不是可下载 URL。下载需要服务端提供受控文件接口，
              浏览器不得直接读取服务端文件系统（docs/二阶段开发文档.md）。
            </p>
          </div>

          <div className="detail-block">
            <h4>治理操作</h4>
            <div className="action-row">
              <button type="button" disabled={patchPending}
                onClick={() => onPatch(asset, { agent_visible: !asset.agent_visible })}>
                {asset.agent_visible ? '设为 Agent 不可见' : '设为 Agent 可见'}
              </button>
              <button className="secondary-button" type="button" disabled={patchPending}
                onClick={() => onPatch(asset, { locked: !asset.locked })}>
                {asset.locked ? '解锁' : '锁定'}
              </button>
              <button className="secondary-button" type="button" disabled={patchPending}
                onClick={() => onPatch(asset, { human_approved: !asset.human_approved })}>
                {asset.human_approved ? '撤销批准' : '人工批准'}
              </button>
            </div>
            {patchError && <p className="inline-error" role="alert">更新失败：{patchError}</p>}
            <p className="hint">PATCH 只发送上述治理字段；服务端返回的完整资产会替换本地面板缓存。</p>
          </div>

          <div className="detail-block">
            <h4>关联任务</h4>
            {tasksLoading && <p className="state">正在按审计记录推导关联任务…</p>}
            {tasksError && <p className="inline-error" role="alert">关联任务读取失败：{tasksError}</p>}
            {!tasksLoading && !tasksError && tasks.length === 0 && (
              <p className="state">
                没有关联任务。控制面暂不提供「按资产列任务」接口，此列表由审计中的
                task.create 加 GET /api/tasks/{'{'}id{'}'} 推导。
              </p>
            )}
            {tasks.length > 0 && (
              <ul className="asset-task-list">
                {tasks.map((task) => (
                  <li key={task.task_id}>
                    <div className="tracked-top">
                      <code>{task.task_id}</code>
                      <span>{taskStatusLabel(task.status)}（{task.status}）</span>
                    </div>
                    <small>
                      {task.type} · {task.agent_role} · 进度 {percent(task.progress)}
                      {` · 更新于 ${stamp(task.updated_at)}`}
                    </small>
                    <progress max="1" value={safeProgress(task.progress)}
                      aria-label={`任务 ${task.task_id} 进度`} />
                  </li>
                ))}
              </ul>
            )}
            {failures.length > 0 && (
              <p className="hint">
                {failures.length} 个任务查询失败（可能已被审计保留策略清理）：
                {failures.slice(0, 5).map((item) => ` ${item.task_id}`).join('')}
                {failures.length > 5 ? ' …' : ''}
              </p>
            )}
          </div>

          {actionCounts.length > 0 && (
            <div className="detail-block">
              <h4>审计动作分布（当前页）</h4>
              <ul className="action-count-list">
                {actionCounts.slice(0, 5).map((item) => (
                  <li key={item.value}>
                    <code>{item.value}</code><span>{item.label}</span><strong>{item.count}</strong>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </div>
  );
}

/* ------------------------------------------------------------------ *
 * 纯展示辅助
 * ------------------------------------------------------------------ */

/** progress 收敛到 0–1；NaN/Infinity 不会渲染成非法 progress value。 */
function safeProgress(progress: number): number {
  if (!Number.isFinite(progress)) return 0;
  return Math.min(1, Math.max(0, progress));
}

/** 一个产物键落在哪个推断类别下，供详情表格逐行说明推断依据。 */
function categoryOfArtifact(key: string, classification: AssetClassification): string {
  for (const [category, keys] of Object.entries(classification.evidence) as [AssetCategory, string[]][]) {
    if (keys.includes(key)) return ASSET_CATEGORY_LABELS[category];
  }
  return '工程产物';
}

/** 当前审计页的动作计数，标签与 AuditLogViewer 用同一套映射。 */
function countActions(logs: readonly { action: string }[]): AuditActionCount[] {
  const counts = new Map<string, number>();
  for (const log of logs) counts.set(log.action, (counts.get(log.action) ?? 0) + 1);
  return [...counts.entries()]
    .map(([value, count]) => ({ value, label: GOVERNANCE_ACTION_LABELS[value] ?? value, count }))
    .sort((left, right) => right.count - left.count || left.value.localeCompare(right.value));
}

const GOVERNANCE_ACTION_LABELS: Record<string, string> = {
  'asset.create': '资产入库',
  'asset.governance': '治理变更',
  'asset.approve': '人工批准',
  'asset.unapprove': '撤销批准',
  'task.create': '创建任务',
  'task.claim': '领取任务',
  'task.submit': '提交结果',
  'task.fail': '任务失败',
  'task.requeue': '租约到期重排'
};

export type { AssetRow };
