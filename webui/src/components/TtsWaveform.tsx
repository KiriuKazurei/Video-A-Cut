import { useEffect, useState } from 'react';

/** Decode the actual local WAV, never draw a fabricated audio waveform. */
export function TtsWaveform({ url }: { url?: string }) {
  const [peaks, setPeaks] = useState<number[]>([]);
  const [error, setError] = useState('');
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
  return <div className="tts-waveform">
    {peaks.length ? <svg viewBox="0 0 600 120" role="img" aria-label="试听音频实际波形" preserveAspectRatio="none">
      {peaks.map((v, i) => <rect key={i} x={i * 5 + 1} y={60 - Math.max(1, v * 54)} width="3" height={Math.max(2, v * 108)} rx="1" />)}
    </svg> : <span>{error || (url ? '正在读取音频波形…' : '生成试听后显示音频波形')}</span>}
  </div>;
}
