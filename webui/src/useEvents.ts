import { useCallback, useEffect, useRef, useState } from 'react';
import type { QueryClient } from '@tanstack/react-query';
import type { ConnectionStatus } from './types';
import {
  DEFAULT_BASE_DELAY_MS,
  DEFAULT_MAX_DELAY_MS,
  DEFAULT_MAX_RETRIES,
  computeBackoffDelay,
  formatRetryDelay
} from './sseBackoff';

export const EVENTS_PATH = '/api/events';

/** useEvents 的可调参数：连接端点与退避策略。 */
export interface UseEventsOptions {
  /** SSE 端点，默认 `/api/events`。 */
  url?: string;
  /** 首次重连等待基数，毫秒。 */
  baseDelayMs?: number;
  /** 单次重连等待上限，毫秒。 */
  maxDelayMs?: number;
  /** 放弃前最多重试次数。 */
  maxRetries?: number;
}

/** useEvents 的返回值：连接状态与控制入口。 */
export interface UseEventsResult {
  /** 当前连接状态。 */
  status: ConnectionStatus;
  /** 本次挂载内累计发生的自动重连次数（连接成功后清零）。 */
  retryCount: number;
  /** 是否已用尽重连预算（此时"立即重连"仍可手动重置）。 */
  retryExhausted: boolean;
  /** 重连预算上限，供状态栏展示剩余额度。 */
  maxRetries: number;
  /** 下一次自动重连的等待，毫秒；未在等待中则为 null。 */
  nextRetryInMs: number | null;
  /** 人类可读的下次重连等待；未在等待中则为 null。 */
  nextRetryLabel: string | null;
  /** 手动触发一次重连：清空退避并立即重开连接。 */
  reconnect: () => void;
  /** 主动断开：取消所有定时器，停止重连，状态置为 disconnected。 */
  disconnect: () => void;
}

/**
 * 订阅 Go 控制面的 SSE 事件流，并在断线时以指数退避重连。
 *
 * 三条约束来自文档而非实现便利：
 *
 * 1. **SSE 没有历史回放。** 每次连接成功（含自动重连）都必须先刷新 REST
 *    快照，再把界面当作最新。
 * 2. **事件只是"有变化"的通知。** 收到事件只让对应 Query 失效，不把事件
 *    负载当作新的权威状态来源。
 * 3. **断线不清空已有快照。** 重连期间保持上一批数据可见，只改连接状态。
 *
 * 重连由这里接管而不交给浏览器：`EventSource` 的原生重连间隔是规范固定的
 * 约 3 秒，没有次数上限，也无法在界面上报告进度与剩余预算。因此 `onerror`
 * 一到就关闭旧流，由退避定时器在 `base * 2^n`（受上限截断）之后重开一个
 * 全新的连接。
 */
export function useEvents(
  queryClient: QueryClient,
  watchedTaskId: string | null,
  options: UseEventsOptions = {}
): UseEventsResult {
  const {
    url = EVENTS_PATH,
    baseDelayMs = DEFAULT_BASE_DELAY_MS,
    maxDelayMs = DEFAULT_MAX_DELAY_MS,
    maxRetries = DEFAULT_MAX_RETRIES
  } = options;

  // generation 是"重开连接"的信号：手动重连、端点变化、退避到时时都通过
  // +1 让下面的连接 effect 重建 EventSource。把重开收敛到这一个依赖上，
  // 就不会出现多条散落的建流路径。
  const [generation, setGeneration] = useState(0);
  const [status, setStatus] = useState<ConnectionStatus>('connecting');
  const [retryCount, setRetryCount] = useState(0);
  const [nextRetryInMs, setNextRetryInMs] = useState<number | null>(null);
  const retryExhausted = retryCount >= maxRetries;

  // attemptRef 保存退避到第几阶，用 ref 而不是 state：它只驱动重连决策，
  // 不进渲染结果，放 state 会让每次退避都触发一次多余的渲染。
  const attemptRef = useRef(0);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // 手动断开标记。EventSource 的 onerror 会在 close() 之后依然触发
  // （浏览器对主动关闭也报错），没有这个标记的话主动断开会立刻被当成
  // 断线并重连，disconnect() 就形同虚设。
  const stoppedRef = useRef(false);
  const activeSourceRef = useRef<EventSource | null>(null);
  // 只让 watcher 引用最新的回调，避免 queryClient/watchedTaskId 变化时
  // 重建连接（重建会丢退避进度，并让"断线后仍能刷新"退化成反复连接）。
  const refreshRef = useRef<() => void>(() => {});

  const refreshSnapshot = useCallback(() => {
    void queryClient.invalidateQueries({queryKey:['processing-profiles']});
    void queryClient.invalidateQueries({queryKey:['prepared']});
    void queryClient.invalidateQueries({ queryKey: ['workflows'] });
    void queryClient.invalidateQueries({ queryKey: ['review'] });
    void queryClient.invalidateQueries({ queryKey: ['acceptance'] });
    void queryClient.invalidateQueries({ queryKey: ['workflow'] });
    void queryClient.invalidateQueries({ queryKey: ['assets'] });
    void queryClient.invalidateQueries({ queryKey: ['audit'] });
    void queryClient.invalidateQueries({ queryKey: ['ingest'] });
    if (watchedTaskId) void queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] });
  }, [queryClient, watchedTaskId]);

  useEffect(() => {
    refreshRef.current = refreshSnapshot;
  }, [refreshSnapshot]);

  const clearRetryTimer = useCallback(() => {
    if (retryTimerRef.current !== null) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
    setNextRetryInMs(null);
  }, []);

  /** 停止所有重连并关闭当前活动流。 */
  const disconnect = useCallback(() => {
    stoppedRef.current = true;
    clearRetryTimer();
    if (activeSourceRef.current !== null) {
      activeSourceRef.current.close();
      activeSourceRef.current = null;
    }
    setStatus('disconnected');
  }, [clearRetryTimer]);

  /** 手动重连：重置退避计数后立即打开新连接。 */
  const reconnect = useCallback(() => {
    stoppedRef.current = false;
    attemptRef.current = 0;
    clearRetryTimer();
    setRetryCount(0);
    setStatus('connecting');
    // bump 是 effect 的依赖：它变化时 effect 重跑，正是"重开一个连接"。
    setGeneration((n) => n + 1);
  }, [clearRetryTimer]);

  useEffect(() => {
    if (stoppedRef.current) return undefined;

    const source = new EventSource(url);
    activeSourceRef.current = source;
    let closed = false;

    /** 安排一次指数退避后的重连；预算耗尽则转入 error 并不再自愈。 */
    const scheduleRetry = () => {
      if (closed || stoppedRef.current) return;
      // attemptRef 是 ref：读到的总是最新值，不是渲染快照。
      if (attemptRef.current >= maxRetries) {
        setStatus('error');
        setNextRetryInMs(null);
        return;
      }
      const attempt = attemptRef.current;
      const delay = computeBackoffDelay(attempt, baseDelayMs, maxDelayMs);
      attemptRef.current = attempt + 1;
      setRetryCount(attempt + 1);
      setStatus('reconnecting');
      setNextRetryInMs(delay);
      retryTimerRef.current = setTimeout(() => {
        retryTimerRef.current = null;
        setNextRetryInMs(null);
        // 重开用新建的 EventSource 而不是 source.close() 后调 open()：
        // 闭源重开是已知的浏览器不一致行为，重建连接在语义上等同且可靠。
        // 这一条正是上限与计数可信的前提。
        setGeneration((n) => n + 1);
      }, delay);
    };

    source.onopen = () => {
      if (closed) return;
      attemptRef.current = 0;
      setRetryCount(0);
      setStatus('connected');
      // The Go stream has no replay. Every open, including auto-reconnect,
      // must reconcile from REST before treating the screen as current.
      refreshRef.current();
    };

    source.onerror = () => {
      if (closed) return;
      // 先关旧流再排退避：留着它只会让浏览器按自己固定的约 3 秒间隔重试，
      // 与这里的退避竞速，上限与计数就都不可信了。
      source.close();
      scheduleRetry();
    };

    for (const name of ['asset_created', 'asset_updated']) {
      source.addEventListener(name, () => {
        void queryClient.invalidateQueries({ queryKey: ['assets'] });
        void queryClient.invalidateQueries({ queryKey: ['audit'] });
      });
    }
    for (const name of ['task_created', 'task_updated']) {
      source.addEventListener(name, () => {
        if (watchedTaskId) void queryClient.invalidateQueries({ queryKey: ['task', watchedTaskId] });
        void queryClient.invalidateQueries({ queryKey: ['audit'] });
        void queryClient.invalidateQueries({ queryKey: ['ingest', 'run'] });
      });
    }
    for (const name of ['ingest.changed', 'capability.changed']) {
      source.addEventListener(name, () => {
        void queryClient.invalidateQueries({ queryKey: ['ingest'] });
        void queryClient.invalidateQueries({ queryKey: ['assets'] });
      });
    }
    for (const name of ['workflow.changed', 'review.changed', 'acceptance.changed', 'profile.changed','capability.changed']) {
      source.addEventListener(name, () => {
        void queryClient.invalidateQueries({ queryKey: ['workflows'] });
        void queryClient.invalidateQueries({queryKey:['processing-profiles']});
        void queryClient.invalidateQueries({queryKey:['prepared']});
        void queryClient.invalidateQueries({ queryKey: ['workflow'] });
        void queryClient.invalidateQueries({ queryKey: ['review'] });
        void queryClient.invalidateQueries({ queryKey: ['acceptance'] });
      });
    }

    return () => {
      closed = true;
      if (activeSourceRef.current === source) {
        activeSourceRef.current = null;
      }
      clearRetryTimer();
      source.close();
    };
  }, [queryClient, watchedTaskId, url, baseDelayMs, maxDelayMs, maxRetries, generation, clearRetryTimer]);

  return {
    status,
    retryCount,
    retryExhausted,
    maxRetries,
    nextRetryInMs,
    nextRetryLabel: nextRetryInMs === null ? null : formatRetryDelay(nextRetryInMs),
    reconnect,
    disconnect
  };
}
