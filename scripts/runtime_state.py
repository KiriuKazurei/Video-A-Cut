"""Offline SQLite pre-start guard/backup, called while the supervisor owns the data lock.

Never migrates, restores, deletes business data, or imports worker/model code.
"""
from __future__ import annotations

import argparse
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import time
import os
import re
import datetime


class StateError(ValueError):
    """A fixed operator message, safe to include in a diagnostic report."""


def guard_legacy_session(directory: Path) -> None:
    """Refuse reuse if the older PowerShell session still owns live processes.

    This reads identities only; it never stops a PID from a historical journal.
    """
    def read_plain(path: Path):
        for parent in (path, *path.parents):
            if parent.exists() and os.lstat(parent).st_file_attributes & 0x400:
                raise StateError("Previous session path contains a reparse point.")
        return json.loads(path.read_text(encoding="utf-8-sig"))

    active = directory / "active-session.json"
    if not active.exists():
        return
    if os.name != "nt":
        raise StateError("Legacy Windows process identities require a Windows runtime.")
    import ctypes
    from ctypes import wintypes
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
    kernel.OpenProcess.restype = wintypes.HANDLE
    kernel.CloseHandle.argtypes = [wintypes.HANDLE]
    kernel.QueryFullProcessImageNameW.argtypes = [wintypes.HANDLE, wintypes.DWORD, wintypes.LPWSTR, ctypes.POINTER(wintypes.DWORD)]
    kernel.GetProcessTimes.argtypes = [wintypes.HANDLE, *([ctypes.POINTER(wintypes.FILETIME)] * 4)]
    kernel.GetExitCodeProcess.argtypes = [wintypes.HANDLE, ctypes.POINTER(wintypes.DWORD)]
    def alive(entry):
        pid = entry.get("pid") if isinstance(entry, dict) else None
        if not isinstance(pid, int) or pid < 1:
            raise StateError("Previous session process identity is incomplete.")
        handle = kernel.OpenProcess(0x1000, False, pid)
        if not handle:
            if ctypes.get_last_error() == 87:
                return False
            raise StateError("Cannot verify previous session process identity.")
        try:
            exit_code = wintypes.DWORD()
            if not kernel.GetExitCodeProcess(handle, ctypes.byref(exit_code)):
                raise StateError("Cannot verify previous session process state.")
            if exit_code.value != 259:
                return False
            if not entry.get("path") or not entry.get("started_at"):
                raise StateError("Previous live process identity is incomplete.")
            name = ctypes.create_unicode_buffer(32768)
            size = wintypes.DWORD(len(name))
            times = [wintypes.FILETIME() for _ in range(4)]
            if not kernel.QueryFullProcessImageNameW(handle, 0, name, ctypes.byref(size)) or not kernel.GetProcessTimes(handle, *[ctypes.byref(t) for t in times]):
                raise StateError("Cannot verify previous session process identity.")
            created = ((times[0].dwHighDateTime << 32) | times[0].dwLowDateTime) // 10
            stamp = datetime.datetime.fromisoformat(entry["started_at"].replace("Z", "+00:00"))
            epoch = datetime.datetime(1601, 1, 1, tzinfo=datetime.timezone.utc)
            delta = stamp.astimezone(datetime.timezone.utc) - epoch
            recorded = (delta.days * 86400 + delta.seconds) * 1_000_000 + delta.microseconds
            return name.value.casefold() == entry["path"].casefold() and created == recorded
        finally:
            kernel.CloseHandle(handle)
    previous = read_plain(active)
    session = previous.get("session_id", "")
    if not re.fullmatch(r"[a-f0-9]{32}", session):
        raise StateError("Previous session identifier is invalid.")
    if previous.get("status") == "running" and alive(previous.get("supervisor")):
        raise StateError("Previous supervisor is still alive; stop its session first.")
    owned = directory / "sessions" / session / "owned-processes.json"
    if owned.exists():
        entries = read_plain(owned)
        if isinstance(entries, dict):
            entries = [entries]
        if any(alive(entry) for entry in entries):
            raise StateError("Previous session still has live processes; review its identities before reuse.")


def prepare(database: Path, supported_schemas: list[int], backup: Path) -> dict:
    if not database.exists():
        return {"status": "new_database", "schema_version": 0, "backup": None}
    # Read-only URI handles Windows paths and special characters without SQL interpolation.
    deadline = time.monotonic() + 45
    with closing(sqlite3.connect(database.absolute().as_uri() + "?mode=ro", uri=True, timeout=5)) as source:
        source.set_progress_handler(lambda: int(time.monotonic() > deadline), 10000)
        versions = [row[0] for row in source.execute("SELECT version FROM schema_migrations ORDER BY version")]
        if any(not isinstance(v, int) or v not in supported_schemas for v in versions):
            raise StateError("Database schema is not supported by this version; keep its matching program and backup.")
        if source.execute("PRAGMA quick_check").fetchall() != [("ok",)]:
            raise StateError("Database consistency check failed; automatic startup is blocked.")
        # Reserve a new destination, never replace an existing business backup.
        with backup.open("xb"):
            pass
        with closing(sqlite3.connect(backup, timeout=5)) as target:
            target.set_progress_handler(lambda: int(time.monotonic() > deadline), 10000)
            backup_deadline = min(deadline, time.monotonic() + 30)

            def bound_backup(_status: int, _remaining: int, _total: int) -> None:
                if time.monotonic() > backup_deadline:
                    raise StateError("Database backup exceeded its time budget; preserve the store and review it offline.")

            source.backup(target, pages=256, progress=bound_backup, sleep=0.1)
            if target.execute("PRAGMA quick_check").fetchall() != [("ok",)]:
                raise StateError("Pre-start backup consistency check failed; startup is blocked.")
    return {"status": "backed_up", "schema_version": max(versions, default=0), "backup": str(backup)}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--database", required=True, type=Path)
    parser.add_argument("--supported-schemas", required=True)
    parser.add_argument("--backup", required=True, type=Path)
    parser.add_argument("--data-directory", type=Path)
    args = parser.parse_args()
    try:
        supported_schemas = [int(value) for value in args.supported_schemas.split(",")]
        if not supported_schemas or any(value < 1 for value in supported_schemas):
            raise ValueError("Invalid supported migrations")
    except ValueError:
        parser.error("supported-schemas must be a comma-separated list of positive migration versions")
    try:
        if args.data_directory:
            guard_legacy_session(args.data_directory)
        print(json.dumps(prepare(args.database, supported_schemas, args.backup)))
    except (ValueError, OSError, sqlite3.Error) as error:
        # Avoid emitting SQLite/raw path/provider diagnostics into shared logs.
        message = str(error) if isinstance(error, StateError) else "Cannot read or back up this SQLite store; verify permissions, free space and the migration ledger."
        print(json.dumps({"status": "blocked", "error_type": type(error).__name__, "message": message}))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
