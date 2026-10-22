import { useLayoutEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Checkbox, Collapse, Flex, Input, InputNumber, Popconfirm, Space, Steps, Tag, Tooltip, Typography } from 'antd';
import { DownloadOutlined, InfoCircleOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import { api, ApiError } from '../api';
import { apiURL } from '../runtimeUrl';
import { previewFileURL } from '../previewContracts';
import type { ProcessingProfile } from '../preparation';
import type { WorkflowRun } from '../types';
import {
  HIGH_SCORE_DEFAULT, HIGH_SCORE_FLOOR, autoDecisionBody, autoErrorText, autoPanelModel, autoPolling, autoPreflightBlockers, autoSteps,
  bandLabel, buildAutoRunBody, canStartAuto, decisionHeadline, decisionHoldLabel, deliveryNote, noteAutoMissing, noteAutoPresent, reasonLines, shouldRequestAuto, type AutoView
} from '../autoRun';
import { ValueSelect } from './ui';

/** 当前流程的自动模式视图。普通（非自动）流程后端回 404：按「不是自动模式」处理，不当作错误；记下之后不再请求。 */
export function useAutoView(runId: string | undefined) {
  // 404 记在模块级集合里。查询结果回来后这次渲染会把 enabled 关掉，快照刷新不会再打。
  return useQuery<AutoView | null>({
    queryKey: ['auto', runId],
    enabled: shouldRequestAuto(runId),
    retry: false,
    queryFn: async () => {
      if (!runId || !shouldRequestAuto(runId)) return null;
      try {
        const view = await api.getAuto(runId);
        noteAutoPresent(runId);
        return view;
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) { noteAutoMissing(runId); return null; }
        throw e;
      }
    },
    // 出错后不再自动轮询（避免失败后无限请求）；用户点「重试」或事件到来才会再拉。
    refetchInterval: (q) => (q.state.status === 'error' ? false : autoPolling(q.state.data?.state) ? 5000 : false)
  });
}

/** 进度 + 自评结果 + 决策卡 + 下载入口。当前流程不是自动模式时不渲染。 */
export function AutoRunProgress({ run, onGoReview }: { run?: WorkflowRun; onGoReview: () => void }) {
  const client = useQueryClient();
  const auto = useAutoView(run?.run_id);
  const snapshot = useQuery({ queryKey: ['workflow', run?.run_id], queryFn: () => api.getWorkflow(run!.run_id), enabled: Boolean(run) && Boolean(auto.data), retry: false });
  const scripts = useQuery({ queryKey: ['scripts', run?.asset_id], queryFn: () => api.listScripts(run!.asset_id), enabled: Boolean(run) && auto.data?.halt_reason === 'script_changed', retry: false });
  const finalScript = scripts.data?.at(-1);
  const script = useQuery({ queryKey: ['script-workbench', finalScript?.script_id], queryFn: () => api.scriptWorkbench(finalScript!.script_id), enabled: Boolean(finalScript) && auto.data?.halt_reason === 'script_changed', retry: false });
  const [feedbackOpen, setFeedbackOpen] = useState(false);
  const [feedback, setFeedback] = useState('');
  const [alsoConsent, setAlsoConsent] = useState(false);
  const [err, setErr] = useState('');
  const locked = useRef(false);
  const refresh = async () => { await Promise.all(['auto', 'workflows', 'workflow', 'review', 'assets', 'mp4Previews'].map(k => client.invalidateQueries({ queryKey: [k] }))); };
  const act = useMutation({
    mutationFn: async (fn: () => Promise<unknown>) => { try { await fn(); } finally { locked.current = false; } },
    onSuccess: async () => { setErr(''); await refresh(); },
    onError: async (e) => { setErr(autoErrorText(e)); await refresh(); }
  });
  const invoke = (fn: () => Promise<unknown>) => { if (locked.current) return; locked.current = true; act.mutate(fn); };

  if (!run) return null;
  if (auto.isError) {
    return <Alert type="error" showIcon role="alert" className="auto-load-error" message="暂时读不到自动剪辑的进度"
      description="已暂停自动刷新。你可以点右侧「重试」再读一次。"
      action={<Button type="link" size="small" onClick={() => void auto.refetch()} disabled={auto.isFetching}>重试</Button>} />;
  }
  const a = auto.data;
  if (!a) return null;

  const currentRun = snapshot.data?.run ?? run;
  const model = autoPanelModel(a, currentRun);
  const failed = currentRun.status === 'failed';
  const steps = autoSteps(currentRun.stage, model, { failed, decisionReason: a.decision?.reason ?? a.halt_reason });
  const review = a.latest_review;
  const lines = reasonLines(review);
  const decide = (decision: 'export' | 'revise' | 'discard' | 'confirm_visible_set' | 'confirm_script') => invoke(() => api.decideAuto(a.run_id, autoDecisionBody(decision, {
    feedback: decision === 'confirm_script' ? JSON.stringify({ script_id: script.data!.script.script_id, script_version: script.data!.version }) : feedback, expectedAutoVersion: a.auto_version, idempotencyKey: crypto.randomUUID()
  })).then(() => { setFeedback(''); setFeedbackOpen(false); }));
  const revoke = () => invoke(() => api.revokeAuto(a.run_id, { reason: 'user_revoked_from_ui', expected_auto_version: a.auto_version, also_revoke_consent: alsoConsent }));
  const deadline = a.decision?.deadline_at ? dayjs(a.decision.deadline_at).format('YYYY-MM-DD HH:mm') : '';
  const d = a.delivery;
  const holdLabel = decisionHoldLabel(a.delivery_hold);
  const footnote = deliveryNote(d);
  const finished = steps.filter(step => step.status === 'finish').length;

  return (
    <Card size="small" className="auto-progress" data-auto-kind={model.kind} title="自动剪辑进度"
      extra={model.canRevoke && (
        <Popconfirm title="撤销自动授权？" okText="撤销" cancelText="先不撤销" onConfirm={revoke}
          description={<Flex vertical gap={8} style={{ maxWidth: 280 }}>
            <span>撤销后不会再有任何自动动作；已经在进行的步骤不会被强行中断，后续由你手动确认。</span>
            {a.scope.external_egress && <Checkbox checked={alsoConsent} onChange={e => setAlsoConsent(e.target.checked)}>同时撤销该预设版本的外发许可</Checkbox>}
          </Flex>}>
          <Button size="small" danger disabled={act.isPending}>撤销授权</Button>
        </Popconfirm>
      )}>
      <Flex vertical gap={12}>
        <Alert type={model.alert} showIcon message={model.title} description={model.description} />
        <Steps size="small" className="auto-steps" current={finished}
          items={steps.map((step) => ({ title: step.title, status: step.status, description: step.note }))} />
        {review && (
          <Flex vertical gap={4} className="auto-review">
            <Space size={8} wrap>
              <Typography.Text>AI 自评：<Tag bordered={false} color={review.band === 'high' ? 'success' : review.band === 'medium' ? 'warning' : 'error'}>{bandLabel(review.band)}档</Tag></Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>综合 {Math.round(review.score * 100)} 分 · 第 {review.round + 1} 版</Typography.Text>
            </Space>
            {review.band !== 'high' && lines.length > 0 && <ul className="auto-reasons">{lines.map(l => <li key={l}>{l}</li>)}</ul>}
          </Flex>
        )}
        {model.kind === 'decision' && a.decision && (
          <Card size="small" type="inner" className="auto-decision" title={<span>待你确认 <Tag bordered={false}>{a.decision.can_export ? '成片可导出' : '尚无可导出成片'}</Tag>{holdLabel && <> <Tag bordered={false}>{holdLabel}</Tag></>}</span>}>
            <Flex vertical gap={12}>
              <Typography.Text>{decisionHeadline(a.decision.reason, a.decision.can_export).description}</Typography.Text>
              {deadline && <Typography.Text type="secondary" className="auto-deadline">请在 {deadline} 前决定；超时会停止，不会自动导出。</Typography.Text>}
              <Space wrap>
                {a.decision.reason === 'script_changed' && <Button type="primary" disabled={act.isPending || !script.data || script.data.script.status !== 'finalized'} onClick={() => decide('confirm_script')}>确认已定稿脚本修订 {script.data?.version ?? '—'} 并继续</Button>}
                {(a.decision.reason === 'visible_set_changed') && (
                  <Button type="primary" className="vac-cta" disabled={act.isPending} onClick={() => decide('confirm_visible_set')}>确认新的可见集并继续</Button>
                )}
                <Tooltip title={a.decision.can_export ? undefined : '还没有可导出的成片，只能提出修改意见或放弃'}>
                  <Button type="primary" className="vac-cta" disabled={!a.decision.can_export || act.isPending || ['visible_set_changed', 'script_changed'].includes(a.decision.reason)} onClick={() => decide('export')}>直接导出</Button>
                </Tooltip>
                <Tooltip title={a.decision.reason === 'visible_set_changed' ? '可见集变了，请先确认新的可见集' : a.decision.can_revise ? undefined : '重剪次数或额度已用完，不能再提修改意见'}>
                  <Button disabled={!a.decision.can_revise || act.isPending || ['visible_set_changed', 'script_changed'].includes(a.decision.reason)} aria-expanded={feedbackOpen} onClick={() => setFeedbackOpen(o => !o)}>提出修改意见</Button>
                </Tooltip>
                <Popconfirm title="放弃这次自动剪辑结果？" okText="放弃结果" cancelText="继续保留"
                  description="自动模式会停止，已有文件会保留，你仍可手动处理。" onConfirm={() => decide('discard')}>
                  <Button danger disabled={act.isPending}>放弃本次结果</Button>
                </Popconfirm>
              </Space>
              {feedbackOpen && a.decision.can_revise && (
                <Flex vertical gap={8}>
                  <Input.TextArea aria-label="修改意见" rows={3} maxLength={2000} showCount value={feedback} onChange={e => setFeedback(e.target.value)}
                    placeholder="例如：节奏再紧凑一些，多保留对战的镜头，少一点过场" />
                  <div><Button type="primary" disabled={!feedback.trim() || act.isPending} onClick={() => decide('revise')}>按这个意见重新剪辑</Button></div>
                </Flex>
              )}
            </Flex>
          </Card>
        )}
        {model.kind === 'done' && (
          <Flex vertical gap={8}>
            <Space wrap>
              {d.delivered && d.preview_id && <Button type="primary" className="vac-cta" icon={<DownloadOutlined />} href={previewFileURL(d.preview_id, 'mp4', true)} download>下载成片（{d.spec} 预览规格）</Button>}
              {d.package_ready && <Button icon={<DownloadOutlined />} href={apiURL(`/api/workflows/${encodeURIComponent(a.run_id)}/delivery.zip`)}>下载交付包（含原画质分段）</Button>}
              <Button onClick={onGoReview}>去时间线确认</Button>
            </Space>
            {footnote && <Typography.Text type="secondary" className="auto-delivery-note" style={{ fontSize: 12 }}>{footnote}</Typography.Text>}
          </Flex>
        )}
        {model.canTakeOver && <div><Button onClick={onGoReview}>改为手动处理</Button></div>}
        {err && <Alert type="error" showIcon role="alert" closable onClose={() => setErr('')} message={err} />}
      </Flex>
    </Card>
  );
}

/** 一句话 + 全局授权 + 一键启动。 */
export function AutoRunStart({ assetId, onStarted, onGoSettings }: {
  assetId: string;
  onStarted: (run: WorkflowRun) => void;
  onGoSettings: () => void;
}) {
  const client = useQueryClient();
  const profiles = useQuery({ queryKey: ['processing-profiles'], queryFn: api.listProfiles });
  const usable = (profiles.data ?? []).filter((p: ProcessingProfile) => p.content_mode === 'configured');
  const [picked, setPicked] = useState('');
  const profile = usable.find(p => p.profile_id === picked) ?? usable[0];
  const rev = profile?.revision ?? 0;
  const notice = useQuery({ queryKey: ['auto-notice', profile?.profile_id, rev], queryFn: () => api.autoNotice(profile!.profile_id, rev), enabled: Boolean(profile) && rev > 0, retry: false });
  const preflight = useQuery({ queryKey: ['prepared', assetId, profile?.profile_id, rev], queryFn: () => api.preparedPreflight(assetId, profile!.profile_id, rev), enabled: Boolean(assetId && profile && rev > 0), retry: false });
  const [brief, setBrief] = useState('');
  const noticeReady = !notice.isError && Boolean(notice.data?.notice_sha256) && notice.data?.profile_id === profile?.profile_id && notice.data?.revision === rev;
  const identity = assetId && profile && noticeReady ? JSON.stringify([assetId, profile.profile_id, rev, notice.data!.notice_sha256]) : null;
  const authorized = identity !== null;
  const [maxRounds, setMaxRounds] = useState(1);
  const [relaxOn, setRelaxOn] = useState(false);
  const [relaxValue, setRelaxValue] = useState<number | undefined>(0.8);
  const [relaxConfirmed, setRelaxConfirmed] = useState(false);
  const [error, setError] = useState('');
  const starting = useRef(false);
  const attempt = useRef<{ sig: string; key: string }>({ sig: '', key: '' });

  const blockers = autoPreflightBlockers(preflight.data?.checks);
  const preflightOk = Boolean(preflight.data) && Boolean(preflight.data?.expected_asset_version) && blockers.length === 0;
  const inVisibleSet = Boolean(notice.data?.visible_assets?.some((item) => item.asset_id === assetId && item.agent_visible));
  const ok = canStartAuto({
    authorized, profileOk: Boolean(profile), noticeReady, preflightOk, busy: false, relaxOn, relaxValue, relaxConfirmed,
    visibleReady: Boolean(notice.data?.visible_set_sha) && inVisibleSet
  });
  const currentStart = useRef({ identity, ok });
  useLayoutEffect(() => { currentStart.current = { identity, ok }; }, [identity, ok]);

  const start = useMutation({
    mutationFn: async (requestedIdentity: string) => {
      try {
        // mutation 可能排队执行；发送前再次核对当前已提交到界面的授权上下文。
        if (!ok || !currentStart.current.ok || identity !== requestedIdentity || currentStart.current.identity !== requestedIdentity) throw new Error('运行上下文已变化，请重新开始。');
        const base = {
          profileId: profile!.profile_id, revision: rev, expectedAssetVersion: preflight.data!.expected_asset_version!, brief, maxRounds,
          relaxHighScore: relaxOn ? relaxValue : undefined, relaxConfirmed, noticeSha: notice.data!.notice_sha256,
          external: notice.data!.external, visibleSetSha: notice.data!.visible_set_sha
        };
        const sig = JSON.stringify({ assetId, ...base });
        // 相同内容重试沿用同一幂等键（网络抖动不会起两个 run）；内容变了换新键。
        if (attempt.current.sig !== sig) attempt.current = { sig, key: crypto.randomUUID() };
        return await api.startAutoRun(assetId, buildAutoRunBody({ ...base, idempotencyKey: attempt.current.key }));
      } finally { starting.current = false; }
    },
    onSuccess: async ({ run }) => {
      setError('');
      noteAutoPresent(run.run_id);
      await Promise.all(['workflows', 'workflow', 'assets', 'auto', 'prepared', 'audit'].map(k => client.invalidateQueries({ queryKey: [k] })));
      onStarted(run);
    },
    onError: async (e) => { setError(autoErrorText(e)); await Promise.all(['prepared', 'auto-notice'].map(k => client.invalidateQueries({ queryKey: [k] }))); }
  });
  const go = () => { if (starting.current || !ok || !identity) return; starting.current = true; setError(''); start.mutate(identity); };

  if (profiles.isError) {
    return <Alert type="error" showIcon role="alert" message="读取预设失败" description="已暂停自动重试。" action={<Button type="link" size="small" onClick={() => void profiles.refetch()}>重试</Button>} />;
  }
  if (profiles.data && usable.length === 0) {
    return <Alert type="info" showIcon className="auto-no-profile" message="自动剪辑需要一个「已配置模型」的预设"
      description="工程回归预设不调用模型，不能自动剪辑。请先在设置里建一个已配置模型的预设。"
      action={<Button type="primary" size="small" onClick={onGoSettings}>去设置预设</Button>} />;
  }

  return (
    <Flex vertical gap={12} className="auto-start" data-nav-anchor="auto-start">
      <Typography.Text type="secondary">写一句你想要的成片，Agent 会自动完成剪辑、解说、配音、字幕，自检后导出。</Typography.Text>
      <Flex vertical gap={4}>
        <label htmlFor="auto-brief"><Typography.Text strong>你的要求</Typography.Text></label>
        <Input.TextArea id="auto-brief" rows={3} maxLength={2000} showCount value={brief} onChange={e => setBrief(e.target.value)}
          placeholder="例如：剪成 1 分钟的高光集锦，节奏紧凑，只保留团战和关键击杀" />
      </Flex>
      {usable.length > 1 && (
        <Flex vertical gap={4}>
          <Typography.Text strong>使用预设</Typography.Text>
          <ValueSelect<string> className="auto-profile-select" aria-label="自动剪辑预设" style={{ maxWidth: 360 }} value={profile?.profile_id} onChange={v => { setPicked(v); }}
            options={usable.map(p => ({ value: p.profile_id, label: `${p.name} · v${p.revision}` }))} />
        </Flex>
      )}
      {notice.isError && <Alert type="error" showIcon role="alert" message="读取运行说明失败" description="请重试后开始。" action={<Button type="link" size="small" onClick={() => void notice.refetch()}>重试</Button>} />}
      {notice.data && (
        <Alert type={notice.data.external ? 'warning' : 'info'} showIcon className="auto-notice" message={notice.data.external ? '数据会发送到外部服务' : '数据只在本机处理'}
          description={<Flex vertical gap={8}>
            <div style={{ whiteSpace: 'pre-wrap' }} data-testid="auto-notice-text">{notice.data.notice}</div>
            <div>
              <Typography.Text strong>本次运行内容</Typography.Text>
              <ul className="auto-grant-list">
                {(notice.data.grants ?? []).map((grant) => <li key={grant.code}>{grant.label}</li>)}
                {(notice.data.visible_assets ?? []).filter((item) => item.agent_visible).map((item) => (
                  <li key={item.asset_id}>{item.asset_id}：已入库，Agent 可见</li>
                ))}
                {(notice.data.visible_assets ?? []).filter((item) => item.agent_visible).length === 0 && <li>当前没有 Agent 可见素材</li>}
              </ul>
            </div>
          </Flex>} />
      )}
      <Collapse size="small" ghost className="auto-advanced" items={[{
        key: 'adv', label: '高级设置（可选）',
        children: (
          <Flex vertical gap={12}>
            <Flex align="center" gap={8} wrap>
              <span>自评偏低时，最多自动重剪</span>
              <ValueSelect<number> aria-label="最多自动重剪轮数" style={{ width: 96 }} value={maxRounds} onChange={setMaxRounds} options={[{ value: 1, label: '1 轮' }, { value: 2, label: '2 轮' }]} />
            </Flex>
            <Flex vertical gap={8}>
              <Checkbox checked={relaxOn} onChange={e => { setRelaxOn(e.target.checked); if (!e.target.checked) setRelaxConfirmed(false); }}>放宽自评标准（不推荐）</Checkbox>
              {relaxOn && (
                <Flex vertical gap={8} className="auto-relax">
                  <Flex align="center" gap={8} wrap>
                    <span>高档分数线（默认 {HIGH_SCORE_DEFAULT}，最低 {HIGH_SCORE_FLOOR}）</span>
                    <InputNumber aria-label="高档分数线" min={HIGH_SCORE_FLOOR} max={HIGH_SCORE_DEFAULT} step={0.01} precision={2} value={relaxValue} onChange={v => setRelaxValue(v ?? undefined)} />
                  </Flex>
                  <Checkbox checked={relaxConfirmed} onChange={e => setRelaxConfirmed(e.target.checked)}>
                    我明确确认放宽：更容易被判为高档并自动导出，成片质量可能更低。
                  </Checkbox>
                </Flex>
              )}
            </Flex>
          </Flex>
        )
      }]} />
      {preflight.isError && <Alert type="error" showIcon role="alert" className="auto-load-error" message="暂时无法检查能否开始" description="已暂停自动重试。"
        action={<Button type="link" size="small" onClick={() => void preflight.refetch()} disabled={preflight.isFetching}>重试</Button>} />}
      {preflight.data && blockers.length > 0 && <Alert type="warning" showIcon message="还不能开始" description={`还差：${blockers.slice(0, 3).join('；')}`} />}
      {error && <Alert type="error" showIcon role="alert" className="auto-start-error" message={error} />}
      <Flex align="center" gap={12} wrap>
        <Button type="primary" className="vac-cta auto-start-btn" disabled={!ok || start.isPending} loading={start.isPending} onClick={go}>一键自动剪辑</Button>
        {!authorized && <Typography.Text type="secondary" style={{ fontSize: 12 }}><InfoCircleOutlined /> 先勾选「全局授权」才能开始</Typography.Text>}
        {authorized && relaxOn && !relaxConfirmed && <Typography.Text type="secondary" style={{ fontSize: 12 }}><InfoCircleOutlined /> 放宽标准需要你再勾选确认</Typography.Text>}
      </Flex>
    </Flex>
  );
}
