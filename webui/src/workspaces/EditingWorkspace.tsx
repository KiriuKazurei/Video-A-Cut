import { useState } from 'react';
import { Tabs, Typography } from 'antd';
import type { Asset, WorkflowRun } from '../types';
import type { RouteState } from '../navigation';
import { EDITING_SECTIONS } from '../navigation';
import type { RunSelection } from '../components/WorkflowPanel';
import { IngestPanel, IngestRegister } from '../components/IngestPanel';
import { DeliveryImport } from '../components/DeliveryPanel';
import { WorkflowPanel } from '../components/WorkflowPanel';
import { Monitor, PreviewScope, type PreviewSource } from '../components/Monitor';

interface EditingWorkspaceProps {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
  asset?: Asset;
  selection: RunSelection;
  onSelect: (id: string) => void;
  onPreparedStarted: (run: WorkflowRun) => void;
}

/**
 * 编辑工作区：导入与粗剪 / 内容编辑 / 审查
 */
export function EditingWorkspace({ route, navigate, asset, selection, onSelect, onPreparedStarted }: EditingWorkspaceProps) {
  const { editingSection = 'import' } = route;
  const [preview, setPreview] = useState<PreviewSource>(null);

  return (
    <PreviewScope onPreview={setPreview}>
      <div className="page-body">
        <div className="page-header">
          <div>
            <Typography.Title level={4} style={{ margin: 0 }}>
              编辑 <Typography.Text type="secondary" style={{ fontSize: 14, fontWeight: 400 }}>Editing</Typography.Text>
            </Typography.Title>
            <Typography.Text type="secondary">
              {asset ? `当前资产：${asset.asset_id}` : '未选择资产'}
            </Typography.Text>
          </div>
        </div>

        <Tabs
          activeKey={editingSection}
          onChange={(key) => navigate({ editingSection: key as typeof editingSection })}
          items={EDITING_SECTIONS.map((section) => ({
            key: section.id,
            label: section.label,
            children: (
              <div className="section-content">
                {section.id === 'import' && (
                  <>
                    <div className="sticky-monitor">
                      <Monitor 
                        label="源" 
                        preview={preview} 
                        compact 
                        emptyTitle="源监视器"
                        emptyHint="切分完成后点击候选片段或时间线片段查看缩略图；短片生成后在此播放。" 
                      />
                    </div>
                    <IngestRegister onSelect={onSelect} />
                    <DeliveryImport onImported={onSelect} />
                    <IngestPanel asset={asset} />
                  </>
                )}
                
                {section.id === 'content' && asset && (
                  <WorkflowPanel 
                    key={asset.asset_id} 
                    assetId={asset.asset_id} 
                    selection={selection}
                    onGoExport={() => navigate({ workspace: 'export', exportSection: 'delivery' })}
                    monitor={
                      <Monitor 
                        label="节目" 
                        preview={preview} 
                        emptyTitle="节目监视器"
                        emptyHint="点击场景证据、时间线片段或解说的「在监视器预览」查看画面。" 
                      />
                    }
                  />
                )}
                
                {section.id === 'content' && !asset && (
                  <Typography.Text type="secondary">请先在左侧项目面板选择资产。</Typography.Text>
                )}
                
                {section.id === 'review' && asset && (
                  <WorkflowPanel 
                    key={asset.asset_id} 
                    assetId={asset.asset_id} 
                    selection={selection}
                    onGoExport={() => navigate({ workspace: 'export', exportSection: 'delivery' })}
                    monitor={
                      <Monitor 
                        label="节目" 
                        preview={preview} 
                        emptyTitle="节目监视器"
                        emptyHint="点击场景证据或解说预览画面。" 
                      />
                    }
                  />
                )}
                
                {section.id === 'review' && !asset && (
                  <Typography.Text type="secondary">请先在左侧项目面板选择资产。</Typography.Text>
                )}
              </div>
            )
          }))}
        />
      </div>
    </PreviewScope>
  );
}
