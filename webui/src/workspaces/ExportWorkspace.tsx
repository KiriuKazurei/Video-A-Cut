import { useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Card, Col, Collapse, Empty, Flex, Form, Input, List, Row, Tabs, Tag, Typography } from 'antd';
import { useQuery, type UseMutationResult, type UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, CreateTask, Task } from '../types';
import { parseDependsOn } from '../types';
import type { RouteState } from '../navigation';
import { EXPORT_SECTIONS } from '../navigation';
import { SECTION_STAGE } from '../pipeline';
import { StageDot } from '../components/PipelineContext';
import type { RunSelection } from '../components/WorkflowPanel';
import { Monitor, type PreviewSource } from '../components/Monitor';
import { AcceptancePanel, useWorkflowData } from '../components/WorkflowPanel';
import { DeliveryPanel, filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
import { PreviewPanel } from '../components/PreviewPanel';
import { NoAssetEmpty } from '../components/NoAssetEmpty';
import { DeliveryVariantPanel } from '../components/DeliveryVariantPanel';
import { isCurrentMp4, previewApi, previewFileURL, previewQueryKey } from '../previewContracts';
import { TaskOrchestrator } from '../components/TaskOrchestrator';
import { ScriptDeliveryPanel } from '../components/ScriptDeliveryPanel';
import { errorText, ValueSelect } from '../components/ui';
import { fetchTasksByIds } from '../api';
import { MAX_TASK_LOOKUPS, taskIdsFromAudit, taskStatusLabel } from '../components/governanceModel';

const TASK_TYPES = ['recognize', 'sort', 'narrate', 'tts', 'subtitle', 'mix', 'export', 'preview'];

const taskTagColor = (status?: string) =>
  status === 'succeeded' ? 'success'
    : status === 'failed' || status === 'cancelled' ? 'error'
      : status === 'running' || status === 'claimed' ? 'processing'
        : status === 'queued' || status === 'paused' ? 'warning' : 'default';

const shortId = (id: string) => (id.length <= 16 ? id : `${id.slice(0, 10)}…${id.slice(-4)}`);

interface ExportWorkspaceProps {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
  asset?: Asset;
  selection: RunSelection;
  audit: UseQueryResult<AuditLog[], Error>;
  task: UseQueryResult<Task, Error>;
  watchedTaskId: string | null;
  onWatchTask?: (taskId: string) => void;
  createTask: UseMutationResult<Task, Error, CreateTask, unknown>;
}

/**
 * 导出工作区：交付 / 人工验收 / 任务监控
 * P1-03 修复：恢复完整任务派发表单与任务监控可达入口
 */
export function ExportWorkspace({ route, navigate, asset, selection, audit, task, watchedTaskId, onWatchTask, createTask }: ExportWorkspaceProps) {
  const { exportSection = 'delivery' } = route;
  const [selectedFileKeyByAsset, setSelectedFileKeyByAsset] = useState<Record<string, string>>({});
  const files = useDeliveryFiles(asset);

  // 任务派发表单状态
  const [taskType, setTaskType] = useState('recognize');
  const [agentRole, setAgentRole] = useState('recognizer');
  const [dependsOn, setDependsOn] = useState('');
  const [manualInputTaskId, setManualInputTaskId] = useState('');
  /** 本会话创建成功的任务 ID（无按资产列任务 API 时的补充来源）。 */
  const [sessionTaskIds, setSessionTaskIds] = useState<string[]>([]);

  const playable = (files.data ?? []).filter((file) => file.playable);
  const assetId = asset?.asset_id;
  const selectedKey = assetId ? selectedFileKeyByAsset[assetId] : undefined;
  const selectedFile = playable.find((file) => file.key === selectedKey) ?? playable[0];
  const defaultFilePreview: PreviewSource = asset && selectedFile ? filePreview(asset.asset_id, selectedFile) : null;

  const [activeMp4Preview, setActiveMp4Preview] = useState<PreviewSource | null>(null);
  const { current, snapshot } = useWorkflowData(asset?.asset_id ?? '', selection);

  const owner = { assetId: assetId ?? '', runId: current?.run_id ?? '', revisionId: current?.current_revision_id ?? '' };
  const mp4s = useQuery({
    queryKey: previewQueryKey(owner.assetId, owner.runId, owner.revisionId),
    queryFn: () => previewApi.list(owner.runId),
    enabled: Boolean(owner.assetId && owner.runId && owner.revisionId)
  });
  const selectedMp4 = mp4s.data?.previews.find(view => previewFileURL(view.preview_id, 'mp4') === activeMp4Preview?.src);
  const validMp4 = activeMp4Preview && selectedMp4 && isCurrentMp4(selectedMp4, owner) ? activeMp4Preview : null;

  // 切换资产或切换 revision 时清理过期媒体播放，避免旧版本的成片残留播放
  const prevAssetRef = useRef<string | undefined>(assetId);
  const prevRevisionRef = useRef<string | undefined>(current?.current_revision_id);
  const prevRunRef = useRef<string | undefined>(current?.run_id);
  useEffect(() => {
    if (prevAssetRef.current !== assetId || prevRevisionRef.current !== current?.current_revision_id || prevRunRef.current !== current?.run_id) {
      prevAssetRef.current = assetId;
      prevRevisionRef.current = current?.current_revision_id;
      prevRunRef.current = current?.run_id;
      setActiveMp4Preview(null);
    }
  }, [assetId, current?.run_id, current?.current_revision_id]);

  useEffect(() => {
    if (activeMp4Preview && !validMp4) setActiveMp4Preview(null);
  }, [activeMp4Preview, validMp4]);

  useEffect(() => {
    return () => {
      setActiveMp4Preview(null);
    };
  }, []);

  // 已知任务 ID：流程快照 tasks + 审计 task.create + 本会话创建 + 当前观察
  const workflowTaskIds = useMemo(
    () => (snapshot.data?.tasks ?? []).map((t) => t.task_id).filter(Boolean),
    [snapshot.data?.tasks]
  );
  const auditTaskIds = useMemo(
    () => taskIdsFromAudit(audit.data ?? []).slice(0, MAX_TASK_LOOKUPS),
    [audit.data]
  );
  const lookupIds = useMemo(() => {
    const ids = [...workflowTaskIds, ...auditTaskIds, ...sessionTaskIds];
    if (watchedTaskId) ids.push(watchedTaskId);
    return [...new Set(ids.map((id) => id.trim()).filter(Boolean))];
  }, [workflowTaskIds, auditTaskIds, sessionTaskIds, watchedTaskId]);

  const linkedTasks = useQuery({
    queryKey: ['export-task-pick', assetId, lookupIds.join('\0')],
    queryFn: async () => {
      if (lookupIds.length === 0) return { tasks: [] as Awaited<ReturnType<typeof fetchTasksByIds>>['tasks'], failed: [] as Awaited<ReturnType<typeof fetchTasksByIds>>['failed'] };
      return fetchTasksByIds(lookupIds);
    },
    enabled: Boolean(assetId) && lookupIds.length > 0,
    staleTime: 10_000
  });

  const pickList = useMemo(() => {
    const byId = new Map<string, { task_id: string; asset_id: string; status: string; progress: number; type: string; agent_role: string; updated_at: string; source: string }>();
    for (const t of snapshot.data?.tasks ?? []) {
      if (!assetId || t.asset_id === assetId || !t.asset_id) {
        byId.set(t.task_id, {
          task_id: t.task_id,
          asset_id: t.asset_id || assetId || '',
          status: t.status,
          progress: t.progress,
          type: t.type,
          agent_role: t.agent_role,
          updated_at: t.updated_at,
          source: '流程'
        });
      }
    }
    for (const t of linkedTasks.data?.tasks ?? []) {
      if (assetId && t.asset_id !== assetId) continue;
      const prev = byId.get(t.task_id);
      if (!prev || Date.parse(t.updated_at) >= Date.parse(prev.updated_at)) {
        byId.set(t.task_id, { ...t, source: prev?.source === '流程' ? '流程' : '审计' });
      }
    }
    return [...byId.values()].sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at));
  }, [snapshot.data?.tasks, linkedTasks.data?.tasks, assetId]);

  const preview: PreviewSource = validMp4 ?? defaultFilePreview;

  const handleSelectFile = (key: string) => {
    if (assetId) {
      setSelectedFileKeyByAsset((prev) => ({ ...prev, [assetId]: key }));
      setActiveMp4Preview(null);
    }
  };

  const handleCreateTask = () => {
    if (!asset || !agentRole.trim()) return;
    createTask.mutate({
      task_id: `task_${crypto.randomUUID()}`,
      asset_id: asset.asset_id,
      type: taskType,
      agent_role: agentRole.trim(),
      depends_on: parseDependsOn(dependsOn)
    }, {
      onSuccess: (created) => {
        setDependsOn('');
        setSessionTaskIds((prev) => (prev.includes(created.task_id) ? prev : [created.task_id, ...prev]));
        onWatchTask?.(created.task_id);
      }
    });
  };

  return (
    <div className="page-body">
      <Tabs
        activeKey={exportSection}
        onChange={(key) => navigate({ exportSection: key as typeof exportSection })}
        items={EXPORT_SECTIONS.map((section) => ({
          key: section.id,
          label: <span className="section-tab-label">{section.label}<StageDot stage={SECTION_STAGE.export[section.id]} /></span>,
          children: (
            <div className="section-content" data-nav-anchor={section.id} tabIndex={-1}>
              {section.id === 'delivery' && (
                <Row gutter={[24, 24]}>
                  {asset && <Col span={24}><ScriptDeliveryPanel key={asset.asset_id} assetId={asset.asset_id} active={route.workspace === 'export' && exportSection === 'delivery'} /></Col>}
                  <Col xs={24} xl={14}>
                    <Flex vertical gap={16}>
                      <Monitor 
                        label="节目" 
                        preview={preview} 
                        emptyTitle={asset ? '还没有可预览的成片' : '请先选择素材'}
                        emptyHint="生成或导入成片后，可在这里预览、试听，再到「人工验收」勾选通过。"
                      />
                      <PreviewPanel
                        asset={asset}
                        selection={selection}
                        onSelectPreview={setActiveMp4Preview}
                        activePreviewSrc={activeMp4Preview?.src}
                      />
                      {asset && <AcceptancePanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} />}
                      <DeliveryVariantPanel asset={asset} selection={selection} onSelectPreview={setActiveMp4Preview} />
                    </Flex>
                  </Col>
                  <Col xs={24} xl={10}>
                    <DeliveryPanel 
                      asset={asset} 
                      files={files} 
                      selectedFile={selectedFile} 
                      onSelectFile={handleSelectFile} 
                    />
                  </Col>
                </Row>
              )}
              
              {section.id === 'acceptance' && asset && (
                <AcceptancePanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} />
              )}
              
              {section.id === 'acceptance' && !asset && (
                <NoAssetEmpty navigate={navigate} description="还没有选中素材，没有可验收的成片。" />
              )}
              
              {section.id === 'tasks' && (
                <Flex vertical gap={16}>
                  <Card 
                    size="small" 
                    title={<span id="task-title">任务</span>} 
                    aria-labelledby="task-title"
                    extra={<Tag>{asset ? asset.asset_id : '未选择素材'}</Tag>}
                  >
                    <Alert type="info" showIcon style={{ marginBottom: 12 }}
                      message="一般跟着顶部「下一步」走即可"
                      description="启动流程后，相关任务会出现在下方。多数情况不必手动创建。" />
                    <Form layout="vertical" onFinish={handleCreateTask}>
                      <Row gutter={12}>
                        <Col xs={24} md={6}>
                          <Form.Item label="任务类型">
                            <ValueSelect<string> 
                              id="task-type" 
                              aria-label="任务类型" 
                              value={taskType} 
                              onChange={setTaskType}
                              options={TASK_TYPES.map((value) => ({ value, label: value }))} 
                            />
                          </Form.Item>
                        </Col>
                        <Col xs={24} md={6}>
                          <Form.Item label="Agent 角色">
                            <Input 
                              id="agent-role" 
                              aria-label="Agent 角色" 
                              value={agentRole} 
                              required 
                              onChange={(e) => setAgentRole(e.target.value)} 
                            />
                          </Form.Item>
                        </Col>
                        <Col xs={24} md={12}>
                          <Form.Item label="前置任务（可选）">
                            <Input 
                              id="depends-on" 
                              aria-label="前置任务" 
                              value={dependsOn} 
                              placeholder="task_id，多个用逗号分隔"
                              onChange={(e) => setDependsOn(e.target.value)} 
                            />
                          </Form.Item>
                        </Col>
                      </Row>
                      {taskType === 'preview' && (
                        <Alert
                          style={{ marginBottom: 12 }}
                          type="info"
                          showIcon
                          message="成片预览请用「交付」页的专用按钮"
                          description="不要在这里手动创建 preview 任务。请到「交付」里使用「生成 MP4 预览」。"
                        />
                      )}
                      <Flex className="task-actions" wrap gap={8} align="center">
                        <Button 
                          type="primary" 
                          className="vac-cta"
                          htmlType="submit" 
                          disabled={!asset || taskType === 'preview'}
                          loading={createTask.isPending}
                        >
                          创建任务
                        </Button>
                        <Input.Search
                          className="task-watch-input"
                          aria-label="按任务 ID 观察已有任务"
                          placeholder="用任务编号查看进度（可选）"
                          value={manualInputTaskId}
                          onChange={(e) => setManualInputTaskId(e.target.value)}
                          onSearch={(val) => {
                            if (val.trim()) onWatchTask?.(val.trim());
                          }}
                          enterButton="查看"
                        />
                      </Flex>
                    </Form>
                    <Collapse size="small" style={{ marginTop: 12 }} items={[{
                      key: 'tech', label: '详细说明（技术人员）',
                      children: <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
                        控制面暂无「按资产列任务」接口；列表由流程快照、操作记录中的 task.create 与本会话创建合并。也可用任务编号查看进度。
                      </Typography.Paragraph>
                    }]} />
                    {createTask.isError && (
                      <Alert style={{ marginTop: 12 }} type="error" showIcon message={`任务创建失败：${errorText(createTask.error)}`} />
                    )}
                  </Card>

                  <Card size="small" className="task-pick-list" title="当前相关任务" extra={<Typography.Text type="secondary" style={{ fontSize: 12 }}>{asset ? `${pickList.length} 条` : '未选资产'}</Typography.Text>}>
                    {!asset && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请先在左侧选择素材。" />}
                    {asset && linkedTasks.isFetching && pickList.length === 0 && <Typography.Text type="secondary">正在读取任务列表…</Typography.Text>}
                    {asset && !linkedTasks.isFetching && pickList.length === 0 && (
                      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有任务。通常在「运行准备」启动流程后会出现在这里。" />
                    )}
                    {asset && pickList.length > 0 && (
                      <List
                        size="small"
                        className="task-pick-rows"
                        dataSource={pickList}
                        renderItem={(row) => {
                          const active = row.task_id === watchedTaskId;
                          return (
                            <List.Item
                              className={`task-pick-row${active ? ' is-active' : ''}`}
                              onClick={() => onWatchTask?.(row.task_id)}
                              actions={[
                                <Button key="watch" type={active ? 'primary' : 'link'} size="small" onClick={(e) => { e.stopPropagation(); onWatchTask?.(row.task_id); }}>
                                  {active ? '查看中' : '查看进度'}
                                </Button>
                              ]}
                            >
                              <List.Item.Meta
                                title={
                                  <Flex gap={8} align="center" wrap>
                                    <Typography.Text code style={{ fontSize: 12 }}>{shortId(row.task_id)}</Typography.Text>
                                    <Tag color={taskTagColor(row.status)}>{taskStatusLabel(row.status)}</Tag>
                                    <Tag bordered={false}>{row.type}</Tag>
                                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>{row.source}</Typography.Text>
                                  </Flex>
                                }
                                description={
                                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                                    {row.agent_role} · {Math.round((row.progress ?? 0) * 100)}% · {row.updated_at}
                                    <span title={row.task_id}> · {row.task_id}</span>
                                  </Typography.Text>
                                }
                              />
                            </List.Item>
                          );
                        }}
                      />
                    )}
                  </Card>

                  <TaskOrchestrator 
                    taskId={watchedTaskId}
                    task={task.data}
                    isPending={task.isPending}
                    isError={task.isError}
                    error={task.error}
                  />
                </Flex>
              )}
            </div>
          )
        }))}
      />
    </div>
  );
}
