/** Backend contracts only. No workspace layout is prescribed here. */
import type { TtsSettings } from './ttsContracts';
export type SkillFlow = 'lightweight' | 'visual_heavy' | 'custom';
export type SkillExecutionMode = 'external_mcp' | 'builtin_api';
export interface VacProject {
  transcription_authorized: boolean; profile_id?: string; profile_revision?: number;
  id: string; name: string; flow: SkillFlow; version: number;
  assets: string[]; visible: string[]; confirmed_sha: string; confirmed_by?: string;
  default_skills: Partial<Record<SkillFlow, string>> | null;
  disabled_skills: string[] | null; external_authorized: boolean;
  created_at: string; updated_at: string;
}
export interface ProjectSave {
  accept_transcription?: boolean; profile_id?: string; profile_revision?: number;
  name: string; flow: SkillFlow; expected_version: number;
  assets: string[]; visible: string[];
  default_skills?: Partial<Record<SkillFlow, string>>;
  disabled_skills?: string[]; accept_external?: boolean;
}
export interface SkillPackage {
  id: string; name: string; description: string; version: string;
  source: 'builtin' | 'local_path'; builtin: boolean; enabled: boolean;
  runtime: 'instructions_only' | 'harness'; compatible_flows: SkillFlow[];
  execution_modes: SkillExecutionMode[]; tools: string[]; permissions: string[];
  inputs: string[]; outputs: string[]; content: string; resources: Record<string, string>;
  content_sha256: string; upstream?: string;
}
export interface ProjectMedia {
  kind?: 'video' | 'audio';
  id: string; asset_id: string; name: string; in: number; out: number; sha256: string;
}
export interface ProjectVisibleSet {
  project_id: string; scope_version: 2; sha256: string; confirmed: boolean; media: ProjectMedia[];
}
export interface SkillBudget { max_steps: number; max_seconds: number; max_frames: number }
export interface SkillPlan {
  body: string; segments: { media_id: string; start: number; end: number; narration: string;
    tts_enabled: boolean; subtitles_enabled?: boolean; voice?: TtsSettings;
    voice_offset: number; voice_consent: boolean }[];
  subtitles: boolean; version: number; finalized_by?: string; finalized_at?: string;
}
export interface SkillVoiceSave {
  expected_version: number; segment_index: number; enabled: boolean;
  settings: TtsSettings; offset_seconds: number; accept_external: boolean;
}
export interface SkillArtifact {
  id: string; name: string; kind: 'frame' | 'transcript' | 'summary' | 'mp4' | 'premiere_package' | 'inspection';
  size_bytes: number; sha256: string; plan_version?: number;
}
export interface SkillFrame {
  id: string; media_id: string; pts: number; score: number; strategy: string; sha256: string;
}
export interface ContentSummary {
  model: string; narrative: string; beats: string[];
  timeline: { media_id: string; start: number; end: number; description: string;
    confidence: 'high' | 'medium' | 'low'; frame_ids: string[] }[];
}
export interface SkillRun {
	edit_request?: SkillEditContext;
  lease_until?: string;
  transcription_authorized: boolean; agent_role?: string; elapsed_milliseconds: number;
  budget_step_base: number; budget_elapsed_base: number;
  inspection?: { plan_version: number; frame_ids: string[]; verdict: 'pending' | 'passed' | 'needs_changes'; issues: string[] };
  id: string; project_id: string; project_version: number; skill_id: string;
  skill_sha256: string; skill_version: string; execution_mode: SkillExecutionMode;
  flow: SkillFlow; visible_set_sha: string; sequence_id?:string;
  state: 'queued' | 'running' | 'waiting_human' | 'succeeded' | 'failed' | 'cancelled';
  reason?: string; owner: string; version: number; steps: number; budget: SkillBudget;
  goal: string; profile_id?: string; profile_revision?: number; profile_sha256?: string;
  external_authorized: boolean; plan?: SkillPlan; summary?: ContentSummary;
  frames: SkillFrame[]; frames_spent: number; artifacts: SkillArtifact[]; created_at: string; updated_at: string;
}
export interface SkillEditWindow {
  segment_index: number; media_id: string; timeline_start: number; timeline_end: number;
  source_start: number; source_end: number;
}
export interface SkillEditContext {
  prompt: string; base_plan_version: number; base_plan: SkillPlan; windows: SkillEditWindow[];
  requested_start?: number; requested_end?: number; source_artifact_id: string; source_artifact_sha256: string;
}
export interface SkillEditRequest {
  expected_version: number; plan_version: number; prompt: string; artifact_id: string; artifact_sha256: string;
  idempotency_key: string; segment_index?: number; at_seconds?: number; timeline_start?: number; timeline_end?: number;
}
export interface SkillCheckpoint { id: string; run_id: string; parent_id: string; snapshot: Partial<SkillRun>; sha256: string }
export interface SkillHistory { cursor: { checkpoint_id: string; redo: string[] }; checkpoints: SkillCheckpoint[] }
export interface AgentOperation { sequence: number; run_id: string; previous_sha256: string; sha256: string;
  body: { actor: string; action: string; at: string; detail: unknown } }
export interface SkillWorkbench {
  run: SkillRun; segments: SkillEditWindow[]; history: SkillHistory['cursor'];
  previews: { artifact: SkillArtifact; available: boolean; is_current: boolean; historical: boolean; checkpoint_id: string; exportable: boolean; preview_url?: string }[];
}
export interface SkillRunCreate {
  sequence_id?: string;
  flow?: SkillFlow;
  accept_transcription?: boolean;
  project_id: string; skill_id: string; execution_mode: SkillExecutionMode; visible_set_sha: string;
  goal: string; profile_id?: string; profile_revision?: number; accept_external?: boolean;
  idempotency_key: string; budget?: Partial<SkillBudget>;
}
export type SkillToolName = 'media.list' | 'media.get_frames' | 'media.transcribe'
  | 'timeline.read' | 'timeline.edit' | 'references.resolve'
  | 'summaries.get' | 'summaries.submit' | 'scripts.propose' | 'renders.create'
  | 'renders.inspect' | 'renders.review' | 'artifacts.list' | 'artifacts.get' | 'run.finish';
export interface SkillToolRequest {
  name: SkillToolName; arguments: Record<string, unknown>;
  expected_version: number; idempotency_key: string;
}
export interface SkillStep {
  id: string; run_id: string; name: string; state: 'running' | 'succeeded' | 'failed';
  request_sha256: string; result?: unknown; error?: string; created_at: string;
}
export interface SkillRunContext { run: SkillRun; skill: SkillPackage; media: ProjectMedia[]; tools: string[] }
export interface FFmpegCapabilities {
  available: boolean; ffprobe: boolean; version: string; libx264: boolean; aac: boolean;
  subtitles: boolean; hardware_acceleration?: string;
}
/** Queries must include project/run identity. SSE is an invalidation signal, never a snapshot. */
export const skillQueryKeys = {
  projects: ['vu-projects'] as const,
  project: (id: string) => ['vu-project', id] as const,
  visible: (id: string) => ['vu-visible', id] as const,
  skills: ['vu-skills'] as const,
  runs: (project: string) => ['vu-runs', project] as const,
  run: (project: string, run: string) => ['vu-run', project, run] as const,
  context: (project: string, run: string) => ['vu-context', project, run] as const,
};
export const skillEvents = ['project.changed', 'skill.changed', 'skill.run.changed'] as const;
