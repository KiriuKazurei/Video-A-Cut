"""media_prepare: transcode only the selected ranges into CFR clips with the
chosen game audio, measure them and write the initial EDL package."""
from __future__ import annotations

import time
from fractions import Fraction
from pathlib import Path

from . import segment as seg_mod
from .common import Exec, IngestFailure, dump_json, link_or_copy, sha256_file, us_to_s
from .segment import origin_us, snapshot_path, stream

PRE_ROLL_US = 1_000_000
SEEK_MARGIN_US = 3_000_000


def output_frames(start: int, end: int, fps: int) -> int:
    return ((end - start) * fps + 500_000) // 1_000_000


def output_samples(frames: int, fps: int, rate: int) -> int:
    return frames * rate // fps


def _tb(text: str) -> Fraction:
    try:
        num, den = text.split("/")
        tb = Fraction(int(num), int(den))
        return tb if tb > 0 else Fraction(1, 1000)
    except (ValueError, ZeroDivisionError):
        return Fraction(1, 1000)


def _pts(us: int, tb: Fraction) -> int:
    return round(Fraction(us, 1_000_000) / tb)


def _source_frames(ex: Exec, snap: Path, vidx: int, tb: Fraction, abs_a: int, abs_b: int) -> tuple[int, int | None]:
    """Count source frames presented in [abs_a, abs_b) and the first frame time at or after abs_a."""
    lo = max(0, abs_a - 2_000_000)
    out, _ = ex.runner.run([ex.ffprobe, "-v", "error", "-select_streams", str(vidx), "-show_entries", "packet=pts",
                            "-read_intervals", f"{us_to_s(lo)}%{us_to_s(abs_b + 1_000_000)}", "-of", "csv=p=0", str(snap)], timeout=300)
    count, first = 0, None
    for tok in out.decode("ascii", "replace").split():
        tok = tok.strip().rstrip(",")
        if not tok.lstrip("-").isdigit():
            continue
        t = round(int(tok) * tb * 1_000_000)
        if abs_a <= t < abs_b:
            count += 1
            first = t if first is None or t < first else first
    return count, first


def _measure(ex: Exec, media: Path, has_audio: bool) -> tuple[int, int, int]:
    """(decoded video frames, presented audio samples, decoded audio samples)."""
    out, _ = ex.runner.run([ex.ffprobe, "-v", "error", "-count_frames", "-show_entries",
                            "stream=codec_type,nb_read_frames,duration_ts,time_base,sample_rate", "-of", "json", str(media)], timeout=300)
    import json
    streams = json.loads(out).get("streams") or []
    frames = next((int(s.get("nb_read_frames") or 0) for s in streams if s.get("codec_type") == "video"), 0)
    audio = [s for s in streams if s.get("codec_type") == "audio"]
    if not has_audio:
        if audio:
            raise IngestFailure("a clip contains audio although none was selected")
        return frames, 0, 0
    if len(audio) != 1:
        raise IngestFailure("a clip is missing its game audio stream")
    a = audio[0]
    presented = round(int(a.get("duration_ts") or 0) * _tb(a.get("time_base", "1/48000")) * 48000)
    count = [0]

    def sink(chunk: bytes) -> None:
        count[0] += len(chunk)

    ex.runner.run([ex.ffmpeg, "-v", "error", "-i", str(media), "-map", "0:a:0", "-f", "s16le", "-ac", "1", "-ar", "48000", "-"],
                  timeout=300, stdout_sink=sink)
    return frames, presented, count[0] // 2


def _encode(ex: Exec, snap: Path, plan: dict, sel_out: dict, a_us: int, b_us: int, origin: int, frames: int, samples: int,
            dest: Path, timeout: float) -> None:
    fps, rate = sel_out["fps"], sel_out["sample_rate"]
    vidx, aidx = plan["video_stream_index"], plan["game_audio_stream_index"]
    abs_a, abs_b = origin + a_us, origin + b_us
    graph = (f"[0:{vidx}]trim=start={us_to_s(max(0, abs_a - PRE_ROLL_US))}:end={us_to_s(abs_b + PRE_ROLL_US)},"
             f"setpts=PTS-{us_to_s(abs_a)}/TB,tpad=stop_mode=clone:stop_duration=2,"
             f"fps={fps}:start_time=0:round=near,format=yuv420p[v]")
    maps = ["-map", "[v]"]
    if aidx is not None:
        graph += (f";[0:{aidx}]atrim=start={us_to_s(abs_a)}:end={us_to_s(abs_b)},asetpts=PTS-{us_to_s(abs_a)}/TB,"
                  f"aresample={rate}:async=1:first_pts=0,apad,atrim=end_sample={samples}[a]")
        maps += ["-map", "[a]", "-c:a", "aac", "-b:a", "192k", "-ar", str(rate)]
    tmp = dest.with_name(dest.stem + ".tmp.mp4")
    tmp.unlink(missing_ok=True)
    ex.runner.run([ex.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-y", "-copyts",
                   "-ss", us_to_s(max(0, a_us - SEEK_MARGIN_US)), "-i", str(snap), "-filter_complex", graph, *maps,
                   "-frames:v", str(frames), "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p",
                   "-g", str(fps * 2), "-map_metadata", "-1", "-map_chapters", "-1", "-avoid_negative_ts", "disabled",
                   "-movflags", "+faststart", str(tmp)], timeout=timeout)
    tmp.replace(dest)


def media_prepare(ex: Exec) -> dict:
    sel, plan, probe = ex.inp["selection"], ex.inp["analysis"], ex.inp["probe"]
    quotas = ex.inp["frame_quotas"]
    snap = snapshot_path(ex)
    fps, rate = sel["output"]["fps"], sel["output"]["sample_rate"]
    vidx, aidx = plan["video_stream_index"], plan["game_audio_stream_index"]
    vst = stream(probe, vidx)
    tb = _tb(vst.get("time_base") or "1/1000")
    origin = origin_us(probe, vidx)
    audio_offset = 0
    if aidx is not None:
        audio_offset = int(stream(probe, aidx).get("start_us") or 0) - origin
    frame_us = -(-1_000_000 // fps)
    spf = rate // fps
    selected = sel["selected_segments"]
    if len(quotas) != len(selected):
        raise IngestFailure("frame quotas do not match the selection")
    reuse: dict[int, dict] = {}
    for _row, m in ex.prior_checkpoints("segment_media"):
        entry, work_ref = m.get("segment"), m.get("work_media")
        idx = m.get("item_index")
        if not isinstance(entry, dict) or not isinstance(idx, int) or not 1 <= idx <= len(selected):
            continue
        want = selected[idx - 1]
        if entry.get("segment_id") != want["segment_id"] or entry.get("source_start_us") != want["start_us"] or entry.get("source_end_us") != want["end_us"]:
            continue
        try:
            path = ex.under_root(work_ref)
            if path.is_file() and sha256_file(path, cancel=ex.check) == entry.get("media_sha256"):
                reuse[idx] = {"entry": entry, "path": path}
        except (OSError, IngestFailure):
            continue
    if reuse:
        ex.log(f"reusing {len(reuse)} verified prepared clips")
    ex.work.mkdir(parents=True, exist_ok=True)
    started = time.monotonic()
    deadline = float(ex.policy["prepare_timeout_seconds"])
    mapped, timeline = [], 0
    for i, want in enumerate(selected, 1):
        a_us, b_us = want["start_us"], want["end_us"]
        frames = output_frames(a_us, b_us, fps)
        samples = output_samples(frames, fps, rate) if aidx is not None else 0
        if i in reuse:
            entry = dict(reuse[i]["entry"])
            entry["timeline_in_frames"], entry["frame_quota"] = timeline, quotas[i - 1]
            mapped.append((entry, reuse[i]["path"]))
            timeline += frames
            continue
        left = deadline - (time.monotonic() - started)
        if left <= 0:
            raise IngestFailure(f"prepare exceeded {deadline:.0f}s after {i - 1}/{len(selected)} clips; finished clips are kept for retry")
        ex.check_space((b_us - a_us) * 4 * 1024 * 1024 // 1_000_000)
        dest = ex.work / f"segment_{i:03d}.mp4"
        if dest.exists():
            dest.unlink()
        _encode(ex, snap, plan, sel["output"], a_us, b_us, origin, frames, samples, dest, left)
        measured_frames, measured_samples, decoded = _measure(ex, dest, aidx is not None)
        if abs(measured_frames - frames) > 1:
            raise IngestFailure(f"clip {want['segment_id']} has {measured_frames} frames, planned {frames}")
        if aidx is not None:
            if decoded <= 0 or decoded + spf < samples or abs(measured_samples - samples) > spf:
                raise IngestFailure(f"clip {want['segment_id']} audio length is outside one frame of samples")
        src_frames, first = _source_frames(ex, snap, vidx, tb, origin + a_us, origin + b_us)
        start_err = 0
        if first is not None:
            rel = first - (origin + a_us)
            start_err = round(round(rel * fps / 1_000_000) * 1_000_000 / fps) - rel
        end_err = round(frames * 1_000_000 / fps) - (b_us - a_us)
        if abs(start_err) > frame_us or abs(end_err) > frame_us:
            raise IngestFailure(f"clip {want['segment_id']} boundary error exceeds one target frame")
        pad = 0
        if aidx is not None and audio_offset > a_us:
            pad = min(samples, (audio_offset - a_us) * rate // 1_000_000)
        entry = {
            "segment_id": want["segment_id"], "index": i, "media": f"media/segment_{i:03d}.mp4",
            "media_sha256": sha256_file(dest, cancel=ex.check),
            "source_start_us": a_us, "source_end_us": b_us,
            "source_start_pts": _pts(origin + a_us, tb), "source_end_pts": _pts(origin + b_us, tb),
            "timeline_in_frames": timeline, "output_frames": frames, "output_samples": samples,
            "measured_frames": measured_frames, "measured_samples": measured_samples,
            "start_error_us": start_err, "end_error_us": end_err, "audio_sample_error": measured_samples - samples,
            "source_frames_in_range": src_frames, "duplicated_frames": max(0, frames - src_frames),
            "dropped_frames": max(0, src_frames - frames), "audio_pad_start_samples": pad, "frame_quota": quotas[i - 1],
        }
        ex.save_checkpoint("segment_media", i, {"segment": entry, "work_media": ex.rel(dest)})
        mapped.append((entry, dest))
        timeline += frames
        ex.progress(0.05 + 0.85 * i / len(selected), f"prepared clip {i}/{len(selected)}")

    staging = ex.fresh_staging()
    media_dir = staging / "media"
    for entry, path in mapped:
        link_or_copy(path, media_dir / Path(entry["media"]).name)
    segments = [e for e, _ in mapped]
    source_map = {
        "schema_version": 1, "source_id": ex.inp["source"]["source_id"], "source_sha256": sel["source_sha256"],
        "selection_sha256": ex.inp["selection_sha256"], "video_stream_index": vidx, "game_audio_stream_index": aidx,
        "source_video_time_base": vst.get("time_base") or "", "source_origin_pts": vst.get("start_pts") or 0,
        "source_origin_us": origin, "audio_offset_us": audio_offset, "output": sel["output"],
        "frame_quota_version": ex.policy["frame_quota_version"], "segments": segments,
    }
    provenance = dict(ex.inp["provenance"])
    provenance["worker_version"] = ex.receipt()["worker_version"]
    provenance["method_version"] = seg_mod.METHOD_VERSION
    video, audio = [], []
    for e in segments:
        dur = e["output_frames"] / fps
        tin = e["timeline_in_frames"] / fps
        video.append({"src": e["media"], "in": 0, "out": dur, "timeline_in": tin,
                      "ingest": {"segment_id": e["segment_id"], "frame_quota": e["frame_quota"],
                                 "source_start_us": e["source_start_us"], "source_end_us": e["source_end_us"]}})
        if aidx is not None:
            audio.append({"src": e["media"], "in": 0, "out": dur, "timeline_in": tin})
    edl = {"timeline": {"fps": fps, "sample_rate": rate}, "video": video, "game_audio": audio, "voice": [], "subtitle": [], "music": [],
           "ingest": {"schema_version": 1, "source_id": source_map["source_id"], "run_id": provenance["run_id"],
                      "source_map": "source-map.json", "provenance": "ingest-provenance.json",
                      "frame_quota_version": source_map["frame_quota_version"]}}
    dump_json(staging / "source-map.json", source_map)
    dump_json(staging / "ingest-provenance.json", provenance)
    dump_json(staging / "edl.json", edl)
    artifacts = [{"kind": "edl", "path": "edl.json"}]
    artifacts += [{"kind": "video", "path": e["media"]} for e in segments]
    artifacts += [{"kind": "source_map", "path": "source-map.json"}, {"kind": "ingest_provenance", "path": "ingest-provenance.json"}]
    dump_json(staging / "delivery-manifest.json", {"schema_version": 1, "source_edl": "edl.json", "artifacts": artifacts})
    ex.progress(0.95, "registering prepared package")
    ex.publish()
    return {"segments": len(segments), "frames": timeline}
