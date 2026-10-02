"""Ingester stages on real synthetic recordings (needs ffmpeg/ffprobe on PATH).

Every assertion measures media: cut times against the fixture layout, clip
frame/sample counts, and flash/beep markers in the prepared clips."""
import hashlib
import json
import os
import shutil
import sys
import tempfile
import threading
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))

import phase7_fixtures as fx  # noqa: E402
from vac_worker.ingest import common, prepare, probe, segment  # noqa: E402
from vac_worker.ingest.common import Cancelled, Exec, IngestFailure  # noqa: E402

HAVE_FFMPEG = shutil.which("ffmpeg") and shutil.which("ffprobe")
MIB = 1 << 20


def policy(**over):
    p = {"schema_version": 1, "policy_id": "media-ingest-default", "max_source_bytes": 64 << 30,
         "max_source_duration_us": 6 * 3600 * 10**6, "copy_chunk_bytes": MIB, "min_free_bytes": 0,
         "chunk_us": 60 * 10**6, "overlap_us": 10**6, "analysis_max_edge": 640, "chunk_timeout_seconds": 120,
         "scan_timeout_seconds": 7200, "max_candidates": 2048, "max_thumbnails": 128, "max_selected": 48,
         "max_output_us": 15 * 60 * 10**6, "max_detection_json_bytes": 16 * MIB, "prepare_timeout_seconds": 1800,
         "frame_quota_version": "quota-1", "max_frames_per_segment": 5}
    p.update(over)
    return p


class FakeClient:
    def __init__(self, on_checkpoint=None):
        self.calls = []
        self.on_checkpoint = on_checkpoint

    def call(self, tool, args=None):
        self.calls.append((tool, args or {}))
        if tool == "save_ingest_checkpoint" and self.on_checkpoint:
            self.on_checkpoint(args)
        return {}

    def tool(self, name):
        return [a for t, a in self.calls if t == name]


def make_exec(root: Path, roots: dict, stage: str, inp_extra: dict, exec_id="exe_1", client=None, cancel=None, pol=None):
    out = f"ingest/ing_1/tk_{stage}/{exec_id}"
    inp = {"run_id": "ing_1", "stage": stage, "execution_id": exec_id, "input_sha256": "a" * 64, "policy_sha256": "b" * 64,
           "roots_sha256": common.roots_fingerprint(roots), "policy": pol or policy(), "output_dir": out,
           "package_dir": out + "/package", "checkpoints": [], **inp_extra}
    return Exec(client=client or FakeClient(), task_id=f"tk_{stage}", inp=inp, delivery_root=root, roots=roots,
                ffmpeg="ffmpeg", ffprobe="ffprobe", cancel_event=cancel or threading.Event(), log=lambda m: None)


def source_ref(roots, rid, rel, sid="src_1", **extra):
    st = os.stat(roots[rid] / rel)
    return {"source_id": sid, "root_id": rid, "relative_path": rel, "source_version": "c" * 64,
            "size_bytes": st.st_size, "mtime_ns": str(st.st_mtime_ns), "source_dir": f"ingest/sources/{sid}", **extra}


def checkpoint_rows(ex: Exec) -> list[dict]:
    rows = []
    for args in ex.client.tool("save_ingest_checkpoint"):
        rows.append({"task_id": args["task_id"], "sequence": args["sequence"], "execution_id": args["execution_id"],
                     "input_sha256": args["input_sha256"], "kind": args["kind"], "item_index": args["item_index"],
                     "ref": args["ref"], "sha256": args["sha256"]})
    return rows


class RootsTest(unittest.TestCase):
    def test_fingerprint_matches_control_plane_vector(self):
        # Same vector as control-plane/internal/ingest TestRootsFingerprintVector.
        roots = {"rec": Path("E:\\Data\\Rec\\"), "b": Path("D:/Video Files/OBS")}
        text = "b\td:/video files/obs\nrec\te:/data/rec\n"
        self.assertEqual(common.roots_fingerprint(roots), hashlib.sha256(text.encode()).hexdigest())

    def test_clean_relative_rejects_escapes(self):
        for bad in ("../x.mkv", "a/../../x", "C:/x.mkv", "\\\\srv\\share\\x", "a/con.mkv", "a//b", "x.mkv.", "a/b:stream"):
            with self.assertRaises(IngestFailure, msg=bad):
                common.clean_relative(bad)
        self.assertEqual(common.clean_relative("录屏 2026/a b.mkv"), ["录屏 2026", "a b.mkv"])

    def test_candidates_merge_and_split(self):
        cuts = [{"t_us": 500_000, "score": 0.9}, {"t_us": 4_000_000, "score": 0.5}]
        out = segment.candidates(cuts, 0, 20_000_000, 1_000_000, 6_000_000)
        self.assertEqual([(c["start_us"], c["end_us"], c["reason"]) for c in out],
                         [(0, 4_000_000, "merged_short"), (4_000_000, 9_333_333, "scene_change"),
                          (9_333_333, 14_666_666, "duration_limit"), (14_666_666, 20_000_000, "duration_limit")])
        self.assertEqual(segment.candidates([], 0, 3_000_000, 1_000_000, 6_000_000)[0]["reason"], "range_start")
        self.assertEqual(segment.dedup([{"t_us": 1_000_000, "score": 0.4}, {"t_us": 1_100_000, "score": 0.8}], 0, 5_000_000),
                         [{"t_us": 1_100_000, "score": 0.8}])


@unittest.skipUnless(HAVE_FFMPEG, "ffmpeg/ffprobe not on PATH")
class IngestMediaTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        base = Path(cls.tmp.name)
        cls.files = fx.build_all("ffmpeg", base / "rec 录屏")
        cls.roots = {"rec": base / "rec 录屏"}

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def setUp(self):
        self._d = tempfile.TemporaryDirectory()
        self.root = Path(self._d.name).resolve()

    def tearDown(self):
        self._d.cleanup()

    def probe_file(self, name, **pol):
        self._n = getattr(self, "_n", 0) + 1
        ex = make_exec(self.root, self.roots, "media_probe", {"source": source_ref(self.roots, "rec", name, sid=f"src_{self._n}")},
                       exec_id=f"exe_p{self._n}", pol=policy(**pol))
        probe.media_probe(ex)
        pkg = ex.package_dir
        return ex, json.loads((pkg / "snapshot.json").read_text("utf-8")), json.loads((pkg / "probe.json").read_text("utf-8"))

    def published(self, name):
        ex, snap, pr = self.probe_file(name)
        src = source_ref(self.roots, "rec", name, sid=snap["source_id"], snapshot_ref=snap["snapshot"], sha256=snap["sha256"])
        return src, pr

    def test_probe_streams_snapshot_and_hash(self):
        name = self.files["scenes"].name
        ex, snap, pr = self.probe_file(name)
        self.assertEqual(snap["sha256"], hashlib.sha256(self.files["scenes"].read_bytes()).hexdigest())
        self.assertGreater(snap["chunks"], 1)
        self.assertTrue((self.root / snap["snapshot"]).is_file())
        self.assertEqual([s["type"] for s in pr["streams"]], ["video", "audio", "audio"])
        v, a1, a2 = pr["streams"]
        self.assertEqual(v["start_us"], 1_500_000)
        # The fixture starts at 1.5 s; the usable range is the video span, not the container end.
        self.assertEqual(v["duration_us"], int(fx.SCENE_DURATION * 1e6))
        self.assertEqual(pr["duration_us"], v["duration_us"])
        self.assertEqual(v["frame_rate_mode"], "cfr")
        self.assertAlmostEqual(a2["start_us"] - v["start_us"], 750_000, delta=30_000)
        self.assertEqual(a1["title"], "game")
        self.assertIn("multiple_audio_streams", pr["limitations"])
        self.assertTrue(ex.client.tool("save_ingest_checkpoint"))
        receipt = json.loads((ex.package_dir / "worker-receipt.json").read_text("utf-8"))
        self.assertEqual(receipt["execution_id"], ex.inp["execution_id"])
        self.assertEqual(ex.client.tool("submit_ingest_result")[0]["package_dir"], ex.package_rel)

    def test_probe_classifies_vfr_and_no_audio(self):
        _, _, pr = self.probe_file(self.files["sync_vfr"].name)
        self.assertEqual(pr["streams"][0]["frame_rate_mode"], "vfr")
        self.assertIn("variable_frame_rate", pr["limitations"])
        _, _, pr = self.probe_file(self.files["noaudio"].name)
        self.assertIn("no_audio", pr["limitations"])
        _, _, pr = self.probe_file(self.files["ntsc"].name)
        self.assertEqual(pr["streams"][0]["r_frame_rate"], "30000/1001")

    def test_probe_rejects_damaged_and_videoless(self):
        for key in ("broken", "audio_only"):
            with self.assertRaises(IngestFailure, msg=key):
                self.probe_file(self.files[key].name)

    def test_copy_resumes_from_verified_chunks_and_rejects_changes(self):
        name = self.files["scenes"].name
        cancel = threading.Event()
        client = FakeClient(on_checkpoint=lambda a: cancel.set() if a["sequence"] == 2 else None)
        src = source_ref(self.roots, "rec", name)
        ex1 = make_exec(self.root, self.roots, "media_probe", {"source": src}, client=client, cancel=cancel)
        with self.assertRaises(Cancelled):
            probe.media_probe(ex1)
        rows = checkpoint_rows(ex1)
        self.assertEqual(len(rows), 2)
        ex2 = make_exec(self.root, self.roots, "media_probe", {"source": src, "checkpoints": rows}, exec_id="exe_2")
        logs = []
        ex2.log = logs.append
        probe.media_probe(ex2)
        self.assertTrue(any("resuming copy" in m for m in logs), logs)
        snap = json.loads((ex2.package_dir / "snapshot.json").read_text("utf-8"))
        self.assertEqual(snap["sha256"], hashlib.sha256(self.files["scenes"].read_bytes()).hexdigest())
        later = ex2.client.tool("save_ingest_checkpoint")
        if later:
            self.assertEqual(later[0]["sequence"], 3)

        # A tampered checkpoint manifest is ignored and the copy restarts.
        bad = [dict(rows[-1], sha256="0" * 64)]
        shutil.rmtree(self.root / "ingest" / "sources")
        ex3 = make_exec(self.root, self.roots, "media_probe", {"source": src, "checkpoints": bad}, exec_id="exe_3")
        logs3 = []
        ex3.log = logs3.append
        probe.media_probe(ex3)
        self.assertTrue(any("failed verification" in m for m in logs3), logs3)

        stale = dict(src, mtime_ns=str(int(src["mtime_ns"]) - 10**9))
        shutil.rmtree(self.root / "ingest" / "sources")
        ex4 = make_exec(self.root, self.roots, "media_probe", {"source": stale}, exec_id="exe_4")
        with self.assertRaises(IngestFailure):
            probe.media_probe(ex4)
        self.assertTrue(self.files["scenes"].is_file())

    def analysis(self, src, pr, rng, threshold=0.3, min_us=1_000_000, max_us=600_000_000, audio=1):
        return {"schema_version": 1, "source_id": src["source_id"], "source_sha256": src["sha256"], "probe_sha256": "d" * 64,
                "policy_sha256": "b" * 64, "video_stream_index": 0, "game_audio_stream_index": audio, "source_range_us": rng,
                "segmentation": {"method": "scene_change", "threshold": threshold, "min_segment_us": min_us, "max_segment_us": max_us}}

    def test_segment_measures_known_cuts_across_chunk_boundary(self):
        src, pr = self.published(self.files["scenes"].name)
        plan = self.analysis(src, pr, [0, pr["duration_us"]])
        # 9 s chunks put the 9.0 s cut exactly on a chunk boundary.
        ex = make_exec(self.root, self.roots, "segment", {"source": src, "probe": pr, "analysis": plan, "analysis_sha256": "e" * 64},
                       pol=policy(chunk_us=9_000_000))
        segment.segment(ex)
        doc = json.loads((ex.package_dir / "segments.json").read_text("utf-8"))
        self.assertTrue(doc["complete"])
        self.assertEqual(doc["chunks_total"], -(-pr["duration_us"] // 9_000_000))
        cuts = [c["t_us"] for c in doc["cuts"]]
        self.assertEqual(len(cuts), len(fx.SCENE_CUTS), cuts)
        for got, want in zip(cuts, fx.SCENE_CUTS):
            self.assertLessEqual(abs(got - want * 1e6), 34_000, (got, want))
        cands = doc["candidates"]
        self.assertEqual(cands[0]["start_us"], 0)
        self.assertEqual(cands[-1]["end_us"], pr["duration_us"])
        for a, b in zip(cands, cands[1:]):
            self.assertEqual(a["end_us"], b["start_us"])
        for c in cands:
            self.assertTrue((ex.package_dir / c["thumbnail"]).is_file())
        self.assertEqual(len(ex.client.tool("save_ingest_checkpoint")), doc["chunks_total"])

        # Re-running with the verified checkpoints skips every chunk.
        rows = checkpoint_rows(ex)
        ex2 = make_exec(self.root, self.roots, "segment", {"source": src, "probe": pr, "analysis": plan, "analysis_sha256": "e" * 64,
                                                           "checkpoints": rows}, exec_id="exe_2", pol=policy(chunk_us=9_000_000))
        logs = []
        ex2.log = logs.append
        segment.segment(ex2)
        self.assertTrue(any(f"reusing {doc['chunks_total']} verified scan chunks" in m for m in logs), logs)
        self.assertEqual(ex2.client.tool("save_ingest_checkpoint"), [])

    def test_segment_without_changes_uses_length_rules_and_range(self):
        src, pr = self.published(self.files["noaudio"].name)
        plan = self.analysis(src, pr, [1_000_000, 5_500_000], threshold=0.9, min_us=1_000_000, max_us=2_000_000, audio=None)
        ex = make_exec(self.root, self.roots, "segment", {"source": src, "probe": pr, "analysis": plan, "analysis_sha256": "e" * 64})
        segment.segment(ex)
        doc = json.loads((ex.package_dir / "segments.json").read_text("utf-8"))
        self.assertEqual(doc["cuts"], [])
        self.assertEqual([c["reason"] for c in doc["candidates"]], ["range_start", "duration_limit", "duration_limit"])
        self.assertEqual(doc["candidates"][0]["start_us"], 1_000_000)
        self.assertEqual(doc["candidates"][-1]["end_us"], 5_500_000)

    def test_segment_blocks_when_candidates_exceed_budget(self):
        src, pr = self.published(self.files["scenes"].name)
        plan = self.analysis(src, pr, [0, pr["duration_us"]])
        ex = make_exec(self.root, self.roots, "segment", {"source": src, "probe": pr, "analysis": plan, "analysis_sha256": "e" * 64},
                       pol=policy(max_candidates=2))
        with self.assertRaises(IngestFailure):
            segment.segment(ex)
        self.assertFalse(ex.package_dir.exists())

    def prepare_inputs(self, name, segs, fps, audio):
        src, pr = self.published(name)
        plan = self.analysis(src, pr, [0, pr["duration_us"]], audio=audio)
        sel = {"schema_version": 1, "base_plan_revision": 1, "source_sha256": src["sha256"],
               "selected_segments": [{"segment_id": f"seg_{i:04d}", "start_us": a, "end_us": b} for i, (a, b) in enumerate(segs, 1)],
               "output": {"fps": fps, "sample_rate": 48000}}
        prov = {"schema_version": 1, "source_id": src["source_id"], "source_sha256": src["sha256"], "source_version": src["source_version"],
                "run_id": "ing_1", "analysis_revision": 1, "analysis_sha256": "e" * 64, "selection_revision": 2,
                "selection_sha256": "f" * 64, "policy_sha256": "b" * 64, "probe_sha256": "d" * 64, "method_version": "",
                "profile_id": "p", "profile_revision": 1, "profile_sha256": "9" * 64, "frame_quota_version": "quota-1", "worker_version": ""}
        return {"source": src, "probe": pr, "analysis": plan, "analysis_sha256": "e" * 64, "selection": sel,
                "selection_sha256": "f" * 64, "frame_quotas": [2] * len(segs), "provenance": prov}

    def assert_sync(self, pkg: Path, fps: int, segs, audio=True):
        smap = json.loads((pkg / "source-map.json").read_text("utf-8"))
        frame = 1 / fps
        for entry, (a, b) in zip(smap["segments"], segs):
            clip = pkg / entry["media"]
            flashes, beeps = fx.measure_markers("ffmpeg", clip, audio=audio)
            expected = [m - a / 1e6 for m in fx.SYNC_MARKERS if a / 1e6 <= m < b / 1e6]
            self.assertEqual(len(flashes), len(expected), (entry["segment_id"], flashes, expected))
            for got, want in zip(flashes, expected):
                self.assertLessEqual(abs(got - want), frame + 1e-6, ("flash", got, want))
            if audio:
                self.assertEqual(len(beeps), len(expected), (entry["segment_id"], beeps, expected))
                for f, bp in zip(flashes, beeps):
                    self.assertLessEqual(abs(bp - f), frame + 1e-6, ("av skew", f, bp))
        return smap

    def test_prepare_vfr_late_audio_is_frame_accurate_and_in_sync(self):
        segs = [(1_500_000, 3_000_000), (5_000_000, 9_000_000)]
        for fps in (30, 60):
            inp = self.prepare_inputs(self.files["sync_vfr"].name, segs, fps, audio=1)
            ex = make_exec(self.root, self.roots, "media_prepare", inp, exec_id=f"exe_{fps}")
            prepare.media_prepare(ex)
            pkg = ex.package_dir
            smap = self.assert_sync(pkg, fps, segs)
            self.assertEqual(smap["source_origin_us"], 1_500_000)
            self.assertAlmostEqual(smap["audio_offset_us"], 400_000, delta=30_000)
            timeline = 0
            for entry, (a, b) in zip(smap["segments"], segs):
                frames = prepare.output_frames(a, b, fps)
                self.assertEqual(entry["output_frames"], frames)
                self.assertLessEqual(abs(entry["measured_frames"] - frames), 1)
                self.assertEqual(entry["output_samples"], frames * 48000 // fps)
                self.assertLessEqual(abs(entry["audio_sample_error"]), 48000 // fps)
                self.assertEqual(entry["timeline_in_frames"], timeline)
                timeline += frames
            edl = json.loads((pkg / "edl.json").read_text("utf-8"))
            self.assertEqual([v["ingest"]["segment_id"] for v in edl["video"]], ["seg_0001", "seg_0002"])
            self.assertEqual(len(edl["game_audio"]), 2)
            self.assertEqual(edl["voice"] + edl["subtitle"] + edl["music"], [])
            manifest = json.loads((pkg / "delivery-manifest.json").read_text("utf-8"))
            kinds = sorted(a["kind"] for a in manifest["artifacts"])
            self.assertEqual(kinds, ["edl", "ingest_provenance", "source_map", "video", "video"])
            self.assertEqual(sorted(p.name for p in (pkg / "media").iterdir()), ["segment_001.mp4", "segment_002.mp4"])

    def test_prepare_without_audio_and_reuses_finished_clips(self):
        segs = [(500_000, 2_000_000), (3_000_000, 4_000_000)]
        inp = self.prepare_inputs(self.files["sync_cfr"].name, segs, 30, audio=None)
        ex = make_exec(self.root, self.roots, "media_prepare", inp)
        prepare.media_prepare(ex)
        edl = json.loads((ex.package_dir / "edl.json").read_text("utf-8"))
        self.assertEqual(edl["game_audio"], [])
        smap = self.assert_sync(ex.package_dir, 30, segs, audio=False)
        self.assertIsNone(smap["game_audio_stream_index"])
        rows = checkpoint_rows(ex)
        inp2 = dict(inp, checkpoints=rows)
        ex2 = make_exec(self.root, self.roots, "media_prepare", inp2, exec_id="exe_2")
        logs = []
        ex2.log = logs.append
        prepare.media_prepare(ex2)
        self.assertTrue(any("reusing 2 verified prepared clips" in m for m in logs), logs)

    def test_prepare_tail_segment_of_offset_recording_ends_at_video_end(self):
        name = self.files["scenes"].name
        _, pr = self.published(name)
        segs = [(int(fx.SCENE_CUTS[-1] * 1e6), pr["duration_us"])]
        inp = self.prepare_inputs(name, segs, 30, audio=1)
        ex = make_exec(self.root, self.roots, "media_prepare", inp, exec_id="exe_tail")
        prepare.media_prepare(ex)
        entry = json.loads((ex.package_dir / "source-map.json").read_text("utf-8"))["segments"][0]
        self.assertEqual(entry["output_frames"], prepare.output_frames(*segs[0], 30))
        self.assertLessEqual(abs(entry["measured_frames"] - entry["output_frames"]), 1)


if __name__ == "__main__":
    unittest.main()
