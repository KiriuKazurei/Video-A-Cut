"""Phase 4 content-provider boundary for scene analysis and narration.

The built-in provider preserves the verified phase-3 behavior. Future model
adapters implement the protocol and return bounded, reviewable decisions;
they must be selected explicitly rather than silently replacing the built-in.
"""
from __future__ import annotations

import base64
import hashlib
import json
import math
import os
import re
import socket
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol, Sequence


MAX_HTTP_RESPONSE_BYTES = 10 * 1024 * 1024  # 10 MB limit
MAX_FRAME_BYTES = 10 * 1024 * 1024
MAX_REQUEST_IMAGE_BYTES = 32 * 1024 * 1024


def narration_draft_hash(text: str, start: float, end: float, source: str) -> str:
    """Fingerprint a narration draft so a later edit cannot reuse an old approval.

    The canonical bytes are UTF-8 of ``text``, start, end and source, separated
    by newlines. Times use six decimal places. The same spelling is implemented
    by the control-plane service that records the human approval.
    """
    if not isinstance(text, str) or not isinstance(source, str):
        raise ContentError("narration draft hash requires text and source strings")
    if isinstance(start, bool) or isinstance(end, bool) or not all(isinstance(v, (int, float)) for v in (start, end)):
        raise ContentError("narration draft timestamps must be numbers")
    if not math.isfinite(float(start)) or not math.isfinite(float(end)):
        raise ContentError("narration draft timestamps must be finite")
    canonical = f"{text}\n{float(start):.6f}\n{float(end):.6f}\n{source}"
    return hashlib.sha256(canonical.encode("utf-8")).hexdigest()


def resolve_evidence_file(root: str, relative_path: str) -> Path:
    """Resolve one sampled frame strictly inside the worker-controlled root."""
    if not isinstance(root, str) or not root.strip():
        raise ContentError("sampled frame has no controlled evidence root")
    if not isinstance(relative_path, str) or not relative_path.strip():
        raise ContentError("sampled frame path is missing")
    raw = relative_path.replace("\\", "/")
    if raw.startswith("/") or ":" in raw or raw.startswith("../") or "/../" in f"/{raw}/" or raw.endswith("/.."):
        raise ContentError("frame path escapes the evidence root")
    parts = [part for part in raw.split("/") if part not in ("", ".")]
    if not parts or any(part == ".." for part in parts):
        raise ContentError("frame path escapes the evidence root")
    root_path = Path(root).resolve()
    if not root_path.is_dir():
        raise ContentError("evidence root is not a directory")
    candidate = root_path.joinpath(*parts).resolve()
    if candidate != root_path and root_path not in candidate.parents:
        raise ContentError("frame path escapes the evidence root")
    return candidate


def load_frame_image(clip: ClipEvidence, frame: Any) -> dict[str, Any]:
    """Read one sampled frame, check its hash, and return a payload with image bytes.

    The absolute path stays on the worker. The outbound object carries a
    relative name, the measured metadata, and base64 image bytes.
    """
    if hasattr(frame, "to_dict"):
        meta = frame.to_dict()
    elif isinstance(frame, dict):
        meta = dict(frame)
    else:
        raise ContentError("sampled frame is missing metadata")
    relative = meta.get("path")
    if isinstance(relative, Path):
        relative = relative.name
    if not isinstance(relative, str):
        raise ContentError("sampled frame path is missing")
    expected = meta.get("sha256")
    if not isinstance(expected, str) or len(expected) != 64 or any(ch not in "0123456789abcdef" for ch in expected):
        raise ContentError("sampled frame is missing a sha256 digest")
    frame_file = resolve_evidence_file(clip.evidence_root, relative)
    if not frame_file.is_file():
        raise ContentError(f"sampled frame file is missing: {Path(relative).name}")
    size = frame_file.stat().st_size
    if size <= 0 or size > MAX_FRAME_BYTES:
        raise ContentError("sampled frame size is outside the allowed limit")
    raw = frame_file.read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    if digest != expected:
        raise ContentError("sampled frame hash does not match the evidence record")
    payload = {
        "frame_index": meta.get("frame_index", meta.get("index")),
        "timestamp": meta.get("timestamp", meta.get("pts")),
        "width": meta.get("width"),
        "height": meta.get("height"),
        "sha256": digest,
        "path": "/".join(part for part in relative.replace("\\", "/").split("/") if part not in ("", ".")),
        "image_base64": base64.b64encode(raw).decode("ascii"),
    }
    return payload


def _read_limited_response(resp: Any, max_bytes: int = MAX_HTTP_RESPONSE_BYTES) -> bytes:
    if not hasattr(resp, "read"):
        return b""
    try:
        data = resp.read(max_bytes + 1)
    except TypeError:
        # If mock object or custom stream does not accept size argument
        data = resp.read()

    if isinstance(data, (bytes, bytearray)):
        if len(data) > max_bytes:
            raise ContentError(f"external service response exceeded maximum size limit ({max_bytes} bytes)")
        return bytes(data)
    elif isinstance(data, str):
        encoded = data.encode("utf-8")
        if len(encoded) > max_bytes:
            raise ContentError(f"external service response exceeded maximum size limit ({max_bytes} bytes)")
        return encoded
    return b""


def _redact_sensitive_text(text: str, token: str | None = None) -> str:
    if not text:
        return text
    res = text
    if token and token in res:
        res = res.replace(token, "[REDACTED]")
    res = re.sub(r'://([^:]+):([^@]+)@', r'://\1:[REDACTED]@', res)
    res = re.sub(r'(Bearer\s+)[A-Za-z0-9_\-\.]+', r'\1[REDACTED]', res, flags=re.IGNORECASE)
    res = re.sub(r'((?:token|api_key|key|secret|password)=)[^&\s]+', r'\1[REDACTED]', res, flags=re.IGNORECASE)
    return res


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
    # Directory the worker owns. The vision adapter may read frames only here.
    evidence_root: str = ""


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
        if not isinstance(allow_external, bool):
            raise TypeError(f"allow_external must be a bool, got {type(allow_external).__name__}")
        self.endpoint = endpoint
        self.token_env = token_env or "VAC_VISION_TOKEN"
        self.allow_external = allow_external
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
        image_bytes = 0
        for c in clips:
            if not c.frames:
                raise ContentError(f"clip {c.index} has no sampled frames for visual recognition")
            frames_data = []
            for f in c.frames:
                clean_fd = load_frame_image(c, f)
                if "image_base64" not in clean_fd:
                    raise ContentError(f"clip {c.index} frame has no image bytes")
                image_bytes += len(base64.b64decode(clean_fd["image_base64"]))
                if image_bytes > MAX_REQUEST_IMAGE_BYTES:
                    raise ContentError("visual request exceeds the image byte budget")
                frames_data.append(clean_fd)
            src = c.src if isinstance(c.src, str) and not os.path.isabs(c.src) and "\\" not in c.src and ":" not in c.src else ""
            clip_payloads.append({
                "clip_index": c.index,
                "src": src,
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
            return _redact_sensitive_text(text, token)

        try:
            with self._opener(req, timeout=self.timeout_seconds) as resp:
                raw_bytes = _read_limited_response(resp)
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
        if not isinstance(allow_external, bool):
            raise TypeError(f"allow_external must be a bool, got {type(allow_external).__name__}")
        self.endpoint = endpoint
        self.token_env = token_env or "VAC_NARRATION_TOKEN"
        self.allow_external = allow_external
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

        def _redact(text: str) -> str:
            return _redact_sensitive_text(text, token)

        try:
            resp = self._opener(req, timeout=self.timeout_seconds)
            try:
                status = getattr(resp, "status", None) or getattr(resp, "code", 200)
                if status >= 400:
                    raise ContentError(f"external narration service returned HTTP {status}")
                raw_data = _read_limited_response(resp)
            finally:
                if hasattr(resp, "close"):
                    resp.close()
        except ContentError:
            raise
        except urllib.error.HTTPError as err:
            reason = _redact(str(err.reason) if getattr(err, "reason", None) else str(err.code))
            raise ContentError(f"external narration service HTTP error: {err.code} {reason}") from None
        except (TimeoutError, socket.timeout) as err:
            raise ContentError(f"external narration service timed out: {_redact(str(err))}") from None
        except urllib.error.URLError as err:
            err_reason = _redact(str(err.reason))
            if isinstance(err.reason, (TimeoutError, socket.timeout)) or "timed out" in err_reason.lower():
                raise ContentError(f"external narration service timed out: {err_reason}") from None
            raise ContentError(f"external narration service network error: {err_reason}") from None
        except OSError as err:
            err_str = _redact(str(err))
            if "timed out" in err_str.lower():
                raise ContentError(f"external narration service timed out: {err_str}") from None
            raise ContentError(f"external narration service network error: {err_str}") from None
        except Exception as err:
            raise ContentError(f"external narration service request failed: {_redact(str(err))}") from None

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
            # The model cannot grant itself an exemption. Review is a human
            # or operator-config decision applied later by the TTS gate.
            needs_review = True
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
    api_format = kwargs.pop('api_format', '')
    if api_format:
        from .provider_http import StandardGateway
        if name not in ('vision', 'narration'):
            raise ContentError('standard API formats require vision or narration adapter')
        kwargs['opener'] = StandardGateway(api_format, kwargs.get('endpoint', ''), name,
                                           kwargs.pop('opener', None)).open
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
        clip = next((c for c in clips if c.index == item.clip_index), None)
        valid_clip_frame_keys = set()
        if clip and getattr(clip, "frames", None):
            for cf in clip.frames:
                if hasattr(cf, "sha256"):
                    valid_clip_frame_keys.add(cf.sha256)
                if hasattr(cf, "path"):
                    valid_clip_frame_keys.add(cf.path)
                if hasattr(cf, "frame_index"):
                    valid_clip_frame_keys.add(str(cf.frame_index))
                if isinstance(cf, dict):
                    if "sha256" in cf: valid_clip_frame_keys.add(cf["sha256"])
                    if "path" in cf: valid_clip_frame_keys.add(cf["path"])
                    if "frame_index" in cf: valid_clip_frame_keys.add(str(cf["frame_index"]))
        for f in item.evidence_frames:
            if not isinstance(f, str) or not f.strip() or not f.isprintable() or len(f) > 256:
                raise ContentError("recognition evidence_frame must be a non-empty printable string under 256 chars")
            if valid_clip_frame_keys and f not in valid_clip_frame_keys:
                raise ContentError(f"recognition evidence_frame {f!r} does not belong to sampled frame evidence for clip {item.clip_index}")
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
