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


class StateError(ValueError):
    """A fixed operator message, safe to include in a diagnostic report."""


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
    args = parser.parse_args()
    try:
        supported_schemas = [int(value) for value in args.supported_schemas.split(",")]
        if not supported_schemas or any(value < 1 for value in supported_schemas):
            raise ValueError("Invalid supported migrations")
    except ValueError:
        parser.error("supported-schemas must be a comma-separated list of positive migration versions")
    try:
        print(json.dumps(prepare(args.database, supported_schemas, args.backup)))
    except (ValueError, OSError, sqlite3.Error) as error:
        # Avoid emitting SQLite/raw path/provider diagnostics into shared logs.
        message = str(error) if isinstance(error, StateError) else "Cannot read or back up this SQLite store; verify permissions, free space and the migration ledger."
        print(json.dumps({"status": "blocked", "error_type": type(error).__name__, "message": message}))
        raise SystemExit(1)


if __name__ == "__main__":
    main()
