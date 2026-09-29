import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, ApiError } from '../api';
import type { Task } from '../types';
import { isTaskActiveStatus, isTaskTerminalStatus, taskProgressPercent } from '../types';

/** 任务编排控制台：展示任务实时生命周期进度、阶段详情与状态。 */
export interface TaskOrchestratorProps {
  /** 当前跟踪的任务 ID；null 表示尚未派发任务。 */
  taskId: string | null;
  /**
   * 可选的父组件 query 数据。传入时组件复用父级缓存，不再自行发起轮询，
   * 否则同一个任务会被两条 15 秒轮询各拉一次。
   */
  task?: Task | undefined;
  /** 父组件传入的读取中状态；未传 `task` 时忽略。 */
  isPending?: boolean;
  /** 父组件传入的读取错误；未传 `task` 时忽略。 */
  isError?: boolean;
  /** 父组件传入的错误对象；未传 `task` 时忽略。 */
  error?: unknown;
}

function failMessage(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

/** 阶段详情：由 status 派生，不发明后端没有的阶段名。 */
function phaseDetail(status: string, progress: number, hasDeps = false): string {
  switch (status) {
    case 'queued':
      return hasDeps ? '排队中：等待前置任务全部成功后才可被认领' : '排队中：等待 Agent 认领';
    case 'claimed':
      return '已认领：Agent 正在准备执行';
    case 'running':
      return `执行中：${progress}%`;
    case 'paused':
      return '已暂停：等待手动恢复';
    case 'succeeded':
      return '已完成：产出已记录到资产';
    case 'failed':
      return '已失败：执行异常';
    case 'cancelled':
      return '已取消：任务不再执行';
    default:
      return status || '未知状态';
  }
}

export function TaskOrchestrator({
  taskId,
  task,
  isPending = false,
  isError = false,
  error
}: TaskOrchestratorProps) {
  const queryClient = useQueryClient();
  // 动画用的已展示进度：真实进度由 query 驱动，这里向目标值平滑追赶，
  // 让后端两秒一次的心跳刷新之间也有可见的进度反馈。
  const [shownPercent, setShownPercent] = useState(0);
  const frameRef = useRef<number | null>(null);

  // 无条件调用：父组件没传 task 时自行读取同一份缓存，避免同一任务被两条
  // 轮询同时拉取。enabled 控制它是否真的发起请求。
  const ownQuery = useQuery({
    queryKey: ['task', taskId],
    queryFn: () => api.getTask(taskId!),
    enabled: Boolean(taskId) && task === undefined,
    refetchInterval: 15_000
  });
  const resolved = task ?? ownQuery.data;
  const loading = isPending || ownQuery.isPending;
  const failedLoad = isError || ownQuery.isError;
  const loadError = isError ? error : ownQuery.error;

  const targetPercent = taskProgressPercent(resolved?.progress);

  useEffect(() => {
    if (frameRef.current !== null) {
      cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }
    const step = () => {
      setShownPercent((current) => {
        if (current === targetPercent) {
          frameRef.current = null;
          return current;
        }
        frameRef.current = requestAnimationFrame(step);
        const delta = targetPercent - current;
        // 每帧收敛 25% 的差距：快到目标时自然减速，不会在终点抖动。
        return Math.abs(delta) < 1 ? targetPercent : current + delta * 0.25;
      });
    };
    frameRef.current = requestAnimationFrame(step);
    return () => {
      if (frameRef.current !== null) {
        cancelAnimationFrame(frameRef.current);
        frameRef.current = null;
      }
    };
  }, [targetPercent]);

  if (!taskId) {
    return (
      <section className="panel" aria-labelledby="orchestrator-title">
        <div className="panel-heading"><div><p className="eyebrow">ORCHESTRATION</p>
          <h2 id="orchestrator-title">任务编排控制台</h2></div></div>
        <p className="state">尚未派发任务。创建任务后可在此观察状态、进度与失败原因，并手动刷新任务状态。</p>
      </section>
    );
  }

  const status = resolved?.status ?? '';
  const active = status !== '' && isTaskActiveStatus(status);
  const terminal = status !== '' && isTaskTerminalStatus(status);
  const failed = status === 'failed';

  return (
    <section className="panel" aria-labelledby="orchestrator-title">
      <div className="panel-heading"><div><p className="eyebrow">ORCHESTRATION</p>
        <h2 id="orchestrator-title">任务编排控制台</h2></div>
        <span className="pill">{status || '读取中'}</span>
      </div>

      <div className="tracked-top"><strong>{taskId}</strong>
        <span>{resolved?.type ?? '—'}</span></div>

      {loading && <p className="state">正在读取任务状态…</p>}
      {!loading && failedLoad && <p className="inline-error" role="alert">
        状态读取失败：{failMessage(loadError)}
        <button type="button" className="secondary-button"
          onClick={() => {
            void queryClient.invalidateQueries({ queryKey: ['task', taskId] });
          }}>重试读取</button>
      </p>}

      {!loading && !failedLoad && <>
        <div className="progress-block">
          <div className="progress-meta">
            <span>进度</span><span>{shownPercent}%</span>
          </div>
          {/* 进度同时用文本与 aria 值表达：不只靠颜色和长度传达状态。 */}
          <div className="progress-track" role="progressbar" aria-valuenow={shownPercent}
            aria-valuemin={0} aria-valuemax={100} aria-label="任务进度">
            <div className="progress-fill" style={{ width: `${shownPercent}%` }}
              data-state={terminal ? 'terminal' : active ? 'active' : 'idle'} />
          </div>
          <p className="hint">{phaseDetail(status, shownPercent, (resolved?.depends_on?.length ?? 0) > 0)}</p>
          {resolved?.message && <p className="progress-message">{resolved.message}</p>}
        </div>

        <dl className="detail-grid">
          <div><dt>Agent 角色</dt><dd>{resolved?.agent_role ?? '—'}</dd></div>
          <div><dt>执行 Agent</dt><dd>{resolved?.agent_id ?? '未认领'}</dd></div>
          <div><dt>租约到期</dt><dd>{resolved?.lease_expires_at ?? '—'}</dd></div>
          <div><dt>最近更新</dt><dd>{resolved?.updated_at ?? '—'}</dd></div>
          <div><dt>前置任务</dt><dd>{resolved?.depends_on?.length ? resolved.depends_on.join('、') : '无'}</dd></div>
          <div><dt>回收次数</dt><dd>{resolved?.attempts ?? 0}</dd></div>
        </dl>

        <div className="action-row" aria-live="polite">
          <span className="hint">
            状态机由服务端接管，客户端仅做观察：租约到期会自动重排，达到回收上限或前置任务失败时服务端判定失败，原因见上方消息与审计日志。
          </span>
          <button type="button" className="secondary-button" onClick={() => {
            void queryClient.invalidateQueries({ queryKey: ['task', taskId] });
          }}>刷新任务状态</button>
        </div>
      </>}
    </section>
  );
}

export default TaskOrchestrator;
