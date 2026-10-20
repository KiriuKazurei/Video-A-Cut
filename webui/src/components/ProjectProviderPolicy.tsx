import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Card, Switch, Typography } from 'antd';
import { projectApi } from '../projectApi';
import { errorText } from './ui';
import { useState } from 'react';

export function ProjectProviderPolicy() {
  const client = useQueryClient();
  const [error, setError] = useState('');
  const [pending, setPending] = useState(false);
  const policy = useQuery({ queryKey: ['provider-policy'], queryFn: projectApi.providerPolicy });
  return <Card size="small" title="服务商调用设置" style={{ marginBottom: 16 }}>
    <Typography.Paragraph type="secondary">模型和转录使用下方预设中已启用的服务商设置，适用于所有工程。云端配音在这里统一启用。</Typography.Paragraph>
    <Switch aria-label="允许云端配音" checkedChildren="云端配音已启用" unCheckedChildren="云端配音未启用" checked={policy.data?.external_tts ?? false} loading={pending} onChange={value => {
      setPending(true); setError(''); void projectApi.saveProviderPolicy(value).then(saved => client.setQueryData(['provider-policy'], saved)).catch(reason => setError(errorText(reason))).finally(() => setPending(false));
    }} />
    {(error || policy.error) && <Alert type="error" showIcon message={error || errorText(policy.error)} />}
  </Card>;
}
