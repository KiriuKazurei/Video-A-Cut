import { Button, Card, Descriptions, Empty, Flex, Tag, Typography } from 'antd';
import { RESOURCE_LABELS } from '../projectContracts';
import { useProjectWorkspace } from '../projectWorkspace';

export function ResourceInspector() {
  const project = useProjectWorkspace();
  const resource = project.resource;
  if (!resource) return <Card size="small" variant="borderless" title="元信息"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择资源查看元信息" /></Card>;
  const metadata = resource.metadata;
  const streams = Array.isArray(metadata.streams) ? metadata.streams as Record<string, unknown>[] : [];
  const text = (value: unknown) => value === undefined || value === null || value === '' ? '—' : String(value);
  return <Card size="small" variant="borderless" title="元信息" styles={{ body: { padding: '8px 16px 20px' } }}>
    <Flex vertical gap={12}>
      <Typography.Text strong style={{ overflowWrap: 'anywhere' }}>{resource.name}</Typography.Text>
      {resource.missing && <Tag color="warning">文件缺失</Tag>}
      {resource.changed && <Tag color="warning">源文件已变化</Tag>}
      <Descriptions column={1} size="small" items={[
        { key: 'type', label: '类型', children: RESOURCE_LABELS[resource.kind] },
        { key: 'path', label: '位置', children: <Typography.Text style={{ overflowWrap: 'anywhere' }}>{resource.draw_source?'工程内绘制源 · '+(resource.draw_source.mode==='follow'?'跟随源更新':'冻结快照'):resource.source.original_path || '工程内脚本'}</Typography.Text> },
        { key: 'size', label: '大小', children: `${(resource.source.size_bytes / 1048576).toFixed(2)} MiB` },
        { key: 'format', label: '格式', children: text(metadata.extension) },
        ...(metadata.duration ? [{ key: 'duration', label: '时长', children: `${Number(metadata.duration).toFixed(3)} 秒` }] : []),
        ...(metadata.width ? [{ key: 'resolution', label: '分辨率', children: `${metadata.width} × ${metadata.height}` }] : []),
        ...(metadata.avg_frame_rate ? [{ key: 'fps', label: '帧率', children: text(metadata.avg_frame_rate) }] : []),
        ...(metadata.codec_name ? [{ key: 'codec', label: '编码', children: text(metadata.codec_name) }] : []),
        ...(metadata.encoding ? [{ key: 'encoding', label: '文本编码', children: text(metadata.encoding) }] : []),
        ...(metadata.characters ? [{ key: 'characters', label: '字符数', children: text(metadata.characters) }] : []),
        ...streams.filter(stream => stream.codec_type === 'audio').map((stream, index) => ({ key: `audio-${index}`, label: `音轨 ${index + 1}`, children: `${text(stream.codec_name)} · ${text(stream.sample_rate)} Hz · ${text(stream.channels)} 声道` }))
      ]} />
      {(resource.missing || resource.changed) && resource.kind !== 'script' && <Button onClick={() => void project.relink(resource.id).catch(() => {})}>重新定位文件</Button>}
      {resource.removed && resource.kind === 'script' && <Button onClick={() => void project.patchResource(resource.id, { removed: false }).catch(() => {})}>恢复脚本引用</Button>}
    </Flex>
  </Card>;
}
