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
    """HTTP or JSON-RPC level failure; not a task outcome.

    ``kind`` is ``transient`` (retry within the local budget), ``auth``
    (401/403, stop and back off), or ``protocol`` (incompatible response).
    """

    kind = "transient"


class TransientTransportError(TransportError):
    kind = "transient"


class AuthTransportError(TransportError):
    kind = "auth"


class ProtocolTransportError(TransportError):
    kind = "protocol"


# Ingest control calls stay at or below this. Media tools are not MCP calls.
INGEST_REQUEST_TIMEOUT = 3.0

_TRANSIENT_HTTP = {408, 429, 500, 502, 503, 504}


def transport_kind(err: BaseException) -> str:
    return getattr(err, "kind", "transient") if isinstance(err, TransportError) else ""


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
            body = err.read()[:200]
            if err.code in (401, 403):
                raise AuthTransportError(f"mcp http {err.code}: {body!r}") from None
            if err.code in _TRANSIENT_HTTP:
                raise TransientTransportError(f"mcp http {err.code}: {body!r}") from None
            raise ProtocolTransportError(f"mcp http {err.code}: {body!r}") from None
        except (urllib.error.URLError, TimeoutError, OSError) as err:
            raise TransientTransportError(f"mcp transport: {err}") from None
        try:
            msg = json.loads(raw)
        except json.JSONDecodeError as err:
            raise ProtocolTransportError(f"mcp protocol: invalid json ({err})") from None
        if not isinstance(msg, dict) or "result" not in msg and "error" not in msg:
            raise ProtocolTransportError("mcp protocol: response envelope is not a JSON-RPC object")
        if "error" in msg:
            error = msg["error"] if isinstance(msg["error"], dict) else {}
            code = error.get("code")
            message = str(error.get("message") or "")
            text = f"mcp rpc error {code}: {message}"
            if code in (-32600, -32601, -32602) or "schema" in message.lower() or "incompatible" in message.lower():
                raise ProtocolTransportError(text)
            if code in (-32000,) and any(word in message.lower() for word in ("timeout", "unavailable", "temporarily")):
                raise TransientTransportError(text)
            raise ProtocolTransportError(text)
        return msg["result"]

    def call(self, tool: str, args: dict | None = None) -> dict:
        result = self._rpc("tools/call", {"name": tool, "arguments": args or {}}, tool)
        if result.get("isError"):
            text = next((c.get("text", "") for c in result.get("content", []) if c.get("type") == "text"), "unknown error")
            raise ToolError(tool, text)
        return result.get("structuredContent") or {}

    def list_tools(self) -> list[str]:
        return [t["name"] for t in self._rpc("tools/list", {})["tools"]]
