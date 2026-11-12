import { Alert, Button, Card, Empty, Flex, List, Space, Typography } from 'antd';
import { AppstoreOutlined, FileAddOutlined, FolderOpenOutlined } from '@ant-design/icons';
import { useProjectWorkspace } from '../projectWorkspace';

export function ProjectHome({ onOpenTemplates }: { onOpenTemplates: () => void }) {
  const project = useProjectWorkspace();
  const target = project.openingPath ?? project.pendingPath;
  const name = project.recent?.find(item => item.path === target)?.name ?? target?.split(/[\\/]/).at(-1);
  return <main id="main" className="project-home">
    <div className="project-home-heading"><Typography.Title level={2}>Video Auto Cut</Typography.Title><Typography.Paragraph type="secondary">打开一个工程，开始创作。</Typography.Paragraph></div>
    {target && <Alert type="info" showIcon role="status"
      message={project.openingPath ? `正在打开 ${name}` : `后台就绪后将打开 ${name}`}
      action={!project.openingPath && <Button size="small" onClick={project.cancelOpen}>取消</Button>} />}
    {project.error && <Alert type="error" showIcon message={project.error} />}
    <Flex gap={12} wrap className="project-home-actions"><Button type="primary" size="large" icon={<FileAddOutlined />} disabled={!project.ready || project.busy} onClick={() => void project.create().catch(() => {})}>新建项目</Button><Button size="large" icon={<FolderOpenOutlined />} disabled={project.busy} onClick={() => void project.open().catch(() => {})}>打开项目</Button><Button size="large" icon={<AppstoreOutlined aria-hidden="true" />} disabled={!project.ready || project.busy} onClick={onOpenTemplates}>模板库</Button></Flex>
    {Boolean(project.session?.recoveries.length) && <Card title="恢复未保存的工程" size="small"><List dataSource={project.session?.recoveries} renderItem={item => <List.Item actions={[<Button key="restore" disabled={project.busy} onClick={() => void project.recover(item).catch(() => {})}>恢复</Button>]}><List.Item.Meta title={item.name} description={item.path} /></List.Item>} /></Card>}
    <Card title="最近的项目" size="small"><List dataSource={project.recent ?? []}
      rowKey="path"
      loading={project.recent === undefined && !project.recentError && !project.error}
      locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={project.recentError ? '暂时无法读取最近项目，可手动选择工程文件' : '还没有最近打开的工程'} /> }}
      renderItem={item => <List.Item actions={[<Button key="open" aria-pressed={target === item.path} loading={project.openingPath === item.path} disabled={project.busy} onClick={() => void project.open(item.path).catch(() => {})}>打开</Button>]}><List.Item.Meta title={<Space><FolderOpenOutlined /><Typography.Text>{item.name}</Typography.Text></Space>} description={<Typography.Text type="secondary" style={{ overflowWrap: 'anywhere' }}>{item.path}</Typography.Text>} /></List.Item>} /></Card>
  </main>;
}
