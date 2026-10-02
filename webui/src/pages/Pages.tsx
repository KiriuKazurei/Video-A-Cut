import { useEffect, useState, type ReactNode } from 'react';
import type { UseMutationResult, UseQueryResult } from '@tanstack/react-query';
import { Alert, Button, Card, Col, Descriptions, Empty, Flex, Form, Input, List, Result, Row, Space, Tag, Typography } from 'antd';
import type { Asset, AuditLog, CreateTask, Task, WorkflowRun } from '../types';
import { parseDependsOn } from '../types';
import { Monitor, PreviewScope, type PreviewSource } from '../components/Monitor';
import { IngestPanel, IngestRegister } from '../components/IngestPanel';
import { DeliveryImport, DeliveryPanel, filePreview, useDeliveryFiles } from '../components/DeliveryPanel';
import { PreparationPanel } from '../components/PreparationPanel';
import { AcceptancePanel, WorkflowPanel, type RunSelection } from '../components/WorkflowPanel';
import { TaskOrchestrator } from '../components/TaskOrchestrator';
import AssetManager from '../components/AssetManager';
import AuditLogViewer from '../components/AuditLogViewer';
import { kindLabel } from '../components/MediaBin';
import { errorText, stamp, ValueSelect } from '../components/ui';
import type { PageDef } from '../navigation';

/** 每页独立的监视器状态；切换资产时清空，避免把上一资产的媒体留作当前结果。 */
function usePagePreview(assetId: string | undefined): [PreviewSource, (p: PreviewSource) => void] {
  const [preview, setPreview] = useState<PreviewSource>(null);
  useEffect(() => { setPreview(null); }, [assetId]);
  return [preview, setPreview];
}

export function PageHeader({ page, children }: { page: PageDef; children?: ReactNode }) {
  return <Flex className="page-header" justify="space-between" align="flex-start" gap={12} wrap>
    <div>
      <Typography.Title level={4} style={{ margin: 0 }}>{page.step} · {page.label}
        <Typography.Text type="secondary" style={{ fontSize: 14, fontWeight: 400, marginInlineStart: 8 }}>{page.english}</Typography.Text></Typography.Title>
      <Typography.Text type="secondary">{page.description}</Typography.Text>
    </div>
    <Space wrap>{children}<Tag className="api-tag" title="本页调用的接口编号，见 docs/frontend-api-inventory.json">{page.apis}</Tag></Space>
  </Flex>;
}

function NoAsset({ text }: { text: string }) {
  return <Card><Result status="info" title="未选择资产" subTitle={text} /></Card>;
}

/* ---------------- 01 导入 ---------------- */

export function ImportPage({ page, asset, onSelect }: { page: PageDef; asset?: Asset; onSelect: (id: string) => void }) {
  const files = useDeliveryFiles(asset);
  const playable = files.data?.filter((file) => file.playable) ?? [];
  const first = playable.find((file) => file.mime.startsWith('video/')) ?? playable[0];
  const preview = asset && first ? filePreview(asset.asset_id, first) : null;
  return <div className="page-body">
    <PageHeader page={page} />
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={15}>
        <Monitor label="源" preview={preview}
          emptyTitle={asset ? '该资产暂无可预览媒体' : '未选择资产'}
          emptyHint={!asset ? '在左侧项目面板选择资产，或在下方登记录像、导入交付包。'
            : asset.input_kind === 'raw_recording' ? '原始录像需先在「粗剪」页导入、切分并生成短片，生成后可在那里预览。'
            : '该资产尚无已登记的可播放产物。'} />
      </Col>
      <Col xs={24} xl={9}>
        <Card size="small" title="素材概览" aria-label="资产概览" style={{ height: '100%' }}>
          {!asset && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未选择资产" />}
          {asset && <Descriptions size="small" column={1} items={[
            { key: 'id', label: '资产 ID', children: <Typography.Text code>{asset.asset_id}</Typography.Text> },
            { key: 'kind', label: '输入类型', children: kindLabel(asset) },
            { key: 'status', label: '状态', children: <Tag>{asset.status}</Tag> },
            { key: 'created', label: '创建时间', children: stamp(asset.created_at) },
            { key: 'files', label: '产物记录', children: `${Object.keys(asset.artifacts ?? {}).length} 项 · 可播放 ${playable.length} 个` }
          ]} />}
          <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginBottom: 0 }}>artifacts 值是服务端记录，不是下载地址；预览只通过受控文件键读取。</Typography.Paragraph>
        </Card>
      </Col>
      <Col xs={24} lg={12}><IngestRegister onSelect={onSelect} /></Col>
      <Col xs={24} lg={12}><DeliveryImport onImported={onSelect} /></Col>
      <Col span={24}><AssetManager onSelect={onSelect} /></Col>
    </Row>
  </div>;
}

/* ---------------- 02 粗剪 ---------------- */

export function AssemblyPage({ page, asset }: { page: PageDef; asset?: Asset }) {
  const [preview, setPreview] = usePagePreview(asset?.asset_id);
  return <PreviewScope onPreview={setPreview}><div className="page-body">
    <PageHeader page={page} />
    <div className="sticky-monitor"><Monitor label="源" preview={preview} compact emptyTitle="源监视器"
      emptyHint="切分完成后点击候选片段或时间线片段查看缩略图；短片生成后在此播放。" /></div>
    <IngestPanel asset={asset} />
  </div></PreviewScope>;
}

/* ---------------- 03 准备 ---------------- */

export function PreparePage({ page, asset, onStarted }: { page: PageDef; asset?: Asset; onStarted: (run: WorkflowRun) => void }) {
  return <div className="page-body">
    <PageHeader page={page} />
    {asset ? <PreparationPanel key={'preparation-' + asset.asset_id} assetId={asset.asset_id} onStarted={onStarted} />
      : <NoAsset text="运行预检绑定具体资产，请先在左侧项目面板选择资产。" />}
  </div>;
}

/* ---------------- 04 编辑 ---------------- */

export function EditPage({ page, asset, selection, onGoExport }: { page: PageDef; asset?: Asset; selection: RunSelection; onGoExport: () => void }) {
  const [preview, setPreview] = usePagePreview(asset?.asset_id);
  useEffect(() => { setPreview(null); }, [selection.runId, setPreview]);
  return <PreviewScope onPreview={setPreview}><div className="page-body">
    <PageHeader page={page} />
    {asset ? <WorkflowPanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} onGoExport={onGoExport}
      monitor={<Monitor label="节目" preview={preview} emptyTitle="节目监视器"
        emptyHint="点击场景证据、时间线片段或解说的“在监视器预览”查看画面。" />} />
      : <NoAsset text="请先在左侧项目面板选择资产。" />}
  </div></PreviewScope>;
}

/* ---------------- 05 导出 ---------------- */

export function ExportPage({ page, asset, selection }: { page: PageDef; asset?: Asset; selection: RunSelection }) {
  const [preview, setPreview] = usePagePreview(asset?.asset_id);
  return <PreviewScope onPreview={setPreview}><div className="page-body">
    <PageHeader page={page} />
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={14}>
        <Flex vertical gap={16}>
          <Monitor label="节目" preview={preview} emptyTitle={asset ? '暂无可播放的交付文件' : '未选择资产'}
            emptyHint="资产登记交付产物后，在右侧文件列表选择视频或音频进行预览、试听。" />
          {asset && <AcceptancePanel key={asset.asset_id} assetId={asset.asset_id} selection={selection} />}
        </Flex>
      </Col>
      <Col xs={24} xl={10}><DeliveryPanel asset={asset} /></Col>
    </Row>
  </div></PreviewScope>;
}

/* ---------------- 06 监控 ---------------- */

const taskTypes = ['recognize', 'sort', 'narrate', 'tts', 'subtitle', 'mix', 'export', 'preview'];

export function MonitorPage({ page, asset, audit, task, watchedTaskId, createTask }: {
  page: PageDef;
  asset?: Asset;
  audit: UseQueryResult<AuditLog[]>;
  task: UseQueryResult<Task>;
  watchedTaskId: string | null;
  createTask: UseMutationResult<Task, Error, CreateTask>;
}) {
  const [taskType, setTaskType] = useState('recognize');
  const [agentRole, setAgentRole] = useState('recognizer');
  const [dependsOn, setDependsOn] = useState('');
  function submitTask() {
    if (!asset || !agentRole.trim()) return;
    createTask.mutate({
      task_id: `task_${crypto.randomUUID()}`,
      asset_id: asset.asset_id,
      type: taskType,
      agent_role: agentRole.trim(),
      depends_on: parseDependsOn(dependsOn)
    }, { onSuccess: () => setDependsOn('') });
  }
  return <div className="page-body">
    <PageHeader page={page} />
    <Row gutter={[16, 16]}>
      <Col xs={24} xl={14}>
        <Flex vertical gap={16}>
          <Card size="small" title={<span id="task-title">任务派发</span>} aria-labelledby="task-title"
            extra={<Tag>{asset ? asset.asset_id : '未选择资产'}</Tag>}>
            <Form layout="vertical" onFinish={submitTask}>
              <Row gutter={12}>
                <Col xs={24} md={6}><Form.Item label="任务类型">
                  <ValueSelect<string> id="task-type" aria-label="任务类型" value={taskType} onChange={setTaskType}
                    options={taskTypes.map((value) => ({ value, label: value }))} />
                </Form.Item></Col>
                <Col xs={24} md={6}><Form.Item label="Agent 角色">
                  <Input id="agent-role" aria-label="Agent 角色" value={agentRole} required onChange={(event) => setAgentRole(event.target.value)} />
                </Form.Item></Col>
                <Col xs={24} md={12}><Form.Item label="前置任务（可选）">
                  <Input id="depends-on" aria-label="前置任务" value={dependsOn} placeholder="task_id，多个用逗号分隔"
                    onChange={(event) => setDependsOn(event.target.value)} />
                </Form.Item></Col>
              </Row>
              <Button type="primary" htmlType="submit" disabled={!asset} loading={createTask.isPending}>创建任务</Button>
            </Form>
            <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>手动创建单任务，不是启动完整内容流程的替代入口。任务绑定当前选中的资产；前置任务须属于同一资产。服务端没有任务列表、单任务取消/重试/暂停接口，这里按 ID 跟踪。</Typography.Paragraph>
            {createTask.isError && <Alert style={{ marginTop: 12 }} type="error" showIcon message={`任务创建失败：${errorText(createTask.error)}`} />}
          </Card>
          <TaskOrchestrator taskId={watchedTaskId} task={task.data}
            isPending={task.isPending} isError={task.isError} error={task.error} />
        </Flex>
      </Col>
      <Col xs={24} xl={10}>
        <Card size="small" title={<span id="audit-title">最近审计</span>} aria-labelledby="audit-title" extra={<Tag>最近 100 条</Tag>}>
          {audit.isError && <Alert type="error" showIcon message={`审计读取失败：${errorText(audit.error)}`}
            action={<Button size="small" onClick={() => void audit.refetch()}>重试</Button>} />}
          <List className="audit-list" size="small" loading={audit.isPending} dataSource={audit.data ?? []}
            locale={{ emptyText: '暂无审计记录。' }} pagination={{ pageSize: 8, size: 'small', showSizeChanger: false }}
            renderItem={(entry) => <List.Item key={entry.id}>
              <List.Item.Meta title={<Typography.Text code>{entry.action}</Typography.Text>}
                description={<Typography.Text type="secondary" style={{ overflowWrap: 'anywhere' }}>{entry.target} · {entry.actor}</Typography.Text>} />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}><time dateTime={entry.created_at}>{stamp(entry.created_at)}</time></Typography.Text>
            </List.Item>} />
        </Card>
      </Col>
      <Col span={24}><AuditLogViewer /></Col>
    </Row>
  </div>;
}
