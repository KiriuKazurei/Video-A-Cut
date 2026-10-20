import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Checkbox, Col, Flex, Form, Input, InputNumber, Row, Typography } from 'antd';
import { api } from '../api';
import { desktopCommand, isDesktop } from '../desktop';
import type { ProcessingProfile, TranscriptionProvider, TranscriptionDiagnostic } from '../preparation';
import { ValueSelect } from './ui';

const LOCAL: TranscriptionProvider = { api_format: 'faster_whisper', model_path: '', device: 'cpu', compute_type: 'int8', cpu_threads: 4 };
const CLOUD: TranscriptionProvider = { api_format: 'elevenlabs', endpoint: 'https://api.elevenlabs.io/v1', model: 'scribe_v1', token_env: 'ELEVENLABS_API_KEY' };

/** Optional transcription uses a frozen profile; local recognition never downloads or uploads. */
export function TranscriptionFields({ draft, disabled, onChange }: {
  draft: ProcessingProfile; disabled?: boolean; onChange: (next: ProcessingProfile) => void;
}) {
  const value = draft.transcription ?? LOCAL;
  const enabled = Boolean(draft.transcription);
  const local = value.api_format === 'faster_whisper';
  const signature = JSON.stringify([enabled, value]);
  const latest = useRef(signature); latest.current = signature;
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [diagnostic, setDiagnostic] = useState<{ signature: string; result: TranscriptionDiagnostic }>();
  useEffect(() => {
    controller.current?.abort(); setBusy(false); setError('');
    return () => controller.current?.abort();
  }, [signature]);
  const edit = (patch: Partial<TranscriptionProvider>) => onChange({ ...draft, transcription: { ...value, ...patch } });
  const pick = async (field: 'model_path' | 'python_executable') => {
    const started = signature;
    try {
      const files = await desktopCommand<string[]>('project_dialog', { mode: field === 'model_path' ? 'transcription-model' : 'transcription-python', defaultPath: value[field] || null });
      if (files[0] && latest.current === started) edit({ [field]: files[0] });
    } catch (reason) { if (latest.current === started) setError(String(reason)); }
  };
  const check = async () => {
    const started = signature;
    const abort = new AbortController(); controller.current?.abort(); controller.current = abort;
    setBusy(true); setError('');
    try {
      const result = await api.checkLocalTranscription(value, abort.signal);
      if (latest.current === started && !abort.signal.aborted) setDiagnostic({ signature: started, result });
    } catch (reason) { if (!abort.signal.aborted && latest.current === started) setError(String(reason)); }
    finally { if (latest.current === started) setBusy(false); }
  };
  const result = diagnostic?.signature === signature ? diagnostic.result : undefined;
  return <div className="transcription-settings">
    <Typography.Title level={5}>可选转录</Typography.Title>
    <Typography.Paragraph type="secondary">轻量流程需要词级时间戳，画面流程可不使用转录。调用许可统一在服务商设置中管理。</Typography.Paragraph>
    <Checkbox checked={enabled} disabled={disabled} onChange={event => onChange({ ...draft, transcription: event.target.checked ? value : undefined })}>启用转录</Checkbox>
    {enabled && <>
      <Form.Item label="转录方式" style={{ marginTop: 8 }}>
        <ValueSelect<TranscriptionProvider['api_format']> aria-label="转录方式" disabled={disabled} value={value.api_format} onChange={format => onChange({ ...draft, transcription: { ...(format === 'faster_whisper' ? LOCAL : CLOUD) } })}
          options={[{ value: 'faster_whisper', label: '本地 faster-whisper' }, { value: 'elevenlabs', label: 'ElevenLabs 云转录' }]} />
      </Form.Item>
      {local ? <Flex vertical gap={8}>
        <Typography.Text type="secondary">只读取所选本地模型目录和当前工程素材，不自动下载模型、不上传素材、不回退云服务。</Typography.Text>
        <Form.Item label="本地模型目录" style={{ marginBottom: 0 }}><Flex gap={8}>
          <Input required aria-label="本地模型目录" disabled={disabled} value={value.model_path} placeholder="包含 model.bin、config.json 和 tokenizer.json 的绝对路径" onChange={event => edit({ model_path: event.target.value })} />
          {isDesktop() && <Button disabled={disabled} onClick={() => void pick('model_path')}>选择模型目录</Button>}
        </Flex></Form.Item>
        <Form.Item label="本地转录 Python（可选）" style={{ marginBottom: 0 }}><Flex gap={8}>
          <Input aria-label="本地转录 Python" disabled={disabled} value={value.python_executable ?? ''} placeholder="留空使用桌面 Worker 环境；或填写已安装依赖的 Python 绝对路径" onChange={event => edit({ python_executable: event.target.value })} />
          {isDesktop() && <Button disabled={disabled} onClick={() => void pick('python_executable')}>选择 Python</Button>}
        </Flex></Form.Item>
        <Row gutter={12}>
          <Col xs={12} md={6}><Form.Item label="运行设备"><ValueSelect<'cpu' | 'cuda'> aria-label="转录运行设备" disabled={disabled} value={value.device ?? 'cpu'} onChange={device => edit({ device, compute_type: device === 'cpu' ? 'int8' : 'float16' })} options={[{ value: 'cpu', label: 'CPU' }, { value: 'cuda', label: 'NVIDIA CUDA' }]} /></Form.Item></Col>
          <Col xs={12} md={6}><Form.Item label="计算精度"><ValueSelect<string> aria-label="转录计算精度" disabled={disabled} value={value.compute_type ?? 'int8'} onChange={compute => edit({ compute_type: compute as TranscriptionProvider['compute_type'] })} options={(value.device === 'cuda' ? ['float16', 'int8_float16', 'int8', 'float32'] : ['int8', 'float32']).map(compute => ({ value: compute, label: compute }))} /></Form.Item></Col>
          <Col xs={12} md={6}><Form.Item label="语言代码"><Input aria-label="转录语言" disabled={disabled} maxLength={3} value={value.language ?? ''} placeholder="留空自动，zh / en" onChange={event => edit({ language: event.target.value.trim().toLowerCase() })} /></Form.Item></Col>
          <Col xs={12} md={6}><Form.Item label="CPU 线程"><InputNumber aria-label="转录 CPU 线程" disabled={disabled} min={1} max={16} value={value.cpu_threads || 4} onChange={threads => edit({ cpu_threads: Number(threads ?? 4) })} /></Form.Item></Col>
        </Row>
        <Flex gap={8}><Button disabled={disabled || busy || !value.model_path} loading={busy} onClick={() => void check()}>检查本地转录环境</Button>{busy && <Button onClick={() => { controller.current?.abort(); setBusy(false); }}>取消检查</Button>}</Flex>
        {busy && <Typography.Text role="status">正在核对本地模型与依赖，不执行识别…</Typography.Text>}
        {result && <Alert showIcon type={result.ok ? 'success' : 'error'} role={result.ok ? 'status' : 'alert'} message={result.message} description={result.python_executable ? `Python：${result.python_executable}${result.dependency_version ? ` · faster-whisper ${result.dependency_version}` : ''}` : undefined} />}
      </Flex> : <Row gutter={12}>
        <Col xs={24} md={8}><Form.Item label="转录 endpoint"><Input aria-label="转录 endpoint" disabled={disabled} value={value.endpoint} onChange={event => edit({ endpoint: event.target.value })} /></Form.Item></Col>
        <Col xs={24} md={8}><Form.Item label="转录 model"><Input aria-label="转录 model" disabled={disabled} value={value.model} onChange={event => edit({ model: event.target.value })} /></Form.Item></Col>
        <Col xs={24} md={8}><Form.Item label="转录 token_env"><Input aria-label="转录 token_env" disabled={disabled} value={value.token_env} onChange={event => edit({ token_env: event.target.value })} /></Form.Item></Col>
      </Row>}
      {error && <Alert type="error" showIcon role="alert" message={error} />}
    </>}
  </div>;
}
