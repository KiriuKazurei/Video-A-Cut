import { useCallback, useEffect, useState } from 'react';

/**
 * 工作区页面：顺序对应 Premiere 式工作流（导入 → 粗剪 → 准备 → 编辑审查 → 导出），
 * 最后是不属于剪辑流程的监控/审计页。页面只是前端组织方式，不代表服务端状态。
 */
export type PageId = 'import' | 'assembly' | 'prepare' | 'edit' | 'export' | 'monitor';

export interface PageDef {
  id: PageId;
  step: string;
  label: string;
  english: string;
  description: string;
  /**
   * 本页实际调用的接口编号（docs/frontend-api-inventory.json）。API-01/03/50 也由常驻的
   * 项目面板、属性面板和状态栏使用；API-02、22、27、51、52 没有界面入口（见 README）。
   */
  apis: string;
}

export const PAGES: readonly PageDef[] = [
  { id: 'import', step: '01', label: '导入', english: 'Import', description: '项目素材、录像登记、交付包导入与资产库', apis: 'API-01 · 03~06 · 42 · 44/45 · 48/49' },
  { id: 'assembly', step: '02', label: '粗剪', english: 'Assembly', description: '探测、场景切分、候选选段、拆分合并与生成短片', apis: 'API-03 · 06~16 · 19' },
  { id: 'prepare', step: '03', label: '准备', english: 'Prepare', description: '处理预设、服务商诊断、外发授权与运行预检', apis: 'API-17~21 · 23~26' },
  { id: 'edit', step: '04', label: '编辑', english: 'Edit', description: '内容流程、场景审查、解说改稿与序列时间线', apis: 'API-28~38' },
  { id: 'export', step: '05', label: '导出', english: 'Export', description: '节目预览、试听、交付文件、ZIP 与人工验收', apis: 'API-28 · 30 · 39~41 · 43~46' },
  { id: 'monitor', step: '06', label: '监控', english: 'Monitor', description: '任务派发与观察、审计记录、事件流连接', apis: 'API-47~50' }
];

const DEFAULT_PAGE: PageId = 'import';

function readHash(): PageId {
  const raw = window.location.hash.replace(/^#\/?/, '').split(/[/?]/)[0];
  return (PAGES.find((page) => page.id === raw)?.id) ?? DEFAULT_PAGE;
}

/** 以 URL hash（#/edit）保存当前页面：可刷新恢复、可深链接，浏览器后退按预期工作。 */
export function useHashPage(): [PageId, (page: PageId) => void] {
  const [page, setPage] = useState<PageId>(readHash);
  useEffect(() => {
    const sync = () => setPage(readHash());
    window.addEventListener('hashchange', sync);
    return () => window.removeEventListener('hashchange', sync);
  }, []);
  const navigate = useCallback((next: PageId) => {
    if (readHash() !== next || !window.location.hash) window.location.hash = `/${next}`;
    setPage(next);
  }, []);
  return [page, navigate];
}
