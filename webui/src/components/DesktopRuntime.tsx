import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { App, Badge, Button, Card, Space, Spin, Typography } from 'antd';
import { useQueryClient } from '@tanstack/react-query';
import { desktopCommand, isDesktop, runtimeListener, type RuntimeStatus } from '../desktop';
import { setApiBase } from '../runtimeUrl.ts';
import { DesktopUpdates } from './DesktopUpdates';

const RuntimeContext = createContext<{
  runtime?: RuntimeStatus;
  error: string;
  command: (name: string) => void;
} | undefined>(undefined);
export const useDesktopRuntimeStatus = () => useContext(RuntimeContext)?.runtime;

export function DesktopServiceStatus() {
  const context = useContext(RuntimeContext);
  if (!context) return null;
  const { runtime, error, command } = context;
  const blocked = runtime?.status === 'ready' ? runtime.workers.filter(worker => worker.status === 'blocked') : [];
  const problem = Boolean(error) || runtime?.status === 'fault' || blocked.length > 0;
  const text = error || (blocked.length
    ? `后台 Worker 已停止：${blocked.map(worker => worker.role).join('、')}`
    : runtime?.status === 'ready' ? '后台服务已就绪' : runtime?.message || '正在连接桌面服务…');
  return <div className="desktop-service-status" role={problem ? 'alert' : 'status'} aria-label="后台服务状态">
    <Badge status={problem ? 'error' : runtime?.status === 'ready' ? 'success' : runtime?.status === 'stopped' ? 'default' : 'processing'} />
    <Typography.Text className="desktop-service-message" type={problem ? 'danger' : 'secondary'} ellipsis title={text}>{text}</Typography.Text>
    {blocked.length > 0 && <Button size="small" type="link" danger title="请查看本次会话日志，修正配置后重新启动后台" onClick={() => command('stop_runtime')}>停止后台</Button>}
  </div>;
}

export function DesktopRuntime({ children }: { children: ReactNode }) {
  const [runtime, setRuntime] = useState<RuntimeStatus>();
  const [error, setError] = useState('');
  const [maintenance, setMaintenance] = useState(false);
  const client = useQueryClient();
  const { message } = App.useApp();
  useEffect(() => {
    if (!isDesktop()) return;
    let active = true;
    let unlisten: (() => void) | undefined;
    let session = '';
    let revision = -1;
    const receive = (value: RuntimeStatus) => {
      if (!active || value.revision < revision) return;
      revision = value.revision;
      try {
        if (value.status === 'ready' && value.api_base_url) {
          setApiBase(value.api_base_url);
          if (session !== value.session_id) { client.clear(); session = value.session_id ?? ''; }
        }
        setRuntime(value); setError('');
      } catch (e) { setError(String(e)); }
    };
    void (async () => {
      const dispose = await runtimeListener(receive);
      if (!active) { dispose(); return; }
      unlisten = dispose;
      receive(await desktopCommand<RuntimeStatus>('runtime_status'));
    })().catch(e => { if (active) setError(String(e)); });
    return () => { active = false; unlisten?.(); };
  }, [client]);

  useEffect(() => {
    if (!isDesktop() || !runtime?.api_base_url) return;
    const download = (event: MouseEvent) => {
      const anchor = event.target instanceof Element ? event.target.closest('a[href]') as HTMLAnchorElement | null : null;
      if (!anchor || !anchor.href.startsWith(runtime.api_base_url + '/api/')) return;
      // Links are downloads; players continue requesting media directly over HTTP.
      event.preventDefault();
      void desktopCommand('download_file', { url: anchor.href }).catch(e => message.error(String(e)));
    };
    document.addEventListener('click', download);
    return () => document.removeEventListener('click', download);
  }, [runtime?.api_base_url, message]);

  if (!isDesktop()) return children;
  const command = (name: string) => { setError(''); void desktopCommand(name).catch(e => setError(String(e))); };
  return <RuntimeContext.Provider value={{ runtime, error, command }}><div className="desktop-runtime-shell">
    <DesktopUpdates onMaintenance={setMaintenance} />
    {runtime?.status === 'ready' ? <div className="desktop-runtime-content" key={runtime.session_id}>{children}</div> :
      <><div className="desktop-runtime-content"><Card style={{ maxWidth: 640, margin: '64px auto' }} title="Video Auto Cut">
        <Space direction="vertical" size="large">
          {(!runtime || runtime.status === 'starting' || runtime.status === 'stopping') && <Spin />}
          <Typography.Paragraph role={runtime?.status === 'fault' ? 'alert' : 'status'}>{runtime?.message ?? '正在启动本地服务…'}</Typography.Paragraph>
          {(runtime?.status === 'fault' || runtime?.status === 'stopped') && <Button type="primary" disabled={maintenance} onClick={() => command('restart_runtime')}>重新启动</Button>}
        </Space>
      </Card></div><footer className="app-footer desktop-runtime-footer"><DesktopServiceStatus /></footer></>}
  </div></RuntimeContext.Provider>;
}
