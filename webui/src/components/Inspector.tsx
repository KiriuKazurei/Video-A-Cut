import type { UseMutationResult } from '@tanstack/react-query';
import { Alert, Button, Card, Descriptions, Empty, Flex, Space, Tag, Typography } from 'antd';
import { CheckOutlined, EyeInvisibleOutlined, EyeOutlined, LockOutlined, RightOutlined, UnlockOutlined } from '@ant-design/icons';
import type { Asset, GovernancePatch } from '../types';
import type { PageId } from '../navigation';
import { kindLabel } from './MediaBin';
import { errorText, stamp } from './ui';

type Patch = UseMutationResult<Asset, Error, { id: string; values: GovernancePatch }>;

/** 属性面板（Premiere Properties）：当前资产的治理状态与 PATCH 操作（API-03）。全部为 antd 默认暗色组件。 */
export function Inspector({ asset, patch, onNavigate }: { asset?: Asset; patch: Patch; onNavigate: (page: PageId) => void }) {
  const next: { page: PageId; text: string } | null = !asset ? null
    : asset.input_kind === 'raw_recording' ? { page: 'assembly', text: '原始录像：到「粗剪」导入、切分并生成短片' }
    : asset.status === 'exported' ? { page: 'export', text: '已导出：到「导出」预览、下载与验收' }
    : { page: 'prepare', text: 'EDL 包：到「准备」预检并启动内容流程' };
  return (
    <Card size="small" variant="borderless" aria-labelledby="detail-title" title={<span id="detail-title">属性 · 资产治理</span>}>
      {!asset && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="在项目面板选择一项资产以查看状态和治理操作。" />}
      {asset && <Flex vertical gap={12}>
        <Flex align="center" gap={8} wrap>
          <Typography.Title level={5} style={{ margin: 0, overflowWrap: 'anywhere' }}>{asset.asset_id}</Typography.Title>
          <Tag color={asset.status === 'exported' ? 'success' : 'default'}>{asset.status}</Tag>
        </Flex>
        <Descriptions size="small" column={1} bordered styles={{ label: { whiteSpace: 'nowrap' } }} items={[
          { key: 'kind', label: '输入类型', children: kindLabel(asset) },
          { key: 'visible', label: 'Agent 可见', children: asset.agent_visible ? '是' : '否' },
          { key: 'locked', label: '锁定', children: asset.locked ? '是' : '否' },
          { key: 'approved', label: '人工批准', children: asset.human_approved ? '是' : '否' },
          { key: 'agents', label: '允许的角色', children: asset.allowed_agents?.join('、') || '未指定' },
          { key: 'updated', label: '更新时间', children: stamp(asset.updated_at) },
          { key: 'artifacts', label: '产物记录', children: `${Object.keys(asset.artifacts ?? {}).length} 项` },
          ...(asset.ingest_run_id ? [{ key: 'run', label: '导入运行', children: <Typography.Text code style={{ overflowWrap: 'anywhere' }}>{asset.ingest_run_id}</Typography.Text> }] : [])
        ]} />
        <Space direction="vertical" style={{ width: '100%' }}>
          <Button block type="primary" disabled={patch.isPending} icon={asset.agent_visible ? <EyeInvisibleOutlined /> : <EyeOutlined />}
            onClick={() => patch.mutate({ id: asset.asset_id, values: { agent_visible: !asset.agent_visible } })}>
            {asset.agent_visible ? '设为 Agent 不可见' : '设为 Agent 可见'}</Button>
          <Button block disabled={patch.isPending} icon={asset.locked ? <UnlockOutlined /> : <LockOutlined />}
            onClick={() => patch.mutate({ id: asset.asset_id, values: { locked: !asset.locked } })}>
            {asset.locked ? '解锁' : '锁定'}</Button>
          <Button block disabled={patch.isPending} icon={<CheckOutlined />}
            onClick={() => patch.mutate({ id: asset.asset_id, values: { human_approved: !asset.human_approved } })}>
            {asset.human_approved ? '撤销批准' : '人工批准'}</Button>
        </Space>
        {patch.isError && <Alert type="error" showIcon message={`更新失败：${errorText(patch.error)}`} />}
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>只发送修改的治理字段；人工批准资产不等于批准某句解说。修改权限可能影响活动任务。</Typography.Text>
        {next && <Alert type="info" message="建议下一步" description={next.text}
          action={<Button size="small" type="link" icon={<RightOutlined />} iconPosition="end" onClick={() => onNavigate(next.page)}>前往</Button>} />}
      </Flex>}
    </Card>
  );
}
