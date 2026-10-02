import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError, ingestApi, ingestFileURL } from '../api';
import type { Asset } from '../types';
import {
  ACTIVE_STATES, clockToUs, displayProgress, executionBanner, selectionProblem, usToClock, splitSelection, mergeSelection,
  type IngestRunView, type IngestPolicy, type SegmentItem, type SelectedSegment
} from '../ingest';

const PAGE = 50;
const stateText: Record<string, string> = {
  queued: '排队中', processing: '处理中', awaiting_review: '等待人工', ready: '已就绪', failed: '失败', cancelled: '已取消'
};
const stageText: Record<string, string> = {
  probe: '复制与探测', segment: '场景切分', segment_review: '片段审查', prepare: '生成短片', ready: '完成'
};
const reasonText: Record<string, string> = {
  scene_change: '场景切换', range_start: '范围起点', duration_limit: '超长拆分', merged_short: '短段合并'
};

function message(error: unknown): string {
  if (error instanceof ApiError && error.code === 'resource_busy') return '资源被有效执行占用';
  if (error instanceof ApiError && error.code === 'conflict') return '版本已变化，已重新读取最新状态，请核对后重试';
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

const allowsIngester = (asset: Asset) =>
  asset.agent_visible && !asset.locked && (asset.allowed_agents ?? []).includes('ingester');

export function IngestPanel({ asset, onSelect }: { asset?: Asset; onSelect: (id: string) => void }) {
  const client = useQueryClient();
  const roots = useQuery({ queryKey: ['ingest', 'roots'], queryFn: ingestApi.roots });
  const [rootId, setRootId] = useState('');
  const [relative, setRelative] = useState('');
  const [assetId, setAssetId] = useState(() => `rec_${Date.now()}`);
  const [error, setError] = useState('');
  const refresh = () => Promise.all([['ingest'], ['assets'], ['audit']].map((queryKey) => client.invalidateQueries({ queryKey })));
  const action = useMutation({
    mutationFn: (fn: () => Promise<unknown>) => fn(),
    onSuccess: async () => { setError(''); await refresh(); },
    onError: (e) => { setError(message(e)); void refresh(); }
  });

  const raw = asset && (asset.input_kind === 'raw_recording' || Boolean(asset.ingest_run_id));
  const sources = useQuery({ queryKey: ['ingest', 'sources', asset?.asset_id], queryFn: () => ingestApi.sources(asset!.asset_id), enabled: Boolean(raw) });
  const runs = useQuery({ queryKey: ['ingest', 'runs', asset?.asset_id], queryFn: () => ingestApi.runs(asset!.asset_id), enabled: Boolean(raw) });
  const [runId, setRunId] = useState<string | null>(null);
  const current = runs.data?.find((r) => r.run_id === runId) ?? runs.data?.[0];
  const active = runs.data?.some((r) => ACTIVE_STATES.includes(r.state));
  const [cleanupBlocked, setCleanupBlocked] = useState(false);
  const busy = action.isPending;

  function register(event: FormEvent) {
    event.preventDefault();
    action.mutate(async () => {
      const out = await ingestApi.register(assetId.trim(), rootId || roots.data!.roots[0].root_id, relative.trim());
      setRelative('');
      setAssetId(`rec_${Date.now()}`);
      onSelect(out.asset.asset_id);
    });
  }

  function allow(a: Asset) {
    const agents = [...new Set([...(a.allowed_agents ?? []), 'ingester'])];
    action.mutate(() => api.patchAsset(a.asset_id, { agent_visible: true, locked: false, allowed_agents: agents }));
  }

  return (
    <section className="panel ingest-panel" aria-labelledby="ingest-title">
      <p className="eyebrow">INGEST</p>
      <h2 id="ingest-title">原始录像导入与自动切分</h2>
      <p className="hint">只能从管理员配置的录像根目录选择文件；原始文件不会被修改，系统先复制快照再处理。切分建议需要人工审查后才会生成短片。</p>
      {roots.isError && <p role="alert" className="inline-error">读取导入配置失败：{message(roots.error)}</p>}
      {roots.data && !roots.data.configured && <p className="state">控制面未配置 ingest_roots，导入功能不可用。</p>}
      {roots.data?.configured && <>
        <p role="status">本机导入 Worker：{roots.data.ingesters.length > 0 ? `${roots.data.ingesters.length} 个在线` : '未在线（导入任务会排队等待）'}</p>
        <form className="ingest-form" onSubmit={register}>
          <label className="field-label">录像根目录
            <select value={rootId || roots.data.roots[0]?.root_id} onChange={(e) => setRootId(e.target.value)} disabled={busy}>
              {roots.data.roots.map((r) => <option key={r.root_id} value={r.root_id}>{r.name}</option>)}
            </select></label>
          <label className="field-label">根目录内相对路径
            <input required maxLength={1024} value={relative} placeholder="例如 2026-06/session.mkv" disabled={busy}
              onChange={(e) => setRelative(e.target.value)} /></label>
          <label className="field-label">资产 ID
            <input required maxLength={128} pattern="[A-Za-z0-9._-]+" value={assetId} disabled={busy}
              onChange={(e) => setAssetId(e.target.value)} /></label>
          <button disabled={busy || roots.data.roots.length === 0}>登记录像</button>
        </form>
      </>}
      {error && <p role="alert" className="inline-error">{error}</p>}
      {busy && <p role="status">正在处理…</p>}

      {asset && raw && <div className="ingest-asset">
        <h3>资产 {asset.asset_id}</h3>
        {!allowsIngester(asset) && <div className="action-row">
          <p className="hint">该资产尚未允许本机导入 Worker 处理（需可见、未锁定且允许 ingester）。</p>
          <button disabled={busy} onClick={() => allow(asset)}>允许本机导入 Worker 处理</button>
        </div>}
        {sources.isError && <p role="alert" className="inline-error">{message(sources.error)}</p>}
        <ul className="ingest-list">{sources.data?.map((s) => <li key={s.source_id}>
          <span>{s.relative_path}</span><small>{(s.size_bytes / 1048576).toFixed(1)} MiB · {s.has_snapshot ? '已有快照' : '未复制'}</small>
          <button className="secondary-button" disabled={busy || active || cleanupBlocked || !allowsIngester(asset)}
            onClick={() => action.mutate(() => ingestApi.start(asset.asset_id, s))}>开始导入</button>
        </li>)}</ul>
        {runs.isError && <p role="alert" className="inline-error">{message(runs.error)}</p>}
        {(runs.data?.length ?? 0) > 0 && <label className="field-label">导入运行
          <select value={current?.run_id} onChange={(e) => setRunId(e.target.value)}>
            {runs.data!.map((r) => <option key={r.run_id} value={r.run_id}>{r.run_id} · {stateText[r.state]} / {stageText[r.stage]}</option>)}
          </select></label>}
        {current && <RunDetail key={current.run_id} runId={current.run_id} policy={roots.data?.policy} busy={busy}
          act={(fn) => action.mutate(fn)} onCleanup={setCleanupBlocked} />}
      </div>}
    </section>
  );
}

function RunDetail({ runId, policy, busy, act, onCleanup }: { runId: string; policy?: IngestPolicy; busy: boolean; act: (fn: () => Promise<unknown>) => void; onCleanup: (blocked: boolean) => void }) {
  const view = useQuery({
    queryKey: ['ingest', 'run', runId], queryFn: () => ingestApi.run(runId),
    refetchInterval: (q) => {
      const state = q.state.data?.run?.state;
      return state && ACTIVE_STATES.includes(state) && state !== 'awaiting_review' ? 5000 : false;
    }
  });
  const execution = view.data?.execution;
  const blocked = execution?.status === 'cleanup_blocked' || execution?.cleanup === 'blocked' || execution?.cleanup === 'cleanup_blocked';
  useEffect(() => { onCleanup(Boolean(blocked)); }, [blocked, onCleanup]);
  if (view.isError) return <p role="alert" className="inline-error">{message(view.error)}</p>;
  if (!view.data?.run) return <p className="state">正在读取运行…</p>;
  const { run, probe } = view.data;
  const task = view.data.tasks.find((t) => t.task.task_id === run.current_task_id)?.task;
  const cancellable = ACTIVE_STATES.includes(run.state);
  const banner = executionBanner(execution);
  const progress = displayProgress(task, execution, run);
  const notStarted = Boolean(execution)
    ? ['allocated', 'waiting_resource'].includes(execution?.status ?? '')
    : task?.status === 'queued' || task?.status === 'pending';
  return (
    <div className="ingest-run">
      <div className="detail-title"><strong>{stateText[run.state]} · {stageText[run.stage]}</strong><span className="pill">v{run.version}</span></div>
      {banner && <p className="execution-status" role="status">{banner}</p>}
      {notStarted && <p className="execution-status" role="status">尚未开始执行</p>}
      {progress && (run.state === 'queued' || run.state === 'processing' || run.state === 'ready') && <div>
        <progress max={1} value={progress.value} aria-label="当前步骤进度" /> <small>{progress.label}</small>
      </div>}
      {run.error_message && <p role="alert" className="inline-error">{run.error_code}：{run.error_message}</p>}
      <div className="action-row">
        {cancellable && <button className="secondary-button" disabled={busy}
          onClick={() => act(() => ingestApi.cancel(runId, run.version, '人工取消'))}>取消导入</button>}
        {run.state === 'failed' && <button disabled={busy}
          onClick={() => act(() => ingestApi.retry(runId, run.version, run.stage))}>重试失败步骤（复用已校验进度）</button>}
      </div>
      {probe && <ProbeSummary view={view.data} />}
      {probe && run.state === 'awaiting_review' && <AnalysisForm view={view.data} busy={busy} act={act} />}
      {run.state === 'awaiting_review' && run.stage === 'segment_review' && view.data.segments &&
        <SegmentReview key={`${run.run_id}-${run.analysis_revision}`} view={view.data} policy={policy} busy={busy} act={act} />}
      {run.state === 'ready' && <ReadyFiles view={view.data} />}
    </div>
  );
}

function ProbeSummary({ view }: { view: IngestRunView }) {
  const probe = view.probe!;
  return <details className="ingest-probe" open={view.run.stage === 'probe'}>
    <summary>探测结果：{probe.container} · {usToClock(probe.duration_us)} · {probe.streams.length} 条流</summary>
    {probe.limitations.length > 0 && <p className="hint">注意：{probe.limitations.join('、')}</p>}
    <ul className="ingest-list">{probe.streams.map((s) => <li key={s.index}>
      <span>#{s.index} {s.type} · {s.codec}</span>
      <small>{s.type === 'video' ? `${s.width}×${s.height} · ${s.avg_frame_rate} · ${s.frame_rate_mode}`
        : s.type === 'audio' ? `${s.sample_rate} Hz · ${s.channels} 声道` : ''}{s.title ? ` · ${s.title}` : ''}{s.language ? ` · ${s.language}` : ''}</small>
    </li>)}</ul>
  </details>;
}

function AnalysisForm({ view, busy, act }: { view: IngestRunView; busy: boolean; act: (fn: () => Promise<unknown>) => void }) {
  const probe = view.probe!;
  const last = [...view.plans].reverse().find((p) => p.kind === 'analysis')?.analysis;
  const videos = probe.streams.filter((s) => s.type === 'video');
  const audios = probe.streams.filter((s) => s.type === 'audio');
  const [video, setVideo] = useState(last?.video_stream_index ?? videos[0]?.index ?? 0);
  const [audio, setAudio] = useState<number | null>(last ? last.game_audio_stream_index : audios[0]?.index ?? null);
  const [start, setStart] = useState(usToClock(last?.source_range_us[0] ?? 0));
  const [end, setEnd] = useState(usToClock(last?.source_range_us[1] ?? probe.duration_us));
  const [threshold, setThreshold] = useState(last?.segmentation.threshold ?? 0.3);
  const [minS, setMinS] = useState((last?.segmentation.min_segment_us ?? 2_000_000) / 1e6);
  const [maxS, setMaxS] = useState((last?.segmentation.max_segment_us ?? 120_000_000) / 1e6);
  const a = clockToUs(start), b = clockToUs(end);
  const problem = a === null || b === null ? '时间格式应为 mm:ss.sss'
    : a < 0 || b <= a || b > probe.duration_us ? '分析范围须在录像时长内且结束晚于开始'
    : minS < 0.5 || minS * 2 > maxS || maxS > 1800 ? '片段长度须满足 0.5 秒 ≤ 最短，最长 ≥ 2×最短且 ≤ 30 分钟' : null;
  return <form className="ingest-analysis" onSubmit={(e) => {
    e.preventDefault();
    if (problem) return;
    act(() => ingestApi.analysis(view.run.run_id, {
      expected_version: view.run.version, video_stream_index: video, game_audio_stream_index: audio, source_range_us: [a!, b!],
      segmentation: { method: 'scene_change', threshold, min_segment_us: Math.round(minS * 1e6), max_segment_us: Math.round(maxS * 1e6) }
    }));
  }}>
    <h4>{view.run.stage === 'segment_review' ? '调整切分参数并重新分析' : '确认流与范围，开始场景切分'}</h4>
    <div className="ingest-grid">
      <label className="field-label">画面流<select value={video} disabled={busy} onChange={(e) => setVideo(Number(e.target.value))}>
        {videos.map((s) => <option key={s.index} value={s.index}>#{s.index} {s.width}×{s.height}</option>)}</select></label>
      <label className="field-label">游戏音轨<select value={audio ?? ''} disabled={busy} onChange={(e) => setAudio(e.target.value === '' ? null : Number(e.target.value))}>
        <option value="">不使用（静音）</option>
        {audios.map((s) => <option key={s.index} value={s.index}>#{s.index} {s.title || s.codec} · {s.channels} 声道</option>)}</select></label>
      <label className="field-label">起点<input value={start} disabled={busy} onChange={(e) => setStart(e.target.value)} /></label>
      <label className="field-label">终点<input value={end} disabled={busy} onChange={(e) => setEnd(e.target.value)} /></label>
      <label className="field-label">场景阈值（0.05–0.95）<input type="number" min={0.05} max={0.95} step={0.01} value={threshold} disabled={busy} onChange={(e) => setThreshold(Number(e.target.value))} /></label>
      <label className="field-label">最短片段（秒）<input type="number" min={0.5} step={0.5} value={minS} disabled={busy} onChange={(e) => setMinS(Number(e.target.value))} /></label>
      <label className="field-label">最长片段（秒）<input type="number" min={1} max={1800} step={1} value={maxS} disabled={busy} onChange={(e) => setMaxS(Number(e.target.value))} /></label>
    </div>
    {problem && <p className="inline-error" role="alert">{problem}</p>}
    <p className="hint">场景检测只看亮度变化：仅色相变化、亮度接近的切换可能识别不到，请在审查时手动调整边界。</p>
    <button disabled={busy || Boolean(problem)}>{view.run.stage === 'segment_review' ? '按新参数重新分析' : '开始切分'}</button>
  </form>;
}

type Draft = { start: string; end: string };

function SegmentReview({ view, policy, busy, act }: { view: IngestRunView; policy?: IngestPolicy; busy: boolean; act: (fn: () => Promise<unknown>) => void }) {
  const run = view.run;
  const [offset, setOffset] = useState(0);
  const page = useQuery({ queryKey: ['ingest', 'segments', run.run_id, run.analysis_revision, offset], queryFn: () => ingestApi.segments(run.run_id, offset, PAGE) });
  const lastSel = [...view.plans].reverse().find((p) => p.kind === 'selection' && p.revision === run.selection_revision)?.selection;
  const [picked, setPicked] = useState<Map<string, Draft>>(() => new Map(
    (lastSel?.selected_segments ?? []).map((s) => [s.segment_id, { start: usToClock(s.start_us), end: usToClock(s.end_us) }])));
  const [fps, setFps] = useState<30 | 60>(lastSel?.output.fps ?? 30);
  const [splitPoints, setSplitPoints] = useState<Record<string, string>>({});
  const [editError, setEditError] = useState('');
  const [known, setKnown] = useState<Map<string, SegmentItem>>(new Map());
  useEffect(() => {
    if (page.data) setKnown((prev) => { const next = new Map(prev); for (const it of page.data.items) next.set(it.segment_id, it); return next; });
  }, [page.data]);

  const segments = useMemo<SelectedSegment[] | string>(() => {
    const out: SelectedSegment[] = [];
    for (const [id, d] of picked) {
      const s = clockToUs(d.start), e = clockToUs(d.end);
      if (s === null || e === null) return `${id} 的时间格式应为 mm:ss.sss`;
      out.push({ segment_id: id, start_us: s, end_us: e });
    }
    return out.sort((x, y) => x.start_us - y.start_us);
  }, [picked]);
  const problem = typeof segments === 'string' ? segments : selectionProblem(segments, view.probe!.duration_us, fps, policy);
  const dirty = typeof segments === 'string' || !lastSel || lastSel.output.fps !== fps || JSON.stringify(segments) !== JSON.stringify(lastSel.selected_segments);
  const transform = (fn: (segs: SelectedSegment[]) => SelectedSegment[]) => {
    try {
      if (typeof segments === 'string' || problem) throw new Error(problem || '请先修正时间');
      const next = fn(segments);
      const error = selectionProblem(next, view.probe!.duration_us, fps, policy);
      if (error) throw new Error(error);
      setPicked(new Map(next.map(s => [s.segment_id, { start: usToClock(s.start_us), end: usToClock(s.end_us) }])));
      setSplitPoints({}); setEditError('');
    } catch (e) { setEditError(message(e)); }
  };
  const profiles = useQuery({ queryKey: ['processing-profiles'], queryFn: api.listProfiles });
  const [profileId, setProfileId] = useState('');
  const profile = profiles.data?.find((p) => p.profile_id === profileId);
  const toggle = (it: SegmentItem) => setPicked((prev) => {
    const next = new Map(prev);
    if (next.has(it.segment_id)) next.delete(it.segment_id);
    else next.set(it.segment_id, { start: usToClock(it.start_us), end: usToClock(it.end_us) });
    return next;
  });
  const edit = (id: string, field: keyof Draft, value: string) => setPicked((prev) => new Map(prev).set(id, { ...prev.get(id)!, [field]: value }));
  const total = page.data?.total ?? view.segments!.count;

  return <div className="ingest-review">
    <h4>候选片段（{total} 个，方法 {view.segments!.method_version}，已选 {picked.size}）</h4>
    {page.isError && <p role="alert" className="inline-error">{message(page.error)}</p>}
    <ol className="segment-list" start={offset + 1}>{page.data?.items.map((it) => {
      const draft = picked.get(it.segment_id);
      return <li key={it.segment_id} className={draft ? 'is-selected' : ''}>
        {it.thumbnail_key ? <img src={ingestFileURL(run.run_id, it.thumbnail_key)} alt={`${it.segment_id} 缩略图`} loading="lazy" /> : <span className="segment-thumb-missing">无缩略图</span>}
        <div className="segment-meta">
          <label><input type="checkbox" checked={Boolean(draft)} disabled={busy} onChange={() => toggle(it)} /> {it.segment_id}</label>
          <small>{usToClock(it.start_us)} – {usToClock(it.end_us)} · {reasonText[it.reason]} · 分数 {it.score.toFixed(2)}</small>
          {draft && <div className="segment-bounds">
            <label className="field-label">起点<input value={draft.start} disabled={busy} onChange={(e) => edit(it.segment_id, 'start', e.target.value)} /></label>
            <label className="field-label">终点<input value={draft.end} disabled={busy} onChange={(e) => edit(it.segment_id, 'end', e.target.value)} /></label>
          </div>}
        </div>
      </li>;
    })}</ol>
    <div className="action-row">
      <button className="secondary-button" disabled={offset === 0 || page.isFetching} onClick={() => setOffset(Math.max(0, offset - PAGE))}>上一页</button>
      <span>{offset + 1}–{Math.min(offset + PAGE, total)} / {total}</span>
      <button className="secondary-button" disabled={offset + PAGE >= total || page.isFetching} onClick={() => setOffset(offset + PAGE)}>下一页</button>
    </div>
    {picked.size > 0 && <p className="hint" style={{ overflowWrap: 'anywhere' }}>已选（跨页保留）：{[...picked.keys()].map((id) => known.has(id) || id.startsWith('manual_') ? id : `${id}（未在已读页中）`).join('、')}</p>}
    {picked.size > 0 && <section className="selected-editor" aria-label="已选片段编辑">
      <h4>已选片段：编辑、拆分与合并</h4>
      <p className="hint">时间使用原录像坐标；合并仅适用于首尾相接的片段。修改后先保存选择版本，再生成短片。</p>
      {[...picked.entries()].sort((a, b) => (clockToUs(a[1].start) ?? 0) - (clockToUs(b[1].start) ?? 0)).map(([id, d]) => {
        const start = clockToUs(d.start), end = clockToUs(d.end);
        const point = splitPoints[id] ?? (start !== null && end !== null ? usToClock(Math.floor((start + end) / 2)) : '');
        const index = typeof segments !== 'string' ? segments.findIndex(s => s.segment_id === id) : -1;
        const adjacent = typeof segments !== 'string' && index >= 0 && segments[index + 1]?.start_us === end;
        return <div className="workflow-card" key={id}>
          <strong style={{ overflowWrap: 'anywhere' }}>{id}</strong>
          <div className="ingest-grid">
            <label className="field-label">起点<input value={d.start} disabled={busy} onChange={e => edit(id, 'start', e.target.value)} /></label>
            <label className="field-label">终点<input value={d.end} disabled={busy} onChange={e => edit(id, 'end', e.target.value)} /></label>
            <label className="field-label">拆分点<input value={point} disabled={busy} onChange={e => setSplitPoints(prev => ({ ...prev, [id]: e.target.value }))} /></label>
          </div>
          <div className="action-row">
            <button disabled={busy || Boolean(problem)} onClick={() => transform(s => splitSelection(s, id, clockToUs(point) ?? -1, `manual_${crypto.randomUUID().replaceAll('-', '')}`))}>拆分片段</button>
            <button disabled={busy || Boolean(problem) || !adjacent} onClick={() => transform(s => mergeSelection(s, id))}>与下一片段合并</button>
            <button className="secondary-button" disabled={busy} onClick={() => setPicked(prev => { const next = new Map(prev); next.delete(id); return next; })}>移除此片段</button>
          </div>
        </div>;
      })}
      {editError && <p role="alert" className="inline-error">{editError}</p>}
    </section>}
    <div className="ingest-grid">
      <label className="field-label">输出帧率<select value={fps} disabled={busy} onChange={(e) => setFps(Number(e.target.value) as 30 | 60)}>
        <option value={30}>30 fps</option><option value={60}>60 fps</option></select></label>
    </div>
    {problem && picked.size > 0 && <p role="alert" className="inline-error">{problem}</p>}
    <div className="action-row">
      <button disabled={busy || Boolean(problem)} onClick={() => act(() => ingestApi.selection(run.run_id, {
        expected_version: run.version, base_plan_revision: run.analysis_revision,
        selected_segments: segments as SelectedSegment[], output: { fps, sample_rate: 48000 }
      }))}>保存选择版本</button>
    </div>
    {run.selection_revision > 0 && <div className="ingest-prepare">
      <p>当前选择版本 r{run.selection_revision}：{lastSel?.selected_segments.length ?? 0} 段 · {lastSel?.output.fps} fps。生成短片前需绑定一个已保存的处理预设（帧预算按它计算，不调用任何模型）。</p>
      <div className="ingest-grid">
        <label className="field-label">处理预设<select value={profileId} disabled={busy} onChange={(e) => setProfileId(e.target.value)}>
          <option value="">选择预设</option>
          {profiles.data?.map((p) => <option key={p.profile_id} value={p.profile_id}>{p.name} · v{p.revision} · {p.sampling.max_frames} 帧</option>)}
        </select></label>
      </div>
      {dirty && <p role="status">选择或帧率有未保存的修改，请先保存选择版本。</p>}
      <button disabled={busy || !profile || dirty} onClick={() => act(() => ingestApi.prepare(run.run_id, {
        expected_version: run.version, plan_revision: run.selection_revision, profile_id: profile!.profile_id, profile_revision: profile!.revision
      }))}>生成短片并登记为 EDL 包</button>
    </div>}
  </div>;
}

function ReadyFiles({ view }: { view: IngestRunView }) {
  const media = view.files.filter((k) => k.startsWith('media_'));
  return <div className="ingest-ready">
    <p>已生成 {media.length} 个短片，资产已切换为 EDL 包，可在下方「处理预设与运行准备」中启动内容流程。内容质量仍需人工验收。</p>
    <div className="workflow-evidence">{media.map((k) => <figure key={k}>
      <video controls preload="metadata" src={ingestFileURL(view.run.run_id, k)} />
      <figcaption>{k.slice('media_'.length)}</figcaption>
    </figure>)}</div>
  </div>;
}
