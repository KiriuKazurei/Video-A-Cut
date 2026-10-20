import { request } from './api.ts';
import { apiURL } from './runtimeUrl.ts';
import type { AvsDocument, AvsImport, ProjectSession, ResourcePatch } from './projectContracts';

const id = encodeURIComponent;
const json = (value: unknown, method = 'POST'): RequestInit => ({ method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(value) });
export const projectApi = {
  workspace: (signal?: AbortSignal) => request<ProjectSession>('/api/workspace', { signal }),
  command: (action: string, value: { path?: string; name?: string; decision?: string; stop_tasks?: boolean; expected_session?: string; recovery_path?: string }) => request<ProjectSession>(`/api/workspace/${id(action)}`, json(value)),
  state: (session: string, state: Record<string, unknown>) => request<ProjectSession>('/api/workspace/state', json({ session, state }, 'PATCH')),
  importResource: (project: string, path: string) => request<ProjectSession>(`/api/projects/${id(project)}/resources`, json({ path })),
  patchResource: (project: string, resource: string, patch: ResourcePatch) => request<ProjectSession>(`/api/projects/${id(project)}/resources/${id(resource)}`, json(patch, 'PATCH')),
  inspectProject: (path: string) => request('/api/workspace/inspect-project', json({ path })),
  inspectScript: (path: string) => request<AvsDocument>('/api/workspace/inspect-script', json({ path })),
  importScript: (project: string, value: AvsImport) => request<ProjectSession>(`/api/projects/${id(project)}/scripts/import-file`, json(value)),
  bindScript: (project: string, script: string, value: AvsImport) => request<ProjectSession>(`/api/projects/${id(project)}/scripts/${id(script)}/bindings`, json(value, 'PATCH')),
  exportScript: (project: string, script: string, path: string) => request<{ saved: boolean }>(`/api/projects/${id(project)}/scripts/${id(script)}/export-file`, json({ path })),
  contentURL: (project: string, resource: string, proxy = false) => apiURL(`/api/projects/${id(project)}/resources/${id(resource)}/content${proxy ? '?proxy=1' : ''}`),
  createPreview: (project: string, resource: string, signal?: AbortSignal) => request<{ ready: boolean }>(`/api/projects/${id(project)}/resources/${id(resource)}/preview`, { ...json({}), signal }),
  providerPolicy: () => request<{ external_tts: boolean }>('/api/workspace/providers'),
  saveProviderPolicy: (external_tts: boolean) => request<{ external_tts: boolean }>('/api/workspace/providers', json({ external_tts }, 'PUT'))
};
