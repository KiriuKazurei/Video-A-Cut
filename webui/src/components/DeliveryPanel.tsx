import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, deliveryFileURL, deliveryZipURL, ApiError } from '../api';
import type { Asset, DeliveryFile } from '../types';
import { usePreview } from './Monitor';
import { Alert, Button, Card, Empty, Flex, Form, Input, List, Space, Tag, Typography } from 'antd';
import { AudioOutlined, DownloadOutlined, FileImageOutlined, FileOutlined, VideoCameraOutlined } from '@ant-design/icons';
import { ValueSelect } from './ui';

function errorText(value: unknown): string {
  if (value instanceof ApiError) return `${value.message}（${value.code}）`;
  return value instanceof Error ? value.message : '发生未知错误';
}

const sizeText = (bytes: number) => bytes >= 1048576 ? `${(bytes / 1048576).toFixed(1)} MiB` : `${(bytes / 1024).toFixed(1)} KiB`;

/** 资产交付文件查询：导出页与导入页监视器共用同一缓存键，不重复请求（API-44）。 */
export function useDeliveryFiles(asset: Asset | undefined) {
  return useQuery({
    queryKey: ['deliveryFiles', asset?.asset_id],
    queryFn: () => api.listDeliveryFiles(asset!.asset_id),
    enabled: Boolean(asset?.asset_id)
  });
}

/** 把一个可播放交付文件转换为监视器输入；地址只用文件键（API-45）。 */
export function filePreview(assetID: string, file: DeliveryFile) {
  const kind = file.mime.startsWith('video/') ? 'video' : file.mime.startsWith('audio/') ? 'audio' : file.mime.startsWith('image/') ? 'image' : null;
  return kind ? { kind, src: deliveryFileURL(assetID, file.key), title: file.name, meta: `${file.mime} · ${sizeText(file.size)}` } as const : null;
}

/** 导入页：从 delivery_root 内相对目录导入 CLI 交付包（API-42），不是文件上传。 */
export function DeliveryImport({ onImported }: { onImported: (assetId: string) => void }) {
  const queryClient = useQueryClient();
  const [assetID, setAssetID] = useState('');
  const [packageDir, setPackageDir] = useState('');
  const imported = useMutation({
    mutationFn: () => api.importDelivery(assetID.trim(), packageDir.trim()),
    onSuccess(created) {
      setAssetID('');
      setPackageDir('');
      void queryClient.invalidateQueries({ queryKey: ['assets'] });
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
      onImported(created.asset_id);
    }
  });
  return (
    <Card size="small" className="delivery-import" aria-labelledby="delivery-import-title" title={<span id="delivery-import-title">导入交付包</span>}>
      <Typography.Paragraph type="secondary">从控制面配置的交付根目录导入 CLI 产物。这里只输入相对目录，文件由 Go 校验 delivery-manifest.json 后登记为新资产。</Typography.Paragraph>
      <Form layout="vertical" className="delivery-import-form" onFinish={() => { if (assetID.trim() && packageDir.trim()) imported.mutate(); }}>
        <Form.Item label="新资产 ID" htmlFor="delivery-asset-id">
          <Input id="delivery-asset-id" required value={assetID} onChange={(event) => setAssetID(event.target.value)} placeholder="例如：clip_001" />
        </Form.Item>
        <Form.Item label="交付包相对目录" htmlFor="delivery-package-dir">
          <Input id="delivery-package-dir" required value={packageDir} onChange={(event) => setPackageDir(event.target.value)} placeholder="例如：delivery-final" />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={imported.isPending}>{imported.isPending ? '正在校验并导入…' : '导入交付包'}</Button>
      </Form>
      {imported.isError && <Alert style={{ marginTop: 12 }} type="error" showIcon role="alert" message={`导入失败：${errorText(imported.error)}`} />}
    </Card>
  );
}

/** 导出页：资产产物列表、节目监视器预览、单文件下载、资产 ZIP 与重开（API-43~46）。 */
export function DeliveryPanel({ asset }: { asset: Asset | undefined }) {
  const queryClient = useQueryClient();
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const preview = usePreview();
  const files = useDeliveryFiles(asset);

  const reopened = useMutation({
    mutationFn: (id: string) => api.reopenAsset(id),
    onSuccess(updated) {
      queryClient.setQueryData<Asset[]>(['assets'], (previous) =>
        previous?.map((item) => item.asset_id === updated.asset_id ? updated : item));
      void queryClient.invalidateQueries({ queryKey: ['deliveryFiles', updated.asset_id] });
      void queryClient.invalidateQueries({ queryKey: ['audit'] });
    }
  });

  const playable = (files.data ?? []).filter((file) => file.playable);
  const selected = playable.find((file) => file.key === selectedKey) ?? playable[0];
  // 文件列表以资产 ID 区分：切换资产或文件后重新推送，不把上一资产的媒体留在监视器里。
  useEffect(() => {
    preview(asset && selected ? filePreview(asset.asset_id, selected) : null);
  }, [asset?.asset_id, selected?.key]);

  const icon = (mime: string) => mime.startsWith('video/') ? <VideoCameraOutlined /> : mime.startsWith('audio/') ? <AudioOutlined /> : mime.startsWith('image/') ? <FileImageOutlined /> : <FileOutlined />;
  return (
    <Card size="small" className="delivery-panel" aria-labelledby="delivery-title" title={<span id="delivery-title">交付文件</span>}
      extra={files.isSuccess && <Tag>{files.data.length} 个文件</Tag>}>
      {!asset && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择资产后可查看已登记的媒体和交付文件。" />}
      {asset && <Flex vertical gap={12} className="delivery-files">
        {files.isPending && <Typography.Text type="secondary">正在读取产物…</Typography.Text>}
        {files.isError && <Alert type="error" showIcon role="alert" message={`产物读取失败：${errorText(files.error)}`} />}
        {files.isSuccess && files.data.length === 0 && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="此资产尚无已登记产物。完成内容流程或在「导入」页导入交付包后会出现在这里。" />}
        {files.isSuccess && files.data.length > 0 && <>
          <Space wrap className="delivery-actions">
            <Button type="primary" icon={<DownloadOutlined />} href={deliveryZipURL(asset.asset_id)}>下载资产交付 ZIP</Button>
            {asset.status === 'exported' && <Button disabled={reopened.isPending} aria-describedby="reopen-hint" onClick={() => reopened.mutate(asset.asset_id)}>
              {reopened.isPending ? '正在重开…' : '重开为新一轮素材'}</Button>}
          </Space>
          {asset.status === 'exported' && <Typography.Text type="secondary" id="reopen-hint">
            重开会以当前导出包作为新的源素材，把资产状态改回“已入库”，治理设置保持不变；资产上有进行中的任务时服务端会拒绝。</Typography.Text>}
          {reopened.isError && <Alert type="error" showIcon role="alert" message={`重开失败：${errorText(reopened.error)}`} />}
          {playable.length > 0 && <Form.Item label="节目监视器中的文件" layout="vertical" style={{ marginBottom: 0 }}>
            <ValueSelect<string> id="delivery-preview-file" className="select-delivery-preview" value={selected?.key} onChange={setSelectedKey}
              options={playable.map((file) => ({ value: file.key, label: file.name }))} />
          </Form.Item>}
          <List size="small" bordered className="delivery-file-list" dataSource={files.data} renderItem={(file) => <List.Item key={file.key}
            className={selected?.key === file.key ? 'is-selected' : undefined}
            actions={[
              ...(file.playable ? [<Button key="preview" size="small" type={selected?.key === file.key ? 'primary' : 'default'} onClick={() => setSelectedKey(file.key)}>预览</Button>] : []),
              <Typography.Link key="download" href={deliveryFileURL(asset.asset_id, file.key, true)} download>下载</Typography.Link>]}>
            <List.Item.Meta avatar={icon(file.mime)} title={file.name} description={<span style={{ overflowWrap: 'anywhere' }}>{file.key} · {sizeText(file.size)}</span>} />
          </List.Item>} />
          <Typography.Text type="secondary">ZIP 保留原目录结构。若解压到新位置后 Premiere 提示媒体离线，请用“链接媒体”定位解压目录中的文件；包内附有操作说明。</Typography.Text>
        </>}
      </Flex>}
    </Card>
  );
}
