// Minimal MCP 2026-07-28 stateless Streamable HTTP client for the control
// plane. Worker identity is the bearer token only; the worker never names its
// own agent id or role.

const PROTOCOL = '2026-07-28';

export class ToolError extends Error {
  constructor(tool, text) {
    super(`${tool}: ${text}`);
    this.name = 'ToolError';
    this.tool = tool;
    // Stable prefix from the control plane, e.g. "lease_expired".
    this.code = String(text).split(':', 1)[0];
  }
}

export function createClient({ url, token, timeoutMs = 15_000, fetchImpl = fetch }) {
  if (!url) throw new Error('mcp url is required');
  if (!token || token.length < 32) throw new Error('mcp token is missing or too short');
  let nextId = 1;

  async function rpc(method, params, name) {
    const headers = {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      'Mcp-Protocol-Version': PROTOCOL,
      'Mcp-Method': method,
      Authorization: `Bearer ${token}`
    };
    if (name) headers['Mcp-Name'] = name;
    const body = JSON.stringify({
      jsonrpc: '2.0',
      id: nextId++,
      method,
      params: {
        ...params,
        _meta: {
          'io.modelcontextprotocol/protocolVersion': PROTOCOL,
          'io.modelcontextprotocol/clientCapabilities': {}
        }
      }
    });
    const res = await fetchImpl(url, { method: 'POST', headers, body, signal: AbortSignal.timeout(timeoutMs) });
    const text = await res.text();
    if (!res.ok) throw new Error(`mcp http ${res.status}: ${text.slice(0, 200)}`);
    const msg = JSON.parse(text);
    if (msg.error) throw new Error(`mcp rpc error ${msg.error.code}: ${msg.error.message}`);
    return msg.result;
  }

  return {
    async call(tool, args = {}) {
      const result = await rpc('tools/call', { name: tool, arguments: args }, tool);
      if (result.isError) {
        const text = result.content?.find((c) => c.type === 'text')?.text ?? 'unknown error';
        throw new ToolError(tool, text);
      }
      return result.structuredContent ?? {};
    },
    async listTools() {
      const result = await rpc('tools/list', {});
      return result.tools.map((t) => t.name);
    }
  };
}
