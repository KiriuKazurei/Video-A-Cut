"""Phase 4 content-provider boundary for scene analysis and narration.

The built-in provider preserves the verified phase-3 behavior. Future model
adapters implement the protocol and return bounded, reviewable decisions;
they must be selected explicitly rather than silently replacing the built-in.
"""
from __future__ import annotations

import math
from dataclasses import dataclass
from typing import Protocol, Sequence


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


@dataclass(frozen=True)
class SceneDecision:
    clip_index: int
    label: str
    method: str
    confidence: float | None = None


@dataclass(frozen=True)
class NarrationDecision:
    clip_index: int
    text: str
    start: float
    end: float


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
        for clip in clips:
            span = clip.source_out - clip.source_in
            start = clip.timeline_in + min(0.2, span / 4)
            end = min(clip.timeline_in + span - 0.1, start + max(0.6, span * 0.6))
            if end > start:
                lines.append(NarrationDecision(clip.index, f"第 {clip.index + 1} 段", round(start, 3), round(end, 3)))
        return lines


def make_content_provider(name: str) -> ContentProvider:
    if name == "builtin":
        return BuiltinContentProvider()
    raise ContentError(f"content provider {name!r} is unavailable; configure an installed adapter explicitly")


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
        if not isinstance(item.text, str) or not item.text.strip() or not item.text.isprintable() or len(item.text) > 160:
            raise ContentError("narration text is empty or too long")
        if not all(isinstance(v, (int, float)) and math.isfinite(v) for v in (item.start, item.end)):
            raise ContentError("narration timestamps must be finite")
        if not clip.timeline_in <= item.start < item.end <= clip_end:
            raise ContentError("narration window escapes its clip")
        seen.add(item.clip_index)
        ordered.append(item)
    if not ordered:
        raise ContentError("provider returned no narration")
    return sorted(ordered, key=lambda item: item.start)
