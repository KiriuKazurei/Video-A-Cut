export interface MediaTime { value: number; scale: number }
export interface FrameRate { num: number; den: number }
export interface TimelineMarker { id: string; notes: string; kind: 'point' | 'in' | 'out'; frame: number; source_time?: MediaTime; locked: boolean }
export interface EditOrigin { actor: string; run_id?: string; artifact_id?: string; sha256?: string; plan_version?: number }
export interface GainKeyframe { t: number; db: number }
export interface ClipEffects { gain_db: number; gain_keyframes?: GainKeyframe[]; fade_in: number; fade_out: number; fade_color?: 'black' | 'white' | ''; scale: number; x: number; y: number; crop_left: number; crop_right: number; crop_top: number; crop_bottom: number; transition: number; from_frame?: number; to_frame?: number }
export interface TimelineClip { id: string; resource_id: string; name: string; stream_index: number; at: number; duration: number; source_in: MediaTime; source_out: MediaTime; speed: number; link_id?: string; origin: EditOrigin; effects: ClipEffects; text?: string; font?: string; font_size?: number }
export interface TimelineTrack { id: string; name: string; kind: 'video' | 'audio' | 'subtitle'; height: number; locked: boolean; muted: boolean; hidden: boolean; solo: boolean; group_id?: string; gain_db: number; clips: TimelineClip[] }
export interface TimelineSequence { id: string; revision: number; name: string; width: number; height: number; fps: FrameRate; sample_rate: number; configured: boolean; tracks: TimelineTrack[]; markers: TimelineMarker[]; groups: { id: string; name: string; origin: EditOrigin }[]; scope_group?: string; history_cursor: number }
export interface ScriptReference { id: string; name: string; kind: string; target_id: string; resource_id?: string; sequence_id?: string; clip_id?: string; start: number; end: number; unresolved: boolean }
export interface ReferenceCandidate extends ScriptReference { owner: string; frame?: number; source_time?: MediaTime; fps: FrameRate; in?: number; out?: number }
export interface TimelineOperation { type: string; track_id?: string; target_track_id?: string; clip_ids?: string[]; resource_id?: string; at?: number; end?: number; source_in?: MediaTime; source_out?: MediaTime; duration?: number; mode?: string; kind?: string; name?: string; height?: number; order?: number; locked?: boolean; muted?: boolean; hidden?: boolean; solo?: boolean; gain_db?: number; speed?: number; effects?: ClipEffects; marker?: TimelineMarker; marker_id?: string; group_id?: string; text?: string; font?: string; font_size?: number; clips?: TimelineClip[]; unlinked?: boolean }
export interface TimelineView { session: string; project_revision: number; sequence: TimelineSequence; history: { id: string; label: string; actor: string; applied: boolean }[]; content_version: string }
export interface BasicQuality { sha256?: string; status: 'passed' | 'failed' | 'not_checked'; audio_status: string; video_status: string; issues: { kind: string; start: number; end: number }[] }
export interface SequencePreview { basic_quality?: BasicQuality; content_version: string; sha256: string; duration: number; frames: number; width: number; height: number; fps: FrameRate; scope_group: string }
export const fpsValue = (rate: FrameRate) => rate.num / rate.den;
export const timeSeconds = (time: MediaTime) => time.value / time.scale;
export const frameSeconds = (frame: number, rate: FrameRate) => frame / fpsValue(rate);
export const mediaTime = (seconds: number): MediaTime => ({ value: Math.round(seconds * 1e9), scale: 1e9 });
export function activeTracks(sequence: TimelineSequence) {const tracks=sequence.tracks.filter(t=>!sequence.scope_group||(sequence.scope_group==='original'?!t.group_id:t.group_id===sequence.scope_group)),solo=new Set(tracks.filter(t=>t.solo).map(t=>t.kind));return tracks.filter(t=>!t.hidden&&!(t.kind==='audio'&&t.muted)&&(!solo.has(t.kind)||t.solo));}
// Non-drop timecode counts nominal whole frames; elapsed seconds still use
// the rational rate. Mixing the two produces jumps near minute boundaries.
export function frameLabel(frame: number, rate: FrameRate) { frame=Math.max(0,Math.round(frame));const fps = Math.round(fpsValue(rate)), total = Math.floor(frame / fps); return `${String(Math.floor(total / 3600)).padStart(2,'0')}:${String(Math.floor(total / 60) % 60).padStart(2,'0')}:${String(total % 60).padStart(2,'0')}:${String(frame % fps).padStart(2,'0')}`; }
/** Frames for HH:MM:SS:FF (or ;) and whole HH:MM:SS / MM:SS. Fractional or bare numbers are seconds. Invalid input returns null. */
export function parseTimecode(input: string, rate: FrameRate): number | null {
  const text = input.trim().replace(/[；;]/g, ':').replace(/s$/i, '').trim();
  if (!text || !rate?.num || !rate.den) return null;
  const nominal = Math.max(1, Math.round(fpsValue(rate)));
  const clock = (hh: number, mm: number, ss: number) => (mm > 59 || ss >= 60 || hh < 0) ? null : ((hh * 60 + mm) * 60 + ss);
  const tc = /^(\d+):(\d{1,2}):(\d{1,2}):(\d{1,2})$/.exec(text);
  if (tc) {
    const seconds = clock(Number(tc[1]), Number(tc[2]), Number(tc[3]));
    const ff = Number(tc[4]);
    if (seconds === null || ff >= nominal) return null;
    return seconds * nominal + ff;
  }
  const hms = /^(\d+):(\d{1,2}):(\d{1,2}(?:\.\d+)?)$/.exec(text);
  if (hms) {
    const ss = Number(hms[3]);
    const whole = clock(Number(hms[1]), Number(hms[2]), Math.floor(ss));
    if (whole === null) return null;
    const seconds = whole + (ss - Math.floor(ss));
    return Math.max(0, Math.round(hms[3].includes('.') ? seconds * fpsValue(rate) : seconds * nominal));
  }
  const ms = /^(\d+):(\d{1,2}(?:\.\d+)?)$/.exec(text);
  if (ms) {
    const mm = Number(ms[1]), ss = Number(ms[2]);
    if (mm > 59 || ss >= 60) return null;
    const seconds = mm * 60 + ss;
    return Math.max(0, Math.round(ms[2].includes('.') ? seconds * fpsValue(rate) : seconds * nominal));
  }
  if (!/^\d+(?:\.\d+)?$/.test(text)) return null;
  const seconds = Number(text);
  if (!Number.isFinite(seconds) || seconds < 0) return null;
  return Math.max(0, Math.round(seconds * fpsValue(rate)));
}
/** Empty spans on one track, including a leading hole. The tail after the last clip is not a gap. */
export function trackGaps(clips: { at: number; duration: number }[]): { start: number; end: number }[] {
  const sorted = [...clips].sort((a, b) => a.at - b.at);
  const gaps: { start: number; end: number }[] = [];
  let cursor = 0;
  for (const clip of sorted) {
    const at = Math.max(0, clip.at);
    if (at > cursor) gaps.push({ start: cursor, end: at });
    cursor = Math.max(cursor, at + Math.max(0, clip.duration));
  }
  return gaps;
}
export function linkedSelection(sequence: TimelineSequence, ids: string[], separate = false) { const set = new Set(ids); if (!separate) { const links = new Set(sequence.tracks.flatMap(t=>t.clips).filter(c=>set.has(c.id) && c.link_id).map(c=>c.link_id)); sequence.tracks.forEach(t=>t.clips.forEach(c=>{if (c.link_id && links.has(c.link_id)) set.add(c.id);})); } return [...set]; }
/** True when ids leave out a clip that shares a link_id, so an edit must stay unlinked. */
export function omitsLinkedPartner(sequence: TimelineSequence, ids: string[]) { return linkedSelection(sequence, ids).some(id => !ids.includes(id)); }
export function snapFrame(frame: number, candidates: number[], pixelsPerFrame: number, enabled: boolean) { frame=Math.max(0,Math.round(frame));if(!enabled)return frame;let closest=frame,distance=8 / pixelsPerFrame;for(const value of candidates){if(Math.abs(frame-value)<distance){closest=value;distance=Math.abs(frame-value);}}return closest; }
