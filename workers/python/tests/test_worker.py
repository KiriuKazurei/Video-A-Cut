from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from vac_worker.content import VisionContentProvider, make_content_provider
from vac_worker.mcp import Client, ToolError
from vac_worker.stages import StageFailure
from vac_worker.worker import Config, load_config, process_task, resolve_under_root


class FakeClient:
    def __init__(self, handlers):
        self.handlers = handlers
        self.calls = []

    def call(self, tool, args=None):
        self.calls.append((tool, args))
        h = self.handlers.get(tool)
        return h(args) if h else {"ok": True}


def cfg(root: Path) -> Config:
    return Config(mcp_url="http://x/mcp", token="t" * 40, delivery_root=root, heartbeat_interval=60)


def fixture(tmp: Path):
    (tmp / "src").mkdir(exist_ok=True)
    (tmp / "src" / "edl.json").write_text("{}")
    return {
        "get_asset": lambda a: {"asset": {"artifacts": {"edl": "src/edl.json"}}},
        "get_asset_edl": lambda a: {"edl": {"timeline": {}}},
    }


TASK = {"task_id": "t1", "asset_id": "clip", "type": "narrate"}


class WorkerTest(unittest.TestCase):
    def test_success_publishes_package_and_submits(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            client = FakeClient(fixture(root))

            def stage(ctx):
                (ctx.out_dir / "edl.json").write_text("{}")
                (ctx.out_dir / "delivery-manifest.json").write_text("{}")
                return {"n": 1}

            r = process_task(client, cfg(root), TASK, lambda m: None, stages={"narrate": stage})
            self.assertEqual(r["outcome"], "succeeded")
            self.assertTrue((root / "stages" / "clip" / "t1" / "edl.json").is_file())
            self.assertIn(("submit_delivery", {"task_id": "t1", "package_dir": "stages/clip/t1"}), client.calls)
            self.assertFalse(any(p.name.endswith("-staging") for p in (root / "stages" / "clip").iterdir()))

    def test_stage_failure_reports_and_cleans_staging(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            client = FakeClient(fixture(root))

            def stage(ctx):
                (ctx.out_dir / "partial").write_text("x")
                raise StageFailure("boom")

            r = process_task(client, cfg(root), TASK, lambda m: None, stages={"narrate": stage})
            self.assertEqual(r["outcome"], "failed")
            self.assertEqual([c for c in client.calls if c[0] == "fail_task"][0][1]["reason"], "StageFailure: boom")
            self.assertEqual(list((root / "stages" / "clip").iterdir()), [])
            self.assertFalse(any(c[0] == "submit_delivery" for c in client.calls))

    def test_lease_expired_abandons_without_fail(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            h = fixture(root)

            def expired(a):
                raise ToolError("submit_delivery", "lease_expired: lapsed")

            h["submit_delivery"] = expired
            client = FakeClient(h)
            stage = lambda ctx: ((ctx.out_dir / "edl.json").write_text("{}"), {})[1]
            r = process_task(client, cfg(root), TASK, lambda m: None, stages={"narrate": stage})
            self.assertEqual(r["outcome"], "abandoned")
            self.assertFalse(any(c[0] == "fail_task" for c in client.calls))

    def test_unsafe_ids_and_types_fail(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            for task in ({**TASK, "type": "export"}, {**TASK, "asset_id": ".."}, {**TASK, "task_id": "a/b"}):
                client = FakeClient(fixture(root) if task["asset_id"] != ".." else {})
                r = process_task(client, cfg(root), task, lambda m: None, stages={"narrate": lambda c: {}})
                self.assertEqual(r["outcome"], "failed", task)

    def test_resolve_under_root(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            fixture(root)
            self.assertEqual(resolve_under_root(root, "src/edl.json"), root / "src" / "edl.json")
            for bad in ("../x", "/abs", "src\\edl.json", "C:/x", "missing.json", None):
                with self.assertRaises(StageFailure):
                    resolve_under_root(root, bad)

    def test_config(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            p = root / "w.json"
            good = {"mcp_url": "http://x/mcp", "delivery_root": str(root)}
            p.write_text(json.dumps(good))
            with self.assertRaisesRegex(ValueError, "VAC_WORKER_TOKEN"):
                load_config(str(p), env={})
            self.assertEqual(load_config(str(p), env={"VAC_WORKER_TOKEN": "x" * 40}).delivery_root, root)
            p.write_text(json.dumps({**good, "output_prefix": "../up"}))
            with self.assertRaisesRegex(ValueError, "output_prefix"):
                load_config(str(p), env={"VAC_WORKER_TOKEN": "x" * 40})
            p.write_text(json.dumps({**good, "delivery_root": "rel"}))
            with self.assertRaisesRegex(ValueError, "absolute"):
                load_config(str(p), env={"VAC_WORKER_TOKEN": "x" * 40})
            p.write_text(json.dumps({**good, "content_provider": "remote-unconfigured"}))
            with self.assertRaisesRegex(ValueError, "unavailable"):
                load_config(str(p), env={"VAC_WORKER_TOKEN": "x" * 40})

    def test_client_maps_is_error(self):
        class Resp:
            def __init__(self, body):
                self.body = body

            def read(self):
                return self.body

            def __enter__(self):
                return self

            def __exit__(self, *a):
                return False

        body = json.dumps({"jsonrpc": "2.0", "id": 1, "result": {"isError": True, "content": [{"type": "text", "text": "not_found: x"}]}}).encode()
        c = Client("http://x/mcp", "t" * 40, opener=lambda req, timeout: Resp(body))
        with self.assertRaises(ToolError) as ctx:
            c.call("get_task_status", {"task_id": "x"})
        self.assertEqual(ctx.exception.code, "not_found")
        with self.assertRaises(ValueError):
            Client("http://x", "short")


    def test_worker_load_config_with_content_provider_config(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            cfg_path = tmp / "config.json"
            cfg_path.write_text(json.dumps({
                "mcp_url": "http://localhost:8080/mcp",
                "delivery_root": str(tmp.resolve()),
                "content_provider": "vision",
                "content_provider_config": {
                    "endpoint": "https://api.vision.local/v1/scenes",
                    "allow_external": True,
                    "timeout_seconds": 15.0,
                    "model": "gpt-4o"
                }
            }))
            cfg = load_config(cfg_path, env={"VAC_WORKER_TOKEN": "token-12345"})
            self.assertEqual(cfg.content_provider, "vision")
            self.assertEqual(cfg.content_provider_config, {
                "endpoint": "https://api.vision.local/v1/scenes",
                "allow_external": True,
                "timeout_seconds": 15.0,
                "model": "gpt-4o"
            })
            provider = make_content_provider(cfg.content_provider, **cfg.content_provider_config)
            self.assertIsInstance(provider, VisionContentProvider)
            self.assertEqual(provider.endpoint, "https://api.vision.local/v1/scenes")
            self.assertTrue(provider.allow_external)
            self.assertEqual(provider.timeout_seconds, 15.0)
            self.assertEqual(provider.model, "gpt-4o")


if __name__ == "__main__":
    unittest.main()
