import { useMemo, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from './api';
import type { Asset, CreateTask, GovernancePatch } from './types';
import { parseDependsOn } from './types';
import { useEvents } from './useEvents';
import { ConnectionStatusBar } from './components/ConnectionStatusBar';
import { TaskOrchestrator } from './components/TaskOrchestrator';
import AssetManager from './components/AssetManager';
import AuditLogViewer from './components/AuditLogViewer';
import { DeliveryPanel } from './components/DeliveryPanel';

const taskTypes = ['recognize', 'sort', 'narrate', 'tts', 'subtitle', 'mix', 'export', 'preview'];

function message(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

function stamp(value: string | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toLocaleString('zh-CN');
}

export default function App() {
  const queryClient = useQueryClient();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [watchedTaskId, setWatchedTaskId] = useState<string | null>(null);
  const [taskType, setTaskType] = useState('recognize');
  const [agentRole, setAgentRole] = useState('recognizer');
  const [dependsOn, setDependsOn] = useState('');
  const [search, setSearch] = useState('');
  const { status: connection, ...events } = useEvents(queryClient, watchedTaskId);

  const assets = useQuery({ queryKey: ['assets'], queryFn: api.listAssets });
  const audit = useQuery({ queryKey: ['audit'], queryFn: () => api.listAudit(100) });
  // 任务查询保留在此：编排控制台通过 props 复用它，避免同一任务被两条
  // 15 秒轮询各拉一次。
  const task = useQuery({
    queryKey: ['task', watchedTaskId],
    queryFn: () => api.getTask(watchedTaskId!),
    enabled: Boolean(watchedTaskId),
    refetchInterval: 15_000
  });
  const patch = useMutation({
    mutationFn: ({ id, values }: { id: string; values: GovernancePatch }) => api.patchAsset(id, values),
    onSuccess(updated) {
      queryClient.setQueryData<Asset[]>(['assets'], (previous) =>
        previous?.map((item) => item.asset_id === updated.asset_id ? updated : item));
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
    }
  });
  const createTask = useMutation({
    mutationFn: (input: CreateTask) => api.createTask(input),
    onSuccess(created) {
      setWatchedTaskId(created.task_id);
      setDependsOn('');
      queryClient.setQueryData(['task', created.task_id], created);
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
    }
  });

  const selected = assets.data?.find((item) => item.asset_id === selectedId) ?? assets.data?.[0];
  const visibleAssets = useMemo(() => (assets.data ?? []).filter((item) =>
    item.asset_id.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [assets.data, search]);

  function submitTask(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selected || !agentRole.trim()) return;
    createTask.mutate({
      task_id: `task_${crypto.randomUUID()}`,
      asset_id: selected.asset_id,
      type: taskType,
      agent_role: agentRole.trim(),
      depends_on: parseDependsOn(dependsOn)
    });
  }

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">跳至主要内容</a>
      <header className="topbar">
        <div className="brand"><span className="brand-mark" aria-hidden="true" />
          <div><strong>Video Auto Cut</strong><span>Agent 预处理控制站</span></div>
        </div>
        <span className={`connection connection-${connection}`} role="status" aria-live="polite">
          <span className="connection-dot" aria-hidden="true" />
          {connection === 'connected' ? '事件流已连接'
            : connection === 'connecting' ? '正在连接事件流'
            : connection === 'disconnected' ? '事件流已断开 · 显示最近快照'
            : connection === 'error' ? '事件流连接失败 · 显示最近快照'
            : `事件流重连中（${events.retryCount}/${events.maxRetries}）· 显示最近快照`}
        </span>
      </header>

      <main id="main" className="main-content">
        <div className="page-heading">
          <div><p className="eyebrow">WORKSPACE / OPERATIONS</p><h1>工作台</h1>
            <p>资产治理、任务派发与审计。最终状态以 Go 控制面的响应为准。</p></div>
          <button className="secondary-button" type="button" onClick={() => {
            void queryClient.invalidateQueries({ queryKey: ['assets'] });
            void queryClient.invalidateQueries({ queryKey: ['audit'] });
            if (watchedTaskId) void queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] });
          }}>刷新快照</button>
        </div>

        <ConnectionStatusBar
          status={connection}
          retryCount={events.retryCount}
          maxRetries={events.maxRetries}
          retryExhausted={events.retryExhausted}
          nextRetryLabel={events.nextRetryLabel}
          onReconnect={events.reconnect}
          onDisconnect={events.disconnect}
        />

        <div className="workspace-grid">
          <section className="panel asset-panel" aria-labelledby="assets-title">
            <div className="panel-heading"><div><p className="eyebrow">LIBRARY</p><h2 id="assets-title">资产</h2></div>
              <span className="count">{assets.data?.length ?? '—'} 项</span></div>
            <label className="field-label" htmlFor="asset-search">按资产 ID 筛选</label>
            <input id="asset-search" type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="输入资产 ID" />
            {assets.isPending && <p className="state">正在读取资产…</p>}
            {assets.isError && <div className="state state-error" role="alert">资产读取失败：{message(assets.error)}
              <button type="button" onClick={() => void assets.refetch()}>重试</button></div>}
            {assets.isSuccess && assets.data.length === 0 && <p className="state">暂无资产。素材入库后会出现在这里。</p>}
            {assets.isSuccess && assets.data.length > 0 && visibleAssets.length === 0 && <p className="state">没有匹配的资产。</p>}
            <div className="asset-list">
              {visibleAssets.map((item) => <button key={item.asset_id} type="button"
                className={`asset-row ${selected?.asset_id === item.asset_id ? 'is-selected' : ''}`}
                aria-pressed={selected?.asset_id === item.asset_id}
                onClick={() => setSelectedId(item.asset_id)}>
                <span className="asset-name">{item.asset_id}</span><span className="asset-status">{item.status}</span>
                <small>{item.locked ? '已锁定' : item.agent_visible ? 'Agent 可见' : 'Agent 不可见'}</small>
              </button>)}
            </div>
          </section>

          <div className="content-column">
            <section className="panel" aria-labelledby="detail-title">
              <div className="panel-heading"><div><p className="eyebrow">GOVERNANCE</p><h2 id="detail-title">资产治理</h2></div></div>
              {!selected && <p className="state">选择一项资产以查看状态和治理操作。</p>}
              {selected && <>
                <div className="detail-title"><strong>{selected.asset_id}</strong><span className="pill">{selected.status}</span></div>
                <dl className="detail-grid">
                  <div><dt>Agent 可见</dt><dd>{selected.agent_visible ? '是' : '否'}</dd></div>
                  <div><dt>锁定</dt><dd>{selected.locked ? '是' : '否'}</dd></div>
                  <div><dt>人工批准</dt><dd>{selected.human_approved ? '是' : '否'}</dd></div>
                  <div><dt>允许的角色</dt><dd>{selected.allowed_agents?.join('、') || '未指定'}</dd></div>
                  <div><dt>更新时间</dt><dd>{stamp(selected.updated_at)}</dd></div>
                  <div><dt>产物记录</dt><dd>{Object.keys(selected.artifacts ?? {}).length} 项</dd></div>
                </dl>
                <div className="action-row">
                  <button type="button" disabled={patch.isPending} onClick={() => patch.mutate({ id: selected.asset_id,
                    values: { agent_visible: !selected.agent_visible } })}>
                    {selected.agent_visible ? '设为 Agent 不可见' : '设为 Agent 可见'}</button>
                  <button className="secondary-button" type="button" disabled={patch.isPending}
                    onClick={() => patch.mutate({ id: selected.asset_id, values: { locked: !selected.locked } })}>
                    {selected.locked ? '解锁' : '锁定'}</button>
                  <button className="secondary-button" type="button" disabled={patch.isPending}
                    onClick={() => patch.mutate({ id: selected.asset_id, values: { human_approved: !selected.human_approved } })}>
                    {selected.human_approved ? '撤销批准' : '人工批准'}</button>
                </div>
                {patch.isError && <p className="inline-error" role="alert">更新失败：{message(patch.error)}</p>}
                <p className="hint">产物字段当前仅显示记录数量。文件预览与下载需由服务端提供受控接口。</p>
              </>}
            </section>

            <section className="panel" aria-labelledby="task-title">
              <div className="panel-heading"><div><p className="eyebrow">DISPATCH</p><h2 id="task-title">任务</h2></div></div>
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
                <button type="submit" disabled={!selected || createTask.isPending}>{createTask.isPending ? '提交中…' : '创建任务'}</button>
              </form>
              <p className="hint" id="depends-on-hint">任务绑定当前选中的资产；前置任务须属于同一资产，全部成功后才会被领取，任一失败则本任务随之失败。此阶段按 ID 跟踪任务，服务端尚无任务列表接口。</p>
              {createTask.isError && <p className="inline-error" role="alert">任务创建失败：{message(createTask.error)}</p>}
              <TaskOrchestrator taskId={watchedTaskId} task={task.data}
                isPending={task.isPending} isError={task.isError} error={task.error} />
            </section>
          </div>

          <section className="panel audit-panel" aria-labelledby="audit-title">
            <div className="panel-heading"><div><p className="eyebrow">HISTORY</p><h2 id="audit-title">审计记录</h2></div>
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

        <hr className="section-divider" />

        <div className="governance-column">
          <DeliveryPanel asset={selected} onImported={setSelectedId} />
          <AssetManager onSelect={setSelectedId} />
          <AuditLogViewer />
        </div>
      </main>
    </div>
  );
}
