import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Col, Flex, Form, Input, Row, Tabs, Tag, Typography } from 'antd';
import { useQuery, type UseMutationResult, type UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, CreateTask, Task } from '../types';
import { parseDependsOn } from '../types';
import type { RouteState } from '../navigation';
import { EXPORT_SECTIONS } from '../navigation';
import type { RunSelection } from '../components/WorkflowPanel';
import { Monitor, type PreviewSource } from '../components/Monitor';
import { AcceptancePanel, useWorkflowData } from '../components/WorkflowPanel';
import { DeliveryPanel, filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
import { PreviewPanel } from '../components/PreviewPanel';
import { DeliveryVariantPanel } from '../components/DeliveryVariantPanel';
import { isCurrentMp4, previewApi, previewFileURL, previewQueryKey } from '../previewContracts';
import { TaskOrchestrator } from '../components/TaskOrchestrator';
import { errorText, ValueSelect } from '../components/ui';

const TASK_TYPES = ['recognize', 'sort', 'narrate', 'tts', 'subtitle', 'mix', 'export', 'preview'];

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

  const playable = (files.data ?? []).filter((file) => file.playable);
  const assetId = asset?.asset_id;
  const selectedKey = assetId ? selectedFileKeyByAsset[assetId] : undefined;
  const selectedFile = playable.find((file) => file.key === selectedKey) ?? playable[0];
  const defaultFilePreview: PreviewSource = asset && selectedFile ? filePreview(asset.asset_id, selectedFile) : null;

  const [activeMp4Preview, setActiveMp4Preview] = useState<PreviewSource | null>(null);
  const { current } = useWorkflowData(asset?.asset_id ?? '', selection);

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
        onWatchTask?.(created.task_id);
      }
    });
  };

  return (
    <div className="page-body">
      <div className="page-header">
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            导出 <Typography.Text type="secondary" style={{ fontSize: 14, fontWeight: 400 }}>Export</Typography.Text>
          </Typography.Title>
          <Typography.Text type="secondary">
            {asset ? `当前资产：${asset.asset_id}` : '未选择资产'}
          </Typography.Text>
        </div>
      </div>

      <Tabs
        activeKey={exportSection}
        onChange={(key) => navigate({ exportSection: key as typeof exportSection })}
        items={EXPORT_SECTIONS.map((section) => ({
          key: section.id,
          label: section.label,
          children: (
            <div className="section-content">
              {section.id === 'delivery' && (
                <Row gutter={[16, 16]}>
                  <Col xs={24} xl={14}>
                    <Flex vertical gap={16}>
                      <Monitor 
                        label="节目" 
                        preview={preview} 
                        emptyTitle={asset ? '暂无可播放的交付文件或成片预览' : '未选择资产'}
                        emptyHint="资产登记交付产物或生成 MP4 预览后，可在监视器中进行成片检查、试听与人工验收。"
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
                <Typography.Text type="secondary">请先在左侧项目面板选择资产。</Typography.Text>
              )}
              
              {section.id === 'tasks' && (
                <Flex vertical gap={16}>
                  <Card 
                    size="small" 
                    title={<span id="task-title">任务派发</span>} 
                    aria-labelledby="task-title"
                    extra={<Tag>{asset ? asset.asset_id : '未选择资产'}</Tag>}
                  >
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
                          message="合成成片 MP4 预览请使用上方的‘生成 MP4 预览’专用功能"
                          description="通用任务派发不建议手动分派 preview。合成成片 MP4 预览依赖当前不可变导出版的 EDL、Manifest 及最终三轨音频 stems，请前往「交付」标签页的 MP4 预览面板使用专用入口。"
                        />
                      )}
                      <Flex className="task-actions" wrap gap={8} align="center">
                        <Button 
                          type="primary" 
                          htmlType="submit" 
                          disabled={!asset || taskType === 'preview'}
                          loading={createTask.isPending}
                        >
                          创建任务
                        </Button>
                        <Input.Search
                          className="task-watch-input"
                          aria-label="按任务 ID 观察已有任务"
                          placeholder="按任务 ID 观察已有任务"
                          value={manualInputTaskId}
                          onChange={(e) => setManualInputTaskId(e.target.value)}
                          onSearch={(val) => {
                            if (val.trim()) onWatchTask?.(val.trim());
                          }}
                          enterButton="观察"
                        />
                      </Flex>
                    </Form>
                    <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
                      手动创建单任务，绑定当前选中的资产；前置任务须属于同一资产。服务端按 ID 跟踪观察。
                    </Typography.Paragraph>
                    {createTask.isError && (
                      <Alert style={{ marginTop: 12 }} type="error" showIcon message={`任务创建失败：${errorText(createTask.error)}`} />
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
