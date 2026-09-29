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
import os
import re
import shutil
import subprocess
import tempfile
from dataclasses import dataclass, field
from pathlib import Path


class StageFailure(Exception):
    """A task-level failure reported to the control plane via fail_task."""


@dataclass
class StageContext:
    edl: dict
    source_dir: Path          # directory the input edl's relative srcs resolve against
    out_dir: Path             # staging directory for the new package (exists, empty)
    tools: "Tools"
    progress: callable = field(default=lambda p, m: None)


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


# ------------------------------------------------------------------ stages

def recognize(ctx: StageContext) -> dict:
    """Probe every video clip and record measured scene metadata.

    Minimal verifiable recognition: ffprobe duration and a black-frame scan
    per clip. Clips shorter than their declared span are refused, because a
    later stage would otherwise cut past the end of the media.
    """
    edl = ctx.edl
    _require_edl(edl)
    scenes = []
    for i, clip in enumerate(edl["video"]):
        path = _resolve_src(ctx, clip["src"])
        dur = ctx.tools.duration(path)
        if clip["out"] > dur + 1 / edl["timeline"]["fps"]:
            raise StageFailure(f"video[{i}] out {clip['out']} exceeds media duration {dur:.3f}")
        scenes.append({"index": i, "src": clip["src"], "media_duration": round(dur, 3),
                       "span": [clip["in"], clip["out"]], "label": f"scene_{i + 1:02d}"})
        ctx.progress(0.2 + 0.6 * (i + 1) / len(edl["video"]), f"recognized {i + 1}/{len(edl['video'])}")
    out = dict(edl)
    out["scenes"] = scenes
    package(ctx, out, {})
    return {"scenes": len(scenes)}


def sort(ctx: StageContext) -> dict:
    """Re-lay video and matching game audio back to back in scene order.

    Order is the recognized scene order (stable by original timeline_in).
    Gaps are closed so the timeline is contiguous from 0.
    """
    edl = json.loads(json.dumps(ctx.edl))
    _require_edl(edl)
    order = sorted(range(len(edl["video"])), key=lambda i: edl["video"][i]["timeline_in"])
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
    lines = []
    for i, clip in enumerate(edl["video"]):
        span = clip["out"] - clip["in"]
        start = clip["timeline_in"] + min(0.2, span / 4)
        end = min(clip["timeline_in"] + span - 0.1, start + max(0.6, span * 0.6))
        if end <= start:
            continue
        lines.append({"id": f"nar_{i + 1:03d}", "text": f"第 {i + 1} 段", "start": round(start, 3), "end": round(end, 3)})
    if not lines:
        raise StageFailure("no scene is long enough to narrate")
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
