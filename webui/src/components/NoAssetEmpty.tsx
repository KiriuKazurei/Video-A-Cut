import { Button, Empty, Space } from 'antd';
import type { RouteState } from '../navigation';

/** 还没选素材时的统一空状态：说明原因，并给出两个可直接点的去处（导入 / 选择）。 */
export function NoAssetEmpty({ navigate, description = '还没有选中素材。先导入一段录像，或在资源管理里选一个已有素材。' }: {
  navigate: (state: Partial<RouteState>) => void;
  description?: string;
}) {
  return (
    <Empty className="no-asset-empty" image={Empty.PRESENTED_IMAGE_SIMPLE} description={description}>
      <Space wrap>
        <Button type="primary" className="vac-cta" onClick={() => navigate({ workspace: 'editing', editingSection: 'import' })}>去导入录像</Button>
        <Button className="vac-cta" onClick={() => navigate({ workspace: 'resources' })}>去资源管理选择素材</Button>
      </Space>
    </Empty>
  );
}
