"""Check the preview blueprint against real migrations, entirely in memory."""
from pathlib import Path
import json
import sqlite3


def main():
    root = Path(__file__).resolve().parent.parent
    migrations = sorted((root / "control-plane/internal/store/migrations").glob("*.sql"))
    with sqlite3.connect(":memory:") as db:
        db.execute("PRAGMA foreign_keys = ON")
        for migration in migrations:
            db.executescript(migration.read_text(encoding="utf-8"))
        existing = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
        present = {"preview_jobs", "preview_artifacts"} & existing
        if present and len(present) != 2:
            raise RuntimeError("Partial preview migration")
        if not present:
            db.executescript((root / "contracts/mp4-preview/v1/preview-store.sql").read_text(encoding="utf-8"))
        columns = {row[1] for row in db.execute("PRAGMA table_info(preview_jobs)")}
        required = {"preview_id", "task_id", "asset_id", "run_id", "revision_id", "request_id", "input_json", "input_sha256", "preset_sha256", "invalidated", "output_dir", "manifest_sha256"}
        if not required <= columns:
            raise RuntimeError("Missing fixed-input or result binding columns")
        if {"status", "progress", "message"} & columns:
            raise RuntimeError("Preview must project Task state rather than duplicate it")
        if list(db.execute("PRAGMA foreign_key_check")):
            raise RuntimeError("Invalid foreign keys")
        print(json.dumps({"status": "passed", "migration_count": len(migrations), "database": "memory_only", "source": "existing_migration" if present else "blueprint"}))


if __name__ == "__main__":
    main()
