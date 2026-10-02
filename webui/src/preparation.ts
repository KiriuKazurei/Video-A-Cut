export type ProviderFormat = 'openai' | 'anthropic' | 'gemini';
export interface ProcessingProvider {
  adapter: 'builtin' | 'vision' | 'narration';
  endpoint?: string;
  model?: string;
  token_env?: string;
  api_format?: ProviderFormat;
}
export interface ProviderDiagnostic {
  ok: boolean;
  code: string;
  message: string;
  latency_ms: number;
  models?: { id: string; name: string }[];
  truncated?: boolean;
}
export interface ProcessingProfile {
  schema_version: 1;
  profile_id: string;
  revision: number;
  name: string;
  content_mode: 'builtin' | 'configured';
  vision: ProcessingProvider;
  narration: ProcessingProvider;
  sampling: { max_frames: number; max_bytes: number; timeout_seconds: number };
  tts_voice: string;
  export_target: 'premiere';
}
export interface PreparationReport {
  expected_asset_version?: string;
  schema_version: 1;
  profile_id: string;
  profile_revision: number;
  profile_sha256: string;
  status: 'blocked' | 'ready';
  can_start: boolean;
  checks: { code: string; status: 'passed' | 'blocked' | 'not_checked'; message: string }[];
}
