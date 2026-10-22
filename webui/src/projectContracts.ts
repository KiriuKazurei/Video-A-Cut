import type { ScriptSegment } from './scriptWorkbench';
import type { TimelineSequence, TimelineMarker, ScriptReference } from './timelineContracts';

export type ResourceKind = 'video' | 'audio' | 'image' | 'subtitle' | 'document' | 'script';
export interface ProjectResource {
  id: string; kind: ResourceKind; name: string;
  source: { relative_path?: string; original_path?: string; sha256?: string; size_bytes: number; modified_ns?: number };
  metadata: Record<string, unknown>; notes: string; in: number; out: number;
  missing: boolean; removed?: boolean; changed?: boolean; referenced?: boolean; asset_id?: string; script_id?: string;
  markers?: TimelineMarker[];
}
export interface ScriptBinding { id: string; name: string; kind: ResourceKind; resource_id?: string; unresolved: boolean }
export interface AvsDocument {
  format: 'avs'; schema_version: 1 | 2; id: string; title: string; body: string; references?: ScriptReference[];
  segments: ScriptSegment[]; bindings: ScriptBinding[];
  tts_enabled: boolean; subtitles_enabled: boolean; allow_web_research: boolean;
}
export interface AvpDocument {
  format: 'avp'; schema_version: 1 | 2;
  project: { id: string; name: string; created_at: string; updated_at: string };
  resources: ProjectResource[]; scripts: AvsDocument[];
  sequences: TimelineSequence[];
  settings: Record<string, unknown>; workspace: Record<string, unknown>;
}
export interface ProjectSession {
  id: string; path: string; dirty: boolean; revision: number; active_tasks: number; document: AvpDocument | null;
  recent: { path: string; name: string; opened_at: string }[];
  recoveries: { path: string; name: string; recovery_path: string; updated_at: string }[];
}
export interface ResourcePatch { name?: string; notes?: string; in?: number; out?: number; path?: string; removed?: boolean }
export interface AvsImport { path: string; script_only?: boolean; bindings?: Record<string, string>; files?: Record<string, string> }
export const RESOURCE_LABELS: Record<ResourceKind, string> = { video: '视频', audio: '音频', image: '图片', subtitle: '字幕', document: '文稿', script: '脚本' };
