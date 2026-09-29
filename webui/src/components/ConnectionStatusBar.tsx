import type { ConnectionStatus } from '../types';

/** 连接状态指示器：展示 SSE 状态、重连次数，断开时提供立即重连。 */
export interface ConnectionStatusBarProps {
  /** useEvents 返回的连接状态。 */
  status: ConnectionStatus;
  /** 本次连接生命期内累计重连次数。 */
  retryCount: number;
  /** 重连预算上限；缺省时只显示已重连次数。 */
  maxRetries?: number;
  /** 是否已用尽预算。 */
  retryExhausted?: boolean;
  /** 人类可读的下次重连等待。 */
  nextRetryLabel?: string | null;
  /** 点击"立即重连"时调用。 */
  onReconnect: () => void;
  /** 可选的"断开"回调：提供时才渲染断开按钮。 */
  onDisconnect?: () => void;
}

/** 状态文案与色彩令牌。文本优先，颜色只是补充（不靠颜色单独传达状态）。 */
const STATUS_TEXT: Record<ConnectionStatus, { label: string; hint: string }> = {
  connecting: { label: '正在连接', hint: '首次连接事件流…' },
  connected: { label: '已连接', hint: '事件流实时推送中' },
  reconnecting: { label: '重连中', hint: '连接断开，按指数退避自动重试' },
  disconnected: { label: '已断开', hint: '已停止连接，可手动重连' },
  error: { label: '连接失败', hint: '重试次数已用尽，请手动重连' }
};

export function ConnectionStatusBar({
  status,
  retryCount,
  maxRetries,
  retryExhausted = false,
  nextRetryLabel = null,
  onReconnect,
  onDisconnect
}: ConnectionStatusBarProps) {
  const text = STATUS_TEXT[status];
  const retryText = maxRetries === undefined
    ? `重连次数 ${retryCount}`
    : `重连次数 ${retryCount} / ${maxRetries}`;
  const waiting = status === 'reconnecting' && nextRetryLabel !== null;

  return (
    <div className={`connection-status-bar status-${status}`} role="status" aria-live="polite">
      <span className="connection-dot" aria-hidden="true" />
      <span className="connection-status-text">
        <strong>{text.label}</strong>
        <small>{text.hint}</small>
      </span>
      <span className="connection-retry" data-testid="connection-retry">{retryText}</span>
      {waiting && <span className="connection-next">下次重连：{nextRetryLabel}</span>}
      {retryExhausted && <span className="connection-warning">已达重试上限</span>}
      {/* 断开/重连按钮在已连接时也可点：断开是主动操作，不应藏在错误态。 */}
      <button type="button" className="secondary-button"
        disabled={status === 'connecting' || status === 'reconnecting'}
        onClick={onReconnect}>立即重连</button>
      {onDisconnect && <button type="button" className="secondary-button"
        disabled={status === 'disconnected'}
        onClick={onDisconnect}>断开</button>}
    </div>
  );
}

export default ConnectionStatusBar;
