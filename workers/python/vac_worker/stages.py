"""Pipeline stages. Each stage is a pure transform of (input edl, input package
dir) into a new package directory containing edl.json, delivery-manifest.json
and every media file the new edl references.

The implementations are the minimal verifiable versions required by phase 3:
they use local deterministic tools (ffmpeg / ffprobe, Windows SAPI for speech)
instead of multimodal or LLM APIs. Test tones require an explicit opt-in and
can never silently stand in for speech in a production delivery.
"""
from __future__ import annotations

import json
import math
import os
import re
import shutil
import subprocess
import tempfile
from dataclasses import dataclass, field
from pathlib import Path

from .content import (ClipEvidence, ContentError, ContentProvider, SceneDecision,
                      make_content_provider, validate_narration, validate_scenes)
from .sampling import SamplingError, sample_clip_frames


class StageFailure(Exception):
    """A task-level failure reported to the control plane via fail_task."""


@dataclass
class StageContext:
    edl: dict
    source_dir: Path          # directory the input edl's relative srcs resolve against
    out_dir: Path             # staging directory for the new package (exists, empty)
    tools: "Tools"
    progress: callable = field(default=lambda p, m: None)
    content_provider: ContentProvider | None = None
    work_dir: Path | None = None


@dataclass
class Tools:
    ffmpeg: str = "ffmpeg"
    ffprobe: str = "ffprobe"
    powershell: str = "powershell"
    timeout: float = 300.0
    allow_test_tone: bool = False

    def run(self, args: list[str]) -> str:
        try:
            done = subprocess.run(args, capture_output=True, text=True, timeout=self.timeout,
                                  creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        except FileNotFoundError as err:
            raise StageFailure(f"tool not found: {args[0]}") from err
        except subprocess.TimeoutExpired as err:
            raise StageFailure(f"{Path(args[0]).name} timed out after {self.timeout}s") from err
        if done.returncode != 0:
            raise StageFailure(f"{Path(args[0]).name} exited {done.returncode}: {done.stderr.strip()[-400:]}")
        return done.stdout

    def duration(self, path: Path) -> float:
        out = self.run([self.ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "json", str(path)])
        try:
            return float(json.loads(out)["format"]["duration"])
        except (KeyError, ValueError) as err:
            raise StageFailure(f"cannot read duration of {path.name}") from err


# --------------------------------------------------------------- packaging

_SAFE = re.compile(r"[^A-Za-z0-9._-]")


def _resolve_src(ctx: StageContext, src: str) -> Path:
    if os.path.isabs(src) or "\\" in src or src.startswith("/"):
        raise StageFailure(f"edl source must be a relative slash path: {src}")
    path = (ctx.source_dir / src).resolve()
    base = ctx.source_dir.resolve()
    if base not in path.parents or not path.is_file():
        raise StageFailure(f"edl source is missing or escapes its package: {src}")
    return path


def package(ctx: StageContext, edl: dict, generated: dict[str, Path], extra: list[tuple[str, str]] = ()) -> list[dict]:
    """Write edl.json + manifest into ctx.out_dir.

    ``generated`` maps a src value used in ``edl`` to a file already written
    under out_dir (new TTS clips, mixes). Every other src is copied from the
    input package under media/. Returns the manifest artifact list.
    """
    out = json.loads(json.dumps(edl))
    media_dir = ctx.out_dir / "media"
    media_dir.mkdir(exist_ok=True)
    copied: dict[str, str] = {}
    artifacts: list[dict] = [{"kind": "edl", "path": "edl.json"}]
    n = 0
    for track in ("video", "game_audio", "voice", "music"):
        for item in out.get(track, []):
            src = item["src"]
            if src in generated:
                if src not in copied:
                    copied[src] = generated[src].relative_to(ctx.out_dir).as_posix()
                    artifacts.append({"kind": track, "path": copied[src]})
                item["src"] = copied[src]
                continue
            if src not in copied:
                n += 1
                real = _resolve_src(ctx, src)
                name = f"{n:02d}_{_SAFE.sub('_', real.name)}"
                shutil.copy2(real, media_dir / name)
                copied[src] = f"media/{name}"
                artifacts.append({"kind": "video" if track == "video" else track, "path": copied[src]})
            item["src"] = copied[src]
    for scene in out.get("scenes", []):
        if scene.get("src") in copied:
            scene["src"] = copied[scene["src"]]
    for kind, rel in extra:
        artifacts.append({"kind": kind, "path": rel})
    # Kinds must be unique per path only; the control plane numbers repeats.
    (ctx.out_dir / "edl.json").write_text(json.dumps(out, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    manifest = {"schema_version": 1, "source_edl": "edl.json", "artifacts": artifacts}
    (ctx.out_dir / "delivery-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return artifacts


def _require_edl(edl: dict) -> None:
    if not isinstance(edl, dict) or not isinstance(edl.get("timeline"), dict):
        raise StageFailure("edl has no timeline object")
    for track in ("video", "game_audio", "voice", "subtitle", "music"):
        if not isinstance(edl.get(track), list):
            raise StageFailure(f"edl.{track} must be an array")
    if not edl["video"]:
        raise StageFailure("edl.video must contain at least one clip")


def _timeline_end(edl: dict) -> float:
    return max(v["timeline_in"] + (v["out"] - v["in"]) for v in edl["video"])


def _clip_evidence(edl: dict, durations: list[float] | None = None) -> list[ClipEvidence]:
    return [ClipEvidence(index=i, src=clip["src"],
                         media_duration=durations[i] if durations else clip["out"],
                         source_in=clip["in"], source_out=clip["out"],
                         timeline_in=clip["timeline_in"])
            for i, clip in enumerate(edl["video"])]


# ------------------------------------------------------------------ stages

def recognize(ctx: StageContext) -> dict:
    """Probe every video clip and record measured scene metadata.

    Minimal verifiable recognition: ffprobe duration and a black-frame scan
    per clip. Clips shorter than their declared span are refused, because a
    later stage would otherwise cut past the end of the media.
    """
    edl = ctx.edl
    _require_edl(edl)
    durations = []
    clips_evidence: list[ClipEvidence] = []
    for i, clip in enumerate(edl["video"]):
        path = _resolve_src(ctx, clip["src"])
        dur = ctx.tools.duration(path)
        if clip["out"] > dur + 1 / edl["timeline"]["fps"]:
            raise StageFailure(f"video[{i}] out {clip['out']} exceeds media duration {dur:.3f}")
        durations.append(dur)

        frames = ()
        if ctx.work_dir is not None and path.is_file():
            dest_dir = ctx.work_dir / "samples" / f"clip_{i}"
            try:
                sampled = sample_clip_frames(
                    clip_path=path,
                    source_in=clip["in"],
                    source_out=clip["out"],
                    dest_dir=dest_dir,
                    relative_source_path=clip["src"],
                    clip_prefix=f"clip_{i}",
                    ffmpeg_bin=ctx.tools.ffmpeg,
                    ffprobe_bin=ctx.tools.ffprobe,
                )
                frames = tuple(sampled)
            except SamplingError as err:
                raise StageFailure(f"sampling failed: {err}") from err
            except Exception as err:
                raise StageFailure(f"frame sampling failed: {err}") from err

        clips_evidence.append(
            ClipEvidence(
                index=i,
                src=clip["src"],
                media_duration=dur,
                source_in=clip["in"],
                source_out=clip["out"],
                timeline_in=clip["timeline_in"],
                frames=frames,
            )
        )
        ctx.progress(0.2 + 0.6 * (i + 1) / len(edl["video"]), f"recognized {i + 1}/{len(edl['video'])}")

    try:
        decisions = validate_scenes(
            clips_evidence,
            (ctx.content_provider or make_content_provider("builtin")).recognize(clips_evidence),
        )
    except ContentError as err:
        raise StageFailure(str(err)) from err
    except Exception as err:
        raise StageFailure(f"content provider recognize failed ({type(err).__name__})") from err

    scenes = [
        {
            "index": item.clip_index,
            "src": edl["video"][item.clip_index]["src"],
            "media_duration": round(durations[item.clip_index], 3),
            "span": [edl["video"][item.clip_index]["in"], edl["video"][item.clip_index]["out"]],
            "label": item.label,
            "method": item.method,
            "confidence": item.confidence,
            "evidence_frames": list(item.evidence_frames),
            "sequence_rank": item.sequence_rank,
        }
        for item in decisions
    ]
    out = dict(edl)
    out["scenes"] = scenes
    package(ctx, out, {})
    return {"scenes": len(scenes)}


def _resolve_sort_order(edl: dict, min_confidence: float = 0.5) -> list[int]:
    """Determine clip execution order based on sequence_rank and timeline position.

    Rules:
    - If no scenes or all scenes have sequence_rank is None, preserve timeline order.
    - If any scene has sequence_rank, all scenes must have a valid non-negative sequence_rank;
      otherwise raise StageFailure (partial ranking / conflicting signal).
    - If sequence_rank is negative or not an int, raise StageFailure / ContentError.
    - If any scene has confidence < min_confidence, raise StageFailure (low confidence).
    - If duplicate sequence_rank values exist, raise StageFailure (duplicate rank conflict).
    - Otherwise, sort by sequence_rank ascending.
    """
    video_count = len(edl["video"])
    scenes = edl.get("scenes") or []
    if not scenes:
        return sorted(range(video_count), key=lambda i: edl["video"][i]["timeline_in"])

    by_index = {scene["index"]: scene for scene in scenes}
    if set(by_index) != set(range(video_count)):
        raise StageFailure("scene indices differ from video clips")

    ranks = [by_index[i].get("sequence_rank") for i in range(video_count)]
    has_rank = [r is not None for r in ranks]

    if not any(has_rank):
        return sorted(range(video_count), key=lambda i: edl["video"][i]["timeline_in"])

    if not all(has_rank):
        missing = [i for i, r in enumerate(ranks) if r is None]
        raise StageFailure(f"sort conflict: partial sequence_rank missing for clips {missing}")

    for i in range(video_count):
        r = ranks[i]
        if type(r) is not int or r < 0:
            raise ContentError(f"sort conflict: invalid sequence_rank {r} for clip {i}")
        conf = by_index[i].get("confidence")
        if conf is not None:
            if not isinstance(conf, (int, float)) or not math.isfinite(conf) or not 0 <= conf <= 1:
                raise ContentError(f"sort conflict: invalid confidence {conf} for clip {i}")
            if conf < min_confidence:
                raise StageFailure(f"sort conflict: low confidence {conf} for clip {i} (threshold {min_confidence})")

    if len(set(ranks)) != len(ranks):
        seen = set()
        dupes = set()
        for r in ranks:
            if r in seen:
                dupes.add(r)
            seen.add(r)
        raise StageFailure(f"sort conflict: duplicate sequence_rank {sorted(dupes)}")

    return sorted(range(video_count), key=lambda i: ranks[i])


def sort(ctx: StageContext) -> dict:
    """Re-lay video and matching game audio back to back in scene order.

    Order is determined by sequence_rank if provided (reviewable order),
    or stable timeline_in order if unranked.
    Gaps are closed so the timeline is contiguous from 0.
    """
    edl = json.loads(json.dumps(ctx.edl))
    _require_edl(edl)
    order = _resolve_sort_order(edl)
    cursor = 0.0
    video, audio = [], []
    ga_by_key = {(g["src"], g["in"], g["out"]): g for g in edl["game_audio"]}
    for i in order:
        clip = dict(edl["video"][i])
        span = clip["out"] - clip["in"]
        clip["timeline_in"] = round(cursor, 6)
        video.append(clip)
        ga = ga_by_key.get((clip["src"], clip["in"], clip["out"]))
        if ga:
            ga = dict(ga)
            ga["timeline_in"] = clip["timeline_in"]
            audio.append(ga)
        cursor += span
    edl["video"], edl["game_audio"] = video, audio
    if edl.get("scenes"):
        by_index = {scene["index"]: scene for scene in edl["scenes"]}
        if set(by_index) != set(order):
            raise StageFailure("scene indices differ from video clips")
        edl["scenes"] = [{**by_index[old_index], "index": new_index}
                         for new_index, old_index in enumerate(order)]
    package(ctx, edl, {})
    return {"clips": len(video), "duration": round(cursor, 6)}


def narrate(ctx: StageContext) -> dict:
    """Write one narration line per scene into the edl (text only).

    A deterministic template stands in for the LLM script writer; the output
    contract (``narration`` entries with id/text/start/end inside the
    timeline) is what tts and subtitle consume.
    """
    edl = json.loads(json.dumps(ctx.edl))
    _require_edl(edl)
    clips = _clip_evidence(edl)
    scenes = [
        SceneDecision(
            clip_index=scene["index"],
            label=scene["label"],
            method=scene.get("method", "metadata_only"),
            confidence=scene.get("confidence"),
            evidence_frames=tuple(scene.get("evidence_frames") or ()),
            sequence_rank=scene.get("sequence_rank"),
        )
        for scene in edl.get("scenes", [])
    ]
    try:
        scenes = validate_scenes(clips, scenes)
        decisions = validate_narration(clips, (ctx.content_provider or make_content_provider("builtin")).narrate(clips, scenes))
    except ContentError as err:
        raise StageFailure(str(err)) from err
    except Exception as err:
        raise StageFailure(f"content provider narrate failed ({type(err).__name__})") from err
    lines = []
    for i, item in enumerate(decisions):
        lines.append({
            "id": f"nar_{i + 1:03d}",
            "text": item.text,
            "start": round(item.start, 6),
            "end": round(item.end, 6),
            "clip_index": item.clip_index,
            "duration": round(item.duration, 6),
            "source_scene": item.source_scene_label,
            "source_scene_label": item.source_scene_label,
            "model_version": item.model_version,
            "needs_review": item.needs_review,
        })
    edl["narration"] = lines
    package(ctx, edl, {})
    return {"lines": len(lines)}


def _sapi_script(text: str, out: Path) -> str:
    # Text and path are passed as base64 to avoid any quoting into the script.
    import base64
    t = base64.b64encode(text.encode("utf-8")).decode()
    p = base64.b64encode(str(out).encode("utf-8")).decode()
    return (
        "$ErrorActionPreference='Stop';"
        f"$t=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{t}'));"
        f"$p=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{p}'));"
        "$s=New-Object -ComObject SAPI.SpVoice;"
        "$voices=$s.GetVoices('Language=804','');"
        "if($voices.Count -lt 1){throw 'Chinese SAPI voice unavailable'};"
        "$s.Voice=$voices.Item(0);"
        "$s.Rate=5;"
        "$stream=New-Object -ComObject SAPI.SpFileStream;"
        "try{$stream.Open($p,3,$false);$s.AudioOutputStream=$stream;[void]$s.Speak($t)}"
        "finally{$stream.Close()}"
    )


def tts(ctx: StageContext) -> dict:
    """Synthesize speech and write its measured duration back into the EDL.

    Narration windows are upper bounds. Overlong speech fails instead of being
    truncated; test tones require explicit opt-in.
    """
    edl = json.loads(json.dumps(ctx.edl))
    _require_edl(edl)
    lines = edl.get("narration") or []
    if not lines:
        raise StageFailure("edl has no narration; run narrate first")
    rate = edl["timeline"]["sample_rate"]
    voice_dir = ctx.out_dir / "voice"
    voice_dir.mkdir()
    generated: dict[str, Path] = {}
    voice = []
    engine = "sapi"
    with tempfile.TemporaryDirectory() as tmp:
        for i, line in enumerate(lines):
            span = line["end"] - line["start"]
            raw = Path(tmp) / f"{line['id']}.wav"
            try:
                if os.name != "nt":
                    raise StageFailure("sapi unavailable")
                ctx.tools.run([ctx.tools.powershell, "-NoProfile", "-NonInteractive", "-Command", _sapi_script(line["text"], raw)])
                src_args = ["-i", str(raw)]
                duration = ctx.tools.duration(raw)
            except StageFailure:
                if not ctx.tools.allow_test_tone:
                    raise
                engine = "tone"
                src_args = ["-f", "lavfi", "-i", f"sine=frequency=520:sample_rate={rate}:duration={span}"]
                duration = span
            samples = round(duration * rate)
            window_samples = round(span * rate)
            if samples <= 0 or samples > window_samples:
                raise StageFailure(f"narration {line['id']} speech duration {duration:.3f}s exceeds window {span:.3f}s")
            final = voice_dir / f"{line['id']}.wav"
            ctx.tools.run([ctx.tools.ffmpeg, "-hide_banner", "-loglevel", "error", "-y", *src_args,
                           "-af", f"aresample={rate},apad,atrim=end_sample={samples}",
                           "-ar", str(rate), "-ac", "2", "-c:a", "pcm_s16le", str(final)])
            key = f"__gen_voice_{i}"
            generated[key] = final
            voice.append({"id": line["id"].replace("nar", "vo"), "src": key, "start": line["start"],
                          "end": round(line["start"] + samples / rate, 6), "gain_db": 0})
            ctx.progress(0.2 + 0.6 * (i + 1) / len(lines), f"tts {i + 1}/{len(lines)}")
    edl["voice"] = voice
    package(ctx, edl, generated)
    return {"clips": len(voice), "engine": engine}


def subtitle(ctx: StageContext) -> dict:
    """Derive subtitle timing from measured TTS clips, not draft windows."""
    edl = json.loads(json.dumps(ctx.edl))
    _require_edl(edl)
    lines = edl.get("narration") or []
    if not lines:
        raise StageFailure("edl has no narration; run narrate first")
    voice = edl.get("voice") or []
    if len(voice) != len(lines):
        raise StageFailure("voice and narration count differ; run tts first")
    end = _timeline_end(edl)
    subtitles = []
    for line, clip in zip(lines, voice):
        if clip.get("id") != line["id"].replace("nar", "vo") or clip.get("start") != line["start"]:
            raise StageFailure("voice and narration do not align")
        if clip["end"] > end + 1 / edl["timeline"]["fps"]:
            raise StageFailure("voice extends past timeline")
        subtitles.append({"id": line["id"].replace("nar", "sub"), "text": line["text"],
                          "start": clip["start"], "end": min(clip["end"], end)})
    edl["subtitle"] = subtitles
    package(ctx, edl, {})
    return {"subtitles": len(edl["subtitle"])}


def mix(ctx: StageContext) -> dict:
    """Prepare the music bed and turn on ducking under narration.

    A bed covering the whole timeline is rendered (from existing music when
    present, otherwise a quiet generated pad) at the project rate so the
    export sync check sees an exact span; ``duck`` is enabled whenever voice
    exists. The actual gain automation is rendered by the export stage.
    """
    edl = json.loads(json.dumps(ctx.edl))
    _require_edl(edl)
    rate = edl["timeline"]["sample_rate"]
    total = _timeline_end(edl)
    (ctx.out_dir / "mix").mkdir()
    bed = ctx.out_dir / "mix" / "music_bed.wav"
    if edl["music"]:
        src = _resolve_src(ctx, edl["music"][0]["src"])
        inputs = ["-stream_loop", "-1", "-i", str(src)]
    else:
        inputs = ["-f", "lavfi", "-i", f"sine=frequency=196:sample_rate={rate}:duration={total}"]
    ctx.tools.run([ctx.tools.ffmpeg, "-hide_banner", "-loglevel", "error", "-y", *inputs,
                   "-af", f"aresample={rate},apad,atrim=end_sample={round(total * rate)}",
                   "-ar", str(rate), "-ac", "2", "-c:a", "pcm_s16le", str(bed)])
    gain = edl["music"][0].get("gain_db", -20) if edl["music"] else -24
    edl["music"] = [{"src": "__gen_bed", "start": 0, "end": round(total, 6), "gain_db": gain, "duck": bool(edl["voice"])}]
    package(ctx, edl, {"__gen_bed": bed})
    return {"bed_seconds": round(total, 3), "duck": bool(edl["voice"])}


STAGES = {
    "recognize": recognize,
    "sort": sort,
    "narrate": narrate,
    "tts": tts,
    "subtitle": subtitle,
    "mix": mix,
}
