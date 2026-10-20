import { request } from './api.ts';
import { apiURL } from './runtimeUrl.ts';
import type { VacProject, ProjectSave, SkillPackage, ProjectVisibleSet, SkillRun, SkillRunCreate,
  SkillRunContext, SkillToolRequest, SkillStep, FFmpegCapabilities, SkillVoiceSave,
  SkillEditRequest, SkillWorkbench, SkillHistory, SkillPlan, AgentOperation } from './skillContracts';
const id = encodeURIComponent;
const json = (value: unknown, method = 'POST'): RequestInit =>
  ({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) });
export const skillApi = {
  workbench: (run: string, signal?: AbortSignal) => request<SkillWorkbench>(`/api/skill-runs/${id(run)}/workbench`, { signal }),
  savePlan: (run: string, expected_version: number, plan: SkillPlan) => request<SkillRun>(`/api/skill-runs/${id(run)}/plan`, json({ expected_version, plan }, 'PUT')),
  requestEdit: (run: string, value: SkillEditRequest) => request<SkillRun>(`/api/skill-runs/${id(run)}/edits`, json(value)),
  history: (run: string) => request<SkillHistory>(`/api/skill-runs/${id(run)}/checkpoints`),
  moveHistory: (run: string, action: 'undo' | 'redo', expected_version: number) => request<SkillRun>(`/api/skill-runs/${id(run)}/${action}`, json({ expected_version })),
  checkpointPreviewURL: (run: string, checkpoint: string, artifact: string) => apiURL(`/api/skill-runs/${id(run)}/checkpoints/${id(checkpoint)}/artifacts/${id(artifact)}`),
  agentOperations: (run: string, after = 0, limit = 100) => request<AgentOperation[]>(`/api/agent-operations?run_id=${id(run)}&after=${after}&limit=${limit}`),
  verifyAgentOperations: () => request<{ verified_through: number }>('/api/agent-operations/verify'),
  appendOperationNote: (run_id: string, note: string) => request<{ appended: boolean }>('/api/agent-operations/notes', json({ run_id, note })),
  projects: (signal?: AbortSignal) => request<VacProject[]>('/api/projects', { signal }),
  project: (project: string, signal?: AbortSignal) => request<VacProject>(`/api/projects/${id(project)}`, { signal }),
  createProject: (value: ProjectSave) => request<VacProject>('/api/projects', json(value)),
  saveProject: (project: string, value: ProjectSave) => request<VacProject>(`/api/projects/${id(project)}`, json(value, 'PUT')),
  exportProject: (project: string, value: { run_id: string; expected_version: number; idempotency_key: string }, signal?: AbortSignal) =>
    request<{ run: SkillRun; result: unknown }>(`/api/projects/${id(project)}/exports/ffmpeg`, { ...json(value), signal }),
  importMedia: (project: string, value: { root_id: string; relative_path: string; expected_version: number; idempotency_key: string }, signal?: AbortSignal) =>
    request<VacProject>(`/api/projects/${id(project)}/media`, { ...json(value), signal }),
  visibleSet: (project: string, signal?: AbortSignal) => request<ProjectVisibleSet>(`/api/projects/${id(project)}/visible-set`, { signal }),
  confirmVisible: (project: string, sha: string, expected_version: number) =>
    request<ProjectVisibleSet>(`/api/projects/${id(project)}/visible-set/confirm`, json({ visible_set_sha: sha, expected_version })),
  skills: () => request<SkillPackage[]>('/api/skills'),
  skill: (skill: string) => request<SkillPackage>(`/api/skills/${id(skill)}`),
  importSkill: (path: string) => request<SkillPackage>('/api/skills', json({ path })),
  enableSkill: (skill: string, enabled: boolean) => request<SkillPackage>(`/api/skills/${id(skill)}`, json({ enabled }, 'PATCH')),
  validateSkill: (skill: string) => request<Record<string, unknown>>(`/api/skills/${id(skill)}/validate`, json({})),
  ffmpeg: () => request<FFmpegCapabilities>('/api/system/ffmpeg'),
  createRun: (value: SkillRunCreate) => request<SkillRun>('/api/skill-runs', json(value)),
  runs: (project: string, signal?: AbortSignal) => request<SkillRun[]>(`/api/skill-runs?project_id=${id(project)}`, { signal }),
  run: (run: string, signal?: AbortSignal) => request<SkillRun>(`/api/skill-runs/${id(run)}`, { signal }),
  context: (run: string, signal?: AbortSignal) => request<SkillRunContext>(`/api/skill-runs/${id(run)}/context`, { signal }),
  steps: (run: string) => request<SkillStep[]>(`/api/skill-runs/${id(run)}/steps`),
  revisions: (run: string) => request<unknown[]>(`/api/skill-runs/${id(run)}/revisions`),
  saveVoice: (run: string, value: SkillVoiceSave) => request<SkillRun>(`/api/skill-runs/${id(run)}/voice`, json(value, 'PUT')),
  tool: (run: string, value: SkillToolRequest, signal?: AbortSignal) =>
    request<{ run: SkillRun; result: unknown }>(`/api/skill-runs/${id(run)}/tools`, { ...json(value), signal }),
  action: (run: string, action: 'finalize' | 'resume' | 'cancel', expected_version: number) =>
    request<SkillRun>(`/api/skill-runs/${id(run)}/${action}`, json({ expected_version })),
  execute: (run: string, expected_version: number, signal?: AbortSignal) =>
    request<SkillRun>(`/api/skill-runs/${id(run)}/execute`, { ...json({ expected_version }), signal }),
  artifactURL: (run: string, artifact: string) => apiURL(`/api/skill-runs/${id(run)}/artifacts/${id(artifact)}`),
};
