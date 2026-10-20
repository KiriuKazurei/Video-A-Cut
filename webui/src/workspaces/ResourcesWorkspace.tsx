import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Alert, Button, Card, Empty, Flex, Input, InputNumber, Select, Slider, Space, Typography } from 'antd';
import { CaretRightOutlined, PauseOutlined, StepBackwardOutlined, StepForwardOutlined } from '@ant-design/icons';
import { useProjectWorkspace } from '../projectWorkspace';
import { projectApi } from '../projectApi';
import { formatSeconds } from '../components/Monitor';
import { errorText } from '../components/ui';

export function ResourcesWorkspace({ onOpenScript }: { onOpenScript: (asset: string, script: string) => void }) {
  const project = useProjectWorkspace();
  const resource = project.resource;
  const projectId = project.session?.document?.project.id ?? '';
  const media = useRef<HTMLVideoElement | HTMLAudioElement | null>(null);
  const [position, setPosition] = useState(0);
  const [duration, setDuration] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [speed, setSpeed] = useState(1);
  const [volume, setVolume] = useState(100);
  const [zoom, setZoom] = useState(100);
  const [notes, setNotes] = useState('');
  const [failure, setFailure] = useState('');
  const [proxy, setProxy] = useState(false);
  const [preparing, setPreparing] = useState(false);
  const proxyRequest = useRef<AbortController | null>(null);
  const timed = resource?.kind === 'video' || resource?.kind === 'audio';
  const textual = resource && ['document', 'subtitle', 'script'].includes(resource.kind);
  const source = resource ? projectApi.contentURL(projectId, resource.id, proxy) : '';
  const text = useQuery({ queryKey: ['resource-text', projectId, resource?.id], enabled: Boolean(textual && !resource?.removed && !resource?.missing), queryFn: async ({ signal }) => {
    const result = await fetch(source, { signal }); if (!result.ok) throw new Error('无法读取资源内容'); return result.text();
  } });
  useEffect(() => {
    media.current?.pause(); proxyRequest.current?.abort(); setPlaying(false); setPosition(0); setDuration(Number(resource?.metadata.duration ?? 0));
    setProxy(false); setPreparing(false); setFailure(''); setNotes(resource?.notes ?? ''); setZoom(100);
    return () => proxyRequest.current?.abort();
  }, [resource?.id, resource?.source.sha256, resource?.removed, resource?.missing]);
  useEffect(() => {
    if (!resource) return;
    const id = resource.id;
    return project.registerEditor(`notes:${id}`, { dirty: notes !== resource.notes, flush: async () => { if (notes !== resource.notes) await project.patchResource(id, { notes }); } });
  }, [resource?.id, resource?.notes, notes, project.registerEditor]);
  useEffect(() => { if (media.current) { media.current.playbackRate = speed; media.current.volume = volume / 100; } }, [speed, volume, source]);
  const seek = (value: number) => { if (media.current) { media.current.currentTime = Math.max(0, Math.min(duration, value)); setPosition(media.current.currentTime); } };
  const toggle = () => { const node = media.current; if (!node) return; if (node.paused) void node.play().catch(e => setFailure(errorText(e))); else node.pause(); };
  const fps = String(resource?.metadata.avg_frame_rate ?? '30/1').split('/').map(Number);
  const frameDuration = 1 / (fps[0] && fps[1] ? fps[0] / fps[1] : 30);
  const mark = (edge: 'in' | 'out') => { if (!resource) return; void project.patchResource(resource.id, edge === 'in' ? { in: position, out: Math.max(position, resource.out) } : { out: position, in: Math.min(position, resource.in) }).catch(e => setFailure(errorText(e))); };
  const prepareProxy = async () => {
    if (!resource || (!timed && resource.kind !== 'image') || preparing) return;
    if (proxy) { setFailure('预览代理无法播放，请检查媒体或重新定位源文件。'); return; }
    const controller = new AbortController(); proxyRequest.current = controller; setPreparing(true); setFailure('');
    try { await projectApi.createPreview(projectId, resource.id, controller.signal); if (!controller.signal.aborted) setProxy(true); }
    catch (e) { if (!controller.signal.aborted) setFailure(errorText(e)); }
    finally { if (!controller.signal.aborted) setPreparing(false); }
  };
  const mediaEvents = {
    onTimeUpdate: () => setPosition(media.current?.currentTime ?? 0),
    onLoadedMetadata: () => { if (media.current && Number.isFinite(media.current.duration)) setDuration(media.current.duration); },
    onPlay: () => setPlaying(true), onPause: () => setPlaying(false), onEnded: () => setPlaying(false),
    onError: () => { void prepareProxy(); }
  };
  if (!resource || (resource.removed && !resource.referenced)) return <div className="resource-workspace-empty"><Empty description="在左侧选择资源，或通过“文件 → 导入”添加素材" /></div>;
  return <div className="resource-workspace">
    <Card className="resource-preview" size="small" title={<Typography.Text ellipsis>{resource.name}</Typography.Text>}>
      {(failure || text.error) && <Alert type="error" showIcon message={failure || errorText(text.error)} />}
      {resource.removed || (resource.missing && resource.kind !== 'script') ? <Empty description="资源引用缺失，工程内容和绑定已保留">{resource.kind === 'script' ? <Button onClick={() => void project.patchResource(resource.id, { removed: false }).catch(e => setFailure(errorText(e)))}>恢复脚本引用</Button> : <Button onClick={() => void project.relink(resource.id).catch(e => setFailure(errorText(e)))}>重新定位</Button>}</Empty> :
        <div className={`resource-preview-canvas kind-${resource.kind}`} tabIndex={timed ? 0 : undefined} aria-label="资源预览" onKeyDown={event => {
          if (!timed || event.target !== event.currentTarget) return;
          if (event.code === 'Space') { event.preventDefault(); toggle(); }
          if (event.key === 'ArrowLeft') { event.preventDefault(); media.current?.pause(); seek(position - frameDuration); }
          if (event.key === 'ArrowRight') { event.preventDefault(); media.current?.pause(); seek(position + frameDuration); }
          if (event.key.toLowerCase() === 'i') mark('in');
          if (event.key.toLowerCase() === 'o') mark('out');
        }}>
          {resource.kind === 'video' && <video key={source} ref={node => { media.current = node; }} src={source} preload="metadata" playsInline aria-label={`预览 ${resource.name}`} {...mediaEvents} />}
          {resource.kind === 'audio' && <Flex vertical gap={24} align="center"><Typography.Text>音频预览</Typography.Text><audio key={source} ref={node => { media.current = node; }} src={source} preload="metadata" aria-label={`试听 ${resource.name}`} {...mediaEvents} /></Flex>}
          {resource.kind === 'image' && <img src={source} alt={resource.name} style={{ transform: `scale(${zoom / 100})` }} onError={() => { void prepareProxy(); }} />}
          {textual && <pre className="resource-text-preview" aria-label="资源文本内容">{text.isPending ? '正在读取…' : text.data ?? ''}</pre>}
          {preparing && <Typography.Text className="resource-proxy-status">正在生成本地预览代理…</Typography.Text>}
        </div>}
    </Card>
    <Card size="small" title="预览操作台" className="resource-controls">
      {timed ? <Flex vertical gap={12}>
        <Slider aria-label="播放位置" min={0} max={duration || 1} step={0.001} value={position} onChange={seek} disabled={resource.missing || preparing} tooltip={{ formatter: value => formatSeconds(value ?? 0) }} />
        <Flex gap={12} align="center" justify="space-between" wrap>
          <Space><Button aria-label="后退一帧" icon={<StepBackwardOutlined />} disabled={resource.kind !== 'video' || resource.missing} onClick={() => { media.current?.pause(); seek(position - frameDuration); }} /><Button aria-label={playing ? '暂停' : '播放'} icon={playing ? <PauseOutlined /> : <CaretRightOutlined />} disabled={resource.missing || preparing} onClick={toggle} /><Button aria-label="前进一帧" icon={<StepForwardOutlined />} disabled={resource.kind !== 'video' || resource.missing} onClick={() => { media.current?.pause(); seek(position + frameDuration); }} /></Space>
          <Typography.Text code>{formatSeconds(position)} / {formatSeconds(duration)}</Typography.Text>
          <Space><Typography.Text>速度</Typography.Text><Select aria-label="播放速度" value={speed} onChange={setSpeed} style={{ width: 88 }} options={[0.25, 0.5, 1, 1.5, 2].map(value => ({ value, label: `${value}×` }))} /><Typography.Text>音量</Typography.Text><Slider aria-label="预览音量" style={{ width: 88 }} min={0} max={100} value={volume} onChange={setVolume} /></Space>
        </Flex>
        <Flex gap={12} align="center" wrap><Button onClick={() => mark('in')}>设置入点 I</Button><InputNumber aria-label="资源入点" min={0} max={resource.out} value={resource.in} step={0.001} onChange={value => { if (value !== null) void project.patchResource(resource.id, { in: value }).catch(e => setFailure(errorText(e))); }} /><Button onClick={() => mark('out')}>设置出点 O</Button><InputNumber aria-label="资源出点" min={resource.in} max={duration} value={resource.out} step={0.001} onChange={value => { if (value !== null) void project.patchResource(resource.id, { out: value }).catch(e => setFailure(errorText(e))); }} /></Flex>
      </Flex> : resource.kind === 'image' ? <Flex align="center" gap={12}><Typography.Text>缩放</Typography.Text><Slider aria-label="图片缩放" min={25} max={200} step={25} value={zoom} onChange={setZoom} style={{ width: 220 }} /><Typography.Text>{zoom}%</Typography.Text><Button onClick={() => setZoom(100)}>适合窗口</Button></Flex> : <Flex align="center" gap={12}><Typography.Text type="secondary">{resource.kind === 'script' ? '脚本副本保存在当前工程中。' : '文本预览'}</Typography.Text>{resource.script_id && resource.asset_id && <Button onClick={() => onOpenScript(resource.asset_id!, resource.script_id!)}>在脚本工作台打开</Button>}</Flex>}
    </Card>
    <Card size="small" title="备注" className="resource-notes"><Input.TextArea aria-label="资源备注" value={notes} rows={4} placeholder="记录这项资源的用途、内容或编辑想法" onChange={event => setNotes(event.target.value)} onBlur={() => { if (notes !== resource.notes) void project.patchResource(resource.id, { notes }).catch(e => setFailure(errorText(e))); }} /></Card>
  </div>;
}
