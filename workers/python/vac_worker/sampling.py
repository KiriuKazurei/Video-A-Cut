"""Bounded frame sampling for content providers.

Extracts representative frames from video clips within strict resource bounds
(frame count, resolution, timeout, file size). Paths in metadata and returned
structures are strictly relative / normalized to prevent host path leaks.
"""

from __future__ import annotations

from dataclasses import asdict, dataclass
import hashlib
import json
import os
from pathlib import Path
import subprocess


class SamplingError(RuntimeError):
    """Raised when frame sampling fails or exceeds resource bounds."""


@dataclass(frozen=True)
class FrameEvidence:
    """Metadata for an extracted frame."""

    frame_index: int
    source_path: str  # Strictly relative/normalized path to source clip
    timestamp: float  # Timestamp within clip source timeline (in seconds)
    width: int
    height: int
    sha256: str  # Hex digest of the sampled frame file
    path: str  # Relative path to destination frame within staging directory

    def to_dict(self) -> dict:
        return asdict(self)


@dataclass(frozen=True)
class SamplingLimits:
    """Strict resource bounds for sampling."""

    max_frames_per_clip: int = 5
    max_width: int = 1920
    max_height: int = 1080
    max_bytes_per_frame: int = 10 * 1024 * 1024  # 10 MB
    timeout_per_clip: float = 30.0  # seconds


def calculate_sample_timestamps(
    source_in: float,
    source_out: float,
    max_frames: int = 5,
) -> list[float]:
    """Calculate bounded, deterministic sample timestamps based on clip duration.

    - Duration <= 0: invalid.
    - Duration <= 3.0s: 1 frame at midpoint.
    - Duration <= 10.0s: 2 frames at 25% and 75%.
    - Duration > 10.0s: 3 frames at 20%, 50%, 80%.
    Bounded strictly to at most max_frames timestamps, all within [source_in, source_out].
    """
    if source_out <= source_in:
        raise SamplingError(f"Invalid clip boundaries: in={source_in}, out={source_out}")

    duration = source_out - source_in
    if duration <= 3.0:
        ratios = [0.5]
    elif duration <= 10.0:
        ratios = [0.25, 0.75]
    else:
        ratios = [0.2, 0.5, 0.8]

    if len(ratios) > max_frames:
        ratios = ratios[:max_frames]

    return [round(source_in + r * duration, 3) for r in ratios]


def normalize_relative_path(path: str | Path) -> str:
    """Normalize a path to a forward-slash relative path without host leaks."""
    p_str = str(path).replace("\\", "/")
    # Strip drive letters like C:/
    if len(p_str) >= 2 and p_str[1] == ":":
        p_str = p_str[2:].lstrip("/")
    parts = [part for part in p_str.split("/") if part and part != "."]
    safe_parts: list[str] = []
    for part in parts:
        if part == "..":
            if safe_parts:
                safe_parts.pop()
            continue
        safe_parts.append(part)
    return "/".join(safe_parts) if safe_parts else "unknown"


def _probe_frame_dimensions(
    frame_path: Path,
    ffprobe_bin: str = "ffprobe",
    timeout: float = 10.0,
) -> tuple[int, int]:
    """Probe width and height of an image file."""
    cmd = [
        ffprobe_bin,
        "-v",
        "error",
        "-select_streams",
        "v:0",
        "-show_entries",
        "stream=width,height",
        "-of",
        "json",
        str(frame_path),
    ]
    try:
        proc = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=timeout,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
    except FileNotFoundError as err:
        raise SamplingError(f"ffprobe tool not found: {ffprobe_bin}") from err
    except subprocess.TimeoutExpired as err:
        raise SamplingError(f"ffprobe timed out after {timeout}s") from err

    if proc.returncode != 0:
        raise SamplingError(
            f"ffprobe failed to probe frame {frame_path.name} (exit {proc.returncode}): {proc.stderr.strip()[-200:]}"
        )

    try:
        data = json.loads(proc.stdout)
        streams = data.get("streams", [])
        if not streams:
            raise ValueError("No video streams found in frame")
        width = int(streams[0]["width"])
        height = int(streams[0]["height"])
        return width, height
    except Exception as err:
        raise SamplingError(f"Failed to parse dimensions from frame {frame_path.name}: {err}") from err


def sample_clip_frames(
    clip_path: str | Path,
    source_in: float,
    source_out: float,
    dest_dir: str | Path,
    *,
    relative_source_path: str | None = None,
    clip_prefix: str = "clip",
    limits: SamplingLimits | None = None,
    ffmpeg_bin: str = "ffmpeg",
    ffprobe_bin: str = "ffprobe",
) -> list[FrameEvidence]:
    """Sample representative frames from a video clip within bounds.

    Args:
        clip_path: Path to the video clip on disk (absolute or relative).
        source_in: Clip in-point in seconds.
        source_out: Clip out-point in seconds.
        dest_dir: Target directory for sampled image files (staging/scratch).
        relative_source_path: Normalized relative identifier for the clip.
            If None, derived cleanly from clip_path without leaking absolute paths.
        clip_prefix: Prefix used for naming extracted frame files.
        limits: Resource limits configuration.
        ffmpeg_bin: ffmpeg executable name or path.
        ffprobe_bin: ffprobe executable name or path.

    Returns:
        List of FrameEvidence describing the sampled frames.

    Raises:
        SamplingError: On decoding errors, missing input, timeout, 0-byte output,
            or boundary violations (frames count, resolution, file size).
    """
    if limits is None:
        limits = SamplingLimits()

    clip_path = Path(clip_path)
    if not clip_path.is_file():
        raise SamplingError(f"Source clip does not exist or is not a file: {clip_path.name}")

    dest_dir = Path(dest_dir)
    dest_dir.mkdir(parents=True, exist_ok=True)

    rel_source = (
        relative_source_path
        if relative_source_path is not None
        else normalize_relative_path(clip_path.name)
    )

    timestamps = calculate_sample_timestamps(
        source_in, source_out, max_frames=limits.max_frames_per_clip
    )

    if len(timestamps) > limits.max_frames_per_clip:
        raise SamplingError(
            f"Requested frame count {len(timestamps)} exceeds limit {limits.max_frames_per_clip}"
        )

    scale_filter = (
        f"scale=w=min({limits.max_width}\\,iw):h=min({limits.max_height}\\,ih):"
        f"force_original_aspect_ratio=decrease"
    )

    evidence_list: list[FrameEvidence] = []

    for idx, ts in enumerate(timestamps):
        out_filename = f"{clip_prefix}_frame_{idx}_{ts:.3f}.jpg"
        out_path = dest_dir / out_filename

        cmd = [
            ffmpeg_bin,
            "-y",
            "-ss",
            f"{ts:.3f}",
            "-i",
            str(clip_path),
            "-vf",
            scale_filter,
            "-vframes",
            "1",
            str(out_path),
        ]

        try:
            proc = subprocess.run(
                cmd,
                capture_output=True,
                text=True,
                timeout=limits.timeout_per_clip,
                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
            )
        except FileNotFoundError as err:
            raise SamplingError(f"ffmpeg tool not found: {ffmpeg_bin}") from err
        except subprocess.TimeoutExpired as err:
            raise SamplingError(
                f"Sampling clip {clip_path.name} timed out after {limits.timeout_per_clip}s"
            ) from err

        if proc.returncode != 0:
            raise SamplingError(
                f"ffmpeg frame extraction failed (exit {proc.returncode}): {proc.stderr.strip()[-300:]}"
            )

        if not out_path.is_file():
            raise SamplingError(f"Frame was not produced by ffmpeg: {out_filename}")

        file_bytes = out_path.read_bytes()
        byte_size = len(file_bytes)
        if byte_size == 0:
            raise SamplingError(f"Extracted frame is 0 bytes: {out_filename}")

        if byte_size > limits.max_bytes_per_frame:
            raise SamplingError(
                f"Frame size {byte_size} bytes exceeds limit {limits.max_bytes_per_frame} bytes: {out_filename}"
            )

        width, height = _probe_frame_dimensions(
            out_path, ffprobe_bin=ffprobe_bin, timeout=limits.timeout_per_clip
        )

        if width > limits.max_width or height > limits.max_height:
            raise SamplingError(
                f"Frame dimensions ({width}x{height}) exceed maximum allowed ({limits.max_width}x{limits.max_height})"
            )

        hasher = hashlib.sha256()
        hasher.update(file_bytes)
        sha256_hex = hasher.hexdigest()

        rel_frame_path = normalize_relative_path(out_filename)

        evidence_list.append(
            FrameEvidence(
                frame_index=idx,
                source_path=rel_source,
                timestamp=ts,
                width=width,
                height=height,
                sha256=sha256_hex,
                path=rel_frame_path,
            )
        )

    return evidence_list


# Alias for compatibility if requested by consumers
extract_clip_frames = sample_clip_frames
