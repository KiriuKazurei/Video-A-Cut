import { request } from './api.ts';

export interface ExternalAgentPresence {
  agent_id: string;
  name: string;
  version: string;
  transport: 'desktop_mcp' | 'worker_mcp';
  state: 'connected' | 'busy' | 'stale' | 'expired' | 'disconnected' | 'error';
  connected_at: string;
  last_seen_at: string;
  last_tool?: string;
  active_calls: number;
}
export interface ExternalAgentStatusReport {
  generated_at: string;
  stale_after_seconds: number;
  agents: ExternalAgentPresence[];
}

export const externalAgentApi = {
  status: (signal?: AbortSignal) => request<ExternalAgentStatusReport>('/api/agents/status', { signal })
};

export const EXTERNAL_AGENT_LABELS: Record<ExternalAgentPresence['state'], string> = {
  connected: '最近有活动', busy: '工具调用中', stale: '连接待确认', expired: '会话已过期', disconnected: '已主动断开', error: '最近调用失败'
};

export function externalAgentSummary(report: ExternalAgentStatusReport | undefined, failed: boolean): { label: string; tone: 'default' | 'success' | 'processing' | 'warning' | 'error' } {
  if (failed) return { label: 'Agent 状态读取失败', tone: 'error' };
  if (!report) return { label: '正在读取 Agent 状态', tone: 'default' };
  const agents = report.agents;
  const busy = agents.filter(agent => agent.state === 'busy').length;
  const recent = agents.filter(agent => agent.state === 'connected').length;
  if (busy) return { label: `${busy} 个 Agent 正在调用工具`, tone: 'processing' };
  if (recent) return { label: `${recent} 个 Agent 最近有活动`, tone: 'success' };
  if (agents.some(agent => agent.state === 'error')) return { label: 'Agent 最近调用失败', tone: 'error' };
  if (agents.some(agent => agent.state === 'stale')) return { label: 'Agent 连接待确认', tone: 'warning' };
  if (agents.length) return { label: 'Agent 已断开', tone: 'default' };
  return { label: '尚无 Agent 活动', tone: 'default' };
}
