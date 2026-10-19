import { useState } from 'react';
import { Alert, Button, Flex, Form, Input, Typography } from 'antd';
import { skillApi } from '../skillApi';
import { useSkillWorkspace } from '../skillWorkspace';
import { errorText } from './ui';
import { humanUnlockMessage, isHumanUnlockRequired } from '../skillView';

export function HumanSessionControl({ compact = false }: { compact?: boolean }) {
  const workspace = useSkillWorkspace();
  const [token, setToken] = useState('');
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);

  const unlock = async () => {
    setPending(true);
    setError('');
    try {
      const result = await skillApi.unlockHumanSession(token.trim());
      setToken('');
      if (!result.unlocked) {
        setError('服务端没有签发人工会话。');
        return;
      }
      workspace.markUnlocked();
    } catch (reason) {
      setError(isHumanUnlockRequired(reason) ? humanUnlockMessage() : errorText(reason));
    } finally {
      setPending(false);
    }
  };

  return <Flex vertical gap={8} className="human-session-control">
    <Typography.Text type="secondary">
      {workspace.unlocked ? '人工会话已解锁。凭证只留在本次内存请求头，不写入本地存储。' : '受保护的项目、运行、日志和审计需要人工会话。'}
    </Typography.Text>
    {!workspace.unlocked && <Form layout={compact ? 'inline' : 'vertical'} onFinish={() => void unlock()}>
      <Form.Item label="人工访问凭证" style={{ marginBottom: compact ? 0 : 8, minWidth: compact ? 220 : undefined }}>
        <Input.Password aria-label="人工访问凭证" autoComplete="off" value={token} onChange={(event) => setToken(event.target.value)} placeholder="宿主配置的 VAC_HUMAN_ACCESS_TOKEN" />
      </Form.Item>
      <Form.Item label={compact ? ' ' : undefined} style={{ marginBottom: 0 }}>
        <Button type="primary" htmlType="submit" loading={pending} disabled={token.trim().length === 0}>解锁人工会话</Button>
      </Form.Item>
    </Form>}
    {workspace.unlocked && <Button loading={pending} onClick={() => void workspace.lock().catch((reason) => setError(errorText(reason)))}>退出人工会话</Button>}
    {error && <Alert type="error" showIcon message={error} />}
    {!compact && <Typography.Text type="secondary">退出会清除内存凭证、受保护缓存、当前项目/运行选择，并停止播放器。</Typography.Text>}
  </Flex>;
}

export function ProtectedError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  if (!error) return null;
  if (isHumanUnlockRequired(error)) {
    return <Alert type="warning" showIcon message="需要解锁人工会话" description={<Flex vertical gap={8}><span>{humanUnlockMessage()}</span><HumanSessionControl compact /></Flex>} />;
  }
  return <Alert type="error" showIcon message={errorText(error)} action={onRetry ? <Button size="small" onClick={onRetry}>重新读取</Button> : undefined} />;
}
