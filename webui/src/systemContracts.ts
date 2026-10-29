import { request } from './api.ts';

export type DiagnosticStatus = 'passed' | 'warning' | 'failed' | 'not_checked';
export interface DiagnosticCheck {
  id: string; label: string; status: DiagnosticStatus; message: string;
  next_step?: string; version?: string;
}
export interface DiagnosticReport {
  schema_version: 1; generated_at: string; platform: string; architecture: string;
  status: 'passed' | 'warning' | 'failed'; redacted: true;
  scope: 'local_read_only_no_model_requests'; checks: DiagnosticCheck[];
}
export interface OnlineSkillCapabilities {
  schema_version: 1; enabled: false; status: 'reserved';
  operations: ('install' | 'check_updates' | 'update')[];
}
/** Future request shape only: mutation routes currently return 501 without reading the payload. */
export interface OnlineSkillRequest { source?: string; version?: string; expected_sha256?: string; idempotency_key?: string }
export const systemApi = {
  diagnostics: (signal?: AbortSignal) => request<DiagnosticReport>('/api/system/diagnostics', { signal }),
  diagnosticReport: (signal?: AbortSignal) => request<DiagnosticReport>('/api/system/diagnostics/report', { signal }),
  onlineSkills: (signal?: AbortSignal) => request<OnlineSkillCapabilities>('/api/skills/online/capabilities', { signal }),
  installSkill: (value: OnlineSkillRequest) => request<never>('/api/skills/online/install', { method: 'POST', body: JSON.stringify(value) }),
  checkSkillUpdates: (id: string) => request<never>(`/api/skills/${encodeURIComponent(id)}/check-updates`, { method: 'POST', body: '{}' }),
  updateSkill: (id: string, value: OnlineSkillRequest) => request<never>(`/api/skills/${encodeURIComponent(id)}/update`, { method: 'POST', body: JSON.stringify(value) }),
};
export function diagnosticSummary(report: DiagnosticReport) {
  return { failed: report.checks.filter(c => c.status === 'failed').length,
    warning: report.checks.filter(c => c.status === 'warning').length,
    notChecked: report.checks.filter(c => c.status === 'not_checked').length };
}
