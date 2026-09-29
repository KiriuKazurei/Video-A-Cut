from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from vac_worker.content import (ClipEvidence, ContentError, NarrationDecision,
                                SceneDecision, make_content_provider, validate_narration,
                                validate_scenes)
from vac_worker.stages import STAGES, StageContext, StageFailure, Tools


class FixedTools(Tools):
    def duration(self, path: Path) -> float:
        return 2.0


class ExampleProvider:
    def recognize(self, clips):
        return [SceneDecision(0, "关卡一", "sampled_frames", 0.9)]

    def narrate(self, clips, scenes):
        if scenes[0].label != "关卡一":
            raise AssertionError("recognition did not reach narration")
        return [NarrationDecision(0, "进入关卡一", 0.2, 1.2)]


class ContentBoundaryTest(unittest.TestCase):
    def test_provider_flows_through_recognition_sort_and_narration(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / "source"
            source.mkdir()
            (source / "clip.mp4").write_bytes(b"placeholder; duration supplied by FixedTools")
            edl = {"timeline": {"fps": 30, "sample_rate": 48000},
                   "video": [{"src": "clip.mp4", "in": 0, "out": 2, "timeline_in": 0}],
                   "game_audio": [], "voice": [], "subtitle": [], "music": []}
            provider = ExampleProvider()
            for name in ("recognize", "sort", "narrate"):
                output = root / name
                output.mkdir()
                STAGES[name](StageContext(edl, source, output, FixedTools(), content_provider=provider))
                edl = json.loads((output / "edl.json").read_text(encoding="utf-8"))
                source = output
            self.assertEqual(edl["scenes"][0]["label"], "关卡一")
            self.assertEqual(edl["scenes"][0]["method"], "sampled_frames")
            self.assertEqual(edl["narration"][0]["text"], "进入关卡一")
            self.assertEqual((edl["narration"][0]["start"], edl["narration"][0]["end"]), (0.2, 1.2))
            self.assertTrue((source / edl["scenes"][0]["src"]).is_file())

    def test_invalid_provider_output_fails_closed(self):
        clips = [ClipEvidence(0, "clip.mp4", 2, 0, 2, 0)]
        with self.assertRaises(ContentError):
            make_content_provider("remote-unconfigured")
        for scenes in ([], [SceneDecision(0, "", "model")], [SceneDecision(0, "line\nbreak", "model")],
                       [SceneDecision(0, "scene", "model", float("nan"))]):
            with self.assertRaises(ContentError):
                validate_scenes(clips, scenes)
        for narration in ([NarrationDecision(0, "text", 1.8, 2.2)],
                          [NarrationDecision(0, "", 0.1, 0.5)],
                          [NarrationDecision(0, "line\nbreak", 0.1, 0.5)],
                          [NarrationDecision(0, "text", float("nan"), 1)]):
            with self.assertRaises(ContentError):
                validate_narration(clips, narration)

    def test_stage_does_not_publish_bad_provider_output(self):
        class BadProvider(ExampleProvider):
            def recognize(self, clips):
                return [SceneDecision(9, "wrong", "model")]

        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source, output = root / "source", root / "output"
            source.mkdir()
            output.mkdir()
            (source / "clip.mp4").write_bytes(b"x")
            edl = {"timeline": {"fps": 30, "sample_rate": 48000},
                   "video": [{"src": "clip.mp4", "in": 0, "out": 2, "timeline_in": 0}],
                   "game_audio": [], "voice": [], "subtitle": [], "music": []}
            with self.assertRaises(StageFailure):
                STAGES["recognize"](StageContext(edl, source, output, FixedTools(), content_provider=BadProvider()))
            self.assertFalse((output / "delivery-manifest.json").exists())

            class BrokenProvider(ExampleProvider):
                def recognize(self, clips):
                    raise RuntimeError("private provider detail")

            with self.assertRaisesRegex(StageFailure, "content provider recognize failed") as error:
                STAGES["recognize"](StageContext(edl, source, output, FixedTools(), content_provider=BrokenProvider()))
            self.assertNotIn("private provider detail", str(error.exception))
            self.assertFalse((output / "delivery-manifest.json").exists())


if __name__ == "__main__":
    unittest.main()
