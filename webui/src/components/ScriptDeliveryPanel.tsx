import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Collapse, Empty, Flex, List, Select, Space, Tag, Typography } from 'antd';
import { api } from '../api';
import { apiURL } from '../runtimeUrl';
import { errorText } from './ui';

/** One current script revision owns the on-page film, manifest and Premiere package. */
export function ScriptDeliveryPanel({ scriptId, assetId, active = true }: { scriptId?: string; assetId?: string; active?: boolean }) {
  const client = useQueryClient();
  const [picked, setPicked] = useState('');
  const scripts = useQuery({ queryKey: ['scripts', assetId], queryFn: () => api.listScripts(assetId!), enabled: active && !scriptId && Boolean(assetId), retry: false });
  const id = scriptId || scripts.data?.find(s => s.script_id === picked)?.script_id || scripts.data?.at(-1)?.script_id;
  const record = useQuery({ queryKey: ['script-workbench', id], queryFn: () => api.scriptWorkbench(id!), enabled: active && Boolean(id), retry: false });
  const w = record.data;
  const ready = w?.script.status === 'finalized' && w.delivery?.version === w.version;
  const manifest = useQuery({ queryKey: ['script-delivery', id, w?.version], queryFn: () => api.scriptDelivery(id!, w!.version), enabled: active && Boolean(id) && ready, retry: false });
  const render = useMutation({
    mutationFn: (snapshot: { id: string; version: number; sha: string }) => api.exportScript(snapshot.id, { expected_version: snapshot.version, visible_set_sha: snapshot.sha }),
    onSuccess: async () => { await Promise.all(['script-workbench', 'script-delivery', 'scripts', 'audit'].map(k => client.invalidateQueries({ queryKey: [k] }))); }
  });
  const info = ready && manifest.data?.version === w?.version ? manifest.data : undefined;
  const query = info ? new URLSearchParams({ version: String(info.version), sha256: info.sha256 }).toString() : '';
  const preview = active && info && !render.isPending ? apiURL(`/api/scripts/${encodeURIComponent(id!)}/preview.mp4?${query}`) : undefined;
  const video = useRef<HTMLVideoElement>(null);
  useEffect(() => { const element = video.current; if (!active) element?.pause(); return () => element?.pause(); }, [active, preview]);
  const failure = render.error || record.error || manifest.error || scripts.error;
  return <Card size="small" title="脚本成片与 Premiere 交付">
    <Flex vertical gap={12}>
      {!scriptId && (scripts.data?.length ?? 0) > 1 && <Select aria-label="选择交付脚本" value={id} disabled={render.isPending} options={scripts.data!.map(s => ({ value: s.script_id, label: s.title || s.script_id }))} onChange={setPicked} />}
      {!id && !scripts.isPending && <Empty description="当前资产还没有脚本，请先在内容编辑中创建并定稿。" />}
      {w && <>
        <Space wrap><Typography.Text>{w.script.title || w.script.script_id} · 修订 {w.version}</Typography.Text><Tag>{w.script.status === 'finalized' ? '已人工定稿' : '待人工定稿'}</Tag></Space>
        {w.script.status !== 'finalized' && <Alert type="warning" showIcon message="脚本或声音已更新，旧交付已失效。请回到脚本检查并人工定稿。" />}
        <Button type="primary" loading={render.isPending} disabled={w.script.status !== 'finalized' || !active} onClick={() => render.mutate({ id: id!, version: w.version, sha: w.script.visible_set_sha })}>{ready ? '重新生成当前修订成片' : '生成成片与 Premiere 包'}</Button>
      </>}
      {preview && info && <>
        <video ref={video} key={preview} src={preview} controls preload="metadata" aria-label={`脚本修订 ${info.version} 成片预览`} style={{ width: '100%', maxHeight: 420 }} />
        <Alert showIcon type={info.basic_quality?.status === 'passed' ? 'success' : 'warning'} message={info.basic_quality?.status === 'passed' ? '基础检验通过' : info.basic_quality?.status === 'failed' ? '基础检验发现异常，仍可交付' : '基础检验未完成'} description={info.basic_quality?.issues.map(i => `${i.kind === 'black' ? '黑屏' : '静音'} ${i.start.toFixed(1)}–${i.end.toFixed(1)} 秒`).join('、')} />
        <Typography.Text type="secondary">{info.width}×{info.height} · {info.duration.toFixed(3)} 秒 · {info.frames} 帧 · {info.tts_enabled ? '包含旁白混音' : '无配音'}。预览和交付包对应同一修订。</Typography.Text>
        <Space wrap>
          <Button type="primary" href={apiURL(`/api/scripts/${encodeURIComponent(id!)}/delivery.zip?${query}`)}>进入 Premiere：下载交付包</Button>
          <Button href={preview} download="final.mp4">下载同版成片</Button>
        </Space>
        <Typography.Paragraph type="secondary">解压后在 Premiere 导入 edit.xml；移动目录时将媒体重链接到 media/。字幕、效果与混音已预渲染，不会向外部服务发送交付包。</Typography.Paragraph>
        <Typography.Text code copyable style={{ wordBreak: 'break-all' }}>{info.sha256}</Typography.Text>
        <Collapse items={[{ key: 'files', label: `交付文件清单（${info.files.length} 项，另含清单本身）`, children: <List size="small" dataSource={info.files} renderItem={file => <List.Item extra={<Typography.Text type="secondary">{(file.size_bytes / 1024).toFixed(1)} KiB</Typography.Text>}><Flex vertical><Typography.Text style={{ wordBreak: 'break-all' }}>{file.path}</Typography.Text><Typography.Text type="secondary" style={{ wordBreak: 'break-all' }}>{file.sha256}</Typography.Text></Flex></List.Item>} /> }]} />
      </>}
      {failure && <Alert showIcon type="error" role="alert" message={errorText(failure)} />}
      {render.isPending && <Typography.Text type="secondary">正在合成所选段落、渲染字幕/效果并校验交付文件，请勿关闭本次操作。</Typography.Text>}
    </Flex>
  </Card>;
}
