"""Long-lived ingester supervisor.

Connection failures stay inside the task boundary. The daemon keeps polling
unless the process is stopped or the local configuration is unusable.
"""
from __future__ import annotations

import atexit
import os
import threading
import time
import uuid

from ..mcp import INGEST_REQUEST_TIMEOUT, Client, ToolError, transport_kind
from .common import OPERATIONS, WORKER_VERSION, EXECUTION_PROTOCOL, Cancelled, Exec, IngestFailure, roots_fingerprint, safe_rmtree, sanitize
from .prepare import media_prepare
from .probe import media_probe
from .segment import segment
from .supervise import (
    CleanupBlocked, FlightGate, Halt, LOST_EXECUTION, LocalLease, PhysicalLock, ProcessRegistry,
    backoff_delay, wait_interruptible,
)

HANDLERS = {"media_probe": media_probe, "segment": segment, "media_prepare": media_prepare}
REQUIRED_TOOLS = (
    "claim_task", "heartbeat", "report_progress", "get_task_input", "save_ingest_checkpoint",
    "submit_ingest_result", "fail_task",
    "begin_execution", "get_execution_control", "ack_execution_stopped", "get_execution_result_status",
    "list_recovery_candidates",
    "reconcile_execution_drain",
)
_DEGRADED_SECONDS = 30.0
_instances: list[ProcessRegistry] = []


def _tool_ready(path: str) -> bool:
    import subprocess
    try:
        done = subprocess.run([path, "-version"], capture_output=True, timeout=20,
                              creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        return done.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def capability(cfg) -> dict:
    return {"roots_sha256": roots_fingerprint(cfg.ingest_roots), "ffmpeg_ready": _tool_ready(cfg.ffmpeg),
            "ffprobe_ready": _tool_ready(cfg.ffprobe), "operations": list(OPERATIONS),
            "worker_version": WORKER_VERSION, "execution_protocol": EXECUTION_PROTOCOL}


def _session_stopped() -> bool:
    path = os.environ.get("VAC_SESSION_STOP", "")
    return bool(path) and os.path.exists(path)


def _install_exit_hook(registry: ProcessRegistry) -> None:
    if registry in _instances:
        return
    _instances.append(registry)

    def _stop() -> None:
        registry.stop_owned(Halt(), grace=0.0, force_wait=2.0)

    atexit.register(_stop)


def _wire(ident: dict, **extra) -> dict:
    """Top-level execution identity. agent_id and role stay on the server token."""
    body = {
        "task_id": ident["task_id"],
        "runtime_instance_id": ident["runtime_instance_id"],
        "execution_id": ident["execution_id"],
        "generation": int(ident["generation"]),
    }
    for key, value in extra.items():
        if key in ("scope", "agent", "agent_id", "role", "wait_seconds"):
            continue
        body[key] = value
    return body


def _apply_lease(lease: LocalLease, payload, rtt: float) -> None:
    if lease is None or not isinstance(payload, dict):
        return
    if payload.get("lease_remaining_ms") is not None:
        lease.observe(float(payload["lease_remaining_ms"]) / 1000.0, rtt)
    elif payload.get("lease_expires_at"):
        lease.observe_expires(str(payload["lease_expires_at"]), rtt)


class _Lease:
    """Renews the lease and polls execution control. Submit retries do not share this thread."""

    def __init__(self, client: Client, scope: dict, lease: LocalLease, halt: Halt, log, worker_stop: threading.Event, gate: FlightGate, interval: float, capability: dict):
        self.halt = halt
        self.lease = lease
        self._stop = threading.Event()
        self._t = threading.Thread(target=self._run, args=(client, scope, log, worker_stop, gate, interval, capability), daemon=True)

    def _run(self, client, scope, log, worker_stop, gate: FlightGate, interval: float, capability: dict):
        next_beat = time.monotonic() + interval
        known = int(scope.get("control_version") or 0)
        while not self._stop.is_set():
            if worker_stop.is_set() or _session_stopped():
                self.halt.stop("worker_stop")
                return
            if self.lease.expired():
                self.lease.suspended = True
                self.halt.stop("suspended_connection")
                log(f"task {scope.get('task_id')}: local lease deadline reached; suspending")
                return
            if not gate.acquire(self.halt, 0.2):
                if self.halt.cancelled():
                    return
                continue
            try:
                control = client.call("get_execution_control", _wire(scope, known_control_version=known))
                if isinstance(control, dict) and control.get("control_version") is not None:
                    known = int(control["control_version"])
                    scope["control_version"] = known
                if isinstance(control, dict) and control.get("command") == "stop":
                    log(f"task {scope.get('task_id')}: control stop ({control.get('reason') or 'cancelled'})")
                    self.halt.stop("cancelled")
                    return
                if isinstance(control, dict) and control.get("status") in ("succeeded", "failed"):
                    return  # A committed result must not be mistaken for loss of its lease.
                if time.monotonic() >= next_beat:
                    started = time.monotonic()
                    beat = client.call("heartbeat", _wire(scope, ingest_capability=capability))
                    rtt = time.monotonic() - started
                    next_beat = time.monotonic() + interval
                    _apply_lease(self.lease, beat, rtt)
            except ToolError as err:
                if err.code in LOST_EXECUTION:
                    self.halt.stop(err.code)
                    log(f"task {scope.get('task_id')}: lease lost ({err.code}); stopping this execution")
                    return
                log(f"task {scope.get('task_id')}: control error {err}")
            except Exception as err:
                kind = transport_kind(err)
                if kind == "transient":
                    log(f"task {scope.get('task_id')}: control transport error {err}")
                elif kind in ("auth", "protocol"):
                    self.halt.stop("degraded")
                    log(f"task {scope.get('task_id')}: control {kind} error {err}")
                    return
                else:
                    log(f"task {scope.get('task_id')}: control error {type(err).__name__}")
            finally:
                gate.release()
            if wait_interruptible(self._stop, 0.5):
                return

    def __enter__(self):
        self._t.start()
        return self

    def __exit__(self, *exc):
        self._stop.set()
        self._t.join(timeout=5)


def _identity_from(claim: dict, task: dict, instance_id: str) -> dict | None:
    if claim.get("execution_protocol") != EXECUTION_PROTOCOL:
        return None
    raw = claim.get("scope")
    if not isinstance(raw, dict):
        return None
    ident = {
        "agent_id": str(raw.get("agent_id") or ""),
        "role": str(raw.get("role") or ""),
        "runtime_instance_id": str(raw.get("runtime_instance_id") or ""),
        "task_id": str(raw.get("task_id") or task.get("task_id") or ""),
        "execution_id": str(raw.get("execution_id") or ""),
        "generation": int(raw.get("generation") or 0),
        "control_version": int(claim.get("control_version") or 0),
    }
    if not ident["task_id"] or not ident["execution_id"] or ident["runtime_instance_id"] != instance_id:
        return None
    return ident


def _ack_stopped(client, scope: dict, halt: Halt, lease: LocalLease, log, outcome: str) -> None:
    from .supervise import retry_call
    if outcome not in ("stopped", "cleanup_blocked"):
        outcome = "stopped"
    args = _wire(scope, outcome=outcome)
    # The execution halt is already set. The ack still has to leave on its own budget.
    ack_halt = Halt()
    try:
        retry_call(client, "ack_execution_stopped", args, ack_halt, None, log, None)
    except Exception as err:
        log(f"stop ack incomplete: {type(err).__name__}")


def _fail(client, task_id: str, reason: str, halt: Halt, cfg, scope: dict | None) -> dict:
    reason = sanitize(reason, str(cfg.delivery_root), *[str(p) for p in cfg.ingest_roots.values()])
    if scope:
        args = _wire(scope, reason=reason[:1900])
        args["task_id"] = task_id
    else:
        args = {"task_id": task_id, "reason": reason[:1900]}
    try:
        client.call("fail_task", args)
        return {"outcome": "failed", "reason": reason}
    except Exception as err:
        return {"outcome": "fail_report_error", "reason": f"{reason}; report: {err}"}


def _preserved(ex: Exec | None) -> int:
    if ex is None or getattr(ex, "journal", None) is None:
        return 0
    return ex.journal.completed_units()


def process_ingest(client: Client, cfg, task: dict, log, stop: threading.Event, scope: dict | None = None,
                   instance_id: str | None = None, registry: ProcessRegistry | None = None) -> dict:
    task_id, kind = task["task_id"], task["type"]
    scope = scope or task.get("_scope")
    halt = Halt()
    lease = LocalLease()
    _apply_lease(lease, {
        "lease_remaining_ms": task.get("_lease_remaining_ms"),
        "lease_expires_at": task.get("_lease_expires_at"),
    }, float(task.get("_lease_rtt") or 0))
    gate = FlightGate()
    registry = registry or ProcessRegistry(cfg.delivery_root, instance_id or uuid.uuid4().hex)
    if registry.cleanup_blocked:
        return {"outcome": "cleanup_blocked", "reason": "previous owned cleanup did not finish"}
    _install_exit_hook(registry)
    ex: Exec | None = None
    lock = None
    stop_outcome = None
    conditional_ack = False
    registry.bind(scope or {})
    try:
        if not scope:
            return {"outcome": "blocked", "reason": "protocol incompatible: claim did not return an execution scope"}
        if kind not in HANDLERS:
            raise IngestFailure(f"unsupported ingest task type {kind}")
        cap = dict(capability(cfg))
        cap["execution_protocol"] = EXECUTION_PROTOCOL
        with _Lease(client, scope, lease, halt, log, stop, gate, getattr(cfg, "heartbeat_interval", 5.0), cap):
            payload = client.call("get_task_input", _wire(scope))
            if payload.get("cancelled") is True:
                halt.stop("cancelled")
                stop_outcome = "stopped"
                return {"outcome": "abandoned", "reason": "ingest run cancelled this task"}
            inp = payload.get("ingest")
            if payload.get("input_kind") != "media_ingest" or not isinstance(inp, dict) or inp.get("stage") != kind:
                raise IngestFailure("task input is not a media_ingest input for this stage")
            if inp.get("execution_id") and inp.get("execution_id") != scope["execution_id"]:
                halt.stop("stale_execution")
                return {"outcome": "abandoned", "reason": "refusing to adopt a different execution id"}
            if inp.get("roots_sha256") != roots_fingerprint(cfg.ingest_roots):
                raise IngestFailure("ingest root mapping differs from the control plane")
            keys = list(inp.get("resource_keys") or [])
            if isinstance(inp.get("resource_key"), str):
                keys.append(inp["resource_key"])
            if not keys:
                raise IngestFailure("control plane did not provide a stable source resource key")
            locks = []
            for key in sorted(set(keys)):
                safe = __import__("hashlib").sha256(str(key).encode("utf-8")).hexdigest()
                item = PhysicalLock(cfg.delivery_root / ".vac-runtime" / f"{safe}.lock",
                                    {"runtime_instance_id": scope["runtime_instance_id"], "execution_id": scope["execution_id"], "marker": registry.marker},
                                    registry)
                state = item.try_acquire()
                if state != "acquired":
                    for held in reversed(locks):
                        held.release()
                    return {"outcome": "waiting_resource" if state == "busy" else "cleanup_blocked",
                            "reason": "资源被有效执行占用" if state == "busy" else "清理受阻"}
                locks.append(item)
            lock = locks
            if inp.get("previous_execution_id"):
                try:
                    client.call("reconcile_execution_drain", _wire(scope, former_execution_id=inp["previous_execution_id"]))
                except ToolError as err:
                    if err.code in ("conflict", "resource_busy"):
                        return {"outcome": "waiting_resource", "reason": "等待旧执行退出"}
                    raise
            try:
                begin = client.call("begin_execution", _wire(scope))
            except ToolError as err:
                if err.code in ("conflict", "resource_busy"):
                    return {"outcome": "waiting_resource", "reason": "等待旧执行退出"}
                raise
            if not isinstance(begin, dict) or begin.get("ok") is not True:
                return {"outcome": "waiting_resource", "reason": "等待旧执行退出"}
            ex = Exec(client=client, task_id=task_id, inp=inp, delivery_root=cfg.delivery_root, roots=cfg.ingest_roots,
                      ffmpeg=cfg.ffmpeg, ffprobe=cfg.ffprobe, cancel_event=halt.event, log=log,
                      scope=scope, lease=lease, halt=halt, registry=registry, gate=gate)
            listed = client.call("list_recovery_candidates", _wire(scope))
            candidates = listed.get("candidates") if isinstance(listed, dict) else None
            if ex.recover(candidates or []):
                return {"outcome": "succeeded", "package": ex.package_rel, "recovered": True}
            summary = HANDLERS[kind](ex)
            if ex.work.exists():
                safe_rmtree(ex.work, cfg.delivery_root)
            return {"outcome": "succeeded", "package": ex.package_rel, "summary": summary}
    except Cancelled as err:
        reason = halt.reason or str(err)
        if reason == "suspended_connection" or lease.suspended or lease.expired():
            state = registry.stop_owned(halt, grace=0)
            stop_outcome = "cleanup_blocked" if state == "cleanup_blocked" else "stopped"
            return {"outcome": "suspended_connection", "reason": "连接中断，暂停并保留完成单元", "preserved_units": _preserved(ex)}
        if reason in LOST_EXECUTION or reason in ("cancelled", "worker_stop", "stale_execution"):
            state = registry.stop_owned(halt) if registry.pids or halt.cancelled() else "stopped"
            stop_outcome = "cleanup_blocked" if state == "cleanup_blocked" else "stopped"
            if state == "cleanup_blocked":
                return {"outcome": "cleanup_blocked", "reason": "清理受阻"}
            return {"outcome": "abandoned", "reason": reason}
        return {"outcome": "abandoned", "reason": reason}
    except CleanupBlocked:
        registry.cleanup_blocked = True
        stop_outcome = "cleanup_blocked"
        return {"outcome": "cleanup_blocked", "reason": "清理受阻", "preserved_units": _preserved(ex)}
    except ToolError as err:
        if err.code in LOST_EXECUTION or halt.reason in LOST_EXECUTION:
            state = registry.stop_owned(halt)
            stop_outcome = "cleanup_blocked" if state == "cleanup_blocked" else "stopped"
            if state == "cleanup_blocked":
                return {"outcome": "cleanup_blocked", "reason": "清理受阻"}
            return {"outcome": "abandoned", "reason": err.code}
        if halt.cancelled():
            return {"outcome": "abandoned", "reason": halt.reason or str(err)}
        return _fail(client, task_id, str(err), halt, cfg, scope)
    except Exception as err:
        kind = transport_kind(err)
        if kind == "transient":
            state = registry.stop_owned(halt, grace=0)
            stop_outcome = "cleanup_blocked" if state == "cleanup_blocked" else "stopped"
            conditional_ack = not lease.expired()
            return {"outcome": "suspended_connection", "reason": "连接中断，暂停并保留完成单元", "preserved_units": _preserved(ex)}
        if kind in ("auth", "protocol") or halt.reason == "degraded":
            return {"outcome": "blocked", "reason": sanitize(str(err))}
        if isinstance(err, (IngestFailure, OSError, KeyError, ValueError, TypeError)):
            if halt.cancelled():
                return {"outcome": "abandoned", "reason": halt.reason or "cancelled"}
            return _fail(client, task_id, f"{type(err).__name__}: {err}", halt, cfg, scope)
        if halt.cancelled():
            return {"outcome": "abandoned", "reason": halt.reason or "cancelled"}
        log(f"task {task_id}: isolated {type(err).__name__}: {err}")
        return _fail(client, task_id, f"{type(err).__name__}: {err}", halt, cfg, scope)
    finally:
        if lock:
            for item in reversed(lock):
                item.release()
        if stop_outcome and scope:
            registry.queue_ack(scope, stop_outcome, conditional=conditional_ack)
            registry.flush_acks(client, log)


def run_ingest_loop(client: Client, cfg, log, stop: threading.Event, once: bool = False) -> dict:
    if getattr(client, "_timeout", INGEST_REQUEST_TIMEOUT) > INGEST_REQUEST_TIMEOUT:
        client._timeout = INGEST_REQUEST_TIMEOUT
    instance_id = uuid.uuid4().hex
    registry = ProcessRegistry(cfg.delivery_root, instance_id)
    _install_exit_hook(registry)
    orphan = registry.recover_orphans()
    if orphan == "cleanup_blocked":
        log("leftover owned processes could not be stopped; new executions stay blocked")
    startup_attempt = 0
    while not stop.is_set() and not _session_stopped():
        try:
            tools = client.list_tools()
            break
        except Exception as err:
            kind = transport_kind(err)
            log(f"startup connection {kind or 'error'}: {err}")
            if once:
                return {"outcome": "blocked" if kind in ("auth", "protocol") else "suspended_connection"}
            wait_interruptible(stop, getattr(cfg, "degraded_interval", _DEGRADED_SECONDS) if kind in ("auth", "protocol") else backoff_delay(startup_attempt))
            startup_attempt += 1
    else:
        return {"outcome": "stopped"}
    missing = [t for t in REQUIRED_TOOLS if t not in tools]
    degraded_for = getattr(cfg, "degraded_interval", _DEGRADED_SECONDS)
    if missing:
        log(f"execution protocol incompatible; missing {', '.join(missing)}")
        if once:
            return {"outcome": "blocked", "reason": "protocol incompatible"}
        while not stop.is_set() and not _session_stopped():
            if wait_interruptible(stop, degraded_for):
                break
            try:
                tools = client.list_tools()
            except Exception as err:
                log(f"protocol probe {transport_kind(err) or type(err).__name__}: {err}")
                continue
            if all(t in tools for t in REQUIRED_TOOLS):
                missing = []
                break
        if missing:
            return {"outcome": "blocked", "reason": "protocol incompatible"}
    cap = capability(cfg)
    if not cap["ffmpeg_ready"] or not cap["ffprobe_ready"]:
        log("ffmpeg/ffprobe not runnable; reporting capability without them")
    try:
        client.call("heartbeat", {"ingest_capability": cap, "runtime_instance_id": instance_id})
    except Exception as err:
        if transport_kind(err) in ("auth", "protocol"):
            log(f"capability heartbeat rejected ({transport_kind(err)}); degraded")
            if once:
                return {"outcome": "blocked", "reason": str(err)}
        else:
            log(f"capability heartbeat error: {err}")
    last = time.monotonic()
    log(f"ingester registered instance {instance_id}; polling")
    claim_request = uuid.uuid4().hex
    attempt = 0
    while not stop.is_set() and not _session_stopped():
        registry.flush_acks(client, log)
        if registry.cleanup_blocked:
            log("cleanup blocked; not starting a new execution")
            if once:
                return {"outcome": "cleanup_blocked"}
            if wait_interruptible(stop, degraded_for):
                break
            continue
        if time.monotonic() - last >= 30:
            cap = capability(cfg)
            try:
                client.call("heartbeat", {"ingest_capability": cap, "runtime_instance_id": instance_id})
                last = time.monotonic()
            except Exception as err:
                kind = transport_kind(err)
                log(f"capability heartbeat error: {err}")
                wait_interruptible(stop, degraded_for if kind in ("auth", "protocol") else cfg.poll_interval)
                if kind in ("auth", "protocol") and once:
                    return {"outcome": "blocked", "reason": str(err)}
                continue
        try:
            started = time.monotonic()
            claim = client.call("claim_task", {
                "request_id": claim_request,
                "runtime_instance_id": instance_id,
            })
            claim_rtt = time.monotonic() - started
            attempt = 0
        except Exception as err:
            kind = transport_kind(err)
            log(f"claim error: {err}")
            if kind in ("auth", "protocol"):
                if once:
                    return {"outcome": "blocked", "reason": str(err)}
                wait_interruptible(stop, degraded_for)
                continue
            if once:
                return {"outcome": "suspended_connection", "reason": "连接中断，暂停并保留完成单元", "preserved_units": 0}
            if wait_interruptible(stop, backoff_delay(attempt)):
                break
            attempt += 1
            continue
        if not claim.get("claimed"):
            claim_request = uuid.uuid4().hex
            if once:
                return {"outcome": "idle"}
            stop.wait(cfg.poll_interval)
            continue
        if claim.get("execution_protocol") != EXECUTION_PROTOCOL:
            log(f"refusing execution_protocol {claim.get('execution_protocol')}")
            claim_request = uuid.uuid4().hex
            if once:
                return {"outcome": "blocked", "reason": "execution protocol does not match this worker"}
            wait_interruptible(stop, degraded_for)
            continue
        task = claim["task"]
        scope = _identity_from(claim, task, instance_id)
        task = dict(task)
        task["_scope"] = scope
        if claim.get("lease_remaining_ms") is not None:
            task["_lease_remaining_ms"] = claim.get("lease_remaining_ms")
            task["_lease_rtt"] = claim_rtt
        elif claim.get("lease_expires_at"):
            task["_lease_expires_at"] = claim.get("lease_expires_at")
            task["_lease_rtt"] = claim_rtt
        log(f"claimed {task['task_id']} ({task['type']}) for {task.get('asset_id')}")
        try:
            result = process_ingest(client, cfg, task, log, stop, scope=scope, instance_id=instance_id, registry=registry)
        except Exception as err:
            log(f"task {task['task_id']}: isolated {type(err).__name__}")
            result = {"outcome": "isolated", "reason": sanitize(str(err), str(cfg.delivery_root))}
        log(f"task {task['task_id']}: {result['outcome']}" + (f" - {result.get('reason')}" if result.get("reason") else ""))
        claim_request = uuid.uuid4().hex
        try:
            client.call("heartbeat", {"ingest_capability": cap, "runtime_instance_id": instance_id})
            last = time.monotonic()
        except Exception:
            pass
        if result.get("outcome") in ("blocked", "suspended_connection") and not once:
            wait_interruptible(stop, degraded_for if result.get("outcome") == "blocked" else min(degraded_for, backoff_delay(0)))
        elif result.get("outcome") in ("waiting_resource", "cleanup_blocked") and not once:
            wait_interruptible(stop, cfg.poll_interval)
        if once:
            return result
    return {"outcome": "stopped"}
