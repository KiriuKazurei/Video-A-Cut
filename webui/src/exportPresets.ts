import type { SequenceExportOptions } from './timelineContracts';

/** Sequence delivery presets. program-native copies the current program preview. mp4-720p-v1 is also the unchanged workflow preview contract; selecting it here re-encodes this sequence to that ceiling. */
export const SEQUENCE_EXPORT_PRESETS = [
  { id: 'program-native', label: '当前节目（与预览相同）', note: '不重新编码' },
  { id: 'mp4-720p-v1', label: '720p 横屏', note: '最大 1280×720，不放大' },
  { id: '1080p-landscape', label: '1080p 横屏', note: '1920×1080' },
  { id: '1080x1920-vertical', label: '1080×1920 竖屏', note: '竖屏成片' },
  { id: '2160p-4k', label: '4K 横屏', note: '3840×2160' },
  { id: 'custom', label: '自定义分辨率', note: '填写偶数宽高' }
] as const;

export type SequenceExportPresetId = (typeof SEQUENCE_EXPORT_PRESETS)[number]['id'];

export function sequenceExportRequest(input: {
  presetId: SequenceExportPresetId;
  width?: number;
  height?: number;
  codec: 'h264' | 'hevc';
  rateMode: 'crf' | 'bitrate';
  crf: number;
  bitrate: string;
  subtitles: 'burn' | 'sidecar';
  loudness: '' | '-14' | '-16';
  threads?: number;
}): SequenceExportOptions | undefined {
  if (input.presetId === 'program-native') return undefined;
  const request: SequenceExportOptions = {
    preset_id: input.presetId,
    codec: input.codec,
    subtitles: input.subtitles
  };
  if (input.presetId === 'custom') {
    request.width = input.width;
    request.height = input.height;
  }
  if (input.rateMode === 'bitrate') request.video_bitrate = input.bitrate.trim();
  else request.crf = input.crf;
  if (input.loudness) request.loudness = input.loudness;
  if (input.threads && input.threads > 0) request.threads = input.threads;
  return request;
}
