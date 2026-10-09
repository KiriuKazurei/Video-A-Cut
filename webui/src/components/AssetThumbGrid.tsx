import { Button, Empty, Tag, Tooltip, Typography } from 'antd';
import { CheckCircleFilled, CheckCircleOutlined, EyeInvisibleOutlined, EyeOutlined, FileTextOutlined, LockOutlined, VideoCameraOutlined } from '@ant-design/icons';
import type { AssetRow } from './AssetManager';
import { categoryText } from './governanceModel';

export interface AssetThumbGridProps {
  rows: readonly AssetRow[];
  selectedId?: string;
  pendingId?: string;
  onSelect: (assetId: string) => void;
  /** 切换 Agent 可见性：调用方走真实的 PATCH /api/assets/{id}（agent_visible）。 */
  onToggleVisible: (row: AssetRow) => void;
}

/**
 * 资源缩略图视图。
 * 右下角对勾 = 已入库（服务端资产即已入库；本地草稿未入库，显示为暗色空心对勾）；
 * 左下角眼睛 = Agent 可见（资产 agent_visible 字段），点击走真实治理 PATCH；本地草稿没有服务端字段，眼睛禁用。
 * 控制面没有资产缩略图/海报接口，因此用类型占位图。
 */
export function AssetThumbGrid({ rows, selectedId, pendingId, onSelect, onToggleVisible }: AssetThumbGridProps) {
  if (rows.length === 0) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有可显示的资产" />;
  return (
    <div className="asset-grid" role="list" aria-label="资产缩略图">
      {rows.map((row, index) => {
        const { asset, draft } = row;
        const inLibrary = !draft;
        const visible = asset.agent_visible;
        const selected = selectedId === asset.asset_id;
        const Icon = asset.input_kind === 'raw_recording' ? VideoCameraOutlined : FileTextOutlined;
        const visibleTip = draft ? '本地草稿没有 Agent 可见字段' : asset.locked ? '资产已锁定，需先解锁才能修改可见性' : visible ? 'Agent 可见 · 点击设为不可见' : 'Agent 不可见 · 点击设为可见';
        return (
          <div key={asset.asset_id} role="listitem" data-asset-card={asset.asset_id}
            className={`asset-card${selected ? ' is-selected' : ''}${inLibrary ? '' : ' is-draft'}`} style={{ animationDelay: `${Math.min(index, 12) * 18}ms` }}>
            {/* 选择按钮与眼睛/入库徽标是同级兄弟：徽标绝对定位叠在缩略图上，不嵌在按钮里（避免 button 套 button） */}
            <div className="asset-card-media">
              <button type="button" className="asset-card-thumb" aria-pressed={selected} aria-label={`选择资产 ${asset.asset_id}`} onClick={() => onSelect(asset.asset_id)}>
                <Icon className="asset-card-placeholder" aria-hidden="true" />
                {asset.locked && <LockOutlined className="asset-card-lock" aria-label="已锁定" />}
              </button>
              <span className="asset-card-badges">
                <Tooltip title={visibleTip}>
                  <span className="asset-badge-slot">
                    <Button size="small" type="text" className={`asset-badge asset-badge-eye${visible ? ' is-on' : ''}`}
                      disabled={Boolean(draft) || asset.locked || pendingId === asset.asset_id}
                      aria-label={visible ? `${asset.asset_id} Agent 可见，点击设为不可见` : `${asset.asset_id} Agent 不可见，点击设为可见`} aria-pressed={visible}
                      icon={visible ? <EyeOutlined /> : <EyeInvisibleOutlined />}
                      onClick={() => onToggleVisible(row)} />
                  </span>
                </Tooltip>
                <Tooltip title={inLibrary ? `已入库（状态：${row.statusLabel}）` : '未入库（本地草稿，未写入控制面）'}>
                  <span className={`asset-badge asset-badge-check${inLibrary ? ' is-on' : ''}`} role="img" aria-label={inLibrary ? '已入库' : '未入库'}>
                    {inLibrary ? <CheckCircleFilled /> : <CheckCircleOutlined />}
                  </span>
                </Tooltip>
              </span>
            </div>
            <div className="asset-card-meta">
              <Typography.Text strong ellipsis={{ tooltip: asset.asset_id }}>{asset.asset_id}</Typography.Text>
              <Typography.Text type="secondary" ellipsis style={{ fontSize: 12 }}>{categoryText(row.categories) || (asset.input_kind === 'raw_recording' ? '原始录像' : '无产物记录')}</Typography.Text>
              <div>{draft ? <Tag color="warning">草稿</Tag> : <Tag>{row.statusLabel}</Tag>}</div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
