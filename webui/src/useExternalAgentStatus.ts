import { useQuery } from '@tanstack/react-query';
import { useDesktopRuntimeStatus } from './components/DesktopRuntime';
import { externalAgentApi } from './externalAgentStatus';

/** Read every three minutes; MCP events mark stale without extra background reads. */
export function useExternalAgentStatus(active = true) {
  const runtime = useDesktopRuntimeStatus();
  return useQuery({
    queryKey: ['external-agent-status', runtime?.session_id ?? 'browser'],
    queryFn: ({ signal }) => externalAgentApi.status(signal),
    enabled: active,
    refetchInterval: active ? 180_000 : false,
    staleTime: 180_000,
    refetchOnWindowFocus: false,
    networkMode: 'always',
    retry: false
  });
}
