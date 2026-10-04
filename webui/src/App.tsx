import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Badge, Button, ConfigProvider, Drawer, Grid, Layout, Menu, Typography, theme } from 'antd';
import {
  ExportOutlined, FileTextOutlined, FolderOpenOutlined, MenuFoldOutlined, MenuUnfoldOutlined, 
  PlaySquareOutlined, ReloadOutlined, SettingOutlined, VideoCameraOutlined
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
import { ResourcesWorkspace } from './workspaces/ResourcesWorkspace';
import { EditingWorkspace } from './workspaces/EditingWorkspace';
import { ExportWorkspace } from './workspaces/ExportWorkspace';
import { SettingsWorkspace } from './workspaces/SettingsWorkspace';

const WORKSPACE_ICONS: Record<WorkspaceId, React.ReactNode> = {
  resources: <FolderOpenOutlined />,
  editing: <PlaySquareOutlined />,
  export: <ExportOutlined />,
  settings: <SettingOutlined />
};

/**
 * 四工作区外壳：顶部导航切换主工作区，左右侧栏响应式折叠。
 * 资源管理、编辑、导出为主流程；设置页独立，从右上角齿轮进入。
 */
export default function App() {
  const queryClient = useQueryClient();
  const { token } = theme.useToken();
  const screens = Grid.useBreakpoint();
  
  // 响应式侧栏宽度：xxl 280/320，xl 240/280，lg 及以下折叠
  const siderWidths = screens.xxl ? [280, 320] : [240, 280];
  const [leftCollapsed, setLeftCollapsed] = useState(false);
  const [rightCollapsed, setRightCollapsed] = useState(false);
  const [leftDrawerOpen, setLeftDrawerOpen] = useState(false);
  const [rightDrawerOpen, setRightDrawerOpen] = useState(false);
  
  const siderBorder = `1px solid ${token.colorBorderSecondary}`;
  const { route, navigate } = useWorkspaceRoute();
  const { workspace } = route;
  
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
  const showLeftSider = useSidebar && workspace !== 'settings';
  const showRightSider = useSidebar && workspace !== 'settings';

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
        
        {/* 工作区导航：三个主工作区 + 右侧设置 */}
        <div className="app-nav-wrap">
          <Menu 
            className="app-nav glass-nav" 
            mode="horizontal" 
            aria-label="工作区" 
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
        
        <div className="app-header-right">
          {!useSidebar && workspace !== 'settings' && (
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
            icon={<ReloadOutlined />} 
            onClick={() => void refreshSnapshot(queryClient, watchedTaskId)}
          >
            刷新快照
          </Button>
          
          <Button
            type={workspace === 'settings' ? 'primary' : 'default'}
            icon={<SettingOutlined />}
            onClick={() => navigate({ workspace: 'settings' })}
          >
            设置
          </Button>
        </div>
      </Layout.Header>

      <Layout hasSider className={`workspace workspace-${workspace}`} style={{ '--vac-sider-border': siderBorder } as React.CSSProperties}>
        {/* 左侧栏：xl 及以上显示，lg 及以下折叠 */}
        {showLeftSider && (
          <Layout.Sider 
            theme="light" 
            width={siderWidths[0]}
            collapsedWidth={0}
            collapsed={leftCollapsed}
            collapsible
            trigger={null}
            className="dock dock-left" 
            aria-label="项目面板" 
            style={{ borderInlineEnd: siderBorder }}
          >
            {leftSiderContent}
          </Layout.Sider>
        )}
        
        <Layout.Content id="main" className="stage-main" aria-label={`${current.label}工作区`}>
          {/* 折叠按钮：仅在 xl 及以上且非设置页显示 */}
          {useSidebar && workspace !== 'settings' && (
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
        
        {/* 右侧栏：xl 及以上显示，lg 及以下折叠 */}
        {showRightSider && (
          <Layout.Sider 
            theme="light" 
            width={siderWidths[1]}
            collapsedWidth={0}
            collapsed={rightCollapsed}
            collapsible
            trigger={null}
            className="dock dock-right" 
            aria-label="属性面板" 
            style={{ borderInlineStart: siderBorder }}
          >
            {rightSiderContent}
          </Layout.Sider>
        )}
      </Layout>

      {/* 移动端抽屉 */}
      <Drawer
        title="项目面板"
        placement="left"
        open={leftDrawerOpen}
        onClose={() => setLeftDrawerOpen(false)}
        width={280}
      >
        {leftSiderContent}
      </Drawer>
      
      <Drawer
        title="属性面板"
        placement="right"
        open={rightDrawerOpen}
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
          {selected ? `当前资产 ${selected.asset_id}` : '未选择资产'} · {current.label}
        </Typography.Text>
      </Layout.Footer>
    </Layout>
  );
}
