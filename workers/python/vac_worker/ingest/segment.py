"""segment: chunked local scene-change detection over the analysis range and
deterministic candidate segments. No external model is called."""
from __future__ import annotations

import re
import time
from pathlib import Path

from .common import Exec, IngestFailure, dump_json, us_to_s

METHOD_VERSION = "ffmpeg-scene-1"
MIN_CUT_GAP_US = 250_000
THUMB_EDGE = 320
_PTS = re.compile(r"pts_time:(-?[0-9.]+)")
_SCORE = re.compile(r"lavfi\.scene_score=([0-9.]+)")


def snapshot_path(ex: Exec) -> Path:
    src = ex.inp["source"]
    if not src.get("snapshot_ref"):
        raise IngestFailure("the source snapshot is not published")
    snap = ex.under_root(src["snapshot_ref"])
    if not snap.is_file() or snap.stat().st_size != int(src["size_bytes"]):
        raise IngestFailure("the published snapshot is missing or changed")
    return snap


def stream(probe: dict, index: int) -> dict:
    for st in probe["streams"]:
        if st["index"] == index:
            return st
    raise IngestFailure(f"stream {index} is not in the probe")


def origin_us(probe: dict, video_index: int) -> int:
    """Source time zero: the first presentation time of the chosen video stream."""
    return int(stream(probe, video_index).get("start_us") or 0)


def scale_filter(edge: int) -> str:
    return f"scale='if(gte(iw,ih),min({edge},iw),-2)':'if(gte(iw,ih),-2,min({edge},ih))'"


def chunk_ranges(r0: int, r1: int, chunk: int, overlap: int) -> list[tuple[int, int, int]]:
    """(start, owned_end, scan_end) per chunk; scans overlap the next chunk."""
    out, s = [], r0
    while s < r1:
        e = min(s + chunk, r1)
        out.append((s, e, min(e + overlap, r1)))
        s = e
    return out


def scan_chunk(ex: Exec, snap: Path, vidx: int, origin: int, s: int, scan_end: int, threshold: float, timeout: float) -> list[dict]:
    work = ex.work
    work.mkdir(parents=True, exist_ok=True)
    name = f"scan_{s}.txt"
    (work / name).unlink(missing_ok=True)
    abs_s, abs_e = origin + s, origin + scan_end
    seek = max(0, s - 1_000_000)
    vf = (f"trim=start={us_to_s(abs_s)}:end={us_to_s(abs_e)},{scale_filter(int(ex.policy['analysis_max_edge']))},"
          f"select='gt(scene\\,{threshold})',metadata=print:file={name}")
    ex.runner.run([ex.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-copyts", "-ss", us_to_s(seek), "-i", str(snap),
                   "-map", f"0:{vidx}", "-an", "-sn", "-dn", "-vf", vf, "-f", "null", "-"], timeout=timeout, cwd=work)
    cuts, pending = [], None
    path = work / name
    if path.exists():
        for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
            m = _PTS.search(line)
            if m:
                pending = round(float(m.group(1)) * 1_000_000) - origin
                continue
            m = _SCORE.search(line)
            if m and pending is not None:
                cuts.append({"t_us": pending, "score": round(float(m.group(1)), 4)})
                pending = None
        path.unlink()
    return cuts


def dedup(cuts: list[dict], r0: int, r1: int) -> list[dict]:
    out: list[dict] = []
    for c in sorted(cuts, key=lambda c: c["t_us"]):
        if not r0 < c["t_us"] < r1:
            continue
        if out and c["t_us"] - out[-1]["t_us"] < MIN_CUT_GAP_US:
            if c["score"] > out[-1]["score"]:
                out[-1] = c
            continue
        out.append(c)
    return out


def candidates(cuts: list[dict], r0: int, r1: int, min_us: int, max_us: int) -> list[dict]:
    """Gap-free segments from cut points, short ones merged, long ones split."""
    segs = []
    bounds = [(r0, 0.0, "range_start")] + [(c["t_us"], c["score"], "scene_change") for c in cuts]
    for i, (start, score, reason) in enumerate(bounds):
        end = bounds[i + 1][0] if i + 1 < len(bounds) else r1
        segs.append({"start_us": start, "end_us": end, "score": score, "reason": reason})
    merged: list[dict] = []
    for seg in segs:
        if merged and (merged[-1]["end_us"] - merged[-1]["start_us"] < min_us):
            merged[-1]["end_us"] = seg["end_us"]
            merged[-1]["reason"] = "merged_short"
            merged[-1]["score"] = max(merged[-1]["score"], seg["score"])
        else:
            merged.append(dict(seg))
    if len(merged) > 1 and merged[-1]["end_us"] - merged[-1]["start_us"] < min_us:
        last = merged.pop()
        merged[-1]["end_us"] = last["end_us"]
        merged[-1]["reason"] = "merged_short"
    out = []
    for seg in merged:
        length = seg["end_us"] - seg["start_us"]
        if length <= max_us:
            out.append(seg)
            continue
        n = -(-length // max_us)
        for k in range(n):
            a = seg["start_us"] + length * k // n
            b = seg["start_us"] + length * (k + 1) // n
            out.append({"start_us": a, "end_us": b, "score": seg["score"] if k == 0 else 0.0,
                        "reason": seg["reason"] if k == 0 else "duration_limit"})
    for i, seg in enumerate(out, 1):
        seg["segment_id"] = f"seg_{i:04d}"
    return out


def thumbnail_indexes(n: int, budget: int) -> list[int]:
    if budget <= 0:
        return []
    if n <= budget:
        return list(range(n))
    return sorted({round(i * (n - 1) / (budget - 1)) for i in range(budget)}) if budget > 1 else [0]


def segment(ex: Exec) -> dict:
    plan = ex.inp["analysis"]
    probe = ex.inp["probe"]
    snap = snapshot_path(ex)
    vidx = plan["video_stream_index"]
    origin = origin_us(probe, vidx)
    r0, r1 = plan["source_range_us"]
    seg = plan["segmentation"]
    threshold = seg["threshold"]
    chunk_us, overlap = int(ex.policy["chunk_us"]), int(ex.policy["overlap_us"])
    chunks = chunk_ranges(r0, r1, chunk_us, overlap)
    done: dict[int, list[dict]] = {}
    for _row, m in ex.prior_checkpoints("scan_chunk"):
        idx = m.get("item_index")
        if isinstance(idx, int) and 0 <= idx < len(chunks) and m.get("range_us") == list(chunks[idx]) and isinstance(m.get("cuts"), list):
            done[idx] = m["cuts"]
    if done:
        ex.log(f"reusing {len(done)} verified scan chunks")
    started = time.monotonic()
    scan_deadline = float(ex.policy["scan_timeout_seconds"])
    for i, (s, e, scan_end) in enumerate(chunks):
        if i in done:
            continue
        left = scan_deadline - (time.monotonic() - started)
        if left <= 0:
            raise IngestFailure(f"scan exceeded the {scan_deadline:.0f}s total deadline after {len(done)}/{len(chunks)} chunks; "
                                "completed chunks are kept for retry")
        cuts = scan_chunk(ex, snap, vidx, origin, s, scan_end, threshold, min(float(ex.policy["chunk_timeout_seconds"]), left))
        done[i] = cuts
        ex.save_checkpoint("scan_chunk", i, {"range_us": [s, e, scan_end], "cuts": cuts})
        ex.progress(0.05 + 0.75 * len(done) / len(chunks), f"scanned {(e - r0) // 1_000_000}s/{(r1 - r0) // 1_000_000}s")
    all_cuts = dedup([c for i in sorted(done) for c in done[i]], r0, r1)
    cands = candidates(all_cuts, r0, r1, int(seg["min_segment_us"]), int(seg["max_segment_us"]))
    if len(cands) > int(ex.policy["max_candidates"]) or len(all_cuts) > int(ex.policy["max_candidates"]):
        raise IngestFailure(f"{len(cands)} candidates exceed the policy budget of {ex.policy['max_candidates']}; "
                            "raise the threshold or minimum length in a new analysis plan")
    staging = ex.fresh_staging()
    thumbs = staging / "thumbnails"
    thumbs.mkdir()
    picks = thumbnail_indexes(len(cands), int(ex.policy["max_thumbnails"]))
    for n, i in enumerate(picks):
        c = cands[i]
        mid = origin + (c["start_us"] + c["end_us"]) // 2
        ex.runner.run([ex.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-copyts", "-ss", us_to_s(max(0, mid - origin - 1_000_000)),
                       "-i", str(snap), "-map", f"0:{vidx}",
                       "-vf", f"trim=start={us_to_s(mid)},{scale_filter(THUMB_EDGE)}", "-frames:v", "1", "-q:v", "5",
                       str(thumbs / f"{c['segment_id']}.jpg")], timeout=60)
        if (thumbs / f"{c['segment_id']}.jpg").is_file():
            c["thumbnail"] = f"thumbnails/{c['segment_id']}.jpg"
        if n % 8 == 0:
            ex.progress(0.82 + 0.12 * (n + 1) / max(1, len(picks)), f"thumbnails {n + 1}/{len(picks)}")
    doc = {"schema_version": 1, "method": seg["method"], "method_version": METHOD_VERSION, "plan_sha256": ex.inp["analysis_sha256"],
           "range_us": [r0, r1],
           "config": {"threshold": threshold, "min_segment_us": seg["min_segment_us"], "max_segment_us": seg["max_segment_us"],
                      "chunk_us": chunk_us, "overlap_us": overlap, "max_edge": int(ex.policy["analysis_max_edge"]),
                      "min_cut_gap_us": MIN_CUT_GAP_US},
           "chunks_total": len(chunks), "chunks_done": len(done), "complete": len(done) == len(chunks),
           "cuts": all_cuts,
           "candidates": [{"segment_id": c["segment_id"], "start_us": c["start_us"], "end_us": c["end_us"], "score": c["score"],
                           "reason": c["reason"], "thumbnail": c.get("thumbnail", "")} for c in cands]}
    raw = dump_json(staging / "segments.json", doc)
    if len(raw) > int(ex.policy["max_detection_json_bytes"]):
        raise IngestFailure("segments.json exceeds the detection metadata budget")
    ex.progress(0.96, "registering candidates")
    ex.publish()
    return {"cuts": len(all_cuts), "candidates": len(cands)}
