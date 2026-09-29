import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from vac_worker.sampling import (
    FrameEvidence,
    SamplingError,
    SamplingLimits,
    _probe_frame_dimensions,
    calculate_sample_timestamps,
    normalize_relative_path,
    sample_clip_frames,
)


class TestSampling(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp_dir = Path(tempfile.mkdtemp(prefix="test_vac_sampling_"))
        # Generate synthetic test clip (3s duration, 320x240, 25fps)
        cls.synthetic_clip = cls.tmp_dir / "synthetic.mp4"
        cmd = [
            "ffmpeg",
            "-y",
            "-f",
            "lavfi",
            "-i",
            "testsrc=duration=3:size=320x240:rate=25",
            str(cls.synthetic_clip),
        ]
        res = subprocess.run(cmd, capture_output=True)
        if res.returncode != 0:
            raise RuntimeError(f"Failed to generate synthetic clip: {res.stderr}")

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmp_dir, ignore_errors=True)

    def setUp(self):
        self.scratch_dir = Path(tempfile.mkdtemp(prefix="test_scratch_"))

    def tearDown(self):
        shutil.rmtree(self.scratch_dir, ignore_errors=True)

    def test_calculate_sample_timestamps(self):
        # Invalid boundaries
        with self.assertRaises(SamplingError):
            calculate_sample_timestamps(5.0, 5.0)
        with self.assertRaises(SamplingError):
            calculate_sample_timestamps(6.0, 5.0)

        # Short clip <= 3.0s: 1 frame at midpoint
        ts_short = calculate_sample_timestamps(0.0, 2.0)
        self.assertEqual(ts_short, [1.0])

        # Medium clip 3s < dur <= 10s: 2 frames at 25% and 75%
        ts_med = calculate_sample_timestamps(1.0, 5.0)  # duration 4s -> 1 + 1=2, 1 + 3=4
        self.assertEqual(ts_med, [2.0, 4.0])

        # Long clip > 10s: 3 frames at 20%, 50%, 80%
        ts_long = calculate_sample_timestamps(0.0, 20.0)
        self.assertEqual(ts_long, [4.0, 10.0, 16.0])

        # Bounded by max_frames
        ts_capped = calculate_sample_timestamps(0.0, 20.0, max_frames=2)
        self.assertEqual(ts_capped, [4.0, 10.0])

    def test_normalize_relative_path(self):
        # Windows drive letters & backslashes
        self.assertEqual(normalize_relative_path(r"C:\workspace\clips\shot1.mp4"), "workspace/clips/shot1.mp4")
        # Relative paths with ./ and ../
        self.assertEqual(normalize_relative_path("./clips/../shots/shot1.mp4"), "shots/shot1.mp4")
        # Plain relative filename
        self.assertEqual(normalize_relative_path("media/shot1.mp4"), "media/shot1.mp4")
        # Absolute unix path
        self.assertEqual(normalize_relative_path("/var/media/shot1.mp4"), "var/media/shot1.mp4")

    def test_successful_sampling_synthetic(self):
        evidence = sample_clip_frames(
            clip_path=self.synthetic_clip,
            source_in=0.0,
            source_out=2.5,
            dest_dir=self.scratch_dir,
            relative_source_path="inputs/synthetic.mp4",
            clip_prefix="test01",
        )

        self.assertEqual(len(evidence), 1)
        ev = evidence[0]
        self.assertIsInstance(ev, FrameEvidence)
        self.assertEqual(ev.frame_index, 0)
        self.assertEqual(ev.source_path, "inputs/synthetic.mp4")
        self.assertEqual(ev.timestamp, 1.25)
        self.assertEqual(ev.width, 320)
        self.assertEqual(ev.height, 240)
        self.assertEqual(ev.path, "test01_frame_0_1.250.jpg")

        # Verify file exists on disk and matches sha256
        frame_file = self.scratch_dir / ev.path
        self.assertTrue(frame_file.is_file())
        file_bytes = frame_file.read_bytes()
        self.assertEqual(ev.sha256, hashlib.sha256(file_bytes).hexdigest())

        # Check to_dict
        d = ev.to_dict()
        self.assertEqual(d["sha256"], ev.sha256)
        self.assertNotIn("C:", d["source_path"])
        self.assertNotIn("\\", d["source_path"])

    def test_failure_on_missing_source(self):
        missing = self.tmp_dir / "does_not_exist.mp4"
        with self.assertRaises(SamplingError) as cm:
            sample_clip_frames(
                clip_path=missing,
                source_in=0.0,
                source_out=2.0,
                dest_dir=self.scratch_dir,
            )
        self.assertIn("does not exist", str(cm.exception))

    def test_failure_on_invalid_source_decode_error(self):
        # Corrupt file
        corrupt = self.scratch_dir / "corrupt.mp4"
        corrupt.write_bytes(b"NOT_A_VALID_VIDEO_STREAM")

        with self.assertRaises(SamplingError) as cm:
            sample_clip_frames(
                clip_path=corrupt,
                source_in=0.0,
                source_out=2.0,
                dest_dir=self.scratch_dir,
            )
        self.assertTrue(
            "failed" in str(cm.exception) or "could not" in str(cm.exception).lower() or "not produced" in str(cm.exception).lower()
        )

    def test_failure_on_zero_byte_frame(self):
        # Mock subprocess.run to touch a 0-byte output file
        def fake_run(cmd, *args, **kwargs):
            if "-vframes" in cmd:
                out_path = Path(cmd[-1])
                out_path.write_bytes(b"")
                class Proc:
                    returncode = 0
                    stdout = ""
                    stderr = ""
                return Proc()
            return subprocess.run(cmd, *args, **kwargs)

        with patch("subprocess.run", side_effect=fake_run):
            with self.assertRaises(SamplingError) as cm:
                sample_clip_frames(
                    clip_path=self.synthetic_clip,
                    source_in=0.0,
                    source_out=2.0,
                    dest_dir=self.scratch_dir,
                )
            self.assertIn("0 bytes", str(cm.exception))

    def test_failure_on_file_size_limit_exceeded(self):
        # Set max_bytes_per_frame to tiny 100 bytes
        limits = SamplingLimits(max_bytes_per_frame=100)
        with self.assertRaises(SamplingError) as cm:
            sample_clip_frames(
                clip_path=self.synthetic_clip,
                source_in=0.0,
                source_out=2.0,
                dest_dir=self.scratch_dir,
                limits=limits,
            )
        self.assertIn("exceeds limit", str(cm.exception))

    def test_failure_on_timeout(self):
        limits = SamplingLimits(timeout_per_clip=0.001)

        def mock_timeout(cmd, *args, **kwargs):
            raise subprocess.TimeoutExpired(cmd=cmd, timeout=0.001)

        with patch("subprocess.run", side_effect=mock_timeout):
            with self.assertRaises(SamplingError) as cm:
                sample_clip_frames(
                    clip_path=self.synthetic_clip,
                    source_in=0.0,
                    source_out=2.0,
                    dest_dir=self.scratch_dir,
                    limits=limits,
                )
            self.assertIn("timed out", str(cm.exception))

    def test_failure_on_dimension_limit_exceeded(self):
        # If dimensions exceed max_width/height after probe
        limits = SamplingLimits(max_width=100, max_height=100)
        with patch("vac_worker.sampling._probe_frame_dimensions", return_value=(200, 200)):
            with self.assertRaises(SamplingError) as cm:
                sample_clip_frames(
                    clip_path=self.synthetic_clip,
                    source_in=0.0,
                    source_out=2.0,
                    dest_dir=self.scratch_dir,
                    limits=limits,
                )
            self.assertIn("exceed maximum allowed", str(cm.exception))

    def test_deterministic_sha256_and_metadata(self):
        # Run sampling twice on the exact same synthetic clip and check deterministic output
        dir1 = self.scratch_dir / "run1"
        dir2 = self.scratch_dir / "run2"

        ev1 = sample_clip_frames(
            clip_path=self.synthetic_clip,
            source_in=0.0,
            source_out=2.0,
            dest_dir=dir1,
            relative_source_path="clips/clip0.mp4",
        )
        ev2 = sample_clip_frames(
            clip_path=self.synthetic_clip,
            source_in=0.0,
            source_out=2.0,
            dest_dir=dir2,
            relative_source_path="clips/clip0.mp4",
        )

        self.assertEqual(len(ev1), len(ev2))
        self.assertEqual(ev1[0].sha256, ev2[0].sha256)
        self.assertEqual(ev1[0].timestamp, ev2[0].timestamp)
        self.assertEqual(ev1[0].source_path, ev2[0].source_path)
        self.assertEqual(ev1[0].width, ev2[0].width)
        self.assertEqual(ev1[0].height, ev2[0].height)


if __name__ == "__main__":
    unittest.main()
