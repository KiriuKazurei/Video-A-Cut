import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { Icon } from './Icon';

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

export function Monitor({ label, preview, emptyTitle, emptyHint, children }: {
  label: string;
  preview: PreviewSource;
  emptyTitle: string;
  emptyHint: string;
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
  return (
    <section className="monitor" aria-label={`${label}监视器`}>
      <header className="monitor-head">
        <span className="monitor-tag">{label}</span>
        <strong className="monitor-title" title={preview?.title}>{preview?.title ?? '无媒体'}</strong>
        {preview?.meta && <span className="monitor-meta">{preview.meta}</span>}
      </header>
      <div className="monitor-screen">
        {!preview && <div className="monitor-empty">
          <Icon name="film" size={36} />
          <strong>{emptyTitle}</strong>
          <p>{emptyHint}</p>
        </div>}
        {preview?.kind === 'video' && <video key={preview.src} className="monitor-media" controls preload="metadata" src={preview.src}
          aria-label={`预览 ${preview.title}`}
          onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
          onLoadedMetadata={(e) => setDuration(Number.isFinite(e.currentTarget.duration) ? e.currentTarget.duration : null)} />}
        {preview?.kind === 'image' && <img key={preview.src} className="monitor-media" src={preview.src} alt={preview.title} />}
        {preview?.kind === 'audio' && <div className="monitor-audio">
          <Icon name="audio" size={44} />
          <audio key={preview.src} ref={audioRef} controls preload="metadata" src={preview.src} aria-label={`试听 ${preview.title}`}
            onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
            onLoadedMetadata={(e) => setDuration(Number.isFinite(e.currentTarget.duration) ? e.currentTarget.duration : null)} />
          <label className="monitor-volume">试听音量 {volume}%
            <input type="range" min="0" max="100" value={volume} onChange={(e) => setVolume(Number(e.target.value))} /></label>
        </div>}
        {preview?.caption && <p className="monitor-caption">{preview.caption}</p>}
      </div>
      <footer className="monitor-transport">
        <span className="timecode" aria-label="当前时间">{timed ? formatSeconds(time) : '--:--:--.---'}</span>
        <span className="monitor-note">{preview?.note ?? (preview?.kind === 'audio' ? '音量仅作用于本次浏览器试听，不改写交付文件' : '')}</span>
        <span className="timecode timecode-dim" aria-label="媒体时长">{timed && duration !== null ? formatSeconds(duration) : '--:--:--.---'}</span>
      </footer>
      {children && <div className="monitor-extra">{children}</div>}
    </section>
  );
}
