import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Flex, Form, Input, List, Space, Tag, Typography } from 'antd';
import { skillApi } from '../skillApi';
import { useSkillWorkspace } from '../skillWorkspace';
import { ProtectedError } from './HumanSessionControl';
import { errorText } from './ui';
import { protectedQueryRetry, runDetailKey } from '../skillView';

/** Human-only operation log. Records cannot be edited or deleted. */
export function AgentOperationPanel() {
  const workspace = useSkillWorkspace();
  const client = useQueryClient();
  const [note, setNote] = useState('');
  const [notice, setNotice] = useState('');
  const runId = workspace.runId;
  const operations = useQuery({
    queryKey: runId && workspace.projectId ? runDetailKey(workspace.projectId, runId, 'operations') : ['vu-run', 'operations-idle'],
    queryFn: () => skillApi.agentOperations(runId, 0, 100),
    enabled: Boolean(runId),
    retry: protectedQueryRetry
  });
  const verify = useQuery({
    queryKey: ['audit', 'agent-verify'],
    queryFn: () => skillApi.verifyAgentOperations(),
    retry: protectedQueryRetry
  });
  const append = useMutation({
    mutationFn: () => skillApi.appendOperationNote(runId, note.trim()),
    onSuccess: async () => {
      setNote('');
      setNotice('注释已追加，原记录保持不变。');
      workspace.markUnlocked();
      await client.invalidateQueries({ queryKey: ['vu-run'] });
      await client.invalidateQueries({ queryKey: ['audit', 'agent-verify'] });
    }
  });
  const error = operations.error || verify.error || append.error;

  return <Flex vertical gap={12} className="agent-operation-panel">
    <Typography.Title level={5} style={{ margin: 0 }}>人工操作日志</Typography.Title>
    <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
      日志只供人工查看、核验摘要链和追加注释。不能修改或删除原记录，也不会交给 Agent。
      {runId ? ` 当前运行 ${runId}。` : ' 请先在编辑页选择一次 Skill 运行，再按该运行筛选。'}
    </Typography.Paragraph>
    {error ? <ProtectedError error={error} onRetry={() => { void operations.refetch(); void verify.refetch(); }} /> : null}
    {notice && <Alert type="info" showIcon message={notice} />}
    <Space wrap>
      <Tag>{verify.isPending ? '核验中' : verify.data ? `摘要链已核验至 ${verify.data.verified_through}` : '尚未核验'}</Tag>
      <Button onClick={() => void verify.refetch()} loading={verify.isFetching}>重新核验摘要链</Button>
      <Button onClick={() => void operations.refetch()} disabled={!runId} loading={operations.isFetching}>重新读取日志</Button>
    </Space>
    <List size="small" loading={operations.isFetching} dataSource={operations.data ?? []} locale={{ emptyText: runId ? '这个运行还没有操作记录' : '未选择运行' }}
      renderItem={(item) => <List.Item>
        <Flex vertical gap={4} style={{ width: '100%' }}>
          <Space wrap>
            <Tag>#{item.sequence}</Tag>
            <Typography.Text strong>{item.body.action}</Typography.Text>
            <Typography.Text type="secondary">{item.body.actor} · {item.body.at}</Typography.Text>
          </Space>
          <Typography.Text type="secondary">前项 {item.previous_sha256 || '（链首）'} · 本项 {item.sha256}</Typography.Text>
        </Flex>
      </List.Item>} />
    <Form layout="vertical" onFinish={() => { if (runId && note.trim()) append.mutate(); }}>
      <Form.Item label="追加注释" extra="只新增一条人工注释，不改已有记录。">
        <Input.TextArea aria-label="人工操作注释" rows={3} value={note} onChange={(event) => setNote(event.target.value)} />
      </Form.Item>
      <Button type="primary" htmlType="submit" disabled={!runId || note.trim().length === 0} loading={append.isPending}>追加注释</Button>
      {append.isError ? <Typography.Text type="danger">{errorText(append.error)}</Typography.Text> : null}
    </Form>
  </Flex>;
}
