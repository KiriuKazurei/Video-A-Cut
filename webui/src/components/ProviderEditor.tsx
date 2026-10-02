import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Card, Col, Flex, Form, Input, Row, Space, Typography } from 'antd';
import { api } from '../api';
import { ValueSelect } from './ui';
import type { ProcessingProvider, ProviderDiagnostic, ProviderFormat } from '../preparation';

const defaults: Record<ProviderFormat, string> = {
  openai: 'https://api.openai.com/v1',
  anthropic: 'https://api.anthropic.com/v1',
  gemini: 'https://generativelanguage.googleapis.com/v1beta'
};

export function ProviderEditor({ role, provider, disabled, onChange }: {
  role: 'vision' | 'narration'; provider: ProcessingProvider; disabled: boolean;
  onChange: (next: ProcessingProvider) => void;
}) {
  // Credentials never enter query caches, profiles, localStorage or audit records.
  const [key, setKey] = useState('');
  const [busy, setBusy] = useState<'test' | 'models' | null>(null);
  const [error, setError] = useState('');
  const [models, setModels] = useState<{ signature: string; result: ProviderDiagnostic } | null>(null);
  const [test, setTest] = useState<{ signature: string; result: ProviderDiagnostic } | null>(null);
  const controller = useRef<AbortController | null>(null);
  const baseSignature = JSON.stringify([provider.api_format, provider.endpoint, provider.token_env, role, key]);
  const signature = JSON.stringify([baseSignature, provider.model]);
  const latest = useRef(signature);
  latest.current = signature;
  const credentialScope = JSON.stringify([provider.api_format, provider.endpoint, provider.token_env, role]);
  useEffect(() => { setKey(''); }, [credentialScope]);
  useEffect(() => {
    controller.current?.abort(); setBusy(null); setError('');
    return () => controller.current?.abort();
  }, [signature]);
  const available = models?.signature === baseSignature ? models.result : null;
  const tested = test?.signature === signature ? test.result : null;
  const run = async (operation: 'test' | 'models') => {
    const startedWith = signature;
    const abort = new AbortController(); controller.current?.abort(); controller.current = abort;
    setBusy(operation); setError('');
    if (operation === 'test') setTest(null); else setModels(null);
    try {
      const result = await api.diagnoseProvider(provider, operation, key, abort.signal);
      if (abort.signal.aborted || latest.current !== startedWith) return;
      if (operation === 'test') setTest({ signature: startedWith, result });
      else setModels({ signature: baseSignature, result });
    } catch (e) {
      if (!abort.signal.aborted && latest.current === startedWith) setError(e instanceof Error ? e.message : '请求失败');
    } finally {
      if (controller.current === abort) setBusy(null);
    }
  };
  const changeFormat = (format: string) => {
    if (!format) {
      const { api_format: _, ...rest } = provider;
      onChange(rest);
    } else {
      onChange({ ...provider, api_format: format as ProviderFormat, endpoint: defaults[format as ProviderFormat], model: '' });
    }
  };
  const modelValue = available?.ok && provider.model ? provider.model : undefined;
  return <Card size="small" className="provider-editor" data-provider-role={role} title={role === 'vision' ? '画面识别' : '中文解说'}>
    <Row gutter={12}>
      <Col xs={24} md={12}><Form.Item label="API 格式">
        <ValueSelect<string> disabled={disabled} value={provider.api_format || ''} onChange={changeFormat} options={[
          { value: 'openai', label: 'OpenAI · Chat Completions' }, { value: 'anthropic', label: 'Anthropic · Messages' },
          { value: 'gemini', label: 'Google · Gemini' }, { value: '', label: '原有自定义接口' }]} />
      </Form.Item></Col>
      <Col xs={24} md={12}><Form.Item label={provider.api_format ? 'API 基础地址' : '服务地址'}>
        <Input required type="url" disabled={disabled} value={provider.endpoint || ''} onChange={e => onChange({ ...provider, endpoint: e.target.value })} />
      </Form.Item></Col>
      <Col xs={24} md={12}><Form.Item label="模型 ID">
        <Input required disabled={disabled} value={provider.model || ''} onChange={e => onChange({ ...provider, model: e.target.value })} />
      </Form.Item></Col>
      <Col xs={24} md={12}><Form.Item label="运行凭证环境变量名">
        <Input required disabled={disabled} value={provider.token_env || ''} onChange={e => onChange({ ...provider, token_env: e.target.value })} />
      </Form.Item></Col>
    </Row>
    {provider.api_format && <Flex vertical gap={8}>
      <Typography.Text type="secondary">可替换为服务商兼容地址，保留其 API 路径前缀。连接测试使用固定文字{role === 'vision' ? '和合成图片' : ''}，可能产生少量费用。</Typography.Text>
      <Row gutter={12} align="bottom">
        <Col xs={24} md={12}><Form.Item label="本次测试用 API key" style={{ marginBottom: 8 }}>
          <Input.Password autoComplete="off" disabled={disabled} value={key} onChange={e => setKey(e.target.value)} placeholder="留空则使用启动环境中的凭证变量" />
        </Form.Item></Col>
        <Col flex="auto"><Space wrap style={{ marginBottom: 8 }}>
          <Button disabled={disabled || !!busy || !provider.endpoint || !provider.token_env} loading={busy === 'models'} onClick={() => void run('models')}>{busy === 'models' ? '发现中…' : '发现模型'}</Button>
          <Button disabled={disabled || !!busy || !provider.endpoint || !provider.model || !provider.token_env} loading={busy === 'test'} onClick={() => void run('test')}>{busy === 'test' ? '测试中…' : '测试连接'}</Button>
          {busy && <Button onClick={() => { controller.current?.abort(); setBusy(null); }}>取消请求</Button>}
          {key && <Button type="text" onClick={() => setKey('')}>清除测试 key</Button>}
        </Space></Col>
      </Row>
      <Typography.Text type="secondary">测试 key 仅用于当前请求。保存的预设使用环境变量名，正式 Worker 运行需要在启动环境中设置该变量。修改地址、格式、模型或凭证后请重新测试。</Typography.Text>
      {busy && <Typography.Text type="secondary" role="status">{busy === 'models' ? '正在读取模型列表…' : '正在请求所选模型…'}</Typography.Text>}
      {error && <Alert type="error" showIcon role="alert" message={error} />}
      {available && <div className="provider-models">
        <Alert type={available.ok ? 'success' : 'error'} showIcon role={available.ok ? 'status' : 'alert'} message={`${available.message}（${available.latency_ms} ms）`} />
        {available.ok && !!available.models?.length && <Form.Item label={`已发现模型（${available.models.length}${available.truncated ? '，部分结果' : ''}）`} style={{ margin: '8px 0 0' }}>
          <ValueSelect<string> disabled={disabled} value={modelValue} placeholder="选择模型，或在上方手动填写" onChange={v => { if (v) onChange({ ...provider, model: v }); }} options={[
            ...(provider.model && !available.models.some(m => m.id === provider.model) ? [{ value: provider.model, label: `${provider.model}（手动填写）` }] : []),
            ...available.models.map(m => ({ value: m.id, label: m.name === m.id ? m.id : `${m.name} · ${m.id}` }))]} />
        </Form.Item>}
        {available.ok && !available.models?.length && <Typography.Text type="secondary">该凭证未发现可用模型，可手动填写模型 ID 后测试。</Typography.Text>}
      </div>}
      {tested && <Alert type={tested.ok ? 'success' : 'error'} showIcon role={tested.ok ? 'status' : 'alert'} message={`${tested.message}（${tested.latency_ms} ms）`} />}
    </Flex>}
  </Card>;
}
