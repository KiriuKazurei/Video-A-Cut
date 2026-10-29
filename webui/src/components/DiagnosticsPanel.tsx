import { Alert, Button, Card, List, Space, Tag, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { diagnosticSummary, systemApi, type DiagnosticStatus } from '../systemContracts';

const states: Record<DiagnosticStatus, { label: string; color: string }> = {
  passed: { label: '检查通过', color: 'success' }, warning: { label: '需检查', color: 'warning' },
  failed: { label: '不可用', color: 'error' }, not_checked: { label: '未验证', color: 'default' },
};
export function DiagnosticsPanel() {
  const snapshot = useQuery({ queryKey: ['system-diagnostics'], queryFn: ({ signal }) => systemApi.diagnostics(signal), enabled: false, retry: false });
  const report = snapshot.data;
  const summary = report && diagnosticSummary(report);
  const download = () => {
    if (!report) return;
    const url = URL.createObjectURL(new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' }));
    const anchor = document.createElement('a'); anchor.href = url; anchor.download = 'vac-diagnostics.json';
    document.body.append(anchor); anchor.click(); anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return <Card title="统一诊断" extra={<Space wrap>
    <Button onClick={() => void snapshot.refetch()} loading={snapshot.isFetching}>运行诊断</Button>
    <Button onClick={download} disabled={!report || snapshot.isFetching || snapshot.isError}>下载脱敏报告</Button>
  </Space>}>
    <Typography.Paragraph type="secondary">按需检查本机工具、Worker 和模型配置，不调用外部模型。报告不含密钥、路径、素材名称或原始日志。</Typography.Paragraph>
    <div aria-live="polite">
      {snapshot.isFetching && <Alert type="info" showIcon message="正在检查本机环境…" />}
      {snapshot.isError && <Alert type="error" showIcon message="诊断未完成，请确认后台可连接后重试。" />}
      {!report && !snapshot.isFetching && !snapshot.isError && <Typography.Paragraph>点击“运行诊断”查看当前环境。</Typography.Paragraph>}
      {report && summary && <Typography.Paragraph>
        {report.platform} / {report.architecture} · {new Date(report.generated_at).toLocaleString()} · 不可用 {summary.failed} 项，需检查 {summary.warning} 项，未验证 {summary.notChecked} 项
        {snapshot.isError && '（以下为上次成功快照）'}
      </Typography.Paragraph>}
    </div>
    {report && <List size="small" dataSource={report.checks} renderItem={check => <List.Item>
      <List.Item.Meta title={<Space wrap><Typography.Text strong>{check.label}</Typography.Text><Tag color={states[check.status].color}>{states[check.status].label}</Tag>{check.version && <Typography.Text type="secondary">{check.version}</Typography.Text>}</Space>}
        description={<><div>{check.message}</div>{check.next_step && <div>下一步：{check.next_step}</div>}</>} />
    </List.Item>} />}
  </Card>;
}
