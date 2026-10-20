import { apiURL } from '../runtimeUrl.ts';
import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert, Button, Card, Col, Collapse, Descriptions, Empty, Flex, Form, Input, InputNumber, List, Popconfirm, Row, Space, Spin, Steps, Tag, Tooltip, Typography
} from 'antd';
import type { StepProps } from 'antd';
import { CopyOutlined, DownloadOutlined, QuestionCircleOutlined, RightOutlined } from '@ant-design/icons';
import { api, previewApi, previewQueryKey } from '../api';
import type { ReviewView, WorkflowRun } from '../types';
import { usePreview } from './Monitor';
import { Timeline, type TimelineClip } from './Timeline';
import { ValueSelect } from './ui';
import { createWorkflowPreview, latestWorkflowRun, reviewMatchesCurrent, samePreviewContext, workflowPreviewContext } from '../previewContext';
import { EvidenceSummaryCard } from './EvidenceSummaryCard';
import { previewProductKey } from '../previewContracts';
import { buildEvidenceDigest } from '../evidenceSummary';
import { REVIEW_SHORTCUT_EVENT, type ReviewShortcutAction } from '../hooks/useKeyboardShortcuts';

const checks = [['content_fidelity', '内容是否正确'], ['evidence_trace', '画面与素材是否对得上'], ['browser', '浏览器里能否正常操作'], ['premiere', '能否导入 Premiere'], ['cuts_fps', '切点与帧率是否正常'], ['subtitle_audio_duck', '字幕、旁白与压低是否正常']] as const;
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
  apiURL(`/api/workflows/${encodeURIComponent(runId)}/evidence/${encodeURIComponent(key)}?revision_id=${encodeURIComponent(revisionId)}`);

/** 同一组查询键供编辑页、导出页复用，React Query 自动去重（API-28、30、31、39）。 */
export function useWorkflowData(assetId: string, selection: RunSelection) {
  const runs = useQuery({ queryKey: ['workflows', assetId, selection.offset], queryFn: () => api.listWorkflows(assetId, selection.offset), enabled: Boolean(assetId) });
  const run = runs.data?.find(r => r.run_id === selection.runId) ?? runs.data?.[0];
  const snapshot = useQuery({ queryKey: ['workflow', run?.run_id], queryFn: () => api.getWorkflow(run!.run_id), enabled: Boolean(run) });
  const reviewQuery = useQuery({ queryKey: ['review', run?.run_id], queryFn: () => api.reviewWorkflow(run!.run_id), enabled: Boolean(run) });
  const acceptance = useQuery({ queryKey: ['acceptance', run?.run_id], queryFn: () => api.listAcceptance(run!.run_id), enabled: Boolean(run) });
  const [known, setKnown] = useState<Record<string, WorkflowRun>>({});
  const identity = `${assetId}\0${run?.run_id ?? ''}`;
  const current = latestWorkflowRun(assetId, run?.run_id, [reviewQuery.data?.run, snapshot.data?.run, run, known[identity]]);
  // Guarded render-time adjustment prevents a stale version being committed to the UI.
  if (current && current !== known[identity]) setKnown(previous => ({ ...previous, [identity]: current }));
  const review = { ...reviewQuery, data: reviewMatchesCurrent(reviewQuery.data, current) ? reviewQuery.data : undefined };
  return { runs, run, snapshot, review, acceptance, current };
}

function useWorkflowAction() {
  const client = useQueryClient();
  const [failure, setFailure] = useState('');
  const refresh = async () => { await Promise.all(['workflows', 'workflow', 'review', 'acceptance', 'assets', 'audit'].map(key => client.invalidateQueries({ queryKey: [key] }))); };
  const action = useMutation({ mutationFn: async (fn: () => Promise<unknown>) => fn(), onSuccess: async () => { setFailure(''); await refresh(); }, onError: e => { setFailure(errorText(e)); void refresh(); } });
  return { invoke: (fn: () => Promise<unknown>) => action.mutate(fn), busy: action.isPending, failure };
}

const statusColor = (status?: string) => status === 'succeeded' || status === 'ready_for_acceptance' ? 'success'
  : status === 'failed' || status === 'cancelled' ? 'error' : status === 'running' || status === 'claimed' ? 'processing' : status === 'awaiting_review' ? 'warning' : 'default';
const stepStatus = (status?: string): StepProps['status'] => status === 'succeeded' ? 'finish'
  : status === 'failed' || status === 'cancelled' ? 'error' : status === 'running' || status === 'claimed' ? 'process' : 'wait';
const decisionColor = (decision?: string) => decision === 'confirmed' ? 'success' : decision === 'rejected' ? 'error' : 'processing';

const STATUS_ZH: Record<string, string> = {
  created: '已创建', running: '生成中', claimed: '执行中', awaiting_review: '等待审查',
  ready_for_acceptance: '待验收', succeeded: '已成功', failed: '失败', cancelled: '已取消'
};
const STAGE_ZH: Record<string, string> = {
  recognize: '识别', sort: '排序', scene_review: '场景审查', narrate: '撰稿',
  draft_review: '解说审查', tts: '配音', subtitle: '字幕', mix: '混音', export: '导出', acceptance: '验收'
};
const TASK_ZH: Record<string, string> = {
  succeeded: '已成功', failed: '失败', cancelled: '已取消', running: '运行中', claimed: '已领取',
  pending: '等待中', queued: '排队中', invalidated: '已失效'
};
const shortId = (id?: string) => {
  if (!id) return '—';
  return id.length <= 14 ? id : `${id.slice(0, 8)}…${id.slice(-4)}`;
};
const copyText = async (text: string) => {
  try { await navigator.clipboard.writeText(text); } catch { /* ignore */ }
};

function RunPicker({ runs, run, selection, busy }: { runs: WorkflowRun[] | undefined; run: WorkflowRun | undefined; selection: RunSelection; busy: boolean }) {
  return <Flex gap={8} align="flex-end" wrap className="run-picker">
    <Form.Item label="流程历史" layout="vertical" style={{ marginBottom: 0, flex: '1 1 260px', minWidth: 0 }}>
      <ValueSelect<string> className="select-workflow-run" aria-label="流程历史" value={run?.run_id} placeholder="暂无流程"
        onChange={v => selection.setRunId(v)} options={(runs ?? []).map(r => ({ value: r.run_id, label: `${shortId(r.run_id)} · ${STATUS_ZH[r.status] ?? r.status}` }))} />
    </Form.Item>
    <Button disabled={busy || selection.offset === 0} onClick={() => { selection.setOffset(Math.max(0, selection.offset - PAGE)); selection.setRunId(''); }}>较新流程</Button>
    <Button disabled={busy || (runs?.length ?? 0) < PAGE} onClick={() => { selection.setOffset(selection.offset + PAGE); selection.setRunId(''); }}>较早流程</Button>
  </Flex>;
}

/** 水平流程条：点击切换 run（复用现有 listWorkflows + setRunId）。 */
function RunStrip({ runs, currentId, busy, onSelect }: { runs: WorkflowRun[] | undefined; currentId?: string; busy: boolean; onSelect: (id: string) => void }) {
  if (!runs?.length) return null;
  return (
    <div className="run-strip" role="list" aria-label="流程一览">
      {runs.map((r, i) => {
        const active = r.run_id === currentId;
        return (
          <button type="button" key={r.run_id} role="listitem" disabled={busy}
            className={`run-chip${active ? ' is-active' : ''}`}
            aria-current={active ? 'true' : undefined}
            title={`${r.run_id} · ${r.status} / ${r.stage}`}
            onClick={() => onSelect(r.run_id)}>
            <span className="run-chip-ord">{i + 1}</span>
            <Tag color={statusColor(r.status)} bordered={false}>{STATUS_ZH[r.status] ?? r.status}</Tag>
            <span className="run-chip-meta">{STAGE_ZH[r.stage] ?? r.stage}</span>
            <span className="run-chip-id">{shortId(r.run_id)}</span>
          </button>
        );
      })}
    </div>
  );
}

/** 编辑页：内容流程、阶段、序列时间线、场景审查与解说改稿（API-28~38）。 */
type WorkflowData = ReturnType<typeof useWorkflowData>;
interface WorkflowPanelProps {
  assetId: string;
  selection: RunSelection;
  monitor: ReactNode;
  onGoExport?: () => void;
  onGoTasks?: () => void;
  /** 还没有流程时的主操作：去「导入与粗剪」页底部的「开始生成」 */
  onGoStart?: () => void;
  /** content：侧重改文稿/场景/时间线，不放确认与批准按钮；review：侧重逐项确认场景、批准解说，并带流程控制。 */
  mode?: 'content' | 'review';
  /** 去「审查」页（内容编辑页提示用） */
  onGoReview?: () => void;
}

export function WorkflowPanel({ data, ...props }: WorkflowPanelProps & { data?: WorkflowData }) {
  return data ? <WorkflowContent {...props} data={data} /> : <QueriedWorkflowPanel {...props} />;
}

function QueriedWorkflowPanel(props: WorkflowPanelProps) {
  const data = useWorkflowData(props.assetId, props.selection);
  return <WorkflowContent {...props} data={data} />;
}

function WorkflowContent({ assetId, selection, monitor, onGoExport, onGoTasks, onGoStart, onGoReview, mode: panelMode = 'review', data }: WorkflowPanelProps & { data: WorkflowData }) {
  const isReview = panelMode === 'review';
  const rootRef = useRef<HTMLDivElement>(null);
  const [mode, setMode] = useState<'configured' | 'builtin'>('configured');
  const [note, setNote] = useState('');
  const [focused, setFocused] = useState<{ key: string; owner: ReturnType<typeof workflowPreviewContext> } | null>(null);
  const [tab, setTab] = useState<'scenes' | 'narration' | null>(null);
  const [copied, setCopied] = useState(false);
  const preview = usePreview();
  const { runs, run, snapshot, review, current } = data;
  const owner = workflowPreviewContext(current);
  const focus = review.data && samePreviewContext(focused?.owner, owner) ? focused?.key : null;
  useEffect(() => { setFocused(null); setTab(null); }, [assetId, current?.run_id, current?.current_revision_id]);
  useEffect(() => {
    if (!focus) return;
    const el = document.querySelector<HTMLElement>(`.workflow-card[data-focus-key="${CSS.escape(focus)}"]`);
    el?.scrollIntoView({ block: 'nearest', behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
  }, [focus]);
  const { invoke, busy, failure } = useWorkflowAction();
  const sceneGate = current?.stage === 'scene_review' && current.status === 'awaiting_review';
  const draftGate = current?.stage === 'draft_review' && current.status === 'awaiting_review';
  const context = () => ({ expected_version: current!.version, revision_id: review.data!.revision.revision_id });
  const scenes = review.data?.scenes ?? [];
  const lines = review.data?.narration ?? [];
  const evidence = review.data?.evidence_files ?? [];
  const activeTab = tab ?? (current?.stage === 'draft_review' || (lines.length > 0 && (current?.stage !== 'scene_review' || !isReview)) ? 'narration' : 'scenes');
  const generating = current && (current.status === 'running' || current.status === 'created' || current.status === 'claimed');

  const showScene = (scene: Scene, key?: string) => {
    setFocused({ key: `scene:${scene.scene_id}`, owner }); setTab('scenes');
    preview(scene.scene_id ? createWorkflowPreview(review.data, { kind: 'scene', id: scene.scene_id }, key) : null);
  };
  const showLine = (line: Line) => {
    setFocused({ key: `line:${line.id}`, owner }); setTab('narration');
    preview(line.id ? createWorkflowPreview(review.data, { kind: 'line', id: line.id }) : null);
  };
  const cardActivate = (e: MouseEvent, fn: () => void) => {
    const t = e.target as HTMLElement;
    if (t.closest('button, input, textarea, .ant-select, a, .ant-input-number')) return;
    fn();
    (e.currentTarget as HTMLElement).focus({ preventScroll: true });
  };

  // 审查提速：待处理项与快捷键（C 确认场景 / A 批准解说 / N 下一待处理）。只有当前可见的面板响应，且仅在对应闸口开启时生效。
  const pendingScenes = scenes.filter(sc => sc.decision !== 'confirmed' && sc.decision !== 'rejected');
  const pendingLines = lines.filter(l => !l.approved_hash);
  const focusCard = (key: string) => rootRef.current?.querySelector<HTMLElement>(`.workflow-card[data-focus-key="${CSS.escape(key)}"]`)?.focus({ preventScroll: true });
  const jumpNext = () => {
    if (activeTab === 'scenes') {
      const list = pendingScenes; if (!list.length) return false;
      const at = list.findIndex(sc => focus === `scene:${sc.scene_id}`);
      const next = list[(at + 1) % list.length];
      showScene(next); focusCard(`scene:${next.scene_id}`); return true;
    }
    const list = pendingLines; if (!list.length) return false;
    const at = list.findIndex(l => focus === `line:${l.id}`);
    const next = list[(at + 1) % list.length];
    showLine(next); focusCard(`line:${next.id}`); return true;
  };
  const onShortcut = (action: ReviewShortcutAction) => {
    if (!current || !review.data || busy) return;
    if (action === 'next') { jumpNext(); return; }
    // 审批必须对应此面板当前的实际 DOM 焦点；旧选择/尚未提交的焦点更新不能审批其他项。
    const element = document.activeElement?.closest<HTMLElement>('[data-focus-key]');
    if (!element || !rootRef.current?.contains(element) || element.dataset.focusKey !== focus) return;
    if (action === 'confirm' && isReview && sceneGate) {
      const sc = pendingScenes.find(x => focus === `scene:${x.scene_id}`);
      if (sc?.scene_id) invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: sc.scene_id!, decision: 'confirmed', note }));
    }
    if (action === 'approve' && isReview && draftGate) {
      const ln = pendingLines.find(x => focus === `line:${x.id}`);
      if (ln?.id) invoke(() => api.approveNarration(current.run_id, { ...context(), narration_id: ln.id! }));
    }
  };
  const shortcutRef = useRef(onShortcut); shortcutRef.current = onShortcut;
  useEffect(() => {
    const handle = (e: Event) => {
      const root = rootRef.current;
      if (!root || root.getClientRects().length === 0) return; // 隐藏的页/分区不响应
      const action = (e as CustomEvent<{ action: ReviewShortcutAction }>).detail?.action;
      if (action) shortcutRef.current(action);
    };
    window.addEventListener(REVIEW_SHORTCUT_EVENT, handle);
    return () => window.removeEventListener(REVIEW_SHORTCUT_EVENT, handle);
  }, []);

  const ordered = [...scenes].map((scene, index) => ({ scene, index })).sort((a, b) => (a.scene.sequence_rank ?? a.index) - (b.scene.sequence_rank ?? b.index));
  const timed = lines.filter(l => typeof l.start === 'number' && typeof l.end === 'number' && l.end! > l.start!);
  const seqEnd = timed.reduce((m, l) => Math.max(m, l.end!), 0);
  const sceneTone = (scene: Scene) => scene.decision === 'confirmed' ? 'ok' : scene.decision === 'rejected' ? 'bad' : 'video';
  const sceneClips: TimelineClip[] = seqEnd > 0
    ? ordered.flatMap(({ scene }) => {
      const own = timed.filter(l => l.source_scene_id === scene.scene_id);
      if (!own.length) return [];
      return [{ id: scene.scene_id ?? String(scene.label), start: Math.min(...own.map(l => l.start!)), end: Math.max(...own.map(l => l.end!)), label: scene.label || scene.scene_id || '场景',
        tone: sceneTone(scene), focusKey: `scene:${scene.scene_id}`, active: focus === `scene:${scene.scene_id}`, onSelect: () => showScene(scene), title: `${scene.label} · ${scene.decision || '待确认'}（时间窗由所属解说推得）` }];
    })
    : ordered.map(({ scene }, i) => ({ id: scene.scene_id ?? String(i), start: i, end: i + 1, label: `${i + 1}. ${scene.label || scene.scene_id}`, tone: sceneTone(scene),
      focusKey: `scene:${scene.scene_id}`, active: focus === `scene:${scene.scene_id}`, onSelect: () => showScene(scene), title: `${scene.label} · ${scene.decision || '待确认'}` }));
  const lineClips: TimelineClip[] = timed.map(line => ({ id: line.id ?? `${line.start}`, start: line.start!, end: line.end!, label: line.text || line.id || '解说',
    tone: line.approved_hash ? 'ok' : 'audio', focusKey: `line:${line.id}`, active: focus === `line:${line.id}`, onSelect: () => showLine(line), title: `${line.start}s – ${line.end}s · ${line.approved_hash ? '已批准' : '未批准'} · ${line.text}` }));

  const revisionId = current?.current_revision_id ?? '';
  const doCopy = () => { void copyText(revisionId).then(() => { setCopied(true); setTimeout(() => setCopied(false), 1500); }); };

  return <Card ref={rootRef} className="workflow-panel" data-panel-mode={panelMode} aria-labelledby="workflow-title" title={<span id="workflow-title">{isReview ? '审查：确认场景、批准解说' : '内容编辑：场景与解说文稿'}</span>}
    extra={current && <Tag color={statusColor(current.status)}>{STATUS_ZH[current.status] ?? current.status}</Tag>}>
    <Flex vertical gap={12}>
      <Flex gap={8} align="flex-end" wrap className="workflow-toolbar">
        {isReview && <>
        <Form.Item label="内容模式" layout="vertical" style={{ marginBottom: 0, width: 180 }}>
          <ValueSelect<'configured' | 'builtin'> className="select-content-mode" aria-label="内容模式" value={mode} onChange={setMode}
            options={[{ value: 'configured', label: '已配置模型' }, { value: 'builtin', label: 'builtin 工程回归' }]} />
        </Form.Item>
        <Button type="primary" className="vac-cta" disabled={busy} title="通用入口；固定预设流程请在「准备」页通过预检后启动" onClick={() => invoke(async () => { const created = await api.startWorkflow(assetId, crypto.randomUUID(), mode); selection.setOffset(0); selection.setRunId(created.run_id); })}>启动新流程</Button>
        </>}
        <div style={{ flex: '1 1 360px', minWidth: 0 }}><RunPicker runs={runs.data} run={run} selection={selection} busy={runs.isFetching} /></div>
      </Flex>
      <Typography.Text type="secondary">{isReview
        ? '先看画面、确认场景，再批准解说。点时间线或卡片可在监视器预览。快捷键：C 确认场景 · A 批准解说 · N 跳到下一个待处理。'
        : '在这里修改场景名称、顺序和解说文字，保存后会生成新修订并重新审查。逐项确认与批准请到「审查」页。'}
        {' '}<Tooltip title="「证据」是系统从素材里截取的取样画面，说明为什么识别出这个场景；点缩略图可在节目监视器查看。"><span className="jargon-hint"><QuestionCircleOutlined /> 什么是证据</span></Tooltip></Typography.Text>
      {isReview && current && (sceneGate || draftGate) && <Alert type="info" showIcon className="review-todo"
        message={sceneGate ? `还有 ${pendingScenes.length} 个场景待确认` : `还有 ${pendingLines.length} 段解说待批准`}
        action={<Button size="small" disabled={sceneGate ? !pendingScenes.length : !pendingLines.length} onClick={() => { jumpNext(); }}>跳到下一个 (N)</Button>} />}
      {!isReview && current && (sceneGate || draftGate) && onGoReview && <Alert type="warning" showIcon className="review-todo"
        message={sceneGate ? '流程在等待场景确认' : '流程在等待解说批准'}
        action={<Button type="primary" className="vac-cta" onClick={onGoReview}>去审查</Button>} />}
      {busy && <Typography.Text type="secondary" role="status">正在保存，请稍候…</Typography.Text>}
      {(failure || runs.error || review.error || snapshot.error) && <Alert type="error" showIcon role="alert" message={failure || errorText(runs.error || review.error || snapshot.error)} />}
      {runs.isSuccess && !current && <Empty description="此资产还没有生成内容。先到「导入与粗剪」完成导入，再点「开始生成」。">
        {onGoStart && <Button type="primary" className="vac-cta" onClick={onGoStart}>去开始生成</Button>}
      </Empty>}
      {current && <>
        <div className="workflow-status-strip">
          <Flex gap={8} align="center" wrap>
            <Tag color={statusColor(current.status)}>{STATUS_ZH[current.status] ?? current.status}</Tag>
            <Tag color={sceneGate || draftGate ? 'warning' : 'default'}>{STAGE_ZH[current.stage] ?? current.stage}</Tag>
            {sceneGate && <Tag color="warning">等待场景确认</Tag>}
            {draftGate && <Tag color="warning">等待解说批准</Tag>}
            <Typography.Text type="secondary" className="tech-detail" style={{ fontSize: 12 }}>{current.status} / {current.stage}</Typography.Text>
            <span className="revision-chip" title={revisionId}>
              <Tooltip title="每次保存修改都会生成一个新「修订」，旧修订不会被覆盖；审查与导出都针对当前这一个修订。">修订</Tooltip> <Typography.Text code>{shortId(revisionId)}</Typography.Text>
              <Tooltip title={copied ? '已复制' : '复制完整修订 ID'}>
                <Button type="text" size="small" icon={<CopyOutlined />} aria-label="复制修订 ID" onClick={doCopy} />
              </Tooltip>
            </span>
            {snapshot.data?.profile_binding && (
              <Tag bordered={false}>{snapshot.data.profile_binding.profile_id} · v{snapshot.data.profile_binding.revision}</Tag>
            )}
          </Flex>
          <RunStrip runs={runs.data} currentId={current.run_id} busy={runs.isFetching || busy} onSelect={id => selection.setRunId(id)} />
        </div>
        <div className="edit-stage">
          <div className="edit-stage-monitor">{monitor}</div>
          <Card size="small" className="review-panel" activeTabKey={activeTab} onTabChange={(key) => setTab(key as 'scenes' | 'narration')}
            tabList={[{ key: 'scenes', label: `场景画面 (${scenes.length})` }, { key: 'narration', label: `解说文稿 (${lines.length})` }]}
            tabBarExtraContent={<Tag bordered={false}>{sceneGate ? '等待场景确认' : draftGate ? '等待解说批准' : (STAGE_ZH[current.stage] ?? current.stage)}</Tag>}>
            <Spin spinning={review.isLoading || (review.isFetching && !review.data)}>
              <div className="review-scroll" role="tabpanel" id="review-scenes" hidden={activeTab !== 'scenes'}>
                {!scenes.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="识别完成后会在这里列出场景画面。" />}
                {review.data?.scenes?.map(scene => <Card size="small" key={scene.scene_id}
                  data-focus-key={`scene:${scene.scene_id}`}
                  className={`workflow-card scene-card${focus === `scene:${scene.scene_id}` ? ' is-focused' : ''}`} tabIndex={0}
                  onFocus={e => { if (e.target === e.currentTarget) showScene(scene); }}
                  onClick={e => cardActivate(e, () => showScene(scene))}
                  title={<Typography.Text strong className="scene-title">{scene.label} · {scene.decision || '待确认'}</Typography.Text>}
                  extra={<Tag color={decisionColor(scene.decision)}>{scene.decision || '待确认'}</Tag>}>
                  <Typography.Text type="secondary">置信度：{scene.confidence ?? '未提供'}；模型/方法：{scene.model_version || scene.method || '未提供'}</Typography.Text>
                  <div className="workflow-evidence">{evidence.filter(f => f.scene_id === scene.scene_id).map(f => <figure key={f.sha256}>
                    <button type="button" className="evidence-button" onClick={() => showScene(scene, f.key)} aria-label={`在节目监视器查看 ${scene.label} 源时间 ${f.timestamp} 秒`}>
                      <img loading="lazy" src={evidenceURL(current.run_id, f.key, review.data!.revision.revision_id)} alt={`${scene.label}，源时间 ${f.timestamp} 秒的取样画面`} />
                    </button>
                    <figcaption><Typography.Text type="secondary" style={{ fontSize: 12 }}>源时间 {f.timestamp} 秒</Typography.Text></figcaption>
                  </figure>)}</div>
                  <SceneEdit key={`${review.data!.revision.revision_id}-${scene.scene_id}`} scene={scene} disabled={busy || !(sceneGate || current.stage === 'acceptance')} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), scene_id: scene.scene_id, ...fields }))} />
                  {focus === `scene:${scene.scene_id}` && <Tag color="warning" bordered={false} style={{ marginBottom: 8 }}>正在节目监视器</Tag>}
                  {isReview && <Space wrap>
                    <Button type="primary" className="vac-cta" disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'confirmed', note }))}>确认此场景</Button>
                    <Button danger disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'rejected', note }))}>退回场景</Button>
                  </Space>}
                </Card>)}
              </div>
              <div className="review-scroll" role="tabpanel" id="review-narration" hidden={activeTab !== 'narration'}>
                {!lines.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="场景确认、排序后生成草稿。" />}
                {review.data?.narration?.map(line => <Card size="small" key={line.id}
                  data-focus-key={`line:${line.id}`}
                  className={`workflow-card narration-card${focus === `line:${line.id}` ? ' is-focused' : ''}`} tabIndex={0}
                  onFocus={e => { if (e.target === e.currentTarget) showLine(line); }}
                  onClick={e => cardActivate(e, () => showLine(line))}>
                  <DraftEdit key={`${review.data!.revision.revision_id}-${line.id}`} line={line} scenes={scenes} disabled={busy || !['draft_review', 'acceptance'].includes(current.stage)} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), narration_id: line.id, ...fields }))} />
                  <Flex align="center" gap={8} wrap style={{ margin: '8px 0' }}>
                    <Tag color={line.approved_hash ? 'success' : 'default'}>{line.approved_hash ? '当前版本已批准' : '当前版本未批准'}</Tag>
                    <Typography.Text type="secondary" style={{ overflowWrap: 'anywhere' }}>来源场景 {line.source_scene_id || '未提供'}</Typography.Text>
                    {focus === `line:${line.id}` && <Tag color="warning" bordered={false}>正在节目监视器</Tag>}
                  </Flex>
                  {isReview ? <Space wrap>
                    <Button type="primary" className="vac-cta" disabled={busy || !draftGate || Boolean(line.approved_hash)} onClick={() => invoke(() => api.approveNarration(current.run_id, { ...context(), narration_id: line.id! }))}>批准当前版本</Button>
                    <Button danger disabled={busy || current.status === 'cancelled'} onClick={() => invoke(() => api.revokeWorkflowNarration(current.run_id, line.id!, current.version))}>撤销并停止下游</Button>
                    <Button type="link" size="small" onClick={() => showLine(line)}>节目监视器</Button>
                  </Space> : <Button type="link" size="small" onClick={() => showLine(line)}>在节目监视器预览</Button>}
                </Card>)}
              </div>
            </Spin>
          </Card>
        </div>
        <div className="timeline-legend" aria-label="时间线图例">
          <span><i className="legend-swatch tone-video" />场景待确认</span>
          <span><i className="legend-swatch tone-ok" />已确认 / 已批准</span>
          <span><i className="legend-swatch tone-bad" />已退回</span>
          <span><i className="legend-swatch tone-audio" />解说未批准</span>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>单击片段 → 聚焦卡片并载入节目监视器</Typography.Text>
        </div>
        <Timeline label="序列时间线" duration={seqEnd > 0 ? seqEnd : Math.max(1, ordered.length)} formatTick={seqEnd > 0 ? (v) => `${Math.round(v * 10) / 10}s` : undefined}
          caption={seqEnd > 0 ? `解说时间轴（秒）· 场景时间窗由所属解说推得` : '尚无解说时间：场景按 sequence_rank 等宽显示顺序'}
          tracks={[{ id: 'V1', name: '场景', hint: '蓝=待确认 绿=已确认 红=已退回', clips: sceneClips }, { id: 'A1', name: '解说', hint: '绿=已批准 青=未批准', clips: lineClips }]}
          emptyText="识别完成后在此显示场景与解说" />
        {isReview && <Card size="small" title="流程状态与阶段" className="edit-status">
          <Flex vertical gap={12}>
            <Descriptions size="small" column={{ xs: 1, sm: 2, xl: 4 }} items={[
              { key: 'status', label: '状态 / 阶段', children: `${current.status} / ${current.stage}` },
              { key: 'revision', label: '修订', children: <Typography.Text code style={{ overflowWrap: 'anywhere' }}>{current.current_revision_id}</Typography.Text> },
              { key: 'mode', label: '内容模式', children: current.content_mode === 'builtin' ? 'builtin 工程回归' : current.content_mode === 'configured' ? '已配置模型' : current.content_mode },
              { key: 'profile', label: '处理预设', children: snapshot.data?.profile_binding ? `${snapshot.data.profile_binding.profile_id} / v${snapshot.data.profile_binding.revision}` : '未绑定' }
            ]} />
            {current.blocked_reason && <Alert type="info" showIcon message={current.blocked_reason} />}
            {current.content_mode === 'builtin' && <Alert type="warning" showIcon message="本流程使用模板回归，不代表模型内容识别通过。" />}
            <Flex gap={8} align="flex-end" wrap>
              <Form.Item label="操作原因（取消、退回时填写）" layout="vertical" style={{ marginBottom: 0, flex: '1 1 280px' }}>
                <Input aria-label="操作原因" value={note} onChange={e => setNote(e.target.value)} placeholder="取消流程须填写原因；退回场景时作为备注" />
              </Form.Item>
              <Button danger disabled={busy || ['cancelled', 'ready_for_acceptance'].includes(current.status) || !note.trim()} onClick={() => invoke(() => api.cancelWorkflow(current.run_id, current.version, note))}>取消流程</Button>
              <Button disabled={busy || current.status !== 'failed'} onClick={() => invoke(() => api.retryWorkflow(current.run_id, current.version, current.stage, crypto.randomUUID()))}>重试失败阶段</Button>
              {current.status === 'ready_for_acceptance' && onGoExport && <Button type="primary" ghost icon={<RightOutlined />} iconPosition="end" onClick={onGoExport}>前往导出与验收</Button>}
              {generating && onGoTasks && <Button ghost icon={<RightOutlined />} iconPosition="end" onClick={onGoTasks}>查看生成任务</Button>}
            </Flex>
            {snapshot.data?.stages.length ? <Steps size="small" className="stage-pipeline" aria-label="阶段任务" responsive
              items={snapshot.data.stages.map(s => {
                const task = snapshot.data?.tasks?.find(t => t.task_id === s.task_id);
                const pct = Math.round((task?.progress ?? 0) * 100);
                const st = task?.status;
                const stZh = TASK_ZH[st ?? ''] ?? st ?? '读取中';
                const mark = s.invalidated ? '已失效' : s.output_revision_id ? '已交付' : '';
                return {
                  key: s.task_id,
                  status: s.invalidated ? 'error' : stepStatus(st),
                  title: <span title={`${s.task_id}${task?.message ? ' · ' + task.message : ''}`} className={s.invalidated ? 'stage-invalidated' : undefined}>{STAGE_ZH[s.stage] ?? s.stage}</span>,
                  description: <span className={`stage-desc${s.invalidated ? ' is-invalidated' : ''}${s.output_revision_id && !s.invalidated ? ' is-delivered' : ''}`}>
                    {stZh} · {pct}%
                    {mark ? <> · <Tag color={s.invalidated ? 'error' : 'success'} bordered={false} style={{ marginInlineEnd: 0 }}>{mark}</Tag></> : null}
                    <br />回收 {task?.attempts ?? 0} 次
                  </span>
                };
              })} /> : null}
          </Flex>
        </Card>}
      </>}
    </Flex>
  </Card>;
}

/** 导出页：此流程的交付 ZIP 与按修订记录的人工验收（API-39~41）。 */
export function AcceptancePanel({ assetId, selection }: { assetId: string; selection: RunSelection }) {
  const [note, setNote] = useState('');
  const { runs, run, snapshot, acceptance, current } = useWorkflowData(assetId, selection);
  const { invoke, busy, failure } = useWorkflowAction();
  const ready = current?.status === 'ready_for_acceptance';
  const revisionId = current?.current_revision_id ?? '';
  const runId = current?.run_id ?? '';

  const previewList = useQuery({
    queryKey: previewQueryKey(assetId, runId, revisionId),
    queryFn: () => previewApi.list(runId),
    enabled: Boolean(assetId && runId)
  });
  const deliveryFiles = useQuery({
    queryKey: ['deliveryFiles', assetId],
    queryFn: () => api.listDeliveryFiles(assetId),
    enabled: Boolean(assetId)
  });

  const downloadSummary = () => {
    const url = URL.createObjectURL(new Blob([JSON.stringify({
      run_id: current!.run_id,
      asset_id: assetId,
      content_mode: current!.content_mode,
      current_revision_id: current!.current_revision_id,
      profile_binding: snapshot.data?.profile_binding ?? null,
      records: acceptance.data ?? [],
      preview: previewList.data ?? null,
      delivery_file_count: deliveryFiles.data?.length ?? 0
    }, null, 2)], { type: 'application/json' }));
    const link = document.createElement('a'); link.href = url; link.download = 'acceptance-summary.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
  };

  const latestFor = (item: string) => acceptance.data?.filter(r => r.check_item === item && r.export_revision_id === revisionId).at(-1);
  /** 仅 pending 算剩余；已 failed 不再被批量通过扫入（避免把失败项悄悄改成通过）。 */
  const remaining = checks.filter(([item]) => {
    const r = latestFor(item)?.result;
    return r !== 'passed' && r !== 'failed';
  });
  const failedCount = checks.filter(([item]) => latestFor(item)?.result === 'failed').length;

  const srtPreviews = (previewList.data?.previews ?? []).filter(p => previewProductKey(p) === 'srt');
  const playablePreview = srtPreviews.find(p => p.status === 'succeeded' && !p.invalidated && p.revision_id === revisionId);
  const previewBusy = srtPreviews.some(p => (p.status === 'queued' || p.status === 'claimed' || p.status === 'running') && p.revision_id === revisionId && !p.invalidated);
  const blockedHint = previewList.data?.checks?.filter(c => c.status === 'blocked').map(c => c.message).filter(Boolean).join('；') || undefined;
  const mp4Art = playablePreview?.artifacts?.find(a => a.key === 'mp4' || a.kind === 'preview');

  const digest = buildEvidenceDigest({
    workflowStatus: current?.status,
    workflowStage: current?.stage,
    revisionId: revisionId || undefined,
    runId: runId || undefined,
    checkResults: checks.map(([item, label]) => ({ item, label, result: latestFor(item)?.result })),
    preview: {
      hasPlayable: Boolean(playablePreview),
      inProgress: previewBusy,
      canStart: previewList.data?.can_start,
      blockedHint,
      previewId: playablePreview?.preview_id,
      inputSha256: playablePreview?.input_sha256,
      mp4Sha256: mp4Art?.sha256
    },
    delivery: {
      fileCount: deliveryFiles.data?.length ?? 0,
      hasPlayable: (deliveryFiles.data ?? []).some(f => f.playable)
    },
    packageDownloadReady: ready
  });

  const recordOne = (item: string, result: 'passed' | 'failed') =>
    invoke(() => api.recordAcceptance(current!.run_id, { expected_version: current!.version, check_item: item, result, note: note.trim() || undefined }));
  const passRemaining = () => invoke(async () => {
    // 每条记录会使 run.version +1，批量须递增 expected_version（无 bulk API）；中途失败保留已写入项并报告进度
    let version = current!.version;
    let written = 0;
    const total = remaining.length;
    try {
      for (const [item] of remaining) {
        await api.recordAcceptance(current!.run_id, { expected_version: version, check_item: item, result: 'passed', note: note.trim() || undefined });
        version += 1;
        written += 1;
      }
    } catch (err) {
      throw new Error(`批量通过中断：已写入 ${written}/${total} 项。${errorText(err)}`);
    }
  });

  return <Card size="small" className="acceptance-panel" aria-labelledby="acceptance-title" title={<span id="acceptance-title">人工验收</span>}
    extra={current && <Tag color={statusColor(current.status)}>{STATUS_ZH[current.status] ?? current.status}</Tag>}>
    <Flex vertical gap={12}>
      <RunPicker runs={runs.data} run={run} selection={selection} busy={runs.isFetching} />
      {(failure || runs.error || acceptance.error) && <Alert type="error" showIcon role="alert" message={failure || errorText(runs.error || acceptance.error)} />}
      {runs.isSuccess && !current && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这个素材还没有可验收的成片。请先完成「运行准备」并生成内容。" />}
      {current && <>
        <EvidenceSummaryCard digest={digest} />
        <Space wrap>
          {ready ? <Button type="primary" icon={<DownloadOutlined />} href={apiURL(`/api/workflows/${encodeURIComponent(current.run_id)}/delivery.zip`)}>下载成片包</Button>
            : <Typography.Text type="secondary">成片包将在开放验收后提供。</Typography.Text>}
          <Popconfirm
            title="把剩下未确认的项都记为通过？"
            description={`将写入 ${remaining.length} 项（已标「未通过」的 ${failedCount} 项不会改动）。中途出错会保留已写入的项。`}
            disabled={busy || !ready || remaining.length === 0}
            onConfirm={() => passRemaining()}
            okText="全部通过"
            cancelText="取消"
          >
            <Button type="primary" ghost disabled={busy || !ready || remaining.length === 0}>全部通过剩余 ({remaining.length})</Button>
          </Popconfirm>
          <Button size="small" disabled={acceptance.isLoading || Boolean(acceptance.error)} onClick={downloadSummary}>下载验收摘要</Button>
        </Space>
        <Form.Item label="备注（点「不通过」时必填）" layout="vertical" style={{ marginBottom: 0 }}>
          <Input aria-label="验收备注" value={note} onChange={e => setNote(e.target.value)} placeholder="例如：第 12 秒切点不准" />
        </Form.Item>
        {busy && <Typography.Text type="secondary" role="status">正在保存，请稍候…</Typography.Text>}
        <List size="small" bordered className="acceptance-list" dataSource={[...checks]} renderItem={([item, label]) => {
          const row = latestFor(item);
          const state = row?.result === 'passed' ? 'passed' : row?.result === 'failed' ? 'failed' : 'pending';
          return <List.Item key={item} className={`acceptance-row is-${state}`} actions={(['passed', 'failed'] as const).map(result => <Button key={result} size="small" className="vac-cta" type={result === 'passed' ? 'primary' : 'default'} ghost={result === 'passed'} danger={result === 'failed'}
            disabled={busy || !ready || (result === 'failed' && !note.trim())}
            onClick={() => recordOne(item, result)}>{result === 'passed' ? '通过' : '不通过'}</Button>)}>
            <List.Item.Meta
              title={<span>{label}</span>}
              description={row
                ? <Space size={4} wrap>
                    <Tag color={state === 'passed' ? 'success' : state === 'failed' ? 'error' : 'default'}>{state === 'passed' ? '通过' : state === 'failed' ? '未通过' : row.result}</Tag>
                    {row.note && <Typography.Text type="secondary" style={{ fontSize: 12 }}>{row.note}</Typography.Text>}
                  </Space>
                : <Tag>待确认</Tag>} />
          </List.Item>;
        }} />
        <Collapse size="small" items={[
          { key: 'history', label: `历史记录（${acceptance.data?.length ?? 0} 条）`,
          children: <List size="small" dataSource={acceptance.data ?? []} locale={{ emptyText: '暂无记录' }} renderItem={r => <List.Item key={r.id}>
            <Flex gap={8} wrap align="center" style={{ width: '100%' }}>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>{r.created_at}</Typography.Text>
              <Typography.Text code style={{ fontSize: 12 }}>{shortId(r.export_revision_id)}</Typography.Text>
              <Tag bordered={false}>{checks.find(c => c[0] === r.check_item)?.[1] ?? r.check_item}</Tag>
              <Tag color={r.result === 'passed' ? 'success' : r.result === 'failed' ? 'error' : 'default'}>{r.result === 'passed' ? '通过' : r.result === 'failed' ? '未通过' : r.result}</Tag>
              {r.note && <Typography.Text type="secondary" style={{ flex: '1 1 100%' }}>{r.note}</Typography.Text>}
            </Flex>
          </List.Item>} /> }
        ]} />
      </>}
    </Flex>
  </Card>;
}


function SceneEdit({ scene, disabled, save }: { scene: Scene; disabled: boolean; save: (fields: Record<string, unknown>) => void }) {
  const [label, setLabel] = useState(scene.label || ''); const [rank, setRank] = useState<number | null>(scene.sequence_rank ?? null);
  return <Form layout="vertical" className="scene-edit" disabled={disabled} style={{ margin: '8px 0' }}
    onFinish={() => save({ label, ...(rank !== null ? { sequence_rank: rank } : {}) })}>
    <Row gutter={8} align="bottom">
      <Col flex="1 1 160px"><Form.Item label="场景标签" style={{ marginBottom: 8 }}><Input aria-label="场景标签" required maxLength={100} value={label} onChange={e => setLabel(e.target.value)} /></Form.Item></Col>
      <Col flex="0 0 96px"><Form.Item label="顺序" style={{ marginBottom: 8 }}><InputNumber aria-label="顺序" style={{ width: '100%' }} min={0} step={1} precision={0} value={rank} onChange={v => setRank(v)} /></Form.Item></Col>
      <Col flex="0 0 auto"><Form.Item style={{ marginBottom: 8 }}><Button htmlType="submit">保存新修订</Button></Form.Item></Col>
    </Row>
  </Form>;
}
function DraftEdit({ line, scenes, disabled, save }: { line: Line; scenes: ReviewView['scenes']; disabled: boolean; save: (fields: Record<string, unknown>) => void }) {
  const [text, setText] = useState(line.text || ''); const [start, setStart] = useState<number | null>(line.start ?? null); const [end, setEnd] = useState<number | null>(line.end ?? null);
  const [source, setSource] = useState(line.source_scene_id || '');
  return <Form layout="vertical" className="draft-edit" disabled={disabled} onFinish={() => save({ text, start: Number(start), end: Number(end), source_scene_id: source })}>
    <Form.Item label="解说文本" style={{ marginBottom: 24 }}><Input.TextArea aria-label="解说文本" required maxLength={160} showCount autoSize={{ minRows: 2, maxRows: 5 }} value={text} onChange={e => setText(e.target.value)} /></Form.Item>
    <Row gutter={8} align="bottom">
      <Col xs={24} sm={10}><Form.Item label="来源场景" style={{ marginBottom: 8 }}>
        <ValueSelect<string> className="select-source-scene" aria-label="来源场景" value={source} onChange={setSource}
          options={scenes.map(scene => ({ value: scene.scene_id ?? '', label: scene.label }))} />
      </Form.Item></Col>
      <Col xs={12} sm={7}><Form.Item label="起点（秒）" style={{ marginBottom: 8 }}><InputNumber aria-label="起点（秒）" required style={{ width: '100%' }} min={0} step={0.001} value={start} onChange={v => setStart(v)} /></Form.Item></Col>
      <Col xs={12} sm={7}><Form.Item label="终点（秒）" style={{ marginBottom: 8 }}><InputNumber aria-label="终点（秒）" required style={{ width: '100%' }} min={0} step={0.001} value={end} onChange={v => setEnd(v)} /></Form.Item></Col>
    </Row>
    <Button htmlType="submit">保存新修订并重新审查</Button>
  </Form>;
}
