import type { RuntimeStatus } from './desktop';

export type McpConfigFormat = 'json' | 'codex';

export function currentMcpURL(runtime?: RuntimeStatus): string {
  if (runtime?.status !== 'ready' || !runtime.mcp_url) return '';
  try {
    const url = new URL(runtime.mcp_url);
    if (url.protocol !== 'http:' || url.hostname !== '127.0.0.1' || Number(url.port) <= 0 || url.pathname !== '/mcp' || url.username || url.password || url.search || url.hash) return '';
    return url.href;
  } catch { return ''; }
}

/** Codex uses config.toml; JSON is for clients that support the mcpServers map. */
export function mcpConnectionConfig(runtime: RuntimeStatus | undefined, format: McpConfigFormat): string {
  const url = currentMcpURL(runtime);
  if (!url) return '';
  return format === 'codex' ? `[mcp_servers.vac]\nurl = ${JSON.stringify(url)}\n` : JSON.stringify({ mcpServers: { vac: { url } } }, null, 2);
}
