"""Phase 4 content-provider boundary for scene analysis and narration.

The built-in provider preserves the verified phase-3 behavior. Future model
adapters implement the protocol and return bounded, reviewable decisions;
they must be selected explicitly rather than silently replacing the built-in.
"""
from __future__ import annotations

import json
import math
import os
import socket
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol, Sequence


class ContentError(ValueError):
    """Provider output is missing, unsafe, or inconsistent with the timeline."""


@dataclass(frozen=True)
class ClipEvidence:
    index: int
    src: str
    media_duration: float
    source_in: float
    source_out: float
    timeline_in: float
    frames: tuple[Any, ...] = ()


@dataclass(frozen=True)
class SceneDecision:
    clip_index: int
    label: str
    method: str
    confidence: float | None = None
    evidence_frames: tuple[str, ...] = ()
    sequence_rank: int | None = None


@dataclass(frozen=True)
class NarrationDecision:
    clip_index: int
    text: str
    start: float
    end: float
    source_scene_label: str = ""
    model_version: str = ""
    needs_review: bool = True

    @property
    def duration(self) -> float:
        return self.end - self.start


class ContentProvider(Protocol):
    def recognize(self, clips: Sequence[ClipEvidence]) -> Sequence[SceneDecision]: ...

    def narrate(self, clips: Sequence[ClipEvidence], scenes: Sequence[SceneDecision]) -> Sequence[NarrationDecision]: ...


class BuiltinContentProvider:
    """Metadata labels and deterministic narration, never described as AI."""

    def recognize(self, clips: Sequence[ClipEvidence]) -> Sequence[SceneDecision]:
        return [SceneDecision(clip_index=c.index, label=f"scene_{c.index + 1:02d}", method="metadata_only")
                for c in clips]

    def narrate(self, clips: Sequence[ClipEvidence], scenes: Sequence[SceneDecision]) -> Sequence[NarrationDecision]:
        lines = []
        scenes_by_idx = {s.clip_index: s for s in scenes}
        for clip in clips:
            span = clip.source_out - clip.source_in
            start = clip.timeline_in + min(0.2, span / 4)
            end = min(clip.timeline_in + span - 0.1, start + max(0.6, span * 0.6))
            if end > start:
                scene = scenes_by_idx.get(clip.index)
                scene_label = scene.label if scene else ""
                lines.append(NarrationDecision(
                    clip_index=clip.index,
                    text=f"第 {clip.index + 1} 段",
                    start=round(start, 3),
                    end=round(end, 3),
                    source_scene_label=scene_label,
                    model_version="builtin",
                    needs_review=True,
                ))
        return lines


class VisionContentProvider:
    """External vision-based scene recognition adapter with strict fail-closed boundary."""

    def __init__(
        self,
        endpoint: str | None = None,
        token_env: str = "VAC_VISION_TOKEN",
        allow_external: bool = False,
        timeout_seconds: float = 30.0,
        model: str = "default",
        opener: Any = None,
    ) -> None:
        self.endpoint = endpoint
        self.token_env = token_env or "VAC_VISION_TOKEN"
        self.allow_external = bool(allow_external)
        self.timeout_seconds = float(timeout_seconds)
        self.model = model
        self._opener = opener or urllib.request.urlopen

    def recognize(self, clips: Sequence[ClipEvidence]) -> list[SceneDecision]:
        if not self.allow_external:
            raise ContentError("external transmission is disabled (allow_external is false)")

        token = os.environ.get(self.token_env)
        if not token or not token.strip():
            raise ContentError(f"missing credentials in environment variable '{self.token_env}'")
        token = token.strip()

        if not self.endpoint or not self.endpoint.strip():
            raise ContentError("missing vision endpoint configuration")
        endpoint = self.endpoint.strip()

        clip_by_idx = {c.index: c for c in clips}
        clip_payloads = []
        for c in clips:
            frames_data = []
            for f in c.frames:
                if hasattr(f, "to_dict"):
                    fd = f.to_dict()
                elif isinstance(f, dict):
                    fd = dict(f)
                else:
                    fd = {"reference": str(f)}
                clean_fd = {}
                for k, v in fd.items():
                    if isinstance(v, Path):
                        clean_fd[k] = str(v)
                    else:
                        clean_fd[k] = v
                frames_data.append(clean_fd)
            clip_payloads.append({
                "clip_index": c.index,
                "src": c.src,
                "media_duration": c.media_duration,
                "source_in": c.source_in,
                "source_out": c.source_out,
                "timeline_in": c.timeline_in,
                "frames": frames_data,
            })

        body = json.dumps({
            "model": self.model,
            "clips": clip_payloads,
        }).encode("utf-8")

        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json",
            "Authorization": f"Bearer {token}",
        }
        req = urllib.request.Request(endpoint, data=body, headers=headers, method="POST")

        def _redact(text: str) -> str:
            if token and token in text:
                return text.replace(token, "[REDACTED]")
            return text

        try:
            with self._opener(req, timeout=self.timeout_seconds) as resp:
                raw_bytes = resp.read()
        except urllib.error.HTTPError as err:
            reason = _redact(str(err.reason) if getattr(err, "reason", None) else str(err.code))
            raise ContentError(f"vision endpoint HTTP error {err.code}: {reason}") from None
        except (TimeoutError, socket.timeout) as err:
            raise ContentError(f"vision endpoint timed out: {_redact(str(err))}") from None
        except urllib.error.URLError as err:
            err_reason = _redact(str(err.reason))
            if isinstance(err.reason, (TimeoutError, socket.timeout)) or "timed out" in err_reason.lower():
                raise ContentError(f"vision endpoint timed out: {err_reason}") from None
            raise ContentError(f"vision endpoint connection error: {err_reason}") from None
        except OSError as err:
            err_str = _redact(str(err))
            if "timed out" in err_str.lower():
                raise ContentError(f"vision endpoint timed out: {err_str}") from None
            raise ContentError(f"vision endpoint connection error: {err_str}") from None
        except Exception as err:
            raise ContentError(f"vision endpoint request failed: {_redact(str(err))}") from None

        try:
            data = json.loads(raw_bytes.decode("utf-8"))
        except Exception as err:
            raise ContentError(f"vision endpoint returned invalid JSON: {_redact(str(err))}") from None

        if isinstance(data, dict):
            raw_scenes = data.get("scenes")
            if raw_scenes is None:
                raw_scenes = data.get("decisions")
            if raw_scenes is None:
                raw_scenes = data.get("results")
        elif isinstance(data, list):
            raw_scenes = data
        else:
            raise ContentError("vision endpoint response must be a JSON object or array")

        if not isinstance(raw_scenes, list):
            raise ContentError("vision endpoint response missing scenes list")

        decisions: list[SceneDecision] = []
        for item in raw_scenes:
            if not isinstance(item, dict):
                raise ContentError("vision scene item must be an object")
            clip_idx = item.get("clip_index")
            if clip_idx is None:
                clip_idx = item.get("index")
            label = item.get("label")
            if label is None:
                raise ContentError("vision scene item missing label")
            method = item.get("method", "vision")
            confidence = item.get("confidence")
            seq_rank = item.get("sequence_rank")

            ev_frames = item.get("evidence_frames")
            if ev_frames is None:
                clip_match = clip_by_idx.get(clip_idx)
                if clip_match and clip_match.frames:
                    ev_frames = tuple(
                        getattr(f, "sha256", getattr(f, "path", str(f)))
                        if not isinstance(f, dict)
                        else (f.get("sha256") or f.get("path") or str(f))
                        for f in clip_match.frames
                    )
                else:
                    ev_frames = ()
            elif isinstance(ev_frames, (list, tuple)):
                ev_frames = tuple(str(x) for x in ev_frames)
            else:
                raise ContentError("vision scene evidence_frames must be a sequence")

            decisions.append(
                SceneDecision(
                    clip_index=clip_idx,
                    label=str(label),
                    method=str(method),
                    confidence=confidence,
                    evidence_frames=ev_frames,
                    sequence_rank=seq_rank,
                )
            )

        return validate_scenes(clips, decisions)

    def narrate(self, clips: Sequence[ClipEvidence], scenes: Sequence[SceneDecision]) -> Sequence[NarrationDecision]:
        raise ContentError("narration is not implemented for VisionContentProvider")


class NarrationContentProvider:
    """External LLM-based narration adapter with strict fail-closed boundary."""

    def __init__(
        self,
        endpoint: str | None = None,
        token_env: str = "VAC_NARRATION_TOKEN",
        allow_external: bool = False,
        timeout_seconds: float = 30.0,
        model: str = "default",
        opener: Any = None,
    ) -> None:
        self.endpoint = endpoint
        self.token_env = token_env or "VAC_NARRATION_TOKEN"
        self.allow_external = bool(allow_external)
        self.timeout_seconds = float(timeout_seconds)
        self.model = model
        self._opener = opener or urllib.request.urlopen

    def recognize(self, clips: Sequence[ClipEvidence]) -> Sequence[SceneDecision]:
        raise ContentError("recognition is not implemented for NarrationContentProvider")

    def narrate(self, clips: Sequence[ClipEvidence], scenes: Sequence[SceneDecision]) -> Sequence[NarrationDecision]:
        if not self.allow_external:
            raise ContentError("external transmission is disabled (allow_external is false)")

        token = os.environ.get(self.token_env)
        if not token or not token.strip():
            raise ContentError(f"missing credentials in environment variable '{self.token_env}'")
        token = token.strip()

        if not self.endpoint or not self.endpoint.strip():
            raise ContentError("missing narration endpoint configuration")
        endpoint = self.endpoint.strip()

        scene_map = {s.clip_index: s for s in scenes}
        clip_payloads = []
        for c in clips:
            s = scene_map.get(c.index)
            clip_len = c.source_out - c.source_in
            clip_payloads.append({
                "clip_index": c.index,
                "scene_label": s.label if s else "",
                "timeline_in": c.timeline_in,
                "timeline_out": c.timeline_in + clip_len,
                "duration": clip_len,
            })

        body = json.dumps({
            "model": self.model,
            "clips": clip_payloads,
        }).encode("utf-8")

        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json",
            "Authorization": f"Bearer {token}",
        }

        req = urllib.request.Request(
            endpoint,
            data=body,
            headers=headers,
            method="POST",
        )

        try:
            resp = self._opener(req, timeout=self.timeout_seconds)
            status = getattr(resp, "status", None) or getattr(resp, "code", 200)
            if status >= 400:
                raise ContentError(f"external narration service returned HTTP {status}")
            raw_data = resp.read()
        except ContentError:
            raise
        except urllib.error.HTTPError as err:
            raise ContentError(f"external narration service HTTP error: {err.code}") from err
        except urllib.error.URLError as err:
            raise ContentError(f"external narration service network error: {err.reason}") from err
        except TimeoutError as err:
            raise ContentError("external narration service timed out") from err
        except Exception as err:
            raise ContentError(f"external narration service request failed ({type(err).__name__})") from err

        try:
            parsed = json.loads(raw_data.decode("utf-8"))
        except Exception as err:
            raise ContentError(f"external narration service returned invalid JSON: {type(err).__name__}") from err

        if not isinstance(parsed, dict) or "narrations" not in parsed:
            raise ContentError("external narration response missing 'narrations' list")

        raw_narrations = parsed["narrations"]
        if not isinstance(raw_narrations, (list, tuple)):
            raise ContentError("external narration 'narrations' must be a sequence")

        model_version = str(parsed.get("model_version") or self.model or "unknown")
        decisions: list[NarrationDecision] = []

        clip_by_idx = {c.index: c for c in clips}
        for item in raw_narrations:
            if not isinstance(item, dict):
                raise ContentError("narration item must be a JSON object")
            clip_idx = item.get("clip_index")
            if clip_idx is None or type(clip_idx) is not int:
                raise ContentError("narration item missing valid integer clip_index")
            text = item.get("text")
            if text is None:
                raise ContentError("narration item missing text")

            clip = clip_by_idx.get(clip_idx)
            if not clip:
                raise ContentError(f"narration references unknown clip_index {clip_idx}")

            clip_len = clip.source_out - clip.source_in
            start = item.get("start")
            end = item.get("end")
            duration = item.get("duration")

            if start is None:
                start = clip.timeline_in + 0.2
            if end is None:
                if duration is not None and isinstance(duration, (int, float)):
                    end = start + float(duration)
                else:
                    end = min(start + 1.0, clip.timeline_in + clip_len)

            scene_item = scene_map.get(clip_idx)
            src_label = str(item.get("source_scene_label") or (scene_item.label if scene_item else ""))
            needs_review = bool(item.get("needs_review", True))
            item_model = str(item.get("model_version") or model_version)

            decisions.append(
                NarrationDecision(
                    clip_index=clip_idx,
                    text=str(text),
                    start=float(start),
                    end=float(end),
                    source_scene_label=src_label,
                    model_version=item_model,
                    needs_review=needs_review,
                )
            )

        return validate_narration(clips, decisions)


ExternalVisionProvider = VisionContentProvider
ExternalNarrationProvider = NarrationContentProvider


def make_content_provider(name: str, **kwargs: Any) -> ContentProvider:
    if name == "builtin":
        return BuiltinContentProvider()
    if name in ("vision", "external_vision", "vision_adapter"):
        return VisionContentProvider(**kwargs)
    if name in ("narration", "external_narration", "llm_narration", "narration_adapter"):
        return NarrationContentProvider(**kwargs)
    raise ContentError(f"unknown content provider: {name!r} is unavailable; configure an installed adapter explicitly")


def validate_scenes(clips: Sequence[ClipEvidence], decisions: Sequence[SceneDecision]) -> list[SceneDecision]:
    if len(decisions) != len(clips):
        raise ContentError("recognition must return one scene per clip")
    by_index = {}
    for item in decisions:
        if not isinstance(item, SceneDecision) or type(item.clip_index) is not int or item.clip_index in by_index:
            raise ContentError("recognition returned an invalid or repeated clip index")
        if not isinstance(item.label, str) or not item.label.strip() or not item.label.isprintable() or len(item.label) > 100:
            raise ContentError("recognition label is empty or too long")
        if not isinstance(item.method, str) or not item.method.strip() or not item.method.isprintable() or len(item.method) > 40:
            raise ContentError("recognition method is missing or too long")
        if item.confidence is not None and (not isinstance(item.confidence, (int, float)) or
                                            not math.isfinite(item.confidence) or not 0 <= item.confidence <= 1):
            raise ContentError("recognition confidence must be between 0 and 1")
        if item.sequence_rank is not None:
            if type(item.sequence_rank) is not int or item.sequence_rank < 0:
                raise ContentError("recognition sequence_rank must be a non-negative integer or None")
        if not isinstance(item.evidence_frames, (list, tuple)):
            raise ContentError("recognition evidence_frames must be a sequence")
        for f in item.evidence_frames:
            if not isinstance(f, str) or not f.strip() or not f.isprintable() or len(f) > 256:
                raise ContentError("recognition evidence_frame must be a non-empty printable string under 256 chars")
        by_index[item.clip_index] = item
    if set(by_index) != {clip.index for clip in clips}:
        raise ContentError("recognition clip indices differ from the input")
    return [by_index[clip.index] for clip in clips]


def validate_narration(clips: Sequence[ClipEvidence], decisions: Sequence[NarrationDecision]) -> list[NarrationDecision]:
    by_index = {clip.index: clip for clip in clips}
    seen = set()
    ordered = []
    for item in decisions:
        if not isinstance(item, NarrationDecision) or type(item.clip_index) is not int or item.clip_index not in by_index or item.clip_index in seen:
            raise ContentError("narration returned an invalid or repeated clip index")
        clip = by_index[item.clip_index]
        clip_end = clip.timeline_in + clip.source_out - clip.source_in
        if not isinstance(item.text, str):
            raise ContentError("narration text is empty or too long")
        if any(ord(ch) < 32 for ch in item.text):
            raise ContentError("narration text must not contain newline or control characters")
        if not item.text.strip() or not item.text.isprintable() or len(item.text) > 160:
            raise ContentError("narration text is empty or too long")
        if not all(isinstance(v, (int, float)) and math.isfinite(v) for v in (item.start, item.end)):
            raise ContentError("narration timestamps must be finite")
        if not clip.timeline_in <= item.start < item.end <= clip_end:
            raise ContentError("narration window escapes its clip")
        if not isinstance(item.source_scene_label, str) or len(item.source_scene_label) > 100 or (item.source_scene_label and not item.source_scene_label.isprintable()):
            raise ContentError("narration source_scene_label must be a printable string under 100 chars")
        if not isinstance(item.model_version, str) or len(item.model_version) > 100 or (item.model_version and not item.model_version.isprintable()):
            raise ContentError("narration model_version must be a printable string under 100 chars")
        if not isinstance(item.needs_review, bool):
            raise ContentError("narration needs_review must be a boolean")
        seen.add(item.clip_index)
        ordered.append(item)
    if not ordered:
        raise ContentError("provider returned no narration")
    return sorted(ordered, key=lambda item: item.start)
