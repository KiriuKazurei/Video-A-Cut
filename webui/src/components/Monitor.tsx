import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { Card, Empty, Flex, Slider, Tag, Typography } from 'antd';
import { SoundOutlined } from '@ant-design/icons';

/**
 * 监视器（Premiere 的源/节目监视器）。只播放服务端受控 URL（文件键、证据键），
 * 从不拼接本机路径；字幕叠加只是审查示意，不代表导出成片。
 */
export type PreviewSource = {
  kind: 'video' | 'audio' | 'image';
  src: string;
  title: string;
  /** 右上角的简短元数据，如时间范围、文件大小。 */
  meta?: string;
  /** 叠加在画面底部的文字（解说草稿示意）。 */
  caption?: string;
  /** 画面下方的说明，例如“取样证据帧，非成片”。 */
  note?: string;
} | null;

const PreviewContext = createContext<(preview: PreviewSource) => void>(() => {});

/** 页面内任意组件把媒体送到本页监视器。 */
export const usePreview = () => useContext(PreviewContext);

export function PreviewScope({ onPreview, children }: { onPreview: (preview: PreviewSource) => void; children: ReactNode }) {
  return <PreviewContext.Provider value={onPreview}>{children}</PreviewContext.Provider>;
}

export function formatSeconds(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '00:00:00.000';
  const ms = Math.floor((value % 1) * 1000);
  const total = Math.floor(value);
  const h = Math.floor(total / 3600), m = Math.floor(total / 60) % 60, s = total % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}.${String(ms).padStart(3, '0')}`;
}

export function Monitor({ label, preview, emptyTitle, emptyHint, compact, children }: {
  label: string;
  preview: PreviewSource;
  emptyTitle: string;
  emptyHint: string;
  /** 吸顶等场景使用较矮的画面区域。 */
  compact?: boolean;
  /** 监视器下方的附加控制（如选择预览文件）。 */
  children?: ReactNode;
}) {
  const [time, setTime] = useState(0);
  const [duration, setDuration] = useState<number | null>(null);
  const [volume, setVolume] = useState(100);
  const audioRef = useRef<HTMLAudioElement | null>(null);
  useEffect(() => { setTime(0); setDuration(null); }, [preview?.src]);
  useEffect(() => { if (audioRef.current) audioRef.current.volume = volume / 100; }, [volume, preview?.src]);
  const timed = preview?.kind === 'video' || preview?.kind === 'audio';
  const note = preview?.note ?? (preview?.kind === 'audio' ? '音量仅作用于本次浏览器试听，不改写交付文件' : '');
  return (
    <Card size="small" className="monitor glass glass-window" aria-label={`${label}监视器`} styles={{ body: { padding: 0 } }}
      title={<Flex gap={8} align="center" style={{ minWidth: 0 }}>
        <Tag color="processing" bordered={false} style={{ marginInlineEnd: 0 }}>{label}</Tag>
        <Typography.Text strong ellipsis={{ tooltip: preview?.title }} className="monitor-title">{preview?.title ?? '无媒体'}</Typography.Text>
      </Flex>}
      extra={preview?.meta && <Typography.Text type="secondary" className="monitor-meta" ellipsis>{preview.meta}</Typography.Text>}>
      <div className={`monitor-screen${preview ? ' has-media' : ''}${compact ? ' is-compact' : ''}`}>
        {!preview && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE}
          description={<><Typography.Text strong>{emptyTitle}</Typography.Text><br /><Typography.Text type="secondary">{emptyHint}</Typography.Text></>} />}
        {preview?.kind === 'video' && <video key={preview.src} className="monitor-media" controls preload="metadata" src={preview.src}
          aria-label={`预览 ${preview.title}`}
          onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
          onLoadedMetadata={(e) => setDuration(Number.isFinite(e.currentTarget.duration) ? e.currentTarget.duration : null)} />}
        {preview?.kind === 'image' && <img key={preview.src} className="monitor-media" src={preview.src} alt={preview.title} />}
        {preview?.kind === 'audio' && <Flex vertical align="center" gap={12} className="monitor-audio">
          <SoundOutlined style={{ fontSize: 40, color: '#fff' }} />
          <audio key={preview.src} ref={audioRef} controls preload="metadata" src={preview.src} aria-label={`试听 ${preview.title}`}
            onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
            onLoadedMetadata={(e) => setDuration(Number.isFinite(e.currentTarget.duration) ? e.currentTarget.duration : null)} />
        </Flex>}
        {preview?.caption && <p className="monitor-caption">{preview.caption}</p>}
      </div>
      {preview?.kind === 'audio' && <Flex align="center" gap={12} style={{ padding: '8px 12px 0' }}>
        <Typography.Text type="secondary">试听音量</Typography.Text>
        <Slider style={{ flex: 1 }} min={0} max={100} value={volume} onChange={setVolume} aria-label="试听音量" tooltip={{ formatter: (v) => `${v}%` }} />
      </Flex>}
      <Flex justify="space-between" align="center" gap={12} className="monitor-transport">
        <Typography.Text code aria-label="当前时间">{timed ? formatSeconds(time) : '--:--:--.---'}</Typography.Text>
        <Typography.Text type="secondary" ellipsis className="monitor-note">{note}</Typography.Text>
        <Typography.Text code type="secondary" aria-label="媒体时长">{timed && duration !== null ? formatSeconds(duration) : '--:--:--.---'}</Typography.Text>
      </Flex>
      {children && <div style={{ padding: '0 12px 12px' }}>{children}</div>}
    </Card>
  );
}
