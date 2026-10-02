import type { CSSProperties } from 'react';
import { Card, Empty, Typography, theme } from 'antd';

/**
 * 只读的时间线示意：按轨道把片段画在统一标尺上，点击片段把它送到监视器。
 * 所有编辑（选择、边界、拆分合并、排序、改稿）仍走各面板的表单并由服务端校验，
 * 时间线本身不提交任何数据。轨道渲染没有对应的 antd 组件，颜色取自 antd 令牌。
 */
export type ClipTone = 'video' | 'audio' | 'candidate' | 'selected' | 'ok' | 'warn' | 'bad' | 'muted';

export interface TimelineClip {
  id: string;
  start: number;
  end: number;
  label: string;
  tone: ClipTone;
  title?: string;
  active?: boolean;
  onSelect?: () => void;
}

export interface TimelineTrack {
  id: string;
  name: string;
  hint?: string;
  clips: TimelineClip[];
}

function niceStep(span: number, target = 8): number {
  if (!(span > 0)) return 1;
  const raw = span / target;
  const pow = 10 ** Math.floor(Math.log10(raw));
  const unit = [1, 2, 5, 10].find((m) => m * pow >= raw) ?? 10;
  return unit * pow;
}

export function Timeline({ label, duration, tracks, formatTick, caption, emptyText }: {
  label: string;
  duration: number;
  tracks: TimelineTrack[];
  formatTick?: (value: number) => string;
  caption?: string;
  emptyText?: string;
}) {
  const { token } = theme.useToken();
  const tones: Record<ClipTone, { background: string; color: string; borderColor: string }> = {
    video: { background: token.colorPrimary, color: token.colorTextLightSolid, borderColor: token.colorPrimary },
    selected: { background: token.colorPrimary, color: token.colorTextLightSolid, borderColor: token.colorPrimary },
    audio: { background: token.cyan6, color: token.colorTextLightSolid, borderColor: token.cyan6 },
    candidate: { background: token.colorFillSecondary, color: token.colorText, borderColor: token.colorBorder },
    ok: { background: token.colorSuccess, color: token.colorTextLightSolid, borderColor: token.colorSuccess },
    warn: { background: token.colorWarning, color: token.colorTextLightSolid, borderColor: token.colorWarning },
    bad: { background: token.colorError, color: token.colorTextLightSolid, borderColor: token.colorError },
    muted: { background: token.colorFillTertiary, color: token.colorTextSecondary, borderColor: token.colorBorderSecondary }
  };
  const span = duration > 0 ? duration : 1;
  const step = niceStep(span);
  const ticks: number[] = [];
  if (formatTick) for (let t = 0; t <= span + step / 1000; t += step) ticks.push(t);
  const hasClips = tracks.some((track) => track.clips.length > 0);
  const lane: CSSProperties = { background: token.colorFillQuaternary, borderColor: token.colorBorderSecondary };
  return (
    <Card size="small" className="timeline" aria-label={label} title={label}
      extra={caption && <Typography.Text type="secondary" className="timeline-caption" ellipsis={{ tooltip: caption }}>{caption}</Typography.Text>}>
      <div className="timeline-ruler" aria-hidden="true">
        <span className="timeline-gutter" />
        <div className="timeline-scale" style={{ borderColor: token.colorBorderSecondary }}>
          {ticks.map((t) => {
            const pct = Math.min(100, (t / span) * 100);
            // 靠近右端的刻度把文字放到刻度线左侧，避免越出轨道右边缘。
            return <span key={t} className={`timeline-tick${pct > 92 ? ' is-end' : ''}`} style={{ left: `${pct}%`, color: token.colorTextTertiary, borderColor: token.colorBorder }}>{formatTick!(t)}</span>;
          })}
        </div>
      </div>
      {tracks.map((track) => <div className="timeline-track" key={track.id}>
        <span className="timeline-gutter" title={track.hint}>
          <Typography.Text strong>{track.id}</Typography.Text> <Typography.Text type="secondary">{track.name}</Typography.Text>
        </span>
        <div className="timeline-lane" style={lane}>
          {track.clips.map((clip) => {
            const left = Math.max(0, Math.min(100, (clip.start / span) * 100));
            const width = Math.max(0.6, Math.min(100 - left, ((clip.end - clip.start) / span) * 100));
            const tone = tones[clip.tone];
            const style: CSSProperties = { left: `${left}%`, width: `${width}%`, ...tone, borderRadius: token.borderRadiusSM,
              boxShadow: clip.active ? `0 0 0 2px ${token.colorBgContainer}, 0 0 0 4px ${token.colorWarning}` : undefined };
            const className = `timeline-clip tone-${clip.tone}${clip.active ? ' is-active' : ''}`;
            return clip.onSelect
              ? <button key={clip.id} type="button" className={className} style={style} title={clip.title ?? clip.label}
                  aria-pressed={Boolean(clip.active)} onClick={clip.onSelect}><span>{clip.label}</span></button>
              : <span key={clip.id} className={className} style={style} title={clip.title ?? clip.label}><span>{clip.label}</span></span>;
          })}
        </div>
      </div>)}
      {!hasClips && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={emptyText ?? '暂无片段'} style={{ margin: '8px 0 0' }} />}
    </Card>
  );
}
