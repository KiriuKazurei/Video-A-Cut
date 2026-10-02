import { useEffect, useRef, useState } from 'react';
import { api } from '../api';
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
  return <fieldset className="provider-editor" data-provider-role={role}>
    <legend>{role === 'vision' ? '画面识别' : '中文解说'}</legend>
    <div className="action-row">
      <label className="field-label">API 格式<select disabled={disabled} value={provider.api_format || ''} onChange={e => changeFormat(e.target.value)}>
        <option value="openai">OpenAI · Chat Completions</option>
        <option value="anthropic">Anthropic · Messages</option>
        <option value="gemini">Google · Gemini</option>
        <option value="">原有自定义接口</option>
      </select></label>
      <label className="field-label">{provider.api_format ? 'API 基础地址' : '服务地址'}<input required type="url" disabled={disabled} value={provider.endpoint || ''} onChange={e => onChange({ ...provider, endpoint: e.target.value })} /></label>
      <label className="field-label">模型 ID<input required disabled={disabled} value={provider.model || ''} onChange={e => onChange({ ...provider, model: e.target.value })} /></label>
      <label className="field-label">运行凭证环境变量名<input required disabled={disabled} value={provider.token_env || ''} onChange={e => onChange({ ...provider, token_env: e.target.value })} /></label>
    </div>
    {provider.api_format && <>
      <p className="hint">可替换为服务商兼容地址，保留其 API 路径前缀。连接测试使用固定文字{role === 'vision' ? '和合成图片' : ''}，可能产生少量费用。</p>
      <div className="action-row">
        <label className="field-label">本次测试用 API key<input type="password" autoComplete="off" disabled={disabled} value={key} onChange={e => setKey(e.target.value)} placeholder="留空则使用启动环境中的凭证变量" /></label>
        <button type="button" disabled={disabled || !!busy || !provider.endpoint || !provider.token_env} onClick={() => void run('models')}>{busy === 'models' ? '发现中…' : '发现模型'}</button>
        <button type="button" disabled={disabled || !!busy || !provider.endpoint || !provider.model || !provider.token_env} onClick={() => void run('test')}>{busy === 'test' ? '测试中…' : '测试连接'}</button>
        {busy && <button type="button" className="secondary-button" onClick={() => { controller.current?.abort(); setBusy(null); }}>取消请求</button>}
        {key && <button type="button" className="secondary-button" onClick={() => setKey('')}>清除测试 key</button>}
      </div>
      <p className="hint">测试 key 仅用于当前请求。保存的预设使用环境变量名，正式 Worker 运行需要在启动环境中设置该变量。修改地址、格式、模型或凭证后请重新测试。</p>
      {busy && <p role="status">{busy === 'models' ? '正在读取模型列表…' : '正在请求所选模型…'}</p>}
      {error && <p role="alert" className="inline-error">{error}</p>}
      {available && <div className="provider-models">
        <p role={available.ok ? 'status' : 'alert'} className={available.ok ? '' : 'inline-error'}>{available.message}（{available.latency_ms} ms）</p>
        {available.ok && !!available.models?.length && <label className="field-label">已发现模型（{available.models.length}{available.truncated ? '，部分结果' : ''}）<select disabled={disabled} value={provider.model || ''} onChange={e => { if (e.target.value) onChange({ ...provider, model: e.target.value }); }}>
          <option value="">选择模型，或在上方手动填写</option>
          {provider.model && !available.models.some(m => m.id === provider.model) && <option value={provider.model}>{provider.model}（手动填写）</option>}
          {available.models.map(m => <option key={m.id} value={m.id}>{m.name === m.id ? m.id : `${m.name} · ${m.id}`}</option>)}
        </select></label>}
        {available.ok && !available.models?.length && <p className="hint">该凭证未发现可用模型，可手动填写模型 ID 后测试。</p>}
      </div>}
      {tested && <p role={tested.ok ? 'status' : 'alert'} className={tested.ok ? '' : 'inline-error'}>{tested.message}（{tested.latency_ms} ms）</p>}
    </>}
  </fieldset>;
}
