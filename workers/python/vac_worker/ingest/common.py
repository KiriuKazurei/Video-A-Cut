"""Shared plumbing for ingester tasks: root mapping, execution context,
checkpoints, controlled files and cancellable subprocesses."""
from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable

WORKER_VERSION = "vac-ingester/3"
EXECUTION_PROTOCOL = 2
OPERATIONS = ("media_probe", "segment", "media_prepare")
_HEX64 = re.compile(r"^[0-9a-f]{64}$")
_ROOT_ID = re.compile(r"^[a-z0-9][a-z0-9_-]{0,63}$")
_DEVICE = re.compile(r"(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9]|conin\$|conout\$)(\..*)?$")
_ABS = re.compile(r"(?i)([a-z]:[\\/]|\\\\|/(home|users|mnt|tmp|var)/)[^\s\"'<>|]*")
FILE_ATTRIBUTE_REPARSE_POINT = 0x400


class IngestFailure(Exception):
    """A task-level failure with an actionable, path-free reason."""


class Cancelled(Exception):
    """The execution lost its lease, was cancelled or the worker is stopping."""


def sanitize(text: str, *secrets: str) -> str:
    for s in secrets:
        if s:
            text = text.replace(s, "<path>").replace(s.replace("\\", "/"), "<path>")
    return _ABS.sub("<path>", text)[:1500]


# ------------------------------------------------------------------ roots

def normalize_root(path: str) -> str:
    """Same textual form as the control plane's NormalizeRootPath."""
    return os.path.normpath(path).replace("\\", "/").lower()


def roots_fingerprint(roots: dict[str, Path]) -> str:
    text = "".join(f"{rid}\t{normalize_root(str(roots[rid]))}\n" for rid in sorted(roots))
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def parse_roots(raw) -> dict[str, Path]:
    if not isinstance(raw, list) or not raw:
        raise ValueError("config: ingest_roots must be a non-empty list of {root_id, path}")
    roots: dict[str, Path] = {}
    for item in raw:
        if not isinstance(item, dict) or set(item) - {"root_id", "path", "name"}:
            raise ValueError("config: ingest_roots entries are {root_id, path[, name]}")
        rid, path = item.get("root_id"), item.get("path")
        if not isinstance(rid, str) or not _ROOT_ID.match(rid) or rid in roots:
            raise ValueError("config: ingest root ids must be unique [a-z0-9_-]")
        if not isinstance(path, str) or not os.path.isabs(path) or path.startswith(("\\\\", "//")):
            raise ValueError(f"config: ingest root {rid} must be a local absolute directory")
        if not Path(path).is_dir():
            raise ValueError(f"config: ingest root {rid} does not exist")
        roots[rid] = Path(path)
    return roots


def clean_relative(rel: str) -> list[str]:
    if not isinstance(rel, str) or not rel or len(rel) > 1024:
        raise IngestFailure("relative_path is invalid")
    rel = rel.replace("\\", "/")
    if rel.startswith("/") or any(c in rel for c in ':*?"<>|') or any(ord(c) < 0x20 or ord(c) == 0x7F for c in rel):
        raise IngestFailure("relative_path is not a confined relative path")
    parts = rel.split("/")
    for p in parts:
        if p in ("", ".", "..") or p.endswith((" ", ".")) or _DEVICE.match(p):
            raise IngestFailure("relative_path has an unsafe segment")
    return parts


def _is_link(st: os.stat_result) -> bool:
    attrs = getattr(st, "st_file_attributes", 0)
    return stat.S_ISLNK(st.st_mode) or bool(attrs & FILE_ATTRIBUTE_REPARSE_POINT)


def resolve_source(roots: dict[str, Path], root_id: str, rel: str) -> tuple[Path, os.stat_result]:
    """Walk the relative path without following links and return the file."""
    if root_id not in roots:
        raise IngestFailure("source root is not configured on this ingester")
    base = Path(os.path.realpath(roots[root_id]))
    cur = base
    parts = clean_relative(rel)
    st = None
    for i, part in enumerate(parts):
        cur = cur / part
        try:
            st = os.lstat(cur)
        except FileNotFoundError:
            raise IngestFailure("the recording no longer exists in its root") from None
        if _is_link(st):
            raise IngestFailure("the recording path crosses a symlink or junction")
        last = i == len(parts) - 1
        if not last and not stat.S_ISDIR(st.st_mode):
            raise IngestFailure("the recording path has a non-directory segment")
        if last and not stat.S_ISREG(st.st_mode):
            raise IngestFailure("the recording path is not a regular file")
    real = Path(os.path.realpath(cur))
    if base != real and base not in real.parents:
        raise IngestFailure("the recording path escapes its root")
    return real, st


# ------------------------------------------------------------------ files

def sha256_file(path: Path, chunk: int = 8 << 20, cancel: Callable[[], None] | None = None) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while True:
            if cancel:
                cancel()
            buf = f.read(chunk)
            if not buf:
                break
            h.update(buf)
    return h.hexdigest()


def dump_json(path: Path, value) -> bytes:
    raw = (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + ".tmp")
    with open(tmp, "wb") as f:
        f.write(raw)
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)
    return raw


def link_or_copy(src: Path, dst: Path) -> None:
    dst.parent.mkdir(parents=True, exist_ok=True)
    if dst.exists():
        dst.unlink()
    try:
        os.link(src, dst)
    except OSError:
        shutil.copy2(src, dst)


def is_hex64(v) -> bool:
    return isinstance(v, str) and bool(_HEX64.match(v))


def safe_rmtree(path: Path, root: Path) -> None:
    """Remove an owned tree. Refuse reparse points and paths that escape ``root``."""
    from .supervise import CleanupBlocked
    if not path.exists() and not path.is_symlink():
        return
    root_real = Path(os.path.realpath(root))
    try:
        top = os.lstat(path)
    except FileNotFoundError:
        return
    if _is_link(top):
        raise CleanupBlocked("cleanup refused: reparse point")
    for dirpath, dirnames, filenames in os.walk(path, topdown=True, followlinks=False):
        for name in list(dirnames) + list(filenames):
            child = Path(dirpath) / name
            try:
                st = os.lstat(child)
            except FileNotFoundError:
                continue
            if _is_link(st):
                raise CleanupBlocked("cleanup refused: reparse point")
            real = Path(os.path.realpath(child))
            if real != root_real and root_real not in real.parents:
                raise CleanupBlocked("cleanup refused: path escapes the delivery root")
        kept = []
        for name in dirnames:
            child = Path(dirpath) / name
            try:
                if _is_link(os.lstat(child)):
                    raise CleanupBlocked("cleanup refused: reparse point")
            except FileNotFoundError:
                continue
            kept.append(name)
        dirnames[:] = kept
    shutil.rmtree(path)


# ------------------------------------------------------------ subprocess

class Runner:
    """Runs one external tool at a time; cancellation or a deadline ends the
    child process tree by PID and waits for it to exit."""

    def __init__(self, cancel_event: threading.Event, secrets: tuple[str, ...] = (), registry=None,
                 grace: float = 5.0, force_wait: float = 10.0):
        self.cancel_event = cancel_event
        self.secrets = secrets
        self.registry = registry
        self.grace = grace
        self.force_wait = force_wait
        self.invocations: list[str] = []

    def _force(self, proc: subprocess.Popen) -> None:
        from .supervise import taskkill_tree
        if proc.poll() is not None:
            return
        if getattr(self, "job", None) and self.job.handle:
            self.job.terminate()
        else:
            taskkill_tree(int(proc.pid))

    def _wait_exit(self, proc: subprocess.Popen, seconds: float) -> bool:
        deadline = time.monotonic() + seconds
        while proc.poll() is None:
            left = deadline - time.monotonic()
            if left <= 0:
                return False
            # Cancellation is already set on this path; waiting on that Event
            # would spin at full CPU while the owned process drains.
            time.sleep(min(0.05, left))
        return True

    def run(self, args: list[str], timeout: float, stdout_sink: Callable[[bytes], None] | None = None,
            cwd: Path | None = None) -> tuple[bytes, str]:
        from .runtime import OwnedJob
        job = OwnedJob()
        self.job = job
        self.proc = None
        try:
            return self._run(args, timeout, stdout_sink, cwd)
        finally:
            try:
                job.close()
            except OSError as err:
                from .supervise import CleanupBlocked
                raise CleanupBlocked("owned job tree did not drain") from err
            else:
                if self.proc is not None and self.proc.poll() is not None and self.registry is not None:
                    self.registry.forget(int(self.proc.pid))
            finally:
                self.job = None

    def _run(self, args, timeout, stdout_sink, cwd):
        """Return (stdout, stderr_tail). ``stdout_sink`` streams stdout instead of buffering."""
        if self.cancel_event.is_set():
            raise Cancelled("execution cancelled")
        self.invocations.append(Path(args[0]).name)
        try:
            proc = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=cwd,
                                    creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0) | getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0) | (4 if os.name == "nt" else 0))
        except FileNotFoundError:
            raise IngestFailure(f"tool not found: {Path(args[0]).name}") from None
        self.proc = proc
        self.job.attach_and_resume(proc)
        if self.registry is not None:
            self.registry.note(int(proc.pid), self.job)
        out: list[bytes] = []
        err = bytearray()
        sink_errors = []

        def pump_err():
            for chunk in iter(lambda: proc.stderr.read(4096), b""):
                err.extend(chunk)
                if len(err) > 64 * 1024:
                    del err[: len(err) - 64 * 1024]

        def pump_out():
            for chunk in iter(lambda: proc.stdout.read(1 << 16), b""):
                if stdout_sink:
                    try:
                        stdout_sink(chunk)
                    except Exception as error:
                        sink_errors.append(error)
                        # Keep draining the pipe so the child cannot block on output.
                        for _ in iter(lambda: proc.stdout.read(1 << 16), b""):
                            pass
                        return
                else:
                    out.append(chunk)

        threads = [threading.Thread(target=pump_err, daemon=True), threading.Thread(target=pump_out, daemon=True)]
        for t in threads:
            t.start()
        deadline = time.monotonic() + timeout
        reason = ""
        while proc.poll() is None:
            if self.cancel_event.wait(0.2):
                reason = "cancelled"
                break
            if time.monotonic() > deadline:
                reason = "timeout"
                break
        if reason:
            if reason == "cancelled" and not self._wait_exit(proc, self.grace):
                self._force(proc)
            elif reason == "timeout":
                self._force(proc)
            if not self._wait_exit(proc, self.force_wait):
                from .supervise import CleanupBlocked
                raise CleanupBlocked("owned process did not exit before the force-wait deadline")
        # A tool can leave descendants holding its pipes after the root exits.
        # Drain its kernel job before joining readers or releasing the registry.
        try:
            self.job.close()
        except OSError as error:
            from .supervise import CleanupBlocked
            raise CleanupBlocked("owned job tree did not drain") from error
        for t in threads:
            t.join(timeout=10)
        proc.stdout.close()
        proc.stderr.close()
        tail = sanitize(err.decode("utf-8", "replace")[-600:], *self.secrets).strip()
        if sink_errors:
            raise sink_errors[0]
        if reason == "cancelled":
            raise Cancelled("execution cancelled; child process stopped")
        if reason == "timeout":
            raise IngestFailure(f"{Path(args[0]).stem} exceeded the {timeout:.0f}s step deadline")
        if proc.returncode != 0:
            raise IngestFailure(f"{Path(args[0]).stem} exited {proc.returncode}: {tail[-400:]}")
        return b"".join(out), tail


# --------------------------------------------------------------- context

@dataclass
class Exec:
    """One claimed execution of an ingest task."""

    client: object
    task_id: str
    inp: dict
    delivery_root: Path
    roots: dict[str, Path]
    ffmpeg: str
    ffprobe: str
    cancel_event: threading.Event
    log: Callable[[str], None]
    progress_floor: float = 0.0
    _seq: int = 0
    reused: list = field(default_factory=list)
    scope: dict | None = None
    lease: object | None = None
    halt: object | None = None
    registry: object | None = None
    gate: object | None = None

    def __post_init__(self):
        from .journal import Journal, JournalBlocked
        self.out_dir = self.under_root(self.inp["output_dir"])
        self.package_rel = self.inp["package_dir"]
        self.package_dir = self.under_root(self.package_rel)
        self.staging = self.out_dir / "package.staging"
        self.work = self.out_dir / "work"
        self.policy = self.inp["policy"]
        self.runner = Runner(self.cancel_event, (str(self.delivery_root), *[str(p) for p in self.roots.values()]), registry=self.registry)
        self._seq = len([c for c in self.inp.get("checkpoints") or [] if c.get("task_id") == self.task_id])
        scope = dict(self.scope or {})
        scope.setdefault("execution_id", self.inp.get("execution_id", ""))
        scope.setdefault("task_id", self.task_id)
        try:
            self.journal = Journal(self.out_dir, scope, {
                "input_sha256": self.inp.get("input_sha256", ""),
                "policy_sha256": self.inp.get("policy_sha256", ""),
                "worker_version": WORKER_VERSION,
            })
        except JournalBlocked:
            self.journal = None
            self._journal_blocked = True
        else:
            self._journal_blocked = bool(self.journal and self.journal.untrusted)

    def under_root(self, rel: str) -> Path:
        if not isinstance(rel, str) or not rel or rel.startswith("/") or "\\" in rel or ":" in rel or ".." in rel.split("/"):
            raise IngestFailure("controlled reference is not a root-relative path")
        path = self.delivery_root.joinpath(*rel.split("/"))
        if self.delivery_root not in path.parents:
            raise IngestFailure("controlled reference escapes the delivery root")
        for part in [path, *path.parents]:
            if part.is_symlink() or part.is_junction():
                raise IngestFailure("controlled reference contains a reparse point")
            if part == self.delivery_root:
                break
        return path

    def rel(self, path: Path) -> str:
        return path.relative_to(self.delivery_root).as_posix()

    def check(self) -> None:
        if self.lease is not None and self.lease.expired():
            if self.halt is not None:
                self.halt.stop("suspended_connection")
            self.cancel_event.set()
            raise Cancelled("local lease deadline reached")
        if self.cancel_event.is_set():
            reason = getattr(self.halt, "reason", "") or "execution cancelled"
            raise Cancelled(reason)

    def _identity(self, args: dict, *, artifact: bool = False) -> dict:
        """Flat execution identity. The server fills agent_id and role from the token."""
        body = {k: v for k, v in args.items() if k not in ("scope", "agent", "agent_id", "role")}
        if not self.scope:
            return body
        body["task_id"] = self.scope["task_id"]
        body["runtime_instance_id"] = self.scope["runtime_instance_id"]
        body["execution_id"] = self.scope["execution_id"]
        body["generation"] = int(self.scope["generation"])
        if artifact and self.inp.get("input_sha256"):
            body["input_sha256"] = self.inp["input_sha256"]
        return body

    def progress(self, p: float, msg: str) -> None:
        self.check()
        p = max(self.progress_floor, min(0.99, p))
        self.progress_floor = p
        from ..mcp import ToolError, transport_kind
        from .supervise import LOST_EXECUTION
        try:
            self.client.call("report_progress", self._identity({"task_id": self.task_id, "progress": round(p, 4), "message": msg[:200]}))
        except ToolError as err:
            if err.code in LOST_EXECUTION or err.code in ("conflict", "invalid_state", "not_found", "forbidden"):
                if self.halt is not None and err.code in LOST_EXECUTION:
                    self.halt.stop(err.code)
                self.cancel_event.set()
                raise Cancelled(f"progress rejected ({err.code})") from None
        except Exception as err:
            if transport_kind(err) == "transient":
                self.log(f"progress transport error: {err}")
                return
            if transport_kind(err) in ("auth", "protocol"):
                if self.halt is not None:
                    self.halt.stop("degraded")
                self.cancel_event.set()
                raise

    def check_space(self, need: int) -> None:
        free = shutil.disk_usage(self.delivery_root).free
        if free - need < int(self.policy["min_free_bytes"]):
            raise IngestFailure(f"insufficient disk space: need {need} bytes plus a {self.policy['min_free_bytes']} byte reserve")

    # ---------------------------------------------------------- checkpoints

    def prior_checkpoints(self, kind: str) -> list[tuple[dict, dict]]:
        """Verified (row, manifest) pairs for this stage and input, oldest first."""
        out = []
        for row in self.inp.get("checkpoints") or []:
            if row.get("kind") != kind or row.get("input_sha256") != self.inp["input_sha256"]:
                continue
            try:
                path = self.under_root(row["ref"])
                from .journal import JournalBlocked, read_bounded
                raw = read_bounded(path)
            except (OSError, IngestFailure, KeyError, JournalBlocked):
                self.log(f"checkpoint {row.get('sequence')} failed verification; redoing its work")
                continue
            if hashlib.sha256(raw).hexdigest() != row.get("sha256"):
                self.log(f"checkpoint {row.get('sequence')} failed verification; redoing its work")
                continue
            try:
                manifest = json.loads(raw)
            except ValueError:
                continue
            if manifest.get("input_sha256") != self.inp["input_sha256"]:
                continue
            out.append((row, manifest))
        return out

    def save_checkpoint(self, kind: str, item_index: int, body: dict) -> None:
        self.check()
        if self._journal_blocked:
            raise IngestFailure("recovery journal is unreadable")
        manifest = {"schema_version": 1, "kind": kind, "item_index": item_index, "execution_id": self.inp["execution_id"],
                    "input_sha256": self.inp["input_sha256"], **body}
        if len(json.dumps(manifest).encode("utf-8")) > (1 << 20) - 256:
            raise IngestFailure("checkpoint manifest exceeds 1 MiB")
        identity = {"task_id": self.task_id, "execution_id": self.inp["execution_id"], "input_sha256": self.inp["input_sha256"],
                    "kind": kind, "item_index": item_index, "body": body}
        pending = self.journal.pending_args("save_ingest_checkpoint") if self.journal else None
        from .journal import canonical_args
        if pending and pending.get("identity_sha256") == canonical_args(identity):
            seq = int(pending["sequence"])
            request_id = pending["request_id"]
        else:
            seq = self._seq + 1
            request_id = __import__("uuid").uuid4().hex
        path = self.out_dir / "checkpoints" / f"{seq:05d}-{kind}-{item_index}.json"
        raw = dump_json(path, manifest)
        digest = hashlib.sha256(raw).hexdigest()
        args = self._identity({
            "task_id": self.task_id, "execution_id": self.inp["execution_id"], "sequence": seq,
            "input_sha256": self.inp["input_sha256"], "kind": kind, "item_index": item_index,
            "ref": self.rel(path), "sha256": digest}, artifact=True)
        if self.journal and not (pending and pending.get("identity_sha256") == canonical_args(identity)):
            self.journal.data["pending_request"] = {
                "tool": "save_ingest_checkpoint", "request_id": request_id, "sequence": seq,
                "identity_sha256": canonical_args(identity), "args_sha256": canonical_args(args), "args": args,
                "state": "verified_local",
            }
            unit = f"{kind}:{item_index}"
            self.journal.note_unit(stage=self.inp.get("stage", ""), kind=kind, state="verified_local",
                                   sha256=digest, unit=unit, sequence=seq)
            for entry in self.journal.data["entries"]:
                if entry.get("unit") == unit and entry.get("kind") == kind:
                    entry["request_id"] = request_id
                    entry["ref"] = self.rel(path)
            self.journal.data["pending_request"]["args"] = args
            self.journal.save()
        elif self.journal:
            args = pending["args"]
        self._call_same("save_ingest_checkpoint", args)
        if self.journal:
            self.journal.mark_request(request_id, "registered")
        self._seq = max(self._seq, seq)

    # ------------------------------------------------------------- publish

    def fresh_staging(self) -> Path:
        if self.staging.exists():
            safe_rmtree(self.staging, self.delivery_root)
        self.staging.mkdir(parents=True)
        return self.staging

    def _call_same(self, tool: str, args: dict):
        """Send ``args`` unchanged across transient retries. Heartbeats are not blocked by the backoff."""
        from .supervise import retry_call
        result, _rtt = retry_call(self.client, tool, args, self.halt or _IdleHalt(self.cancel_event), self.lease, self.log, self.gate)
        if self.lease is not None and isinstance(result, dict) and result.get("lease_expires_at"):
            self.lease.observe_expires(str(result["lease_expires_at"]), _rtt)
        return result

    def _result_status(self, request_id: str) -> dict | None:
        if not self.scope:
            return None
        try:
            return self.client.call("get_execution_result_status", self._identity(
                {"request_id": request_id, "task_id": self.task_id, "execution_id": self.inp["execution_id"]}))
        except Exception as err:
            from ..mcp import transport_kind
            if transport_kind(err):
                self.log(f"result status unavailable: {err}")
                return None
            raise

    def receipt(self) -> dict:
        return {"schema_version": 1, "task_id": self.task_id, "execution_id": self.inp["execution_id"],
                "stage": self.inp["stage"], "input_sha256": self.inp["input_sha256"],
                "policy_sha256": self.inp["policy_sha256"], "worker_version": WORKER_VERSION}

    def publish(self) -> None:
        """Write the receipt, rename staging to package and register it."""
        self.check()
        dump_json(self.staging / "worker-receipt.json", self.receipt())
        files = []
        for path in sorted(self.staging.rglob("*")):
            self.check()
            if path.is_dir():
                if path.is_symlink() or path.is_junction():
                    raise IngestFailure("sealed package contains a reparse point")
                continue
            self.under_root(self.rel(path))
            files.append({"path": path.relative_to(self.staging).as_posix(), "size_bytes": path.stat().st_size,
                          "sha256": sha256_file(path, cancel=self.check)})
            if len(files) > 512:
                raise IngestFailure("sealed package exceeds file budget")
        dump_json(self.staging / "sealed-manifest.json", {"schema_version": 1, "receipt": self.receipt(), "files": files})
        if self.package_dir.exists():
            raise IngestFailure("package directory of this execution already exists")
        os.replace(self.staging, self.package_dir)
        if self.journal and not self._journal_blocked:
            self.journal.data["sealed"] = {"package_dir": self.package_rel, "state": "sealed"}
            self.journal.save()
        self.submit()

    def submit(self) -> None:
        from .journal import canonical_args
        logical = {"task_id": self.task_id, "execution_id": self.inp["execution_id"],
                   "package_dir": self.package_rel, "input_sha256": self.inp["input_sha256"]}
        pending = self.journal.pending_args("submit_ingest_result") if self.journal and not self._journal_blocked else None
        if pending and pending.get("identity_sha256") == canonical_args(logical) and pending.get("state") == "committed":
            return
        if pending and pending.get("identity_sha256") == canonical_args(logical) and isinstance(pending.get("args"), dict):
            args = pending["args"]
        else:
            args = self._identity({**logical, "request_id": __import__("uuid").uuid4().hex}, artifact=True)
            if self.journal and not self._journal_blocked:
                self.journal.data["pending_request"] = {
                    "tool": "submit_ingest_result", "request_id": args["request_id"], "sequence": None,
                    "identity_sha256": canonical_args(logical), "args_sha256": canonical_args(args),
                    "args": args, "state": "sealed",
                }
                self.journal.save()
        status = self._result_status(args["request_id"])
        if isinstance(status, dict) and status.get("status") == "committed":
            if self.journal:
                self.journal.mark_request(args["request_id"], "committed")
            return
        if isinstance(status, dict) and status.get("status") in ("obsolete", "conflict"):
            raise IngestFailure(f"submit {status.get('status')}")
        self._call_same("submit_ingest_result", args)
        if self.journal:
            self.journal.mark_request(args["request_id"], "committed")

    def note_recovery_candidates(self, candidates) -> int:
        """Record list_recovery_candidates rows. They carry no paths; checkpoint bytes stay on get_task_input."""
        noted = 0
        if not candidates:
            return 0
        if not isinstance(candidates, list):
            raise IngestFailure("recovery candidates are malformed")
        allowed = {"former_execution_id", "generation", "stage", "input_sha256", "checkpoint_count", "summary",
                   "task_id", "output_dir", "package_dir", "journal_ref", "policy_sha256"}
        for cand in candidates:
            if not isinstance(cand, dict) or not set(cand).issubset(allowed):
                raise IngestFailure("recovery candidate is not a controlled reference")
            former = cand.get("former_execution_id")
            if not isinstance(former, str) or not former:
                raise IngestFailure("recovery candidate has no former execution")
            if cand.get("stage") != self.inp.get("stage") or cand.get("input_sha256") != self.inp.get("input_sha256"):
                continue
            noted += 1
        return noted

    def recover(self, candidates=None) -> bool:
        """Register an already-published package of this execution, then note server recovery candidates."""
        if self._journal_blocked:
            raise IngestFailure("recovery journal is unreadable")
        self.note_recovery_candidates(candidates)
        self._recover_current_checkpoint()
        receipt = self.package_dir / "worker-receipt.json"
        if not receipt.is_file():
            return self._recover_former(candidates or [])
        try:
            from .journal import read_bounded
            prior = json.loads(read_bounded(receipt))
        except (OSError, ValueError):
            return False
        if prior != self.receipt():
            raise IngestFailure("existing package belongs to a different execution")
        self.submit()
        return True

    def _recover_current_checkpoint(self):
        """Finish an interrupted registration before advancing its sequence."""
        pending = self.journal.pending_args("save_ingest_checkpoint") if self.journal else None
        if not pending:
            return
        args = pending["args"]
        if args.get("execution_id") != self.inp["execution_id"] or args.get("task_id") != self.task_id:
            raise IngestFailure("pending checkpoint belongs to another execution")
        ref = args.get("ref", "")
        if not isinstance(ref, str) or not ref.startswith(self.inp["output_dir"] + "/checkpoints/"):
            raise IngestFailure("pending checkpoint has an uncontrolled reference")
        from .journal import read_bounded
        raw = read_bounded(self.under_root(ref))
        if hashlib.sha256(raw).hexdigest() != args.get("sha256"):
            self.log("pending local checkpoint failed verification; redoing its unit")
            return
        manifest = json.loads(raw)
        if manifest.get("input_sha256") != self.inp["input_sha256"] or manifest.get("execution_id") != self.inp["execution_id"]:
            raise IngestFailure("pending local checkpoint fingerprint changed")
        self._call_same("save_ingest_checkpoint", args)
        self.journal.mark_request(pending["request_id"], "registered")
        rows = self.inp.setdefault("checkpoints", [])
        if not any(row.get("ref") == ref for row in rows):
            rows.append(dict(args))
        self._seq = max(self._seq, int(args["sequence"]))

    def _recover_former(self, candidates):
        from .journal import read_bounded, JournalBlocked
        for candidate in candidates:
            if candidate.get("policy_sha256") != self.inp["policy_sha256"] or candidate.get("input_sha256") != self.inp["input_sha256"] or candidate.get("stage") != self.inp["stage"]:
                continue
            former = candidate["former_execution_id"]
            prefix = f"ingest/{self.inp['run_id']}/{candidate.get('task_id')}/{former}"
            if candidate.get("output_dir") != prefix or candidate.get("package_dir") != prefix + "/package" or candidate.get("journal_ref") != prefix + "/journal.v1.json":
                raise IngestFailure("recovery candidate reference does not match its controlled owner")
            try:
                journal = json.loads(read_bounded(self.under_root(candidate["journal_ref"])))
                if journal.get("input_sha256") != self.inp["input_sha256"] or journal.get("policy_sha256") != self.inp["policy_sha256"] or journal.get("worker_version") != WORKER_VERSION or journal.get("scope", {}).get("execution_id") != former:
                    continue
                # Preserve verified_local units that never reached the server.
                for entry in journal.get("entries", []):
                    ref = entry.get("ref")
                    if not ref or not ref.startswith(prefix + "/checkpoints/") or entry.get("kind") not in ("copy_chunk", "scan_chunk", "segment_media"):
                        continue
                    raw = read_bounded(self.under_root(ref))
                    if hashlib.sha256(raw).hexdigest() != entry.get("sha256"):
                        continue
                    manifest = json.loads(raw)
                    if manifest.get("input_sha256") != self.inp["input_sha256"] or manifest.get("execution_id") != former:
                        continue
                    if not any(c.get("ref") == ref for c in self.inp["checkpoints"]):
                        self.inp["checkpoints"].append({"task_id": candidate["task_id"], "execution_id": former,
                            "input_sha256": self.inp["input_sha256"], "kind": entry["kind"], "ref": ref,
                            "sha256": entry["sha256"], "sequence": entry.get("sequence", 0)})
                package = self.under_root(candidate["package_dir"])
                sealed = package / "sealed-manifest.json"
                if not sealed.is_file():
                    if journal.get("entries"):
                        self.journal.remember_recovered(former, "verified checkpoint units")
                    continue
                inventory = json.loads(read_bounded(sealed))
                expected = {**self.receipt(), "task_id": candidate["task_id"], "execution_id": former}
                if inventory.get("schema_version") != 1 or inventory.get("receipt") != expected:
                    continue
                rows = inventory.get("files")
                if not isinstance(rows, list) or not 1 <= len(rows) <= 512:
                    raise IngestFailure("invalid sealed inventory")
                staging = self.fresh_staging()
                seen = set()
                for item in rows:
                    relative = item["path"]
                    if relative in seen or relative == "sealed-manifest.json":
                        raise IngestFailure("duplicate sealed artifact")
                    seen.add(relative)
                    source = self.under_root(candidate["package_dir"] + "/" + relative)
                    target = self.under_root(self.rel(staging) + "/" + relative)
                    if source.stat().st_size != item["size_bytes"] or sha256_file(source, cancel=self.check) != item["sha256"]:
                        raise IngestFailure("former sealed artifact failed verification")
                    if relative == "worker-receipt.json":
                        continue
                    target.parent.mkdir(parents=True, exist_ok=True)
                    try:
                        os.link(source, target)
                    except OSError:
                        shutil.copyfile(source, target)
                self.journal.remember_recovered(former, "verified sealed package")
                self.reused.append({"former_execution_id": former, "kind": "sealed_package"})
            except FileNotFoundError:
                continue
            except (JournalBlocked, IngestFailure, ValueError, KeyError, TypeError) as err:
                self.log(f"former recovery manifest rejected: {type(err).__name__}")
                continue
            self.log(f"recovered sealed package from {former}; publishing under current scope")
            self.publish()
            return True
        return False


class _IdleHalt:
    """Adapt a bare Event so retry waits stay interruptible when no supervisor Halt was supplied."""

    def __init__(self, event: threading.Event):
        self.event = event
        self.reason = ""

    def cancelled(self) -> bool:
        return self.event.is_set()


def us_to_s(us: int) -> str:
    """Exact decimal seconds for ffmpeg arguments."""
    sign = "-" if us < 0 else ""
    us = abs(int(us))
    return f"{sign}{us // 1_000_000}.{us % 1_000_000:06d}"
