import { Alert, Badge, Button, Flex, List, Space, Tag, Typography } from 'antd';
import { useExternalAgentStatus } from '../useExternalAgentStatus';
import { EXTERNAL_AGENT_LABELS, externalAgentSummary } from '../externalAgentStatus';

export function ExternalAgentStatusPanel({ active = true }: { active?: boolean }) {
  const status = useExternalAgentStatus(active);
  const summary = externalAgentSummary(status.data, status.isError);
  return <section aria-label="外部 Agent 状态" className="external-agent-status">
    <Flex vertical gap={8}>
      <Space wrap><Badge status={summary.tone} text={summary.label} /><Button size="small" loading={status.isFetching} onClick={() => void status.refetch()}>刷新 Agent 状态</Button></Space>
      {status.isError ? <Alert role="alert" showIcon type="warning" message="无法读取最新 Agent 状态，暂不能确认连接；请检查后台后重试。" /> : status.data && <List size="small" dataSource={status.data.agents} locale={{ emptyText: '请在客户端连接 VAC MCP，再读取当前工程。' }} renderItem={agent => <List.Item>
        <Flex vertical gap={4}>
          <Space wrap><Typography.Text strong>{agent.name}</Typography.Text><Tag>{EXTERNAL_AGENT_LABELS[agent.state] ?? '状态待确认'}</Tag><Typography.Text type="secondary">{agent.version}</Typography.Text></Space>
          <Typography.Text type="secondary">最近活动 {new Date(agent.last_seen_at).toLocaleString()}{agent.last_tool ? ` · 最近工具 ${agent.last_tool}` : ''}{agent.active_calls ? ` · ${agent.active_calls} 个调用处理中` : ''}</Typography.Text>
        </Flex>
      </List.Item>} />}
      <Typography.Text type="secondary">每 3 分钟自动刷新，也可手动刷新。状态来自 VAC 实际收到的 MCP 请求，客户端名称为自报信息；快照中超过 90 秒无活动标为“连接待确认”，静默不代表 Agent 已退出。</Typography.Text>
    </Flex>
  </section>;
}
