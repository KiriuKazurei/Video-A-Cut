import { useEffect, useRef, useState, type FormEvent, type RefObject } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, deliveryFileURL, deliveryZipURL, ApiError } from '../api';
import type { Asset, DeliveryFile } from '../types';

function errorText(value: unknown): string {
  if (value instanceof ApiError) return `${value.message}（${value.code}）`;
  return value instanceof Error ? value.message : '发生未知错误';
}

export function DeliveryPanel({ asset, onImported }: {
  asset: Asset | undefined;
  onImported: (assetId: string) => void;
}) {
  const queryClient = useQueryClient();
  const [assetID, setAssetID] = useState('');
  const [packageDir, setPackageDir] = useState('');
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [volume, setVolume] = useState(100);
  const audioRef = useRef<HTMLAudioElement | null>(null);

  const files = useQuery({
    queryKey: ['deliveryFiles', asset?.asset_id],
    queryFn: () => api.listDeliveryFiles(asset!.asset_id),
    enabled: Boolean(asset?.asset_id)
  });
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
  useEffect(() => {
    if (audioRef.current) audioRef.current.volume = volume / 100;
  }, [volume, selected?.key]);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (assetID.trim() && packageDir.trim()) imported.mutate();
  }

  return (
    <section className="panel delivery-panel" aria-labelledby="delivery-title">
      <div className="panel-heading"><div><p className="eyebrow">DELIVERY</p>
        <h2 id="delivery-title">预览、试听与交付</h2></div></div>
      <p className="hint">从控制面配置的交付根目录导入 CLI 产物。这里只输入相对目录，文件由 Go 校验后登记。</p>
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
      {!asset && <p className="state">选择资产后可查看已登记的媒体和交付文件。</p>}
      {asset && <div className="delivery-files">
        <h3>{asset.asset_id} 的产物</h3>
        {files.isPending && <p className="state">正在读取产物…</p>}
        {files.isError && <p className="inline-error" role="alert">产物读取失败：{errorText(files.error)}</p>}
        {files.isSuccess && files.data.length === 0 && <p className="state">此资产尚无已登记产物。</p>}
        {files.isSuccess && files.data.length > 0 && <>
          <div className="delivery-actions">
            <a className="button-link" href={deliveryZipURL(asset.asset_id)}>下载交付 ZIP</a>
            {asset.status === 'exported' && <button type="button" className="secondary-button"
              disabled={reopened.isPending} aria-describedby="reopen-hint"
              onClick={() => reopened.mutate(asset.asset_id)}>
              {reopened.isPending ? '正在重开…' : '重开为新一轮素材'}</button>}
          </div>
          {asset.status === 'exported' && <p className="hint" id="reopen-hint">
            重开会以当前导出包作为新的源素材，把资产状态改回“已入库”，治理设置保持不变；资产上有进行中的任务时服务端会拒绝。</p>}
          {reopened.isError && <p className="inline-error" role="alert">重开失败：{errorText(reopened.error)}</p>}
          <p className="hint">ZIP 保留原目录结构。若解压到新位置后 Premiere 提示媒体离线，请用“链接媒体”定位解压目录中的文件；包内附有操作说明。</p>
          {playable.length > 0 && <>
            <label className="field-label" htmlFor="delivery-preview-file">预览或试听文件</label>
            <select id="delivery-preview-file" value={selected?.key ?? ''}
              onChange={(event) => setSelectedKey(event.target.value)}>
              {playable.map((file) => <option key={file.key} value={file.key}>{file.name}</option>)}
            </select>
            {selected && <MediaPreview assetID={asset.asset_id} file={selected}
              audioRef={audioRef} volume={volume} onVolume={setVolume} />}
          </>}
          <ul className="delivery-file-list">{files.data.map((file) => <li key={file.key}>
            <span><strong>{file.name}</strong><small>{file.key} · {(file.size / 1024).toFixed(1)} KiB</small></span>
            <a href={deliveryFileURL(asset.asset_id, file.key, true)} download>下载</a>
          </li>)}</ul>
        </>}
      </div>}
    </section>
  );
}

function MediaPreview({ assetID, file, audioRef, volume, onVolume }: {
  assetID: string;
  file: DeliveryFile;
  audioRef: RefObject<HTMLAudioElement | null>;
  volume: number;
  onVolume: (value: number) => void;
}) {
  const url = deliveryFileURL(assetID, file.key);
  if (file.mime.startsWith('video/')) {
    return <video key={url} className="delivery-media" controls preload="metadata" src={url} aria-label={`预览 ${file.name}`} />;
  }
  if (file.mime.startsWith('audio/')) {
    return <div className="delivery-audio">
      <audio key={url} ref={audioRef} controls preload="metadata" src={url} aria-label={`试听 ${file.name}`} />
      <label htmlFor="delivery-volume">试听音量 {volume}%</label>
      <input id="delivery-volume" type="range" min="0" max="100" value={volume}
        onChange={(event) => onVolume(Number(event.target.value))} />
      <p className="hint">音量仅作用于本次浏览器试听，不改写交付文件。</p>
    </div>;
  }
  if (file.mime.startsWith('image/')) {
    return <img className="delivery-media" src={url} alt={`预览 ${file.name}`} />;
  }
  return null;
}
