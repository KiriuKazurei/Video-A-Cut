import { useEffect, useLayoutEffect, useState } from 'react';
import { Tabs, Typography } from 'antd';
import { StageDot } from '../components/PipelineContext';
import type { Asset, WorkflowRun } from '../types';
import type { RouteState } from '../navigation';
import { EDITING_SECTIONS } from '../navigation';
import { SECTION_STAGE } from '../pipeline';
import type { RunSelection } from '../components/WorkflowPanel';
import { IngestPanel, IngestRegister } from '../components/IngestPanel';
import { DeliveryImport } from '../components/DeliveryPanel';
import { WorkflowPanel, useWorkflowData } from '../components/WorkflowPanel';
import { displayWorkflowPreview, samePreviewContext, workflowPreviewContext } from '../previewContext';
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
 * 源/节目状态独立；节目监视器与两份审查面板使用同一实际流程及修订快照。
 */
export function EditingWorkspace({ route, navigate, asset, selection, onSelect, onPreparedStarted }: EditingWorkspaceProps) {
  const { editingSection = 'import' } = route;
  const assetId = asset?.asset_id;
  const workflowData = useWorkflowData(assetId ?? '', selection);
  const owner = workflowPreviewContext(workflowData.current);

  // 导入源监视器状态：随资产切换同步清空
  const [sourcePreview, setSourcePreview] = useState<PreviewSource>(null);
  const [sourceAssetId, setSourceAssetId] = useState(assetId);
  useLayoutEffect(() => {
    setSourcePreview(null);
    setSourceAssetId(assetId);
  }, [assetId]);

  // 渲染时验证归属；effect 仅回收已失效的保存记录。
  const [programPreview, setProgramPreview] = useState<PreviewSource>(null);
  useEffect(() => {
    setProgramPreview(previous => samePreviewContext(previous?.workflowContext, owner) ? previous : null);
  }, [owner?.assetId, owner?.runId, owner?.revisionId]);
  const visibleProgramPreview = displayWorkflowPreview(programPreview, owner, workflowData.review.data);

  return (
    <div className="page-body">
      <Tabs
        activeKey={editingSection}
        onChange={(key) => navigate({ editingSection: key as typeof editingSection })}
        items={EDITING_SECTIONS.map((section) => ({
          key: section.id,
          label: <span className="section-tab-label">{section.label}<StageDot stage={SECTION_STAGE.editing[section.id]} /></span>,
          children: (
            <div className="section-content" data-nav-anchor={section.id === 'import' ? 'ingest' : section.id} tabIndex={-1}>
              {section.id === 'import' && (
                <PreviewScope onPreview={setSourcePreview}>
                  <div className="sticky-monitor">
                    <Monitor 
                      label="源" 
                      preview={sourceAssetId === assetId ? sourcePreview : null}
                      compact 
                      emptyTitle="源监视器"
                      emptyHint="切分完成后点击候选片段或时间线片段查看缩略图；短片生成后在此播放。" 
                    />
                  </div>
                  <IngestRegister onSelect={onSelect} />
                  <DeliveryImport onImported={onSelect} />
                  <IngestPanel asset={asset} />
                </PreviewScope>
              )}
              
              {section.id === 'content' && asset && (
                <PreviewScope onPreview={setProgramPreview}>
                  <WorkflowPanel 
                    key={asset.asset_id} 
                    assetId={asset.asset_id} 
                    selection={selection}
                    data={workflowData}
                    onGoExport={() => navigate({ workspace: 'export', exportSection: 'delivery' })}
                    monitor={
                      <Monitor 
                        label="节目" 
                        preview={visibleProgramPreview}
                        emptyTitle="节目监视器"
                        emptyHint="点击场景证据、时间线片段或解说的「在监视器预览」查看画面。" 
                      />
                    }
                  />
                </PreviewScope>
              )}
              
              {section.id === 'content' && !asset && (
                <Typography.Text type="secondary">请先在左侧项目面板选择资产。</Typography.Text>
              )}
              
              {section.id === 'review' && asset && (
                <PreviewScope onPreview={setProgramPreview}>
                  <WorkflowPanel 
                    key={asset.asset_id} 
                    assetId={asset.asset_id} 
                    selection={selection}
                    data={workflowData}
                    onGoExport={() => navigate({ workspace: 'export', exportSection: 'delivery' })}
                    monitor={
                      <Monitor 
                        label="节目" 
                        preview={visibleProgramPreview}
                        emptyTitle="节目监视器"
                        emptyHint="点击场景证据或解说预览画面。" 
                      />
                    }
                  />
                </PreviewScope>
              )}
              
              {section.id === 'review' && !asset && (
                <Typography.Text type="secondary">请先在左侧项目面板选择资产。</Typography.Text>
              )}
            </div>
          )
        }))}
      />
    </div>
  );
}
