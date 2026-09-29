from __future__ import annotations

import io
import json
import os
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

from vac_worker.content import (ClipEvidence, ContentError, NarrationContentProvider, NarrationDecision,
                                SceneDecision, VisionContentProvider, make_content_provider,
                                validate_narration, validate_scenes)
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


class TestVisionContentProvider(unittest.TestCase):
    def setUp(self):
        self.clips = [
            ClipEvidence(
                index=0,
                src="clip1.mp4",
                media_duration=5.0,
                source_in=0.0,
                source_out=5.0,
                timeline_in=0.0,
                frames=(
                    {"index": 0, "pts": 1.0, "path": Path("/tmp/frame0.jpg"), "sha256": "hash0"},
                    {"index": 1, "pts": 3.0, "path": Path("/tmp/frame1.jpg"), "sha256": "hash1"},
                )
            )
        ]

    def test_disabled_by_default(self):
        provider = VisionContentProvider(endpoint="http://example.com/api")
        with self.assertRaisesRegex(ContentError, "external transmission is disabled"):
            provider.recognize(self.clips)

    def test_missing_token_env(self):
        provider = VisionContentProvider(
            endpoint="http://example.com/api",
            allow_external=True,
            token_env="NON_EXISTENT_TOKEN_ENV_VAR_12345",
        )
        with self.assertRaisesRegex(ContentError, "missing credentials in environment variable"):
            provider.recognize(self.clips)

    def test_missing_endpoint(self):
        with tempfile.TemporaryDirectory() as temp:
            env_var = "TEST_VAC_TOKEN"
            os.environ[env_var] = "secret-token"
            try:
                provider = VisionContentProvider(
                    endpoint="",
                    allow_external=True,
                    token_env=env_var,
                )
                with self.assertRaisesRegex(ContentError, "missing vision endpoint configuration"):
                    provider.recognize(self.clips)
            finally:
                os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_connection_error(self, mock_urlopen):
        mock_urlopen.side_effect = urllib.error.URLError("Connection refused")
        env_var = "TEST_VAC_TOKEN"
        os.environ[env_var] = "secret-token"
        try:
            provider = VisionContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "vision endpoint connection error"):
                provider.recognize(self.clips)
        finally:
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_timeout_error(self, mock_urlopen):
        mock_urlopen.side_effect = TimeoutError("timed out")
        env_var = "TEST_VAC_TOKEN"
        os.environ[env_var] = "secret-token"
        try:
            provider = VisionContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "vision endpoint timed out"):
                provider.recognize(self.clips)
        finally:
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_http_error_redacts_token(self, mock_urlopen):
        secret = "super_secret_bearer_token"
        error_file = io.BytesIO(b"Unauthorized access")
        http_err = urllib.error.HTTPError(
            "http://example.com/api", 401, f"Unauthorized with token {secret}", {}, error_file
        )
        mock_urlopen.side_effect = http_err
        env_var = "TEST_VAC_TOKEN"
        os.environ[env_var] = secret
        try:
            provider = VisionContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaises(ContentError) as cm:
                provider.recognize(self.clips)
            self.assertNotIn(secret, str(cm.exception))
            self.assertIn("401", str(cm.exception))
        finally:
            error_file.close()
            http_err.close()
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_bad_json_response(self, mock_urlopen):
        mock_resp = mock.MagicMock()
        mock_resp.read.return_value = b"Not valid json <html"
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        env_var = "TEST_VAC_TOKEN"
        os.environ[env_var] = "secret-token"
        try:
            provider = VisionContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "vision endpoint returned invalid JSON"):
                provider.recognize(self.clips)
        finally:
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_valid_response_creates_decisions(self, mock_urlopen):
        mock_resp = mock.MagicMock()
        resp_data = {
            "decisions": [
                {
                    "clip_index": 0,
                    "label": "Boss Fight",
                    "confidence": 0.95,
                    "evidence_frames": ["hash0", "hash1"],
                    "sequence_rank": 1,
                }
            ]
        }
        mock_resp.read.return_value = json.dumps(resp_data).encode("utf-8")
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        env_var = "TEST_VAC_TOKEN"
        os.environ[env_var] = "secret-token"
        try:
            provider = VisionContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            decisions = provider.recognize(self.clips)
            self.assertEqual(len(decisions), 1)
            d = decisions[0]
            self.assertEqual(d.clip_index, 0)
            self.assertEqual(d.label, "Boss Fight")
            self.assertEqual(d.method, "vision")
            self.assertEqual(d.confidence, 0.95)
            self.assertEqual(d.evidence_frames, ("hash0", "hash1"))
            self.assertEqual(d.sequence_rank, 1)

            # verify Authorization header and payload
            req = mock_urlopen.call_args[0][0]
            self.assertEqual(req.get_header("Authorization"), "Bearer secret-token")
            payload = json.loads(req.data.decode("utf-8"))
            self.assertEqual(len(payload["clips"]), 1)
            self.assertEqual(len(payload["clips"][0]["frames"]), 2)
            self.assertEqual(payload["clips"][0]["frames"][0]["sha256"], "hash0")
        finally:
            os.environ.pop(env_var, None)

    def test_narrate_not_implemented(self):
        provider = VisionContentProvider(endpoint="http://example.com/api")
        with self.assertRaisesRegex(ContentError, "narration is not implemented"):
            provider.narrate(self.clips, [])


class NarrationContentProviderTest(unittest.TestCase):
    def setUp(self):
        self.clips = [
            ClipEvidence(
                index=0,
                src="clip0.mp4",
                media_duration=5.0,
                source_in=0.0,
                source_out=5.0,
                timeline_in=0.0,
            )
        ]
        self.scenes = [
            SceneDecision(
                clip_index=0,
                label="Boss Fight",
                method="vision",
                confidence=0.9,
            )
        ]

    def test_disabled_by_default(self):
        provider = NarrationContentProvider()
        with self.assertRaisesRegex(ContentError, "external transmission is disabled"):
            provider.narrate(self.clips, self.scenes)

    def test_missing_token_fails_closed(self):
        env_var = "TEST_MISSING_NARRATION_TOKEN"
        os.environ.pop(env_var, None)
        provider = NarrationContentProvider(
            endpoint="http://example.com/api",
            allow_external=True,
            token_env=env_var,
        )
        with self.assertRaisesRegex(ContentError, "missing credentials"):
            provider.narrate(self.clips, self.scenes)

    def test_missing_endpoint_fails_closed(self):
        env_var = "TEST_NARRATION_TOKEN"
        os.environ[env_var] = "token123"
        try:
            provider = NarrationContentProvider(
                endpoint="",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "missing narration endpoint"):
                provider.narrate(self.clips, self.scenes)
        finally:
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_network_failure_fails_closed(self, mock_urlopen):
        mock_urlopen.side_effect = urllib.error.URLError("connection refused")
        env_var = "TEST_NARRATION_TOKEN"
        os.environ[env_var] = "token123"
        try:
            provider = NarrationContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "network error"):
                provider.narrate(self.clips, self.scenes)
        finally:
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_http_error_fails_closed(self, mock_urlopen):
        err_fp = io.BytesIO(b"bad gateway")
        err = urllib.error.HTTPError(
            url="http://example.com/api",
            code=502,
            msg="Bad Gateway",
            hdrs={},
            fp=err_fp,
        )
        mock_urlopen.side_effect = err
        env_var = "TEST_NARRATION_TOKEN"
        os.environ[env_var] = "token123"
        try:
            provider = NarrationContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "HTTP error: 502"):
                provider.narrate(self.clips, self.scenes)
        finally:
            err.close()
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_invalid_json_fails_closed(self, mock_urlopen):
        mock_resp = mock.MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = b"not json"
        mock_urlopen.return_value = mock_resp

        env_var = "TEST_NARRATION_TOKEN"
        os.environ[env_var] = "token123"
        try:
            provider = NarrationContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
            )
            with self.assertRaisesRegex(ContentError, "invalid JSON"):
                provider.narrate(self.clips, self.scenes)
        finally:
            os.environ.pop(env_var, None)

    @mock.patch("urllib.request.urlopen")
    def test_successful_narration(self, mock_urlopen):
        api_response = {
            "model_version": "gpt-4o-2024-08-06",
            "narrations": [
                {
                    "clip_index": 0,
                    "text": "玩家进入首领战区域，准备迎战强大敌人。",
                    "start": 0.5,
                    "end": 3.5,
                    "source_scene_label": "Boss Fight",
                    "needs_review": True,
                }
            ],
        }
        mock_resp = mock.MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = json.dumps(api_response).encode("utf-8")
        mock_urlopen.return_value = mock_resp

        env_var = "TEST_NARRATION_TOKEN"
        os.environ[env_var] = "secret-token"
        try:
            provider = NarrationContentProvider(
                endpoint="http://example.com/api",
                allow_external=True,
                token_env=env_var,
                model="gpt-4o",
            )
            decisions = provider.narrate(self.clips, self.scenes)
            self.assertEqual(len(decisions), 1)
            d = decisions[0]
            self.assertEqual(d.clip_index, 0)
            self.assertEqual(d.text, "玩家进入首领战区域，准备迎战强大敌人。")
            self.assertEqual(d.start, 0.5)
            self.assertEqual(d.end, 3.5)
            self.assertEqual(d.duration, 3.0)
            self.assertEqual(d.source_scene_label, "Boss Fight")
            self.assertEqual(d.model_version, "gpt-4o-2024-08-06")
            self.assertTrue(d.needs_review)

            # verify Authorization header and payload
            req = mock_urlopen.call_args[0][0]
            self.assertEqual(req.get_header("Authorization"), "Bearer secret-token")
            payload = json.loads(req.data.decode("utf-8"))
            self.assertEqual(len(payload["clips"]), 1)
            self.assertEqual(payload["clips"][0]["scene_label"], "Boss Fight")
        finally:
            os.environ.pop(env_var, None)

    def test_validation_constraints(self):
        clips = [ClipEvidence(0, "clip.mp4", 5.0, 0.0, 5.0, 0.0)]
        # > 160 chars
        with self.assertRaisesRegex(ContentError, "narration text is empty or too long"):
            validate_narration(clips, [NarrationDecision(0, "测" * 161, 0.5, 2.0)])
        # empty text
        with self.assertRaisesRegex(ContentError, "narration text is empty or too long"):
            validate_narration(clips, [NarrationDecision(0, "   ", 0.5, 2.0)])
        # newline character
        with self.assertRaisesRegex(ContentError, "newline or control"):
            validate_narration(clips, [NarrationDecision(0, "第一行\n第二行", 0.5, 2.0)])
        # tab or control character
        with self.assertRaisesRegex(ContentError, "newline or control"):
            validate_narration(clips, [NarrationDecision(0, "前\t后", 0.5, 2.0)])
        # escape clip time window
        with self.assertRaisesRegex(ContentError, "narration window escapes its clip"):
            validate_narration(clips, [NarrationDecision(0, "有效解说", 4.0, 6.0)])
        # invalid clip index
        with self.assertRaisesRegex(ContentError, "invalid or repeated clip index"):
            validate_narration(clips, [NarrationDecision(99, "有效解说", 0.5, 2.0)])
        # duplicate clip index
        with self.assertRaisesRegex(ContentError, "invalid or repeated clip index"):
            validate_narration(clips, [
                NarrationDecision(0, "解说一", 0.5, 1.5),
                NarrationDecision(0, "解说二", 1.5, 2.5),
            ])
        # source_scene_label too long
        with self.assertRaisesRegex(ContentError, "source_scene_label"):
            validate_narration(clips, [NarrationDecision(0, "有效解说", 0.5, 2.0, source_scene_label="s" * 101)])
        # model_version too long
        with self.assertRaisesRegex(ContentError, "model_version"):
            validate_narration(clips, [NarrationDecision(0, "有效解说", 0.5, 2.0, model_version="m" * 101)])


if __name__ == "__main__":
    unittest.main()
