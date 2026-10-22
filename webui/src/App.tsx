import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Badge, Button, ConfigProvider, Drawer, Grid, Layout, Menu, Select, Space, Tooltip, Typography, theme } from 'antd';
import {
  ExportOutlined, FileSearchOutlined, FileTextOutlined, FolderOpenOutlined, MenuFoldOutlined, MenuUnfoldOutlined, 
  CompassOutlined, PlaySquareOutlined, QuestionCircleOutlined, ReloadOutlined, SettingOutlined, VideoCameraOutlined, SoundOutlined
} from '@ant-design/icons';
import { api, ingestApi } from './api';
import type { CreateTask, WorkflowRun } from './types';
import { useEvents } from './useEvents';
import { refreshSnapshot } from './queryRefresh';
import { ResourceBin } from './components/ResourceBin';
import { ResourceInspector } from './components/ResourceInspector';
import { EditorRightDock } from './components/EditorRightDock';
import { SequenceEditorProvider } from './sequenceEditor';
import { ProjectMenuBar } from './components/ProjectMenuBar';
import { DesktopServiceStatus } from './components/DesktopRuntime';
import { ProjectHome } from './components/ProjectHome';
import { useProjectWorkspace } from './projectWorkspace';
import { WorkspacePage } from './components/WorkspacePage';
import { useWorkflowData, type RunSelection } from './components/WorkflowPanel';
import { WORKSPACES, useWorkspaceRoute, type WorkspaceId } from './navigation';
import { siderTheme } from './theme';
import { useResizable } from './hooks/useResizable';
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts';
import { PipelineStepper } from './components/PipelineStepper';
import { ContextBar } from './components/ContextBar';
import { PipelineContext } from './components/PipelineContext';
import { derivePipeline, type StageTarget } from './pipeline';
import { KeyboardShortcutsModal } from './components/KeyboardShortcutsModal';
import { AppTour } from './components/AppTour';
import { markTourSeen } from './tour';
import { ResourcesWorkspace } from './workspaces/ResourcesWorkspace';
import { EditingWorkspace } from './workspaces/EditingWorkspace';
import { ExportWorkspace } from './workspaces/ExportWorkspace';
import { SettingsWorkspace } from './workspaces/SettingsWorkspace';
import { TtsWorkspace } from './workspaces/TtsWorkspace';
import { SkillWorkspaceProvider } from './skillWorkspace';
import { protectedQueryRetry } from './skillView';

const WORKSPACE_ICONS: Record<WorkspaceId, React.ReactNode> = {
  resources: <FolderOpenOutlined />,
  editing: <PlaySquareOutlined />,
  export: <ExportOutlined />,
  settings: <SettingOutlined />,
  tts: <SoundOutlined />
};

/**
 * 四工作区外壳：顶部导航切换主工作区，左右侧栏响应式折叠。
 * 资源管理、编辑、导出为主流程；设置页独立，从右上角齿轮进入。
 */
export default function App() {
  const projectWorkspace = useProjectWorkspace();
  const queryClient = useQueryClient();
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  
  // Phase 3: 可拖拽侧栏宽度 (200-420px / 220-450px)，双击还原默认并持久化
  const {
    size: leftSiderWidth,
    isDragging: isLeftDragging,
    startDrag: startLeftDrag,
    setSize: setLeftWidth,
    resetToDefault: resetLeftWidth
  } = useResizable({
    initial: screens.xxl ? 280 : 250,
    min: 200,
    max: 420,
    direction: 'horizontal',
    storageKey: 'vac_left_sider_width',
    defaultSize: 260
  });

  const {
    size: rightSiderWidth,
    isDragging: isRightDragging,
    startDrag: startRightDrag,
    setSize: setRightWidth,
    resetToDefault: resetRightWidth
  } = useResizable({
    initial: screens.xxl ? 320 : 280,
    min: 220,
    max: 450,
    direction: 'horizontal',
    reverse: true,
    storageKey: 'vac_right_sider_width',
    defaultSize: 300
  });

  const [leftCollapsed, setLeftCollapsed] = useState(false);
  const [rightCollapsed, setRightCollapsed] = useState(false);
  const [leftDrawerOpen, setLeftDrawerOpen] = useState(false);
  const [rightDrawerOpen, setRightDrawerOpen] = useState(false);
  
  const siderBorder = `1px solid ${token.colorBorderSecondary}`;
  const { route, navigate } = useWorkspaceRoute();
  const { workspace } = route;
  const workspaceState = projectWorkspace.session?.document?.workspace;
  const restored = useRef(false);
  useEffect(() => {
    if (restored.current) return; restored.current = true;
    const next = workspaceState?.workspace;
    navigate({ workspace: typeof next === 'string' && WORKSPACES.some(item => item.id === next) ? next as WorkspaceId : 'resources' });
    if (typeof workspaceState?.left_width === 'number') setLeftWidth(Math.min(420, Math.max(200, workspaceState.left_width)));
    if (typeof workspaceState?.right_width === 'number') setRightWidth(Math.min(450, Math.max(220, workspaceState.right_width)));
    setLeftCollapsed(workspaceState?.left_collapsed === true); setRightCollapsed(workspaceState?.right_collapsed === true);
  }, []);
  useEffect(() => projectWorkspace.registerEditor('workspace-state', { dirty: false, flush: async () => {} }), []);
  useEffect(() => {
    if (!projectWorkspace.session?.document || !restored.current) return;
    return projectWorkspace.registerEditor('workspace-state', { dirty: JSON.stringify([workspaceState?.workspace, workspaceState?.selected_resource ?? '', workspaceState?.left_width, workspaceState?.right_width, workspaceState?.left_collapsed ?? false, workspaceState?.right_collapsed ?? false]) !== JSON.stringify([workspace, projectWorkspace.selectedId, leftSiderWidth, rightSiderWidth, leftCollapsed, rightCollapsed]), flush: async () => { await projectWorkspace.setWorkspaceState({ workspace, selected_resource: projectWorkspace.selectedId, left_width: leftSiderWidth, right_width: rightSiderWidth, left_collapsed: leftCollapsed, right_collapsed: rightCollapsed }); } });
  }, [workspace, projectWorkspace.selectedId, leftSiderWidth, rightSiderWidth, leftCollapsed, rightCollapsed, workspaceState]);


  // 顶栏分段滑块：量出选中项（或选中的「…」）相对页签组的 left/width，写成 CSS 变量。
  // 滑块是 .app-nav.seg-nav::before。首次定位不动画，之后才打开位移动效。
  const navWrapRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const wrap = navWrapRef.current;
    const nav = wrap?.querySelector<HTMLElement>('.app-nav');
    if (!wrap || !nav) return;
    let last = '';
    const measure = () => {
      const target = [...nav.querySelectorAll<HTMLElement>(':scope > .ant-menu-item-selected, :scope > .ant-menu-submenu-selected')]
        .find((el) => el.style.position !== 'absolute' && el.offsetWidth > 0);
      if (!target) { nav.removeAttribute('data-thumb'); return; }
      const box = nav.getBoundingClientRect();
      const rect = target.getBoundingClientRect();
      const x = Math.round((rect.left - box.left - nav.clientLeft) * 100) / 100;
      const w = Math.round(rect.width * 100) / 100;
      const key = `${x}:${w}`;
      if (key === last) return;
      const first = last === '';
      last = key;
      nav.style.setProperty('--nav-thumb-x', `${x}px`);
      nav.style.setProperty('--nav-thumb-w', `${w}px`);
      if (first) {
        nav.setAttribute('data-thumb', 'on');
        // 首次定位不滑动，下一帧再开 #295 的位移动效
        requestAnimationFrame(() => requestAnimationFrame(() => nav.setAttribute('data-thumb-anim', '1')));
      }
    };
    measure();
    const resize = new ResizeObserver(measure);
    resize.observe(nav);
    const mutate = new MutationObserver(measure);
    mutate.observe(nav, { subtree: true, attributes: true, attributeFilter: ['class', 'style'], childList: true });
    void document.fonts?.ready.then(measure);
    return () => { resize.disconnect(); mutate.disconnect(); };
  }, []);

  // Phase 4: 全局键盘快捷键与帮助面板
  const [isShortcutsHelpOpen, setIsShortcutsHelpOpen] = useState(false);
  // 新手引导：首次启动自动弹出一次（自动化浏览器/?tour=0 不弹），页头「新手引导」可随时重开
  const [tourOpen, setTourOpen] = useState(false);
  const closeTour = () => { markTourSeen(window.localStorage); setTourOpen(false); };
  useKeyboardShortcuts({
    route,
    navigate,
    onOpenHelp: () => setIsShortcutsHelpOpen((prev) => !prev),
    isHelpOpen: isShortcutsHelpOpen
  });
  
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [watchedTaskId, setWatchedTaskId] = useState<string | null>(null);
  const [runState, setRunState] = useState({ assetId: '', runId: '', offset: 0 });
  const { status: connection, ...events } = useEvents(queryClient, watchedTaskId);

  const assets = useQuery({ queryKey: ['assets'], queryFn: api.listAssets });
  const audit = useQuery({ queryKey: ['audit', 'recent', 100], queryFn: () => api.listAudit(100), retry: protectedQueryRetry });
  const task = useQuery({
    queryKey: ['task', watchedTaskId],
    queryFn: () => api.getTask(watchedTaskId!),
    enabled: Boolean(watchedTaskId),
    refetchInterval: 15_000
  });
  
  const createTask = useMutation({
    mutationFn: (input: CreateTask) => api.createTask(input),
    onSuccess(created) {
      setWatchedTaskId(created.task_id);
      queryClient.setQueryData(['task', created.task_id], created);
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
    }
  });

  const selected = assets.data?.find((item) => item.asset_id === (projectWorkspace.resource?.asset_id ?? selectedId));
  const assetId = selected?.asset_id ?? '';

  // G4：切换资产时清空观察任务，避免串看上一资产的 task SSE/详情
  const watchedAssetRef = useRef(assetId);
  useEffect(() => {
    if (watchedAssetRef.current !== assetId) {
      watchedAssetRef.current = assetId;
      setWatchedTaskId(null);
    }
  }, [assetId]);
  useEffect(() => {
    if (task.data && assetId && task.data.asset_id !== assetId) {
      setWatchedTaskId(null);
    }
  }, [task.data, assetId]);
  
  // 编辑页与导出页共享同一资产下选中的内容流程
  const selection = useMemo<RunSelection>(() => {
    const same = runState.assetId === assetId;
    return {
      runId: same ? runState.runId : '',
      offset: same ? runState.offset : 0,
      setRunId: (runId) => setRunState((prev) => ({ assetId, runId, offset: prev.assetId === assetId ? prev.offset : 0 })),
      setOffset: (offset) => setRunState((prev) => ({ assetId, runId: prev.assetId === assetId ? prev.runId : '', offset }))
    };
  }, [runState, assetId]);
  
  const current = WORKSPACES.find((item) => item.id === workspace)!;

  // 统一流程模型：阶段条、位置条的「下一步」、二级菜单的阶段标记都来自同一份推导（pipeline.ts）。
  // 与页面使用相同的查询键，React Query 自动去重，不产生额外请求。
  const workflowData = useWorkflowData(assetId, selection);
  const rawAsset = Boolean(selected && (selected.input_kind === 'raw_recording' || selected.ingest_run_id));
  const ingestRuns = useQuery({ queryKey: ['ingest', 'runs', assetId], queryFn: () => ingestApi.runs(assetId), enabled: rawAsset });
  const visibleSet = useQuery({ queryKey: ['visible-set'], queryFn: api.visibleSet });
  const pipeline = useMemo(() => derivePipeline({
    asset: selected,
    assetCount: assets.data?.length ?? 0,
    route,
    ingestRuns: rawAsset ? ingestRuns.data : undefined,
    // 未加载 = undefined；加载成功但没有流程 = null；查询失败走 workflowError（下一步可点「重新加载」）
    workflow: !selected ? null : workflowData.runs.isSuccess ? (workflowData.current ?? null) : workflowData.runs.isError ? null : undefined,
    workflowError: selected && workflowData.runs.isError ? (workflowData.runs.error?.message || '网络或服务异常') : undefined,
    visibleConfirmed: visibleSet.isSuccess ? visibleSet.data.confirmed : undefined
  }), [selected, assets.data?.length, route, rawAsset, ingestRuns.data, workflowData.runs.isSuccess, workflowData.runs.isError, workflowData.runs.error, workflowData.current, visibleSet.isSuccess, visibleSet.data?.confirmed]);

  // 跳转并把目标分区滚动到视野内、聚焦（保持当前资产不变；路由不变时也会聚焦）
  const [focusTick, setFocusTick] = useState(0);
  const pendingAnchor = useRef<string | undefined>(undefined);
  const goTo = (target: StageTarget) => {
    pendingAnchor.current = target.anchor;
    navigate(target.route);
    setFocusTick((n) => n + 1);
  };
  useEffect(() => {
    const anchor = pendingAnchor.current;
    if (!anchor) return;
    pendingAnchor.current = undefined;
    const reduce = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
    let tries = 0;
    const attempt = () => {
      const el = [...document.querySelectorAll<HTMLElement>(`.page:not([hidden]) [data-nav-anchor="${anchor}"]`)].find((node) => node.offsetParent !== null);
      if (!el) { if (++tries < 12) frame = requestAnimationFrame(attempt); return; }
      el.scrollIntoView({ block: 'start', behavior: reduce ? 'auto' : 'smooth' });
      el.focus({ preventScroll: true });
    };
    let frame = requestAnimationFrame(attempt);
    return () => cancelAnimationFrame(frame);
  }, [focusTick]);
  
  // 准备页启动流程后：选中新流程并切到编辑时间线（节目监视查看效果；不再跳「审查」页）
  const onPreparedStarted = (run: WorkflowRun) => {
    setRunState({ assetId, runId: run.run_id, offset: 0 });
    navigate({ workspace: 'editing', editingSection: 'import' });
  };
  

  // 响应式策略：lg 及以下使用抽屉，xl 及以上使用侧栏
  const useSidebar = screens.xl;
  const independentWorkspace = workspace === 'settings' || workspace === 'tts';
  const showLeftSider = useSidebar && !independentWorkspace;
  const showRightSider = useSidebar && !independentWorkspace;

  // 侧栏内容
  const leftSiderContent = (
    <ConfigProvider theme={siderTheme}>
      <ResourceBin onOpenScript={(asset, script) => { setSelectedId(asset); navigate({ workspace: 'editing', editingSection: 'content', scriptId: script }); }} />
    </ConfigProvider>
  );
  
  const rightSiderContent = (
    <ConfigProvider theme={siderTheme}>
      {workspace==='editing'
        ? <EditorRightDock navigate={navigate} active={workspace==='editing'} stripOnly={rightCollapsed} onExpand={()=>setRightCollapsed(false)}/>
        : <ResourceInspector />}
    </ConfigProvider>
  );

  return (
    <SkillWorkspaceProvider key={projectWorkspace.session?.id} projectId={projectWorkspace.session?.document?.project.id}>
    <SequenceEditorProvider active={workspace==='editing'}>
    <PipelineContext.Provider value={{ stages: pipeline.stages, goTo }}>
    <Layout className="app-shell">
      <a className="skip-link" href="#main">跳至主要内容</a>
      <ProjectMenuBar navigate={value => navigate({ workspace: value })} toggleLeft={() => setLeftCollapsed(v => !v)} toggleRight={() => setRightCollapsed(v => !v)} resetLayout={() => { resetLeftWidth(); resetRightWidth(); setLeftCollapsed(false); setRightCollapsed(false); }} refresh={() => { void projectWorkspace.refresh(); void refreshSnapshot(queryClient, watchedTaskId); }} shortcuts={() => setIsShortcutsHelpOpen(true)} tour={() => { navigate({ workspace: 'resources' }); setTourOpen(true); }} />
      {projectWorkspace.error && projectWorkspace.session?.document && <Alert type="error" showIcon message={projectWorkspace.error} />}
      {!projectWorkspace.session?.document && workspace !== 'settings' ? <ProjectHome /> : <>
      
      <Layout.Header className="app-header">
        <div className="app-brand">
          <VideoCameraOutlined style={{ fontSize: 20, color: token.colorPrimary }} />
          <div className="app-brand-text">
            <Typography.Text strong>Video Auto Cut</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>剪辑预处理工作区</Typography.Text>
          </div>
        </div>
        
        {/* 工作区导航：资源、编辑、导出、TTS + 右侧设置 */}
        <div className="app-nav-wrap" ref={navWrapRef} data-tour="nav">
          <Menu 
            className="app-nav seg-nav" 
            mode="horizontal" 
            aria-label="工作区" 
            overflowedIndicatorPopupClassName="app-nav-popup"
            selectedKeys={[workspace]}
            onClick={({ key }) => navigate({ workspace: key as WorkspaceId })}
            items={WORKSPACES.filter(w => w.id !== 'settings').map((item) => ({
              key: item.id, 
              icon: WORKSPACE_ICONS[item.id], 
              title: item.description,
              disabled: !projectWorkspace.session?.document,
              label: <span data-workspace={item.id} aria-current={workspace === item.id ? 'page' : undefined}>{item.label}</span>
            }))} 
          />
        </div>

        {/* Phase 3: 全流程阶段指示器（仅在宽屏 ≥1200px 时在页头居中展示，防止 960-1024px 挤压导航） */}
        {/* ≥1600 在页头放完整阶段条；更窄时改放位置条里的紧凑阶段条（见 ContextBar），避免挤掉「设置」 */}{screens.xxl && workspace !== 'tts' && workspace !== 'resources' && <PipelineStepper stages={pipeline.stages} onGo={goTo} />}
        
        <div className="app-header-right">
          {!useSidebar && !independentWorkspace && (
            <>
              <Button 
                size="small"
                icon={<MenuUnfoldOutlined />} 
                onClick={() => setLeftDrawerOpen(true)}
              >
                资源
              </Button>
              <Button 
                size="small"
                icon={<FileTextOutlined />} 
                onClick={() => setRightDrawerOpen(true)}
              >
                详情
              </Button>
            </>
          )}
          
          

        </div>
      </Layout.Header>

      <Layout hasSider={Boolean(showLeftSider || showRightSider)} className={`workspace workspace-${workspace}`} style={{ '--vac-sider-border': siderBorder } as React.CSSProperties}>
        {/* 左侧栏：xl 及以上显示，lg 及以下折叠，Phase 3 支持宽度拖拽 */}
        {showLeftSider && (
          <Layout.Sider 
            theme="light" 
            width={leftSiderWidth}
            collapsedWidth={0}
            collapsed={leftCollapsed}
            collapsible
            trigger={null}
            className={`dock dock-left dock-resizable${isLeftDragging ? " is-dragging-resize" : ""}`} 
            aria-label="项目面板" 
            style={{ borderInlineEnd: siderBorder }}
          >
            {leftSiderContent}
            <div
              className={`sider-resizer sider-resizer-left ${isLeftDragging ? 'is-dragging' : ''}`}
              onMouseDown={startLeftDrag}
              onDoubleClick={resetLeftWidth}
              title="左右拖拽调整项目面板宽度，双击重置 (260px)"
              role="separator"
              aria-label="左侧栏宽度调整手柄"
            />
          </Layout.Sider>
        )}
        
        <Layout.Content id="main" className="stage-main" aria-label={`${current.label}工作区`}>
          {workspace==='editing'&&useSidebar&&<div className="sider-toggles editor-sider-toggles"><Button type="text" size="small" icon={leftCollapsed?<MenuUnfoldOutlined/>:<MenuFoldOutlined/>} onClick={()=>setLeftCollapsed(!leftCollapsed)} aria-label={leftCollapsed?'展开资源面板':'收起资源面板'}/><Button type="text" size="small" icon={rightCollapsed?<MenuUnfoldOutlined/>:<MenuFoldOutlined/>} onClick={()=>setRightCollapsed(!rightCollapsed)} aria-label={rightCollapsed?'展开片段属性面板':'收起片段属性面板'}/></div>}
          {workspace !== 'resources' && workspace!=='editing' && <ContextBar
            route={route}
            assetId={workspace === 'tts' ? undefined : selected?.asset_id}
            next={pipeline.next}
            onGo={goTo}
            onRetryWorkflow={() => { void workflowData.runs.refetch(); }}
            onNavigate={navigate}
            stepper={!screens.xxl && workspace !== 'tts' ? <PipelineStepper compact stages={pipeline.stages} onGo={goTo} /> : undefined}
            extra={useSidebar && !independentWorkspace ? (
              <div className="sider-toggles">
                <Button type="text" size="small" icon={leftCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                  onClick={() => setLeftCollapsed(!leftCollapsed)} title={leftCollapsed ? '展开项目面板' : '收起项目面板'} aria-label={leftCollapsed ? '展开项目面板' : '收起项目面板'} />
                <Button type="text" size="small" icon={rightCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                  onClick={() => setRightCollapsed(!rightCollapsed)} title={rightCollapsed ? '展开属性面板' : '收起属性面板'} aria-label={rightCollapsed ? '展开属性面板' : '收起属性面板'} />
              </div>
            ) : undefined}
          />}

          <WorkspacePage pageId="tts" active={workspace === 'tts'}>
            <TtsWorkspace active={workspace === 'tts'} route={route} navigate={navigate} onReturnToScript={(assetId, scriptId) => { setSelectedId(assetId); navigate({ workspace: 'editing', editingSection: 'content', scriptId }); }} />
          </WorkspacePage>
          <WorkspacePage pageId="resources" active={workspace === 'resources'}>
            <ResourcesWorkspace active={workspace === 'resources'} onOpenScript={(asset, script) => { setSelectedId(asset); navigate({ workspace: 'editing', editingSection: 'content', scriptId: script }); }} />
          </WorkspacePage>
          
          <WorkspacePage pageId="editing" active={workspace === 'editing'}>
            <EditingWorkspace 
              route={route}
              navigate={navigate}
              asset={selected} 
              selection={selection}
              onSelect={setSelectedId}
              onPreparedStarted={onPreparedStarted}
            />
          </WorkspacePage>
          
          <WorkspacePage pageId="export" active={workspace === 'export'}>
            <ExportWorkspace 
              route={route}
              navigate={navigate}
              asset={selected} 
              selection={selection}
              audit={audit}
              task={task}
              watchedTaskId={watchedTaskId}
              onWatchTask={setWatchedTaskId}
              createTask={createTask}
            />
          </WorkspacePage>
          
          <WorkspacePage pageId="settings" active={workspace === 'settings'}>
            <SettingsWorkspace 
              route={route}
              navigate={navigate}
              asset={selected}
              onStarted={onPreparedStarted}
              audit={audit}
              connection={connection}
              events={events}
              controlPlane={assets}
            />
          </WorkspacePage>
        </Layout.Content>
        
        {/* 右侧栏：xl 及以上显示，lg 及以下折叠，Phase 3 支持宽度拖拽 */}
        {showRightSider && (
          <Layout.Sider 
            theme="light" 
            width={rightSiderWidth}
            collapsedWidth={workspace==='editing' ? 36 : 0}
            collapsed={rightCollapsed}
            collapsible
            trigger={null}
            className={`dock dock-right dock-resizable${isRightDragging ? " is-dragging-resize" : ""}${workspace==='editing'&&rightCollapsed?' is-strip-collapsed':''}`} 
            aria-label="属性面板" 
            style={{ borderInlineStart: siderBorder }}
          >
            <div
              className={`sider-resizer sider-resizer-right ${isRightDragging ? 'is-dragging' : ''}`}
              onMouseDown={startRightDrag}
              onDoubleClick={resetRightWidth}
              title="左右拖拽调整属性面板宽度，双击重置 (300px)"
              role="separator"
              aria-label="右侧栏宽度调整手柄"
            />
            {rightSiderContent}
          </Layout.Sider>
        )}
      </Layout>

      {/* 移动端抽屉 */}
      <Drawer
        title="项目面板"
        placement="left"
        open={leftDrawerOpen && !independentWorkspace}
        onClose={() => setLeftDrawerOpen(false)}
        width={280}
      >
        {leftSiderContent}
      </Drawer>
      
      <Drawer
        title={workspace==='editing'?'编辑器面板':'属性面板'}
        placement="right"
        open={rightDrawerOpen && !independentWorkspace}
        onClose={() => setRightDrawerOpen(false)}
        width={320}
      >
        {workspace==='editing'
          ? <ConfigProvider theme={siderTheme}><EditorRightDock navigate={navigate} active stripOnly={false}/></ConfigProvider>
          : rightSiderContent}
      </Drawer>

      </>}

      <Layout.Footer className="app-footer">
        <Typography.Text type="secondary" className="app-footer-meta">
          {!projectWorkspace.session?.document ? '未打开工程' : workspace === 'tts' ? '独立文本试听 · TTS' : `${selected ? `当前资产 ${selected.asset_id}` : '未选择资产'} · ${current.label}`}
        </Typography.Text>
        <DesktopServiceStatus />
      </Layout.Footer>

      <AppTour open={tourOpen} onClose={closeTour} />

      {/* Phase 4: 键盘快捷键速查模态框 */}
      <KeyboardShortcutsModal
        open={isShortcutsHelpOpen}
        onClose={() => setIsShortcutsHelpOpen(false)}
      />
    </Layout>
    </PipelineContext.Provider>
    </SequenceEditorProvider>
    </SkillWorkspaceProvider>
  );
}
