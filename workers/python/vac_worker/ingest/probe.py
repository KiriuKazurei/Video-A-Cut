"""media_probe: copy the recording into its controlled snapshot while hashing
the byte stream, then describe its streams with ffprobe."""
from __future__ import annotations

import hashlib
import json
import os
import unicodedata
from pathlib import Path

from .common import Exec, IngestFailure, dump_json, is_hex64, resolve_source, sha256_file

PROBE_VERSION = "ffprobe-norm-1"
_TYPES = {"video", "audio", "subtitle", "data", "attachment"}
_HDR = {"smpte2084", "arib-std-b67"}


def _snapshot_paths(ex: Exec) -> tuple[Path, Path, str]:
    src = ex.inp["source"]
    ext = os.path.splitext(src["relative_path"])[1].lower()
    if not ext or len(ext) > 9 or not ext[1:].isalnum():
        raise IngestFailure("the recording has no usable file extension")
    folder = ex.under_root(src["source_dir"])
    snap = folder / f"snapshot{ext}"
    return folder, snap, f"{src['source_dir']}/snapshot{ext}"


def _marker_ok(st, src) -> bool:
    return st.st_size == src["size_bytes"] and st.st_mtime_ns == int(src["mtime_ns"])


def copy_snapshot(ex: Exec) -> dict:
    src = ex.inp["source"]
    folder, snap, snap_rel = _snapshot_paths(ex)
    folder.mkdir(parents=True, exist_ok=True)
    size = int(src["size_bytes"])
    chunk = int(ex.policy["copy_chunk_bytes"])
    chunks = max(1, -(-size // chunk))
    base = {"source_id": src["source_id"], "source_version": src["source_version"], "size_bytes": size,
            "mtime_ns": int(src["mtime_ns"]), "snapshot": snap_rel, "chunk_bytes": chunk, "chunks": chunks}

    if src.get("snapshot_ref") and snap.is_file():
        if src["snapshot_ref"] != snap_rel or snap.stat().st_size != size:
            raise IngestFailure("published snapshot does not match its registration")
        ex.progress(0.05, "verifying published snapshot")
        digest = sha256_file(snap, cancel=ex.check)
        if digest != src.get("sha256"):
            raise IngestFailure("published snapshot hash changed; register the recording again")
        return {**base, "sha256": digest, "reused": True}

    path, st = resolve_source(ex.roots, src["root_id"], src["relative_path"])
    if not _marker_ok(st, src):
        raise IngestFailure("the recording changed after registration (size or modification time); register it again")
    if size > int(ex.policy["max_source_bytes"]):
        raise IngestFailure("the recording exceeds the policy size limit")

    part = folder / f"snapshot.{ex.inp['execution_id']}.part"
    hashes: list[str] = []
    done = 0
    resume = None
    for _row, m in ex.prior_checkpoints("copy_chunk"):
        if m.get("source_version") == src["source_version"] and m.get("chunk_bytes") == chunk and (
                isinstance(m.get("chunk_sha256"), list) or isinstance(m.get("ledger"), list)):
            if resume is None or m["offset_bytes"] > resume["offset_bytes"]:
                resume = m
    if resume is not None:
        old = ex.under_root(resume["part"])
        try:
            hashes, done = _load_resume(ex, old, resume, chunk)
            if old != part:
                ex.check_space(size)
                extra = old.stat().st_size - done
                if extra > 0:
                    ex.log(f"dropping {extra} unverified trailing bytes from the resumed copy; historical file retained")
                # Historical checkpoint files stay usable if the new runtime
                # crashes before registering its own checkpoint.
                with open(old, "rb") as previous, open(part, "wb") as current:
                    remaining = done
                    while remaining:
                        ex.check()
                        buf = previous.read(min(chunk, remaining))
                        if not buf:
                            raise IngestFailure("verified copy prefix became shorter")
                        current.write(buf)
                        remaining -= len(buf)
                    current.flush()
                    os.fsync(current.fileno())
            extra = part.stat().st_size - done
            if extra > 0:
                ex.log(f"dropping {extra} unverified trailing bytes")
                with open(part, "r+b") as clipped:
                    clipped.truncate(done)
            ex.log(f"resuming copy at {done} bytes from a verified checkpoint")
        except (OSError, IngestFailure) as err:
            ex.log(f"copy checkpoint not reusable ({type(err).__name__}); copying from the start")
            hashes, done = [], 0
    if done == 0 and part.exists():
        part.unlink()

    ex.check_space(size - done)
    every = max(1, chunks // 200)
    whole = hashlib.sha256()
    with open(part, "r+b" if done else "wb") as out:
        if done:
            out.seek(0)
            remaining = done
            while remaining:
                ex.check()
                buf = out.read(min(chunk, remaining))
                if not buf:
                    raise IngestFailure("staged copy is shorter than its checkpoint")
                whole.update(buf)
                remaining -= len(buf)
            out.seek(done)
            out.truncate(done)
        with open(path, "rb") as inp:
            inp.seek(done)
            index = len(hashes)
            while done < size:
                ex.check()
                buf = inp.read(min(chunk, size - done))
                if not buf:
                    raise IngestFailure("the recording became shorter while it was copied")
                out.write(buf)
                whole.update(buf)
                hashes.append(hashlib.sha256(buf).hexdigest())
                done += len(buf)
                index += 1
                if index % every == 0 or done == size:
                    out.flush()
                    os.fsync(out.fileno())
                    if done < size:
                        ex.check_space(size - done)
                        ex.save_checkpoint("copy_chunk", index, copy_checkpoint_body(ex, src, part, chunk, done, hashes))
                    ex.progress(0.02 + 0.78 * done / size, f"copied {done}/{size} bytes")
    st2 = os.stat(path)
    if not _marker_ok(st2, src):
        part.unlink(missing_ok=True)
        raise IngestFailure("the recording changed while it was copied; register it again")
    digest = whole.hexdigest()
    if snap.exists():
        if snap.stat().st_size == size and sha256_file(snap, cancel=ex.check) == digest:
            part.unlink()
            return {**base, "sha256": digest, "reused": True}
        snap.unlink()
    os.replace(part, snap)
    return {**base, "sha256": digest, "reused": False}


_MANIFEST_BUDGET = 900_000


def copy_checkpoint_body(ex: Exec, src: dict, part: Path, chunk: int, done: int, hashes: list[str]) -> dict:
    """Keep a full hash list while it fits. Larger copies use a chained ledger of shards."""
    body = {"source_version": src["source_version"], "part": ex.rel(part), "chunk_bytes": chunk,
            "offset_bytes": done, "chunk_sha256": list(hashes)}
    if len(json.dumps(body).encode("utf-8")) <= _MANIFEST_BUDGET:
        return body
    # Fixed-size chunks make construction linear and keep every shard bounded.
    shards = [hashes[i:i + 9000] for i in range(0, len(hashes), 9000)]
    folder = ex.out_dir / "checkpoints" / "ledger"
    refs, chain = [], ""
    for index, shard in enumerate(shards):
        chain = hashlib.sha256((chain + "\n" + "\n".join(shard)).encode("utf-8")).hexdigest()
        name = f"{index:05d}-{chain}.json"
        raw = dump_json(folder / name, {"schema_version": 1, "index": index, "hashes": shard, "chain_sha256": chain})
        if len(raw) > (1 << 20):
            raise IngestFailure("copy ledger shard exceeds 1 MiB")
        refs.append(ex.rel(folder / name))
    return {"source_version": src["source_version"], "part": ex.rel(part), "chunk_bytes": chunk,
            "offset_bytes": done, "ledger": refs, "chain_sha256": chain, "chunks_committed": len(hashes)}


def _load_resume(ex: Exec, part: Path, manifest: dict, chunk: int) -> tuple[list[str], int]:
    if manifest.get("ledger"):
        hashes: list[str] = []
        chain = ""
        from .journal import read_bounded
        for ref in manifest["ledger"]:
            doc = json.loads(read_bounded(ex.under_root(ref)))
            shard = list(doc.get("hashes") or [])
            chain = hashlib.sha256((chain + "\n" + "\n".join(shard)).encode("utf-8")).hexdigest()
            if chain != doc.get("chain_sha256"):
                raise IngestFailure("copy ledger chain does not match")
            hashes.extend(shard)
        if chain != manifest.get("chain_sha256") or len(hashes) != int(manifest.get("chunks_committed") or -1):
            raise IngestFailure("copy ledger is incomplete")
        return _verify_prefix(ex, part, {**manifest, "chunk_sha256": hashes}, chunk)
    return _verify_prefix(ex, part, manifest, chunk)


def _verify_prefix(ex: Exec, part: Path, m: dict, chunk: int) -> tuple[list[str], int]:
    offset = int(m["offset_bytes"])
    want = list(m["chunk_sha256"])
    size = int(ex.inp["source"]["size_bytes"])
    if (offset < 0 or offset > size or len(want) != -(-offset // chunk)
            or (offset % chunk and offset != size)
            or not all(is_hex64(h) for h in want) or part.stat().st_size < offset):
        raise IngestFailure("copy checkpoint is inconsistent")
    with open(part, "rb") as f:
        for i, h in enumerate(want):
            ex.check()
            buf = f.read(min(chunk, offset - i * chunk))
            if hashlib.sha256(buf).hexdigest() != h:
                raise IngestFailure(f"staged block {i} does not match its checkpoint")
    return want, offset


# ------------------------------------------------------------------ probe

def _text(v, n=64) -> str:
    if not isinstance(v, str):
        return ""
    v = "".join(c for c in v if unicodedata.category(c)[0] != "C")
    return v[:n]


def _int(v) -> int:
    try:
        return int(v)
    except (TypeError, ValueError):
        return 0


def _us(v):
    try:
        return round(float(v) * 1_000_000)
    except (TypeError, ValueError):
        return None


def _mkv_duration(tags: dict):
    raw = tags.get("DURATION") or tags.get("duration")
    if not isinstance(raw, str) or raw.count(":") != 2:
        return None
    h, m, s = raw.split(":")
    try:
        return round((int(h) * 3600 + int(m) * 60 + float(s)) * 1_000_000)
    except ValueError:
        return None


def _rotation(st: dict) -> int:
    for sd in st.get("side_data_list") or []:
        if "rotation" in sd:
            return _int(sd["rotation"])
    return _int((st.get("tags") or {}).get("rotate"))


def frame_rate_mode(ex: Exec, snap: Path, index: int, time_base: str) -> str:
    """Classify from real packet timestamps, never from rate fields alone."""
    out, _ = ex.runner.run([ex.ffprobe, "-v", "error", "-select_streams", str(index), "-show_entries", "packet=pts",
                            "-read_intervals", "%+#600", "-of", "csv=p=0", str(snap)], timeout=120)
    pts = sorted(int(x) for x in out.decode("ascii", "replace").split() if x.strip().lstrip("-").isdigit())
    if len(pts) < 10:
        return "unknown"
    deltas = [b - a for a, b in zip(pts, pts[1:]) if b > a]
    if not deltas:
        return "unknown"
    lo, hi = min(deltas), max(deltas)
    return "cfr" if hi - lo <= 1 else "vfr"


def probe(ex: Exec, snap: Path, size: int) -> dict:
    out, _ = ex.runner.run([ex.ffprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", str(snap)], timeout=300)
    try:
        raw = json.loads(out)
    except ValueError:
        raise IngestFailure("ffprobe output is unreadable; the file may be damaged") from None
    fmt = raw.get("format") or {}
    duration = _us(fmt.get("duration"))
    streams, limitations = [], []
    videos = audios = 0
    for st in raw.get("streams") or []:
        kind = st.get("codec_type") if st.get("codec_type") in _TYPES else "unknown"
        tags = st.get("tags") or {}
        item = {
            "index": _int(st.get("index")), "type": kind, "codec": _text(st.get("codec_name")),
            "profile": _text(st.get("profile")),
            "disposition": {k: _int(v) for k, v in list((st.get("disposition") or {}).items())[:32]},
            "language": _text(tags.get("language")), "title": _text(tags.get("title"), 200),
            "time_base": _text(st.get("time_base")),
            "start_pts": st.get("start_pts") if isinstance(st.get("start_pts"), int) else None,
            "start_us": _us(st.get("start_time")),
            "duration_us": _us(st.get("duration")) if st.get("duration") else _mkv_duration(tags),
            "width": _int(st.get("width")), "height": _int(st.get("height")), "rotation": _rotation(st) if kind == "video" else 0,
            "pix_fmt": _text(st.get("pix_fmt")), "color_transfer": _text(st.get("color_transfer")),
            "color_primaries": _text(st.get("color_primaries")),
            "avg_frame_rate": _text(st.get("avg_frame_rate")), "r_frame_rate": _text(st.get("r_frame_rate")),
            "frame_rate_mode": "", "sample_rate": _int(st.get("sample_rate")), "channels": _int(st.get("channels")),
            "channel_layout": _text(st.get("channel_layout")),
        }
        if kind == "video" and not item["disposition"].get("attached_pic"):
            videos += 1
            item["frame_rate_mode"] = frame_rate_mode(ex, snap, item["index"], item["time_base"])
            if item["color_transfer"] in _HDR:
                limitations.append("hdr_transfer")
            if item["rotation"]:
                limitations.append("rotation_metadata")
            if item["frame_rate_mode"] == "vfr":
                limitations.append("variable_frame_rate")
        elif kind == "video":
            item["type"] = "attachment"
        if kind == "audio":
            audios += 1
        streams.append(item)
    if videos == 0:
        raise IngestFailure("the recording has no decodable video stream")
    if videos > 1:
        limitations.append("multiple_video_streams")
    if audios == 0:
        limitations.append("no_audio")
    if audios > 1:
        limitations.append("multiple_audio_streams")
    for s, st in zip(streams, raw.get("streams") or []):
        if not st.get("duration") and s["duration_us"] and s["start_us"]:
            # Matroska DURATION tags hold the absolute end timestamp, not a length.
            s["duration_us"] = max(0, s["duration_us"] - s["start_us"])
    # Source time is measured from the primary video stream's first frame, so
    # the usable range is that stream's span, not the container end.
    primary = next((s for s in streams if s["type"] == "video"), None)
    if primary and primary["duration_us"]:
        duration = primary["duration_us"]
    if not duration:
        raise IngestFailure("the recording duration is unknown; the file may be damaged")
    if duration > int(ex.policy["max_source_duration_us"]):
        raise IngestFailure("the recording is longer than the policy limit")
    _decode_check(ex, snap, streams)
    return {"schema_version": 1, "probe_version": PROBE_VERSION, "container": _text(fmt.get("format_name"), 100),
            "size_bytes": size, "duration_us": duration, "streams": streams[:64], "limitations": sorted(set(limitations))}


def _decode_check(ex: Exec, snap: Path, streams: list[dict]) -> None:
    """Decode the first seconds of the first video stream to catch damaged files."""
    v = next(s for s in streams if s["type"] == "video")
    out, _ = ex.runner.run([ex.ffmpeg, "-v", "error", "-xerror", "-i", str(snap), "-map", f"0:{v['index']}", "-t", "2",
                            "-f", "framemd5", "-"], timeout=120)
    if b"," not in out:
        raise IngestFailure("the video stream could not be decoded")


def media_probe(ex: Exec) -> dict:
    ex.progress(0.01, "checking the recording")
    snap_doc = copy_snapshot(ex)
    _folder, snap, _rel = _snapshot_paths(ex)
    ex.progress(0.82, "probing streams")
    doc = probe(ex, snap, int(ex.inp["source"]["size_bytes"]))
    staging = ex.fresh_staging()
    dump_json(staging / "snapshot.json", {"schema_version": 1, **snap_doc})
    dump_json(staging / "probe.json", doc)
    ex.progress(0.95, "registering snapshot and probe")
    ex.publish()
    return {"reused": snap_doc["reused"], "streams": len(doc["streams"])}
