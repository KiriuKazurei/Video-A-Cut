"""Claim-process-submit loop for the Python pipeline worker."""
from __future__ import annotations

import json
import math
import os
import re
import shutil
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from .mcp import Client, ToolError, TransportError
from .content import make_content_provider
from .sampling import SamplingLimits
from .stages import STAGES, StageContext, StageFailure, Tools

_ID = re.compile(r"^[A-Za-z0-9._-]+$")
_PREFIX = re.compile(r"^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$")
_DRAFT_HASH = re.compile(r"^[0-9a-f]{64}$")
REQUIRED_TOOLS = ("claim_task", "heartbeat", "report_progress", "get_asset", "get_asset_edl", "get_task_input", "submit_delivery", "fail_task")
_SAMPLING_INTS = (
    "max_frames_per_clip", "max_width", "max_height", "max_bytes_per_frame",
    "max_total_frames", "max_total_bytes",
)
_SAMPLING_FLOATS = ("timeout_per_clip", "max_total_time")


@dataclass
class Config:
    mcp_url: str
    token: str
    delivery_root: Path
    role: str = ""
    profile_sha256: str = ""
    processing_profile: dict = field(default_factory=dict)
    tts_voice: str = ""
    output_prefix: str = "stages"
    poll_interval: float = 2.0
    heartbeat_interval: float = 5.0
    tool_timeout: float = 300.0
    ffmpeg: str = "ffmpeg"
    ffprobe: str = "ffprobe"
    content_provider: str = "builtin"
    content_provider_config: dict[str, Any] = field(default_factory=dict)
    tts_auto_approve: bool = False
    sampling_limits: SamplingLimits | None = None
    ingest_roots: dict[str, Path] = field(default_factory=dict)
    provider_timeout_seconds: float = 30.0


WorkerConfig = Config


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
    content_provider_config = raw.get("content_provider_config") or {}
    if not isinstance(content_provider_config, dict):
        raise ValueError("config: content_provider_config must be an object")
    auto_approve = raw.get("tts_auto_approve", False)
    if type(auto_approve) is not bool:
        raise ValueError("config: tts_auto_approve must be a bool")
    cfg = Config(mcp_url=raw["mcp_url"], token=token, delivery_root=root.resolve(), output_prefix=prefix,
                 poll_interval=float(raw.get("poll_interval_ms", 2000)) / 1000,
                 heartbeat_interval=float(raw.get("heartbeat_interval_ms", 5000)) / 1000,
                 tool_timeout=float(raw.get("task_timeout_ms", 300000)) / 1000,
                 ffmpeg=raw.get("ffmpeg", "ffmpeg"), ffprobe=raw.get("ffprobe", "ffprobe"),
                 content_provider=raw.get("content_provider", "builtin"),
                 content_provider_config=content_provider_config,
                 tts_auto_approve=auto_approve,
                 sampling_limits=_parse_sampling_limits(raw))
    try:
        make_content_provider(cfg.content_provider, **cfg.content_provider_config)
    except Exception as err:
        raise ValueError(f"config: content provider unavailable ({err})") from err
    if min(cfg.poll_interval, cfg.heartbeat_interval, cfg.tool_timeout) <= 0:
        raise ValueError("config: intervals and timeout must be positive")
    cfg.role=raw.get('role','')
    cfg.profile_sha256=raw.get('profile_sha256','')
    cfg.processing_profile=raw.get('processing_profile') or {}
    provider_timeout=raw.get('provider_timeout_seconds',30.0)
    if (type(provider_timeout) not in (int,float) or not math.isfinite(provider_timeout)
            or not 1 <= provider_timeout <= 300):
        raise ValueError('config: provider_timeout_seconds must be a finite number from 1 to 300')
    cfg.provider_timeout_seconds=float(provider_timeout)
    if cfg.profile_sha256 and (not _DRAFT_HASH.fullmatch(cfg.profile_sha256) or cfg.role not in ('recognizer','narrator') or not cfg.processing_profile):
        raise ValueError('config: invalid bound processing profile')
    if cfg.role == "ingester":
        from .ingest.common import parse_roots
        cfg.ingest_roots = parse_roots(raw.get("ingest_roots"))
    elif raw.get("ingest_roots") is not None:
        raise ValueError("config: ingest_roots is only valid for role=ingester")
    return cfg


def _parse_sampling_limits(raw: dict) -> SamplingLimits | None:
    block = raw.get("sampling_limits")
    if block is None:
        return None
    if not isinstance(block, dict):
        raise ValueError("config: sampling_limits must be an object")
    unknown = set(block) - set(_SAMPLING_INTS) - set(_SAMPLING_FLOATS)
    if unknown:
        raise ValueError(f"config: sampling_limits has unknown fields: {', '.join(sorted(unknown))}")
    kwargs: dict[str, Any] = {}
    for key in _SAMPLING_INTS:
        if key not in block:
            continue
        value = block[key]
        if type(value) is not int or value <= 0:
            raise ValueError(f"config: sampling_limits.{key} must be a positive integer")
        kwargs[key] = value
    for key in _SAMPLING_FLOATS:
        if key not in block:
            continue
        value = block[key]
        if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
            raise ValueError(f"config: sampling_limits.{key} must be a positive number")
        kwargs[key] = float(value)
    return SamplingLimits(**kwargs)


def _approval_hashes(payload: dict) -> tuple[str, ...]:
    if "narration_approvals" not in payload or payload["narration_approvals"] is None:
        return ()
    raw = payload["narration_approvals"]
    if not isinstance(raw, list) or any(not isinstance(item, str) or not _DRAFT_HASH.match(item) for item in raw):
        raise StageFailure("narration approvals from the control plane are malformed")
    return tuple(raw)


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
                if err.code in ("lease_expired", "not_found", "invalid_state", "conflict"):
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


def _publish_package(staging: Path, target: Path, beat: _Heartbeat) -> None:
    """Retry transient Windows sharing failures without replacing a package."""
    for attempt in range(8):
        if beat.lost:
            raise StageFailure("lease lost before publishing package")
        if target.exists():
            raise StageFailure("output package already exists")
        try:
            staging.rename(target)
            return
        except PermissionError:
            if attempt == 7:
                raise
            time.sleep(0.05 * (attempt + 1))


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
            task_input = client.call("get_task_input", {"task_id": task_id})
            if not isinstance(task_input, dict):
                task_input = {}
            from .preparation import configured_task
            cfg = configured_task(cfg, task_input)
            if task_input.get("versioned") is True:
                if task_input.get("cancelled") is True:
                    raise StageFailure("workflow cancelled this task")
                mode = task_input.get("content_mode")
                if mode and ((mode == "builtin") != (cfg.content_provider == "builtin")):
                    raise StageFailure("workflow content mode does not match Worker provider")
                edl = task_input["edl"]
                edl_rel = task_input.get("edl_path")
                approvals = _approval_hashes(task_input)
            else:
                asset_payload = client.call("get_asset", {"asset_id": asset_id})
                asset = asset_payload["asset"]
                approvals = _approval_hashes(asset_payload)
                edl = client.call("get_asset_edl", {"asset_id": asset_id})["edl"]
                edl_rel = (asset.get("artifacts") or {}).get("edl")
            edl_path = resolve_under_root(cfg.delivery_root, edl_rel)
            target = cfg.delivery_root.joinpath(*out_rel.split("/"))
            receipt = {"task_id": task_id, "revision_id": task_input.get("revision_id"), "content_mode": task_input.get("content_mode")}
            if task_input.get('profile_sha256'):
                receipt['profile_sha256']=task_input['profile_sha256']
            if target.exists():
                if task_input.get("versioned") is True:
                    prior = json.loads((target / "worker-receipt.json").read_text(encoding="utf-8"))
                    if prior != receipt:
                        raise StageFailure("existing delivery belongs to a different task input")
                    client.call("submit_delivery", {"task_id": task_id, "package_dir": out_rel})
                    return {"outcome": "succeeded", "package": out_rel, "recovered": True}
                raise StageFailure(f"output package already exists: {out_rel}")
            target.parent.mkdir(parents=True, exist_ok=True)
            staging = target.parent / f".{task_id}-staging"
            if staging.exists():
                shutil.rmtree(staging)
            staging.mkdir()

            def progress(p, m):
                if not beat.lost:
                    client.call("report_progress", {"task_id": task_id, "progress": p, "message": f"{kind}: {m}"})

            work_dir = staging / ".work"
            work_dir.mkdir(parents=True, exist_ok=True)

            ctx = StageContext(edl=edl, source_dir=edl_path.parent, out_dir=staging,
                               tools=Tools(ffmpeg=cfg.ffmpeg, ffprobe=cfg.ffprobe, timeout=cfg.tool_timeout),
                               progress=progress, content_provider=make_content_provider(cfg.content_provider, **cfg.content_provider_config),
                               work_dir=work_dir, sampling_limits=cfg.sampling_limits,
                               tts_auto_approve=cfg.tts_auto_approve, narration_approvals=approvals, tts_voice=cfg.tts_voice)
            summary = stages[kind](ctx)
            if work_dir.exists():
                shutil.rmtree(work_dir, ignore_errors=True)
            if beat.lost:
                return {"outcome": "abandoned", "reason": "lease lost during stage"}
            if task_input.get("versioned") is True:
                (staging / "worker-receipt.json").write_text(json.dumps(receipt), encoding="utf-8")
            _publish_package(staging, target, beat)
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
    from .preparation import local_capability
    capability=local_capability(cfg)
    heartbeat_args={'capability':capability} if capability else {}
    client.call("heartbeat", heartbeat_args)
    last_heartbeat=time.monotonic()
    log("worker registered; polling")
    while not stop.is_set():
        if capability and time.monotonic()-last_heartbeat>=5:
            capability=local_capability(cfg)
            try:
                client.call('heartbeat',{'capability':capability})
            except (ToolError, TransportError) as err:
                log(f'capability heartbeat error: {err}')
                stop.wait(cfg.poll_interval)
                continue
            last_heartbeat=time.monotonic()
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
