import { useState } from 'react';
import { Alert, Button, Card, Collapse, Flex, Input, Modal, Space, Typography } from 'antd';
import { desktopCommand, isDesktop } from '../desktop';
import type { RouteState } from '../navigation';
import { ExternalAgentStatusPanel } from './ExternalAgentStatusPanel';

export function ScriptAgentHandoff({ task, prepareTask, navigate, active = true }: { task: string; prepareTask?:()=>Promise<string>; navigate?: (route: Partial<RouteState>) => void; active?: boolean }) {
  const [open, setOpen] = useState(false), [feedback, setFeedback] = useState(''), [busy, setBusy] = useState(false);
  const copy = async () => {
    setBusy(true); setFeedback('');
    try {
      const text=prepareTask ? await prepareTask() : task;
      if (isDesktop()) await desktopCommand('clipboard_write_text', { text });
      else await navigator.clipboard.writeText(text);
      setFeedback('已复制任务，请粘贴给已连接 VAC MCP 的 Agent。');
    } catch (error) { setFeedback(`保存或复制失败：${error instanceof Error?error.message:'请重试'}`); }
    finally { setBusy(false); }
  };
  return <Card size="small" title="交给外部 Agent">
    <Flex vertical gap={8}>
      <Typography.Text type="secondary">复制前先保存脚本。将任务粘贴给已连接 VAC 的 Agent，它会读取已保存的脚本并按要求编辑时间线。</Typography.Text>
      <Space wrap>
        {navigate && <Button onClick={() => navigate({ workspace: 'settings', settingsCategory: 'skills' })}>配置外部 Agent</Button>}
        <Button type="primary" disabled={!task.trim()} loading={busy} onClick={() => void copy()}>复制 Agent 任务</Button>
        <Button onClick={() => setOpen(true)}>查看任务文本</Button>
      </Space>
      <Collapse size="small" items={[{key:'connection',label:'Agent 连接状态',children:<ExternalAgentStatusPanel active={active}/>}]} />
      {feedback && <Alert role="status" type="info" message={feedback} />}
    </Flex>
    <Modal title="外部 Agent 任务" open={open} onCancel={() => setOpen(false)} footer={<Button loading={busy} onClick={() => void copy()}>复制 Agent 任务</Button>}>
      <Input.TextArea aria-label="Agent 任务文本" readOnly value={task} rows={12} />
    </Modal>
  </Card>;
}
