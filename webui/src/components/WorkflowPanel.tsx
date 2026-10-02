import { useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import type { ReviewView, WorkflowRun } from '../types';
import { usePreview } from './Monitor';
import { Timeline, type TimelineClip } from './Timeline';
import { Icon } from './Icon';

const checks = [['content_fidelity', '内容忠实度'], ['evidence_trace', '证据追溯'], ['browser', '浏览器操作'], ['premiere', 'Premiere 导入'], ['cuts_fps', '切点与帧率'], ['subtitle_audio_duck', '字幕、音频与压低']] as const;
const errorText = (e: unknown) => e instanceof Error ? e.message : '操作失败，请刷新后重试';
const PAGE = 20;

type Scene = ReviewView['scenes'][number];
type Line = ReviewView['narration'][number];

/** 编辑页与导出页共享的流程选择（同一资产下选中的 run 与翻页位置）。 */
export interface RunSelection {
  runId: string;
  offset: number;
  setRunId: (runId: string) => void;
  setOffset: (offset: number) => void;
}

export const evidenceURL = (runId: string, key: string, revisionId: string) =>
  `/api/workflows/${encodeURIComponent(runId)}/evidence/${encodeURIComponent(key)}?revision_id=${encodeURIComponent(revisionId)}`;

/** 同一组查询键供编辑页、导出页复用，React Query 自动去重（API-28、30、31、39）。 */
function useWorkflowData(assetId: string, selection: RunSelection) {
  const runs = useQuery({ queryKey: ['workflows', assetId, selection.offset], queryFn: () => api.listWorkflows(assetId, selection.offset) });
  const run = runs.data?.find(r => r.run_id === selection.runId) ?? runs.data?.[0];
  const snapshot = useQuery({ queryKey: ['workflow', run?.run_id], queryFn: () => api.getWorkflow(run!.run_id), enabled: Boolean(run) });
  const review = useQuery({ queryKey: ['review', run?.run_id], queryFn: () => api.reviewWorkflow(run!.run_id), enabled: Boolean(run) });
  const acceptance = useQuery({ queryKey: ['acceptance', run?.run_id], queryFn: () => api.listAcceptance(run!.run_id), enabled: Boolean(run) });
  const current: WorkflowRun | undefined = review.data?.run ?? snapshot.data?.run ?? run;
  return { runs, run, snapshot, review, acceptance, current };
}

function useWorkflowAction() {
  const client = useQueryClient();
  const [failure, setFailure] = useState('');
  const refresh = async () => { await Promise.all(['workflows', 'workflow', 'review', 'acceptance', 'assets', 'audit'].map(key => client.invalidateQueries({ queryKey: [key] }))); };
  const action = useMutation({ mutationFn: async (fn: () => Promise<unknown>) => fn(), onSuccess: async () => { setFailure(''); await refresh(); }, onError: e => { setFailure(errorText(e)); void refresh(); } });
  return { invoke: (fn: () => Promise<unknown>) => action.mutate(fn), busy: action.isPending, failure };
}

function RunPicker({ runs, run, selection, busy }: { runs: WorkflowRun[] | undefined; run: WorkflowRun | undefined; selection: RunSelection; busy: boolean }) {
  return <div className="run-picker">
    <label className="field-label">流程历史 <select value={run?.run_id ?? ''} onChange={e => selection.setRunId(e.target.value)}><option value="" disabled>暂无流程</option>{runs?.map(r => <option key={r.run_id} value={r.run_id}>{r.run_id} · {r.status}</option>)}</select></label>
    <button className="secondary-button" disabled={busy || selection.offset === 0} onClick={() => { selection.setOffset(Math.max(0, selection.offset - PAGE)); selection.setRunId(''); }}>较新流程</button>
    <button className="secondary-button" disabled={busy || (runs?.length ?? 0) < PAGE} onClick={() => { selection.setOffset(selection.offset + PAGE); selection.setRunId(''); }}>较早流程</button>
  </div>;
}

const statusTone = (status?: string) => status === 'succeeded' || status === 'ready_for_acceptance' ? 'ok'
  : status === 'failed' || status === 'cancelled' ? 'bad' : status === 'running' || status === 'claimed' ? 'live' : 'idle';

/** 编辑页：内容流程、阶段、序列时间线、场景审查与解说改稿（API-28~38）。 */
export function WorkflowPanel({ assetId, selection, monitor, onGoExport }: { assetId: string; selection: RunSelection; monitor: ReactNode; onGoExport?: () => void }) {
  const [mode, setMode] = useState<'configured' | 'builtin'>('configured');
  const [note, setNote] = useState('');
  const [focus, setFocus] = useState<string | null>(null);
  const [tab, setTab] = useState<'scenes' | 'narration' | null>(null);
  const preview = usePreview();
  const { runs, run, snapshot, review, current } = useWorkflowData(assetId, selection);
  const { invoke, busy, failure } = useWorkflowAction();
  const sceneGate = current?.stage === 'scene_review' && current.status === 'awaiting_review';
  const draftGate = current?.stage === 'draft_review' && current.status === 'awaiting_review';
  const context = () => ({ expected_version: current!.version, revision_id: review.data!.revision.revision_id });
  const scenes = review.data?.scenes ?? [];
  const lines = review.data?.narration ?? [];
  const evidence = review.data?.evidence_files ?? [];
  const activeTab = tab ?? (current?.stage === 'draft_review' || (lines.length > 0 && current?.stage !== 'scene_review') ? 'narration' : 'scenes');
  const firstEvidence = (sceneId?: string) => evidence.find(f => f.scene_id === sceneId);

  const showScene = (scene: Scene, key?: string) => {
    setFocus(`scene:${scene.scene_id}`); setTab('scenes');
    const file = key ? evidence.find(f => f.key === key) : firstEvidence(scene.scene_id);
    preview(file && current && review.data ? { kind: 'image', src: evidenceURL(current.run_id, file.key, review.data.revision.revision_id), title: scene.label || scene.scene_id || '场景',
      meta: `源时间 ${file.timestamp} 秒 · ${scene.decision || '待确认'}`, note: '识别取样证据帧（绑定当前审查修订，非成片）' } : null);
  };
  const showLine = (line: Line) => {
    setFocus(`line:${line.id}`); setTab('narration');
    const scene = scenes.find(s => s.scene_id === line.source_scene_id);
    const file = firstEvidence(line.source_scene_id);
    preview(current && review.data && file ? { kind: 'image', src: evidenceURL(current.run_id, file.key, review.data.revision.revision_id), title: `解说 ${line.id}`,
      meta: `${line.start ?? '?'}s – ${line.end ?? '?'}s · 来源 ${scene?.label || line.source_scene_id || '未提供'}`, caption: line.text,
      note: '字幕叠加仅为审查示意，不代表导出成片的字幕样式' } : null);
  };

  const ordered = [...scenes].map((scene, index) => ({ scene, index })).sort((a, b) => (a.scene.sequence_rank ?? a.index) - (b.scene.sequence_rank ?? b.index));
  const timed = lines.filter(l => typeof l.start === 'number' && typeof l.end === 'number' && l.end! > l.start!);
  const seqEnd = timed.reduce((m, l) => Math.max(m, l.end!), 0);
  const sceneTone = (scene: Scene) => scene.decision === 'confirmed' ? 'ok' : scene.decision === 'rejected' ? 'bad' : 'video';
  const sceneClips: TimelineClip[] = seqEnd > 0
    ? ordered.flatMap(({ scene }) => {
      const own = timed.filter(l => l.source_scene_id === scene.scene_id);
      if (!own.length) return [];
      return [{ id: scene.scene_id ?? String(scene.label), start: Math.min(...own.map(l => l.start!)), end: Math.max(...own.map(l => l.end!)), label: scene.label || scene.scene_id || '场景',
        tone: sceneTone(scene), active: focus === `scene:${scene.scene_id}`, onSelect: () => showScene(scene), title: `${scene.label} · ${scene.decision || '待确认'}（时间窗由所属解说推得）` }];
    })
    : ordered.map(({ scene }, i) => ({ id: scene.scene_id ?? String(i), start: i, end: i + 1, label: `${i + 1}. ${scene.label || scene.scene_id}`, tone: sceneTone(scene),
      active: focus === `scene:${scene.scene_id}`, onSelect: () => showScene(scene), title: `${scene.label} · ${scene.decision || '待确认'}` }));
  const lineClips: TimelineClip[] = timed.map(line => ({ id: line.id ?? `${line.start}`, start: line.start!, end: line.end!, label: line.text || line.id || '解说',
    tone: line.approved_hash ? 'ok' : 'audio', active: focus === `line:${line.id}`, onSelect: () => showLine(line), title: `${line.start}s – ${line.end}s · ${line.approved_hash ? '已批准' : '未批准'} · ${line.text}` }));

  return <section className="panel workflow-panel" aria-labelledby="workflow-title">
    <div className="panel-heading"><div><p className="eyebrow">EDIT · REVIEW</p><h2 id="workflow-title">内容流程与审查</h2></div>
      {current && <span className={`pill pill-${statusTone(current.status)}`}>{current.status}</span>}</div>
    <div className="workflow-toolbar">
      <label className="field-label">内容模式 <select value={mode} onChange={e => setMode(e.target.value as typeof mode)}><option value="configured">已配置模型</option><option value="builtin">builtin 工程回归</option></select></label>
      <button disabled={busy} title="通用入口；固定预设流程请在「准备」页通过预检后启动" onClick={() => invoke(async () => { const created = await api.startWorkflow(assetId, crypto.randomUUID(), mode); selection.setOffset(0); selection.setRunId(created.run_id); })}>启动新流程</button>
      <RunPicker runs={runs.data} run={run} selection={selection} busy={runs.isFetching} />
    </div>
    <p className="hint">先核对画面证据，再批准解说。固定预设流程建议在「准备」页预检后启动；builtin 只是工程回归，不代表真实识别。</p>
    {busy && <p role="status">正在保存，请稍候…</p>}
    {(failure || runs.error || review.error || snapshot.error) && <p className="inline-error" role="alert">{failure || errorText(runs.error || review.error || snapshot.error)}</p>}
    {runs.isSuccess && !current && <p className="state">此资产还没有内容流程。可在「准备」页检查运行条件后启动固定预设流程。</p>}
    {current && <>
      {review.isLoading && <p role="status">读取审查内容…</p>}
      <div className="edit-stage">
        <div className="edit-stage-monitor">{monitor}</div>
        <div className="review-panel">
          <div className="review-tabs" role="tablist" aria-label="审查面板">
            <button type="button" role="tab" id="review-tab-scenes" aria-controls="review-scenes" aria-selected={activeTab === 'scenes'} className={activeTab === 'scenes' ? 'is-active' : ''} onClick={() => setTab('scenes')}>场景证据 <span className="count">{scenes.length}</span></button>
            <button type="button" role="tab" id="review-tab-narration" aria-controls="review-narration" aria-selected={activeTab === 'narration'} className={activeTab === 'narration' ? 'is-active' : ''} onClick={() => setTab('narration')}>解说草稿 <span className="count">{lines.length}</span></button>
            <span className="review-gate">{sceneGate ? '等待场景确认' : draftGate ? '等待解说批准' : current.stage}</span>
          </div>
          <div className="review-scroll" role="tabpanel" id="review-scenes" aria-labelledby="review-tab-scenes" hidden={activeTab !== 'scenes'}>
            {!scenes.length && <p className="state">识别完成后显示场景与证据。</p>}
            {review.data?.scenes?.map(scene => <article className={`workflow-card${focus === `scene:${scene.scene_id}` ? ' is-focused' : ''}`} key={scene.scene_id}>
              <h4>{scene.label} · {scene.decision || '待确认'}</h4>
              <p className="hint">置信度：{scene.confidence ?? '未提供'}；模型/方法：{scene.model_version || scene.method || '未提供'}</p>
              <div className="workflow-evidence">{evidence.filter(f => f.scene_id === scene.scene_id).map(f => <figure key={f.sha256}><button type="button" className="evidence-button" onClick={() => showScene(scene, f.key)} aria-label={`在节目监视器查看 ${scene.label} 源时间 ${f.timestamp} 秒`}><img loading="lazy" src={evidenceURL(current.run_id, f.key, review.data!.revision.revision_id)} alt={`${scene.label}，源时间 ${f.timestamp} 秒的取样画面`} /></button><figcaption>源时间 {f.timestamp} 秒</figcaption></figure>)}</div>
              <SceneEdit key={`${review.data.revision.revision_id}-${scene.scene_id}`} scene={scene} disabled={busy || !(sceneGate || current.stage === 'acceptance')} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), scene_id: scene.scene_id, ...fields }))} />
              <div className="action-row"><button disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'confirmed', note }))}>确认此场景</button><button className="secondary-button" disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'rejected', note }))}>退回场景</button></div>
            </article>)}
          </div>
          <div className="review-scroll" role="tabpanel" id="review-narration" aria-labelledby="review-tab-narration" hidden={activeTab !== 'narration'}>
            {!lines.length && <p className="state">场景确认、排序后生成草稿。</p>}
            {review.data?.narration?.map(line => <article className={`workflow-card${focus === `line:${line.id}` ? ' is-focused' : ''}`} key={line.id}>
              <DraftEdit key={`${review.data.revision.revision_id}-${line.id}`} line={line} scenes={review.data.scenes ?? []} disabled={busy || !['draft_review', 'acceptance'].includes(current.stage)} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), narration_id: line.id, ...fields }))} />
              <p className={line.approved_hash ? 'ok-text' : 'hint'}>{line.approved_hash ? '当前版本已批准' : '当前版本未批准'}；来源场景 {line.source_scene_id || '未提供'}</p>
              <div className="action-row"><button disabled={busy || !draftGate || Boolean(line.approved_hash)} onClick={() => invoke(() => api.approveNarration(current.run_id, { ...context(), narration_id: line.id! }))}>批准当前版本</button><button className="secondary-button" disabled={busy || current.status === 'cancelled'} onClick={() => invoke(() => api.revokeWorkflowNarration(current.run_id, line.id!, current.version))}>撤销并停止下游</button><button type="button" className="secondary-button" onClick={() => showLine(line)}>在监视器预览</button></div>
            </article>)}
          </div>
        </div>
      </div>
      <Timeline label="序列时间线" duration={seqEnd > 0 ? seqEnd : Math.max(1, ordered.length)} formatTick={seqEnd > 0 ? (v) => `${Math.round(v * 10) / 10}s` : undefined}
        caption={seqEnd > 0 ? `解说时间轴（秒）· 场景时间窗由所属解说推得 · 点击片段在节目监视器查看证据` : '尚无解说时间：场景按 sequence_rank 等宽显示顺序'}
        tracks={[{ id: 'V1', name: '场景', hint: '颜色：蓝=待确认 绿=已确认 红=已退回', clips: sceneClips }, { id: 'A1', name: '解说', hint: '绿=当前版本已批准', clips: lineClips }]}
        emptyText="识别完成后在此显示场景与解说" />
      <div className="edit-status">
        <dl className="detail-grid"><div><dt>状态 / 阶段</dt><dd>{current.status} / {current.stage}</dd></div><div><dt>修订</dt><dd className="mono">{current.current_revision_id}</dd></div><div><dt>内容模式</dt><dd>{current.content_mode}</dd></div><div><dt>处理预设</dt><dd>{snapshot.data?.profile_binding ? `${snapshot.data.profile_binding.profile_id} / v${snapshot.data.profile_binding.revision}` : '未绑定'}</dd></div></dl>
        {current.blocked_reason && <p className="hint">{current.blocked_reason}</p>}
        {current.content_mode === 'builtin' && <p className="hint warn-text">本流程使用模板回归，不代表模型内容识别通过。</p>}
        <div className="action-row edit-actions">
          <label className="field-label">操作原因（取消、退回时填写） <input value={note} onChange={e => setNote(e.target.value)} placeholder="取消流程须填写原因；退回场景时作为备注" /></label>
          <button className="secondary-button" disabled={busy || ['cancelled', 'ready_for_acceptance'].includes(current.status) || !note.trim()} onClick={() => invoke(() => api.cancelWorkflow(current.run_id, current.version, note))}>取消流程</button><button disabled={busy || current.status !== 'failed'} onClick={() => invoke(() => api.retryWorkflow(current.run_id, current.version, current.stage, crypto.randomUUID()))}>重试失败阶段</button>
          {current.status === 'ready_for_acceptance' && onGoExport && <button type="button" className="secondary-button" onClick={onGoExport}>前往导出与验收 <Icon name="chevron" /></button>}
        </div>
        <ol className="stage-pipeline" aria-label="阶段任务">{snapshot.data?.stages.map(s => { const task = snapshot.data?.tasks?.find(t => t.task_id === s.task_id); const pct = Math.round((task?.progress ?? 0) * 100); return <li key={s.task_id} className={`stage stage-${statusTone(task?.status)}${s.invalidated ? ' is-invalidated' : ''}`} title={`${s.task_id}${task?.message ? ' · ' + task.message : ''}`}>
          <span className="stage-name">{s.stage}</span><span className="stage-status">{task?.status || '读取中'} · {pct}%</span>
          <span className="stage-bar" aria-hidden="true"><span style={{ width: `${pct}%` }} /></span>
          <small>回收 {task?.attempts ?? 0} 次{s.invalidated ? ' · 已失效' : s.output_revision_id ? ' · 已交付' : ''}</small>
        </li>; })}</ol>
      </div>
    </>}
  </section>;
}

/** 导出页：此流程的交付 ZIP 与按修订记录的人工验收（API-39~41）。 */
export function AcceptancePanel({ assetId, selection }: { assetId: string; selection: RunSelection }) {
  const [note, setNote] = useState('');
  const { runs, run, snapshot, acceptance, current } = useWorkflowData(assetId, selection);
  const { invoke, busy, failure } = useWorkflowAction();
  const downloadSummary = () => {
    const url = URL.createObjectURL(new Blob([JSON.stringify({ run_id: current!.run_id, asset_id: assetId, content_mode: current!.content_mode, current_revision_id: current!.current_revision_id, profile_binding: snapshot.data?.profile_binding ?? null, records: acceptance.data ?? [] }, null, 2)], { type: 'application/json' }));
    const link = document.createElement('a'); link.href = url; link.download = 'acceptance-summary.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  const ready = current?.status === 'ready_for_acceptance';
  return <section className="panel acceptance-panel" aria-labelledby="acceptance-title">
    <div className="panel-heading"><div><p className="eyebrow">ACCEPTANCE</p><h2 id="acceptance-title">流程交付与人工验收</h2></div>
      {current && <span className={`pill pill-${statusTone(current.status)}`}>{current.status}</span>}</div>
    <RunPicker runs={runs.data} run={run} selection={selection} busy={runs.isFetching} />
    {(failure || runs.error || acceptance.error) && <p className="inline-error" role="alert">{failure || errorText(runs.error || acceptance.error)}</p>}
    {runs.isSuccess && !current && <p className="state">此资产还没有内容流程，暂无可验收的导出。</p>}
    {current && <>
      <div className="delivery-actions">
        {ready ? <a className="button-link" href={`/api/workflows/${encodeURIComponent(current.run_id)}/delivery.zip`}><Icon name="export" /> 下载此版本交付 ZIP</a>
          : <span className="hint">流程到达 ready_for_acceptance 后才提供此版本 ZIP（当前 {current.status} / {current.stage}）。</span>}
        <button className="secondary-button" disabled={acceptance.isLoading || Boolean(acceptance.error)} onClick={downloadSummary}>下载验收摘要</button>
      </div>
      <p className="hint">导出修订 <span className="mono">{current.current_revision_id}</span>。请完成对应检查后明确记录结果；旧修订的通过记录不会沿用到当前修订。</p>
      <label className="field-label">验收备注（记录失败时必填） <input value={note} onChange={e => setNote(e.target.value)} placeholder="保留异常时间点或问题描述" /></label>
      {busy && <p role="status">正在保存，请稍候…</p>}
      <ul className="acceptance-list">{checks.map(([item, label]) => { const row = acceptance.data?.filter(r => r.check_item === item && r.export_revision_id === current.current_revision_id).at(-1); return <li key={item} className={`acceptance-${row?.result ?? 'pending'}`}>
        <span className="acceptance-label"><strong>{label}</strong><small>{row ? `${row.result === 'passed' ? '通过' : row.result === 'failed' ? '失败' : row.result} · ${row.actor}` : '待人工验收'}</small></span>
        {(['passed', 'failed'] as const).map(result => <button key={result} className="secondary-button compact" disabled={busy || !ready || (result === 'failed' && !note.trim())} onClick={() => invoke(() => api.recordAcceptance(current.run_id, { expected_version: current.version, check_item: item, result, note }))}>{result === 'passed' ? '记录通过' : '记录失败'}</button>)}
      </li>; })}</ul>
      <details><summary>历史人工验收记录（按修订保留，{acceptance.data?.length ?? 0} 条）</summary><ul className="history-list">{acceptance.data?.map(r => <li key={r.id}>{r.created_at} · <span className="mono">{r.export_revision_id}</span> · {r.check_item}：{r.result} · {r.actor}{r.note && <p>{r.note}</p>}</li>)}</ul></details>
    </>}
  </section>;
}

function SceneEdit({ scene, disabled, save }: { scene: Scene; disabled: boolean; save: (fields: Record<string, unknown>) => void }) {
  const [label, setLabel] = useState(scene.label || ''); const [rank, setRank] = useState(String(scene.sequence_rank ?? ''));
  return <form className="action-row scene-edit" onSubmit={e => { e.preventDefault(); save({ label, ...(rank !== '' ? { sequence_rank: Number(rank) } : {}) }); }}><label className="field-label">场景标签<input required maxLength={100} value={label} onChange={e => setLabel(e.target.value)} disabled={disabled} /></label><label className="field-label narrow">顺序<input type="number" min={0} step={1} value={rank} onChange={e => setRank(e.target.value)} disabled={disabled} /></label><button disabled={disabled}>保存新修订</button></form>;
}
function DraftEdit({ line, scenes, disabled, save }: { line: Line; scenes: ReviewView['scenes']; disabled: boolean; save: (fields: Record<string, unknown>) => void }) {
  const [text, setText] = useState(line.text || ''); const [start, setStart] = useState(String(line.start)); const [end, setEnd] = useState(String(line.end));
  const [source, setSource] = useState(line.source_scene_id || '');
  return <form onSubmit={e => { e.preventDefault(); save({ text, start: Number(start), end: Number(end), source_scene_id: source }); }}><label className="field-label">解说文本<textarea required maxLength={160} value={text} onChange={e => setText(e.target.value)} disabled={disabled} /></label><div className="action-row"><label className="field-label">来源场景<select value={source} onChange={e => setSource(e.target.value)} disabled={disabled}>{scenes.map(scene => <option key={scene.scene_id} value={scene.scene_id}>{scene.label}</option>)}</select></label><label className="field-label narrow">起点（秒）<input required type="number" min={0} step="0.001" value={start} onChange={e => setStart(e.target.value)} disabled={disabled} /></label><label className="field-label narrow">终点（秒）<input required type="number" min={0} step="0.001" value={end} onChange={e => setEnd(e.target.value)} disabled={disabled} /></label><button disabled={disabled}>保存新修订并重新审查</button></div></form>;
}
