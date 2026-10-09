import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Alert, Button, Card, Flex, Form, Grid, Input, InputNumber, List, Popconfirm, Segmented, Slider, Space, Tag, Typography } from 'antd';
import { DownloadOutlined, PlayCircleOutlined, ReloadOutlined, SaveOutlined, SoundOutlined } from '@ant-design/icons';
import { api } from '../api';
import { DEFAULT_TTS_SETTINGS, TTS_MOODS, currentTtsPreview, ttsDraftError, ttsDraftKey, ttsInstruction, type TtsSettings, type TtsTemplate } from '../ttsContracts';
import { ValueSelect, errorText } from '../components/ui';
import { TtsWaveform } from '../components/TtsWaveform';

const SAMPLE_TEXT = '欢迎来到声音调控台。调整音色、语速和语气，找到适合你的表达方式。';
const stateLabels = { queued: '等待生成', running: '正在合成', succeeded: '试听已生成', failed: '生成失败', cancelled: '已取消' };

function Fader({ label, value, min, max, step, reset, suffix, disabled, onChange, vertical }: {
  label: string; value: number; min: number; max: number; step: number; reset: number; suffix: string;
  disabled?: boolean; onChange: (value: number) => void; vertical: boolean;
}) {
  return <div className="tts-fader">
    <Typography.Text>{label}</Typography.Text>
    <div className={vertical ? 'tts-fader-track vertical' : 'tts-fader-track'}>
      <Slider aria-label={label} ariaLabelForHandle={label} vertical={vertical} min={min} max={max} step={step} value={value} disabled={disabled} onChange={onChange} />
    </div>
    <InputNumber aria-label={`${label}数值`} min={min} max={max} step={step} value={value} disabled={disabled} onChange={v => onChange(v ?? reset)} />
    <Typography.Text type="secondary">{suffix}</Typography.Text>
    <Button size="small" type="text" disabled={disabled} onClick={() => onChange(reset)} aria-label={`重置${label}`}>重置</Button>
  </div>;
}

export function TtsWorkspace({ active }: { active: boolean }) {
  const client = useQueryClient(), screens = Grid.useBreakpoint();
  const [settings, setSettings] = useState<TtsSettings>({ ...DEFAULT_TTS_SETTINGS });
  const [text, setText] = useState(SAMPLE_TEXT), [name, setName] = useState('我的声音模板');
  const [loaded, setLoaded] = useState<TtsTemplate>(), [dirty, setDirty] = useState(false);
  const [mood, setMood] = useState(0), [custom, setCustom] = useState('');
  const [jobID, setJobID] = useState(''), [historyRevision, setHistoryRevision] = useState(1);
  const [capabilityEnv, setCapabilityEnv] = useState('VAC_TTS_TOKEN');
  const [notice, setNotice] = useState(''), [error, setError] = useState('');
  const audio = useRef<HTMLAudioElement>(null), autoplayJob = useRef('');
  const templates = useQuery({ queryKey: ['tts-templates'], queryFn: api.ttsTemplates, enabled: active });
  useEffect(() => { const timer = setTimeout(() => setCapabilityEnv(settings.token_env || 'VAC_TTS_TOKEN'), 400); return () => clearTimeout(timer); }, [settings.token_env]);
  const capability = useQuery({ queryKey: ['tts-capabilities', capabilityEnv],
    queryFn: () => api.ttsCapabilities(capabilityEnv), enabled: active });
  const previews = useQuery({ queryKey: ['tts-previews'], queryFn: api.ttsPreviews, enabled: active });
  const preview = useQuery({ queryKey: ['tts-preview', jobID], queryFn: () => api.ttsPreview(jobID), enabled: active && !!jobID,
    refetchInterval: q => q.state.data && ['queued', 'running'].includes(q.state.data.status) ? 1500 : false });
  const result = preview.data;
  const running = !!result && ['queued', 'running'].includes(result.status);
  const current = currentTtsPreview(result, text, settings);
  const audioURL = result?.status === 'succeeded' && !result.expired ? api.ttsAudioURL(result.job_id) : undefined;
  const draftError = ttsDraftError(text, settings), saveError = ttsDraftError(text, settings, false);
  const save = useMutation({ mutationFn: (snapshot: { copy: boolean; source?: TtsTemplate; name: string; settings: TtsSettings; text: string }) => api.saveTtsTemplate({
    template_id: !snapshot.copy && snapshot.source ? snapshot.source.template_id : `voice_${crypto.randomUUID().replaceAll('-', '')}`,
    name: snapshot.name, settings: snapshot.settings, reference_text: snapshot.text
  }, !snapshot.copy && snapshot.source ? snapshot.source.revision : 0), onSuccess: t => {
    const changed = ttsDraftKey(t.reference_text, t.settings) !== ttsDraftKey(text, settings) || t.name !== name;
    setLoaded(t); setHistoryRevision(t.revision); setDirty(changed); setNotice(`已保存 ${t.name} · v${t.revision}${changed ? '；当前草稿还有新修改' : ''}`); setError('');
    void client.invalidateQueries({ queryKey: ['tts-templates'] });
  }, onError: e => setError(errorText(e)) });
  const generate = useMutation({ mutationFn: () => api.startTtsPreview(text, settings), onSuccess: p => {
    setJobID(p.job_id); client.setQueryData(['tts-preview', p.job_id], p); autoplayJob.current = p.job_id;
    setError(''); setNotice('已提交试听；音色和参数已固定为本次配置。');
    void client.invalidateQueries({ queryKey: ['tts-previews'] });
  }, onError: e => setError(errorText(e)) });
  const action = useMutation({ mutationFn: (fn: () => Promise<unknown>) => fn(), onSuccess: () => {
    setError(''); void client.invalidateQueries({ queryKey: ['tts-templates'] });
    void client.invalidateQueries({ queryKey: ['tts-preview'] }); void client.invalidateQueries({ queryKey: ['tts-previews'] });
  }, onError: e => setError(errorText(e)) });
  const edit = (patch: Partial<TtsSettings>) => { setSettings(s => ({ ...s, ...patch })); setDirty(true); setNotice(''); };
  const load = (t: TtsTemplate) => {
    audio.current?.pause(); autoplayJob.current = ''; setSettings({ ...t.settings }); setLoaded(t); setName(t.name);
    setText(t.reference_text || SAMPLE_TEXT); setMood(0); setCustom(t.settings.instruction); setHistoryRevision(t.revision);
    setDirty(false); setNotice(`已加载 ${t.name} · v${t.revision}`); setError('');
  };
  useEffect(() => {
    if (settings.engine === 'sapi' && !settings.voice && capability.data?.sapi_voices.length)
      setSettings(s => s.engine === 'sapi' && !s.voice ? { ...s, voice: capability.data!.sapi_voices[0] } : s);
  }, [settings.engine, settings.voice, capability.data]);
  useEffect(() => {
    if (!result || ['queued', 'running'].includes(result.status)) return;
    void client.invalidateQueries({ queryKey: ['tts-previews'] });
    if (autoplayJob.current !== result.job_id) return;
    autoplayJob.current = '';
    if (active && current && result.status === 'succeeded') void audio.current?.play().catch(() => setNotice('试听已生成，请点击播放。'));
  }, [result?.status, result?.job_id, active, current, client]);
  useEffect(() => { if (!active) { audio.current?.pause(); autoplayJob.current = ''; } }, [active]);
  const cap = capability.data;
  const voices = settings.engine === 'stepfun' ? (cap?.stepfun_voices ?? [{ id: 'vibrant-youth', name: '元气男声' }]).map(v => ({ value: v.id, label: v.name }))
    : (cap?.sapi_voices ?? []).map(v => ({ value: v, label: v }));
  if (settings.voice && !voices.some(v => v.value === settings.voice)) voices.unshift({ value: settings.voice, label: `${settings.voice}（当前未列出）` });
  const pending = save.isPending || action.isPending;
  const saveSnapshot = (copy: boolean) => save.mutate({ copy, source: loaded, name, text, settings: { ...settings } });

  return <div className="page-body tts-workspace">
    <div className="tts-console-grid">
      <Card title="声音模板" size="small" className="tts-template-library">
        <Space direction="vertical" style={{ width: '100%' }}>
          <Button block disabled={pending} onClick={() => { setLoaded(undefined); setName('新声音模板'); setDirty(true); setNotice('当前声音配置将另存为新模板。'); }}>新建模板</Button>
          {templates.error && <Alert type="error" message="模板读取失败" showIcon />}
          <List size="small" loading={templates.isFetching} dataSource={templates.data ?? []} locale={{ emptyText: '还没有声音模板' }}
            renderItem={t => <List.Item className={loaded?.template_id === t.template_id ? 'tts-template-selected' : ''}>
              <div style={{ width: '100%' }}>
                <Popconfirm title="加载模板将替换未保存草稿" disabled={!dirty} onConfirm={() => load(t)}>
                  <Button type="text" block disabled={pending} onClick={() => { if (!dirty) load(t); }} className="tts-template-button">{t.name}</Button>
                </Popconfirm>
                <Typography.Text type="secondary">v{t.revision} · {t.settings.engine === 'stepfun' ? 'StepAudio' : 'SAPI'}</Typography.Text>
              </div>
            </List.Item>} />
          {loaded && <>
            <Form.Item label="历史版本" style={{ marginBottom: 0 }}><InputNumber min={1} precision={0} aria-label="TTS 模板历史版本" value={historyRevision} onChange={v => setHistoryRevision(v ?? 1)} /></Form.Item>
            <Popconfirm title="加载历史版本将替换当前草稿" disabled={!dirty} onConfirm={() => action.mutate(async () => load(await api.ttsTemplate(loaded.template_id, historyRevision)))}>
              <Button block disabled={pending} onClick={() => { if (!dirty) action.mutate(async () => load(await api.ttsTemplate(loaded.template_id, historyRevision))); }}>加载历史版本</Button>
            </Popconfirm>
            <Popconfirm title="归档后从模板库隐藏，已绑定版本仍可使用" onConfirm={() => action.mutate(async () => { await api.archiveTtsTemplate(loaded.template_id, loaded.revision); setLoaded(undefined); setDirty(true); })}>
              <Button block danger disabled={pending}>归档模板</Button>
            </Popconfirm>
          </>}
        </Space>
      </Card>

      <div className="tts-text-column">
        <Card title="文本与试听" size="small" extra={<Tag>{settings.engine === 'stepfun' ? 'StepAudio 2.5' : 'Windows SAPI'}</Tag>}>
          <Form layout="vertical">
            <Form.Item label="朗读文本" help={`${Array.from(text).length} / 1,000 字符`} validateStatus={Array.from(text).length > 1000 ? 'error' : undefined}>
              <Input.TextArea aria-label="TTS 朗读文本" value={text} rows={7} onChange={e => { setText(e.target.value); setDirty(true); }} />
            </Form.Item>
            {settings.engine === 'stepfun' && <Typography.Paragraph type="secondary">圆括号内的文字作为表达指令，不会被朗读。仅在点击“生成试听”后发送文本到当前云端服务。</Typography.Paragraph>}
            {(error || preview.error || capability.error) && <Alert showIcon type="error" role="alert" message={error || errorText(preview.error || capability.error)} />}
            {notice && <Alert showIcon type="info" role="status" message={notice} />}
            {draftError && <Typography.Paragraph type="danger" role="alert">{draftError}</Typography.Paragraph>}
            <Flex gap={8} wrap className="tts-generate-actions">
              <Button type="primary" icon={<PlayCircleOutlined />} loading={generate.isPending} disabled={!!draftError || running || !cap?.runtime_ready || !cap.media_ready || (settings.engine === 'stepfun' && !cap.credentials_ready) || (!!settings.pitch_semitones && !cap.pitch_available)} onClick={() => generate.mutate()}>生成试听</Button>
              <Button disabled={!running || pending} onClick={() => action.mutate(() => api.cancelTtsPreview(jobID))}>取消生成</Button>
              <Button icon={<ReloadOutlined />} onClick={() => { void capability.refetch(); void templates.refetch(); void previews.refetch(); }}>刷新运行条件</Button>
            </Flex>
          </Form>
          <div className="tts-player-panel">
            {result && <Flex gap={8} wrap><Tag color={result.status === 'failed' ? 'error' : result.status === 'succeeded' ? 'success' : 'processing'}>{stateLabels[result.status]}</Tag>
              {result.status === 'succeeded' && <Tag color={current ? 'blue' : 'warning'}>{result.expired ? '缓存已过期' : current ? '当前配置' : '旧配置试听'}</Tag>}
              {!!result.duration_seconds && <Typography.Text type="secondary">{result.duration_seconds.toFixed(2)} 秒 · 48 kHz · 双声道</Typography.Text>}</Flex>}
            {result?.message && <Alert role="status" showIcon type={result.status === 'failed' ? 'error' : 'info'} message={result.message} />}
            <TtsWaveform url={audioURL} />
            {audioURL && <><audio ref={audio} controls src={audioURL} aria-label="TTS 试听播放器" preload="metadata" />
              <Space wrap><Button onClick={() => { if (audio.current) { audio.current.currentTime = 0; void audio.current.play().catch(() => setNotice('请使用播放器上的播放按钮。')); } }}>从头重听</Button>
                <Button icon={<DownloadOutlined />} href={api.ttsAudioURL(result!.job_id, true)}>下载 WAV</Button></Space></>}
          </div>
          <Form layout="vertical" className="tts-save-form">
            <Form.Item label="模板名称"><Input aria-label="TTS 模板名称" value={name} maxLength={100} onChange={e => { setName(e.target.value); setDirty(true); }} /></Form.Item>
            <Space wrap><Button icon={<SaveOutlined />} loading={save.isPending} disabled={!!saveError || !name.trim()} onClick={() => saveSnapshot(false)}>{loaded ? '保存新版本' : '保存模板'}</Button>
              <Button disabled={!!saveError || !name.trim() || save.isPending} onClick={() => saveSnapshot(true)}>另存为</Button>
              {loaded && <Typography.Text type="secondary">{loaded.name} · v{loaded.revision}{dirty ? ' · 有未保存修改' : ''}</Typography.Text>}</Space>
          </Form>
        </Card>
        <Card title="最近试听" size="small"><List size="small" dataSource={previews.data ?? []} locale={{ emptyText: '生成试听后可在这里重新加载' }}
          renderItem={p => <List.Item extra={<Button size="small" onClick={() => { audio.current?.pause(); autoplayJob.current = ''; setJobID(p.job_id); }}>查看</Button>}>
            <List.Item.Meta title={`${new Date(p.created_at).toLocaleTimeString('zh-CN')} · ${stateLabels[p.status]}${p.expired && p.status === 'succeeded' ? ' · 已过期' : ''}`} description={p.message || p.text.slice(0, 48)} />
          </List.Item>} /></Card>
      </div>

      <Card title="声音控制台" size="small" className="tts-controls">
        <Form layout="vertical">
          <Form.Item label="TTS 引擎"><Segmented block aria-label="TTS 引擎" value={settings.engine} options={[{ value: 'stepfun', label: 'StepFun 云端' }, { value: 'sapi', label: 'Windows SAPI' }]}
            onChange={v => { const next: TtsSettings = v === 'stepfun' ? { ...DEFAULT_TTS_SETTINGS } : { engine: 'sapi', voice: cap?.sapi_voices[0] ?? '', language: 'zh', speed: 1, volume: 1, pitch_semitones: 0, sapi_rate: 0, instruction: '' };
              setSettings(next); setMood(0); setCustom(''); setDirty(true); }} /></Form.Item>
          <Form.Item label="音色"><ValueSelect<string> aria-label="TTS 音色" style={{ width: '100%' }} value={settings.voice || undefined} options={voices} onChange={voice => edit({ voice })} /></Form.Item>
          {cap?.message && <Alert showIcon type="warning" message={cap.message} />}
          {settings.engine === 'stepfun' && <>
            <Form.Item label="API 地址"><Input aria-label="TTS API 地址" value={settings.endpoint} onChange={e => edit({ endpoint: e.target.value })} /></Form.Item>
            <Form.Item label="凭证环境变量" help={cap?.credentials_ready ? '已配置凭证' : '启动环境尚未配置此变量；仍可保存模板。'}>
              <Input aria-label="TTS 凭证变量" value={settings.token_env} onChange={e => edit({ token_env: e.target.value })} /></Form.Item>
            <Form.Item label="语言"><ValueSelect<TtsSettings['language']> aria-label="TTS 语言" style={{ width: '100%' }} value={settings.language} onChange={language => edit({ language })} options={[{ value: 'zh', label: '中文' }, { value: 'en', label: '英语' }, { value: 'ja', label: '日语（预览）' }, { value: 'ko', label: '韩语（预览）' }, { value: 'fr', label: '法语（预览）' }, { value: 'es', label: '西班牙语（预览）' }]} /></Form.Item>
          </>}
          <div className="tts-fader-bank">
            <Fader label="语速" vertical={!!screens.lg} value={settings.engine === 'sapi' ? settings.sapi_rate : settings.speed} min={settings.engine === 'sapi' ? -10 : .5} max={settings.engine === 'sapi' ? 10 : 2} step={settings.engine === 'sapi' ? 1 : .05} reset={settings.engine === 'sapi' ? 0 : 1} suffix={settings.engine === 'sapi' ? 'SAPI 等级' : '倍速'} onChange={v => edit(settings.engine === 'sapi' ? { sapi_rate: v } : { speed: v })} />
            <Fader label="音量" vertical={!!screens.lg} value={settings.volume} min={.1} max={settings.engine === 'sapi' ? 1 : 2} step={.05} reset={1} suffix="倍增益" onChange={volume => edit({ volume })} />
            <Fader label="音调" vertical={!!screens.lg} value={settings.pitch_semitones} min={-12} max={12} step={1} reset={0} suffix="半音 · 后期移调" disabled={!cap?.pitch_available} onChange={pitch_semitones => edit({ pitch_semitones })} />
          </div>
          {!cap?.pitch_available && <Typography.Paragraph type="secondary">当前移调能力不可用；需要 FFmpeg rubberband 滤镜。</Typography.Paragraph>}
          <Form.Item label="语气预设"><ValueSelect<number> aria-label="TTS 语气" disabled={settings.engine === 'sapi'} value={mood} style={{ width: '100%' }} options={TTS_MOODS.map((m, i) => ({ value: i, label: m.label }))} onChange={v => { setMood(v); edit({ instruction: ttsInstruction(v, custom) }); }} /></Form.Item>
          <Form.Item label="自定义表达指令" help={settings.engine === 'sapi' ? 'SAPI 不支持模型语气控制' : '语气由模型理解，效果以试听为准。'}>
            <Input.TextArea aria-label="TTS 自定义表达指令" rows={3} disabled={settings.engine === 'sapi'} value={custom} onChange={e => { setCustom(e.target.value); edit({ instruction: ttsInstruction(mood, e.target.value) }); }} /></Form.Item>
          {settings.engine === 'stepfun' && <><Typography.Text type="secondary">实际发送的 instruction · {Array.from(settings.instruction).length}/200</Typography.Text>
            <pre className="tts-instruction-preview">{settings.instruction || '中性朗读（无额外指令）'}</pre></>}
        </Form>
      </Card>
    </div>
  </div>;
}
