import { useState, type ReactNode } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert, Button, Card, Col, Collapse, Descriptions, Empty, Flex, Form, Input, InputNumber, List, Row, Space, Spin, Steps, Tag, Typography
} from 'antd';
import type { StepProps } from 'antd';
import { DownloadOutlined, RightOutlined } from '@ant-design/icons';
import { api } from '../api';
import type { ReviewView, WorkflowRun } from '../types';
import { usePreview } from './Monitor';
import { Timeline, type TimelineClip } from './Timeline';
import { ValueSelect } from './ui';

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
  return <Flex gap={8} align="flex-end" wrap className="run-picker">
    <Form.Item label="流程历史" layout="vertical" style={{ marginBottom: 0, flex: '1 1 260px', minWidth: 0 }}>
      <ValueSelect<string> className="select-workflow-run" aria-label="流程历史" value={run?.run_id} placeholder="暂无流程"
        onChange={v => selection.setRunId(v)} options={(runs ?? []).map(r => ({ value: r.run_id, label: `${r.run_id} · ${r.status}` }))} />
    </Form.Item>
    <Button disabled={busy || selection.offset === 0} onClick={() => { selection.setOffset(Math.max(0, selection.offset - PAGE)); selection.setRunId(''); }}>较新流程</Button>
    <Button disabled={busy || (runs?.length ?? 0) < PAGE} onClick={() => { selection.setOffset(selection.offset + PAGE); selection.setRunId(''); }}>较早流程</Button>
  </Flex>;
}

const statusColor = (status?: string) => status === 'succeeded' || status === 'ready_for_acceptance' ? 'success'
  : status === 'failed' || status === 'cancelled' ? 'error' : status === 'running' || status === 'claimed' ? 'processing' : 'default';
const stepStatus = (status?: string): StepProps['status'] => status === 'succeeded' ? 'finish'
  : status === 'failed' || status === 'cancelled' ? 'error' : status === 'running' || status === 'claimed' ? 'process' : 'wait';
const decisionColor = (decision?: string) => decision === 'confirmed' ? 'success' : decision === 'rejected' ? 'error' : 'processing';

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

  return <Card className="workflow-panel" aria-labelledby="workflow-title" title={<span id="workflow-title">内容流程与审查</span>}
    extra={current && <Tag color={statusColor(current.status)}>{current.status}</Tag>}>
    <Flex vertical gap={12}>
      <Flex gap={8} align="flex-end" wrap className="workflow-toolbar">
        <Form.Item label="内容模式" layout="vertical" style={{ marginBottom: 0, width: 180 }}>
          <ValueSelect<'configured' | 'builtin'> className="select-content-mode" aria-label="内容模式" value={mode} onChange={setMode}
            options={[{ value: 'configured', label: '已配置模型' }, { value: 'builtin', label: 'builtin 工程回归' }]} />
        </Form.Item>
        <Button type="primary" disabled={busy} title="通用入口；固定预设流程请在「准备」页通过预检后启动" onClick={() => invoke(async () => { const created = await api.startWorkflow(assetId, crypto.randomUUID(), mode); selection.setOffset(0); selection.setRunId(created.run_id); })}>启动新流程</Button>
        <div style={{ flex: '1 1 360px', minWidth: 0 }}><RunPicker runs={runs.data} run={run} selection={selection} busy={runs.isFetching} /></div>
      </Flex>
      <Typography.Text type="secondary">先核对画面证据，再批准解说。固定预设流程建议在「准备」页预检后启动；builtin 只是工程回归，不代表真实识别。</Typography.Text>
      {busy && <Typography.Text type="secondary" role="status">正在保存，请稍候…</Typography.Text>}
      {(failure || runs.error || review.error || snapshot.error) && <Alert type="error" showIcon role="alert" message={failure || errorText(runs.error || review.error || snapshot.error)} />}
      {runs.isSuccess && !current && <Empty description="此资产还没有内容流程。可在「准备」页检查运行条件后启动固定预设流程。" />}
      {current && <>
        <div className="edit-stage">
          <div className="edit-stage-monitor">{monitor}</div>
          <Card size="small" className="review-panel" activeTabKey={activeTab} onTabChange={(key) => setTab(key as 'scenes' | 'narration')}
            tabList={[{ key: 'scenes', label: `场景证据 (${scenes.length})` }, { key: 'narration', label: `解说草稿 (${lines.length})` }]}
            tabBarExtraContent={<Tag bordered={false}>{sceneGate ? '等待场景确认' : draftGate ? '等待解说批准' : current.stage}</Tag>}>
            <Spin spinning={review.isLoading}>
              <div className="review-scroll" role="tabpanel" id="review-scenes" hidden={activeTab !== 'scenes'}>
                {!scenes.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="识别完成后显示场景与证据。" />}
                {review.data?.scenes?.map(scene => <Card size="small" key={scene.scene_id}
                  className={`workflow-card scene-card${focus === `scene:${scene.scene_id}` ? ' is-focused' : ''}`}
                  title={<Typography.Text strong className="scene-title">{scene.label} · {scene.decision || '待确认'}</Typography.Text>}
                  extra={<Tag color={decisionColor(scene.decision)}>{scene.decision || '待确认'}</Tag>}>
                  <Typography.Text type="secondary">置信度：{scene.confidence ?? '未提供'}；模型/方法：{scene.model_version || scene.method || '未提供'}</Typography.Text>
                  <div className="workflow-evidence">{evidence.filter(f => f.scene_id === scene.scene_id).map(f => <figure key={f.sha256}>
                    <button type="button" className="evidence-button" onClick={() => showScene(scene, f.key)} aria-label={`在节目监视器查看 ${scene.label} 源时间 ${f.timestamp} 秒`}>
                      <img loading="lazy" src={evidenceURL(current.run_id, f.key, review.data!.revision.revision_id)} alt={`${scene.label}，源时间 ${f.timestamp} 秒的取样画面`} />
                    </button>
                    <figcaption><Typography.Text type="secondary" style={{ fontSize: 12 }}>源时间 {f.timestamp} 秒</Typography.Text></figcaption>
                  </figure>)}</div>
                  <SceneEdit key={`${review.data.revision.revision_id}-${scene.scene_id}`} scene={scene} disabled={busy || !(sceneGate || current.stage === 'acceptance')} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), scene_id: scene.scene_id, ...fields }))} />
                  <Space wrap>
                    <Button type="primary" disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'confirmed', note }))}>确认此场景</Button>
                    <Button danger disabled={busy || !sceneGate} onClick={() => invoke(() => api.confirmScene(current.run_id, { ...context(), scene_id: scene.scene_id!, decision: 'rejected', note }))}>退回场景</Button>
                  </Space>
                </Card>)}
              </div>
              <div className="review-scroll" role="tabpanel" id="review-narration" hidden={activeTab !== 'narration'}>
                {!lines.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="场景确认、排序后生成草稿。" />}
                {review.data?.narration?.map(line => <Card size="small" key={line.id}
                  className={`workflow-card narration-card${focus === `line:${line.id}` ? ' is-focused' : ''}`}>
                  <DraftEdit key={`${review.data.revision.revision_id}-${line.id}`} line={line} scenes={review.data.scenes ?? []} disabled={busy || !['draft_review', 'acceptance'].includes(current.stage)} save={fields => invoke(() => api.editWorkflow(current.run_id, { ...context(), narration_id: line.id, ...fields }))} />
                  <Flex align="center" gap={8} wrap style={{ margin: '8px 0' }}>
                    <Tag color={line.approved_hash ? 'success' : 'default'}>{line.approved_hash ? '当前版本已批准' : '当前版本未批准'}</Tag>
                    <Typography.Text type="secondary" style={{ overflowWrap: 'anywhere' }}>来源场景 {line.source_scene_id || '未提供'}</Typography.Text>
                  </Flex>
                  <Space wrap>
                    <Button type="primary" disabled={busy || !draftGate || Boolean(line.approved_hash)} onClick={() => invoke(() => api.approveNarration(current.run_id, { ...context(), narration_id: line.id! }))}>批准当前版本</Button>
                    <Button danger disabled={busy || current.status === 'cancelled'} onClick={() => invoke(() => api.revokeWorkflowNarration(current.run_id, line.id!, current.version))}>撤销并停止下游</Button>
                    <Button onClick={() => showLine(line)}>在监视器预览</Button>
                  </Space>
                </Card>)}
              </div>
            </Spin>
          </Card>
        </div>
        <Timeline label="序列时间线" duration={seqEnd > 0 ? seqEnd : Math.max(1, ordered.length)} formatTick={seqEnd > 0 ? (v) => `${Math.round(v * 10) / 10}s` : undefined}
          caption={seqEnd > 0 ? `解说时间轴（秒）· 场景时间窗由所属解说推得 · 点击片段在节目监视器查看证据` : '尚无解说时间：场景按 sequence_rank 等宽显示顺序'}
          tracks={[{ id: 'V1', name: '场景', hint: '颜色：蓝=待确认 绿=已确认 红=已退回', clips: sceneClips }, { id: 'A1', name: '解说', hint: '绿=当前版本已批准', clips: lineClips }]}
          emptyText="识别完成后在此显示场景与解说" />
        <Card size="small" title="流程状态与阶段" className="edit-status">
          <Flex vertical gap={12}>
            <Descriptions size="small" column={{ xs: 1, sm: 2, xl: 4 }} items={[
              { key: 'status', label: '状态 / 阶段', children: `${current.status} / ${current.stage}` },
              { key: 'revision', label: '修订', children: <Typography.Text code style={{ overflowWrap: 'anywhere' }}>{current.current_revision_id}</Typography.Text> },
              { key: 'mode', label: '内容模式', children: current.content_mode },
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
            </Flex>
            {snapshot.data?.stages.length ? <Steps size="small" className="stage-pipeline" aria-label="阶段任务" responsive
              items={snapshot.data.stages.map(s => {
                const task = snapshot.data?.tasks?.find(t => t.task_id === s.task_id);
                const pct = Math.round((task?.progress ?? 0) * 100);
                return { key: s.task_id, status: stepStatus(task?.status),
                  title: <span title={`${s.task_id}${task?.message ? ' · ' + task.message : ''}`} style={s.invalidated ? { textDecoration: 'line-through' } : undefined}>{s.stage}</span>,
                  description: <>{task?.status || '读取中'} · {pct}%<br />回收 {task?.attempts ?? 0} 次{s.invalidated ? ' · 已失效' : s.output_revision_id ? ' · 已交付' : ''}</> };
              })} /> : null}
          </Flex>
        </Card>
      </>}
    </Flex>
  </Card>;
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
  return <Card size="small" className="acceptance-panel" aria-labelledby="acceptance-title" title={<span id="acceptance-title">流程交付与人工验收</span>}
    extra={current && <Tag color={statusColor(current.status)}>{current.status}</Tag>}>
    <Flex vertical gap={12}>
      <RunPicker runs={runs.data} run={run} selection={selection} busy={runs.isFetching} />
      {(failure || runs.error || acceptance.error) && <Alert type="error" showIcon role="alert" message={failure || errorText(runs.error || acceptance.error)} />}
      {runs.isSuccess && !current && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="此资产还没有内容流程，暂无可验收的导出。" />}
      {current && <>
        <Space wrap>
          {ready ? <Button type="primary" icon={<DownloadOutlined />} href={`/api/workflows/${encodeURIComponent(current.run_id)}/delivery.zip`}>下载此版本交付 ZIP</Button>
            : <Typography.Text type="secondary">流程到达 ready_for_acceptance 后才提供此版本 ZIP（当前 {current.status} / {current.stage}）。</Typography.Text>}
          <Button disabled={acceptance.isLoading || Boolean(acceptance.error)} onClick={downloadSummary}>下载验收摘要</Button>
        </Space>
        <Typography.Text type="secondary">导出修订 <Typography.Text code>{current.current_revision_id}</Typography.Text>。请完成对应检查后明确记录结果；旧修订的通过记录不会沿用到当前修订。</Typography.Text>
        <Form.Item label="验收备注（记录失败时必填）" layout="vertical" style={{ marginBottom: 0 }}>
          <Input aria-label="验收备注" value={note} onChange={e => setNote(e.target.value)} placeholder="保留异常时间点或问题描述" />
        </Form.Item>
        {busy && <Typography.Text type="secondary" role="status">正在保存，请稍候…</Typography.Text>}
        <List size="small" bordered className="acceptance-list" dataSource={[...checks]} renderItem={([item, label]) => {
          const row = acceptance.data?.filter(r => r.check_item === item && r.export_revision_id === current.current_revision_id).at(-1);
          return <List.Item key={item} actions={(['passed', 'failed'] as const).map(result => <Button key={result} size="small" type={result === 'passed' ? 'primary' : 'default'} ghost={result === 'passed'} danger={result === 'failed'}
            disabled={busy || !ready || (result === 'failed' && !note.trim())}
            onClick={() => invoke(() => api.recordAcceptance(current.run_id, { expected_version: current.version, check_item: item, result, note }))}>{result === 'passed' ? '记录通过' : '记录失败'}</Button>)}>
            <List.Item.Meta title={label} description={row ? <Tag color={row.result === 'passed' ? 'success' : row.result === 'failed' ? 'error' : 'default'}>
              {row.result === 'passed' ? '通过' : row.result === 'failed' ? '失败' : row.result} · {row.actor}</Tag> : <Tag>待人工验收</Tag>} />
          </List.Item>;
        }} />
        <Collapse size="small" items={[{ key: 'history', label: `历史人工验收记录（按修订保留，${acceptance.data?.length ?? 0} 条）`,
          children: <List size="small" dataSource={acceptance.data ?? []} locale={{ emptyText: '暂无记录' }} renderItem={r => <List.Item key={r.id}>
            <Typography.Text style={{ overflowWrap: 'anywhere' }}>{r.created_at} · <Typography.Text code>{r.export_revision_id}</Typography.Text> · {r.check_item}：{r.result} · {r.actor}{r.note && <><br /><Typography.Text type="secondary">{r.note}</Typography.Text></>}</Typography.Text>
          </List.Item>} /> }]} />
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
