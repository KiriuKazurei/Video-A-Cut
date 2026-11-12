import { useQuery } from '@tanstack/react-query';
import { ApiError, request } from './api';
import { apiURL } from './runtimeUrl';

const runtimeShotPath = '/api/shotcraft/templates';
const directoryPath = '/api/shotcraft/directory';

export interface ShotCard {
  id: string;
  summary: string;
  category: string;
  use: string;
  duration: string;
  styles: { id: string; description: string }[];
  user?: boolean;
}

export const shotFramesKey = ['shotcraft-frame'] as const;
export const shotCardsKey = ['shotcraft-templates'] as const;

export function useShotCards(enabled = true) {
  return useQuery({
    queryKey: shotCardsKey,
    queryFn: ({ signal }) => request<ShotCard[]>(runtimeShotPath, { signal }),
    enabled,
    staleTime: 30_000,
    retry: false,
  });
}

export const shotcraftApi = {
  directory: (signal?: AbortSignal) => request<{ directory: string }>(directoryPath, { signal }),
  setDirectory: (directory: string) => request<{ directory: string }>(directoryPath, {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ directory }),
  }),
  import: (body: string) => request<ShotCard>(runtimeShotPath, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body,
  }),
  download: (id: string) => request<unknown>(runtimeShotPath + '/' + encodeURIComponent(id)),
  remove: (id: string) => request(runtimeShotPath + '/' + encodeURIComponent(id), { method: 'DELETE' }),
  preview: async (id: string, variant: string, position: number, signal: AbortSignal) => {
    const query = new URLSearchParams({ variant, position: String(position) });
    const response = await fetch(apiURL(runtimeShotPath + '/' + encodeURIComponent(id) + '/preview?' + query), {
      credentials: 'include', signal,
    });
    if (!response.ok) {
      const body = await response.json().catch(() => null);
      throw new ApiError(response.status, body?.error?.code ?? 'http_error', body?.error?.message ?? '预览生成失败');
    }
    return response.blob();
  },
};
