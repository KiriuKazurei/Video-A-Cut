"""Measure a Premiere XML delivery against its EDL and actual WAV samples."""
from __future__ import annotations

import argparse
import array
import json
import math
import re
import subprocess
import sys
import wave
import xml.etree.ElementTree as ET
from pathlib import Path
from urllib.parse import unquote, urlparse


def require(ok: bool, detail: str) -> None:
    if not ok:
        raise ValueError(detail)


def left_samples(path: Path, rate: int) -> array.array:
    with wave.open(str(path), "rb") as w:
        require((w.getframerate(), w.getnchannels(), w.getsampwidth()) == (rate, 2, 2),
                f"{path.name}: expected {rate} Hz stereo PCM16")
        frames = w.readframes(w.getnframes())
    samples = array.array("h")
    samples.frombytes(frames)
    if sys.byteorder != "little":
        samples.byteswap()
    return samples[::2]


def measured_gain(source: array.array, output: array.array, rate: int, start: float, end: float) -> float:
    points = range(round(start * rate), round(end * rate), 64)
    numerator = sum(source[i] * output[i] for i in points)
    denominator = sum(source[i] * source[i] for i in points)
    require(denominator > 100_000, "music reference is silent in a measurement window")
    return numerator / denominator


def voice_offset(source: array.array, output: array.array, rate: int, expected_start: float,
                 fps: int) -> tuple[float, float]:
    # Locate a loud quarter-second of the original spoken clip. Correlating
    # against that clip accounts for any leading silence in the SAPI voice.
    probe_len = min(round(0.25 * rate), len(source) // 2)
    candidates = range(0, max(1, len(source) - probe_len), round(0.1 * rate))
    probe_start = max(candidates, key=lambda start: sum(abs(source[i]) for i in
                      range(start, start + probe_len, 64)))
    probe = list(range(probe_start, probe_start + probe_len, 8))
    expected = round(expected_start * rate)
    search = round(2 * rate / fps)
    best_shift, best_score = 0, float("-inf")
    src_power = sum(source[i] ** 2 for i in probe)
    require(src_power > 100_000, "voice reference is silent")
    for shift in range(-search, search + 1, 8):
        positions = [expected + shift + i for i in probe]
        if positions[0] < 0 or positions[-1] >= len(output):
            continue
        dot = sum(source[i] * output[j] for i, j in zip(probe, positions))
        out_power = sum(output[j] ** 2 for j in positions)
        if out_power:
            score = dot / math.sqrt(src_power * out_power)
            if score > best_score:
                best_shift, best_score = shift, score
    return round(best_shift * 1000 / rate, 3), round(best_score, 4)


def probe(path: Path) -> dict:
    result = subprocess.run(["ffprobe", "-v", "error", "-show_entries",
                             "stream=codec_type,start_time,duration,avg_frame_rate,sample_rate",
                             "-of", "json", str(path)], capture_output=True, text=True, check=True)
    return json.loads(result.stdout)


def srt_seconds(value: str) -> float:
    h, m, rest = value.split(":")
    sec, ms = rest.split(",")
    return int(h) * 3600 + int(m) * 60 + int(sec) + int(ms) / 1000


def verify(delivery: Path) -> dict:
    edl = json.loads((delivery / "edl.json").read_text(encoding="utf-8"))
    fps = edl["timeline"]["fps"]
    rate = edl["timeline"]["sample_rate"]
    total = max(v["timeline_in"] + v["out"] - v["in"] for v in edl["video"])
    total_frames, total_samples = round(total * fps), round(total * rate)
    require(fps == 60 and rate == 48000, "acceptance sample must be 60 fps / 48 kHz")
    stems = {name: left_samples(delivery / f"audio_{name}.wav", rate) for name in ("a1", "a2", "a3")}
    for name, data in stems.items():
        require(len(data) == total_samples, f"{name}: wrong sample count {len(data)}")

    xml = ET.parse(delivery / "edit.xml").getroot()
    sequence = xml.find("sequence")
    require(sequence is not None and int(sequence.findtext("duration")) == total_frames,
            "XML sequence duration differs from EDL")
    video_items = sequence.findall("./media/video/track/clipitem")
    require(len(video_items) == len(edl["video"]), "XML video clip count differs from EDL")
    require(len(sequence.findall("./media/audio/track")) == 3, "XML must contain three audio stems")
    video_probes = []
    for clip, item in zip(edl["video"], video_items):
        start, end = int(item.findtext("start")), int(item.findtext("end"))
        require((start, end) == (round(clip["timeline_in"] * fps),
                                 round((clip["timeline_in"] + clip["out"] - clip["in"]) * fps)),
                "XML cut frames differ from EDL")
        media = (delivery / clip["src"]).resolve()
        require(delivery.resolve() in media.parents and media.is_file(), "video source escapes package")
        info = probe(media)["streams"]
        video = next(s for s in info if s["codec_type"] == "video")
        audio = next(s for s in info if s["codec_type"] == "audio")
        require(video["avg_frame_rate"] == "60/1", "source clip frame rate is not 60")
        require(abs(float(video["start_time"]) - float(audio["start_time"])) <= 1 / fps,
                "source container audio/video starts more than one frame apart")
        require(abs(float(video["duration"]) - float(audio["duration"])) <= 1 / fps,
                "source container audio/video durations differ by more than one frame")
        pathurl = item.findtext("./file/pathurl")
        require(pathurl is not None, "XML clip has no file URL")
        xml_path = Path(unquote(urlparse(pathurl).path).lstrip("/"))
        require(xml_path.resolve() == media, "XML file URL differs from delivered media path")
        video_probes.append({"timeline_frames": [start, end], "source_fps": video["avg_frame_rate"],
                             "audio_video_start_delta_ms": round(1000 * abs(float(video["start_time"]) - float(audio["start_time"])), 3)})

    srt = (delivery / "subtitles.srt").read_text(encoding="utf-8")
    windows = re.findall(r"(\d\d:\d\d:\d\d,\d{3}) --> (\d\d:\d\d:\d\d,\d{3})", srt)
    require(len(windows) == len(edl["subtitle"]), "SRT cue count differs from EDL")
    for (start, end), subtitle in zip(windows, edl["subtitle"]):
        require(abs(srt_seconds(start) - subtitle["start"]) <= 0.002 and
                abs(srt_seconds(end) - subtitle["end"]) <= 0.002,
                "SRT timing differs from EDL by more than 2 ms")

    offsets = []
    require(len(edl["voice"]) == len(edl["subtitle"]), "voice/subtitle count differs")
    for voice, subtitle in zip(edl["voice"], edl["subtitle"]):
        require(abs(voice["start"] - subtitle["start"]) <= 0.002 and
                abs(voice["end"] - subtitle["end"]) <= 0.002,
                "subtitle does not follow measured voice duration")
        source = left_samples(delivery / voice["src"], rate)
        require(len(source) == round((voice["end"] - voice["start"]) * rate),
                "voice source duration differs from EDL")
        ms, corr = voice_offset(source, stems["a2"], rate, voice["start"], fps)
        require(abs(ms) <= 1000 / fps and corr >= 0.7,
                f"voice {voice['id']} is not aligned with its EDL window: {ms} ms / {corr}")
        offsets.append({"id": voice["id"], "offset_ms": ms, "correlation": corr})

    require(len(edl["music"]) == 1 and edl["music"][0]["duck"], "expected one ducked music bed")
    music = left_samples(delivery / edl["music"][0]["src"], rate)
    baseline = measured_gain(music, stems["a3"], rate, 3.0, 3.6)
    ducked = [measured_gain(music, stems["a3"], rate, start, start + 0.6)
              for start in (0.8, 4.8)]
    ratios = [v / baseline for v in ducked]
    require(0.07 <= baseline <= 0.13, f"music baseline gain differs from -20 dB: {baseline}")
    require(all(0.20 <= ratio <= 0.30 for ratio in ratios),
            f"ducked music gain is not approximately -12 dB relative to baseline: {ratios}")
    return {"result": "passed", "fps": fps, "sample_rate": rate,
            "timeline_frames": total_frames, "stem_samples_each": total_samples,
            "video": video_probes, "voice": offsets,
            "music_baseline_linear_gain": round(baseline, 4),
            "music_ducked_relative_gain": [round(r, 4) for r in ratios],
            "subtitle_cues": len(windows), "premiere_xml": str(delivery / "edit.xml")}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--delivery", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args()
    result = verify(args.delivery.resolve(strict=True))
    args.report.write_text(json.dumps(result, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps(result, indent=2, ensure_ascii=False))


if __name__ == "__main__":
    main()
