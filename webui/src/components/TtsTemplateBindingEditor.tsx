import { Button, Flex, Form, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { api } from '../api';
import type { TtsBinding } from '../ttsContracts';
import { ValueSelect } from './ui';

export function TtsTemplateBindingEditor({ binding, onChange }: { binding?: TtsBinding; onChange: (binding?: TtsBinding) => void }) {
  const templates = useQuery({ queryKey: ['tts-templates'], queryFn: api.ttsTemplates });
  const value = binding ? `${binding.template_id}/${binding.revision}` : '';
  const options = (templates.data ?? []).map(t => ({ value: `${t.template_id}/${t.revision}`, label: `${t.name} · v${t.revision}` }));
  if (binding && !options.some(o => o.value === value)) options.unshift({ value, label: `${binding.template_id} · v${binding.revision}（固定历史版本）` });
  return <Form.Item label="正式配音使用的 TTS 模板" layout="vertical" style={{ marginBottom: 8 }}>
    <Flex gap={8} wrap>
      <ValueSelect<string> aria-label="正式配音 TTS 模板" style={{ flex: '1 1 240px', minWidth: 0 }} value={value}
        loading={templates.isFetching} options={[{ value: '', label: '旧版 SAPI 配置（使用声音名称）' }, ...options]}
        onChange={key => { const t = templates.data?.find(t => `${t.template_id}/${t.revision}` === key);
          if (!key) onChange(undefined); else if (t) onChange({ template_id: t.template_id, revision: t.revision, sha256: t.sha256, settings: t.settings }); }} />
      <Button href="#/tts">打开 TTS 调控台</Button>
    </Flex>
    <Typography.Text type="secondary">保存处理预设时固定模板版本。更新模板后需在此重新选择并保存新预设版本。</Typography.Text>
    {templates.error && <Typography.Text type="danger" role="alert">模板读取失败，请刷新后重试。</Typography.Text>}
  </Form.Item>;
}
