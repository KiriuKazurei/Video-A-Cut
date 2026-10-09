import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Badge, Button, ConfigProvider, Drawer, Grid, Layout, Menu, Select, Space, Tooltip, Typography, theme } from 'antd';
import {
  ExportOutlined, FileSearchOutlined, FileTextOutlined, FolderOpenOutlined, MenuFoldOutlined, MenuUnfoldOutlined, 
  PlaySquareOutlined, QuestionCircleOutlined, ReloadOutlined, SettingOutlined, ThunderboltOutlined, VideoCameraOutlined, SoundOutlined
} from '@ant-design/icons';
import { api } from './api';
import type { Asset, CreateTask, GovernancePatch, WorkflowRun } from './types';
import { useEvents } from './useEvents';
import { refreshSnapshot } from './queryRefresh';
import { ConnectionStatusBar } from './components/ConnectionStatusBar';
import { MediaBin } from './components/MediaBin';
import { Inspector } from './components/Inspector';
import { WorkspacePage } from './components/WorkspacePage';
import type { RunSelection } from './components/WorkflowPanel';
import { WORKSPACES, useWorkspaceRoute, type WorkspaceId } from './navigation';
import { siderTheme } from './theme';
import { useResizable } from './hooks/useResizable';
import { useKeyboardShortcuts } from './hooks/useKeyboardShortcuts';
import { PipelineStepper } from './components/PipelineStepper';
import { KeyboardShortcutsModal } from './components/KeyboardShortcutsModal';
import { ResourcesWorkspace } from './workspaces/ResourcesWorkspace';
import { EditingWorkspace } from './workspaces/EditingWorkspace';
import { ExportWorkspace } from './workspaces/ExportWorkspace';
import { SettingsWorkspace } from './workspaces/SettingsWorkspace';
import { TtsWorkspace } from './workspaces/TtsWorkspace';

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
  const queryClient = useQueryClient();
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  
  // Phase 3: 可拖拽侧栏宽度 (200-420px / 220-450px)，双击还原默认并持久化
  const {
    size: leftSiderWidth,
    isDragging: isLeftDragging,
    startDrag: startLeftDrag,
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

  // 顶部页签的液态玻璃滑块：量出选中项（或选中的「…」）相对页签组的 left/width，写成 CSS 变量；
  // 滑块本身是 .app-nav::before（见 styles.css），无需额外 DOM；窗口/字体/折叠变化时自动重新量。
  const navWrapRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const wrap = navWrapRef.current;
    const nav = wrap?.querySelector<HTMLElement>('.app-nav');
    if (!wrap || !nav) return;
    let last = '';
    let timer: number | undefined;
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
        // 首次定位不做滑动动画，下一帧再开启
        requestAnimationFrame(() => requestAnimationFrame(() => nav.setAttribute('data-thumb-anim', '1')));
      } else {
        nav.setAttribute('data-thumb-moving', '1');
        window.clearTimeout(timer);
        timer = window.setTimeout(() => nav.removeAttribute('data-thumb-moving'), 280);
      }
    };
    measure();
    const resize = new ResizeObserver(measure);
    resize.observe(nav);
    const mutate = new MutationObserver(measure);
    mutate.observe(nav, { subtree: true, attributes: true, attributeFilter: ['class', 'style'], childList: true });
    void document.fonts?.ready.then(measure);
    return () => { resize.disconnect(); mutate.disconnect(); window.clearTimeout(timer); };
  }, []);

  // Phase 4: 全局键盘快捷键与帮助面板
  const [isShortcutsHelpOpen, setIsShortcutsHelpOpen] = useState(false);
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
  const audit = useQuery({ queryKey: ['audit', 'recent', 100], queryFn: () => api.listAudit(100) });
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
  
  // 准备页启动流程后：选中新流程并切到编辑/审查
  const onPreparedStarted = (run: WorkflowRun) => {
    setRunState({ assetId, runId: run.run_id, offset: 0 });
    navigate({ workspace: 'editing', editingSection: 'review' });
  };
  
  const connectionText = connection === 'connected' ? '事件流已连接'
    : connection === 'connecting' ? '正在连接事件流'
    : connection === 'disconnected' ? '事件流已断开 · 显示最近快照'
    : connection === 'error' ? '事件流连接失败 · 显示最近快照'
    : `事件流重连中（${events.retryCount}/${events.maxRetries}）· 显示最近快照`;

  // 响应式策略：lg 及以下使用抽屉，xl 及以上使用侧栏
  const useSidebar = screens.xl;
  const independentWorkspace = workspace === 'settings' || workspace === 'tts';
  const showLeftSider = useSidebar && !independentWorkspace;
  const showRightSider = useSidebar && !independentWorkspace;

  // 侧栏内容
  const leftSiderContent = (
    <ConfigProvider theme={siderTheme}>
      <MediaBin assets={assets} selectedId={selected?.asset_id} onSelect={setSelectedId} />
    </ConfigProvider>
  );
  
  const rightSiderContent = (
    <ConfigProvider theme={siderTheme}>
      <Inspector asset={selected} patch={patch} onNavigate={(ws) => navigate({ workspace: ws as WorkspaceId })} />
    </ConfigProvider>
  );

  return (
    <Layout className="app-shell">
      <a className="skip-link" href="#main">跳至主要内容</a>
      
      <Layout.Header className="app-header">
        <div className="app-brand">
          <VideoCameraOutlined style={{ fontSize: 20, color: token.colorPrimary }} />
          <div className="app-brand-text">
            <Typography.Text strong>Video Auto Cut</Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>剪辑预处理工作区</Typography.Text>
          </div>
        </div>
        
        {/* 工作区导航：资源、编辑、导出、TTS + 右侧设置 */}
        <div className="app-nav-wrap" ref={navWrapRef}>
          <Menu 
            className="app-nav glass-nav" 
            mode="horizontal" 
            aria-label="工作区" 
            overflowedIndicatorPopupClassName="app-nav-popup"
            selectedKeys={[workspace]}
            onClick={({ key }) => navigate({ workspace: key as WorkspaceId })}
            items={WORKSPACES.filter(w => w.id !== 'settings').map((item) => ({
              key: item.id, 
              icon: WORKSPACE_ICONS[item.id], 
              title: item.description,
              label: <span data-workspace={item.id} aria-current={workspace === item.id ? 'page' : undefined}>{item.label}</span>
            }))} 
          />
        </div>

        {/* Phase 3: 全流程阶段指示器（仅在宽屏 ≥1200px 时在页头居中展示，防止 960-1024px 挤压导航） */}
        {screens.xl && workspace !== 'tts' && (
          <PipelineStepper 
            route={route} 
            navigate={navigate} 
            asset={selected} 
          />
        )}
        
        <div className="app-header-right">
          {/* Phase 3: 全局快捷操作栏（>=960px 显示下拉，>=1280px 显示额外直达按钮） */}
          {screens.md && workspace !== 'tts' && (
            <div className="app-quick-actions">
              {/* 资产只有一个选择入口：侧栏可见时用项目面板，侧栏不可见（窄屏/设置页）时才在页头提供下拉。 */}
              {!showLeftSider && <Select
                size="small"
                placeholder="选择资产"
                value={assetId || undefined}
                onChange={(val) => setSelectedId(val)}
                options={assets.data?.map((a) => ({
                  value: a.asset_id,
                  label: `${a.asset_id} (${a.input_kind === 'raw_recording' ? '录像' : '素材'})`
                }))}
                style={{ width: screens.sm ? 120 : 90 }}
                aria-label="快速切换资产"
                popupMatchSelectWidth={false}
              />}

              {screens.xl && (
                <>
                  <Tooltip title={assetId ? `为资产 ${assetId} 打开运行准备` : '打开运行准备'}>
                    <Button
                      type="text"
                      size="small"
                      icon={<ThunderboltOutlined />}
                      onClick={() => navigate({ workspace: 'settings', settingsCategory: 'preparation' })}
                    >
                      运行准备
                    </Button>
                  </Tooltip>

                  <Tooltip title="查看审计与诊断日志">
                    <Button
                      type="text"
                      size="small"
                      icon={<FileSearchOutlined />}
                      onClick={() => navigate({ workspace: 'settings', settingsCategory: 'logs' })}
                    >
                      查看日志
                    </Button>
                  </Tooltip>
                </>
              )}
            </div>
          )}

          {!useSidebar && !independentWorkspace && (
            <>
              <Button 
                size="small"
                icon={<MenuUnfoldOutlined />} 
                onClick={() => setLeftDrawerOpen(true)}
              >
                项目
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
          
          <Badge 
            className="app-connection" 
            role="status" 
            aria-live="polite"
            status={connection === 'connected' ? 'success' : connection === 'connecting' || connection === 'reconnecting' ? 'processing' : 'error'}
            text={connectionText} 
          />
          
          <Button 
            size="small"
            icon={<ReloadOutlined />} 
            onClick={() => void refreshSnapshot(queryClient, watchedTaskId)}
          >
            刷新
          </Button>
          
          <Tooltip title="键盘快捷键速查 (?)">
            <Button
              size="small"
              icon={<QuestionCircleOutlined />}
              onClick={() => setIsShortcutsHelpOpen(true)}
              aria-label="键盘快捷键速查"
            />
          </Tooltip>

          <Button
            size="small"
            type={workspace === 'settings' ? 'primary' : 'default'}
            icon={<SettingOutlined />}
            onClick={() => navigate({ workspace: 'settings' })}
          >
            设置
          </Button>
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
          {/* 折叠按钮：仅在 xl 及以上且非设置页显示 */}
          {useSidebar && !independentWorkspace && (
            <div className="sider-toggles">
              <Button 
                type="text" 
                size="small"
                icon={leftCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                onClick={() => setLeftCollapsed(!leftCollapsed)}
                title={leftCollapsed ? '展开项目面板' : '收起项目面板'}
              />
              <Button 
                type="text" 
                size="small"
                icon={rightCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
                onClick={() => setRightCollapsed(!rightCollapsed)}
                title={rightCollapsed ? '展开属性面板' : '收起属性面板'}
              />
            </div>
          )}
          
          <WorkspacePage pageId="tts" active={workspace === 'tts'}>
            <TtsWorkspace active={workspace === 'tts'} />
          </WorkspacePage>
          <WorkspacePage pageId="resources" active={workspace === 'resources'}>
            <ResourcesWorkspace asset={selected} onSelect={setSelectedId} />
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
            />
          </WorkspacePage>
        </Layout.Content>
        
        {/* 右侧栏：xl 及以上显示，lg 及以下折叠，Phase 3 支持宽度拖拽 */}
        {showRightSider && (
          <Layout.Sider 
            theme="light" 
            width={rightSiderWidth}
            collapsedWidth={0}
            collapsed={rightCollapsed}
            collapsible
            trigger={null}
            className={`dock dock-right dock-resizable${isRightDragging ? " is-dragging-resize" : ""}`} 
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
        title="属性面板"
        placement="right"
        open={rightDrawerOpen && !independentWorkspace}
        onClose={() => setRightDrawerOpen(false)}
        width={320}
      >
        {rightSiderContent}
      </Drawer>

      <Layout.Footer className="app-footer">
        <ConnectionStatusBar
          status={connection}
          retryCount={events.retryCount}
          maxRetries={events.maxRetries}
          retryExhausted={events.retryExhausted}
          nextRetryLabel={events.nextRetryLabel}
          onReconnect={events.reconnect}
          onDisconnect={events.disconnect}
        />
        <Typography.Text type="secondary" className="app-footer-meta">
          {workspace === 'tts' ? '独立文本试听 · TTS' : `${selected ? `当前资产 ${selected.asset_id}` : '未选择资产'} · ${current.label}`}
        </Typography.Text>
      </Layout.Footer>

      {/* Phase 4: 键盘快捷键速查模态框 */}
      <KeyboardShortcutsModal
        open={isShortcutsHelpOpen}
        onClose={() => setIsShortcutsHelpOpen(false)}
      />
    </Layout>
  );
}
