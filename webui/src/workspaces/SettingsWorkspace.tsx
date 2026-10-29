import { useRef } from 'react';
import { Button, Card, Grid, Menu, Tabs, Tag, Typography } from 'antd';
import { ArrowLeftOutlined, ApiOutlined, AppstoreOutlined, CheckCircleOutlined, FileSearchOutlined, HeartOutlined, RobotOutlined } from '@ant-design/icons';
import type { UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, WorkflowRun } from '../types';
import type { RouteState, SettingsCategory } from '../navigation';
import { SETTINGS_CATEGORIES, WORKSPACES } from '../navigation';
import { PreparationPanel } from '../components/PreparationPanel';
import AuditLogViewer from '../components/AuditLogViewer';
import { StatusMonitor } from '../components/StatusMonitor';
import { SkillManagerModule } from '../components/SkillManagerModule';
import { AgentOperationPanel } from '../components/AgentOperationPanel';
import { ProjectProviderPolicy } from '../components/ProjectProviderPolicy';
import { McpConnectionPanel } from '../components/McpConnectionPanel';
import { ValueSelect } from '../components/ui';
import { DiagnosticsPanel } from '../components/DiagnosticsPanel';

interface SettingsWorkspaceProps {
  route: RouteState;
  navigate: (state: Partial<RouteState>) => void;
  asset?: Asset;
  onStarted: (run: WorkflowRun) => void;
  audit: UseQueryResult<AuditLog[], Error>;
  connection: 'connected' | 'connecting' | 'disconnected' | 'reconnecting' | 'error';
  events: {
    retryCount: number;
    maxRetries: number;
    retryExhausted: boolean;
    nextRetryLabel: string | null;
    reconnect: () => void;
    disconnect: () => void;
  };
  controlPlane: UseQueryResult<Asset[], Error>;
}

const groups = [
  {label:'工作流配置',ids:['profiles','providers','preparation']},
  {label:'Agent 扩展',ids:['skills']},
  {label:'诊断与维护',ids:['logs','status']}
] as const;
const icons = { profiles:<AppstoreOutlined aria-hidden />, providers:<ApiOutlined aria-hidden />, preparation:<CheckCircleOutlined aria-hidden />, skills:<RobotOutlined aria-hidden />, logs:<FileSearchOutlined aria-hidden />, status:<HeartOutlined aria-hidden /> };

/** Global settings keep one preparation draft mounted across category changes. */
export function SettingsWorkspace({ route, navigate, asset, onStarted, audit, connection, events, controlPlane }: SettingsWorkspaceProps) {
  const {settingsCategory = 'profiles'} = route;
  const screens = Grid.useBreakpoint();
  const compact = !screens.lg;
  const returnRoute = useRef<Partial<RouteState>>({workspace:'resources'});
  if(route.workspace !== 'settings') returnRoute.current = {...route};
  const category = SETTINGS_CATEGORIES.find(item=>item.id===settingsCategory)!;
  const isProfileCategory = ['profiles','providers','preparation'].includes(settingsCategory);
  const changeCategory = (key:SettingsCategory) => navigate({settingsCategory:key});
  return <div className="page-body settings-workspace" data-testid="settings-workspace">
    <header className="settings-page-heading">
      <div><Typography.Title level={1}>设置</Typography.Title><Typography.Text type="secondary">管理处理流程、Agent 接入与运行环境。</Typography.Text></div>
      <Button icon={<ArrowLeftOutlined />} title={`返回${WORKSPACES.find(item=>item.id===returnRoute.current.workspace)?.label ?? '工作区'}`} onClick={()=>navigate(returnRoute.current)}>返回工作区</Button>
    </header>
    <div className="settings-grid">
      <aside className="settings-sidebar" aria-label="设置导航">
        {compact ? <div className="settings-category-picker"><Typography.Text type="secondary">设置分类</Typography.Text>
          <ValueSelect<SettingsCategory> transitionName="" getPopupContainer={trigger => trigger.parentElement ?? document.body} aria-label="设置分类" value={settingsCategory} onChange={changeCategory} options={SETTINGS_CATEGORIES.map(item=>({value:item.id,label:item.label}))}/>
        </div> : <Menu mode="inline" aria-label="设置分类" selectedKeys={[settingsCategory]} onClick={({key})=>changeCategory(key as SettingsCategory)} items={groups.map(group=>({type:'group' as const,label:group.label,children:group.ids.map(id=>({key:id,icon:icons[id],label:SETTINGS_CATEGORIES.find(item=>item.id===id)!.label}))}))}/>}
      </aside>
      <section className="settings-content" aria-labelledby="settings-category-title">
        <header className="settings-section-heading"><div><Typography.Title level={2} id="settings-category-title">{category.label}</Typography.Title><Typography.Paragraph type="secondary">{category.description}</Typography.Paragraph></div><Tag>全局设置</Tag></header>
        {settingsCategory==='providers' && <ProjectProviderPolicy />}
        <div hidden={!isProfileCategory}>
          <PreparationPanel assetId={asset?.asset_id ?? ''} onStarted={onStarted} activeSection={isProfileCategory ? settingsCategory as 'profiles'|'providers'|'preparation' : 'profiles'} onNavigate={navigate}/>
        </div>
        {settingsCategory==='skills' && <div className="settings-stack"><McpConnectionPanel /><SkillManagerModule /></div>}
        {settingsCategory==='logs' && <Tabs className="settings-log-tabs" items={[
          {key:'audit',label:'审计日志',children:<AuditLogViewer />},
          {key:'operations',label:'Agent 操作记录',children:<Card size="small" title="Agent 操作记录"><AgentOperationPanel /></Card>}
        ]}/>}
        {settingsCategory==='status' && <div className="settings-stack"><DiagnosticsPanel /><StatusMonitor connection={connection} events={events} controlPlane={controlPlane} audit={audit}/></div>}
      </section>
    </div>
  </div>;
}
