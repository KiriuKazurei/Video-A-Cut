export interface TtsSettings {
  engine: 'sapi' | 'stepfun';
  endpoint?: string;
  model?: string;
  token_env?: string;
  voice: string;
  language: 'zh' | 'en' | 'ja' | 'ko' | 'fr' | 'es';
  speed: number;
  volume: number;
  pitch_semitones: number;
  sapi_rate: number;
  instruction: string;
}
export interface TtsTemplate {
  template_id: string; revision: number; name: string; settings: TtsSettings;
  reference_text: string; sha256: string; archived: boolean; created_at: string;
}
export interface TtsBinding {
  template_id: string; revision: number; sha256?: string; settings?: TtsSettings;
}
export interface TtsCapabilities {
  runtime_ready: boolean; media_ready: boolean; pitch_available: boolean; credentials_ready: boolean;
  sapi_voices: string[]; stepfun_voices: { id: string; name: string }[]; message?: string;
}
export interface TtsPreview {
  job_id: string; input_sha256: string; status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled';
  text: string; settings: TtsSettings; message?: string; duration_seconds: number; size_bytes: number;
  expired: boolean; created_at: string; updated_at: string;
}
export const DEFAULT_TTS_SETTINGS: TtsSettings = {
  engine: 'stepfun', endpoint: 'https://api.stepfun.com/step_plan/v1', model: 'stepaudio-2.5-tts',
  token_env: 'VAC_TTS_TOKEN', voice: 'vibrant-youth', language: 'zh',
  speed: 1, volume: 1, pitch_semitones: 0, sapi_rate: 0, instruction: ''
};
export const TTS_MOODS = [
  { label: '中性', instruction: '' },
  { label: '温柔', instruction: '温柔、亲切的语气，表达自然。' },
  { label: '轻松', instruction: '轻松、愉快地讲述，像和朋友交谈。' },
  { label: '激昂', instruction: '充满热情、激昂有力地讲述。' },
  { label: '严肃', instruction: '严肃、沉稳地讲述，吐字清晰。' }
] as const;
export function ttsInstruction(mood: number, custom: string) {
  return [TTS_MOODS[mood]?.instruction ?? '', custom.trim()].filter(Boolean).join('\n');
}
// Explicit field order makes snapshots returned by Go comparable to local drafts.
export function ttsDraftKey(text: string, s: TtsSettings) {
  return JSON.stringify([text, s.engine, s.endpoint ?? '', s.model ?? '', s.token_env ?? '', s.voice,
    s.language, s.speed, s.volume, s.pitch_semitones, s.sapi_rate, s.instruction]);
}
export function currentTtsPreview(p: TtsPreview | undefined, text: string, s: TtsSettings) {
  return !!p && !p.expired && p.status === 'succeeded' && ttsDraftKey(p.text, p.settings) === ttsDraftKey(text, s);
}
export function ttsDraftError(text: string, s: TtsSettings, requireText = true): string {
  if ((requireText && !text.trim()) || Array.from(text).length > 1000 || text.includes('\0')) return '文本须为 1–1,000 字符';
  if (Array.from(s.instruction).length > 200) return '合并后的表达指令超过 200 字符';
  if (!s.voice.trim()) return '请选择音色';
  if (s.engine === 'stepfun' && (!s.endpoint?.trim() || !s.token_env?.trim())) return '请填写服务地址和凭证变量';
  return '';
}

/** 实时试听语速：StepFun speed 直接映射；SAPI rate（−10…10）按约 10%/级近似为 playbackRate。 */
export function ttsLivePlaybackRate(s: TtsSettings): number {
  if (s.engine === 'sapi') {
    return Math.min(2, Math.max(0.5, 1 + s.sapi_rate * 0.1));
  }
  return Math.min(2, Math.max(0.5, s.speed));
}

/** 实时试听音量增益（可 >1，由 Web Audio GainNode 施加；元素自身 volume 固定为 1）。 */
export function ttsLiveVolumeGain(s: TtsSettings): number {
  return Math.min(2, Math.max(0, s.volume));
}
