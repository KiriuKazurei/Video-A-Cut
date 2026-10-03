import { useEffect, useState, type FormEvent, type ReactNode } from 'react';
import type { UseMutationResult, UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, CreateTask, Task } from '../types';
import { parseDependsOn } from '../types';
import { ApiError } from '../api';
import { Monitor, PreviewScope, type PreviewSource } from '../components/Monitor';
import { IngestPanel, IngestRegister } from '../components/IngestPanel';
import { DeliveryImport, DeliveryPanel, filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
import { PreparationPanel } from '../components/PreparationPanel';
import { AcceptancePanel, WorkflowPanel, type RunSelection } from '../components/WorkflowPanel';
import { TaskOrchestrator } from '../components/TaskOrchestrator';
import AssetManager from '../components/AssetManager';
import AuditLogViewer from '../components/AuditLogViewer';
import { kindLabel } from '../components/MediaBin';
import type { PageDef } from '../navigation';

function message(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

function stamp(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString('zh-CN');
}

/** 每页独立的监视器状态；切换资产时清空，避免把上一资产的媒体留作当前结果。 */
function usePagePreview(assetId: string | undefined): [PreviewSource, (p: PreviewSource) => void] {
  const [preview, setPreview] = useState<PreviewSource>(null);
  useEffect(() => { setPreview(null); }, [assetId]);
  return [preview, setPreview];
}

export function PageHeader({ page, children }: { page: PageDef; children?: ReactNode }) {
  return <div className="page-header">
    <div><p className="eyebrow">{page.step} · {page.english.toUpperCase()}</p><h1>{page.label}</h1><p>{page.description}</p></div>
    <div className="page-header-side">{children}<span className="api-tag" title="本页覆盖的接口编号，见 docs/frontend-api-inventory.json">{page.apis}</span></div>
  </div>;
}

function NoAsset({ text }: { text: string }) {
  return <div className="empty-page"><strong>未选择资产</strong><p>{text}</p></div>;
}

/* ---------------- 01 导入 ---------------- */

export function ImportPage({ page, asset, onSelect }: { page: PageDef; asset?: Asset; onSelect: (id: string) => void }) {
  const files = useDeliveryFiles(asset);
  const playable = files.data?.filter((file) => file.playable) ?? [];
  const first = playable.find((file) => file.mime.startsWith('video/')) ?? playable[0];
  const preview = asset && first ? filePreview(asset.asset_id, first) : null;
  return <div className="page-body">
    <PageHeader page={page} />
    <div className="split split-monitor">
      <Monitor label="源" preview={preview}
        emptyTitle={asset ? '该资产暂无可预览媒体' : '未选择资产'}
        emptyHint={!asset ? '在左侧项目面板选择资产，或在下方登记录像、导入交付包。'
          : asset.input_kind === 'raw_recording' ? '原始录像需先在「粗剪」页导入、切分并生成短片，生成后可在那里预览。'
          : '该资产尚无已登记的可播放产物。'} />
      <section className="panel asset-summary" aria-label="资产概览">
        <div className="panel-heading"><div><p className="eyebrow">CLIP</p><h2>素材概览</h2></div></div>
        {!asset && <p className="state">未选择资产。</p>}
        {asset && <dl className="detail-grid detail-grid-stack">
          <div><dt>资产 ID</dt><dd className="mono">{asset.asset_id}</dd></div>
          <div><dt>输入类型</dt><dd>{kindLabel(asset)}</dd></div>
          <div><dt>状态</dt><dd>{asset.status}</dd></div>
          <div><dt>创建时间</dt><dd>{stamp(asset.created_at)}</dd></div>
          <div><dt>产物记录</dt><dd>{Object.keys(asset.artifacts ?? {}).length} 项 · 可播放 {files.data?.filter((f) => f.playable).length ?? 0} 个</dd></div>
        </dl>}
        <p className="hint">artifacts 值是服务端记录，不是下载地址；预览只通过受控文件键读取。</p>
      </section>
    </div>
    <div className="split">
      <IngestRegister onSelect={onSelect} />
      <DeliveryImport onImported={onSelect} />
    </div>
    <AssetManager onSelect={onSelect} />
  </div>;
}

/* ---------------- 02 粗剪 ---------------- */

export function AssemblyPage({ page, asset }: { page: PageDef; asset?: Asset }) {
  const [preview, setPreview] = usePagePreview(asset?.asset_id);
  return <PreviewScope onPreview={setPreview}><div className="page-body">
    <PageHeader page={page} />
    <div className="sticky-monitor"><Monitor label="源" preview={preview} emptyTitle="源监视器"
      emptyHint="切分完成后点击候选片段或时间线片段查看缩略图；短片生成后在此播放。" /></div>
    <IngestPanel asset={asset} />
  </div></PreviewScope>;
}

/* ---------------- 03 准备 ---------------- */

export function PreparePage({ page, asset }: { page: PageDef; asset?: Asset }) {
  return <div className="page-body">
    <PageHeader page={page} />
    {asset ? <PreparationPanel key={'preparation-' + asset.asset_id} assetId={asset.asset_id} />
      : <NoAsset text="运行预检绑定具体资产，请先在左侧项目面板选择资产。" />}
  </div>;
}

/* ---------------- 04 编辑 ---------------- */

export function EditPage({ page, asset, selection, onGoExport }: { page: PageDef; asset?: Asset; selection: RunSelection; onGoExport: () => void }) {
  const [preview, setPreview] = usePagePreview(asset?.asset_id);
  useEffect(() => { setPreview(null); }, [selection.runId, setPreview]);
  return <PreviewScope onPreview={setPreview}><div className="page-body">
    <PageHeader page={page} />
    {asset ? <WorkflowPanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} onGoExport={onGoExport}
      monitor={<Monitor label="节目" preview={preview} emptyTitle="节目监视器"
        emptyHint="点击场景证据、时间线片段或解说的“在监视器预览”查看画面。" />} />
      : <NoAsset text="请先在左侧项目面板选择资产。" />}
  </div></PreviewScope>;
}

/* ---------------- 05 导出 ---------------- */

export function ExportPage({ page, asset, selection }: { page: PageDef; asset?: Asset; selection: RunSelection }) {
  const [preview, setPreview] = usePagePreview(asset?.asset_id);
  return <PreviewScope onPreview={setPreview}><div className="page-body">
    <PageHeader page={page} />
    <div className="split split-export">
      <div className="stack">
        <Monitor label="节目" preview={preview} emptyTitle={asset ? '暂无可播放的交付文件' : '未选择资产'}
          emptyHint="资产登记交付产物后，在右侧文件列表选择视频或音频进行预览、试听。" />
        {asset && <AcceptancePanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} />}
      </div>
      <DeliveryPanel asset={asset} />
    </div>
  </div></PreviewScope>;
}

/* ---------------- 06 监控 ---------------- */

const taskTypes = ['recognize', 'sort', 'narrate', 'tts', 'subtitle', 'mix', 'export', 'preview'];

export function MonitorPage({ page, asset, audit, task, watchedTaskId, createTask }: {
  page: PageDef;
  asset?: Asset;
  audit: UseQueryResult<AuditLog[]>;
  task: UseQueryResult<Task>;
  watchedTaskId: string | null;
  createTask: UseMutationResult<Task, Error, CreateTask>;
}) {
  const [taskType, setTaskType] = useState('recognize');
  const [agentRole, setAgentRole] = useState('recognizer');
  const [dependsOn, setDependsOn] = useState('');
  function submitTask(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!asset || !agentRole.trim()) return;
    createTask.mutate({
      task_id: `task_${crypto.randomUUID()}`,
      asset_id: asset.asset_id,
      type: taskType,
      agent_role: agentRole.trim(),
      depends_on: parseDependsOn(dependsOn)
    }, { onSuccess: () => setDependsOn('') });
  }
  return <div className="page-body">
    <PageHeader page={page} />
    <div className="split split-monitor-page">
      <div className="stack">
        <section className="panel" aria-labelledby="task-title">
          <div className="panel-heading"><div><p className="eyebrow">DISPATCH</p><h2 id="task-title">任务派发</h2></div>
            <span className="pill">{asset ? asset.asset_id : '未选择资产'}</span></div>
          <form className="task-form" onSubmit={submitTask}>
            <div><label className="field-label" htmlFor="task-type">任务类型</label>
              <select id="task-type" value={taskType} onChange={(event) => setTaskType(event.target.value)}>
                {taskTypes.map((value) => <option key={value} value={value}>{value}</option>)}
              </select></div>
            <div><label className="field-label" htmlFor="agent-role">Agent 角色</label>
              <input id="agent-role" value={agentRole} required onChange={(event) => setAgentRole(event.target.value)} /></div>
            <div><label className="field-label" htmlFor="depends-on">前置任务（可选）</label>
              <input id="depends-on" value={dependsOn} placeholder="task_id，多个用逗号分隔"
                aria-describedby="depends-on-hint" onChange={(event) => setDependsOn(event.target.value)} /></div>
            <button type="submit" disabled={!asset || createTask.isPending}>{createTask.isPending ? '提交中…' : '创建任务'}</button>
          </form>
          <p className="hint" id="depends-on-hint">手动创建单任务，不是启动完整内容流程的替代入口。任务绑定当前选中的资产；前置任务须属于同一资产。服务端没有任务列表、单任务取消/重试/暂停接口，这里按 ID 跟踪。</p>
          {createTask.isError && <p className="inline-error" role="alert">任务创建失败：{message(createTask.error)}</p>}
        </section>
        <TaskOrchestrator taskId={watchedTaskId} task={task.data}
          isPending={task.isPending} isError={task.isError} error={task.error} />
      </div>
      <section className="panel audit-panel" aria-labelledby="audit-title">
        <div className="panel-heading"><div><p className="eyebrow">HISTORY</p><h2 id="audit-title">最近审计</h2></div>
          <span className="count">最近 100 条</span></div>
        {audit.isPending && <p className="state">正在读取审计记录…</p>}
        {audit.isError && <div className="state state-error" role="alert">审计读取失败：{message(audit.error)}
          <button type="button" onClick={() => void audit.refetch()}>重试</button></div>}
        {audit.isSuccess && audit.data.length === 0 && <p className="state">暂无审计记录。</p>}
        <ol className="audit-list">{audit.data?.map((entry) => <li key={entry.id}>
          <time dateTime={entry.created_at}>{stamp(entry.created_at)}</time>
          <strong>{entry.action}</strong><span>{entry.target}</span><small>{entry.actor}</small>
        </li>)}</ol>
      </section>
    </div>
    <AuditLogViewer />
  </div>;
}
