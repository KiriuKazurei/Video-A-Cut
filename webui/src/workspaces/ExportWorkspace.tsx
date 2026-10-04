import { useState } from 'react';
import { Alert, Button, Card, Col, Flex, Form, Input, Row, Space, Tabs, Tag, Typography } from 'antd';
import type { UseMutationResult, UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, CreateTask, Task } from '../types';
import { parseDependsOn } from '../types';
import type { RouteState } from '../navigation';
import { EXPORT_SECTIONS } from '../navigation';
import type { RunSelection } from '../components/WorkflowPanel';
import { Monitor, type PreviewSource } from '../components/Monitor';
import { AcceptancePanel } from '../components/WorkflowPanel';
import { DeliveryPanel, filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
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
  const preview: PreviewSource = asset && selectedFile ? filePreview(asset.asset_id, selectedFile) : null;

  const handleSelectFile = (key: string) => {
    if (assetId) {
      setSelectedFileKeyByAsset((prev) => ({ ...prev, [assetId]: key }));
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
                        emptyTitle={asset ? '暂无可播放的交付文件' : '未选择资产'}
                        emptyHint="资产登记交付产物后，在右侧文件列表选择视频或音频进行预览、试听。" 
                      />
                      {asset && <AcceptancePanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} />}
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
                      <Space>
                        <Button 
                          type="primary" 
                          htmlType="submit" 
                          disabled={!asset} 
                          loading={createTask.isPending}
                        >
                          创建任务
                        </Button>
                        <Input.Search
                          placeholder="按任务 ID 观察已有任务"
                          style={{ width: 280 }}
                          value={manualInputTaskId}
                          onChange={(e) => setManualInputTaskId(e.target.value)}
                          onSearch={(val) => {
                            if (val.trim()) onWatchTask?.(val.trim());
                          }}
                          enterButton="观察"
                        />
                      </Space>
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
