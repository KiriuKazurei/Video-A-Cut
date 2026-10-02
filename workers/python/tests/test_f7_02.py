"""F7-02: connection isolation, lease cutoff, journal, and resume.

T19 runs separately with verify_phase7_ingest.py --recovery using synthetic
recordings, a loopback mock model, real local SAPI and the real control plane.
"""
from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import unittest.mock
import uuid
from pathlib import Path
from types import SimpleNamespace

from vac_worker.ingest import common, prepare, probe, segment
from vac_worker.ingest.common import Exec, IngestFailure
from vac_worker.ingest.journal import Journal, JournalBlocked, assert_journal_safe, read_bounded
from vac_worker.ingest.loop import REQUIRED_TOOLS, process_ingest, run_ingest_loop
from vac_worker.ingest.supervise import (
    COOPERATIVE_SECONDS, FORCE_WAIT_SECONDS, CleanupBlocked, Halt, LocalLease, PhysicalLock,
    ProcessRegistry, backoff_delay, pid_alive, retry_call, taskkill_tree,
)
from vac_worker.mcp import (
    AuthTransportError, Client, ProtocolTransportError, TransientTransportError, TransportError,
)

ROOT = Path(__file__).resolve().parents[1]


def _cfg(root: Path, roots: dict | None = None):
    return SimpleNamespace(heartbeat_interval=30, delivery_root=root, ingest_roots=roots or {},
                           poll_interval=0.01, degraded_interval=0.05, ffmpeg="ffmpeg", ffprobe="ffprobe", role="ingester")


def _scope(instance="inst-1", execution="exe_1", generation=1, task="tk_1", control_version=0):
    return {"agent_id": "ingester", "role": "ingester", "runtime_instance_id": instance,
            "task_id": task, "execution_id": execution, "generation": generation,
            "control_version": control_version}


def _claimed(args, **extra):
    ident = _scope(instance=args.get("runtime_instance_id") or "inst-1")
    body = {
        "claimed": True,
        "task": {"task_id": "tk_1", "type": "media_probe", "asset_id": "a"},
        "scope": {k: ident[k] for k in ("agent_id", "role", "runtime_instance_id", "task_id", "execution_id", "generation")},
        "control_version": 4,
        "lease_remaining_ms": 60000,
        "lease_expires_at": "2099-01-01T00:00:00Z",
        "execution_protocol": 2,
    }
    body.update(extra)
    return body


class RecordingClient:
    def __init__(self, handler):
        self.handler = handler
        self.calls = []

    def list_tools(self):
        return list(REQUIRED_TOOLS)

    def call(self, tool, args=None):
        args = args or {}
        self.calls.append((tool, args))
        return self.handler(tool, args)

    def names(self):
        return [name for name, _ in self.calls]


def _ingest_input(root: Path, roots: dict, stage="media_probe"):
    return {
        "run_id": "ing_1", "stage": stage, "execution_id": "exe_1", "input_sha256": "a" * 64,
        "resource_keys": ["source-snapshot-fixture"],
        "policy_sha256": "b" * 64, "roots_sha256": common.roots_fingerprint(roots),
        "policy": {"schema_version": 1, "min_free_bytes": 0, "copy_chunk_bytes": 4, "max_source_bytes": 1 << 20,
                   "chunk_us": 1_000_000, "overlap_us": 0, "analysis_max_edge": 64, "chunk_timeout_seconds": 5,
                   "scan_timeout_seconds": 30, "max_candidates": 20, "max_thumbnails": 2, "max_selected": 4,
                   "max_output_us": 10_000_000, "max_detection_json_bytes": 1 << 20, "prepare_timeout_seconds": 30,
                   "frame_quota_version": "quota-1", "max_frames_per_segment": 5},
        "output_dir": "ingest/ing_1/tk_1/exe_1", "package_dir": "ingest/ing_1/tk_1/exe_1/package", "checkpoints": [],
    }


class TransportIsolationTest(unittest.TestCase):
    def test_task_transport_error_does_not_fail_or_leave_the_loop(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        scope = _scope()

        def handler(tool, args):
            self.assertNotIn("scope", args)
            self.assertNotIn("lease_remaining_seconds", args)
            if tool == "get_task_input":
                self.assertEqual(args["execution_id"], "exe_1")
                self.assertNotIn("agent_id", args)
                raise TransientTransportError("injected local connection interruption")
            if tool == "claim_task":
                self.assertEqual(set(args), {"request_id", "runtime_instance_id"})
                return _claimed(args)
            return {}

        client = RecordingClient(handler)
        with unittest.mock.patch("vac_worker.ingest.loop.capability", return_value={"ffmpeg_ready": True, "ffprobe_ready": True, "execution_protocol": 2}):
            result = run_ingest_loop(client, _cfg(root), lambda _m: None, threading.Event(), once=True)
        self.assertEqual(result["outcome"], "suspended_connection")
        self.assertNotIn("fail_task", client.names())

    def test_auth_and_protocol_do_not_spin_or_borrow_a_new_execution(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)

        def handler(tool, args):
            if tool == "claim_task":
                raise AuthTransportError("mcp http 401: b'no'")
            return {}

        client = RecordingClient(handler)
        with unittest.mock.patch("vac_worker.ingest.loop.capability", return_value={"ffmpeg_ready": True, "ffprobe_ready": True}):
            result = run_ingest_loop(client, _cfg(root), lambda _m: None, threading.Event(), once=True)
        self.assertEqual(result["outcome"], "blocked")
        self.assertEqual(client.names().count("claim_task"), 1)
        self.assertNotIn("fail_task", client.names())

    def test_lost_execution_cancels_without_fail_or_new_id(self):
        from vac_worker.mcp import ToolError
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        scope = _scope()

        def handler(tool, args):
            self.assertNotIn("scope", args)
            if tool == "get_task_input":
                self.assertEqual(args["execution_id"], "exe_1")
                self.assertEqual(args["runtime_instance_id"], "inst-1")
                self.assertNotIn("agent_id", args)
                raise ToolError("get_task_input", "stale_execution: replaced")
            if tool == "ack_execution_stopped":
                self.assertEqual(args["outcome"], "stopped")
            return {}

        client = RecordingClient(handler)
        task = {"task_id": "tk_1", "type": "media_probe", "asset_id": "a", "_lease_remaining_ms": 60000}
        result = process_ingest(client, _cfg(root), task, lambda _m: None, threading.Event(), scope=scope, instance_id="inst-1")
        self.assertEqual(result["outcome"], "abandoned")
        self.assertNotIn("fail_task", client.names())
        self.assertIn("ack_execution_stopped", client.names())
        ack = next(args for name, args in client.calls if name == "ack_execution_stopped")
        self.assertEqual(ack["execution_id"], "exe_1")
        self.assertEqual(ack["outcome"], "stopped")
        self.assertNotIn("scope", ack)

    def test_unexpected_task_exception_stays_inside_the_loop(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        scope = _scope()

        def handler(tool, args):
            self.assertNotIn("scope", args)
            if tool == "claim_task":
                self.assertEqual(set(args), {"request_id", "runtime_instance_id"})
                return _claimed(args)
            if tool == "get_task_input":
                raise RuntimeError("ipc exploded")
            if tool == "fail_task":
                self.assertEqual(args["execution_id"], "exe_1")
                self.assertNotIn("agent", args)
                self.assertNotIn("agent_id", args)
                return {"ok": True}
            return {}

        client = RecordingClient(handler)
        with unittest.mock.patch("vac_worker.ingest.loop.capability", return_value={"ffmpeg_ready": True, "ffprobe_ready": True}):
            result = run_ingest_loop(client, _cfg(root), lambda _m: None, threading.Event(), once=True)
        self.assertEqual(result["outcome"], "failed")
        self.assertIn("fail_task", client.names())

    def test_classify_http_and_invalid_json(self):
        class Opener:
            def __init__(self, status, body):
                self.status, self.body = status, body

            def __call__(self, req, timeout):
                self.timeout = timeout
                import urllib.error
                err = urllib.error.HTTPError(req.full_url, self.status, "no", hdrs=None, fp=None)
                err.read = lambda: self.body
                raise err

        client = Client("http://127.0.0.1/mcp", "t" * 32, timeout=3, opener=Opener(401, b"no"))
        with self.assertRaises(AuthTransportError):
            client.call("heartbeat", {})
        client = Client("http://127.0.0.1/mcp", "t" * 32, timeout=3, opener=Opener(503, b"later"))
        with self.assertRaises(TransientTransportError):
            client.call("heartbeat", {})

        class Bad:
            def __call__(self, req, timeout):
                class Resp:
                    def read(self):
                        return b"not-json"
                    def __enter__(self):
                        return self
                    def __exit__(self, *a):
                        return False
                return Resp()

        client = Client("http://127.0.0.1/mcp", "t" * 32, timeout=3, opener=Bad())
        with self.assertRaises(ProtocolTransportError):
            client.call("heartbeat", {})

    def test_protocol_other_than_one_does_not_execute(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)

        def handler(tool, args):
            if tool == "claim_task":
                return _claimed(args, execution_protocol=3)
            if tool == "get_task_input":
                raise AssertionError("a mismatched execution protocol must not read input")
            return {}

        client = RecordingClient(handler)
        with unittest.mock.patch("vac_worker.ingest.loop.capability", return_value={"ffmpeg_ready": True, "ffprobe_ready": True, "execution_protocol": 2}):
            result = run_ingest_loop(client, _cfg(root), lambda _m: None, threading.Event(), once=True)
        self.assertEqual(result["outcome"], "blocked")
        self.assertNotIn("get_task_input", client.names())
        self.assertNotIn("fail_task", client.names())

    def test_begin_drain_errors_wait_without_media(self):
        from vac_worker.mcp import ToolError
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        inp = _ingest_input(root, {})
        for code in ("conflict", "resource_busy"):
            with self.subTest(code=code):
                def handler(tool, args, code=code):
                    self.assertNotIn("scope", args)
                    self.assertNotIn("agent_id", args)
                    if tool == "get_task_input":
                        return {"input_kind": "media_ingest", "ingest": inp}
                    if tool == "begin_execution":
                        raise ToolError("begin_execution", f"{code}: previous execution has not drained")
                    if tool in ("fail_task", "list_recovery_candidates"):
                        raise AssertionError(tool)
                    return {}

                client = RecordingClient(handler)
                task = {"task_id": "tk_1", "type": "media_probe", "_lease_remaining_ms": 60000}
                result = process_ingest(client, _cfg(root), task, lambda _m: None, threading.Event(), scope=_scope(), instance_id="inst-1")
                self.assertEqual(result["outcome"], "waiting_resource")
                self.assertEqual(result["reason"], "等待旧执行退出")
                self.assertNotIn("fail_task", client.names())
                self.assertNotIn("list_recovery_candidates", client.names())

    def test_stop_command_and_heartbeat_use_flat_fields(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        scope = _scope(control_version=2)
        inp = _ingest_input(root, {})
        controls = []
        beats = []

        def handler(tool, args):
            self.assertNotIn("scope", args)
            self.assertNotIn("wait_seconds", args)
            self.assertNotIn("lease_remaining_seconds", args)
            self.assertNotIn("agent_id", args)
            if tool == "get_execution_control":
                controls.append(args["known_control_version"])
                command = "stop" if len(controls) > 1 else ""
                return {"execution_id": "exe_1", "generation": 1, "control_version": 9, "command": command,
                        "reason": "user", "status": "running", "drain_required": command == "stop"}
            if tool == "heartbeat" and "task_id" in args:
                self.assertEqual(args["ingest_capability"]["execution_protocol"], 2)
                self.assertEqual(args["execution_id"], "exe_1")
                self.assertEqual(args["generation"], 1)
                beats.append(args)
                return {"ok": True, "lease_expires_at": "2099-01-01T00:00:00Z"}
            if tool == "get_task_input":
                deadline = time.monotonic() + 3
                while (len(controls) < 2 or not beats) and time.monotonic() < deadline:
                    time.sleep(0.01)
                return {"input_kind": "media_ingest", "cancelled": True, "ingest": inp}
            if tool == "ack_execution_stopped":
                self.assertIn(args["outcome"], ("stopped", "cleanup_blocked"))
                self.assertEqual(args["execution_id"], "exe_1")
                return {"ok": True}
            return {}

        client = RecordingClient(handler)
        cfg = _cfg(root)
        cfg.heartbeat_interval = 0
        task = {"task_id": "tk_1", "type": "media_probe", "_lease_remaining_ms": 60000}
        result = process_ingest(client, cfg, task, lambda _m: None, threading.Event(), scope=scope, instance_id="inst-1")
        self.assertEqual(result["outcome"], "abandoned")
        self.assertEqual(controls[0], 2)
        self.assertGreaterEqual(len(controls), 2)
        self.assertEqual(controls[1], 9)
        self.assertTrue(beats)
        self.assertNotIn("fail_task", client.names())


class LeaseAndRetryTest(unittest.TestCase):
    def test_monotonic_lease_deducts_rtt_and_stops_retry_before_the_margin(self):
        lease = LocalLease()
        before = time.monotonic()
        lease.observe(30, 2)
        self.assertGreaterEqual(lease.deadline, before + 30 - 2 - 1 - 0.05)
        self.assertLessEqual(lease.deadline, before + 30 - 2 - 1 + 0.05)
        started = time.monotonic()
        self.assertLessEqual(lease.retry_until(started), lease.deadline - (COOPERATIVE_SECONDS + FORCE_WAIT_SECONDS) + 0.001)
        self.assertLessEqual(lease.retry_until(started), started + 20 + 0.001)
        lease.observe(1, 0)
        self.assertTrue(lease.expired())
        self.assertEqual(COOPERATIVE_SECONDS, 5)
        self.assertEqual(FORCE_WAIT_SECONDS, 10)
        self.assertLessEqual(backoff_delay(0), 8)
        self.assertLessEqual(backoff_delay(9), 8)

    def test_submit_backoff_does_not_block_heartbeat(self):
        seen = threading.Event()

        def call(tool, args):
            if tool == "submit_ingest_result" and not seen.is_set():
                raise TransientTransportError("lost")
            return {}

        def heart():
            time.sleep(0.05)
            call("heartbeat", {"task_id": "tk_1"})
            seen.set()

        threading.Thread(target=heart, daemon=True).start()
        client = SimpleNamespace(call=call)
        retry_call(client, "submit_ingest_result", {"request_id": "same"}, Halt(), LocalLease(), lambda _m: None)
        self.assertTrue(seen.is_set())

    def test_local_deadline_suspends_without_fail(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        scope = _scope()
        inp = _ingest_input(root, {})
        inp["source"] = {"source_id": "s", "root_id": "missing", "relative_path": "a.mkv", "source_version": "c" * 64,
                         "size_bytes": 1, "mtime_ns": "1", "source_dir": "ingest/sources/s"}

        def handler(tool, args):
            self.assertNotIn("scope", args)
            self.assertNotIn("lease_remaining_seconds", args)
            self.assertNotIn("wait_seconds", args)
            if tool == "get_task_input":
                return {"input_kind": "media_ingest", "ingest": inp}
            if tool == "begin_execution":
                return {"ok": True}
            if tool == "list_recovery_candidates":
                self.assertEqual(set(args), {"task_id", "runtime_instance_id", "execution_id", "generation"})
                return {"candidates": [{
                    "former_execution_id": "exe_old", "generation": 1, "stage": "media_probe",
                    "input_sha256": "a" * 64, "checkpoint_count": 1, "summary": "same-run fixed input",
                }]}
            if tool == "fail_task":
                raise AssertionError("lease expiry must not fail the task")
            return {}

        client = RecordingClient(handler)
        task = {"task_id": "tk_1", "type": "media_probe", "_lease_remaining_ms": 0}
        result = process_ingest(client, _cfg(root), task, lambda _m: None, threading.Event(), scope=scope, instance_id="inst-1")
        self.assertEqual(result["outcome"], "suspended_connection")
        self.assertNotIn("fail_task", client.names())


class JournalAndIdempotencyTest(unittest.TestCase):
    def _exec(self, root: Path, client=None):
        roots = {}
        inp = _ingest_input(root, roots)
        return Exec(client=client or RecordingClient(lambda *a: {}), task_id="tk_1", inp=inp, delivery_root=root,
                    roots=roots, ffmpeg="ffmpeg", ffprobe="ffprobe", cancel_event=threading.Event(), log=lambda _m: None,
                    scope=_scope())

    def test_journal_is_atomic_and_refuses_secrets_and_corruption(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        journal = Journal(root, _scope(), {"input_sha256": "a" * 64, "policy_sha256": "b" * 64, "worker_version": "vac-ingester/2"})
        journal.note_unit(stage="media_probe", kind="copy_chunk", state="verified_local", sha256="d" * 64, unit="copy_chunk:1", sequence=1)
        text = journal.path.read_text(encoding="utf-8")
        self.assertNotIn(".tmp", "".join(os.listdir(root)))
        self.assertNotIn("token", text.lower())
        self.assertNotIn(":\\", text)
        with self.assertRaises(JournalBlocked):
            assert_journal_safe({"token": "secret"})
        with self.assertRaises(JournalBlocked):
            assert_journal_safe({"path": "C:/secret/video.mkv"})
        journal.path.write_text("{", encoding="utf-8")
        again = Journal(root, _scope(), {"input_sha256": "a" * 64})
        self.assertTrue(again.untrusted)
        before = journal.path.read_text(encoding="utf-8")
        with self.assertRaises(JournalBlocked):
            again.save()
        self.assertEqual(journal.path.read_text(encoding="utf-8"), before)
        huge = root / "big.json"
        huge.write_bytes(b"x" * ((1 << 20) + 1))
        with self.assertRaises(JournalBlocked):
            read_bounded(huge)

    def test_lost_submit_reuses_request_and_does_not_double_commit(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        commits = []

        def handler(tool, args):
            if tool == "get_execution_result_status":
                return {"status": "not_committed"}
            if tool == "submit_ingest_result":
                if commits and commits[0] != args["request_id"]:
                    raise AssertionError("request id changed")
                if args["request_id"] not in commits:
                    commits.append(args["request_id"])
                    raise TransientTransportError("response lost after commit")
                return {"status": "committed"}
            return {}

        ex = self._exec(root, RecordingClient(handler))
        ex.package_dir.mkdir(parents=True)
        ex.submit()
        ex.submit()
        self.assertEqual(len(commits), 1)
        self.assertEqual(ex.journal.data["pending_request"]["state"], "committed")

    def test_checkpoint_retry_keeps_sequence_and_args(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        seen = []

        def handler(tool, args):
            if tool == "save_ingest_checkpoint":
                seen.append(dict(args))
                if len(seen) == 1:
                    raise TransientTransportError("checkpoint response lost")
            return {}

        ex = self._exec(root, RecordingClient(handler))
        ex.save_checkpoint("copy_chunk", 1, {"offset_bytes": 4})
        self.assertEqual(seen[0]["sequence"], seen[1]["sequence"])
        self.assertEqual(seen[0]["sha256"], seen[1]["sha256"])
        self.assertEqual(seen[0], seen[1])
        self.assertNotIn("scope", seen[0])
        self.assertNotIn("request_id", seen[0])
        self.assertEqual(seen[0]["runtime_instance_id"], "inst-1")
        self.assertEqual(seen[0]["generation"], 1)
        self.assertEqual(seen[0]["input_sha256"], "a" * 64)
        self.assertEqual(ex._seq, 1)

    def test_recovery_candidates_follow_the_server_fields(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        ex = self._exec(root)
        noted = ex.note_recovery_candidates([{
            "former_execution_id": "exe_old", "generation": 2, "stage": "media_probe",
            "input_sha256": "a" * 64, "checkpoint_count": 3, "summary": "same-run fixed input",
        }])
        self.assertEqual(noted, 1)
        self.assertNotIn("recovered_from_execution_id", ex.journal.data)  # Listing is not adoption.
        self.assertEqual(ex.note_recovery_candidates([{
            "former_execution_id": "exe_other", "generation": 1, "stage": "segment",
            "input_sha256": "a" * 64, "checkpoint_count": 1, "summary": "other stage",
        }]), 0)
        with self.assertRaises(IngestFailure):
            ex.note_recovery_candidates([{"former_execution_id": "exe_old", "checkpoint_ref": "C:/windows/notepad.exe"}])


class ResumeTest(unittest.TestCase):
    def test_probe_drops_only_the_unverified_tail(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        src_dir = root / "rec"
        src_dir.mkdir()
        blob = src_dir / "clip.bin"
        blob.write_bytes(b"abcdefghij")
        roots = {"rec": src_dir}
        st = blob.stat()
        source = {"source_id": "s", "root_id": "rec", "relative_path": "clip.bin", "source_version": "c" * 64,
                  "size_bytes": st.st_size, "mtime_ns": str(st.st_mtime_ns), "source_dir": "ingest/sources/s"}
        cancel = threading.Event()
        client = RecordingClient(lambda tool, args: cancel.set() if tool == "save_ingest_checkpoint" and args["sequence"] == 1 else {})
        inp = _ingest_input(root, roots)
        inp["source"] = source
        ex = Exec(client=client, task_id="tk_1", inp=inp, delivery_root=root, roots=roots, ffmpeg="ffmpeg", ffprobe="ffprobe",
                  cancel_event=cancel, log=lambda _m: None)
        with self.assertRaises(common.Cancelled):
            probe.copy_snapshot(ex)
        part = next((root / "ingest/sources/s").glob("snapshot.*.part"))
        part.write_bytes(part.read_bytes() + b"ZZ")
        rows = []
        for args in client.calls:
            if args[0] != "save_ingest_checkpoint":
                continue
            a = args[1]
            rows.append({"task_id": a["task_id"], "sequence": a["sequence"], "execution_id": a["execution_id"],
                         "input_sha256": a["input_sha256"], "kind": a["kind"], "item_index": a["item_index"],
                         "ref": a["ref"], "sha256": a["sha256"]})
        logs = []
        inp2 = dict(inp, checkpoints=rows, execution_id="exe_2", output_dir="ingest/ing_1/tk_1/exe_2",
                    package_dir="ingest/ing_1/tk_1/exe_2/package")
        ex2 = Exec(client=RecordingClient(lambda *a: {}), task_id="tk_1", inp=inp2, delivery_root=root, roots=roots,
                   ffmpeg="ffmpeg", ffprobe="ffprobe", cancel_event=threading.Event(), log=logs.append,
                   scope=_scope(execution="exe_2"))
        doc = probe.copy_snapshot(ex2)
        self.assertTrue(any("dropping 2 unverified trailing bytes" in line for line in logs), logs)
        self.assertEqual(doc["sha256"], hashlib.sha256(b"abcdefghij").hexdigest())

    def test_copy_ledger_stays_under_one_mebibyte(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        roots = {}
        inp = _ingest_input(root, roots)
        ex = Exec(client=RecordingClient(lambda *a: {}), task_id="tk_1", inp=inp, delivery_root=root, roots=roots,
                  ffmpeg="ffmpeg", ffprobe="ffprobe", cancel_event=threading.Event(), log=lambda _m: None)
        raw = bytes(range(80)) * 4
        inp['source'] = {'size_bytes': len(raw)}
        hashes = [hashlib.sha256(raw[i:i + 4]).hexdigest() for i in range(0, len(raw), 4)]
        with unittest.mock.patch("vac_worker.ingest.probe._MANIFEST_BUDGET", 120):
            body = probe.copy_checkpoint_body(ex, {"source_version": "c" * 64}, root / "part.bin", 4, len(raw), hashes)
        self.assertNotIn("chunk_sha256", body)
        self.assertLessEqual(len(json.dumps(body).encode()), 1 << 20)
        for ref in body["ledger"]:
            self.assertLessEqual((root / ref).stat().st_size, 1 << 20)
        part = root / "part.bin"
        part.write_bytes(raw)
        loaded, done = probe._load_resume(ex, part, body, 4)
        self.assertEqual(done, len(raw))
        self.assertEqual(loaded, hashes)

    def test_segment_does_not_rescan_completed_chunks(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        snap = root / "ingest/sources/s/snapshot.bin"
        snap.parent.mkdir(parents=True)
        snap.write_bytes(b"1234")
        roots = {}
        inp = _ingest_input(root, roots, stage="segment")
        inp["source"] = {"source_id": "s", "snapshot_ref": "ingest/sources/s/snapshot.bin", "size_bytes": 4, "source_version": "c" * 64}
        inp["probe"] = {"streams": [{"index": 0, "type": "video", "start_us": 0}]}
        inp["analysis"] = {"video_stream_index": 0, "game_audio_stream_index": None, "source_range_us": [0, 3_000_000],
                           "segmentation": {"method": "scene_change", "threshold": 0.4, "min_segment_us": 1_000_000, "max_segment_us": 2_000_000}}
        inp["analysis_sha256"] = "e" * 64
        inp["policy"] = dict(inp["policy"], chunk_us=1_000_000, overlap_us=0)
        calls = []

        def fake_scan(ex, snap_path, vidx, origin, start, scan_end, threshold, timeout):
            calls.append(start)
            return []

        ex = Exec(client=RecordingClient(lambda *a: {}), task_id="tk_1", inp=inp, delivery_root=root, roots=roots,
                  ffmpeg="ffmpeg", ffprobe="ffprobe", cancel_event=threading.Event(), log=lambda _m: None)
        manifest = {"schema_version": 1, "kind": "scan_chunk", "item_index": 0, "execution_id": "exe_1",
                    "input_sha256": "a" * 64, "range_us": [0, 1_000_000, 1_000_000], "cuts": []}
        path = root / "ingest/ing_1/tk_1/exe_1/checkpoints/00001-scan_chunk-0.json"
        raw = common.dump_json(path, manifest)
        ex.inp["checkpoints"] = [{"task_id": "tk_1", "sequence": 1, "execution_id": "exe_1", "input_sha256": "a" * 64,
                                  "kind": "scan_chunk", "item_index": 0, "ref": ex.rel(path), "sha256": hashlib.sha256(raw).hexdigest()}]
        with unittest.mock.patch("vac_worker.ingest.segment.scan_chunk", fake_scan):
            ex.runner.run = lambda *a, **k: (b"", "")
            segment.segment(ex)
        self.assertNotIn(0, calls)
        self.assertIn(1_000_000, calls)
        self.assertGreaterEqual(len(calls), 1)

    def test_prepare_reencodes_only_the_incomplete_clip(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        snap = root / "ingest/sources/s/snapshot.bin"
        snap.parent.mkdir(parents=True)
        snap.write_bytes(b"1234")
        roots = {}
        inp = _ingest_input(root, roots, stage="media_prepare")
        inp["source"] = {"source_id": "s", "snapshot_ref": "ingest/sources/s/snapshot.bin", "size_bytes": 4, "sha256": "c" * 64, "source_version": "c" * 64}
        inp["probe"] = {"streams": [{"index": 0, "type": "video", "start_us": 0, "time_base": "1/1000", "start_pts": 0}]}
        inp["analysis"] = {"video_stream_index": 0, "game_audio_stream_index": None, "source_range_us": [0, 2_000_000]}
        inp["analysis_sha256"] = "e" * 64
        inp["selection"] = {"source_sha256": "c" * 64, "selected_segments": [
            {"segment_id": "seg_0001", "start_us": 0, "end_us": 1_000_000},
            {"segment_id": "seg_0002", "start_us": 1_000_000, "end_us": 2_000_000}],
            "output": {"fps": 30, "sample_rate": 48000}}
        inp["selection_sha256"] = "f" * 64
        inp["frame_quotas"] = [1, 1]
        inp["provenance"] = {"schema_version": 1, "source_id": "s", "source_sha256": "c" * 64, "source_version": "c" * 64,
                             "run_id": "ing_1", "analysis_revision": 1, "analysis_sha256": "e" * 64, "selection_revision": 1,
                             "selection_sha256": "f" * 64, "policy_sha256": "b" * 64, "probe_sha256": "d" * 64,
                             "method_version": "", "profile_id": "p", "profile_revision": 1, "profile_sha256": "9" * 64,
                             "frame_quota_version": "quota-1", "worker_version": ""}
        work = root / "ingest/ing_1/tk_1/exe_1/work"
        work.mkdir(parents=True)
        done = work / "segment_001.mp4"
        done.write_bytes(b"finished-clip")
        partial = work / "segment_002.mp4"
        partial.write_bytes(b"partial")
        entry = {"segment_id": "seg_0001", "index": 1, "media": "media/segment_001.mp4", "media_sha256": hashlib.sha256(b"finished-clip").hexdigest(),
                 "source_start_us": 0, "source_end_us": 1_000_000, "source_start_pts": 0, "source_end_pts": 1000,
                 "timeline_in_frames": 0, "output_frames": prepare.output_frames(0, 1_000_000, 30), "output_samples": 0,
                 "measured_frames": prepare.output_frames(0, 1_000_000, 30), "measured_samples": 0,
                 "start_error_us": 0, "end_error_us": 0, "audio_sample_error": 0, "source_frames_in_range": 30,
                 "duplicated_frames": 0, "dropped_frames": 0, "audio_pad_start_samples": 0, "frame_quota": 1}
        manifest = {"schema_version": 1, "kind": "segment_media", "item_index": 1, "execution_id": "exe_1",
                    "input_sha256": "a" * 64, "segment": entry, "work_media": "ingest/ing_1/tk_1/exe_1/work/segment_001.mp4"}
        path = root / "ingest/ing_1/tk_1/exe_1/checkpoints/00001-segment_media-1.json"
        raw = common.dump_json(path, manifest)
        inp["checkpoints"] = [{"task_id": "tk_1", "sequence": 1, "execution_id": "exe_1", "input_sha256": "a" * 64,
                               "kind": "segment_media", "item_index": 1, "ref": "ingest/ing_1/tk_1/exe_1/checkpoints/00001-segment_media-1.json",
                               "sha256": hashlib.sha256(raw).hexdigest()}]
        encoded = []

        def fake_encode(ex, snap_path, plan, out, a_us, b_us, origin, frames, samples, dest, timeout):
            encoded.append(dest.name)
            dest.write_bytes(b"new-clip")

        ex = Exec(client=RecordingClient(lambda *a: {}), task_id="tk_1", inp=inp, delivery_root=root, roots=roots,
                  ffmpeg="ffmpeg", ffprobe="ffprobe", cancel_event=threading.Event(), log=lambda _m: None)
        with unittest.mock.patch("vac_worker.ingest.prepare._encode", fake_encode), \
             unittest.mock.patch("vac_worker.ingest.prepare._measure", lambda *a, **k: (prepare.output_frames(1_000_000, 2_000_000, 30), 0, 0)), \
             unittest.mock.patch("vac_worker.ingest.prepare._source_frames", lambda ex, snap, vidx, tb, abs_a, abs_b: (30, abs_a)):
            prepare.media_prepare(ex)
        self.assertEqual(encoded, ["segment_002.mp4"])
        self.assertFalse(partial.exists() and partial.read_bytes() == b"partial")


class WindowsProcessTest(unittest.TestCase):
    def test_taskkill_requires_a_registered_pid(self):
        with self.assertRaises(CleanupBlocked):
            taskkill_tree(0)
        recorded = []

        def fake_run(args, **kwargs):
            recorded.append(args)
            return SimpleNamespace(returncode=0)

        with unittest.mock.patch("vac_worker.ingest.supervise.subprocess.run", fake_run):
            taskkill_tree(4321)
        self.assertEqual(recorded, [["taskkill", "/T", "/F", "/PID", "4321"]])

    @unittest.skipUnless(os.name == "nt", "requires a Windows process tree")
    def test_owned_tree_is_stopped_without_killing_by_image_name(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        code = "import subprocess,sys,time; p=subprocess.Popen([sys.executable,'-c','import time; time.sleep(120)']); print(p.pid, flush=True); time.sleep(120)"
        proc = subprocess.Popen([sys.executable, "-c", code], stdout=subprocess.PIPE, text=True)
        self.addCleanup(lambda: proc.poll() is not None or proc.kill())
        child = int(proc.stdout.readline().strip())
        registry = ProcessRegistry(root, "inst-tree")
        registry.note(proc.pid)
        status = registry.stop_owned(Halt(), grace=0.2, force_wait=5)
        self.assertEqual(status, "stopped")
        self.assertFalse(pid_alive(proc.pid))
        self.assertFalse(pid_alive(child))
        self.assertEqual(registry.cleanup_blocked, False)

    @unittest.skipUnless(os.name == "nt", "requires a Windows lock file")
    def test_cross_process_lock_is_not_stolen(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        holder = root / "hold.py"
        holder.write_text(
            "import sys, time\nfrom pathlib import Path\n"
            "sys.path.insert(0, sys.argv[1])\n"
            "from vac_worker.ingest.supervise import PhysicalLock, ProcessRegistry\n"
            "root=Path(sys.argv[2])\n"
            "reg=ProcessRegistry(root, 'holder')\n"
            "lock=PhysicalLock(root/'res.lock', {'runtime_instance_id':'holder','execution_id':'old','marker':reg.marker}, reg)\n"
            "print(lock.try_acquire(), flush=True)\n"
            "time.sleep(30)\n",
            encoding="utf-8")
        proc = subprocess.Popen([sys.executable, str(holder), str(ROOT), str(root)], stdout=subprocess.PIPE, text=True)
        self.addCleanup(lambda: proc.poll() is not None or proc.kill())
        line = proc.stdout.readline().strip()
        self.assertEqual(line, "acquired")
        other = ProcessRegistry(root, "new-instance")
        lock = PhysicalLock(root / "res.lock", {"runtime_instance_id": "new-instance", "execution_id": "new", "marker": other.marker}, other)
        self.assertEqual(lock.try_acquire(), "busy")
        self.assertTrue((root / "res.lock").is_file())
        proc.kill()
        proc.wait(timeout=5)
        self.assertEqual(lock.try_acquire(), "acquired")
        lock.release()
        self.assertTrue((root / "res.lock").is_file())

    @unittest.skipUnless(os.name == "nt", "junctions are a Windows reparse point")
    def test_cleanup_refuses_junction_escape_and_keeps_unknown_directories(self):
        root = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, root, True)
        outside = root / "outside"
        outside.mkdir()
        (outside / "keep.txt").write_text("keep", encoding="utf-8")
        work = root / "owned"
        work.mkdir()
        link = work / "jump"
        subprocess.check_call(["cmd", "/c", "mklink", "/J", str(link), str(outside)], stdout=subprocess.DEVNULL)
        with self.assertRaises(CleanupBlocked):
            common.safe_rmtree(work, root)
        self.assertEqual((outside / "keep.txt").read_text(encoding="utf-8"), "keep")
        unknown = root / "not-owned"
        unknown.mkdir()
        (unknown / "a.txt").write_text("stay", encoding="utf-8")
        registry = ProcessRegistry(root, "inst")
        self.assertEqual(registry.recover_orphans(), "clear")
        self.assertEqual((unknown / "a.txt").read_text(encoding="utf-8"), "stay")


class SupervisorScriptTest(unittest.TestCase):
    def test_restart_cap_and_permanent_errors(self):
        script = ROOT.parents[1] / "scripts" / "Start-LocalPipeline.ps1"
        command = (
            "$env:VAC_SUPERVISOR_LIB_ONLY='1'; "
            f". '{script}'; "
            "if ((Get-WorkerRestartDelay @()) -ne 1) { exit 11 }; "
            "if ((Get-WorkerRestartDelay @((Get-Date))) -ne 2) { exit 12 }; "
            "if ((Get-WorkerRestartDelay @((Get-Date),(Get-Date))) -ne 4) { exit 13 }; "
            "if ($null -ne (Get-WorkerRestartDelay @((Get-Date),(Get-Date),(Get-Date)))) { exit 14 }; "
            "if (-not (Test-PermanentWorkerExit 3 '')) { exit 15 }; "
            "if (-not (Test-PermanentWorkerExit 1 \"fatal: config: missing\")) { exit 16 }; "
            "if (Test-PermanentWorkerExit 1 'temporary disconnect') { exit 17 }; "
            "exit 0"
        )
        done = subprocess.run(["powershell", "-NoProfile", "-Command", command], capture_output=True, text=True, timeout=30)
        self.assertEqual(done.returncode, 0, done.stdout + done.stderr)
