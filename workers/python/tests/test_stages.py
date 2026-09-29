"""Stage chain on real synthetic media (needs ffmpeg/ffprobe on PATH)."""
from __future__ import annotations

import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

from vac_worker.stages import STAGES, StageContext, StageFailure, Tools

HAVE_FFMPEG = shutil.which("ffmpeg") and shutil.which("ffprobe")


def ff(*args):
    subprocess.run(["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", *args], check=True)


def make_source(root: Path) -> dict:
    root.mkdir(parents=True)
    ff("-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30:duration=2", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=2",
       "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", str(root / "b.mp4"))
    ff("-f", "lavfi", "-i", "smptebars=size=160x90:rate=30:duration=2", "-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=2",
       "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", str(root / "a.mp4"))
    edl = {"timeline": {"fps": 30, "sample_rate": 48000},
           # out of order on purpose; sort must fix the order and close the gap
           "video": [{"src": "b.mp4", "in": 0, "out": 1.5, "timeline_in": 3}, {"src": "a.mp4", "in": 0.5, "out": 2, "timeline_in": 0}],
           "game_audio": [{"src": "b.mp4", "in": 0, "out": 1.5, "timeline_in": 3, "gain_db": -12},
                          {"src": "a.mp4", "in": 0.5, "out": 2, "timeline_in": 0, "gain_db": -12}],
           "voice": [], "subtitle": [], "music": []}
    (root / "edl.json").write_text(json.dumps(edl))
    return edl


@unittest.skipUnless(HAVE_FFMPEG, "ffmpeg/ffprobe not on PATH")
class StageChainTest(unittest.TestCase):
    def test_full_chain_produces_self_contained_packages(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            src_dir = tmp / "src"
            edl = make_source(src_dir)
            current_dir, current_edl = src_dir, edl
            for i, name in enumerate(["recognize", "sort", "narrate", "tts", "subtitle", "mix"]):
                out = tmp / f"p{i}_{name}"
                out.mkdir()
                STAGES[name](StageContext(edl=current_edl, source_dir=current_dir, out_dir=out,
                                          tools=Tools(allow_test_tone=True)))
                current_edl = json.loads((out / "edl.json").read_text(encoding="utf-8"))
                manifest = json.loads((out / "delivery-manifest.json").read_text())
                paths = {a["path"] for a in manifest["artifacts"]}
                for track in ("video", "game_audio", "voice", "music"):
                    for item in current_edl[track]:
                        self.assertIn(item["src"], paths, f"{name}: {track} src not in manifest")
                        self.assertTrue((out / item["src"]).is_file(), f"{name}: {item['src']} missing")
                current_dir = out
            self.assertEqual([v["timeline_in"] for v in current_edl["video"]], [0, 1.5])
            self.assertEqual(current_edl["video"][0]["src"].endswith("a.mp4"), True)
            self.assertEqual(len(current_edl["scenes"]), 2)
            self.assertEqual(len(current_edl["voice"]), 2)
            self.assertEqual(len(current_edl["subtitle"]), 2)
            for caption, speech in zip(current_edl["subtitle"], current_edl["voice"]):
                self.assertEqual((caption["start"], caption["end"]), (speech["start"], speech["end"]))
            self.assertTrue(current_edl["music"][0]["duck"])
            self.assertEqual(current_edl["music"][0]["end"], 3.0)
            # Real durations match the EDL spans (export sync check tolerance = 1 frame).
            tools = Tools()
            for v in current_edl["voice"]:
                self.assertAlmostEqual(tools.duration(current_dir / v["src"]), v["end"] - v["start"], delta=1 / 30)
            self.assertAlmostEqual(tools.duration(current_dir / current_edl["music"][0]["src"]), 3.0, delta=1 / 30)

    def test_refuses_escaping_and_missing_sources(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            edl = make_source(tmp / "src")
            for bad in ("../x.mp4", "C:/x.mp4", "nope.mp4", "sub\\a.mp4"):
                e = json.loads(json.dumps(edl))
                e["video"][0]["src"] = bad
                out = tmp / f"o{abs(hash(bad))}"
                out.mkdir()
                with self.assertRaises(StageFailure, msg=bad):
                    STAGES["recognize"](StageContext(edl=e, source_dir=tmp / "src", out_dir=out, tools=Tools()))

    def test_tts_and_subtitle_require_narration(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            edl = make_source(tmp / "src")
            for name in ("tts", "subtitle"):
                out = tmp / name
                out.mkdir()
                with self.assertRaisesRegex(StageFailure, "narration"):
                    STAGES[name](StageContext(edl=edl, source_dir=tmp / "src", out_dir=out, tools=Tools()))

    def test_tts_never_silently_substitutes_a_tone(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            edl = make_source(tmp / "src")
            edl["narration"] = [{"id": "nar_001", "text": "测试旁白", "start": 0.2, "end": 1.0}]
            out = tmp / "tts"
            out.mkdir()
            with self.assertRaises(StageFailure):
                STAGES["tts"](StageContext(edl=edl, source_dir=tmp / "src", out_dir=out,
                                           tools=Tools(powershell="missing-speech-command")))
            self.assertFalse((out / "delivery-manifest.json").exists())

    def test_recognize_rejects_span_past_media(self):
        with tempfile.TemporaryDirectory() as tmp:
            tmp = Path(tmp)
            edl = make_source(tmp / "src")
            edl["video"][0]["out"] = 9
            out = tmp / "o"
            out.mkdir()
            with self.assertRaisesRegex(StageFailure, "exceeds media duration"):
                STAGES["recognize"](StageContext(edl=edl, source_dir=tmp / "src", out_dir=out, tools=Tools()))


if __name__ == "__main__":
    unittest.main()
