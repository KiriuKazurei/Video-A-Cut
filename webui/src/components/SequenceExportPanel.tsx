import { useState } from 'react';
import { Alert, Button, Card, Form, InputNumber, Radio, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useProjectWorkspace } from '../projectWorkspace';
import { useSequenceEditor } from '../sequenceEditor';
import { desktopCommand, isDesktop } from '../desktop';
import { timelineApi } from '../timelineApi';
import { errorText, ValueSelect } from './ui';
import { SEQUENCE_EXPORT_PRESETS, sequenceExportRequest, type SequenceExportPresetId } from '../exportPresets';

/** Sequence delivery controls on the export page. The program-native preset keeps the existing preview copy. */
export function SequenceExportPanel() {
  const project = useProjectWorkspace();
  const editor = useSequenceEditor();
  const session = project.session;
  const timeline = useQuery({
    queryKey: ['timeline', session?.id, editor.projectId, editor.sequenceId, 'export-page'],
    queryFn: ({ signal }) => timelineApi.get(editor.projectId, editor.sequenceId, signal),
    enabled: Boolean(session?.id && editor.projectId && editor.sequenceId)
  });
  const [presetId, setPresetId] = useState<SequenceExportPresetId>('program-native');
  const [width, setWidth] = useState(1920);
  const [height, setHeight] = useState(1080);
  const [codec, setCodec] = useState<'h264' | 'hevc'>('h264');
  const [rateMode, setRateMode] = useState<'crf' | 'bitrate'>('crf');
  const [crf, setCrf] = useState(20);
  const [bitrate, setBitrate] = useState('8M');
  const [subtitles, setSubtitles] = useState<'burn' | 'sidecar'>('burn');
  const [loudness, setLoudness] = useState<'' | '-14' | '-16'>('');
  const [threads, setThreads] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [failure, setFailure] = useState('');
  const view = timeline.data;
  const sequence = view?.sequence;
  const preset = SEQUENCE_EXPORT_PRESETS.find(item => item.id === presetId);
  const native = presetId === 'program-native';

  const exportSequence = async () => {
    if (!view || !session || !editor.projectId || busy) return;
    setFailure('');
    setNotice('');
    let path: string | undefined;
    if (isDesktop()) path = (await desktopCommand<string[]>('project_dialog', { mode: 'save-video', defaultPath: `${sequence?.name ?? '序列'}.mp4` }))[0];
    else path = window.prompt('导出 MP4 的绝对路径') ?? undefined;
    if (!path) return;
    const request = sequenceExportRequest({
      presetId, width, height, codec, rateMode, crf, bitrate, subtitles, loudness,
      threads: threads ?? undefined
    });
    setBusy(true);
    try {
      const exported = await timelineApi.export(editor.projectId, editor.sequenceId, {
        session: session.id,
        expected_revision: view.sequence.revision,
        content_version: view.content_version,
        path,
        ...(request ? { export: request } : {})
      });
      const sidecar = exported.subtitle_sidecar ? `，字幕 ${exported.subtitle_sidecar}` : '';
      setNotice(`已导出 ${exported.width}×${exported.height}${sidecar}`);
    } catch (reason) {
      setFailure(errorText(reason));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card size="small" title="序列成片">
      <Form layout="vertical" requiredMark={false}>
        <Typography.Paragraph type="secondary">
          {sequence ? `${sequence.name} · ${sequence.width}×${sequence.height}` : '打开工程并进入序列后可以导出成片。'}
          {preset ? ` ${preset.note}` : ''}
        </Typography.Paragraph>
        <Form.Item label="预设">
          <ValueSelect<SequenceExportPresetId> aria-label="导出预设" value={presetId} onChange={setPresetId}
            options={SEQUENCE_EXPORT_PRESETS.map(item => ({ value: item.id, label: item.label }))} />
        </Form.Item>
        {!native && <>
          {presetId === 'custom' && (
            <Form.Item label="分辨率">
              <span>
                <InputNumber aria-label="导出宽度" min={2} max={7680} step={2} value={width} onChange={value => setWidth(value ?? 2)} />
                <Typography.Text type="secondary"> × </Typography.Text>
                <InputNumber aria-label="导出高度" min={2} max={7680} step={2} value={height} onChange={value => setHeight(value ?? 2)} />
              </span>
            </Form.Item>
          )}
          <Form.Item label="编码">
            <Radio.Group aria-label="视频编码" value={codec} onChange={event => setCodec(event.target.value)}>
              <Radio value="h264">H.264</Radio>
              <Radio value="hevc">HEVC</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item label="画质">
            <Radio.Group aria-label="码率控制" value={rateMode} onChange={event => setRateMode(event.target.value)}>
              <Radio value="crf">CRF</Radio>
              <Radio value="bitrate">码率</Radio>
            </Radio.Group>
            {rateMode === 'crf'
              ? <InputNumber aria-label="CRF" min={0} max={51} value={crf} onChange={value => setCrf(value ?? 20)} style={{ marginTop: 8 }} />
              : <ValueSelect<string> aria-label="视频码率" value={bitrate} onChange={setBitrate} style={{ marginTop: 8, width: 160 }}
                options={['4M', '8M', '12M', '20M', '40M'].map(value => ({ value, label: value }))} />}
          </Form.Item>
          <Form.Item label="字幕">
            <Radio.Group aria-label="字幕输出" value={subtitles} onChange={event => setSubtitles(event.target.value)}>
              <Radio value="burn">烧录到画面</Radio>
              <Radio value="sidecar">外挂 SRT</Radio>
            </Radio.Group>
          </Form.Item>
          <Form.Item label="响度">
            <ValueSelect<'' | '-14' | '-16'> aria-label="响度目标" value={loudness} onChange={setLoudness}
              options={[{ value: '', label: '不处理' }, { value: '-14', label: '-14 LUFS' }, { value: '-16', label: '-16 LUFS' }]} />
          </Form.Item>
          <Form.Item label="编码线程">
            <InputNumber aria-label="编码线程" min={1} max={16} value={threads ?? undefined} placeholder="自动"
              onChange={value => setThreads(value ?? null)} />
          </Form.Item>
        </>}
        <Button type="primary" className="vac-cta" disabled={!sequence || timeline.isFetching} loading={busy} onClick={() => void exportSequence()}>导出序列</Button>
      </Form>
      {notice && <Alert style={{ marginTop: 12 }} type="success" showIcon message={notice} />}
      {failure && <Alert style={{ marginTop: 12 }} type="error" showIcon message={failure} />}
    </Card>
  );
}
