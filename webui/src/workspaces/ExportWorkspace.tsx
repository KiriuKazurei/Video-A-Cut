import { useState } from 'react';
import { Col, Flex, Row, Tabs, Typography } from 'antd';
import type { UseMutationResult, UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, CreateTask, Task } from '../types';
import type { RouteState } from '../navigation';
import { EXPORT_SECTIONS } from '../navigation';
import type { RunSelection } from '../components/WorkflowPanel';
import { Monitor, type PreviewSource } from '../components/Monitor';
import { AcceptancePanel } from '../components/WorkflowPanel';
import { DeliveryPanel, filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
import { TaskOrchestrator } from '../components/TaskOrchestrator';

interface ExportWorkspaceProps {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
  asset?: Asset;
  selection: RunSelection;
  audit: UseQueryResult<AuditLog[], Error>;
  task: UseQueryResult<Task, Error>;
  watchedTaskId: string | null;
  createTask: UseMutationResult<Task, Error, CreateTask, unknown>;
}

/**
 * 导出工作区：交付 / 人工验收 / 任务监控
 */
export function ExportWorkspace({ route, navigate, asset, selection, audit, task, watchedTaskId, createTask }: ExportWorkspaceProps) {
  const { exportSection = 'delivery' } = route;
  const [selectedFileKeyByAsset, setSelectedFileKeyByAsset] = useState<Record<string, string>>({});
  const files = useDeliveryFiles(asset);

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
                <TaskOrchestrator 
                  taskId={watchedTaskId}
                  task={task.data}
                  isPending={task.isPending}
                  isError={task.isError}
                  error={task.error}
                />
              )}
            </div>
          )
        }))}
      />
    </div>
  );
}
