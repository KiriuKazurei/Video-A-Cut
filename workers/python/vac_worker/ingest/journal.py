"""Per-execution recovery journal.

The file is published by a temp write, flush, fsync and atomic replace.
It records scope, content hashes and the exact pending request. It never
stores tokens or absolute paths. A corrupt journal is left untouched.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import stat
import time
import uuid
from pathlib import Path

MAX_MANIFEST_BYTES = 1 << 20
JOURNAL_NAME = "journal.v1.json"
_STATES = ("verified_local", "registered", "sealed", "committed")
_ABS = re.compile(r"(?i)([a-z]:[\\/]|\\\\)")
FILE_ATTRIBUTE_REPARSE_POINT = 0x400


class JournalBlocked(Exception):
    """The on-disk journal cannot be trusted. Callers must not delete files."""


def _is_reparse(st: os.stat_result) -> bool:
    attrs = getattr(st, "st_file_attributes", 0)
    return stat.S_ISLNK(st.st_mode) or bool(attrs & FILE_ATTRIBUTE_REPARSE_POINT)


def assert_journal_safe(value) -> None:
    blob = json.dumps(value, ensure_ascii=False)
    lower = blob.lower()
    if "token" in lower or "bearer " in lower or "authorization" in lower:
        raise JournalBlocked("journal must not record secrets")
    if _ABS.search(blob):
        raise JournalBlocked("journal must not record absolute paths")


def read_bounded(path: Path, limit: int = MAX_MANIFEST_BYTES) -> bytes:
    """Stat first, then read at most ``limit`` bytes. Refuse links and oversize files."""
    st = os.lstat(path)
    if _is_reparse(st) or not stat.S_ISREG(st.st_mode):
        raise JournalBlocked("manifest is not a regular file")
    if st.st_size > limit:
        raise JournalBlocked("manifest exceeds 1 MiB")
    with open(path, "rb") as handle:
        raw = handle.read(limit + 1)
    if len(raw) > limit:
        raise JournalBlocked("manifest exceeds 1 MiB")
    return raw


def atomic_publish(path: Path, raw: bytes) -> None:
    if len(raw) > MAX_MANIFEST_BYTES:
        raise JournalBlocked("manifest exceeds 1 MiB")
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    with open(tmp, "wb") as handle:
        handle.write(raw)
        handle.flush()
        os.fsync(handle.fileno())
    # Windows readers/virus scanners can briefly hold a target without delete
    # sharing. Retry only this transient replace, never rewrite a valid journal.
    for attempt in range(6):
        try:
            os.replace(tmp, path)
            return
        except PermissionError:
            if attempt == 5:
                raise
            time.sleep(0.02 * (attempt + 1))


def canonical_args(args: dict) -> str:
    text = json.dumps(args, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


class Journal:
    def __init__(self, directory: Path, scope: dict, fingerprints: dict):
        self.path = directory / JOURNAL_NAME
        self.scope = {k: scope[k] for k in ("agent_id", "role", "runtime_instance_id", "task_id", "execution_id", "generation") if k in scope}
        self.fingerprints = fingerprints
        self.untrusted = False
        self.data = self._load()

    def _fresh(self) -> dict:
        return {
            "schema_version": 1,
            "scope": self.scope,
            "input_sha256": self.fingerprints.get("input_sha256", ""),
            "policy_sha256": self.fingerprints.get("policy_sha256", ""),
            "worker_version": self.fingerprints.get("worker_version", ""),
            "entries": [],
            "pending_request": None,
            "sealed": None,
        }

    def _load(self) -> dict:
        if not self.path.exists():
            return self._fresh()
        try:
            raw = read_bounded(self.path)
            data = json.loads(raw)
        except (OSError, ValueError, JournalBlocked):
            self.untrusted = True
            return self._fresh()
        if not isinstance(data, dict) or data.get("schema_version") != 1:
            self.untrusted = True
            return self._fresh()
        if data.get("scope") != self.scope or any(data.get(k) != self.fingerprints.get(k) for k in ("input_sha256", "policy_sha256", "worker_version")):
            self.untrusted = True
            return self._fresh()
        data.setdefault("entries", [])
        data.setdefault("pending_request", None)
        data.setdefault("sealed", None)
        return data

    def save(self) -> None:
        if self.untrusted:
            raise JournalBlocked("corrupt journal is left in place")
        assert_journal_safe(self.data)
        raw = (json.dumps(self.data, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
        atomic_publish(self.path, raw)

    def note_unit(self, *, stage: str, kind: str, state: str, sha256: str, unit: str, sequence: int | None = None) -> None:
        if state not in _STATES:
            raise JournalBlocked(f"unknown journal state {state}")
        entry = {"stage": stage, "kind": kind, "state": state, "sha256": sha256, "unit": unit}
        if sequence is not None:
            entry["sequence"] = sequence
        replaced = False
        for i, old in enumerate(self.data["entries"]):
            if old.get("unit") == unit and old.get("kind") == kind:
                self.data["entries"][i] = entry
                replaced = True
                break
        if not replaced:
            self.data["entries"].append(entry)
        self.save()

    def completed_units(self) -> int:
        return sum(1 for e in self.data["entries"] if e.get("state") in _STATES)

    def ensure_request(self, tool: str, args: dict, sequence: int | None = None) -> str:
        """Return the request id for this exact argument set. Same args keep the same id."""
        digest = canonical_args(args)
        pending = self.data.get("pending_request")
        if isinstance(pending, dict) and pending.get("tool") == tool and pending.get("args_sha256") == digest:
            return pending["request_id"]
        request_id = uuid.uuid4().hex
        self.data["pending_request"] = {
            "tool": tool, "request_id": request_id, "sequence": sequence, "args_sha256": digest, "args": args,
        }
        self.save()
        return request_id

    def pending_args(self, tool: str) -> dict | None:
        pending = self.data.get("pending_request")
        if isinstance(pending, dict) and pending.get("tool") == tool and isinstance(pending.get("args"), dict):
            return pending
        return None

    def mark_request(self, request_id: str, state: str) -> None:
        if state not in _STATES:
            raise JournalBlocked(f"unknown journal state {state}")
        pending = self.data.get("pending_request") or {}
        if pending.get("request_id") == request_id:
            pending["state"] = state
            self.data["pending_request"] = pending
        for entry in self.data["entries"]:
            if entry.get("request_id") == request_id:
                entry["state"] = state
        if state in ("sealed", "committed"):
            sealed = dict(self.data.get("sealed") or {})
            sealed["state"] = state
            sealed["request_id"] = request_id
            self.data["sealed"] = sealed
        self.save()

    def remember_recovered(self, former_execution_id: str, summary: str) -> None:
        self.data["recovered_from_execution_id"] = former_execution_id
        self.data["recovery_summary"] = summary[:200]
        self.save()
