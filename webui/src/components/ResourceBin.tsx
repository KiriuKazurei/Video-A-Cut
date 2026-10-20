import { useMemo, useState } from 'react';
import { Badge, Button, Card, Empty, Flex, Input, List, Select, Space, Tag, Typography } from 'antd';
import { FileImageOutlined, FileTextOutlined, SoundOutlined, VideoCameraOutlined, ImportOutlined } from '@ant-design/icons';
import { RESOURCE_LABELS, type ResourceKind } from '../projectContracts';
import { useProjectWorkspace } from '../projectWorkspace';

const icons = { video: <VideoCameraOutlined />, audio: <SoundOutlined />, image: <FileImageOutlined />, subtitle: <FileTextOutlined />, document: <FileTextOutlined />, script: <FileTextOutlined /> };

export function ResourceBin({ onOpenScript }: { onOpenScript: (asset: string, script: string) => void }) {
  const project = useProjectWorkspace();
  const [search, setSearch] = useState('');
  const [kind, setKind] = useState<ResourceKind | ''>('');
  const all = (project.session?.document?.resources ?? []).filter(r => !r.removed || r.referenced);
  const filtered = useMemo(() => all.filter(r => (!kind || r.kind === kind) && r.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [all, kind, search]);
  return <Card variant="borderless" size="small" className="resource-bin" title="资源管理" extra={<Badge count={all.length} showZero />}
    styles={{ body: { padding: '8px 12px 16px' } }}>
    <Flex vertical gap={8}>
      <Input.Search aria-label="搜索当前工程资源" allowClear placeholder="搜索资源名称" value={search} onChange={e => setSearch(e.target.value)} />
      <Select aria-label="资源类型" value={kind} onChange={setKind} options={[{ value: '', label: '所有资源' }, ...Object.entries(RESOURCE_LABELS).map(([value, label]) => ({ value, label }))]} />
      <Button icon={<ImportOutlined />} disabled={project.busy} onClick={() => void project.importFiles().catch(() => {})}>导入资源</Button>
      <List size="small" className="project-resource-list" dataSource={filtered}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={all.length ? '没有匹配的资源' : '导入视频、音频、图片、字幕、文稿或脚本'} /> }}
        renderItem={item => <List.Item key={item.id} role="button" tabIndex={0} aria-pressed={project.selectedId === item.id}
          data-resource-id={item.id} className={project.selectedId === item.id ? 'is-selected' : ''}
          onClick={() => void project.selectResource(item.id)}
          onDoubleClick={() => { if (item.script_id && item.asset_id) onOpenScript(item.asset_id, item.script_id); }}
          onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); void project.selectResource(item.id); } }}>
          <List.Item.Meta avatar={icons[item.kind]} title={<Typography.Text ellipsis={{ tooltip: item.name }}>{item.name}</Typography.Text>}
            description={<Space size={4} wrap><Typography.Text type="secondary">{RESOURCE_LABELS[item.kind]}</Typography.Text>{item.removed ? <Tag color="warning">待重新关联</Tag> : item.missing && <Tag color="warning">文件缺失</Tag>}{item.changed && <Tag color="warning">源文件变化</Tag>}</Space>} />
        </List.Item>} />
    </Flex>
  </Card>;
}
