"""Build an isolated Premiere handoff package from a real local recording.

The source is read only. Output must be a new directory. This is a measured
technical acceptance sample, not a claim that scene labels or narration text
have been editorially approved.
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from vac_worker.stages import STAGES, StageContext, Tools  # noqa: E402


def run(*args: str, timeout: int = 300) -> str:
    result = subprocess.run(args, capture_output=True, text=True, timeout=timeout,
                            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    if result.returncode:
        raise RuntimeError(f"{Path(args[0]).name} failed: {result.stderr[-800:]}")
    return result.stdout


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--start-a", type=float, default=10)
    parser.add_argument("--start-b", type=float, default=50)
    args = parser.parse_args()
    source, output = args.source.resolve(strict=True), args.output.resolve()
    if not source.is_file() or output.exists() or output == source.parent:
        raise ValueError("source must be a file and output must be a new directory")
    source_probe = json.loads(run("ffprobe", "-v", "error", "-show_entries",
                                  "format=duration:stream=index,codec_type,avg_frame_rate,sample_rate",
                                  "-of", "json", str(source)))
    duration = float(source_probe["format"]["duration"])
    if max(args.start_a, args.start_b) + 4 > duration:
        raise ValueError("the recording is too short for the selected sample windows")
    if not any(s["codec_type"] == "audio" for s in source_probe["streams"]):
        raise ValueError("the recording has no audio stream")
    output.mkdir(parents=True)
    raw = output / "source"
    raw.mkdir()
    for index, start in enumerate((args.start_a, args.start_b), 1):
        run("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-ss", str(start),
            "-i", str(source), "-t", "4", "-map", "0:v:0", "-map", "0:a:0",
            "-vf", "scale=1280:-2,fps=60", "-c:v", "libx264", "-preset", "veryfast",
            "-crf", "22", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ar", "48000",
            "-ac", "2", "-movflags", "+faststart", str(raw / f"clip_{index:02d}.mp4"))
    run("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi",
        "-i", "sine=frequency=196:sample_rate=48000:duration=8", "-ar", "48000",
        "-ac", "2", "-c:a", "pcm_s16le", str(raw / "music.wav"))
    edl = {
        "timeline": {"fps": 60, "sample_rate": 48000},
        "video": [{"src": f"clip_{i:02d}.mp4", "in": 0, "out": 4,
                   "timeline_in": 4 * (i - 1)} for i in (1, 2)],
        "game_audio": [{"src": f"clip_{i:02d}.mp4", "in": 0, "out": 4,
                        "timeline_in": 4 * (i - 1), "gain_db": -12} for i in (1, 2)],
        "voice": [], "subtitle": [],
        "music": [{"src": "music.wav", "start": 0, "end": 8,
                   "gain_db": -20, "duck": True}],
    }
    (raw / "edl.json").write_text(json.dumps(edl, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    current_dir, current_edl = raw, edl
    results = {}
    for index, stage in enumerate(("recognize", "sort", "narrate", "tts", "subtitle", "mix"), 1):
        target = output / f"{index:02d}_{stage}"
        target.mkdir()
        results[stage] = STAGES[stage](StageContext(edl=current_edl, source_dir=current_dir,
                                                   out_dir=target, tools=Tools(timeout=300)))
        current_dir = target
        current_edl = json.loads((target / "edl.json").read_text(encoding="utf-8"))
    if results["tts"].get("engine") != "sapi":
        raise RuntimeError("acceptance requires actual SAPI narration, not a test tone")
    cli_root = Path(__file__).resolve().parents[2] / "node" / "timeline-cli"
    delivery = output / "premiere-delivery"
    export_args = ("node", str(cli_root / "src" / "cli.mjs"), "build", "--edl", str(current_dir / "edl.json"),
                   "--output", str(delivery), "--adapters", str(cli_root / "adapters" / "index.mjs"))
    for attempt in range(2):
        try:
            run(*export_args, timeout=600)
            break
        except RuntimeError as error:
            # Windows scanners can briefly hold the staging directory open at rename.
            if attempt or "EPERM" not in str(error) or delivery.exists():
                raise
            time.sleep(1)
    report = {"source": str(source), "source_duration": duration,
              "source_windows": [args.start_a, args.start_b], "timeline_seconds": 8,
              "timeline_fps": 60, "sample_rate": 48000, "stages": results,
              "delivery": str(delivery), "editor_target": "Premiere"}
    (output / "build-report.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
