"""Per-instance ownership records and actual cross-process resource locks."""
from __future__ import annotations
import json
import os
import re
import threading
import time
import uuid
from pathlib import Path
from .runtime import matches_process, process_identity


def _read(path: Path) -> dict:
    if path.is_symlink() or path.is_junction() or path.stat().st_size > (1 << 20):
        raise ValueError("unsafe runtime record")
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError("invalid runtime record")
    return value


def _write(path: Path, value: dict):
    path.parent.mkdir(parents=True, exist_ok=True)
    if any(p.is_symlink() or p.is_junction() for p in [path, *path.parents]):
        raise ValueError("runtime path contains a reparse point")
    raw = (json.dumps(value, ensure_ascii=False) + "\n").encode("utf-8")
    if len(raw) > (1 << 20):
        raise ValueError("runtime record is too large")
    temporary = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    with open(temporary, "xb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)


class ProcessRegistry:
    def __init__(self, root: Path, instance_id: str):
        if not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", instance_id):
            raise ValueError("invalid runtime instance")
        self.root, self.instance_id = root, instance_id
        self.marker = uuid.uuid4().hex
        self.dir = root / ".vac-runtime"
        self.path = self.dir / "instances" / f"{instance_id}.json"
        self.owner = process_identity(os.getpid())
        if not self.owner:
            raise ValueError("cannot establish worker process identity")
        self.entries, self.pids, self.scopes, self.pending_acks = [], [], [], []
        self.jobs = {}
        self.cleanup_blocked = False
        self._guard = threading.RLock()
        self._publish()

    def _publish(self):
        with self._guard:
            _write(self.path, {"schema_version": 2, "runtime_instance_id": self.instance_id, "marker": self.marker,
                              "owner": self.owner, "entries": self.entries, "pids": self.pids,
                              "scopes": self.scopes, "pending_acks": self.pending_acks,
                              "cleanup_blocked": self.cleanup_blocked})

    def note(self, pid: int, job=None):
        from .supervise import CleanupBlocked
        identity = process_identity(pid)
        if not identity:
            # A very short-lived subprocess may already have exited. It owns no live tree.
            return
        with self._guard:
            self.entries.append({**identity, "job_enforced": bool(job and job.handle)})
            self.pids.append(pid)
            if job:
                self.jobs[pid] = job
            self._publish()

    def forget(self, pid):
        with self._guard:
            self.pids = [p for p in self.pids if p != pid]
            self.jobs.pop(pid, None)
            self.entries = [e for e in self.entries if e["pid"] != pid]
            self._publish()

    def known(self, pid):
        return any(e["pid"] == pid and matches_process(e) for e in self.entries)

    def bind(self, scope):
        if scope not in self.scopes:
            self.scopes.append(dict(scope))
            self.scopes = self.scopes[-512:]
            self._publish()

    def queue_ack(self, scope, outcome, conditional=False):
        row = {"scope": dict(scope), "outcome": outcome, "conditional": conditional}
        self.pending_acks = [r for r in self.pending_acks if r["scope"]["execution_id"] != scope["execution_id"]] + [row]
        self._publish()

    def flush_acks(self, client, log):
        from .loop import _wire
        for row in list(self.pending_acks):
            try:
                if row.get("conditional"):
                    control = client.call("get_execution_control", _wire(row["scope"], known_control_version=0))
                    if control.get("status") in ("succeeded", "failed") and control.get("command") != "stop":
                        self.pending_acks.remove(row)
                        self._publish()
                        continue
                    if control.get("command") != "stop":
                        continue
                client.call("ack_execution_stopped", _wire(row["scope"], outcome=row["outcome"]))
            except Exception as err:
                log(f"stop acknowledgement pending: {type(err).__name__}")
                return
            self.pending_acks.remove(row)
            self._publish()

    def recover_orphans(self):
        from .supervise import taskkill_tree
        folder = self.dir / "instances"
        paths = list(folder.glob("*.json"))
        if len(paths) > 1024:
            self.cleanup_blocked = True
            return "cleanup_blocked"
        for path in paths:
            if path == self.path:
                continue
            try:
                record = _read(path)
                if record.get("schema_version") != 2 or not record.get("owner"):
                    raise ValueError("unknown owner")
                if matches_process(record["owner"]):
                    continue  # A live instance is never an orphan, even before server claim.
                for entry in record.get("entries", []):
                    if matches_process(entry):
                        if not entry.get("job_enforced"):
                            raise ValueError("cannot prove ownership of descendant tree")
                        # Kernel job teardown normally already ended this tree; signal only a verified surviving root.
                        taskkill_tree(entry["pid"])
                deadline = time.monotonic() + 10
                while any(matches_process(e) for e in record.get("entries", [])):
                    if time.monotonic() >= deadline:
                        raise ValueError("owned media tree still alive")
                    time.sleep(0.05)
                record["drained"] = True
                _write(path, record)
            except (ValueError, OSError, KeyError, TypeError):
                self.cleanup_blocked = True
                self._publish()
                return "cleanup_blocked"
        # Legacy PID-only records are never trusted to kill a process.
        legacy = self.dir / "owned.json"
        if legacy.exists():
            try:
                if _read(legacy).get("pids"):
                    self.cleanup_blocked = True
            except (ValueError, OSError):
                self.cleanup_blocked = True
        self._publish()
        return "cleanup_blocked" if self.cleanup_blocked else "clear"

    def stop_owned(self, halt, grace=5.0, force_wait=10.0):
        from .supervise import taskkill_tree
        halt.stop(halt.reason or "cancelled")
        try:
            if not self._wait_dead(grace, halt):
                for entry in list(self.entries):
                    if matches_process(entry):
                        job = self.jobs.get(entry["pid"])
                        if job and job.handle:
                            job.terminate()
                        else:
                            taskkill_tree(entry["pid"])
            self.cleanup_blocked = not self._wait_dead(force_wait, halt)
        except OSError:
            # An inaccessible owner is unknown, not proof that its tree died.
            self.cleanup_blocked = True
        self._publish()
        return "cleanup_blocked" if self.cleanup_blocked else "stopped"

    def _wait_dead(self, seconds, halt):
        deadline = time.monotonic() + seconds
        while any(matches_process(e) for e in self.entries):
            if time.monotonic() >= deadline:
                return False
            time.sleep(0.05)  # halt is already set; waiting on it would spin.
        self.pids = []
        return True


class PhysicalLock:
    """Kernel byte-range lock. The owner JSON is diagnostic and never the lock."""
    def __init__(self, path, owner, registry=None):
        self.path, self.owner, self.registry, self.fd = path, owner, registry, None
        self.description = path.with_name(path.name + ".owner.json")

    def try_acquire(self):
        self.path.parent.mkdir(parents=True, exist_ok=True)
        if any(p.is_symlink() or p.is_junction() for p in [self.path, *self.path.parents]):
            return "blocked"
        fd = os.open(self.path, os.O_CREAT | os.O_RDWR, 0o600)
        try:
            if os.fstat(fd).st_size == 0:
                os.write(fd, b"0")
            os.lseek(fd, 0, os.SEEK_SET)
            if os.name == "nt":
                import msvcrt
                msvcrt.locking(fd, msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            os.close(fd)
            return "busy"
        self.fd = fd
        try:
            if self.description.exists():
                old = _read(self.description)
                if not old.get("released") and old.get("runtime_instance_id") != self.owner.get("runtime_instance_id"):
                    instance = str(old.get("runtime_instance_id") or "")
                    if not self.registry or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", instance):
                        raise ValueError("unknown lock owner")
                    record = _read(self.registry.dir / "instances" / f"{instance}.json")
                    if matches_process(record.get("owner", {})) or any(matches_process(e) for e in record.get("entries", [])):
                        raise ValueError("previous owner has not drained")
                    if not record.get("drained"):
                        if self.registry.recover_orphans() != "clear":
                            raise ValueError("previous owner recovery blocked")
            _write(self.description, {**self.owner, "pid": os.getpid(), "released": False})
        except (ValueError, OSError, TypeError):
            self._unlock()
            return "blocked"
        return "acquired"

    def _unlock(self):
        if self.fd is not None:
            os.lseek(self.fd, 0, os.SEEK_SET)
            if os.name == "nt":
                import msvcrt
                msvcrt.locking(self.fd, msvcrt.LK_UNLCK, 1)
            else:
                import fcntl
                fcntl.flock(self.fd, fcntl.LOCK_UN)
            os.close(self.fd)
            self.fd = None

    def release(self):
        if self.fd is None:
            return
        try:
            _write(self.description, {**self.owner, "pid": os.getpid(), "released": True})
        finally:
            self._unlock()
