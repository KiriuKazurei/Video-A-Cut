import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './api';
import type { Asset, CreateTask, GovernancePatch } from './types';
import { useEvents } from './useEvents';
import { ConnectionStatusBar } from './components/ConnectionStatusBar';
import { MediaBin } from './components/MediaBin';
import { Inspector } from './components/Inspector';
import { Icon } from './components/Icon';
import type { RunSelection } from './components/WorkflowPanel';
import { PAGES, useHashPage, type PageId } from './navigation';
import { AssemblyPage, EditPage, ExportPage, ImportPage, MonitorPage, PreparePage } from './pages/Pages';

/**
 * 工作区外壳（Premiere 式）：顶部工作区标签按流程顺序切换页面，左侧项目面板、
 * 右侧属性面板常驻，中间是当前页（含监视器与时间线）。所有页面保持挂载、仅隐藏，
 * 因此跨页切换不会丢失未保存的选段或表单草稿；数据仍以 Go 控制面响应为准。
 */
export default function App() {
  const queryClient = useQueryClient();
  const [page, navigate] = useHashPage();
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [watchedTaskId, setWatchedTaskId] = useState<string | null>(null);
  const [runState, setRunState] = useState({ assetId: '', runId: '', offset: 0 });
  const { status: connection, ...events } = useEvents(queryClient, watchedTaskId);

  const assets = useQuery({ queryKey: ['assets'], queryFn: api.listAssets });
  const audit = useQuery({ queryKey: ['audit'], queryFn: () => api.listAudit(100) });
  // 任务查询保留在此：编排控制台通过 props 复用它，避免同一任务被两条 15 秒轮询各拉一次。
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
      queryClient.setQueryData(['task', created.task_id], created);
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
    }
  });

  const selected = assets.data?.find((item) => item.asset_id === selectedId) ?? assets.data?.[0];
  const assetId = selected?.asset_id ?? '';
  // 编辑页与导出页共享同一资产下选中的内容流程；换资产即回到最新流程。
  const selection = useMemo<RunSelection>(() => {
    const same = runState.assetId === assetId;
    return {
      runId: same ? runState.runId : '',
      offset: same ? runState.offset : 0,
      setRunId: (runId) => setRunState((prev) => ({ assetId, runId, offset: prev.assetId === assetId ? prev.offset : 0 })),
      setOffset: (offset) => setRunState((prev) => ({ assetId, runId: prev.assetId === assetId ? prev.runId : '', offset }))
    };
  }, [runState, assetId]);
  const current = PAGES.find((item) => item.id === page)!;
  const pageDef = (id: PageId) => PAGES.find((item) => item.id === id)!;

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">跳至主要内容</a>
      <header className="topbar">
        <div className="brand"><span className="brand-mark" aria-hidden="true"><Icon name="film" size={16} /></span>
          <div><strong>Video Auto Cut</strong><span>剪辑预处理工作区</span></div>
        </div>
        <nav className="workspace-tabs" aria-label="工作区">
          {PAGES.map((item) => <button key={item.id} type="button" data-page={item.id}
            className={`workspace-tab${page === item.id ? ' is-active' : ''}`}
            aria-current={page === item.id ? 'page' : undefined}
            title={item.description}
            onClick={() => navigate(item.id)}>
            <Icon name={item.id} /><span className="tab-step">{item.step}</span><span className="tab-label">{item.label}</span>
          </button>)}
        </nav>
        <div className="topbar-right">
          <span className={`connection connection-${connection}`} role="status" aria-live="polite">
            <span className="connection-dot" aria-hidden="true" />
            {connection === 'connected' ? '事件流已连接'
              : connection === 'connecting' ? '正在连接事件流'
              : connection === 'disconnected' ? '事件流已断开 · 显示最近快照'
              : connection === 'error' ? '事件流连接失败 · 显示最近快照'
              : `事件流重连中（${events.retryCount}/${events.maxRetries}）· 显示最近快照`}
          </span>
          <button className="secondary-button icon-button" type="button" onClick={() => {
            void queryClient.invalidateQueries({ queryKey: ['assets'] });
            void queryClient.invalidateQueries({ queryKey: ['audit'] });
            if (watchedTaskId) void queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] });
          }}><Icon name="refresh" />刷新快照</button>
        </div>
      </header>

      <div className={`workspace workspace-${page}`}>
        <aside className="dock dock-left" aria-label="项目面板">
          <MediaBin assets={assets} selectedId={selected?.asset_id} onSelect={setSelectedId} />
        </aside>
        <main id="main" className="stage-main" aria-label={`${current.label}页`}>
          <div className="page" data-page-view="import" hidden={page !== 'import'}>
            <ImportPage page={pageDef('import')} asset={selected} onSelect={setSelectedId} />
          </div>
          <div className="page" data-page-view="assembly" hidden={page !== 'assembly'}>
            <AssemblyPage page={pageDef('assembly')} asset={selected} />
          </div>
          <div className="page" data-page-view="prepare" hidden={page !== 'prepare'}>
            <PreparePage page={pageDef('prepare')} asset={selected} />
          </div>
          <div className="page" data-page-view="edit" hidden={page !== 'edit'}>
            <EditPage page={pageDef('edit')} asset={selected} selection={selection} onGoExport={() => navigate('export')} />
          </div>
          <div className="page" data-page-view="export" hidden={page !== 'export'}>
            <ExportPage page={pageDef('export')} asset={selected} selection={selection} />
          </div>
          <div className="page" data-page-view="monitor" hidden={page !== 'monitor'}>
            <MonitorPage page={pageDef('monitor')} asset={selected} audit={audit} task={task}
              watchedTaskId={watchedTaskId} createTask={createTask} />
          </div>
        </main>
        <aside className="dock dock-right" aria-label="属性面板">
          <Inspector asset={selected} patch={patch} onNavigate={navigate} />
        </aside>
      </div>

      <footer className="statusbar">
        <ConnectionStatusBar
          status={connection}
          retryCount={events.retryCount}
          maxRetries={events.maxRetries}
          retryExhausted={events.retryExhausted}
          nextRetryLabel={events.nextRetryLabel}
          onReconnect={events.reconnect}
          onDisconnect={events.disconnect}
        />
        <span className="statusbar-meta">{selected ? `当前资产 ${selected.asset_id}` : '未选择资产'} · {current.step} {current.label}</span>
      </footer>
    </div>
  );
}
