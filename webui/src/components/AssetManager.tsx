import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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
import { Alert, Button, Card, Checkbox, Col, Descriptions, Empty, Flex, Form, Input, InputNumber, List, Progress, Row, Space, Table, Tag, Typography } from 'antd';
import { ValueSelect } from './ui';

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
  const submitDraft = useCallback(() => {
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

  const { Text } = Typography;
  return (
    <Card size="small" className="asset-manager" aria-labelledby="asset-manager-title" title={<span id="asset-manager-title">资产治理面板</span>}
      extra={<Tag>{assetsQuery.isPending ? '读取中' : `${rows.length} 项`}</Tag>}>
      <Flex vertical gap={12}>
        <Row gutter={12} className="asset-toolbar">
          <Col xs={24} md={14}><Form.Item label="按资产 ID 筛选" htmlFor="asset-search" layout="vertical" style={{ marginBottom: 0 }}>
            <Input.Search id="asset-search" allowClear value={search} onChange={(event) => setSearch(event.target.value)} placeholder="输入资产 ID" />
          </Form.Item></Col>
          <Col xs={24} md={10}><Form.Item label="素材类型" htmlFor="asset-category" layout="vertical" style={{ marginBottom: 0 }}>
            <ValueSelect<AssetCategory | 'all'> id="asset-category" value={categoryFilter} onChange={setCategoryFilter}
              options={[{ value: 'all', label: '全部类型' }, ...CATEGORY_FILTERS.map((value) => ({ value, label: ASSET_CATEGORY_LABELS[value] }))]} />
          </Form.Item></Col>
        </Row>
        <Text type="secondary">服务端资产的类型由 <Text code>artifacts</Text> 键名推断（键即文件名），<Text strong>不是</Text>服务端字段；每条推断依据都在「产物记录」表格里逐行列明，可自行核对。</Text>

        {assetsQuery.isPending && <Text type="secondary">正在读取资产…</Text>}
        {assetsQuery.isError && <Alert type="error" showIcon role="alert" message={`资产读取失败：${describeError(assetsQuery.error)}`}
          action={<Button size="small" onClick={() => void assetsQuery.refetch()}>重试</Button>} />}
        {assetsQuery.isSuccess && rows.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无资产。素材入库后会出现在这里。" />}
        {assetsQuery.isSuccess && rows.length > 0 && visibleRows.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的素材。可清空搜索词或把类型改回「全部类型」。" />}

        {visibleRows.length > 0 && <Table<AssetRow> size="small" className="asset-manager-list" rowKey={(row) => row.asset.asset_id} dataSource={visibleRows}
          pagination={{ pageSize: 8, hideOnSinglePage: true, showSizeChanger: false }}
          rowClassName={(row) => selected?.asset.asset_id === row.asset.asset_id ? 'ant-table-row-selected' : ''}
          onRow={(row) => ({ onClick: () => setSelectedId(row.asset.asset_id), style: { cursor: 'pointer' } })}
          columns={[
            { key: 'id', title: '资产 ID', render: (_, row) => <Button type="link" size="small" style={{ padding: 0 }} aria-pressed={selected?.asset.asset_id === row.asset.asset_id}
              onClick={(event) => { event.stopPropagation(); setSelectedId(row.asset.asset_id); }}>{row.asset.asset_id}</Button> },
            { key: 'status', title: '状态', render: (_, row) => <Space size={4}>{row.statusLabel}{row.draft && <Tag color="warning">草稿</Tag>}</Space> },
            { key: 'kind', title: '类型', render: (_, row) => <Text type="secondary">{categoryText(row.categories)}
              {row.duration !== null ? ` · ${durationText(row.duration)}` : ''}{row.resolution !== null ? ` · ${row.resolution}` : ''}
              {!row.draft && row.classification.categories.length === 0 ? ' · 无产物记录' : ''}</Text> }
          ]} />}

        {selected && <AssetDetail row={selected} taskReport={tasksQuery.data} tasksLoading={tasksQuery.isPending}
          tasksError={tasksQuery.isError ? describeError(tasksQuery.error) : null} actionCounts={actionCounts}
          onPatch={runPatch} patchPending={patch.isPending} patchError={patchError} onRemoveDraft={() => removeDraft(selected.asset.asset_id)} />}

        <Card size="small" type="inner" className="draft-section" title="手动补充导入">
          <Typography.Paragraph type="secondary">
            <Text strong>控制面尚未提供资产创建或删除路由</Text>——routes() 只注册了 GET/PATCH /api/assets（control-plane/internal/api/api.go）。
            因此这里补充的素材是<Text strong>本地草稿</Text>：用于把尚未入库的素材先登记到面板，不覆盖服务端快照、不参与治理 PATCH，
            也不会发送到 Go。服务端补上写入口后，同一份数据即可改为真实导入。
          </Typography.Paragraph>
          <Form layout="vertical" className="draft-form" onFinish={submitDraft}>
            <Row gutter={12}>
              <Col xs={24} md={8}><Form.Item label="资产 ID" htmlFor="draft-asset-id">
                <Input id="draft-asset-id" required value={draftInput.asset_id} onChange={(event) => setDraftInput((prev) => ({ ...prev, asset_id: event.target.value }))} placeholder="clip_099" />
              </Form.Item></Col>
              <Col xs={24} md={8}><Form.Item label="状态" htmlFor="draft-status">
                <ValueSelect<string> id="draft-status" value={draftInput.status} onChange={(status) => setDraftInput((prev) => ({ ...prev, status }))}
                  options={ASSET_STATUS_ORDER.map((value) => ({ value, label: ASSET_STATUS_LABELS[value] ?? value }))} />
              </Form.Item></Col>
              <Col xs={12} md={4}><Form.Item label="时长（秒）" htmlFor="draft-duration">
                <InputNumber id="draft-duration" style={{ width: '100%' }} min={0} step={0.1} placeholder="35"
                  value={draftInput.duration === '' ? null : Number(draftInput.duration)} onChange={(value) => setDraftInput((prev) => ({ ...prev, duration: value === null ? '' : String(value) }))} />
              </Form.Item></Col>
              <Col xs={12} md={4}><Form.Item label="分辨率" htmlFor="draft-resolution">
                <Input id="draft-resolution" value={draftInput.resolution} onChange={(event) => setDraftInput((prev) => ({ ...prev, resolution: event.target.value }))} placeholder="1920x1080" />
              </Form.Item></Col>
              <Col xs={24} md={12}><Form.Item label="素材类型">
                <Checkbox.Group value={draftInput.categories} onChange={(values) => setDraftInput((prev) => ({ ...prev, categories: values as AssetCategory[] }))}
                  options={CATEGORY_FILTERS.map((category) => ({ value: category, label: ASSET_CATEGORY_LABELS[category] }))} />
              </Form.Item></Col>
              <Col xs={24} md={12}><Form.Item label="备注" htmlFor="draft-note">
                <Input id="draft-note" value={draftInput.note} onChange={(event) => setDraftInput((prev) => ({ ...prev, note: event.target.value }))} placeholder="素材来源、待办说明" />
              </Form.Item></Col>
            </Row>
            <Space wrap>
              <Button type="primary" htmlType="submit">加入草稿</Button>
              <Button onClick={() => setShowImport((value) => !value)} aria-expanded={showImport} aria-controls="draft-import-box">{showImport ? '收起批量导入' : '批量导入 JSON'}</Button>
              {drafts.length > 0 && <Button danger onClick={() => setDrafts([])}>清空草稿</Button>}
            </Space>
          </Form>
          {draftErrors.length > 0 && <Alert style={{ marginTop: 12 }} type="error" showIcon role="alert" message="未导入，请先修正："
            description={<ul style={{ margin: 0, paddingLeft: 18 }}>{draftErrors.map((message, index) => <li key={index}>{message}</li>)}</ul>} />}
          {showImport && <Form layout="vertical" id="draft-import-box" style={{ marginTop: 12 }}>
            <Form.Item label="粘贴 JSON 数组，字段与表单一致：asset_id、status、duration_seconds、resolution、categories、note" htmlFor="draft-import">
              <Input.TextArea id="draft-import" rows={6} value={importText} onChange={(event) => setImportText(event.target.value)}
                placeholder={'[{"asset_id":"clip_002","categories":["video","audio"],"duration_seconds":42}]'} />
            </Form.Item>
            <Button type="primary" onClick={submitImport} disabled={importText.trim() === ''}>导入</Button>
          </Form>}
          {drafts.length > 0 && <>
            <Typography.Paragraph type="secondary" style={{ margin: '12px 0 4px' }}>草稿 {drafts.length} 条，保存在本机浏览器，刷新后仍在。</Typography.Paragraph>
            <List size="small" className="draft-list" dataSource={drafts} renderItem={(draft) => <List.Item key={draft.asset_id}
              actions={[<Button key="remove" size="small" onClick={() => removeDraft(draft.asset_id)}>删除</Button>]}>
              <List.Item.Meta title={<Space size={6}>{draft.asset_id}<Tag color="warning">草稿</Tag></Space>}
                description={<>{categoryText(draft.categories)} · {ASSET_STATUS_LABELS[draft.status] ?? draft.status}
                  {draft.duration_seconds !== null ? ` · ${durationText(draft.duration_seconds)}` : ''}
                  {draft.resolution !== null ? ` · ${draft.resolution}` : ''}{` · 登记于 ${stamp(draft.imported_at)}`}
                  {draft.note && <><br />{draft.note}</>}</>} />
            </List.Item>} />
          </>}
        </Card>
      </Flex>
    </Card>
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
  const { Text } = Typography;
  const yes = (value: boolean) => value ? '是' : '否';

  return (
    <Card size="small" type="inner" className="asset-detail" title={<Space size={6} wrap>{asset.asset_id}<Tag>{row.statusLabel}</Tag>
      {isDraft && <Tag color="warning">本地草稿 · 未写入控制面</Tag>}</Space>}>
      {isDraft ? <Flex vertical gap={12}>
        <Descriptions size="small" column={{ xs: 1, md: 2 }} items={[
          { key: 'kind', label: '素材类型', children: categoryText(draft.categories) },
          { key: 'duration', label: '时长', children: durationText(draft.duration_seconds) },
          { key: 'resolution', label: '分辨率', children: draft.resolution ?? '未填写' },
          { key: 'status', label: '状态', children: ASSET_STATUS_LABELS[draft.status] ?? draft.status },
          { key: 'at', label: '登记时间', children: stamp(draft.imported_at) },
          { key: 'source', label: '来源', children: draft.source === 'manual' ? '逐条表单' : 'JSON 批量' },
          { key: 'note', label: '备注', children: draft.note || '—' }
        ]} />
        <Space><Button onClick={onRemoveDraft}>删除草稿</Button></Space>
        <Text type="secondary">草稿只存在于浏览器，治理与任务能力以服务端资产为准。</Text>
      </Flex> : <Flex vertical gap={16}>
        <Descriptions size="small" column={{ xs: 1, md: 2 }} items={[
          { key: 'kind', label: '素材类型（推断）', children: categoryText(classification.categories) },
          { key: 'status', label: '服务端状态', children: `${assetStatusLabel(asset.status)}（${asset.status}）` },
          { key: 'visible', label: 'Agent 可见', children: yes(asset.agent_visible) },
          { key: 'locked', label: '锁定', children: yes(asset.locked) },
          { key: 'approved', label: '人工批准', children: yes(asset.human_approved) },
          { key: 'agents', label: '允许的角色', children: asset.allowed_agents?.join('、') || '未指定' },
          { key: 'created', label: '创建时间', children: stamp(asset.created_at) },
          { key: 'updated', label: '更新时间', children: stamp(asset.updated_at) }
        ]} />

        <div>
          <Typography.Title level={5}>产物记录（artifacts）</Typography.Title>
          {Object.keys(asset.artifacts ?? {}).length === 0 ? <Text type="secondary">该资产没有产物记录。</Text>
            : <Table size="small" className="artifact-table" pagination={false} rowKey="key" scroll={{ x: 520 }}
              dataSource={Object.entries(asset.artifacts ?? {}).map(([key, value]) => ({ key, value }))}
              columns={[
                { key: 'k', title: '键', render: (_, item) => <Text code>{item.key}</Text> },
                { key: 'v', title: '值', render: (_, item) => <Text type="secondary" style={{ overflowWrap: 'anywhere' }} title={item.value}>{item.value}</Text> },
                { key: 'c', title: '推断类别', width: 96, render: (_, item) => categoryOfArtifact(item.key, classification) }
              ]} />}
          <Text type="secondary">产物值是服务端记录的本机路径，不是可下载 URL。下载需要服务端提供受控文件接口，浏览器不得直接读取服务端文件系统（docs/二阶段开发文档.md）。</Text>
        </div>

        <div>
          <Typography.Title level={5}>治理操作</Typography.Title>
          <Space wrap>
            <Button type="primary" disabled={patchPending} onClick={() => onPatch(asset, { agent_visible: !asset.agent_visible })}>{asset.agent_visible ? '设为 Agent 不可见' : '设为 Agent 可见'}</Button>
            <Button disabled={patchPending} onClick={() => onPatch(asset, { locked: !asset.locked })}>{asset.locked ? '解锁' : '锁定'}</Button>
            <Button disabled={patchPending} onClick={() => onPatch(asset, { human_approved: !asset.human_approved })}>{asset.human_approved ? '撤销批准' : '人工批准'}</Button>
          </Space>
          {patchError && <Alert style={{ marginTop: 8 }} type="error" showIcon role="alert" message={`更新失败：${patchError}`} />}
          <div><Text type="secondary">PATCH 只发送上述治理字段；服务端返回的完整资产会替换本地面板缓存。</Text></div>
        </div>

        <div>
          <Typography.Title level={5}>关联任务</Typography.Title>
          {tasksLoading && <Text type="secondary">正在按审计记录推导关联任务…</Text>}
          {tasksError && <Alert type="error" showIcon role="alert" message={`关联任务读取失败：${tasksError}`} />}
          {!tasksLoading && !tasksError && tasks.length === 0 && <Text type="secondary">没有关联任务。控制面暂不提供「按资产列任务」接口，此列表由审计中的 task.create 加 GET /api/tasks/{'{'}id{'}'} 推导。</Text>}
          {tasks.length > 0 && <List size="small" className="asset-task-list" dataSource={tasks} renderItem={(task) => <List.Item key={task.task_id}>
            <Flex vertical gap={4} style={{ width: '100%' }}>
              <Flex justify="space-between" gap={8} wrap><Text code>{task.task_id}</Text><Text>{taskStatusLabel(task.status)}（{task.status}）</Text></Flex>
              <Text type="secondary">{task.type} · {task.agent_role} · 进度 {percent(task.progress)}{` · 更新于 ${stamp(task.updated_at)}`}</Text>
              <Progress size="small" percent={Math.round(safeProgress(task.progress) * 100)} showInfo={false} aria-label={`任务 ${task.task_id} 进度`} />
            </Flex>
          </List.Item>} />}
          {failures.length > 0 && <Text type="secondary">{failures.length} 个任务查询失败（可能已被审计保留策略清理）：{failures.slice(0, 5).map((item) => ` ${item.task_id}`).join('')}{failures.length > 5 ? ' …' : ''}</Text>}
        </div>

        {actionCounts.length > 0 && <div>
          <Typography.Title level={5}>审计动作分布（当前页）</Typography.Title>
          <Flex wrap gap={6} className="action-count-list">{actionCounts.slice(0, 5).map((item) => <Tag key={item.value} title={item.value}>{item.label} <Text strong>{item.count}</Text></Tag>)}</Flex>
        </div>}
      </Flex>}
    </Card>
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
