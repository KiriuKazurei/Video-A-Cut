import type { QueryClient } from '@tanstack/react-query';
import { setHumanAccessToken } from './api.ts';
import { skillApi } from './skillApi.ts';

/** Protected reads that may hold preview URLs, audit rows, or run snapshots. */
export const SENSITIVE_QUERY_ROOTS = [
  'audit',
  'vu-projects',
  'vu-project',
  'vu-visible',
  'vu-skills',
  'vu-runs',
  'vu-run',
  'vu-context'
] as const;

export function pauseMediaPlayers(): void {
  if (typeof document === 'undefined') return;
  document.querySelectorAll('video,audio').forEach((node) => {
    if (node instanceof HTMLMediaElement) node.pause();
  });
}

/** Exit the human session and drop in-memory credentials, protected caches, and players. */
export async function endHumanSession(queryClient: Pick<QueryClient, 'removeQueries'>): Promise<void> {
  try {
    await skillApi.lockHumanSession();
  } catch {
    setHumanAccessToken('');
  } finally {
    setHumanAccessToken('');
    pauseMediaPlayers();
    await Promise.all(SENSITIVE_QUERY_ROOTS.map((key) => queryClient.removeQueries({ queryKey: [key] })));
  }
}
