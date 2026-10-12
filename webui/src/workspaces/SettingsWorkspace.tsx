import { Button, Card, Grid, Layout, Menu, Segmented, Typography } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import type { UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, WorkflowRun } from '../types';
import type { RouteState } from '../navigation';
import { SETTINGS_CATEGORIES } from '../navigation';
import { SECTION_STAGE } from '../pipeline';
import { StageDot } from '../components/PipelineContext';
import { PreparationPanel } from '../components/PreparationPanel';
import AuditLogViewer from '../components/AuditLogViewer';
import { StatusMonitor } from '../components/StatusMonitor';

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

/**
 * 设置工作区：处理预设 / 服务商接入 / 运行准备 / 日志与诊断
 * P2-01 修复：提供真实处理预设与服务商接入功能，移除死占位符
 * P2-02 修复：单实例常驻保活，分类切换草稿不丢失
 * P1-04 修复：响应式自适应，小屏下转为顶部分类切换，消除宽度坍缩
 */
export function SettingsWorkspace({ route, navigate, asset, onStarted, audit, connection, events, controlPlane }: SettingsWorkspaceProps) {
  const { settingsCategory = 'profiles' } = route;
  const screens = Grid.useBreakpoint();
  const isMobile = !screens.md; // < 768px

  const isProfileCategory = settingsCategory === 'profiles' || settingsCategory === 'providers' || settingsCategory === 'preparation';

  return (
    <div className="page-body settings-workspace">
      {/* 移动端/窄屏下的顶部分类切换 */}
      {isMobile && (
        <Segmented
          block
          value={settingsCategory}
          onChange={(key) => navigate({ settingsCategory: key as typeof settingsCategory })}
          options={SETTINGS_CATEGORIES.map((cat) => ({
            value: cat.id,
            label: <span className="section-tab-label">{cat.label}<StageDot stage={cat.id === 'preparation' ? SECTION_STAGE.settings.preparation : undefined} /></span>
          }))}
          style={{ marginBottom: 16 }}
        />
      )}

      <Layout hasSider={!isMobile} style={{ background: 'transparent', minHeight: 500, width: '100%' }}>
        {!isMobile && (
          <Layout.Sider 
            theme="light" 
            width={200} 
            style={{ 
              background: 'transparent',
              borderInlineEnd: '1px solid var(--vac-hairline)'
            }}
          >
            <Menu
              mode="inline"
              selectedKeys={[settingsCategory]}
              onClick={({ key }) => navigate({ settingsCategory: key as typeof settingsCategory })}
              items={SETTINGS_CATEGORIES.map((cat) => ({
                key: cat.id,
                label: <span className="section-tab-label">{cat.label}<StageDot stage={cat.id === 'preparation' ? SECTION_STAGE.settings.preparation : undefined} /></span>,
                title: cat.description
              }))}
              style={{ background: 'transparent', border: 'none' }}
            />
          </Layout.Sider>
        )}

        <Layout.Content style={{ paddingLeft: isMobile ? 0 : 20, minHeight: 500, width: '100%', minWidth: 0 }}>
          {/* P2-02: 保持 PreparationPanel 实例常驻挂载，切换分类不卸载草稿 */}
          <div style={{ display: isProfileCategory ? 'block' : 'none', width: '100%' }}>
            <PreparationPanel 
              assetId={asset?.asset_id ?? ''} 
              onStarted={onStarted} 
              activeSection={isProfileCategory ? settingsCategory : 'profiles'}
              onNavigate={navigate}
            />
          </div>

          {settingsCategory === 'logs' && (
            <Card 
              title="日志与诊断" 
              bordered={false}
            >
              <AuditLogViewer />
            </Card>
          )}

          {settingsCategory === 'status' && (
            <StatusMonitor connection={connection} events={events} controlPlane={controlPlane} audit={audit} />
          )}
        </Layout.Content>
      </Layout>
    </div>
  );
}
