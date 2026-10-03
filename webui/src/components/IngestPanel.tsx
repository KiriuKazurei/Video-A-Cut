import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert, Badge, Button, Card, Checkbox, Col, Collapse, Empty, Flex, Form, Input, InputNumber, List,
  Progress, Row, Space, Spin, Tag, Typography
} from 'antd';
import { api, ApiError, ingestApi, ingestFileURL } from '../api';
import type { Asset } from '../types';
import {
  ACTIVE_STATES, clockToUs, displayProgress, executionBanner, selectionProblem, usToClock, splitSelection, mergeSelection,
  type IngestRunView, type IngestPolicy, type SegmentItem, type SelectedSegment
} from '../ingest';
import { usePreview } from './Monitor';
import { Timeline, type TimelineClip } from './Timeline';
import { ValueSelect } from './ui';

const PAGE = 50;
const stateText: Record<string, string> = {
  queued: '排队中', processing: '处理中', awaiting_review: '等待人工', ready: '已就绪', failed: '失败', cancelled: '已取消'
};
const stateColor: Record<string, string> = {
  queued: 'default', processing: 'processing', awaiting_review: 'warning', ready: 'success', failed: 'error', cancelled: 'default'
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

/** 粗剪时间线的标尺刻度：整数微秒 → 简短时钟文本（仅显示，不回写）。 */
const tickClock = (us: number) => usToClock(Math.round(us / 1e6) * 1e6).replace(/\.$/, '').replace(/\.0*$/, '');

const Hint = ({ children }: { children: React.ReactNode }) => <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>{children}</Typography.Paragraph>;

/** 导入页：受控根目录内录像登记（API-04、API-05）。只提交 root_id + 相对路径。 */
export function IngestRegister({ onSelect }: { onSelect: (id: string) => void }) {
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
  const busy = action.isPending;

  function register() {
    action.mutate(async () => {
      const out = await ingestApi.register(assetId.trim(), rootId || roots.data!.roots[0].root_id, relative.trim());
      setRelative('');
      setAssetId(`rec_${Date.now()}`);
      onSelect(out.asset.asset_id);
    });
  }

  const online = roots.data?.ingesters.length ?? 0;
  return (
    <Card size="small" className="ingest-register" aria-labelledby="ingest-register-title" style={{ height: '100%' }}
      title={<span id="ingest-register-title">登记原始录像</span>}
      extra={roots.data?.configured && <Badge status={online > 0 ? 'success' : 'warning'} text={`导入 Worker ${online > 0 ? `${online} 个在线` : '未在线'}`} />}>
      <Hint>只能从管理员配置的录像根目录选择文件；原始文件不会被修改，系统先复制快照再处理。登记后到「粗剪」页开始导入与切分。</Hint>
      {roots.isPending && <Spin size="small" />}
      {roots.isError && <Alert type="error" showIcon message={`读取导入配置失败：${message(roots.error)}`} />}
      {roots.data && !roots.data.configured && <Alert type="warning" showIcon message="控制面未配置 ingest_roots，导入功能不可用。" />}
      {roots.data?.configured && <>
        <p role="status" className="sr-only">本机导入 Worker：{online > 0 ? `${online} 个在线` : '未在线（导入任务会排队等待）'}</p>
        <Form layout="vertical" className="ingest-form" onFinish={register} disabled={busy}>
          <Row gutter={12}>
            <Col xs={24} sm={12}><Form.Item label="录像根目录">
              <ValueSelect<string> aria-label="录像根目录" value={rootId || roots.data.roots[0]?.root_id} onChange={setRootId}
                options={roots.data.roots.map((r) => ({ value: r.root_id, label: r.name }))} />
            </Form.Item></Col>
            <Col xs={24} sm={12}><Form.Item label="根目录内相对路径">
              <Input aria-label="根目录内相对路径" required maxLength={1024} value={relative} placeholder="例如 2026-06/session.mkv"
                onChange={(e) => setRelative(e.target.value)} />
            </Form.Item></Col>
            <Col xs={24} sm={12}><Form.Item label="资产 ID">
              <Input aria-label="资产 ID" required maxLength={128} pattern="[A-Za-z0-9._-]+" value={assetId}
                onChange={(e) => setAssetId(e.target.value)} />
            </Form.Item></Col>
            <Col xs={24} sm={12}><Form.Item label=" ">
              <Button type="primary" htmlType="submit" block loading={busy} disabled={roots.data.roots.length === 0}>登记录像</Button>
            </Form.Item></Col>
          </Row>
        </Form>
      </>}
      {error && <Alert type="error" showIcon role="alert" message={error} />}
    </Card>
  );
}

/** 粗剪页：导入运行、探测、分析计划、候选选段、选择版本与生成短片（API-06~16、19）。 */
export function IngestPanel({ asset }: { asset?: Asset }) {
  const client = useQueryClient();
  const roots = useQuery({ queryKey: ['ingest', 'roots'], queryFn: ingestApi.roots });
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

  function allow(a: Asset) {
    const agents = [...new Set([...(a.allowed_agents ?? []), 'ingester'])];
    action.mutate(() => api.patchAsset(a.asset_id, { agent_visible: true, locked: false, allowed_agents: agents }));
  }

  return (
    <Card className="ingest-panel" aria-labelledby="ingest-title" title={<span id="ingest-title">原始录像导入与自动切分</span>}
      extra={current && <Tag color={stateColor[current.state]}>{stateText[current.state] ?? current.state} · {stageText[current.stage] ?? current.stage}</Tag>}>
      <Hint>切分建议需要人工审查后才会生成短片；时间均为原录像坐标的整数微秒。</Hint>
      <Flex vertical gap={12}>
        {error && <Alert type="error" showIcon role="alert" message={error} />}
        {busy && <Typography.Text type="secondary" role="status">正在处理…</Typography.Text>}
        {!asset && <Empty description="在左侧项目面板选择一项原始录像资产。" />}
        {asset && !raw && <Alert type="info" showIcon message={`资产 ${asset.asset_id} 是 EDL 包，不需要粗剪切分；可直接到「准备」页检查运行条件。`} />}

        {asset && raw && <>
          <Typography.Title level={5} style={{ margin: 0 }}>资产 {asset.asset_id}</Typography.Title>
          {!allowsIngester(asset) && <Alert type="warning" showIcon message="该资产尚未允许本机导入 Worker 处理（需可见、未锁定且允许 ingester）。"
            action={<Button size="small" type="primary" disabled={busy} onClick={() => allow(asset)}>允许本机导入 Worker 处理</Button>} />}
          {sources.isError && <Alert type="error" showIcon role="alert" message={message(sources.error)} />}
          <List size="small" bordered className="ingest-list" dataSource={sources.data ?? []} loading={sources.isPending}
            locale={{ emptyText: '尚未登记源文件' }}
            renderItem={(s) => <List.Item key={s.source_id}
              actions={[<Button key="start" disabled={busy || active || cleanupBlocked || !allowsIngester(asset)}
                onClick={() => action.mutate(() => ingestApi.start(asset.asset_id, s))}>开始导入</Button>]}>
              <List.Item.Meta title={<span style={{ overflowWrap: 'anywhere' }}>{s.relative_path}</span>}
                description={`${(s.size_bytes / 1048576).toFixed(1)} MiB · ${s.has_snapshot ? '已有快照' : '未复制'}`} />
            </List.Item>} />
          {runs.isError && <Alert type="error" showIcon role="alert" message={message(runs.error)} />}
          {(runs.data?.length ?? 0) > 0 && <Form layout="vertical"><Form.Item label="导入运行" style={{ marginBottom: 0 }}>
            <ValueSelect<string> className="select-ingest-run" aria-label="导入运行" value={current?.run_id} onChange={setRunId}
              options={runs.data!.map((r) => ({ value: r.run_id, label: `${r.run_id} · ${stateText[r.state]} / ${stageText[r.stage]}` }))} />
          </Form.Item></Form>}
          {current && <RunDetail key={current.run_id} runId={current.run_id} policy={roots.data?.policy} busy={busy}
            act={(fn) => action.mutate(fn)} onCleanup={setCleanupBlocked} />}
        </>}
      </Flex>
    </Card>
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
  if (view.isError) return <Alert type="error" showIcon role="alert" message={message(view.error)} />;
  if (!view.data?.run) return <Spin tip="正在读取运行…"><div style={{ height: 48 }} /></Spin>;
  const { run, probe } = view.data;
  const task = view.data.tasks.find((t) => t.task.task_id === run.current_task_id)?.task;
  const cancellable = ACTIVE_STATES.includes(run.state);
  const banner = executionBanner(execution);
  const progress = displayProgress(task, execution, run);
  const notStarted = Boolean(execution)
    ? ['allocated', 'waiting_resource'].includes(execution?.status ?? '')
    : task?.status === 'queued' || task?.status === 'pending';
  return (
    <Flex vertical gap={12} className="ingest-run">
      <Flex align="center" gap={8} wrap>
        <Typography.Text strong>{stateText[run.state]} · {stageText[run.stage]}</Typography.Text>
        <Tag>v{run.version}</Tag>
      </Flex>
      {banner && <Alert type="info" showIcon role="status" message={banner} />}
      {notStarted && <Alert type="info" showIcon role="status" message="尚未开始执行" />}
      {progress && (run.state === 'queued' || run.state === 'processing' || run.state === 'ready') && <Flex align="center" gap={12} wrap>
        <Progress style={{ flex: '1 1 200px', margin: 0 }} percent={Math.round(progress.value * 100)} size="small"
          status={run.state === 'ready' ? 'success' : 'active'} aria-label="当前步骤进度" />
        <Typography.Text type="secondary">{progress.label}</Typography.Text>
      </Flex>}
      {run.error_message && <Alert type="error" showIcon role="alert" message={`${run.error_code}：${run.error_message}`} />}
      {(cancellable || run.state === 'failed') && <Space wrap>
        {cancellable && <Button danger disabled={busy}
          onClick={() => act(() => ingestApi.cancel(runId, run.version, '人工取消'))}>取消导入</Button>}
        {run.state === 'failed' && <Button type="primary" disabled={busy}
          onClick={() => act(() => ingestApi.retry(runId, run.version, run.stage))}>重试失败步骤（复用已校验进度）</Button>}
      </Space>}
      {probe && <ProbeSummary view={view.data} />}
      {probe && run.state === 'awaiting_review' && <AnalysisForm view={view.data} busy={busy} act={act} />}
      {run.state === 'awaiting_review' && run.stage === 'segment_review' && view.data.segments &&
        <SegmentReview key={`${run.run_id}-${run.analysis_revision}`} view={view.data} policy={policy} busy={busy} act={act} />}
      {run.state === 'ready' && <ReadyFiles view={view.data} />}
    </Flex>
  );
}

function ProbeSummary({ view }: { view: IngestRunView }) {
  const probe = view.probe!;
  return <Collapse size="small" className="ingest-probe" defaultActiveKey={view.run.stage === 'probe' ? ['probe'] : []} items={[{
    key: 'probe',
    label: `探测结果：${probe.container} · ${usToClock(probe.duration_us)} · ${probe.streams.length} 条流`,
    children: <>
      {probe.limitations.length > 0 && <Alert type="warning" showIcon style={{ marginBottom: 8 }} message={`注意：${probe.limitations.join('、')}`} />}
      <List size="small" dataSource={probe.streams} renderItem={(s) => <List.Item key={s.index}>
        <List.Item.Meta title={`#${s.index} ${s.type} · ${s.codec}`}
          description={`${s.type === 'video' ? `${s.width}×${s.height} · ${s.avg_frame_rate} · ${s.frame_rate_mode}`
            : s.type === 'audio' ? `${s.sample_rate} Hz · ${s.channels} 声道` : ''}${s.title ? ` · ${s.title}` : ''}${s.language ? ` · ${s.language}` : ''}`} />
      </List.Item>} />
    </>
  }]} />;
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
  return <Card size="small" type="inner" className="ingest-analysis"
    title={view.run.stage === 'segment_review' ? '调整切分参数并重新分析' : '确认流与范围，开始场景切分'}>
    <Form layout="vertical" disabled={busy} onFinish={() => {
      if (problem) return;
      act(() => ingestApi.analysis(view.run.run_id, {
        expected_version: view.run.version, video_stream_index: video, game_audio_stream_index: audio, source_range_us: [a!, b!],
        segmentation: { method: 'scene_change', threshold, min_segment_us: Math.round(minS * 1e6), max_segment_us: Math.round(maxS * 1e6) }
      }));
    }}>
      <Row gutter={12}>
        <Col xs={24} sm={12} lg={6}><Form.Item label="画面流">
          <ValueSelect<number> className="select-video-stream" aria-label="画面流" value={video} onChange={setVideo}
            options={videos.map((s) => ({ value: s.index, label: `#${s.index} ${s.width}×${s.height}` }))} />
        </Form.Item></Col>
        <Col xs={24} sm={12} lg={6}><Form.Item label="游戏音轨">
          <ValueSelect<number | ''> className="select-audio-stream" aria-label="游戏音轨" value={audio ?? ''} onChange={(v) => setAudio(v === '' ? null : Number(v))}
            options={[{ value: '', label: '不使用（静音）' }, ...audios.map((s) => ({ value: s.index, label: `#${s.index} ${s.title || s.codec} · ${s.channels} 声道` }))]} />
        </Form.Item></Col>
        <Col xs={12} lg={6}><Form.Item label="起点"><Input aria-label="起点" value={start} onChange={(e) => setStart(e.target.value)} /></Form.Item></Col>
        <Col xs={12} lg={6}><Form.Item label="终点"><Input aria-label="终点" value={end} onChange={(e) => setEnd(e.target.value)} /></Form.Item></Col>
        <Col xs={24} sm={8}><Form.Item label="场景阈值（0.05–0.95）">
          <InputNumber aria-label="场景阈值" style={{ width: '100%' }} min={0.05} max={0.95} step={0.01} value={threshold} onChange={(v) => setThreshold(Number(v ?? 0))} />
        </Form.Item></Col>
        <Col xs={12} sm={8}><Form.Item label="最短片段（秒）">
          <InputNumber aria-label="最短片段" style={{ width: '100%' }} min={0.5} step={0.5} value={minS} onChange={(v) => setMinS(Number(v ?? 0))} />
        </Form.Item></Col>
        <Col xs={12} sm={8}><Form.Item label="最长片段（秒）">
          <InputNumber aria-label="最长片段" style={{ width: '100%' }} min={1} max={1800} step={1} value={maxS} onChange={(v) => setMaxS(Number(v ?? 0))} />
        </Form.Item></Col>
      </Row>
      {problem && <Alert type="error" showIcon role="alert" message={problem} style={{ marginBottom: 12 }} />}
      <Hint>场景检测只看亮度变化：仅色相变化、亮度接近的切换可能识别不到，请在审查时手动调整边界。</Hint>
      <Button type="primary" htmlType="submit" disabled={busy || Boolean(problem)}>{view.run.stage === 'segment_review' ? '按新参数重新分析' : '开始切分'}</Button>
    </Form>
  </Card>;
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
  const preview = usePreview();
  const [focusId, setFocusId] = useState<string | null>(null);
  const show = (it: SegmentItem) => {
    setFocusId(it.segment_id);
    if (it.thumbnail_key) preview({ kind: 'image', src: ingestFileURL(run.run_id, it.thumbnail_key), title: it.segment_id,
      meta: `${usToClock(it.start_us)} – ${usToClock(it.end_us)}`, note: '候选片段缩略图（源素材取样，非成片）' });
    else preview(null);
  };
  const durationUs = view.probe!.duration_us;
  const candidateClips: TimelineClip[] = (page.data?.items ?? []).map((it) => ({
    id: it.segment_id, start: it.start_us, end: it.end_us, label: it.segment_id, tone: picked.has(it.segment_id) ? 'selected' : 'candidate',
    title: `${it.segment_id} · ${usToClock(it.start_us)} – ${usToClock(it.end_us)} · ${reasonText[it.reason]}`,
    active: focusId === it.segment_id, onSelect: () => show(it)
  }));
  const selectedClips: TimelineClip[] = typeof segments === 'string' ? [] : segments.map((it) => ({
    id: it.segment_id, start: it.start_us, end: it.end_us, label: it.segment_id, tone: 'video',
    title: `${it.segment_id} · ${usToClock(it.start_us)} – ${usToClock(it.end_us)}`
  }));

  return <Flex vertical gap={12} className="ingest-review">
    <Timeline label="源时间线" duration={durationUs} formatTick={tickClock}
      caption={`本页候选 ${page.data?.items.length ?? 0} 个 · 已选 ${picked.size} 段 · 点击候选在源监视器查看缩略图`}
      tracks={[{ id: 'C', name: '候选', hint: '当前页的自动候选片段', clips: candidateClips },
        { id: 'V1', name: '已选', hint: '将保存为选择版本的完整列表（含跨页与拆分片段）', clips: selectedClips }]}
      emptyText="当前分析版本暂无候选片段" />
    <Typography.Title level={5} style={{ margin: 0 }}>候选片段（{total} 个，方法 {view.segments!.method_version}，已选 {picked.size}）</Typography.Title>
    {page.isError && <Alert type="error" showIcon role="alert" message={message(page.error)} />}
    <Spin spinning={page.isFetching && !page.data}>
      <Row gutter={[12, 12]} className="segment-list">{page.data?.items.map((it) => {
        const draft = picked.get(it.segment_id);
        return <Col key={it.segment_id} xs={24} md={12} xxl={8}>
          <Card size="small" className={`segment-item${draft ? ' is-selected' : ''}`} styles={{ body: { padding: 8 } }}>
            <div className="segment-row">
              <button type="button" className={`segment-thumb${focusId === it.segment_id ? ' is-active' : ''}`} onClick={() => show(it)} aria-label={`在源监视器查看 ${it.segment_id}`}>
                {it.thumbnail_key ? <img src={ingestFileURL(run.run_id, it.thumbnail_key)} alt={`${it.segment_id} 缩略图`} loading="lazy" /> : <span className="segment-thumb-missing">无缩略图</span>}
              </button>
              <Flex vertical gap={2} style={{ minWidth: 0 }}>
                <Checkbox checked={Boolean(draft)} disabled={busy} onChange={() => toggle(it)}><Typography.Text strong>{it.segment_id}</Typography.Text></Checkbox>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>{usToClock(it.start_us)} – {usToClock(it.end_us)}</Typography.Text>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>{reasonText[it.reason]} · 分数 {it.score.toFixed(2)}</Typography.Text>
              </Flex>
            </div>
            {draft && <Row gutter={8} className="segment-bounds" style={{ marginTop: 8 }}>
              <Col span={12}><Input size="small" addonBefore="起" aria-label="起点" value={draft.start} disabled={busy} onChange={(e) => edit(it.segment_id, 'start', e.target.value)} /></Col>
              <Col span={12}><Input size="small" addonBefore="止" aria-label="终点" value={draft.end} disabled={busy} onChange={(e) => edit(it.segment_id, 'end', e.target.value)} /></Col>
            </Row>}
          </Card>
        </Col>;
      })}</Row>
    </Spin>
    <Flex align="center" gap={12} wrap>
      <Button disabled={offset === 0 || page.isFetching} onClick={() => setOffset(Math.max(0, offset - PAGE))}>上一页</Button>
      <Typography.Text>{offset + 1}–{Math.min(offset + PAGE, total)} / {total}</Typography.Text>
      <Button disabled={offset + PAGE >= total || page.isFetching} onClick={() => setOffset(offset + PAGE)}>下一页</Button>
    </Flex>
    {picked.size > 0 && <Typography.Paragraph type="secondary" style={{ overflowWrap: 'anywhere', marginBottom: 0 }}>已选（跨页保留）：{[...picked.keys()].map((id) => known.has(id) || id.startsWith('manual_') ? id : `${id}（未在已读页中）`).join('、')}</Typography.Paragraph>}
    {picked.size > 0 && <Card size="small" type="inner" className="selected-editor" aria-label="已选片段编辑" title="已选片段：编辑、拆分与合并">
      <Hint>时间使用原录像坐标；合并仅适用于首尾相接的片段。修改后先保存选择版本，再生成短片。</Hint>
      <Flex vertical gap={8}>
        {[...picked.entries()].sort((a, b) => (clockToUs(a[1].start) ?? 0) - (clockToUs(b[1].start) ?? 0)).map(([id, d]) => {
          const start = clockToUs(d.start), end = clockToUs(d.end);
          const point = splitPoints[id] ?? (start !== null && end !== null ? usToClock(Math.floor((start + end) / 2)) : '');
          const index = typeof segments !== 'string' ? segments.findIndex(s => s.segment_id === id) : -1;
          const adjacent = typeof segments !== 'string' && index >= 0 && segments[index + 1]?.start_us === end;
          return <Card size="small" className="selected-item" key={id} title={<span style={{ overflowWrap: 'anywhere', whiteSpace: 'normal' }}>{id}</span>}>
            <Row gutter={8}>
              <Col xs={24} sm={8}><Form.Item label="起点" layout="vertical" style={{ marginBottom: 8 }}><Input aria-label="起点" value={d.start} disabled={busy} onChange={e => edit(id, 'start', e.target.value)} /></Form.Item></Col>
              <Col xs={24} sm={8}><Form.Item label="终点" layout="vertical" style={{ marginBottom: 8 }}><Input aria-label="终点" value={d.end} disabled={busy} onChange={e => edit(id, 'end', e.target.value)} /></Form.Item></Col>
              <Col xs={24} sm={8}><Form.Item label="拆分点" layout="vertical" style={{ marginBottom: 8 }}><Input aria-label="拆分点" value={point} disabled={busy} onChange={e => setSplitPoints(prev => ({ ...prev, [id]: e.target.value }))} /></Form.Item></Col>
            </Row>
            <Space wrap>
              <Button type="primary" ghost disabled={busy || Boolean(problem)} onClick={() => transform(s => splitSelection(s, id, clockToUs(point) ?? -1, `manual_${crypto.randomUUID().replaceAll('-', '')}`))}>拆分片段</Button>
              <Button disabled={busy || Boolean(problem) || !adjacent} onClick={() => transform(s => mergeSelection(s, id))}>与下一片段合并</Button>
              <Button danger disabled={busy} onClick={() => setPicked(prev => { const next = new Map(prev); next.delete(id); return next; })}>移除此片段</Button>
            </Space>
          </Card>;
        })}
        {editError && <Alert type="error" showIcon role="alert" message={editError} />}
      </Flex>
    </Card>}
    <Form layout="inline">
      <Form.Item label="输出帧率">
        <ValueSelect<30 | 60> className="select-fps" aria-label="输出帧率" style={{ width: 120 }} value={fps} disabled={busy} onChange={setFps}
          options={[{ value: 30, label: '30 fps' }, { value: 60, label: '60 fps' }]} />
      </Form.Item>
      <Form.Item>
        <Button type="primary" disabled={busy || Boolean(problem)} onClick={() => act(() => ingestApi.selection(run.run_id, {
          expected_version: run.version, base_plan_revision: run.analysis_revision,
          selected_segments: segments as SelectedSegment[], output: { fps, sample_rate: 48000 }
        }))}>保存选择版本</Button>
      </Form.Item>
    </Form>
    {problem && picked.size > 0 && <Alert type="error" showIcon role="alert" message={problem} />}
    {run.selection_revision > 0 && <Card size="small" type="inner" className="ingest-prepare" title={`当前选择版本 r${run.selection_revision}`}>
      <Hint>{lastSel?.selected_segments.length ?? 0} 段 · {lastSel?.output.fps} fps。生成短片前需绑定一个已保存的处理预设（帧预算按它计算，不调用任何模型）。</Hint>
      <Form layout="vertical">
        <Form.Item label="处理预设">
          <ValueSelect<string> className="select-profile" aria-label="处理预设" value={profileId} disabled={busy} onChange={setProfileId}
            options={[{ value: '', label: '选择预设' }, ...(profiles.data ?? []).map((p) => ({ value: p.profile_id, label: `${p.name} · v${p.revision} · ${p.sampling.max_frames} 帧` }))]} />
        </Form.Item>
      </Form>
      {dirty && <Alert type="warning" showIcon role="status" style={{ marginBottom: 12 }} message="选择或帧率有未保存的修改，请先保存选择版本。" />}
      <Button type="primary" disabled={busy || !profile || dirty} onClick={() => act(() => ingestApi.prepare(run.run_id, {
        expected_version: run.version, plan_revision: run.selection_revision, profile_id: profile!.profile_id, profile_revision: profile!.revision
      }))}>生成短片并登记为 EDL 包</Button>
    </Card>}
  </Flex>;
}

function ReadyFiles({ view }: { view: IngestRunView }) {
  const media = view.files.filter((k) => k.startsWith('media_'));
  const preview = usePreview();
  const open = (k: string) => preview({ kind: 'video', src: ingestFileURL(view.run.run_id, k), title: k.slice('media_'.length), meta: `导入运行 ${view.run.run_id}`, note: '已生成短片（EDL 包媒体）' });
  useEffect(() => { if (media[0]) open(media[0]); }, [view.run.run_id, media[0]]);
  return <div className="ingest-ready">
    <Alert type="success" showIcon style={{ marginBottom: 12 }} message={`已生成 ${media.length} 个短片，资产已切换为 EDL 包，可到「准备」页启动内容流程。内容质量仍需人工验收。`} />
    <Row gutter={[12, 12]}>{media.map((k) => <Col key={k} xs={24} sm={12} xxl={8}>
      <Card size="small" className="glass glass-window" styles={{ body: { padding: 8 } }}>
        <video className="ready-video" controls preload="metadata" src={ingestFileURL(view.run.run_id, k)} />
        <Flex justify="space-between" align="center" gap={8} style={{ marginTop: 6 }}>
          <Typography.Text code ellipsis>{k.slice('media_'.length)}</Typography.Text>
          <Button size="small" onClick={() => open(k)}>送到源监视器</Button>
        </Flex>
      </Card>
    </Col>)}</Row>
  </div>;
}
