import { useEffect, useLayoutEffect, useState } from 'react';
import { Alert, Button, Card, Tabs } from 'antd';
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
import { PreparationPanel } from '../components/PreparationPanel';
import { NoAssetEmpty } from '../components/NoAssetEmpty';

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
  // 「开始生成」入口放在导入页底部：从别处跳来时切到导入页并滚动、聚焦到它
  const goStart = () => {
    navigate({ workspace: 'editing', editingSection: 'import' });
    let tries = 0;
    const attempt = () => {
      const el = [...document.querySelectorAll<HTMLElement>('.page:not([hidden]) [data-nav-anchor="quickstart-card"]')].find((n) => n.offsetParent !== null);
      if (!el) { if (++tries < 15) requestAnimationFrame(attempt); return; }
      // 吸顶的源监视器与位置条会盖住上沿：滚到它们的下沿，而不是视口顶
      const scroller = el.closest<HTMLElement>('.stage-main');
      const pinned = document.querySelector<HTMLElement>('.page:not([hidden]) .sticky-monitor') ?? document.querySelector<HTMLElement>('.context-bar');
      const behavior = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth';
      if (scroller && pinned && getComputedStyle(pinned).position === 'sticky') {
        scroller.scrollTo({ top: scroller.scrollTop + el.getBoundingClientRect().top - pinned.getBoundingClientRect().bottom - 8, behavior });
      } else {
        el.scrollIntoView({ block: 'start', behavior });
      }
      el.focus({ preventScroll: true });
    };
    requestAnimationFrame(attempt);
  };
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
                      emptyHint="单击时间线或缩略图预览；双击时间线片段加入/移出已选；短片生成后点击卡片在此播放。" 
                    />
                  </div>
                  {asset && <Alert type="info" showIcon className="start-banner" message="素材导入好了？"
                    description="在页面最下方的「开始生成」里检查条件并一键开始；也可以先在上方选好段落再开始。"
                    action={<Button type="primary" className="vac-cta" onClick={goStart}>去开始生成</Button>} />}
                  <IngestRegister onSelect={onSelect} />
                  <DeliveryImport onImported={onSelect} />
                  <IngestPanel asset={asset} />
                  {asset && (
                    <Card size="small" className="quickstart-card" title="开始生成" data-nav-anchor="quickstart-card" tabIndex={-1}
                      extra={<span className="quickstart-extra">自动完成识别、撰稿、配音与字幕，随后进入审查</span>}>
                      <PreparationPanel assetId={asset.asset_id} activeSection="quickstart" onStarted={onPreparedStarted} onNavigate={navigate} />
                    </Card>
                  )}
                </PreviewScope>
              )}
              
              {section.id === 'content' && asset && (
                <PreviewScope onPreview={setProgramPreview}>
                  <WorkflowPanel 
                    mode="content"
                    onGoStart={goStart}
                    onGoReview={() => navigate({ workspace: 'editing', editingSection: 'review' })}
                    key={asset.asset_id} 
                    assetId={asset.asset_id} 
                    selection={selection}
                    data={workflowData}
                    onGoExport={() => navigate({ workspace: 'export', exportSection: 'delivery' })}
                    onGoTasks={() => navigate({ workspace: 'export', exportSection: 'tasks' })}
                    monitor={
                      <Monitor 
                        label="节目" 
                        preview={visibleProgramPreview}
                        emptyTitle="节目监视器"
                        emptyHint="单击时间线片段或卡片预览画面；证据缩略图可点进节目监视器。" 
                      />
                    }
                  />
                </PreviewScope>
              )}
              
              {section.id === 'content' && !asset && (
                <NoAssetEmpty navigate={navigate} />
              )}
              
              {section.id === 'review' && asset && (
                <PreviewScope onPreview={setProgramPreview}>
                  <WorkflowPanel 
                    mode="review"
                    onGoStart={goStart}
                    key={asset.asset_id} 
                    assetId={asset.asset_id} 
                    selection={selection}
                    data={workflowData}
                    onGoExport={() => navigate({ workspace: 'export', exportSection: 'delivery' })}
                    onGoTasks={() => navigate({ workspace: 'export', exportSection: 'tasks' })}
                    monitor={
                      <Monitor 
                        label="节目" 
                        preview={visibleProgramPreview}
                        emptyTitle="节目监视器"
                        emptyHint="单击时间线片段或审查卡片预览画面。" 
                      />
                    }
                  />
                </PreviewScope>
              )}
              
              {section.id === 'review' && !asset && (
                <NoAssetEmpty navigate={navigate} />
              )}
            </div>
          )
        }))}
      />
    </div>
  );
}
