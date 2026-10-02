from __future__ import annotations

import base64
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch
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
    def test_publish_retries_transient_sharing_failure_without_rerunning_stage(self):
        with tempfile.TemporaryDirectory() as t:
            root = Path(t).resolve()
            client = FakeClient(fixture(root))
            attempts = []
            runs = []
            original = Path.rename

            def rename(path, target):
                attempts.append(path)
                if len(attempts) == 1:
                    raise PermissionError("temporary Windows sharing violation")
                return original(path, target)

            def stage(ctx):
                runs.append(ctx)
                (ctx.out_dir / "edl.json").write_text("{}")
                return {"n": 1}

            with patch.object(Path, "rename", rename), patch("vac_worker.worker.time.sleep"):
                result = process_task(client, cfg(root), TASK, lambda _: None, {"narrate": stage})
            self.assertEqual(result["outcome"], "succeeded")
            self.assertEqual(len(runs), 1)
            self.assertEqual(len(attempts), 2)

    def test_publish_retry_refuses_existing_package_and_lost_lease(self):
        from vac_worker.worker import _publish_package
        from types import SimpleNamespace
        with tempfile.TemporaryDirectory() as t:
            root = Path(t)
            staging = root / "staging"
            staging.mkdir()
            target = root / "published"

            def raced(path, destination):
                target.mkdir()
                (target / "history").write_text("preserve")
                raise PermissionError("package appeared during retry")

            with patch.object(Path, "rename", raced), patch("vac_worker.worker.time.sleep"):
                with self.assertRaises(StageFailure):
                    _publish_package(staging, target, SimpleNamespace(lost=False))
            self.assertEqual((target / "history").read_text(), "preserve")
            with self.assertRaises(StageFailure):
                _publish_package(staging, root / "new", SimpleNamespace(lost=True))
            self.assertTrue(staging.is_dir())

    def test_versioned_publish_crash_recovers_without_rerunning_stage(self):
        with tempfile.TemporaryDirectory() as t:
            root=Path(t).resolve();handlers=fixture(root)
            handlers['get_task_input']=lambda _: {'versioned':True,'content_mode':'builtin','revision_id':'rev1','edl':{},'edl_path':'src/edl.json'}
            dest=root/'stages/clip/t1';dest.mkdir(parents=True)
            (dest/'worker-receipt.json').write_text(json.dumps({'task_id':'t1','revision_id':'rev1','content_mode':'builtin'}),encoding='utf-8')
            client=FakeClient(handlers)
            result=process_task(client,cfg(root),TASK,lambda _:None,stages={'narrate':lambda _:self.fail('published stage reran')})
            self.assertTrue(result['recovered'])
            self.assertTrue(any(name=='submit_delivery' for name,_ in client.calls))
    def test_versioned_configured_mode_refuses_builtin_worker(self):
        with tempfile.TemporaryDirectory() as t:
            root=Path(t).resolve()
            handlers=fixture(root)
            handlers['get_task_input']=lambda _: {'versioned':True,'content_mode':'configured','edl':{},'edl_path':'src/edl.json'}
            client=FakeClient(handlers)
            result=process_task(client,cfg(root),TASK,lambda _:None,stages={'narrate':lambda _:self.fail('mismatched provider ran')})
            self.assertEqual(result['outcome'],'failed')
            self.assertIn('content mode',result['reason'])
            self.assertFalse(any(name=='submit_delivery' for name,_ in client.calls))
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

    def test_tts_auto_approve_must_be_a_bool(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            cfg_path = root / "config.json"
            good = {"mcp_url": "http://x/mcp", "delivery_root": str(root), "tts_auto_approve": "false"}
            cfg_path.write_text(json.dumps(good))
            with self.assertRaisesRegex(ValueError, "tts_auto_approve"):
                load_config(cfg_path, env={"VAC_WORKER_TOKEN": "x" * 40})
            good["tts_auto_approve"] = True
            good["sampling_limits"] = {"max_total_frames": 2}
            cfg_path.write_text(json.dumps(good))
            loaded = load_config(cfg_path, env={"VAC_WORKER_TOKEN": "x" * 40})
            self.assertTrue(loaded.tts_auto_approve)
            self.assertEqual(loaded.sampling_limits.max_total_frames, 2)

    def test_process_task_passes_governance_approvals_not_edl_fields(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            seen = {}

            def stage(ctx):
                seen["auto"] = ctx.tts_auto_approve
                seen["approvals"] = ctx.narration_approvals
                (ctx.out_dir / "edl.json").write_text("{}")
                (ctx.out_dir / "delivery-manifest.json").write_text("{}")
                return {}

            handlers = fixture(root)
            digest = "ab" * 32
            handlers["get_asset"] = lambda a: {"asset": {"artifacts": {"edl": "src/edl.json"}}, "narration_approvals": [digest]}
            client = FakeClient(handlers)
            config = cfg(root)
            config.tts_auto_approve = True
            result = process_task(client, config, TASK, lambda m: None, stages={"narrate": stage})
            self.assertEqual(result["outcome"], "succeeded")
            self.assertTrue(seen["auto"])
            self.assertEqual(seen["approvals"], (digest,))
            self.assertFalse((root / "stages" / "clip" / "t1" / ".work").exists())

    @unittest.skipUnless(shutil.which("ffmpeg") and shutil.which("ffprobe"), "ffmpeg/ffprobe not on PATH")
    def test_process_task_sends_verified_frame_bytes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp).resolve()
            src = root / "src"
            src.mkdir()
            subprocess.run(
                ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i",
                 "testsrc2=size=160x90:rate=30:duration=2", "-f", "lavfi", "-i",
                 "sine=frequency=440:sample_rate=48000:duration=2", "-c:v", "libx264", "-preset", "ultrafast",
                 "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", str(src / "clip.mp4")],
                check=True,
            )
            edl = {
                "timeline": {"fps": 30, "sample_rate": 48000},
                "video": [{"src": "clip.mp4", "in": 0, "out": 1.5, "timeline_in": 0}],
                "game_audio": [], "voice": [], "subtitle": [], "music": [],
            }
            (src / "edl.json").write_text(json.dumps(edl), encoding="utf-8")
            captured = {}

            def opener(req, timeout=None):
                body = json.loads(req.data.decode("utf-8"))
                frame = body["clips"][0]["frames"][0]
                raw = base64.b64decode(frame["image_base64"])
                captured["digest"] = hashlib.sha256(raw).hexdigest()
                captured["path"] = frame["path"]
                if captured["digest"] != frame["sha256"] or ":" in frame["path"] or "\\" in frame["path"]:
                    raise AssertionError("frame payload was not a verified relative image")

                class Response:
                    def read(self, n=-1):
                        payload = {"scenes": [{
                            "clip_index": 0, "label": "画面", "method": "vision", "confidence": 0.8,
                            "evidence_frames": [frame["sha256"]],
                        }]}
                        return json.dumps(payload).encode("utf-8")

                    def __enter__(self):
                        return self

                    def __exit__(self, *args):
                        return False

                return Response()

            handlers = fixture(root)
            handlers["get_asset_edl"] = lambda a: {"edl": edl}
            client = FakeClient(handlers)
            config = cfg(root)
            config.content_provider = "vision"
            config.content_provider_config = {
                "endpoint": "http://vision.local/recognize",
                "allow_external": True,
                "token_env": "TEST_VAC_VISION_TOKEN",
                "opener": opener,
            }
            os.environ["TEST_VAC_VISION_TOKEN"] = "secret-token"
            try:
                result = process_task(client, config, {**TASK, "type": "recognize"}, lambda m: None)
            finally:
                os.environ.pop("TEST_VAC_VISION_TOKEN", None)
            self.assertEqual(result["outcome"], "succeeded", result)
            self.assertEqual(len(captured["digest"]), 64)
            self.assertNotIn(":", captured["path"])
            package = root / "stages" / "clip" / "t1"
            manifest = json.loads((package / "samples" / "evidence-manifest.json").read_text(encoding="utf-8"))
            self.assertEqual(manifest["frames"][0]["sha256"], captured["digest"])
            self.assertEqual(
                hashlib.sha256((package / manifest["frames"][0]["path"]).read_bytes()).hexdigest(),
                captured["digest"],
            )


if __name__ == "__main__":
    unittest.main()
