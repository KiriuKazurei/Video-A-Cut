import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Flex, Form, Input, InputNumber, List, Radio, Space, Tag, Typography } from 'antd';
import { skillApi } from '../skillApi';
import { api } from '../api';
import { createSkillEdit, skillPreviewSource } from '../skillEditing';
import { skillQueryKeys, type SkillFlow, type SkillExecutionMode, type SkillPlan, type SkillToolName } from '../skillContracts';
import { useSkillWorkspace } from '../skillWorkspace';
import type { RouteState } from '../navigation';
import { Monitor, formatSeconds, type PreviewSource } from './Monitor';
import { ProtectedError } from './HumanSessionControl';
import { ValueSelect, errorText } from './ui';
import {
  FLOW_LABEL, PAUSE_REASON_LABEL, READONLY_TOOL_ACTIONS, RUN_STATE_LABEL,
  historyMoves, isHumanUnlockRequired, isVersionConflict, newIdempotencyKey,
  protectedQueryRetry, rangeFromSegments, revisionLabel, runDetailKey, splitPlanSegment, versionConflictMessage
} from '../skillView';

type EditMode = 'segment' | 'point' | 'range';
type RunCommand = { project: string; run: string; key: string; controller?: AbortController };

export function SkillRunModule({ navigate, active = true }: { navigate: (state: Partial<RouteState>) => void; active?: boolean }) {
  const workspace = useSkillWorkspace();
  const client = useQueryClient();
  const projectId = workspace.projectId;
  const runId = workspace.runId;
  const [flow, setFlow] = useState<SkillFlow>('lightweight');
  const [skillId, setSkillId] = useState('');
  const [mode, setMode] = useState<SkillExecutionMode>('builtin_api');
  const [goal, setGoal] = useState('');
  const [steps, setSteps] = useState<number | null>(null);
  const [seconds, setSeconds] = useState<number | null>(null);
  const [frames, setFrames] = useState<number | null>(null);
  const [profileId, setProfileId] = useState('');
  const profiles = useQuery({ queryKey: ['processing-profiles'], queryFn: api.listProfiles });
  const selectedProfile = profiles.data?.find(item => item.profile_id === profileId);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [preview, setPreview] = useState<PreviewSource>(null);
  const [historical, setHistorical] = useState(false);
  const [editMode, setEditMode] = useState<EditMode>('segment');
  const [segmentIndex, setSegmentIndex] = useState(0);
  const [rangeEnd, setRangeEnd] = useState(0);
  const [atSeconds, setAtSeconds] = useState(0);
  const [prompt, setPrompt] = useState('');
  const [planDraft, setPlanDraft] = useState<SkillPlan | null>(null);
  const [planBase, setPlanBase] = useState<{ project: string; run: string; version: number } | null>(null);
  const [toolResult, setToolResult] = useState('');
  const createKey = useRef(newIdempotencyKey('run'));
  const editKey = useRef(newIdempotencyKey('edit'));
  const abortRef = useRef<AbortController | null>(null);
  const currentRunKey = JSON.stringify([projectId, runId, workspace.epoch]);
  const activeRunKey = useRef(currentRunKey);
  activeRunKey.current = currentRunKey;

  const project = useQuery({
    queryKey: projectId ? skillQueryKeys.project(projectId) : ['vu-project', 'run-idle'],
    queryFn: ({ signal }) => skillApi.project(projectId, signal),
    enabled: Boolean(projectId),
    retry: protectedQueryRetry
  });
  const visible = useQuery({
    queryKey: projectId ? skillQueryKeys.visible(projectId) : ['vu-visible', 'run-idle'],
    queryFn: ({ signal }) => skillApi.visibleSet(projectId, signal),
    enabled: Boolean(projectId),
    retry: protectedQueryRetry
  });
  const skills = useQuery({ queryKey: skillQueryKeys.skills, queryFn: () => skillApi.skills(), enabled: Boolean(projectId), retry: protectedQueryRetry });
  const runs = useQuery({
    queryKey: projectId ? skillQueryKeys.runs(projectId) : ['vu-runs', 'idle'],
    queryFn: ({ signal }) => skillApi.runs(projectId, signal),
    enabled: Boolean(projectId),
    retry: protectedQueryRetry
  });
  const run = useQuery({
    queryKey: projectId && runId ? skillQueryKeys.run(projectId, runId) : ['vu-run', 'idle'],
    queryFn: ({ signal }) => skillApi.run(runId, signal),
    enabled: Boolean(projectId && runId),
    retry: protectedQueryRetry
  });
  const context = useQuery({
    queryKey: projectId && runId ? skillQueryKeys.context(projectId, runId) : ['vu-context', 'idle'],
    queryFn: ({ signal }) => skillApi.context(runId, signal),
    enabled: Boolean(projectId && runId),
    retry: protectedQueryRetry
  });
  const workbench = useQuery({
    queryKey: projectId && runId ? runDetailKey(projectId, runId, 'workbench') : ['vu-run', 'workbench-idle'],
    queryFn: ({ signal }) => skillApi.workbench(runId, signal),
    enabled: Boolean(projectId && runId),
    retry: protectedQueryRetry
  });
  const history = useQuery({
    queryKey: projectId && runId ? runDetailKey(projectId, runId, 'history') : ['vu-run', 'history-idle'],
    queryFn: () => skillApi.history(runId),
    enabled: Boolean(projectId && runId),
    retry: protectedQueryRetry
  });
  const stepLog = useQuery({
    queryKey: projectId && runId ? runDetailKey(projectId, runId, 'steps') : ['vu-run', 'steps-idle'],
    queryFn: () => skillApi.steps(runId),
    enabled: Boolean(projectId && runId),
    retry: protectedQueryRetry
  });
  const revisions = useQuery({
    queryKey: projectId && runId ? runDetailKey(projectId, runId, 'revisions') : ['vu-run', 'revisions-idle'],
    queryFn: () => skillApi.revisions(runId),
    enabled: Boolean(projectId && runId),
    retry: protectedQueryRetry
  });

  useEffect(() => { if (project.data) setFlow(project.data.flow); }, [project.data?.id, project.data?.flow]);
  useEffect(() => {
    setPreview(null);
    setHistorical(false);
    setPlanDraft(null);
    setPlanBase(null);
    setPrompt('');
    setToolResult('');
    abortRef.current?.abort();
  }, [workspace.epoch]);
  useEffect(() => () => abortRef.current?.abort(), []);
  const reloadRun = async () => {
    if (!projectId || !runId) return;
    await client.invalidateQueries({ queryKey: ['vu-run', projectId, runId] });
    await client.invalidateQueries({ queryKey: ['vu-runs', projectId] });
    await client.invalidateQueries({ queryKey: skillQueryKeys.context(projectId, runId) });
  };
  const fail = (reason: unknown) => {
    setError(isVersionConflict(reason) ? versionConflictMessage() : errorText(reason));
    if (isVersionConflict(reason) || isHumanUnlockRequired(reason)) void reloadRun();
  };
  const action = useMutation({
    mutationFn: (fn: () => Promise<unknown>) => fn(),
    onSuccess: async () => { setError(''); workspace.markUnlocked(); await reloadRun(); },
    onError: fail
  });
  useEffect(() => {
    if (!active) return;
    const edit = (event: Event) => {
      if (!run.data || action.isPending) return;
      event.preventDefault();
      const command = (event as CustomEvent<'undo' | 'redo'>).detail;
      action.mutate(async () => { await skillApi.moveHistory(runId, command, run.data!.version); setHistorical(false); setPreview(null); });
    };
    window.addEventListener('vac:edit-command', edit); return () => window.removeEventListener('vac:edit-command', edit);
  }, [active, run.data, runId, action.isPending]);
  const refreshCommand = (snapshot: RunCommand) => Promise.all([
    client.invalidateQueries({ queryKey: skillQueryKeys.run(snapshot.project, snapshot.run) }),
    client.invalidateQueries({ queryKey: skillQueryKeys.runs(snapshot.project) }),
    client.invalidateQueries({ queryKey: skillQueryKeys.context(snapshot.project, snapshot.run) })
  ]);
  const executeRun = useMutation({
    mutationFn: (snapshot: RunCommand & { version: number; controller: AbortController }) =>
      skillApi.execute(snapshot.run, snapshot.version, snapshot.controller.signal),
    onError: (reason, snapshot) => {
      if (!snapshot.controller.signal.aborted && activeRunKey.current === snapshot.key) fail(reason);
    },
    onSettled: (_data, _error, snapshot) => refreshCommand(snapshot)
  });
  const cancelRun = useMutation({
    mutationFn: async (snapshot: RunCommand) => {
      // Execute advances the CAS while it runs. Read before cancelling and retry
      // one conflict; never retry the execute operation itself.
      for (let attempt = 0; ; attempt++) {
        const latest = await skillApi.run(snapshot.run);
        if (latest.state === 'succeeded' || latest.state === 'cancelled') return latest;
        try { return await skillApi.action(snapshot.run, 'cancel', latest.version); }
        catch (reason) { if (!isVersionConflict(reason) || attempt >= 1) throw reason; }
      }
    },
    onSuccess: (updated, snapshot) => {
      snapshot.controller?.abort();
      if (activeRunKey.current === snapshot.key) {
        setError('');
        setNotice(updated.state === 'cancelled' ? '运行已取消，已停止本次执行请求。' : '运行已结束，已重新读取最新状态。');
      }
    },
    onError: (reason, snapshot) => { if (activeRunKey.current === snapshot.key) fail(reason); },
    onSettled: (_data, _error, snapshot) => refreshCommand(snapshot)
  });
  const executing = executeRun.isPending && executeRun.variables?.key === currentRunKey;
  const cancelling = cancelRun.isPending && cancelRun.variables?.key === currentRunKey;
  const busy = action.isPending || executing || cancelling;

  const selectedSkill = skills.data?.find((item) => item.id === skillId);
  const compatible = (skills.data ?? []).filter((item) => item.compatible_flows.includes(flow) && !(project.data?.disabled_skills ?? []).includes(item.id));
  const moves = historyMoves(history.data);
  const terminal = run.data?.state === 'succeeded' || run.data?.state === 'failed' || run.data?.state === 'cancelled';
  const segments = workbench.data?.segments ?? [];
  const currentPlanDraft = planBase?.project === projectId && planBase.run === runId && planBase.version === run.data?.version;

  const create = () => action.mutate(async () => {
    if (!project.data || !visible.data?.confirmed) throw new Error('当前工程素材尚未载入');
    const created = await skillApi.createRun({
      project_id: projectId,
      flow,
      skill_id: skillId,
      execution_mode: mode,
      visible_set_sha: visible.data.sha256,
      goal: goal.trim(),
      idempotency_key: createKey.current,
      ...(selectedProfile ? { profile_id: selectedProfile.profile_id, profile_revision: selectedProfile.revision } : {}),
      ...(steps || seconds || frames ? { budget: { ...(steps ? { max_steps: steps } : {}), ...(seconds ? { max_seconds: seconds } : {}), ...(frames ? { max_frames: frames } : {}) } } : {})
    });
    createKey.current = newIdempotencyKey('run');
    workspace.setRunId(created.id);
    setNotice(mode === 'external_mcp' ? '运行已创建。外部 Agent 自行领取，这里不配置它的推理 Key。' : '运行已创建。内置模式使用预设里的解说服务。');
  });

  const play = (item: NonNullable<typeof workbench.data>['previews'][number]) => {
    const source = skillPreviewSource(runId, item);
    if (!source) return;
    setHistorical(source.historical);
    setPreview({
      kind: 'video',
      src: source.src,
      title: item.artifact.name,
      meta: source.historical ? '历史预览' : '当前成片',
      note: source.historical ? '历史预览只读，不能直接授权新修改。' : '当前工程的成片预览。'
    });
  };

  const submitEdit = () => action.mutate(async () => {
    if (!workbench.data) throw new Error('工作台尚未载入');
    if (historical) throw new Error('历史预览不能直接发起新修改');
    const selection = editMode === 'segment' ? { segment_index: segmentIndex }
      : editMode === 'point' ? { at_seconds: atSeconds }
        : rangeFromSegments(segments, segmentIndex, rangeEnd);
    const request = createSkillEdit(workbench.data, prompt.trim(), selection, editKey.current);
    await skillApi.requestEdit(runId, request);
    editKey.current = newIdempotencyKey('edit');
    setPrompt('');
    setNotice('修改已提交。新计划需要人工确认后才会继续出片。');
  });

  const authError = project.error || runs.error || run.error || workbench.error;
  const reason = run.data?.reason ? (PAUSE_REASON_LABEL[run.data.reason] ?? run.data.reason) : '';

  return <Flex vertical gap={16} className="skill-run-module">
    {!projectId && <Alert type="info" showIcon message="请先在资源页选择或创建项目" action={<Button onClick={() => navigate({ workspace: 'resources' })}>打开资源页</Button>} />}
    {authError ? <ProtectedError error={authError} onRetry={() => { void project.refetch(); void runs.refetch(); void run.refetch(); void workbench.refetch(); }} /> : null}
    {error && <Alert type={isVersionConflict(run.error) ? 'warning' : 'error'} showIcon message={error} />}
    {notice && <Alert type="info" showIcon message={notice} />}
    {project.data && <Card size="small" title="启动 Skill 运行">
      <Flex vertical gap={12}>
        <Typography.Text type="secondary">流程 A 以口播为主，流程 B 以画面摘要为主，不强制转录。内置模式调用 execute；外部模式由 Agent 领取。</Typography.Text>
        <Flex gap={8} wrap align="flex-end">
          <Form.Item label="素材流程" layout="vertical" style={{ marginBottom: 0, minWidth: 220 }}>
            <ValueSelect<SkillFlow> aria-label="素材流程" value={flow} onChange={setFlow} options={(['lightweight', 'visual_heavy'] as const).map((item) => ({ value: item, label: FLOW_LABEL[item] }))} />
          </Form.Item>
        </Flex>
        <Flex gap={8} wrap align="flex-end">
          <Form.Item label="Skill" layout="vertical" style={{ marginBottom: 0, minWidth: 240, flex: 1 }}>
            <ValueSelect<string> aria-label="运行 Skill" value={skillId} onChange={(value) => { setSkillId(value); const pack = skills.data?.find((item) => item.id === value); if (pack && !pack.execution_modes.includes(mode)) setMode(pack.execution_modes[0]); }}
              options={[{ value: '', label: '选择 Skill' }, ...compatible.map((item) => ({ value: item.id, label: `${item.name} · ${item.version}` }))]} />
          </Form.Item>
          <Form.Item label="Agent 模式" layout="vertical" style={{ marginBottom: 0, minWidth: 200 }}>
            <ValueSelect<SkillExecutionMode> aria-label="Agent 模式" value={mode} onChange={setMode}
              options={(selectedSkill?.execution_modes ?? ['builtin_api', 'external_mcp']).map((item) => ({ value: item, label: item === 'builtin_api' ? '内置 API Agent' : '外部 MCP Agent' }))} />
          </Form.Item>
        </Flex>
        <Input.TextArea aria-label="运行目标" rows={3} value={goal} onChange={(event) => setGoal(event.target.value)} placeholder="这次运行要完成的目标" />
        <Space wrap>
          <span>步数上限</span><InputNumber aria-label="步数上限" min={1} max={64} value={steps ?? undefined} onChange={(value) => setSteps(value)} placeholder="24" />
          <span>秒数上限</span><InputNumber aria-label="秒数上限" min={1} max={3600} value={seconds ?? undefined} onChange={(value) => setSeconds(value)} placeholder="900" />
          <span>抽帧上限</span><InputNumber aria-label="抽帧上限" min={1} max={48} value={frames ?? undefined} onChange={(value) => setFrames(value)} placeholder="24" />
        </Space>
        <Form.Item label="处理预设" style={{ marginBottom: 0 }}><ValueSelect<string> aria-label="运行处理预设" value={profileId} onChange={setProfileId} options={[{ value: '', label: '未指定' }, ...(profiles.data ?? []).map(item => ({ value: item.profile_id, label: `${item.name} · v${item.revision}` }))]} /></Form.Item>
        <Typography.Text type="secondary">资源来自当前工程，服务商调用使用设置中的统一配置。</Typography.Text>
        <Button type="primary" disabled={!skillId || !goal.trim() || !visible.data?.confirmed} loading={action.isPending} onClick={create}>创建运行</Button>
      </Flex>
    </Card>}
    <Card size="small" title="运行列表">
      <List size="small" loading={runs.isFetching} dataSource={runs.data ?? []} locale={{ emptyText: projectId ? '这个项目还没有 Skill 运行' : '未选择项目' }}
        renderItem={(item) => <List.Item actions={[<Button key="open" type="link" onClick={() => workspace.setRunId(item.id)}>打开</Button>]}>
          <Space wrap>
            <Typography.Text strong>{item.goal || item.id}</Typography.Text>
            <Tag color={item.state === 'succeeded' ? 'success' : item.state === 'failed' || item.state === 'cancelled' ? 'error' : item.state === 'waiting_human' ? 'warning' : 'processing'}>{RUN_STATE_LABEL[item.state]}</Tag>
            <Typography.Text type="secondary">{FLOW_LABEL[item.flow]} · {item.execution_mode} · v{item.version}</Typography.Text>
          </Space>
        </List.Item>} />
    </Card>
    {run.data && <Card size="small" title="运行状态" extra={<Tag>v{run.data.version}</Tag>}>
      <Flex vertical gap={8}>
        <Space wrap>
          <Tag color={run.data.state === 'succeeded' ? 'success' : run.data.state === 'failed' ? 'error' : 'processing'}>{RUN_STATE_LABEL[run.data.state]}</Tag>
          {reason && <Typography.Text>暂停原因：{reason}</Typography.Text>}
          <Typography.Text type="secondary">步骤 {run.data.steps}/{run.data.budget.max_steps} · 抽帧 {run.data.frames_spent}/{run.data.budget.max_frames}</Typography.Text>
        </Space>
        {context.data && <Typography.Text type="secondary">冻结 Skill {context.data.skill.name} · 媒体 {context.data.media.length} · 工具 {context.data.tools.join('、')}</Typography.Text>}
        <Space wrap>
          <Button disabled={!run.data.plan || busy} onClick={() => action.mutate(() => skillApi.action(runId, 'finalize', run.data!.version))}>人工定稿</Button>
          <Button disabled={run.data.state !== 'waiting_human' || run.data.reason === 'plan_confirmation' || busy} onClick={() => action.mutate(() => skillApi.action(runId, 'resume', run.data!.version))}>恢复执行</Button>
          <Button danger loading={cancelling} disabled={terminal || action.isPending || cancelling} onClick={() => cancelRun.mutate({ project: projectId, run: runId, key: currentRunKey, controller: abortRef.current ?? undefined })}>取消运行</Button>
          {run.data.execution_mode === 'builtin_api'
            ? <Button type="primary" loading={executing} disabled={terminal || busy} onClick={() => {
              abortRef.current?.abort(); const controller = new AbortController(); abortRef.current = controller;
              executeRun.mutate({ project: projectId, run: runId, key: currentRunKey, version: run.data!.version, controller });
            }}>执行内置 Agent</Button>
            : <Typography.Text type="secondary">外部 Agent 自行领取并调用工具，不需要在这里填写 VAC 推理 Key。</Typography.Text>}
          <Button onClick={() => navigate({ workspace: 'tts', scriptId: '' })}>到 TTS 回写声音</Button>
        </Space>
        {run.data.plan && <Typography.Text type="secondary">{run.data.plan.finalized_by ? `计划 v${run.data.plan.version} 已由 ${run.data.plan.finalized_by} 定稿` : `计划 v${run.data.plan.version} 尚未定稿`}</Typography.Text>}
        <Typography.Text strong>人工步骤</Typography.Text>
        <List size="small" dataSource={stepLog.data ?? []} locale={{ emptyText: '还没有步骤' }} renderItem={(item) => <List.Item>
          <Space wrap><Tag>{item.state}</Tag><Typography.Text>{item.name}</Typography.Text>{item.error && <Typography.Text type="danger">{item.error}</Typography.Text>}</Space>
        </List.Item>} />
        <Typography.Text type="secondary">修订 {revisions.data?.length ?? 0} 条：{(revisions.data ?? []).slice(0, 8).map(revisionLabel).join('、') || '—'}</Typography.Text>
        <Space wrap>
          {READONLY_TOOL_ACTIONS.map((tool) => <Button key={tool.name} disabled={busy} onClick={() => action.mutate(async () => {
            const result = await skillApi.tool(runId, { name: tool.name as SkillToolName, arguments: {}, expected_version: run.data!.version, idempotency_key: newIdempotencyKey(tool.name) });
            setToolResult(JSON.stringify(result.result ?? result.run, null, 2));
          })}>{tool.label}</Button>)}
        </Space>
        <Typography.Text type="secondary">这三项只读取冻结上下文，每次调用仍会计入步数。这里不提供任意工具参数。</Typography.Text>
        {toolResult && <pre className="code-block">{toolResult}</pre>}
      </Flex>
    </Card>}
    {workbench.data && <Card size="small" title="成片工作台">
      <Flex vertical gap={12}>
        <Monitor label={historical ? '历史预览' : '成片'} preview={preview} compact emptyTitle="选择一条预览" emptyHint="当前成片可以发起修改。标成历史预览的版本只能浏览。" />
        <List size="small" dataSource={workbench.data.previews} locale={{ emptyText: '还没有成片预览' }} renderItem={(item) => <List.Item actions={[<Button key="play" type="link" onClick={() => play(item)}>播放</Button>]}>
          <Space wrap>
            <Typography.Text>{item.artifact.name}</Typography.Text>
            {item.historical && <Tag color="warning">历史预览</Tag>}
            {item.is_current && <Tag color="processing">当前</Tag>}
            {item.exportable && <Tag color="success">可导出</Tag>}
          </Space>
        </List.Item>} />
        <Typography.Text strong>源时间与成片时间</Typography.Text>
        <List size="small" dataSource={segments} renderItem={(item) => <List.Item>
          <Space wrap>
            <Typography.Text>片段 {item.segment_index + 1}</Typography.Text>
            <Typography.Text type="secondary">成片 {formatSeconds(item.timeline_start)}–{formatSeconds(item.timeline_end)}</Typography.Text>
            <Typography.Text type="secondary">源 {formatSeconds(item.source_start)}–{formatSeconds(item.source_end)}</Typography.Text>
            <Typography.Text code>{item.media_id}</Typography.Text>
          </Space>
        </List.Item>} />
        <Radio.Group value={editMode} onChange={(event) => setEditMode(event.target.value as EditMode)}
          options={[{ value: 'segment', label: '按片段' }, { value: 'point', label: '按成片时间点' }, { value: 'range', label: '按完整区间' }]} />
        <Space wrap>
          <ValueSelect<number> aria-label="起始片段" value={segmentIndex} onChange={setSegmentIndex} options={segments.map((item) => ({ value: item.segment_index, label: `片段 ${item.segment_index + 1}` }))} />
          {editMode === 'range' && <ValueSelect<number> aria-label="结束片段" value={rangeEnd} onChange={setRangeEnd} options={segments.map((item) => ({ value: item.segment_index, label: `片段 ${item.segment_index + 1}` }))} />}
          {editMode === 'point' && <InputNumber aria-label="成片时间点" min={0} step={0.1} value={atSeconds} onChange={(value) => setAtSeconds(value ?? 0)} addonAfter="秒" />}
        </Space>
        <Input.TextArea aria-label="修改提示词" rows={3} value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="说明要改的内容。成片时间不会被当成源素材时间。" />
        <Button type="primary" disabled={busy || historical || !prompt.trim() || !workbench.data.run.plan} loading={action.isPending} onClick={submitEdit}>提交定向修改</Button>
        {historical && <Typography.Text type="warning">正在查看历史预览，不能直接授权新修改。</Typography.Text>}
      </Flex>
    </Card>}
    {run.data?.plan && <Card size="small" title="人工计划">
      <Flex vertical gap={8}>
        <Button onClick={() => { setPlanDraft(structuredClone(run.data!.plan!)); setPlanBase({ project: projectId, run: runId, version: run.data!.version }); }}>载入当前计划</Button>
        {planDraft && <>
          <Input.TextArea aria-label="计划正文" rows={6} value={planDraft.body} onChange={(event) => setPlanDraft({ ...planDraft, body: event.target.value })} />
          {planDraft.segments.map((segment, index) => <Space key={index} wrap>
            <Typography.Text>片段 {index + 1}</Typography.Text>
            <Input aria-label={`片段${index + 1}旁白`} value={segment.narration} onChange={(event) => setPlanDraft({ ...planDraft, segments: planDraft.segments.map((item, itemIndex) => itemIndex === index ? { ...item, narration: event.target.value } : item) })} />
            <InputNumber aria-label={`片段${index + 1}起点`} value={segment.start} onChange={(value) => setPlanDraft({ ...planDraft, segments: planDraft.segments.map((item, itemIndex) => itemIndex === index ? { ...item, start: value ?? item.start } : item) })} />
            <InputNumber aria-label={`片段${index + 1}终点`} value={segment.end} onChange={(value) => setPlanDraft({ ...planDraft, segments: planDraft.segments.map((item, itemIndex) => itemIndex === index ? { ...item, end: value ?? item.end } : item) })} />
            <Button onClick={() => { try { setPlanDraft(splitPlanSegment(planDraft, index)); } catch (reason) { setError(errorText(reason)); } }}>拆开此段</Button>
          </Space>)}
          {!currentPlanDraft && <Alert type="warning" showIcon message="计划版本或所属运行已变化，请重新载入当前计划后再保存。" />}
          <Button type="primary" disabled={busy || !currentPlanDraft} loading={action.isPending} onClick={() => action.mutate(async () => {
            if (!currentPlanDraft || !planBase) throw new Error(versionConflictMessage());
            await skillApi.savePlan(planBase.run, planBase.version, structuredClone(planDraft));
            setNotice('计划已保存为新修订，需要重新人工定稿。');
          })}>保存计划草案</Button>
        </>}
      </Flex>
    </Card>}
    {run.data && <Card size="small" title="备份与撤销">
      <Flex vertical gap={8}>
        <Space wrap>
          <Button disabled={!moves.undo || busy} onClick={() => action.mutate(async () => { await skillApi.moveHistory(runId, 'undo', run.data!.version); setHistorical(false); setPreview(null); setNotice('已撤销到上一节点。定稿已清除，预算不退还，需要重新定稿。'); })}>撤销</Button>
          <Button disabled={!moves.redo || busy} onClick={() => action.mutate(async () => { await skillApi.moveHistory(runId, 'redo', run.data!.version); setHistorical(false); setPreview(null); setNotice('已重做。恢复后的版本需要重新定稿。'); })}>重做</Button>
        </Space>
        <List size="small" dataSource={history.data?.checkpoints ?? []} locale={{ emptyText: '还没有备份节点' }} renderItem={(item) => {
          const film = item.snapshot.artifacts?.find((artifact) => artifact.kind === 'mp4');
          const current = item.id === history.data?.cursor.checkpoint_id;
          return <List.Item actions={film ? [<Button key="play" type="link" onClick={() => { setHistorical(true); setPreview({ kind: 'video', src: skillApi.checkpointPreviewURL(runId, item.id, film.id), title: film.name, meta: '历史预览', note: '历史预览只读，不能直接授权新修改。' }); }}>浏览</Button>] : undefined}>
            <Space wrap>
              <Typography.Text code>{item.id}</Typography.Text>
              {current && <Tag color="processing">当前节点</Tag>}
              {!current && <Tag color="warning">历史预览</Tag>}
            </Space>
          </List.Item>;
        }} />
      </Flex>
    </Card>}
  </Flex>;
}
