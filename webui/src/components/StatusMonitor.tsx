import type { ReactNode } from 'react';
import { Badge, Button, Card, List, Space, Tag, Typography } from 'antd';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { UseQueryResult } from '@tanstack/react-query';
import { api, ingestApi } from '../api';
import type { ConnectionStatus } from '../types';
import { DEFAULT_TTS_SETTINGS } from '../ttsContracts';
import { isHumanUnlockRequired } from '../skillView';

type Tone = 'success' | 'processing' | 'warning' | 'default' | 'error';

interface StatusMonitorProps {
  connection: ConnectionStatus;
  events: {
    retryCount: number; maxRetries: number; retryExhausted: boolean; nextRetryLabel: string | null;
    reconnect: () => void; disconnect: () => void;
  };
  controlPlane: UseQueryResult<unknown, Error>;
  audit: UseQueryResult<unknown, Error>;
}

const SSE_STATE: Record<ConnectionStatus, { tone: Tone; label: string }> = {
  connecting: { tone: 'processing', label: '事件流连接中' },
  connected: { tone: 'success', label: '事件流已连接' },
  reconnecting: { tone: 'warning', label: '事件流重连中' },
  disconnected: { tone: 'default', label: '事件流已断开' },
  error: { tone: 'error', label: '事件流连接失败' }
};

function queryState(q: UseQueryResult<unknown, Error>, ok: string, bad: string): { tone: Tone; label: string } {
  if (q.isError) return { tone: 'error', label: bad };
  if (q.isPending) return { tone: 'processing', label: '检测中' };
  return { tone: 'success', label: ok };
}

interface Row { key: string; title: string; tone: Tone; label: string; description: string; extra?: ReactNode }

/** 状态监控：后端/系统状态集中于此，主流程界面不常驻展示。 */
export function StatusMonitor({ connection, events, controlPlane, audit }: StatusMonitorProps) {
  const queryClient = useQueryClient();
  const tokenEnv = (DEFAULT_TTS_SETTINGS.token_env || 'VAC_TTS_TOKEN').trim() || 'VAC_TTS_TOKEN';
  const controlPlaneDown = controlPlane.isError;
  const tts = useQuery({
    queryKey: ['tts-capabilities', tokenEnv],
    queryFn: () => api.ttsCapabilities(tokenEnv),
    enabled: !controlPlaneDown
  });
  const ingestRoots = useQuery({
    queryKey: ['ingest', 'roots'],
    queryFn: ingestApi.roots,
    enabled: !controlPlaneDown
  });
  const sse = SSE_STATE[connection];
  const retry = `重连 ${events.retryCount}/${events.maxRetries}${connection === 'reconnecting' && events.nextRetryLabel ? ` · 下次 ${events.nextRetryLabel}` : ''}`;
  const ttsState: { tone: Tone; label: string } = controlPlaneDown
    ? { tone: 'default', label: '未检测' }
    : tts.isError ? { tone: 'error', label: '不可达' }
    : tts.isPending ? { tone: 'processing', label: '检测中' }
    : tts.data?.runtime_ready && tts.data?.media_ready ? { tone: 'success', label: '就绪' } : { tone: 'warning', label: '未就绪' };
  const ttsDescription = controlPlaneDown
    ? '控制面不可达，暂不查询语音试听运行环境。'
    : (tts.data?.message || '语音试听所需的运行环境是否就绪。');

  let ingestState: { tone: Tone; label: string };
  let ingestDescription: string;
  if (controlPlaneDown) {
    ingestState = { tone: 'default', label: '未检测' };
    ingestDescription = '控制面不可达，暂不查询本机导入服务。';
  } else if (ingestRoots.isError) {
    ingestState = { tone: 'error', label: '不可达' };
    ingestDescription = '无法读取导入配置。';
  } else if (ingestRoots.isPending) {
    ingestState = { tone: 'processing', label: '检测中' };
    ingestDescription = '正在读取本机导入服务状态…';
  } else if (!ingestRoots.data?.configured) {
    ingestState = { tone: 'default', label: '未配置' };
    ingestDescription = '控制面未配置录像根目录时，本机导入不可用。';
  } else {
    const n = ingestRoots.data.ingesters.filter((i) => i.online !== false).length;
    ingestState = n > 0 ? { tone: 'success', label: `${n} 个在线` } : { tone: 'warning', label: '暂无在线' };
    ingestDescription = '负责把录像拷入工作区并自动切分的本机服务。离线时任务会排队。';
  }

  const rows: Row[] = [
    { key: 'control', title: '控制面连接', ...queryState(controlPlane, '可达', '不可达'),
      description: '本地控制服务；素材、任务与设置的读写都经过它。' },
    { key: 'sse', title: '事件流 (SSE)', ...sse,
      description: '任务进度的实时更新；断开后需手动刷新才看到最新状态。',
      extra: <Space size={4} wrap>
        <Tag bordered={false} data-testid="connection-retry">{retry}</Tag>
        {events.retryExhausted && <Tag color="error">已达重试上限</Tag>}
        <Button size="small" disabled={connection === 'connecting'} onClick={events.reconnect}>立即重连</Button>
        <Button size="small" disabled={connection === 'disconnected'} onClick={events.disconnect}>断开</Button>
      </Space> },
    { key: 'ingest', title: '本机导入服务', ...ingestState, description: ingestDescription },
    { key: 'audit', title: '操作记录接口', ...(isHumanUnlockRequired(audit.error) ? { tone: 'warning' as Tone, label: '需要解锁' } : queryState(audit, '可用', '不可用')),
      description: isHumanUnlockRequired(audit.error) ? '审计请求未完成，请检查当前工程或重试。' : '记录治理与任务操作，用于排查问题。' },
    { key: 'tts', title: '语音试听环境', ...ttsState, description: ttsDescription }
  ];
  return (
    <Card title="状态监控" bordered={false} className="status-monitor"
      extra={<Button size="small" onClick={() => void queryClient.invalidateQueries({ predicate: (q) => ['assets', 'audit', 'tts-capabilities', 'ingest'].includes(String(q.queryKey[0])) })}>重新检测</Button>}>
      <List
        dataSource={rows}
        renderItem={(row) => (
          <List.Item key={row.key} data-status={row.key} data-state={row.key === 'sse' ? connection : row.tone} extra={row.extra}>
            <List.Item.Meta
              title={<Space size={8} wrap><Typography.Text strong>{row.title}</Typography.Text>
                <Tag className="status-tag" color={row.tone === 'processing' ? 'processing' : row.tone === 'default' ? 'default' : row.tone}>
                  <Badge status={row.tone} text={row.label} />
                </Tag></Space>}
              description={<Typography.Text type="secondary" className="status-desc">{row.description}</Typography.Text>}
            />
          </List.Item>
        )}
      />
    </Card>
  );
}

export default StatusMonitor;
