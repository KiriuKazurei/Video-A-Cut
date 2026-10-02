import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '../api';
import type { ReviewView } from '../types';
const checks = [['content_fidelity', '内容忠实度'], ['evidence_trace', '证据追溯'], ['browser', '浏览器操作'], ['premiere', 'Premiere 导入'], ['cuts_fps', '切点与帧率'], ['subtitle_audio_duck', '字幕、音频与压低']] as const;
const errorText = (e: unknown) => e instanceof Error ? e.message : '操作失败，请刷新后重试';
export function WorkflowPanel({ assetId }: { assetId: string }) {
  const client = useQueryClient();
  const [mode, setMode] = useState<'configured' | 'builtin'>('configured');
  const [selected, setSelected] = useState(''); const [note, setNote] = useState(''); const [failure, setFailure] = useState('');
  const [offset,setOffset]=useState(0);
  const runs = useQuery({ queryKey: ['workflows', assetId,offset], queryFn: () => api.listWorkflows(assetId,offset) });
  const run = runs.data?.find(r => r.run_id === selected) ?? runs.data?.[0];
  const snapshot = useQuery({ queryKey: ['workflow', run?.run_id], queryFn: () => api.getWorkflow(run!.run_id), enabled: Boolean(run) });
  const review = useQuery({ queryKey: ['review', run?.run_id], queryFn: () => api.reviewWorkflow(run!.run_id), enabled: Boolean(run) });
  const acceptance = useQuery({ queryKey: ['acceptance', run?.run_id], queryFn: () => api.listAcceptance(run!.run_id), enabled: Boolean(run) });
  const current = review.data?.run ?? snapshot.data?.run ?? run;
  const refresh = async () => { await Promise.all(['workflows', 'workflow', 'review', 'acceptance', 'assets', 'audit'].map(key => client.invalidateQueries({ queryKey: [key] }))); };
  const action = useMutation({ mutationFn: async (fn: () => Promise<unknown>) => fn(), onSuccess: async () => { setFailure(''); await refresh(); }, onError: e => { setFailure(errorText(e)); void refresh(); } });
  const invoke = (fn: () => Promise<unknown>) => action.mutate(fn); const busy = action.isPending;
  const sceneGate = current?.stage === 'scene_review' && current.status === 'awaiting_review';
  const draftGate = current?.stage === 'draft_review' && current.status === 'awaiting_review';
  const context = () => ({ expected_version: current!.version, revision_id: review.data!.revision.revision_id });
  const downloadSummary = () => {
    const url = URL.createObjectURL(new Blob([JSON.stringify({ run_id: current!.run_id, asset_id: assetId, content_mode: current!.content_mode, current_revision_id: current!.current_revision_id, profile_binding:snapshot.data?.profile_binding??null, records: acceptance.data ?? [] }, null, 2)], { type: 'application/json' }));
    const link = document.createElement('a'); link.href = url; link.download = 'acceptance-summary.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return <section className="panel" aria-labelledby="workflow-title">
    <div className="panel-heading"><div><p className="eyebrow">WORKFLOW</p><h2 id="workflow-title">审查与交付流程</h2></div></div>
    <p className="hint">先核对画面证据，再批准解说。工程运行成功后仍需人工内容和 Premiere 验收。</p>
    <div className="action-row">
      <label className="field-label">内容模式 <select value={mode} onChange={e => setMode(e.target.value as typeof mode)}><option value="configured">已配置模型</option><option value="builtin">builtin 工程回归</option></select></label>
      <button disabled={busy} onClick={() => invoke(async () => { const created = await api.startWorkflow(assetId, crypto.randomUUID(), mode); setOffset(0);setSelected(created.run_id); })}>启动新流程</button>
      <label className="field-label">流程历史 <select value={run?.run_id ?? ''} onChange={e => setSelected(e.target.value)}><option value="" disabled>暂无流程</option>{runs.data?.map(r => <option key={r.run_id} value={r.run_id}>{r.run_id} · {r.status}</option>)}</select></label>
    </div>
    <div className="action-row"><button className="secondary-button" disabled={offset===0||runs.isFetching} onClick={()=>{setOffset(Math.max(0,offset-20));setSelected('')}}>较新流程</button><button className="secondary-button" disabled={(runs.data?.length??0)<20||runs.isFetching} onClick={()=>{setOffset(offset+20);setSelected('')}}>较早流程</button></div>
    {busy && <p role="status">正在保存，请稍候…</p>}
    {(failure || runs.error || review.error || snapshot.error || acceptance.error) && <p className="inline-error" role="alert">{failure || errorText(runs.error || review.error || snapshot.error || acceptance.error)}</p>}
    {current && <>
      <dl className="detail-grid"><div><dt>状态 / 阶段</dt><dd>{current.status} / {current.stage}</dd></div><div><dt>修订</dt><dd>{current.current_revision_id}</dd></div><div><dt>内容模式</dt><dd>{current.content_mode}</dd></div></dl>
      {current.blocked_reason && <p className="hint">{current.blocked_reason}</p>}
      {snapshot.data?.profile_binding && <p>处理预设：{snapshot.data.profile_binding.profile_id} / v{snapshot.data.profile_binding.revision}</p>}
      {current.content_mode === 'builtin' && <p className="hint">本流程使用模板回归，不代表模型内容识别通过。</p>}
      <label className="field-label">操作原因或验收备注 <input value={note} onChange={e => setNote(e.target.value)} placeholder="取消、退回或验收时填写原因" /></label>
      <div className="action-row"><button className="secondary-button" disabled={busy || ['cancelled', 'ready_for_acceptance'].includes(current.status) || !note.trim()} onClick={() => invoke(() => api.cancelWorkflow(current.run_id, current.version, note))}>取消流程</button><button disabled={busy || current.status !== 'failed'} onClick={() => invoke(() => api.retryWorkflow(current.run_id, current.version, current.stage, crypto.randomUUID()))}>重试失败阶段</button>{current.status === 'ready_for_acceptance' && <a href={`/api/workflows/${encodeURIComponent(current.run_id)}/delivery.zip`}>下载此版本交付 ZIP</a>}</div>
      <h3>阶段任务</h3><ol>{snapshot.data?.stages.map(s => {const task=snapshot.data?.tasks?.find(t=>t.task_id===s.task_id);return <li key={s.task_id}>{s.stage}：{s.task_id} · {task?.status || '读取中'} · {Math.round((task?.progress??0)*100)}% · 回收 {task?.attempts??0} 次 {s.invalidated ? '（已失效）' : s.output_revision_id ? '（已交付）' : ''}{task?.message&&<p className="hint">{task.message}</p>}</li>})}</ol>
      {review.isLoading && <p role="status">读取审查内容…</p>}
      <h3>场景证据</h3>{!review.data?.scenes?.length && <p className="state">识别完成后显示场景与证据。</p>}
      {review.data?.scenes?.map(scene => <article className="workflow-card" key={scene.scene_id}>
        <h4>{scene.label} · {scene.decision || '待确认'}</h4>
        <p>置信度：{scene.confidence ?? '未提供'}；模型/方法：{scene.model_version || scene.method || '未提供'}</p>
        <div className="workflow-evidence">{(review.data!.evidence_files??[]).filter(f => f.scene_id === scene.scene_id).map(f => <figure key={f.sha256}><img loading="lazy" src={`/api/workflows/${encodeURIComponent(current.run_id)}/evidence/${encodeURIComponent(f.key)}?revision_id=${encodeURIComponent(review.data!.revision.revision_id)}`} alt={`${scene.label}，源时间 ${f.timestamp} 秒的取样画面`} /><figcaption>源时间 {f.timestamp} 秒</figcaption></figure>)}</div>
        <SceneEdit key={`${review.data.revision.revision_id}-${scene.scene_id}`} scene={scene} disabled={busy || !(sceneGate || current.stage === 'acceptance')} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), scene_id: scene.scene_id, ...fields }))} />
        <div className="action-row"><button disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'confirmed', note }))}>确认此场景</button><button className="secondary-button" disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'rejected', note }))}>退回场景</button></div>
      </article>)}
      <h3>解说草稿</h3>{!review.data?.narration?.length && <p className="state">场景确认、排序后生成草稿。</p>}
      {review.data?.narration?.map(line => <article className="workflow-card" key={line.id}>
        <DraftEdit key={`${review.data.revision.revision_id}-${line.id}`} line={line} scenes={review.data.scenes??[]} disabled={busy || !['draft_review', 'acceptance'].includes(current.stage)} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), narration_id: line.id, ...fields }))} />
        <p>{line.approved_hash ? '当前版本已批准' : '当前版本未批准'}；来源场景 {line.source_scene_id || '未提供'}</p>
        <div className="action-row"><button disabled={busy || !draftGate || Boolean(line.approved_hash)} onClick={() => invoke(() => api.approveNarration(current.run_id, { ...context(), narration_id: line.id! }))}>批准当前版本</button><button className="secondary-button" disabled={busy || current.status === 'cancelled'} onClick={() => invoke(() => api.revokeWorkflowNarration(current.run_id, line.id!, current.version))}>撤销并停止下游</button></div>
      </article>)}
      <h3>人工验收记录</h3><p className="hint">请完成对应检查后明确记录结果；备注保留异常时间点。</p>
      <button className="secondary-button" disabled={acceptance.isLoading || Boolean(acceptance.error)} onClick={downloadSummary}>下载验收摘要</button>
      {checks.map(([item, label]) => { const row = acceptance.data?.filter(r => r.check_item === item && r.export_revision_id === current.current_revision_id).at(-1); return <div className="action-row" key={item}><span>{label}：{row?.result || '待人工验收'}</span>{(['passed', 'failed'] as const).map(result => <button key={result} className="secondary-button" disabled={busy || current.status !== 'ready_for_acceptance' || (result === 'failed' && !note.trim())} onClick={() => invoke(() => api.recordAcceptance(current.run_id, { expected_version: current.version, check_item: item, result, note }))}>{result === 'passed' ? '记录通过' : '记录失败'}</button>)}</div>; })}
      <details><summary>历史人工验收记录（按修订保留）</summary><ul>{acceptance.data?.map(r => <li key={r.id}>{r.created_at} · {r.export_revision_id} · {r.check_item}：{r.result} · {r.actor}{r.note && <p>{r.note}</p>}</li>)}</ul></details>
    </>}
  </section>;
}
function SceneEdit({ scene, disabled, save }: { scene: ReviewView['scenes'][number]; disabled: boolean; save: (fields: Record<string, unknown>) => void }) {
  const [label, setLabel] = useState(scene.label || ''); const [rank, setRank] = useState(String(scene.sequence_rank ?? ''));
  return <form className="action-row" onSubmit={e => { e.preventDefault(); save({ label, ...(rank !== '' ? { sequence_rank: Number(rank) } : {}) }); }}><label className="field-label">场景标签<input required maxLength={100} value={label} onChange={e => setLabel(e.target.value)} disabled={disabled} /></label><label className="field-label">顺序<input type="number" min={0} step={1} value={rank} onChange={e => setRank(e.target.value)} disabled={disabled} /></label><button disabled={disabled}>保存新修订</button></form>;
}
function DraftEdit({ line, scenes, disabled, save }: { line: ReviewView['narration'][number]; scenes:ReviewView['scenes']; disabled: boolean; save: (fields: Record<string, unknown>) => void }) {
  const [text, setText] = useState(line.text || ''); const [start, setStart] = useState(String(line.start)); const [end, setEnd] = useState(String(line.end));
  const [source,setSource]=useState(line.source_scene_id||'');
  return <form onSubmit={e => { e.preventDefault(); save({ text, start: Number(start), end: Number(end), source_scene_id:source }); }}><label className="field-label">解说文本<textarea required maxLength={160} value={text} onChange={e => setText(e.target.value)} disabled={disabled} /></label><div className="action-row"><label className="field-label">来源场景<select value={source} onChange={e=>setSource(e.target.value)} disabled={disabled}>{scenes.map(scene=><option key={scene.scene_id} value={scene.scene_id}>{scene.label}</option>)}</select></label><label className="field-label">起点（秒）<input required type="number" min={0} step="0.001" value={start} onChange={e => setStart(e.target.value)} disabled={disabled} /></label><label className="field-label">终点（秒）<input required type="number" min={0} step="0.001" value={end} onChange={e => setEnd(e.target.value)} disabled={disabled} /></label><button disabled={disabled}>保存新修订并重新审查</button></div></form>;
}
