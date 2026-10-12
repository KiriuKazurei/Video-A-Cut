import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react';

/** Decode the actual local WAV, never draw a fabricated audio waveform. */
export function TtsWaveform({
  url,
  progress = 0,
  onSeek,
  interactive = false
}: {
  url?: string;
  /** 0–1 playhead position */
  progress?: number;
  onSeek?: (ratio: number) => void;
  interactive?: boolean;
}) {
  const [peaks, setPeaks] = useState<number[]>([]);
  const [error, setError] = useState('');
  const svgRef = useRef<SVGSVGElement>(null);

  useEffect(() => {
    setPeaks([]); setError('');
    if (!url) return;
    const abort = new AbortController(); let context: AudioContext | undefined;
    void (async () => {
      try {
        const response = await fetch(url, { signal: abort.signal });
        if (!response.ok) throw new Error('音频读取失败');
        const data = await response.arrayBuffer();
        if (abort.signal.aborted) return;
        context = new AudioContext();
        const buffer = await context.decodeAudioData(data);
        const samples = buffer.getChannelData(0), count = 120, stride = Math.max(1, Math.floor(samples.length / count));
        const values = Array.from({ length: count }, (_, i) => {
          let peak = 0;
          for (let j = i * stride; j < Math.min(samples.length, (i + 1) * stride); j++) peak = Math.max(peak, Math.abs(samples[j]));
          return peak;
        });
        if (!abort.signal.aborted) setPeaks(values);
      } catch { if (!abort.signal.aborted) setError('波形暂不可用；仍可使用播放器试听。'); }
      finally { if (context && context.state !== 'closed') await context.close(); }
    })();
    return () => { abort.abort(); };
  }, [url]);

  const seekFromClientX = (clientX: number) => {
    if (!onSeek || !svgRef.current) return;
    const box = svgRef.current.getBoundingClientRect();
    if (box.width <= 0) return;
    const ratio = Math.min(1, Math.max(0, (clientX - box.left) / box.width));
    onSeek(ratio);
  };

  const onPointerDown = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (!interactive || !onSeek) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    seekFromClientX(e.clientX);
  };
  const onPointerMove = (e: ReactPointerEvent<SVGSVGElement>) => {
    if (!interactive || !onSeek || !e.currentTarget.hasPointerCapture(e.pointerId)) return;
    seekFromClientX(e.clientX);
  };

  const playheadX = Math.min(599, Math.max(0, progress * 600));

  return <div className={`tts-waveform${interactive && peaks.length ? ' is-interactive' : ''}`}>
    {peaks.length ? <svg ref={svgRef} viewBox="0 0 600 120" role="img" aria-label="试听音频实际波形"
      preserveAspectRatio="none"
      style={interactive ? { cursor: 'pointer' } : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={e => { try { e.currentTarget.releasePointerCapture(e.pointerId); } catch { /* */ } }}
    >
      {peaks.map((v, i) => <rect key={i} x={i * 5 + 1} y={60 - Math.max(1, v * 54)} width="3" height={Math.max(2, v * 108)} rx="1" />)}
      <line className="tts-playhead" x1={playheadX} x2={playheadX} y1="0" y2="120" />
    </svg> : <span>{error || (url ? '正在读取音频波形…' : '生成试听后显示音频波形')}</span>}
  </div>;
}

export function formatClock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00';
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}:${s.toString().padStart(2, '0')}`;
}

/** Space 在播放条区域切换播放，不拦截输入框。 */
export function playbarKeyToggle(e: KeyboardEvent, toggle: () => void) {
  if (e.code !== 'Space' && e.key !== ' ') return;
  const t = e.target as HTMLElement;
  if (t.closest('input, textarea, [contenteditable=true], button, a, .ant-select, .ant-slider')) return;
  e.preventDefault();
  toggle();
}
