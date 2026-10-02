import { useMemo, useState } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import type { Asset } from '../types';
import { ApiError } from '../api';
import { Icon } from './Icon';

function message(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}（${error.code}）`;
  return error instanceof Error ? error.message : '发生未知错误';
}

export const kindLabel = (asset: Asset) => asset.input_kind === 'raw_recording' ? '原始录像' : 'EDL 包';

/** 项目面板（Premiere Project 面板）：服务端资产列表与本地筛选（API-01）。 */
export function MediaBin({ assets, selectedId, onSelect }: { assets: UseQueryResult<Asset[]>; selectedId?: string; onSelect: (id: string) => void }) {
  const [search, setSearch] = useState('');
  const visible = useMemo(() => (assets.data ?? []).filter((item) =>
    item.asset_id.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [assets.data, search]);
  return (
    <section className="bin-panel" aria-labelledby="assets-title">
      <div className="dock-heading"><h2 id="assets-title">项目 · 资产</h2><span className="count">{assets.data?.length ?? '—'} 项</span></div>
      <div className="bin-search">
        <Icon name="search" />
        <label className="sr-only" htmlFor="asset-search">按资产 ID 筛选</label>
        <input id="asset-search" type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="按资产 ID 筛选" />
      </div>
      {assets.isPending && <p className="state">正在读取资产…</p>}
      {assets.isError && <div className="state state-error" role="alert">资产读取失败：{message(assets.error)}
        <button type="button" onClick={() => void assets.refetch()}>重试</button></div>}
      {assets.isSuccess && assets.data.length === 0 && <p className="state">暂无资产。在「导入」页登记录像或导入交付包后会出现在这里。</p>}
      {assets.isSuccess && assets.data.length > 0 && visible.length === 0 && <p className="state">没有匹配的资产。</p>}
      <div className="asset-list">
        {visible.map((item) => <button key={item.asset_id} type="button"
          className={`asset-row ${selectedId === item.asset_id ? 'is-selected' : ''}`}
          aria-pressed={selectedId === item.asset_id}
          onClick={() => onSelect(item.asset_id)}>
          <span className={`asset-thumb kind-${item.input_kind ?? 'edl_package'}`}><Icon name={item.input_kind === 'raw_recording' ? 'film' : 'edit'} size={18} /></span>
          <span className="asset-text">
            <span className="asset-name">{item.asset_id}</span>
            <small>{kindLabel(item)} · {item.locked ? '已锁定' : item.agent_visible ? 'Agent 可见' : 'Agent 不可见'}</small>
          </span>
          <span className="asset-status">{item.status}</span>
        </button>)}
      </div>
    </section>
  );
}
