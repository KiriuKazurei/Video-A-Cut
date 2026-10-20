import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { Alert, App, Button, Card, Space, Spin, Typography } from 'antd';
import { useQueryClient } from '@tanstack/react-query';
import { desktopCommand, isDesktop, runtimeListener, type RuntimeStatus } from '../desktop';
import { setApiBase } from '../runtimeUrl.ts';
import { DesktopUpdates } from './DesktopUpdates';

const RuntimeContext = createContext<RuntimeStatus | undefined>(undefined);
export const useDesktopRuntimeStatus = () => useContext(RuntimeContext);

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
  return <RuntimeContext.Provider value={runtime}><div className="desktop-runtime-shell">
    <div className="desktop-runtime-bar" role="status" style={{ padding: '6px 16px', display: 'flex', justifyContent: 'space-between', gap: 12 }}>
      <Typography.Text>{runtime?.status === 'ready' ? '后台服务已就绪' : runtime?.message ?? '正在连接桌面服务…'}</Typography.Text>
      <DesktopUpdates onMaintenance={setMaintenance} />
    </div>
    {error && <Alert role="alert" type="error" showIcon message={error} />}
    {runtime?.status === 'ready' && runtime.workers.some(worker => worker.status === 'blocked') &&
      <Alert role="alert" type="warning" showIcon message={`后台 Worker 已停止：${runtime.workers.filter(worker => worker.status === 'blocked').map(worker => worker.role).join('、')}`}
        description="请查看数据目录中本次会话的日志，修正配置后重新启动后台。"
        action={<Button onClick={() => command('stop_runtime')}>停止后台</Button>} />}
    {runtime?.status === 'ready' ? <div className="desktop-runtime-content" key={runtime.session_id}>{children}</div> :
      <Card style={{ maxWidth: 640, margin: '64px auto' }} title="Video Auto Cut">
        <Space direction="vertical" size="large">
          {(!runtime || runtime.status === 'starting' || runtime.status === 'stopping') && <Spin />}
          <Typography.Paragraph role={runtime?.status === 'fault' ? 'alert' : 'status'}>{runtime?.message ?? '正在启动本地服务…'}</Typography.Paragraph>
          {(runtime?.status === 'fault' || runtime?.status === 'stopped') && <Button type="primary" disabled={maintenance} onClick={() => command('restart_runtime')}>重新启动</Button>}
        </Space>
      </Card>}
  </div></RuntimeContext.Provider>;
}
