"""Claim-process-submit loop for the Python pipeline worker."""
from __future__ import annotations

import json
import os
import re
import shutil
import threading
import time
from dataclasses import dataclass
from pathlib import Path

from .mcp import Client, ToolError, TransportError
from .content import make_content_provider
from .stages import STAGES, StageContext, StageFailure, Tools

_ID = re.compile(r"^[A-Za-z0-9._-]+$")
_PREFIX = re.compile(r"^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$")
REQUIRED_TOOLS = ("claim_task", "heartbeat", "report_progress", "get_asset", "get_asset_edl", "submit_delivery", "fail_task")


@dataclass
class Config:
    mcp_url: str
    token: str
    delivery_root: Path
    output_prefix: str = "stages"
    poll_interval: float = 2.0
    heartbeat_interval: float = 5.0
    tool_timeout: float = 300.0
    ffmpeg: str = "ffmpeg"
    ffprobe: str = "ffprobe"
    content_provider: str = "builtin"


def load_config(path: str, env=os.environ) -> Config:
    raw = json.loads(Path(path).read_text(encoding="utf-8"))
    token_env = raw.get("token_env", "VAC_WORKER_TOKEN")
    token = env.get(token_env)
    if not token:
        raise ValueError(f"config: environment variable {token_env} is not set")
    for key in ("mcp_url", "delivery_root"):
        if not isinstance(raw.get(key), str) or not raw[key]:
            raise ValueError(f"config: {key} is required")
    root = Path(raw["delivery_root"])
    if not root.is_absolute() or not root.is_dir():
        raise ValueError("config: delivery_root must be an existing absolute directory")
    prefix = raw.get("output_prefix", "stages")
    if not _PREFIX.match(prefix):
        raise ValueError("config: output_prefix must be a relative slash path of [A-Za-z0-9_-]")
    cfg = Config(mcp_url=raw["mcp_url"], token=token, delivery_root=root.resolve(), output_prefix=prefix,
                 poll_interval=float(raw.get("poll_interval_ms", 2000)) / 1000,
                 heartbeat_interval=float(raw.get("heartbeat_interval_ms", 5000)) / 1000,
                 tool_timeout=float(raw.get("task_timeout_ms", 300000)) / 1000,
                 ffmpeg=raw.get("ffmpeg", "ffmpeg"), ffprobe=raw.get("ffprobe", "ffprobe"),
                 content_provider=raw.get("content_provider", "builtin"))
    make_content_provider(cfg.content_provider)
    if min(cfg.poll_interval, cfg.heartbeat_interval, cfg.tool_timeout) <= 0:
        raise ValueError("config: intervals and timeout must be positive")
    return cfg


def resolve_under_root(root: Path, rel: str) -> Path:
    if not isinstance(rel, str) or not rel or rel.startswith("/") or "\\" in rel or ":" in rel:
        raise StageFailure("artifact path is not a root-relative slash path")
    path = (root / rel).resolve()
    if root not in path.parents or not path.is_file():
        raise StageFailure(f"artifact is missing or escapes delivery root: {rel}")
    return path


class _Heartbeat:
    """Renews the lease in the background; records a lost lease."""

    def __init__(self, client: Client, task_id: str, interval: float, log):
        self.lost = False
        self._stop = threading.Event()
        self._t = threading.Thread(target=self._run, args=(client, task_id, interval, log), daemon=True)

    def _run(self, client, task_id, interval, log):
        while not self._stop.wait(interval):
            try:
                client.call("heartbeat", {"task_id": task_id})
            except ToolError as err:
                if err.code in ("lease_expired", "not_found", "invalid_state"):
                    self.lost = True
                    log(f"task {task_id}: heartbeat lost lease ({err.code})")
                    return
                log(f"task {task_id}: heartbeat error {err}")
            except TransportError as err:
                log(f"task {task_id}: heartbeat transport error {err}")

    def __enter__(self):
        self._t.start()
        return self

    def __exit__(self, *exc):
        self._stop.set()
        self._t.join(timeout=5)


def process_task(client: Client, cfg: Config, task: dict, log, stages=STAGES) -> dict:
    task_id, asset_id, kind = task["task_id"], task["asset_id"], task["type"]
    out_rel = f"{cfg.output_prefix}/{asset_id}/{task_id}"
    staging = None
    with _Heartbeat(client, task_id, cfg.heartbeat_interval, log) as beat:
        try:
            if kind not in stages:
                raise StageFailure(f"unsupported task type {kind}")
            if not _ID.match(asset_id) or not _ID.match(task_id) or set(asset_id) == {"."} or set(task_id) == {"."}:
                raise StageFailure("asset_id or task_id is unsafe for a directory name")
            client.call("report_progress", {"task_id": task_id, "progress": 0.05, "message": f"{kind}: reading edl"})
            asset = client.call("get_asset", {"asset_id": asset_id})["asset"]
            edl = client.call("get_asset_edl", {"asset_id": asset_id})["edl"]
            edl_path = resolve_under_root(cfg.delivery_root, (asset.get("artifacts") or {}).get("edl"))
            target = cfg.delivery_root.joinpath(*out_rel.split("/"))
            if target.exists():
                raise StageFailure(f"output package already exists: {out_rel}")
            target.parent.mkdir(parents=True, exist_ok=True)
            staging = target.parent / f".{task_id}-staging"
            if staging.exists():
                shutil.rmtree(staging)
            staging.mkdir()

            def progress(p, m):
                if not beat.lost:
                    client.call("report_progress", {"task_id": task_id, "progress": p, "message": f"{kind}: {m}"})

            ctx = StageContext(edl=edl, source_dir=edl_path.parent, out_dir=staging,
                               tools=Tools(ffmpeg=cfg.ffmpeg, ffprobe=cfg.ffprobe, timeout=cfg.tool_timeout),
                               progress=progress, content_provider=make_content_provider(cfg.content_provider))
            summary = stages[kind](ctx)
            if beat.lost:
                return {"outcome": "abandoned", "reason": "lease lost during stage"}
            staging.rename(target)
            staging = None
            client.call("report_progress", {"task_id": task_id, "progress": 0.95, "message": f"{kind}: registering package"})
            client.call("submit_delivery", {"task_id": task_id, "package_dir": out_rel})
            return {"outcome": "succeeded", "package": out_rel, "summary": summary}
        except ToolError as err:
            if err.code == "lease_expired" or beat.lost:
                return {"outcome": "abandoned", "reason": str(err)}
            return _fail(client, task_id, str(err), beat)
        except (StageFailure, OSError, KeyError, ValueError, TypeError) as err:
            return _fail(client, task_id, f"{type(err).__name__}: {err}", beat)
        finally:
            if staging is not None and staging.exists():
                shutil.rmtree(staging, ignore_errors=True)


def _fail(client: Client, task_id: str, reason: str, beat: _Heartbeat) -> dict:
    if beat.lost:
        return {"outcome": "abandoned", "reason": reason}
    try:
        client.call("fail_task", {"task_id": task_id, "reason": reason[:1900]})
        return {"outcome": "failed", "reason": reason}
    except (ToolError, TransportError) as err:
        return {"outcome": "fail_report_error", "reason": f"{reason}; report: {err}"}


def run_loop(client: Client, cfg: Config, log, stop: threading.Event, once: bool = False) -> dict:
    tools = client.list_tools()
    missing = [t for t in REQUIRED_TOOLS if t not in tools]
    if missing:
        raise RuntimeError(f"control plane does not expose {', '.join(missing)}")
    client.call("heartbeat", {})
    log("worker registered; polling")
    while not stop.is_set():
        try:
            claim = client.call("claim_task", {})
        except (ToolError, TransportError) as err:
            log(f"claim error: {err}")
            stop.wait(cfg.poll_interval)
            continue
        if not claim.get("claimed"):
            if once:
                return {"outcome": "idle"}
            stop.wait(cfg.poll_interval)
            continue
        task = claim["task"]
        log(f"claimed {task['task_id']} ({task['type']}) for {task['asset_id']}")
        result = process_task(client, cfg, task, log)
        log(f"task {task['task_id']}: {result['outcome']}" + (f" - {result.get('reason')}" if result.get("reason") else ""))
        if once:
            return result
    return {"outcome": "stopped"}


def _now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%S")
