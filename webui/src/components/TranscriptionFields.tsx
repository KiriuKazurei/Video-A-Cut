import { Checkbox, Col, Form, Input, Row, Typography } from 'antd';
import type { ProcessingProfile } from '../preparation';

const EMPTY = { api_format: 'elevenlabs' as const, endpoint: 'https://api.elevenlabs.io/v1', model: 'scribe_v1', token_env: 'ELEVENLABS_API_KEY' };

/** Optional transcription preset. Flow B does not require ASR. */
export function TranscriptionFields({ draft, disabled, onChange }: {
  draft: ProcessingProfile;
  disabled?: boolean;
  onChange: (next: ProcessingProfile) => void;
}) {
  const enabled = Boolean(draft.transcription);
  const value = draft.transcription ?? EMPTY;
  const edit = (patch: Partial<typeof EMPTY>) => onChange({ ...draft, transcription: { ...value, ...patch, api_format: 'elevenlabs' } });
  return <>
    <Typography.Title level={5}>可选转录</Typography.Title>
    <Typography.Paragraph type="secondary">
      转录使用预设里的 endpoint、model 和 token_env，与推理 Key 分开。流程 B 不强制语音识别。关闭后，下一次保存的版本不再携带转录配置。
    </Typography.Paragraph>
    <Checkbox checked={enabled} disabled={disabled} onChange={(event) => onChange({ ...draft, transcription: event.target.checked ? value : undefined })}>
      启用 ElevenLabs 转录
    </Checkbox>
    {enabled && <Row gutter={12} style={{ marginTop: 8 }}>
      <Col xs={24} md={8}><Form.Item label="转录 endpoint"><Input aria-label="转录 endpoint" disabled={disabled} value={value.endpoint} onChange={(event) => edit({ endpoint: event.target.value })} /></Form.Item></Col>
      <Col xs={24} md={8}><Form.Item label="转录 model"><Input aria-label="转录 model" disabled={disabled} value={value.model} onChange={(event) => edit({ model: event.target.value })} /></Form.Item></Col>
      <Col xs={24} md={8}><Form.Item label="转录 token_env"><Input aria-label="转录 token_env" disabled={disabled} value={value.token_env} onChange={(event) => edit({ token_env: event.target.value })} /></Form.Item></Col>
    </Row>}
  </>;
}
