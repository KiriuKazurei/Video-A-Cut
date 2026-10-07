import { useEffect, useRef } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Collapse, Empty, Flex, Progress, Space, Tag, Typography } from 'antd';
import type { Asset } from '../types';
import { ApiError, deliveryVariantApi } from '../api';
import { variantFileURL, variantQueryKey, variantZIPURL, type VariantStartRequest } from '../deliveryVariantContracts';
import { useWorkflowData, type RunSelection } from './WorkflowPanel';
import { errorText } from './ui';

const active = (status: string) => ['queued', 'claimed', 'running'].includes(status);
const statusLabel: Record<string, string> = {
  queued: '等待生成', claimed: '已领取', running: '生成中', succeeded: '已完成', failed: '失败', cancelled: '已取消'
};

export function DeliveryVariantPanel({ asset, selection }: { asset?: Asset; selection: RunSelection }) {
  const client = useQueryClient();
  const { current } = useWorkflowData(asset?.asset_id ?? '', selection);
  const run = current?.run_id ?? '';
  const revision = current?.current_revision_id ?? '';
  const key = variantQueryKey(asset?.asset_id ?? '', run, revision);
  const context = JSON.stringify(key);
  const request = useRef<{ context: string; body: VariantStartRequest } | null>(null);
  const query = useQuery({
    queryKey: key, queryFn: () => deliveryVariantApi.list(run),
    enabled: Boolean(asset?.asset_id && run && revision),
    refetchInterval: 3000
  });
  // Each mutation freezes its run and query key, even when the user switches assets mid-request.
  const start = useMutation({
    mutationFn: (arg: { run: string; key: typeof key; body: VariantStartRequest }) => deliveryVariantApi.start(arg.run, arg.body),
    onSuccess: (_, arg) => {
      if (request.current?.body.request_id === arg.body.request_id) request.current = null;
      void client.invalidateQueries({ queryKey: arg.key });
    },
    onError: (error, arg) => {
      if (error instanceof ApiError && error.status >= 400 && error.status < 500 && error.status !== 408 && request.current?.body.request_id === arg.body.request_id) request.current = null;
      void client.invalidateQueries({ queryKey: arg.key });
    }
  });
  const cancel = useMutation({
    mutationFn: (arg: { id: string; key: typeof key }) => deliveryVariantApi.cancel(arg.id),
    onSuccess: (_, arg) => { void client.invalidateQueries({ queryKey: arg.key }); }
  });
  const resetStart = start.reset, resetCancel = cancel.reset;
  useEffect(() => { request.current = null; resetStart(); resetCancel(); }, [context, resetStart, resetCancel]);
  const bound = query.data?.run_id === run && query.data?.revision_id === revision;
  const variants = (query.data?.variants ?? []).filter(v => v.asset_id === asset?.asset_id && v.run_id === run);
  const pending = variants.find(v => active(v.status) && !v.invalidated && v.revision_id === revision);
  const result = variants.find(v => v.status === 'succeeded' && !v.invalidated && v.is_current && v.revision_id === revision);
  const generate = () => {
    if (!bound || !query.data?.can_start || pending || asset?.locked) return;
    if (request.current?.context !== context) request.current = { context, body: {
      revision_id: revision, expected_version: query.data.expected_version, request_id: crypto.randomUUID(), spec_id: 'ass-basic-v1'
    } };
    start.mutate({ run, key, body: request.current.body });
  };
  return <Card size="small" title="ASS 样式字幕" data-testid="ass-panel">
    <Flex vertical gap={12} style={{ minWidth: 0 }}>
      <Typography.Text type="secondary">白字黑描边 · 底部居中 · 保留原 SRT。生成独立交付包。</Typography.Text>
      {!asset || !run ? <Empty description="选择已有导出版本的资产后生成 ASS" image={Empty.PRESENTED_IMAGE_SIMPLE} /> : <>
        {(query.error || start.error || cancel.error) && <Alert type="error" showIcon message={errorText(query.error ?? start.error ?? cancel.error)} />}
        {bound && query.data?.checks.filter(c => c.status !== 'passed').map(c => <Alert key={c.code} type="info" showIcon message={c.message ?? c.code} />)}
        <Space wrap>
          <Button type="primary" onClick={generate} loading={start.isPending} disabled={!bound || !query.data?.can_start || !!pending || !!asset.locked}>
            {start.isError ? '重试生成 ASS' : '生成 ASS'}
          </Button>
          <Button onClick={() => { void query.refetch(); }} loading={query.isFetching}>刷新</Button>
          {pending && <Button danger loading={cancel.isPending} onClick={() => cancel.mutate({ id: pending.variant_id, key })}>取消 ASS 生成</Button>}
        </Space>
        {pending && <div><Typography.Text>{statusLabel[pending.status]}{pending.message ? `：${pending.message}` : ''}</Typography.Text><Progress percent={Math.round(pending.progress * 100)} /></div>}
        {result && <Space wrap data-testid="ass-downloads">
          <Tag color="success">当前版本 ASS 已完成</Tag>
          <Typography.Link href={variantFileURL(result.variant_id, 'ass')} download>下载 ASS</Typography.Link>
          <Typography.Link href={variantFileURL(result.variant_id, 'srt')} download>下载原 SRT</Typography.Link>
          <Typography.Link href={variantZIPURL(result.variant_id)} download>下载完整交付包</Typography.Link>
        </Space>}
        {variants.length > 0 && <Collapse size="small" items={[{ key: 'history', label: `字幕交付历史 (${variants.length})`, children: <Flex vertical gap={12}>
          {variants.map(v => <Flex key={v.variant_id} vertical gap={4} style={{ overflowWrap: 'anywhere' }}>
            <Space wrap><Tag>{statusLabel[v.status] ?? v.status}</Tag><Typography.Text>{v.revision_id}</Typography.Text>{v.invalidated && <Tag>已失效</Tag>}{v.is_current && <Tag color="blue">当前导出</Tag>}</Space>
            {v.message && <Typography.Text type={v.status === 'failed' ? 'danger' : 'secondary'}>{v.message}</Typography.Text>}
            {v.status === 'succeeded' && !asset.locked && <Space wrap><Typography.Link href={variantFileURL(v.variant_id, 'ass')} download>下载 ASS</Typography.Link><Typography.Link href={variantZIPURL(v.variant_id)} download>交付包</Typography.Link></Space>}
          </Flex>)}
        </Flex> }]} />}
      </>}
    </Flex>
  </Card>;
}
