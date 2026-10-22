import { useEffect, useRef, useState, type RefObject } from 'react';

/** Compact program-side VU — gain feedback beside the preview, not a mixer. */
export function ProgramVu({ media }: { media: RefObject<HTMLVideoElement | null> }) {
  const [level, setLevel] = useState(0);
  const raf = useRef(0);
  const graph = useRef<{ ctx: AudioContext; analyser: AnalyserNode; source?: MediaElementAudioSourceNode } | null>(null);
  useEffect(() => {
    const el = media.current;
    if (!el) return;
    let cancelled = false;
    const ensure = async () => {
      try {
        if (!graph.current) {
          const ctx = new AudioContext();
          const analyser = ctx.createAnalyser();
          analyser.fftSize = 256;
          analyser.smoothingTimeConstant = 0.7;
          const source = ctx.createMediaElementSource(el);
          source.connect(analyser);
          analyser.connect(ctx.destination);
          graph.current = { ctx, analyser, source };
        }
        if (graph.current.ctx.state === 'suspended') await graph.current.ctx.resume();
      } catch {
        /* element may already be wired or autoplay-blocked */
      }
    };
    const tick = () => {
      if (cancelled) return;
      const g = graph.current;
      if (g) {
        const data = new Uint8Array(g.analyser.frequencyBinCount);
        g.analyser.getByteTimeDomainData(data);
        let peak = 0;
        for (const v of data) {
          const a = Math.abs(v - 128) / 128;
          if (a > peak) peak = a;
        }
        setLevel(peak);
      } else setLevel(0);
      raf.current = requestAnimationFrame(tick);
    };
    const onPlay = () => { void ensure().then(() => { if (!cancelled && !raf.current) raf.current = requestAnimationFrame(tick); }); };
    el.addEventListener('play', onPlay);
    if (!el.paused) onPlay();
    return () => {
      cancelled = true;
      el.removeEventListener('play', onPlay);
      if (raf.current) cancelAnimationFrame(raf.current);
      raf.current = 0;
    };
  }, [media]);
  const h = Math.max(2, Math.round(level * 100));
  return (
    <div className="program-vu" aria-hidden="true" title="节目电平">
      <div className="program-vu-bar" style={{ height: `${h}%`, background: level > 0.9 ? 'var(--vac-warning-text)' : 'var(--vac-accent)' }} />
    </div>
  );
}
