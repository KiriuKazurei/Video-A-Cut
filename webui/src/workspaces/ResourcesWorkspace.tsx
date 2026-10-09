import { Card, Col, Descriptions, Empty, Row, Tag, Typography } from 'antd';
import type { Asset } from '../types';
import { Monitor, type PreviewSource } from '../components/Monitor';
import { filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
import AssetManager from '../components/AssetManager';
import { kindLabel } from '../components/MediaBin';
import { stamp } from '../components/ui';

interface ResourcesWorkspaceProps {
  asset?: Asset;
  onSelect: (id: string) => void;
}

/**
 * 资源管理工作区：资产库、素材预览、属性与治理、本地草稿
 */
export function ResourcesWorkspace({ asset, onSelect }: ResourcesWorkspaceProps) {
  const files = useDeliveryFiles(asset);
  const playable = files.data?.filter((file) => file.playable) ?? [];
  const first = playable.find((file) => file.mime.startsWith('video/')) ?? playable[0];
  const preview: PreviewSource = asset && first ? filePreview(asset.asset_id, first) : null;

  return (
    <div className="page-body">
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={15}>
          <Monitor 
            label="资源预览" 
            preview={preview}
            emptyTitle={asset ? '该资产暂无可预览媒体' : '未选择资产'}
            emptyHint={!asset 
              ? '在左侧项目面板选择资产，或前往编辑页登记录像、导入交付包。'
              : asset.input_kind === 'raw_recording' 
                ? '原始录像需在编辑页导入、切分并生成短片后可预览。'
                : '该资产尚无已登记的可播放产物。'
            } 
          />
        </Col>
        
        <Col xs={24} xl={9}>
          <Card size="small" title="资产属性" style={{ height: '100%' }}>
            {!asset && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未选择资产" />}
            {asset && (
              <>
                <Descriptions size="small" column={1} items={[
                  { key: 'id', label: '资产 ID', children: <Typography.Text code>{asset.asset_id}</Typography.Text> },
                  { key: 'kind', label: '输入类型', children: kindLabel(asset) },
                  { key: 'status', label: '状态', children: <Tag>{asset.status}</Tag> },
                  { key: 'created', label: '创建时间', children: stamp(asset.created_at) },
                  { key: 'files', label: '产物记录', children: `${Object.keys(asset.artifacts ?? {}).length} 项 · 可播放 ${playable.length} 个` }
                ]} />
                <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 12, marginBottom: 0 }}>
                  完整治理操作（可见性、锁定、批准）在右侧属性面板。artifacts 值是服务端记录，不是下载地址。
                </Typography.Paragraph>
              </>
            )}
          </Card>
        </Col>

        <Col span={24}>
          <AssetManager onSelect={onSelect} activeId={asset?.asset_id} />
        </Col>
      </Row>
    </div>
  );
}
