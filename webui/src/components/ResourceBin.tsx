import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Badge, Button, Card, Empty, Flex, Input, List, Select, Space, Tag, Tooltip, Typography } from 'antd';
import { CheckOutlined, EyeOutlined, EyeInvisibleOutlined, FileImageOutlined, FileTextOutlined, SoundOutlined, VideoCameraOutlined, ImportOutlined } from '@ant-design/icons';
import { RESOURCE_LABELS, type ResourceKind } from '../projectContracts';
import { useProjectWorkspace } from '../projectWorkspace';
import { timelineApi } from '../timelineApi';
import { api } from '../api';
import type { Asset } from '../types';

const icons = { video: <VideoCameraOutlined />, audio: <SoundOutlined />, image: <FileImageOutlined />, subtitle: <FileTextOutlined />, document: <FileTextOutlined />, script: <FileTextOutlined /> };

export function ResourceBin({ onOpenScript }: { onOpenScript: (asset: string, script: string) => void }) {
  const project = useProjectWorkspace();
  const [search, setSearch] = useState('');
  const [kind, setKind] = useState<ResourceKind | ''>('');
  const assets = useQuery({ queryKey: ['assets'], queryFn: api.listAssets });
  const byAsset = useMemo(() => {
    const map = new Map<string, Asset>();
    for (const item of assets.data ?? []) map.set(item.asset_id, item);
    return map;
  }, [assets.data]);
  const all = (project.session?.document?.resources ?? []).filter(r => !r.removed || r.referenced);
  const filtered = useMemo(() => all.filter(r => (!kind || r.kind === kind) && r.name.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [all, kind, search]);
  const projectId = project.session?.document?.project.id ?? '';
  return <Card variant="borderless" size="small" className="resource-bin" title="资源管理" extra={<Badge count={all.length} showZero />}
    styles={{ body: { padding: '8px 12px 16px' } }}>
    <Flex vertical gap={8}>
      <Input.Search aria-label="搜索当前工程资源" allowClear placeholder="搜索资源名称" value={search} onChange={e => setSearch(e.target.value)} />
      <Select aria-label="资源类型" value={kind} onChange={setKind} options={[{ value: '', label: '所有资源' }, ...Object.entries(RESOURCE_LABELS).map(([value, label]) => ({ value, label }))]} />
      <Button icon={<ImportOutlined />} disabled={project.busy} onClick={() => void project.importFiles().catch(() => {})}>导入资源</Button>
      <List size="small" className="project-resource-list" dataSource={filtered}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={all.length ? '没有匹配的资源' : '导入视频、音频、图片、字幕、文稿或脚本'} /> }}
        renderItem={item => {
          const linked = item.asset_id ? byAsset.get(item.asset_id) : undefined;
          const inLibrary = Boolean(item.asset_id) && !item.removed;
          const agentVisible = Boolean(linked?.agent_visible);
          const thumb = projectId && (item.kind === 'video' || item.kind === 'image') && !item.missing && !item.removed
            ? timelineApi.thumbnailURL(projectId, item.id, item.in ?? 0)
            : undefined;
          return <List.Item key={item.id} role="button" tabIndex={0} aria-pressed={project.selectedId === item.id}
            data-resource-id={item.id} className={project.selectedId === item.id ? 'is-selected' : ''}
            draggable={!item.missing&&!item.removed&&['video','audio','image','subtitle'].includes(item.kind)}
            onDragStart={event=>{event.dataTransfer.effectAllowed='copy';event.dataTransfer.setData('application/x-vac-resource',JSON.stringify({project:project.session?.document?.project.id,session:project.session?.id,resource:item.id}));void project.selectResource(item.id);}}
            onClick={() => void project.selectResource(item.id)}
            onDoubleClick={() => { if (item.script_id && item.asset_id) onOpenScript(item.asset_id, item.script_id); }}
            onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); void project.selectResource(item.id); } }}>
            <List.Item.Meta
              avatar={thumb
                ? <img className="resource-bin-thumb" src={thumb} alt="" loading="lazy" draggable={false} />
                : <span className="resource-bin-thumb is-icon" aria-hidden="true">{icons[item.kind]}</span>}
              title={<Flex align="center" gap={4} wrap={false}>
                <Typography.Text ellipsis={{ tooltip: item.name }} style={{ flex: 1, minWidth: 0 }}>{item.name}</Typography.Text>
                <span className="resource-bin-badges">
                  <Tooltip title={inLibrary ? '已入库' : '未入库 / 待关联'}>
                    <span className={`resource-bin-badge${inLibrary ? ' is-on' : ''}`} aria-label={inLibrary ? '已入库' : '未入库'}><CheckOutlined /></span>
                  </Tooltip>
                  <Tooltip title={agentVisible ? 'Agent 可见' : 'Agent 不可见'}>
                    <span className={`resource-bin-badge${agentVisible ? ' is-on' : ''}`} aria-label={agentVisible ? 'Agent 可见' : 'Agent 不可见'}>
                      {agentVisible ? <EyeOutlined /> : <EyeInvisibleOutlined />}
                    </span>
                  </Tooltip>
                </span>
              </Flex>}
              description={<Space size={4} wrap><Typography.Text type="secondary">{RESOURCE_LABELS[item.kind]}</Typography.Text>{item.removed ? <Tag color="warning">待重新关联</Tag> : item.missing && <Tag color="warning">文件缺失</Tag>}{item.changed && <Tag color="warning">源文件变化</Tag>}</Space>} />
          </List.Item>;
        }} />
    </Flex>
  </Card>;
}
