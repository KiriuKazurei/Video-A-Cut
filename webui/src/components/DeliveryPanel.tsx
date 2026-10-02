import { useEffect, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, deliveryFileURL, deliveryZipURL, ApiError } from '../api';
import type { Asset, DeliveryFile } from '../types';
import { usePreview } from './Monitor';
import { Icon } from './Icon';

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
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (assetID.trim() && packageDir.trim()) imported.mutate();
  }
  return (
    <section className="panel delivery-import" aria-labelledby="delivery-import-title">
      <div className="panel-heading"><div><p className="eyebrow">PACKAGE</p><h2 id="delivery-import-title">导入交付包</h2></div></div>
      <p className="hint">从控制面配置的交付根目录导入 CLI 产物。这里只输入相对目录，文件由 Go 校验 delivery-manifest.json 后登记为新资产。</p>
      <form className="delivery-import-form" onSubmit={submit}>
        <div className="delivery-import-field">
          <label className="field-label" htmlFor="delivery-asset-id">新资产 ID</label>
          <input id="delivery-asset-id" required value={assetID}
            onChange={(event) => setAssetID(event.target.value)} placeholder="例如：clip_001" />
        </div>
        <div className="delivery-import-field">
          <label className="field-label" htmlFor="delivery-package-dir">交付包相对目录</label>
          <input id="delivery-package-dir" required value={packageDir}
            onChange={(event) => setPackageDir(event.target.value)} placeholder="例如：delivery-final" />
        </div>
        <button type="submit" disabled={imported.isPending}>
          {imported.isPending ? '正在校验并导入…' : '导入交付包'}
        </button>
      </form>
      {imported.isError && <p className="inline-error" role="alert">导入失败：{errorText(imported.error)}</p>}
    </section>
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

  return (
    <section className="panel delivery-panel" aria-labelledby="delivery-title">
      <div className="panel-heading"><div><p className="eyebrow">DELIVERY</p>
        <h2 id="delivery-title">交付文件</h2></div>
        {files.isSuccess && <span className="count">{files.data.length} 个文件</span>}</div>
      {!asset && <p className="state">选择资产后可查看已登记的媒体和交付文件。</p>}
      {asset && <div className="delivery-files">
        {files.isPending && <p className="state">正在读取产物…</p>}
        {files.isError && <p className="inline-error" role="alert">产物读取失败：{errorText(files.error)}</p>}
        {files.isSuccess && files.data.length === 0 && <p className="state">此资产尚无已登记产物。完成内容流程或在「导入」页导入交付包后会出现在这里。</p>}
        {files.isSuccess && files.data.length > 0 && <>
          <div className="delivery-actions">
            <a className="button-link" href={deliveryZipURL(asset.asset_id)}><Icon name="export" /> 下载资产交付 ZIP</a>
            {asset.status === 'exported' && <button type="button" className="secondary-button"
              disabled={reopened.isPending} aria-describedby="reopen-hint"
              onClick={() => reopened.mutate(asset.asset_id)}>
              {reopened.isPending ? '正在重开…' : '重开为新一轮素材'}</button>}
          </div>
          {asset.status === 'exported' && <p className="hint" id="reopen-hint">
            重开会以当前导出包作为新的源素材，把资产状态改回“已入库”，治理设置保持不变；资产上有进行中的任务时服务端会拒绝。</p>}
          {reopened.isError && <p className="inline-error" role="alert">重开失败：{errorText(reopened.error)}</p>}
          {playable.length > 0 && <label className="field-label" htmlFor="delivery-preview-file">节目监视器中的文件
            <select id="delivery-preview-file" value={selected?.key ?? ''}
              onChange={(event) => setSelectedKey(event.target.value)}>
              {playable.map((file) => <option key={file.key} value={file.key}>{file.name}</option>)}
            </select></label>}
          <ul className="delivery-file-list">{files.data.map((file) => <li key={file.key} className={selected?.key === file.key ? 'is-selected' : undefined}>
            <Icon name={file.mime.startsWith('video/') ? 'film' : file.mime.startsWith('audio/') ? 'audio' : file.mime.startsWith('image/') ? 'image' : 'export'} />
            <span><strong>{file.name}</strong><small>{file.key} · {sizeText(file.size)}</small></span>
            {file.playable && <button type="button" className="secondary-button compact" onClick={() => setSelectedKey(file.key)}>预览</button>}
            <a href={deliveryFileURL(asset.asset_id, file.key, true)} download>下载</a>
          </li>)}</ul>
          <p className="hint">ZIP 保留原目录结构。若解压到新位置后 Premiere 提示媒体离线，请用“链接媒体”定位解压目录中的文件；包内附有操作说明。</p>
        </>}
      </div>}
    </section>
  );
}
