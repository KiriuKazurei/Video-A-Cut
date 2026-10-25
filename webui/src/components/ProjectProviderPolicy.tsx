import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Switch, Typography } from 'antd';
import { projectApi } from '../projectApi';
import { errorText } from './ui';
import { useState } from 'react';

export function ProjectProviderPolicy() {
  const client = useQueryClient();
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const policy = useQuery({ queryKey: ['provider-policy'], queryFn: projectApi.providerPolicy });
  return <Card size="small" title="全局调用设置" className="settings-provider-policy">
    <div className="settings-policy-row">
    <div><Typography.Text strong>允许云端配音</Typography.Text><Typography.Paragraph type="secondary">适用于所有工程。模型和转录使用当前预设中启用的配置。</Typography.Paragraph></div>
    <Switch aria-label="允许云端配音" checkedChildren="已启用" unCheckedChildren="未启用" checked={policy.data?.external_tts ?? false} loading={pending || policy.isPending} disabled={policy.isError} onChange={value => {
      setPending(true); setError(''); void projectApi.saveProviderPolicy(value).then(saved => client.setQueryData(['provider-policy'], saved)).catch(reason => setError(errorText(reason))).finally(() => setPending(false));
    }} />
    </div>
    <Typography.Text type="secondary">此开关立即保存；下方预设修改需保存新版本。</Typography.Text>
    {(error || policy.error) && <Alert role="alert" type="error" showIcon message={error || errorText(policy.error)} action={policy.isError ? <Button size="small" onClick={()=>void policy.refetch()}>重试</Button> : undefined} />}
  </Card>;
}
