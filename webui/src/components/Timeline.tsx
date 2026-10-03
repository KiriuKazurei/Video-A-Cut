/**
 * 只读的时间线示意：按轨道把片段画在统一标尺上，点击片段把它送到监视器。
 * 所有编辑（选择、边界、拆分合并、排序、改稿）仍走各面板的表单并由服务端校验，
 * 时间线本身不提交任何数据。
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
  const span = duration > 0 ? duration : 1;
  const step = niceStep(span);
  const ticks: number[] = [];
  if (formatTick) for (let t = 0; t <= span + step / 1000; t += step) ticks.push(t);
  const hasClips = tracks.some((track) => track.clips.length > 0);
  return (
    <section className="timeline" aria-label={label}>
      <header className="timeline-head">
        <strong>{label}</strong>
        {caption && <span className="timeline-caption">{caption}</span>}
      </header>
      <div className="timeline-body">
        <div className="timeline-ruler" aria-hidden="true">
          <span className="timeline-gutter" />
          <div className="timeline-scale">
            {ticks.map((t) => <span key={t} className="timeline-tick" style={{ left: `${(t / span) * 100}%` }}>{formatTick!(t)}</span>)}
          </div>
        </div>
        {tracks.map((track) => <div className="timeline-track" key={track.id}>
          <span className="timeline-gutter" title={track.hint}><b>{track.id}</b>{track.name}</span>
          <div className="timeline-lane">
            {track.clips.map((clip) => {
              const left = Math.max(0, Math.min(100, (clip.start / span) * 100));
              const width = Math.max(0.6, Math.min(100 - left, ((clip.end - clip.start) / span) * 100));
              const style = { left: `${left}%`, width: `${width}%` };
              const className = `timeline-clip tone-${clip.tone}${clip.active ? ' is-active' : ''}`;
              return clip.onSelect
                ? <button key={clip.id} type="button" className={className} style={style} title={clip.title ?? clip.label}
                    aria-pressed={Boolean(clip.active)} onClick={clip.onSelect}><span>{clip.label}</span></button>
                : <span key={clip.id} className={className} style={style} title={clip.title ?? clip.label}><span>{clip.label}</span></span>;
            })}
          </div>
        </div>)}
        {!hasClips && <p className="timeline-empty">{emptyText ?? '暂无片段'}</p>}
      </div>
    </section>
  );
}
