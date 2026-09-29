/**
 * SSE 重连退避的纯计算。
 *
 * 单独成文件、不引用 React 或 DOM，是为了让退避策略可以脱离组件树直接验证：
 * 重连时序是稳定性逻辑的核心，它错了不可能从界面上看出来，而它是纯算术，
 * 没有理由把它埋在一个 hook 里。
 */

/** 默认首次重连等待。 */
export const DEFAULT_BASE_DELAY_MS = 1_000;

/** 默认重连等待上限：再赌下去只会让后端更喘。 */
export const DEFAULT_MAX_DELAY_MS = 30_000;

/** 默认放弃前最多重试几次。 */
export const DEFAULT_MAX_RETRIES = 8;

/**
 * 第 `attempt` 次重连应等待的毫秒数（attempt 从 0 开始）。
 *
 * 指数退避：`base * 2^attempt`，被上限截断。attempt 为 0 时是首次重连，
 * 返回 base 本身。负的与非有限的 attempt 一律按 0 处理，因为一个被上游
 * 错误计算的次数不该让这里返回 NaN——NaN 会一路传到 setTimeout，
 * 而 setTimeout 收到 NaN 会立刻触发，等于退避完全失效。
 */
export function computeBackoffDelay(
  attempt: number,
  baseDelayMs: number = DEFAULT_BASE_DELAY_MS,
  maxDelayMs: number = DEFAULT_MAX_DELAY_MS
): number {
  if (!Number.isFinite(attempt) || attempt < 0) attempt = 0;
  if (!Number.isFinite(baseDelayMs) || baseDelayMs < 0) baseDelayMs = DEFAULT_BASE_DELAY_MS;
  if (!Number.isFinite(maxDelayMs) || maxDelayMs < 0) maxDelayMs = DEFAULT_MAX_DELAY_MS;
  // 上限就是上限：调用方把 maxDelay 调到比 base 小，就必须被尊重，
  // 否则 attempt 0 的 base * 2^0 会绕过它，"上限"名不副实。
  const raw = baseDelayMs * 2 ** Math.floor(attempt);
  return Math.min(raw, maxDelayMs);
}

/**
 * 抖动量：在 [half, delay] 之间取一个值，避免多个客户端在同一秒同时重连。
 *
 * 返回的是放缩系数（0.5–1），不是毫秒数，所以调用方可以自行决定是否启用。
 */
export function jitterScale(half: number, random: () => number = Math.random): number {
  return half + (1 - half) * random();
}

/** 重试预算是否已用尽。 */
export function isRetryExhausted(retryCount: number, maxRetries: number): boolean {
  return retryCount >= maxRetries;
}

/** 把等待毫秒数格式化成可读文本（中文界面用）。 */
export function formatRetryDelay(delayMs: number): string {
  const seconds = delayMs / 1_000;
  if (seconds < 1) return `${Math.round(delayMs)} 毫秒`;
  return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)} 秒`;
}
