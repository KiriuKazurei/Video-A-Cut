import { useState, type CSSProperties } from 'react';
import { Button, Card, Empty, Tooltip, Typography, theme } from 'antd';
import {
  DownOutlined,
  UpOutlined,
  VerticalAlignMiddleOutlined
} from '@ant-design/icons';
import { useResizable } from '../hooks/useResizable';

/**
 * 时间线示意：按轨道把片段画在统一标尺上。
 * 单击片段 → 聚焦并送到源监视器；双击或 Ctrl/⌘+单击 → 加入/移出选择版本（回调仍走现有 pick API）。
 * 边界拆分合并等仍走下方表单并由服务端校验。轨道渲染没有对应的 antd 组件，颜色取自 antd 令牌。
 *
 * Phase 3 增强：
 * - 支持高度可拖拽调整 (160px ~ 500px)，双击还原默认 260px
 * - 支持折叠/最小化至 42px，记忆在 localStorage
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
  /** 单击 / Enter：聚焦并送到监视器 */
  onSelect?: () => void;
  /** 双击 / Ctrl·⌘+单击：加入或移出选择版本（仍走现有 pick API） */
  onToggle?: () => void;
  /** 已在选择版本中（候选轨上显示对勾提示） */
  picked?: boolean;
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
  const [isCollapsed, setIsCollapsed] = useState<boolean>(() => {
    if (typeof window !== 'undefined') {
      return window.localStorage.getItem('vac_timeline_collapsed') === 'true';
    }
    return false;
  });

  const {
    size: height,
    isDragging,
    startDrag,
    resetToDefault
  } = useResizable({
    initial: 260,
    min: 160,
    max: 500,
    direction: 'vertical',
    storageKey: 'vac_timeline_height',
    defaultSize: 260
  });

  const toggleCollapse = () => {
    setIsCollapsed((prev) => {
      const next = !prev;
      if (typeof window !== 'undefined') {
        window.localStorage.setItem('vac_timeline_collapsed', String(next));
      }
      return next;
    });
  };

  const tones: Record<ClipTone, { background: string; color: string; borderColor: string }> = {
    video: { background: 'var(--vac-clip-video)', color: 'var(--vac-on-media)', borderColor: 'var(--vac-clip-video)' },
    selected: { background: 'var(--vac-clip-video)', color: 'var(--vac-on-media)', borderColor: 'var(--vac-clip-video)' },
    audio: { background: 'var(--vac-status-cyan)', color: 'var(--vac-on-media)', borderColor: 'var(--vac-status-cyan)' },
    candidate: { background: token.colorFillSecondary, color: token.colorText, borderColor: token.colorBorder },
    ok: { background: 'var(--vac-success)', color: 'var(--vac-on-media)', borderColor: 'var(--vac-success)' },
    warn: { background: 'var(--vac-warning)', color: 'var(--vac-on-media)', borderColor: 'var(--vac-warning)' },
    bad: { background: 'var(--vac-danger)', color: 'var(--vac-on-media)', borderColor: 'var(--vac-danger)' },
    muted: { background: token.colorFillTertiary, color: token.colorTextSecondary, borderColor: token.colorBorderSecondary }
  };

  const span = duration > 0 ? duration : 1;
  const step = niceStep(span);
  const ticks: number[] = [];
  if (formatTick) for (let t = 0; t <= span + step / 1000; t += step) ticks.push(t);
  const hasClips = tracks.some((track) => track.clips.length > 0);
  const lane: CSSProperties = { background: token.colorFillQuaternary, borderColor: token.colorBorderSecondary };

  const extraContent = (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
      {caption && (
        <Typography.Text type="secondary" className="timeline-caption" ellipsis={{ tooltip: caption }}>
          {caption}
        </Typography.Text>
      )}
      <Tooltip title="双击重置默认高度 (260px)">
        <Button
          type="text"
          size="small"
          icon={<VerticalAlignMiddleOutlined />}
          onClick={resetToDefault}
          style={{ fontSize: 12, padding: '0 4px' }}
        />
      </Tooltip>
      <Tooltip title={isCollapsed ? '展开时间线' : '最小化时间线 (42px)'}>
        <Button
          type="text"
          size="small"
          icon={isCollapsed ? <DownOutlined /> : <UpOutlined />}
          onClick={toggleCollapse}
          style={{ fontSize: 12, padding: '0 4px' }}
        />
      </Tooltip>
    </div>
  );

  return (
    <div
      className="timeline-resizable-container"
      style={{
        height: isCollapsed ? 42 : height
      }}
    >
      {/* 顶部高度拖拽手柄 */}
      {!isCollapsed && (
        <div
          className={`timeline-resizer-bar ${isDragging ? 'is-dragging' : ''}`}
          onMouseDown={startDrag}
          onDoubleClick={resetToDefault}
          title="上下拖拽调整时间线高度，双击重置 (260px)"
          role="separator"
          aria-label="时间线高度调整手柄"
        />
      )}

      <Card
        size="small"
        className="timeline"
        aria-label={label}
        title={label}
        extra={extraContent}
        style={{
          height: '100%',
          display: 'flex',
          flexDirection: 'column'
        }}
        bodyStyle={{
          flex: 1,
          overflowY: 'auto',
          display: isCollapsed ? 'none' : 'block',
          padding: 8
        }}
      >
        <div className="timeline-ruler" aria-hidden="true">
          <span className="timeline-gutter" />
          <div className="timeline-scale" style={{ borderColor: token.colorBorderSecondary }}>
            {ticks.map((t) => {
              const pct = Math.min(100, (t / span) * 100);
              return (
                <span
                  key={t}
                  className={`timeline-tick${pct > 92 ? ' is-end' : ''}`}
                  style={{ left: `${pct}%`, color: token.colorTextTertiary, borderColor: token.colorBorder }}
                >
                  {formatTick!(t)}
                </span>
              );
            })}
          </div>
        </div>

        {tracks.map((track) => (
          <div className="timeline-track" key={track.id}>
            <span className="timeline-gutter" title={track.hint}>
              <Typography.Text strong>{track.id}</Typography.Text>{' '}
              <Typography.Text type="secondary">{track.name}</Typography.Text>
            </span>
            <div className="timeline-lane" style={lane}>
              {track.clips.map((clip) => {
                const left = Math.max(0, Math.min(100, (clip.start / span) * 100));
                const width = Math.max(0.6, Math.min(100 - left, ((clip.end - clip.start) / span) * 100));
                const tone = tones[clip.tone];
                const style: CSSProperties = {
                  left: `${left}%`,
                  width: `${width}%`,
                  ...tone,
                  borderRadius: token.borderRadiusSM
                };
                const className = `timeline-clip tone-${clip.tone}${clip.active ? ' is-active' : ''}${clip.picked ? ' is-picked' : ''}`;
                const tip = [
                  clip.title ?? clip.label,
                  clip.onToggle ? (clip.picked ? '双击或 Ctrl/⌘+单击：移出已选' : '双击或 Ctrl/⌘+单击：加入已选') : undefined
                ].filter(Boolean).join(' · ');
                if (!clip.onSelect && !clip.onToggle) {
                  return (
                    <span key={clip.id} className={className} style={style} title={tip}>
                      <span>{clip.label}</span>
                    </span>
                  );
                }
                return (
                  <button
                    key={clip.id}
                    type="button"
                    className={className}
                    style={style}
                    title={tip}
                    aria-pressed={Boolean(clip.active)}
                    aria-label={`${clip.label}${clip.picked ? '（已选）' : ''}${clip.active ? '，当前焦点' : ''}`}
                    data-clip-id={clip.id}
                    onClick={(event) => {
                      if ((event.metaKey || event.ctrlKey) && clip.onToggle) {
                        event.preventDefault();
                        clip.onToggle();
                        return;
                      }
                      clip.onSelect?.();
                    }}
                    onDoubleClick={(event) => {
                      if (!clip.onToggle) return;
                      event.preventDefault();
                      clip.onToggle();
                    }}
                  >
                    <span>{clip.label}</span>
                    {clip.picked && <span className="timeline-clip-mark" aria-hidden="true">✓</span>}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
        {!hasClips && !isCollapsed && (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={emptyText ?? '暂无片段'}
            style={{ margin: '8px 0 0' }}
          />
        )}
      </Card>
    </div>
  );
}
