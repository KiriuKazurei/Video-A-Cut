import { Alert, Button, Card, Empty, Flex, List, Space, Typography } from 'antd';
import { FileAddOutlined, FolderOpenOutlined } from '@ant-design/icons';
import { useProjectWorkspace } from '../projectWorkspace';

export function ProjectHome() {
  const project = useProjectWorkspace();
  return <main id="main" className="project-home">
    <div className="project-home-heading"><Typography.Title level={2}>Video Auto Cut</Typography.Title><Typography.Paragraph type="secondary">打开一个工程，开始创作。</Typography.Paragraph></div>
    {project.error && <Alert type="error" showIcon message={project.error} />}
    <Flex gap={12} wrap className="project-home-actions"><Button type="primary" size="large" icon={<FileAddOutlined />} disabled={project.busy} onClick={() => void project.create().catch(() => {})}>新建项目</Button><Button size="large" icon={<FolderOpenOutlined />} disabled={project.busy} onClick={() => void project.open().catch(() => {})}>打开项目</Button></Flex>
    {Boolean(project.session?.recoveries.length) && <Card title="恢复未保存的工程" size="small"><List dataSource={project.session?.recoveries} renderItem={item => <List.Item actions={[<Button key="restore" disabled={project.busy} onClick={() => void project.recover(item).catch(() => {})}>恢复</Button>]}><List.Item.Meta title={item.name} description={item.path} /></List.Item>} /></Card>}
    <Card title="最近的项目" size="small"><List dataSource={project.session?.recent ?? []} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有最近打开的工程" /> }}
      renderItem={item => <List.Item actions={[<Button key="open" disabled={project.busy} onClick={() => void project.open(item.path).catch(() => {})}>打开</Button>]}><List.Item.Meta title={<Space><FolderOpenOutlined /><Typography.Text>{item.name}</Typography.Text></Space>} description={<Typography.Text type="secondary" style={{ overflowWrap: 'anywhere' }}>{item.path}</Typography.Text>} /></List.Item>} /></Card>
  </main>;
}
