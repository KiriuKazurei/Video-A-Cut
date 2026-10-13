import { useCallback, useEffect, useRef, useState, type MutableRefObject } from 'react';
import { Button, Flex, Slider, Tag, Tooltip, Typography } from 'antd';
import { DownloadOutlined, AudioMutedOutlined, PauseCircleOutlined, PlayCircleOutlined, SoundOutlined } from '@ant-design/icons';
import type { TtsSettings } from '../ttsContracts';
import { ttsLivePlaybackRate, ttsLiveVolumeGain } from '../ttsContracts';
import { TtsWaveform, formatClock, playbarKeyToggle } from './TtsWaveform';

export interface TtsPlayerProps {
  url?: string;
  settings: TtsSettings;
  /** 是否与当前草稿配置一致（烘焙参数） */
  configCurrent: boolean;
  downloadHref?: string;
  audioRefExternal?: MutableRefObject<HTMLAudioElement | null>;
  onPlayBlocked?: (message: string) => void;
}

/**
 * TTS 自定义播放条：隐藏原生 controls，语速/音量推子经 playbackRate + GainNode 即时联动，不触发重新生成。
 * 音调仅在重新生成时烘焙（无实时移调）。
 */
export function TtsPlayer({ url, settings, configCurrent, downloadHref, audioRefExternal, onPlayBlocked }: TtsPlayerProps) {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const setAudioRef = useCallback((el: HTMLAudioElement | null) => {
    audioRef.current = el;
    if (audioRefExternal) audioRefExternal.current = el;
  }, [audioRefExternal]);

  const ctxRef = useRef<AudioContext | null>(null);
  const gainRef = useRef<GainNode | null>(null);
  const [playing, setPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [duration, setDuration] = useState(0);
  const [muted, setMuted] = useState(false);
  const [ready, setReady] = useState(false);

  const rate = ttsLivePlaybackRate(settings);
  const gain = ttsLiveVolumeGain(settings);

  // 每换一条音频重建 Web Audio 图（MediaElementSource 每元素仅一次）
  useEffect(() => {
    const el = audioRef.current;
    if (!el || !url) {
      setReady(false); setPlaying(false); setCurrentTime(0); setDuration(0);
      return;
    }
    let disposed = false;
    const ctx = new AudioContext();
    const source = ctx.createMediaElementSource(el);
    const g = ctx.createGain();
    source.connect(g);
    g.connect(ctx.destination);
    g.gain.value = muted ? 0 : gain;
    el.volume = 1;
    el.playbackRate = rate;
    ctxRef.current = ctx;
    gainRef.current = g;
    setReady(true);

    const onTime = () => setCurrentTime(el.currentTime);
    const onMeta = () => setDuration(Number.isFinite(el.duration) ? el.duration : 0);
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onEnded = () => setPlaying(false);
    el.addEventListener('timeupdate', onTime);
    el.addEventListener('loadedmetadata', onMeta);
    el.addEventListener('durationchange', onMeta);
    el.addEventListener('play', onPlay);
    el.addEventListener('pause', onPause);
    el.addEventListener('ended', onEnded);
    if (el.readyState >= 1) onMeta();

    return () => {
      disposed = true;
      el.removeEventListener('timeupdate', onTime);
      el.removeEventListener('loadedmetadata', onMeta);
      el.removeEventListener('durationchange', onMeta);
      el.removeEventListener('play', onPlay);
      el.removeEventListener('pause', onPause);
      el.removeEventListener('ended', onEnded);
      el.pause();
      gainRef.current = null;
      ctxRef.current = null;
      void ctx.close();
      if (!disposed) setReady(false);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- rate/gain applied in separate effects; muted applied there too
  }, [url]);

  // 推子 → 即时联动（不重新生成）
  useEffect(() => {
    const el = audioRef.current;
    if (el) el.playbackRate = rate;
  }, [rate, url]);

  useEffect(() => {
    if (gainRef.current) gainRef.current.gain.value = muted ? 0 : gain;
  }, [gain, muted, url]);

  const toggle = useCallback(async () => {
    const el = audioRef.current;
    if (!el || !url) return;
    try {
      if (ctxRef.current?.state === 'suspended') await ctxRef.current.resume();
      if (el.paused) await el.play();
      else el.pause();
    } catch {
      onPlayBlocked?.('无法自动播放，请再点一次播放。');
    }
  }, [url, onPlayBlocked]);

  const restart = useCallback(async () => {
    const el = audioRef.current;
    if (!el || !url) return;
    el.currentTime = 0;
    setCurrentTime(0);
    try {
      if (ctxRef.current?.state === 'suspended') await ctxRef.current.resume();
      await el.play();
    } catch {
      onPlayBlocked?.('请使用播放条上的播放按钮。');
    }
  }, [url, onPlayBlocked]);

  const seekRatio = (ratio: number) => {
    const el = audioRef.current;
    if (!el || !duration) return;
    el.currentTime = Math.min(duration, Math.max(0, ratio * duration));
    setCurrentTime(el.currentTime);
  };

  const seekSeconds = (sec: number) => {
    const el = audioRef.current;
    if (!el) return;
    el.currentTime = Math.min(duration || el.duration || 0, Math.max(0, sec));
    setCurrentTime(el.currentTime);
  };

  const progress = duration > 0 ? currentTime / duration : 0;
  const engineHint = settings.engine === 'sapi'
    ? `语速实时 ≈ ${rate.toFixed(2)}×（SAPI 等级 ${settings.sapi_rate} → 1+等级×0.1）`
    : `语速实时 ${rate.toFixed(2)}×`;

  if (!url) {
    return <div className="tts-player-panel tts-playbar is-empty" aria-label="TTS 试听播放条">
      <TtsWaveform />
      <Typography.Text type="secondary">尚无试听音频。生成成功后可在此播放；推子将即时调节语速与音量。</Typography.Text>
    </div>;
  }

  return (
    <div
      className="tts-player-panel tts-playbar"
      aria-label="TTS 试听播放条"
      tabIndex={0}
      onKeyDown={e => playbarKeyToggle(e, () => { void toggle(); })}
    >
      <Flex gap={8} wrap align="center" className="tts-playbar-meta">
        <Tag color="processing" bordered={false}>实时推子</Tag>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>{engineHint}</Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          音量增益 {gain.toFixed(2)}×{gain > 1 ? '（Web Audio，可超过 1）' : ''}
        </Typography.Text>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>音调仅在重新生成时烘焙</Typography.Text>
        {!configCurrent && <Tag color="warning">旧配置试听 · 推子仍即时生效</Tag>}
      </Flex>

      <TtsWaveform url={url} progress={progress} interactive onSeek={seekRatio} />

      <audio ref={setAudioRef} key={url} src={url} preload="metadata" aria-hidden="true" style={{ display: 'none' }} />

      <Flex gap={10} align="center" wrap className="tts-playbar-transport">
        <Button
          type="primary"
          shape="default"
          icon={playing ? <PauseCircleOutlined /> : <PlayCircleOutlined />}
          aria-label={playing ? '暂停' : '播放'}
          onClick={() => { void toggle(); }}
          disabled={!ready}
        />
        <Button aria-label="从头播放" onClick={() => { void restart(); }} disabled={!ready}>从头</Button>
        <Typography.Text className="tts-playbar-clock" aria-live="off">
          {formatClock(currentTime)} / {formatClock(duration)}
        </Typography.Text>
        <div className="tts-playbar-scrub">
          <Slider
            aria-label="播放进度"
            min={0}
            max={Math.max(duration, 0.001)}
            step={0.01}
            value={Math.min(currentTime, duration || 0)}
            disabled={!duration}
            tooltip={{ formatter: v => formatClock(Number(v ?? 0)) }}
            onChange={v => seekSeconds(Number(v))}
          />
        </div>
        <Tooltip title={muted ? '取消静音' : '静音'}>
          <Button
            icon={muted ? <AudioMutedOutlined /> : <SoundOutlined />}
            type={muted ? 'default' : 'text'}
            danger={muted}
            aria-label={muted ? '取消静音' : '静音'}
            aria-pressed={muted}
            onClick={() => setMuted(m => !m)}
            className={muted ? 'is-muted' : undefined}
            data-testid="tts-mute"
          />
        </Tooltip>
        {downloadHref && (
          <Button icon={<DownloadOutlined />} href={downloadHref}>下载 WAV</Button>
        )}
      </Flex>
      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
        拖动右侧语速/音量推子会立刻改变当前播放，不会重新调用生成。空格键（焦点在播放条内）切换播放/暂停。
      </Typography.Text>
    </div>
  );
}

export default TtsPlayer;
