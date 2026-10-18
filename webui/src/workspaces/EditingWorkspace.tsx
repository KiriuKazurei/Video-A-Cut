import { useEffect, useLayoutEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Segmented, Tabs } from 'antd';
import { api } from '../api';
import { enterScriptAllowed, startGenerationAllowed } from '../visibleSet';
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
import { AutoRunProgress, AutoRunStart, useAutoView } from '../components/AutoRunPanel';
import { autoPolling } from '../autoRun';

type StartMode = 'manual' | 'auto';

/** 「开始生成」卡：分步生成（预设 + 预检 + 开始）与自动模式（一句话 + 全局授权 + 一键）并存；当前流程是自动模式时先展示进度。 */
function StartCard({ asset, run, selection, navigate, onPreparedStarted }: {
  asset: Asset;
  run?: WorkflowRun;
  selection: RunSelection;
  navigate: (state: Partial<RouteState>) => void;
  onPreparedStarted: (run: WorkflowRun) => void;
}) {
  const [mode, setMode] = useState<StartMode>('manual');
  const auto = useAutoView(run?.run_id);
  const visibleSet = useQuery({ queryKey: ['visible-set'], queryFn: api.visibleSet });
  const live = autoPolling(auto.data?.state);
  const canGenerate = visibleSet.isSuccess && startGenerationAllowed(visibleSet.data.confirmed);
  // 自动流程已结束（完成/停止/撤销/取消）：默认只看结果，需要时再展开「重新生成」。
  const ended = Boolean(auto.data) && !live;
  const [showForm, setShowForm] = useState(false);
  useEffect(() => { setShowForm(false); }, [run?.run_id]);
  const goReview = () => navigate({ workspace: 'editing', editingSection: 'review' });
  return (
    <Card size="small" className="quickstart-card" title="开始生成" data-nav-anchor="quickstart-card" tabIndex={-1}
      extra={<span className="quickstart-extra">{mode === 'auto' ? '写一句要求，Agent 自动剪辑并导出' : '自动完成识别、撰稿、配音与字幕，随后进入审查'}</span>}>
      <AutoRunProgress run={run} onGoReview={goReview} />
      {ended && !showForm && <Button type="link" className="auto-restart" style={{ paddingInline: 0, marginTop: 8 }} onClick={() => setShowForm(true)}>重新生成…</Button>}
      {!live && (!ended || showForm) && !visibleSet.isSuccess && (
        <Alert type={visibleSet.isError ? 'error' : 'info'} showIcon style={{ marginTop: 12 }}
          message={visibleSet.isError ? '暂时读不到可见素材的确认状态' : '正在读取可见素材的确认状态'}
          action={visibleSet.isError ? <Button size="small" onClick={() => void visibleSet.refetch()}>重试</Button> : undefined} />
      )}
      {!live && (!ended || showForm) && visibleSet.isSuccess && !canGenerate && (
        <Alert type="warning" showIcon style={{ marginTop: 12 }} message="请先确认 Agent 可见素材"
          description="手动和自动模式在进入脚本或开始生成前，都要先确认当前可见集。"
          action={<Button type="primary" onClick={() => navigate({ workspace: 'resources' })}>去资源页确认</Button>} />
      )}
      {!live && (!ended || showForm) && canGenerate && (
        <div className={auto.data ? 'auto-after-progress' : undefined}>
          <Segmented<StartMode> className="start-mode" aria-label="生成方式" value={mode} onChange={setMode}
            options={[{ value: 'manual', label: '分步生成' }, { value: 'auto', label: '自动模式' }]} />
          <div className="start-mode-body">
            {mode === 'auto'
              ? <AutoRunStart assetId={asset.asset_id} onGoSettings={() => navigate({ workspace: 'settings', settingsCategory: 'profiles' })}
                  onStarted={(r) => { selection.setOffset(0); selection.setRunId(r.run_id); }} />
              : <PreparationPanel assetId={asset.asset_id} activeSection="quickstart" onStarted={onPreparedStarted} onNavigate={navigate} />}
          </div>
        </div>
      )}
    </Card>
  );
}

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
  const visibleSet = useQuery({ queryKey: ['visible-set'], queryFn: api.visibleSet });
  const scriptOpen = Boolean(asset) && enterScriptAllowed({
    confirmed: visibleSet.isSuccess && visibleSet.data.confirmed,
    assetVisible: Boolean(asset?.agent_visible),
    hasWorkflow: Boolean(workflowData.current)
  });

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
                  {asset && <StartCard asset={asset} run={workflowData.current} selection={selection} navigate={navigate} onPreparedStarted={onPreparedStarted} />}
                </PreviewScope>
              )}
              
              {section.id === 'content' && asset && !scriptOpen && (
                <Alert type="warning" showIcon message="还不能进入脚本"
                  description={visibleSet.isPending ? '正在读取可见素材的确认状态。' : '请先在资源页确认当前 Agent 可见素材。已在进行的流程不受这次确认影响。'}
                  action={<Button type="primary" onClick={() => navigate({ workspace: 'resources' })}>去资源页确认</Button>} />
              )}
              {section.id === 'content' && asset && scriptOpen && (
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
