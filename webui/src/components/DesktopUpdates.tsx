import { useEffect, useState } from 'react';
import { Alert, Button, Modal, Progress, Space, Typography } from 'antd';
import { desktopCommand, updateListener, type UpdateStatus } from '../desktop';

export function DesktopUpdates({ onMaintenance }: { onMaintenance: (busy: boolean) => void }) {
  const [status, setStatus] = useState<UpdateStatus>();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  const maintenance = ['awaiting_confirmation', 'stopping', 'installing'].includes(status?.phase ?? '');
  const busy = maintenance || status?.phase === 'checking' || status?.phase === 'downloading';
  useEffect(() => { onMaintenance(maintenance); }, [maintenance, onMaintenance]);
  useEffect(() => {
    let active = true;
    let dispose: (() => void) | undefined;
    let revision = -1;
    const receive = (value: UpdateStatus) => {
      if (!active || value.revision < revision) return;
      revision = value.revision; setStatus(value); setError('');
    };
    void (async () => {
      const unlisten = await updateListener(receive);
      if (!active) { unlisten(); return; }
      dispose = unlisten;
      receive(await desktopCommand<UpdateStatus>('update_status'));
    })().catch(e => { if (active) setError(String(e)); });
    return () => { active = false; dispose?.(); };
  }, []);
  const command = (name: string) => {
    setError('');
    void desktopCommand(name).catch(e => setError(String(e)));
  };
  const show = () => {
    setOpen(true);
    if (status && !status.available_version && !busy) command('check_update');
  };
  const percent = status?.ready_to_install ? 100 : status?.total_bytes
    ? Math.min(99, Math.floor(status.downloaded_bytes / status.total_bytes * 100)) : 0;
  return <>
    <Button size="small" onClick={show} loading={status?.phase === 'checking'}>检查更新</Button>
    <Modal title="软件更新" open={open} onCancel={() => { if (!maintenance) setOpen(false); }}
      closable={!maintenance} maskClosable={!maintenance}
      footer={<Space wrap>
        <Button onClick={() => setOpen(false)} disabled={maintenance}>关闭</Button>
        <Button onClick={() => command('check_update')} disabled={busy} loading={status?.phase === 'checking'}>重新检查</Button>
        {status?.available_version && !status.ready_to_install &&
          <Button type="primary" onClick={() => command('download_update')} disabled={busy} loading={status.phase === 'downloading'}>下载更新</Button>}
        {status?.ready_to_install &&
          <Button type="primary" onClick={() => command('install_update')} disabled={busy} loading={maintenance}>安装并重启</Button>}
      </Space>}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Typography.Text>当前版本：{status?.current_version ?? '正在读取…'}</Typography.Text>
        {status?.available_version && <Typography.Text>可用版本：{status.available_version}</Typography.Text>}
        <Alert showIcon role="status" type={error || status?.phase === 'error' ? 'error' : 'info'}
          message={error || status?.message || '正在连接更新器…'} />
        {(status?.phase === 'downloading' || status?.ready_to_install) && <div>
          <Progress percent={percent} status={status.ready_to_install ? 'success' : 'active'} />
          <Typography.Text type="secondary">
            已下载 {(status.downloaded_bytes / 1048576).toFixed(1)} MiB
            {status.total_bytes ? ` / ${(status.total_bytes / 1048576).toFixed(1)} MiB` : ''}
          </Typography.Text>
        </div>}
        {status?.notes && <Typography.Paragraph style={{ whiteSpace: 'pre-wrap', maxHeight: 240, overflowY: 'auto', marginBottom: 0 }}>{status.notes}</Typography.Paragraph>}
        <Typography.Text type="secondary">下载期间可以继续使用。安装会停止后台并重启程序，业务数据保留。</Typography.Text>
      </Space>
    </Modal>
  </>;
}
