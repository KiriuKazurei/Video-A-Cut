import type { ReactNode } from 'react';
import { Badge, Button, Card, List, Space, Tag, Typography } from 'antd';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { UseQueryResult } from '@tanstack/react-query';
import { api } from '../api';
import type { ConnectionStatus } from '../types';
import { DEFAULT_TTS_SETTINGS } from '../ttsContracts';

type Tone = 'success' | 'processing' | 'warning' | 'default' | 'error';

interface StatusMonitorProps {
  connection: ConnectionStatus;
  events: {
    retryCount: number; maxRetries: number; retryExhausted: boolean; nextRetryLabel: string | null;
    reconnect: () => void; disconnect: () => void;
  };
  /** 资产列表查询：其成败即控制面 HTTP 是否可达。 */
  controlPlane: UseQueryResult<unknown, Error>;
  /** 审计查询：审计接口可用性。 */
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

/** 状态监控：集中展示后端系统状态。主界面不再常驻这些指示，普通用户无需关心。 */
export function StatusMonitor({ connection, events, controlPlane, audit }: StatusMonitorProps) {
  const queryClient = useQueryClient();
  // 与 TtsWorkspace 同一 queryKey 形态：['tts-capabilities', tokenEnv]
  const tokenEnv = (DEFAULT_TTS_SETTINGS.token_env || 'VAC_TTS_TOKEN').trim() || 'VAC_TTS_TOKEN';
  const controlPlaneDown = controlPlane.isError;
  const tts = useQuery({
    queryKey: ['tts-capabilities', tokenEnv],
    queryFn: () => api.ttsCapabilities(tokenEnv),
    // 控制面不可达时不拉 TTS，避免展示陈旧缓存文案
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
    ? '控制面不可达，暂不查询 TTS 运行环境（避免显示过期缓存）。'
    : (tts.data?.message || 'TTS 工作进程的运行时与媒体转码依赖是否就绪。');
  const rows: Row[] = [
    { key: 'control', title: '控制面连接', ...queryState(controlPlane, '可达', '不可达'),
      description: '本地控制面 HTTP 接口；资产、任务与设置的读写都经过它。' },
    { key: 'sse', title: '事件流 (SSE)', ...sse,
      description: '任务进度与状态变化的实时推送；断开时页面改为手动刷新才更新。',
      extra: <Space size={4} wrap>
        <Tag bordered={false} data-testid="connection-retry">{retry}</Tag>
        {events.retryExhausted && <Tag color="error">已达重试上限</Tag>}
        {/* 重连中也可点：打断退避并立即重开（useEvents.reconnect） */}
        <Button size="small" disabled={connection === 'connecting'} onClick={events.reconnect}>立即重连</Button>
        <Button size="small" disabled={connection === 'disconnected'} onClick={events.disconnect}>断开</Button>
      </Space> },
    { key: 'audit', title: '审计日志接口', ...queryState(audit, '可用', '不可用'),
      description: '记录每次治理与任务操作，用于追溯与诊断。' },
    { key: 'tts', title: 'TTS 运行环境', ...ttsState,
      description: ttsDescription }
  ];
  return (
    <Card title="状态监控" bordered={false} className="status-monitor"
      extra={<Button size="small" onClick={() => void queryClient.invalidateQueries({ predicate: (q) => ['assets', 'audit', 'tts-capabilities'].includes(String(q.queryKey[0])) })}>重新检测</Button>}>
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
