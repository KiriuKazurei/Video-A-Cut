import { useMemo, useState } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { Alert, Avatar, Badge, Button, Card, Empty, Flex, Input, List, Tag, Typography, theme } from 'antd';
import { FileTextOutlined, VideoCameraOutlined } from '@ant-design/icons';
import type { Asset } from '../types';
import { errorText } from './ui';

export const kindLabel = (asset: Asset) => asset.input_kind === 'raw_recording' ? '原始录像' : 'EDL 包';

/**
 * 项目面板（Premiere Project 面板）：服务端资产列表与本地筛选（API-01）。
 * 全部是 antd 默认暗色组件；选中项只用 antd 的选中底色 token（controlItemBgActive，与 Menu/Select/Tree 相同）。
 */
export function MediaBin({ assets, selectedId, onSelect }: { assets: UseQueryResult<Asset[]>; selectedId?: string; onSelect: (id: string) => void }) {
  const { token } = theme.useToken();
  const [search, setSearch] = useState('');
  const visible = useMemo(() => (assets.data ?? []).filter((item) =>
    item.asset_id.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [assets.data, search]);
  return (
    <Card size="small" variant="borderless" aria-labelledby="assets-title"
      title={<span id="assets-title">项目 · 资产</span>} extra={<Badge count={assets.data?.length ?? 0} showZero color="blue" />}>
      <Input.Search id="bin-search" allowClear value={search} onChange={(event) => setSearch(event.target.value)}
        placeholder="按资产 ID 筛选" aria-label="按资产 ID 筛选" style={{ marginBottom: 8 }} />
      {assets.isError && <Alert type="error" showIcon message={`资产读取失败：${errorText(assets.error)}`}
        action={<Button size="small" onClick={() => void assets.refetch()}>重试</Button>} />}
      {/* antd List 会把内部 outline 置空；键盘焦点按 antd genFocusOutline 的线宽（lineWidthFocus）补回，颜色用 colorPrimary 以保证 ≥3:1 的焦点对比度。 */}
      <List className="asset-list" size="small" style={{ '--vac-focus-outline': `${token.lineWidthFocus}px solid ${token.colorPrimary}` } as React.CSSProperties} loading={assets.isPending} dataSource={visible}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={assets.isSuccess && assets.data.length > 0 ? '没有匹配的资产' : '暂无资产。在「导入」页登记录像或导入交付包后会出现在这里。'} /> }}
        renderItem={(item) => {
          const selected = selectedId === item.asset_id;
          return <List.Item key={item.asset_id} data-asset-id={item.asset_id} role="button" tabIndex={0} aria-pressed={selected}
            style={{ cursor: 'pointer', background: selected ? token.controlItemBgActive : undefined }}
            onClick={() => onSelect(item.asset_id)}
            onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onSelect(item.asset_id); } }}>
            <List.Item.Meta
              avatar={<Avatar shape="square" size="small"
                icon={item.input_kind === 'raw_recording' ? <VideoCameraOutlined /> : <FileTextOutlined />} />}
              title={<Typography.Text strong ellipsis={{ tooltip: item.asset_id }}>{item.asset_id}</Typography.Text>}
              description={<Flex vertical align="flex-start" gap={4}>
                <Typography.Text type="secondary" ellipsis style={{ maxWidth: '100%' }}>
                  {kindLabel(item)} · {item.locked ? '已锁定' : item.agent_visible ? 'Agent 可见' : 'Agent 不可见'}</Typography.Text>
                <Tag color={item.status === 'exported' ? 'success' : 'default'}>{item.status}</Tag>
              </Flex>} />
          </List.Item>;
        }} />
    </Card>
  );
}
