export interface VariantStartRequest {
  revision_id: string;
  expected_version: number;
  request_id: string;
  spec_id: 'ass-basic-v1' | 'ass-basic-v2';
  draft_version?: number;
  style_sha256?: string;
}
export interface VariantArtifact {
  key: string; kind: string; path: string; mime: string; size: number; sha256: string;
}
export interface VariantView {
  style_sha256?: string;
  variant_id: string; task_id: string; asset_id: string; run_id: string; revision_id: string;
  spec_id: string; format: string; input_sha256: string;
  status: string; progress: number; message?: string;
  invalidated: boolean; is_current: boolean; artifacts: VariantArtifact[];
}
export interface VariantList {
  can_start_editable?: boolean;
  run_id: string; revision_id: string; expected_version: number; can_start: boolean;
  checks: { code: string; status: string; message?: string }[];
  specs: { spec_id: string; sha256: string }[];
  variants: VariantView[];
}

export interface SubtitleStyle {
  font_family: string; font_size: number; bold: boolean; italic: boolean; underline: boolean;
  outline: number; shadow: number; primary_color: string; outline_color: string; alignment: number;
  margin_l: number; margin_r: number; margin_v: number; fade_in_ms: number; fade_out_ms: number;
}
export interface SubtitleCueOverride {
  cue_id: number; start_ms?: number; end_ms?: number; style?: Partial<SubtitleStyle>;
}
export interface StyleDocument {
  source_srt_sha256: string; play_res_x: number; play_res_y: 720;
  style: SubtitleStyle; overrides: SubtitleCueOverride[];
}
export interface SourceCue {cue_id: number; start_ms: number; end_ms: number; text: string}
export interface SubtitleStyleDraft {
  asset_id: string; run_id: string; revision_id: string; draft_version: number;
  style_sha256: string; duration_ms: number; cues: SourceCue[]; document: StyleDocument;
}
export interface SaveStyleRequest {revision_id: string; expected_draft_version: number; document: StyleDocument}
export interface SubtitleFontFamily {family: string; sha256: string}
