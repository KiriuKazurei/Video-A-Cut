/** MP4 preview protocol v1. See README.md; these wire types are shared by Node and WebUI. */
export type PreviewStatus = 'queued' | 'claimed' | 'running' | 'succeeded' | 'failed' | 'cancelled';
export type PreviewPhase = 'preparing' | 'rendering' | 'verifying' | 'publishing';
export type InputKind = 'edl' | 'delivery_manifest' | 'source_video' | 'packaged_source' | 'game_audio' | 'voice' | 'music' | 'srt';

export interface PreviewPreset {
  schema_version: 1;
  preset_id: 'mp4-720p-v1';
  video: {
    encoder: 'libx264'; pixel_format: 'yuv420p'; crf: number; speed: string;
    max_width: number; max_height: number; upscale: false;
    canvas: 'export_canvas'; fit: 'contain'; gap_color: 'black'; fps: 'project';
  };
  audio: {
    encoder: 'aac'; sample_rate: 48000; channels: 2; bitrate_kbps: number;
    normalize_mix: false;
    limiter: { limit: number; attack_ms: number; release_ms: number; auto_level: false; latency_compensation: true };
  };
  subtitles: { mode: 'burn_if_present'; font_family: string };
  container: { format: 'mp4'; faststart: true };
}

export interface PreviewInputFile {
  kind: InputKind;
  /** POSIX relative to package_ref. No drive, backslash, empty/dot/dotdot segment. */
  path: string;
  sha256: string;
  size: number;
}

export interface PreviewInput {
  schema_version: 1;
  preview_id: string;
  task_id: string;
  asset_id: string;
  run_id: string;
  revision_id: string;
  /** Relative to the controlled delivery root, never an absolute host path. */
  package_ref: string;
  edl_sha256: string;
  manifest_sha256: string;
  preset_id: 'mp4-720p-v1';
  preset_sha256: string;
  preset: PreviewPreset;
  files: PreviewInputFile[];
  input_sha256: string;
}

export interface PreviewStartRequest {
  revision_id: string;
  expected_version: number;
  preset_id: 'mp4-720p-v1';
  /** Retain on transport retry; generate a new ID for an explicit render retry. */
  request_id: string;
}

export interface PreviewCheck { code: string; status: 'passed' | 'blocked' | 'not_checked'; message: string }
export interface PreviewMedia {
  width: number; height: number; fps: number; frame_count: number; duration_seconds: number;
  video_codec: string; audio_codec: string; sample_rate: number; channels: number;
}
export interface PreviewArtifact {
  key: string;
  kind: 'preview' | 'render_report' | 'receipt';
  /** Relative to the preview directory, not the source package. */
  path: string;
  mime: string;
  size: number;
  sha256: string;
}
export interface PreviewView {
  preview_id: string; task_id: string; asset_id: string; run_id: string; revision_id: string;
  preset_id: string; input_sha256: string;
  /** Projected from Task, never maintained as another state machine. */
  status: PreviewStatus; progress: number; message?: string;
  invalidated: boolean; is_current: boolean;
  artifacts: PreviewArtifact[];
  media?: PreviewMedia;
}
export interface PreviewList {
  run_id: string; revision_id: string; expected_version: number; can_start: boolean;
  checks: PreviewCheck[]; previews: PreviewView[];
}
export interface PreviewCapability {
  protocol_version: 1; execution_protocol: 2; ready: boolean;
  presets: Array<{ preset_id: string; sha256: string }>;
  checks: PreviewCheck[];
}
/** Returned by claim_task; products remain bound to the immutable preview input. */
export interface PreviewExecutionScope {
  task_id: string; runtime_instance_id: string; execution_id: string;
  generation: number; input_sha256: string;
}
export interface SubmitPreviewRequest extends PreviewExecutionScope {
  preview_dir: string;
}
export interface PreviewReceipt {
  schema_version: 1; preview_id: string; task_id: string;
  input_sha256: string; preset_sha256: string;
  renderer_version: string; ffmpeg_version: string;
  /** Absent for subtitle-free inputs. No absolute font path in public metadata. */
  font?: { family: string; sha256: string };
}
export interface PreviewReport {
  schema_version: 1; input_sha256: string;
  target_frames: number; target_samples: number;
  media: PreviewMedia; checks: PreviewCheck[];
}
export interface PreviewManifest {
  schema_version: 1; preview_id: string; task_id: string;
  input_sha256: string; preset_sha256: string;
  /** Only these output files; source media are referenced via the fixed input. */
  artifacts: PreviewArtifact[];
}
