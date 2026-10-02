import { useMemo, useState } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { Alert, Avatar, Badge, Button, Card, Empty, Input, List, Tag, Typography } from 'antd';
import { FileTextOutlined, VideoCameraOutlined } from '@ant-design/icons';
import type { Asset } from '../types';
import { errorText } from './ui';

export const kindLabel = (asset: Asset) => asset.input_kind === 'raw_recording' ? '原始录像' : 'EDL 包';

/** 项目面板（Premiere Project 面板）：服务端资产列表与本地筛选（API-01）。 */
export function MediaBin({ assets, selectedId, onSelect }: { assets: UseQueryResult<Asset[]>; selectedId?: string; onSelect: (id: string) => void }) {
  const [search, setSearch] = useState('');
  const visible = useMemo(() => (assets.data ?? []).filter((item) =>
    item.asset_id.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [assets.data, search]);
  return (
    <Card size="small" variant="borderless" className="dock-card" aria-labelledby="assets-title"
      title={<span id="assets-title">项目 · 资产</span>} extra={<Badge count={assets.data?.length ?? 0} showZero color="blue" />}>
      <Input.Search id="bin-search" allowClear value={search} onChange={(event) => setSearch(event.target.value)}
        placeholder="按资产 ID 筛选" aria-label="按资产 ID 筛选" style={{ marginBottom: 8 }} />
      {assets.isError && <Alert type="error" showIcon message={`资产读取失败：${errorText(assets.error)}`}
        action={<Button size="small" onClick={() => void assets.refetch()}>重试</Button>} />}
      <List className="asset-list" size="small" loading={assets.isPending} dataSource={visible}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={assets.isSuccess && assets.data.length > 0 ? '没有匹配的资产' : '暂无资产。在「导入」页登记录像或导入交付包后会出现在这里。'} /> }}
        renderItem={(item) => {
          const selected = selectedId === item.asset_id;
          return <List.Item key={item.asset_id} data-asset-id={item.asset_id} role="button" tabIndex={0} aria-pressed={selected}
            className={`asset-row${selected ? ' is-selected' : ''}`}
            onClick={() => onSelect(item.asset_id)}
            onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onSelect(item.asset_id); } }}
>
            <List.Item.Meta
              avatar={<Avatar shape="square" size="small" className={item.input_kind === 'raw_recording' ? 'kind-raw' : 'kind-edl'}
                icon={item.input_kind === 'raw_recording' ? <VideoCameraOutlined /> : <FileTextOutlined />} />}
              title={<Typography.Text strong ellipsis={{ tooltip: item.asset_id }}>{item.asset_id}</Typography.Text>}
              description={<>
                <Typography.Text type="secondary" ellipsis style={{ fontSize: 12, display: 'block' }}>
                  {kindLabel(item)} · {item.locked ? '已锁定' : item.agent_visible ? 'Agent 可见' : 'Agent 不可见'}</Typography.Text>
                <Tag bordered={false} color={item.status === 'exported' ? 'success' : 'default'} style={{ marginTop: 4 }}>{item.status}</Tag>
              </>} />
          </List.Item>;
        }} />
    </Card>
  );
}
