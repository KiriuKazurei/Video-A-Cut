"""Minimal MCP 2026-07-28 stateless Streamable HTTP client (stdlib only).

Identity is the bearer token; the worker never names its own agent id or role.
"""
from __future__ import annotations

import itertools
import json
import urllib.error
import urllib.request

PROTOCOL = "2026-07-28"


class ToolError(Exception):
    """A tool returned isError. ``code`` is the control plane's stable prefix."""

    def __init__(self, tool: str, text: str):
        super().__init__(f"{tool}: {text}")
        self.tool = tool
        self.code = text.split(":", 1)[0]


class TransportError(Exception):
    """HTTP or JSON-RPC level failure; not a task outcome."""


class Client:
    def __init__(self, url: str, token: str, timeout: float = 15.0, opener=None):
        if not url:
            raise ValueError("mcp url is required")
        if not token or len(token) < 32:
            raise ValueError("mcp token is missing or too short")
        self._url = url
        self._token = token
        self._timeout = timeout
        self._ids = itertools.count(1)
        self._open = opener or urllib.request.urlopen

    def _rpc(self, method: str, params: dict, name: str | None = None) -> dict:
        body = json.dumps({
            "jsonrpc": "2.0",
            "id": next(self._ids),
            "method": method,
            "params": {
                **params,
                "_meta": {
                    "io.modelcontextprotocol/protocolVersion": PROTOCOL,
                    "io.modelcontextprotocol/clientCapabilities": {},
                },
            },
        }).encode("utf-8")
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            "Mcp-Protocol-Version": PROTOCOL,
            "Mcp-Method": method,
            "Authorization": f"Bearer {self._token}",
        }
        if name:
            headers["Mcp-Name"] = name
        req = urllib.request.Request(self._url, data=body, headers=headers, method="POST")
        try:
            with self._open(req, timeout=self._timeout) as resp:
                raw = resp.read()
        except urllib.error.HTTPError as err:
            raise TransportError(f"mcp http {err.code}: {err.read()[:200]!r}") from None
        except (urllib.error.URLError, TimeoutError, OSError) as err:
            raise TransportError(f"mcp transport: {err}") from None
        msg = json.loads(raw)
        if "error" in msg:
            raise TransportError(f"mcp rpc error {msg['error'].get('code')}: {msg['error'].get('message')}")
        return msg["result"]

    def call(self, tool: str, args: dict | None = None) -> dict:
        result = self._rpc("tools/call", {"name": tool, "arguments": args or {}}, tool)
        if result.get("isError"):
            text = next((c.get("text", "") for c in result.get("content", []) if c.get("type") == "text"), "unknown error")
            raise ToolError(tool, text)
        return result.get("structuredContent") or {}

    def list_tools(self) -> list[str]:
        return [t["name"] for t in self._rpc("tools/list", {})["tools"]]
