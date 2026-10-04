import { Button, Card, Layout, Menu, Typography } from 'antd';
import { ArrowLeftOutlined } from '@ant-design/icons';
import type { UseQueryResult } from '@tanstack/react-query';
import type { Asset, AuditLog, WorkflowRun } from '../types';
import type { RouteState } from '../navigation';
import { SETTINGS_CATEGORIES } from '../navigation';
import { PreparationPanel } from '../components/PreparationPanel';
import AuditLogViewer from '../components/AuditLogViewer';
import { ConnectionStatusBar } from '../components/ConnectionStatusBar';

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
}

/**
 * 设置工作区：处理预设 / 服务商接入 / 运行准备 / 日志与诊断
 */
export function SettingsWorkspace({ route, navigate, asset, onStarted, audit, connection, events }: SettingsWorkspaceProps) {
  const { settingsCategory = 'profiles' } = route;

  return (
    <div className="page-body settings-workspace">
      <div className="page-header">
        <Typography.Title level={4} style={{ margin: 0 }}>
          设置 <Typography.Text type="secondary" style={{ fontSize: 14, fontWeight: 400 }}>Settings</Typography.Text>
        </Typography.Title>
      </div>

      <Layout hasSider style={{ background: 'transparent', minHeight: 500 }}>
        <Layout.Sider 
          theme="light" 
          width={220} 
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
              label: cat.label,
              title: cat.description
            }))}
            style={{ background: 'transparent', border: 'none' }}
          />
        </Layout.Sider>

        <Layout.Content style={{ paddingLeft: 24, minHeight: 500 }}>
          {settingsCategory === 'profiles' && (
            <Card title="处理预设" bordered={false}>
              <Typography.Paragraph type="secondary">
                处理预设包含模型配置、取样参数和服务商绑定。保存后的版本不可覆盖，新版本保留历史。
              </Typography.Paragraph>
              <Typography.Paragraph type="secondary">
                此处应显示 PreparationPanel 的预设管理部分（单独拆分后的组件）。
              </Typography.Paragraph>
            </Card>
          )}

          {settingsCategory === 'providers' && (
            <Card title="服务商接入" bordered={false}>
              <Typography.Paragraph type="secondary">
                配置画面识别与中文解说服务：协议、地址、凭据、模型发现与连接测试。
              </Typography.Paragraph>
              <Typography.Paragraph type="secondary">
                此处应显示 ProviderEditor 组件。配置仍从属于预设草稿，保存时生成完整预设的新版本。
              </Typography.Paragraph>
            </Card>
          )}

          {settingsCategory === 'preparation' && (
            <>
              {asset ? (
                <PreparationPanel 
                  key={'preparation-' + asset.asset_id} 
                  assetId={asset.asset_id} 
                  onStarted={onStarted} 
                />
              ) : (
                <Card title="运行准备" bordered={false}>
                  <Typography.Paragraph type="secondary">
                    运行准备绑定具体资产，请先在左侧项目面板选择资产。
                  </Typography.Paragraph>
                  <Button 
                    type="primary"
                    onClick={() => navigate({ workspace: 'resources' })}
                  >
                    前往资源管理
                  </Button>
                </Card>
              )}
            </>
          )}

          {settingsCategory === 'logs' && (
            <Card 
              title="日志与诊断" 
              bordered={false}
              extra={
                <ConnectionStatusBar
                  status={connection}
                  retryCount={events.retryCount}
                  maxRetries={events.maxRetries}
                  retryExhausted={events.retryExhausted}
                  nextRetryLabel={events.nextRetryLabel}
                  onReconnect={events.reconnect}
                  onDisconnect={events.disconnect}
                />
              }
            >
              <AuditLogViewer />
            </Card>
          )}
        </Layout.Content>
      </Layout>
    </div>
  );
}
