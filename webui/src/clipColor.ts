import { CLIP_BROWN, CLIP_INDIGO, CLIP_MIST, CLIP_ROSE, CLIP_STEEL, CLIP_VIDEO } from './theme.ts';

export { CLIP_VIDEO };

/** Named video-clip presets. Default blue is stored as an empty color so the bar follows --vac-clip-video. */
export const VIDEO_CLIP_PRESETS = [
  { id: 'default', label: '默认蓝', hex: CLIP_VIDEO },
  { id: 'indigo', label: '靛', hex: CLIP_INDIGO },
  { id: 'steel', label: '钢', hex: CLIP_STEEL },
  { id: 'brown', label: '棕', hex: CLIP_BROWN },
  { id: 'rose', label: '玫瑰', hex: CLIP_ROSE },
  { id: 'mist', label: '雾紫', hex: CLIP_MIST },
] as const;

/** Accept #RRGGBB, or #RRGGBBAA with an opaque alpha. Anything else is not a clip color. */
export function normalizeClipHex(value: string): string | null {
  let hex = value.trim();
  if (hex.startsWith('#')) hex = hex.slice(1);
  if (/^[0-9a-fA-F]{8}$/.test(hex) && hex.slice(6).toLowerCase() === 'ff') hex = hex.slice(0, 6);
  if (!/^[0-9a-fA-F]{6}$/.test(hex)) return null;
  return `#${hex.toUpperCase()}`;
}

export function presetSelected(stored: string | undefined, hex: string): boolean {
  const current = normalizeClipHex(stored ?? '');
  if (hex === CLIP_VIDEO) return !current || current === CLIP_VIDEO;
  return current === hex;
}

/** Timeline bar fill. Custom color applies only to video clips. Code never uses the accent. */
export function clipBarFill(kind: string, actor: string, color?: string): string {
  if (kind === 'code') return 'var(--vac-clip-code)';
  if (kind === 'audio') return 'var(--vac-clip-audio)';
  if (kind === 'subtitle') return 'var(--vac-clip-subtitle)';
  const custom = kind === 'video' ? normalizeClipHex(color ?? '') : null;
  if (custom) return custom;
  if (actor.startsWith('agent')) return 'var(--vac-clip-effect)';
  return 'var(--vac-clip-video)';
}
