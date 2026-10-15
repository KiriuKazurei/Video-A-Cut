/**
 * 新手引导（antd Tour）的显示策略：纯函数，便于单元测试。
 * - 首次启动自动弹出一次；完成或跳过后写入带版本号的标记，之后只能从页头「新手引导」重新打开。
 * - 自动化浏览器（navigator.webdriver）默认不自动弹出，避免遮挡验收脚本；`?tour=0` 强制关闭，`?tour=1` 强制打开。
 * - 引导内容改版时递增 TOUR_VERSION，老用户会再看到一次。
 */
export const TOUR_VERSION = 1;
export const TOUR_STORAGE_KEY = `vac_tour_seen_v${TOUR_VERSION}`;

export interface TourStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

/** 读取 `?tour=` 参数（也兼容写在 hash 里的 `#/editing?tour=0` 形式）。 */
export function tourOverride(search: string, hash = ''): 'on' | 'off' | null {
  const read = (raw: string) => {
    const value = new URLSearchParams(raw.startsWith('?') ? raw.slice(1) : raw).get('tour');
    return value === '0' || value === 'off' ? 'off' : value === '1' || value === 'on' ? 'on' : null;
  };
  const fromSearch = read(search);
  if (fromSearch) return fromSearch;
  const q = hash.indexOf('?');
  return q >= 0 ? read(hash.slice(q)) : null;
}

export function hasSeenTour(storage: TourStorage | null | undefined): boolean {
  try { return storage?.getItem(TOUR_STORAGE_KEY) === '1'; } catch { return false; }
}

export function markTourSeen(storage: TourStorage | null | undefined): void {
  try { storage?.setItem(TOUR_STORAGE_KEY, '1'); } catch { /* 隐私模式等写入失败：本次会话内不再弹即可 */ }
}

export function resetTour(storage: TourStorage | null | undefined): void {
  try { storage?.removeItem(TOUR_STORAGE_KEY); } catch { /* ignore */ }
}

export interface TourContext {
  storage?: TourStorage | null;
  search?: string;
  hash?: string;
  /** navigator.webdriver：自动化浏览器 */
  automated?: boolean;
}

export function shouldAutoShowTour({ storage, search = '', hash = '', automated = false }: TourContext): boolean {
  const override = tourOverride(search, hash);
  if (override === 'off') return false;
  if (override === 'on') return true;
  if (automated) return false;
  return !hasSeenTour(storage);
}
