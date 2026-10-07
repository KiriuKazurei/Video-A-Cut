"""Independent protocol golden + real SQLite migration shape, no on-disk database."""
import hashlib
import json
import sqlite3
from pathlib import Path

root = Path(__file__).resolve().parent.parent
protocol = root / "contracts/delivery-variant/v1"
golden = json.loads((protocol / "fixtures/input.json").read_text(encoding="utf-8"))
i = golden["input"]
assert hashlib.sha256((protocol / "specs/ass-basic-v1.json").read_bytes()).hexdigest() == i["spec_sha256"]
atoms = ["vac.delivery-variant.input.v1", "1"] + [i[k] for k in (
    "asset_id", "run_id", "revision_id", "parent_package_ref", "edl_sha256",
    "parent_manifest_sha256", "format", "spec_id", "spec_sha256")]
atoms.append(str(len(i["source_files"])))
for f in sorted(i["source_files"], key=lambda item: item["path"].encode("utf-8")):
    atoms.extend([f["kind"], f["path"], f["sha256"], str(f["size"])])
h = hashlib.sha256()
for atom in atoms:
    b = atom.encode("utf-8")
    h.update(str(len(b)).encode("ascii") + b":" + b)
assert h.hexdigest() == golden["expected_sha256"] == i["input_sha256"]
with sqlite3.connect(":memory:") as db:
    db.execute("PRAGMA foreign_keys=ON")
    migrations = sorted((root / "control-plane/internal/store/migrations").glob("*.sql"))
    for migration in migrations:
        db.executescript(migration.read_text(encoding="utf-8"))
    columns = {row[1] for row in db.execute("PRAGMA table_info(delivery_variants)")}
    assert {"variant_id", "task_id", "input_sha256", "spec_sha256", "output_dir", "invalidated"} <= columns
    assert not {"status", "progress", "message"} & columns
    assert not list(db.execute("PRAGMA foreign_key_check"))
print(json.dumps({"status": "passed", "golden_sha256": h.hexdigest(), "migration_count": len(migrations), "database": "memory_only"}))
