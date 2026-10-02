"""Synthetic recordings for phase-7 tests. Every fixture has known ground
truth (cut times, frame timing, marker times) that tests measure against."""
from __future__ import annotations

import subprocess
from pathlib import Path

# Scene layout of scenes.mkv in source seconds (relative to video start).
SCENE_CUTS = (4.0, 9.0, 13.0)
SCENE_DURATION = 17.0
# Distinct luma: ffmpeg's scene score ignores chroma-only changes.
SCENE_COLORS = ("navy", "orange", "teal", "white")
SCENE_MIC_DELAY = 0.75
# Flash/beep markers of sync fixtures (source seconds relative to video
# start). They sit on frames whose time is not jittered by the VFR pattern.
SYNC_MARKERS = (2.0, 5.5, 8.2)
SYNC_DURATION = 10.0
START_OFFSET = 1.5


def _run(ffmpeg: str, *args: str) -> None:
    done = subprocess.run([ffmpeg, "-hide_banner", "-loglevel", "error", "-y", *args], capture_output=True, text=True,
                          creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    if done.returncode != 0:
        raise RuntimeError(f"fixture ffmpeg failed: {done.stderr[-400:]}")


def scenes(ffmpeg: str, out: Path, fps: str = "30", offset: float = START_OFFSET) -> Path:
    """Solid colour scenes with temporal noise; hard cuts at SCENE_CUTS.
    Audio stream 1 is 440 Hz from the video start; stream 2 is 660 Hz whose
    first PTS is SCENE_MIC_DELAY seconds later."""
    bounds = (0.0, *SCENE_CUTS, SCENE_DURATION)
    inputs, labels = [], []
    for i, color in enumerate(SCENE_COLORS):
        d = bounds[i + 1] - bounds[i]
        inputs += ["-f", "lavfi", "-i", f"color=c={color}:s=320x240:r={fps}:d={d}"]
        labels.append(f"[{i}:v]")
    n = len(SCENE_COLORS)
    inputs += ["-f", "lavfi", "-i", f"sine=frequency=440:sample_rate=48000:duration={SCENE_DURATION}",
               "-itsoffset", str(SCENE_MIC_DELAY), "-f", "lavfi", "-i",
               f"sine=frequency=660:sample_rate=48000:duration={SCENE_DURATION - SCENE_MIC_DELAY}"]
    graph = "".join(labels) + f"concat=n={n}:v=1:a=0,noise=alls=8:allf=t[v]"
    _run(ffmpeg, *inputs, "-filter_complex", graph, "-map", "[v]", "-map", f"{n}:a", "-map", f"{n + 1}:a",
         "-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-c:a", "aac", "-output_ts_offset", str(offset),
         "-metadata:s:a:0", "language=eng", "-metadata:s:a:0", "title=game", "-metadata:s:a:1", "title=mic", str(out))
    return out


def sync(ffmpeg: str, out: Path, vfr: bool = True, audio_delay: float = 0.4, offset: float = START_OFFSET) -> Path:
    """Black video with one white frame at each marker and a sample-exact
    1 kHz beep at the same source time. With ``vfr`` the frame times are
    genuinely non-uniform. The audio stream's first PTS is ``audio_delay``
    seconds after the video's."""
    fps = 30
    flash = "+".join(f"between(T,{m - 0.001:.4f},{m + 1 / fps - 0.002:.4f})" for m in SYNC_MARKERS)
    beep = "+".join(f"between(t,{m - audio_delay:.4f},{m - audio_delay + 0.05:.4f})" for m in SYNC_MARKERS)
    vf = f"format=yuv420p,geq=lum='if({flash},235,16)':cb=128:cr=128"
    if vfr:
        vf += ",settb=1/1000,setpts='(N/30+if(eq(mod(N,3),1),0.008,0))/TB'"
    _run(ffmpeg, "-f", "lavfi", "-i", f"color=c=black:s=320x240:r={fps}:d={SYNC_DURATION}",
         "-itsoffset", str(audio_delay), "-f", "lavfi", "-i",
         f"aevalsrc='if({beep},0.8*sin(2*PI*1000*t),0)':s=48000:d={SYNC_DURATION - audio_delay}",
         "-filter_complex", f"[0:v]{vf}[v]", "-map", "[v]", "-map", "1:a", *(["-fps_mode", "vfr"] if vfr else []),
         "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-output_ts_offset", str(offset), str(out))
    return out


def plain(ffmpeg: str, out: Path, fps: str, seconds: float = 6.0, audio: bool = True) -> Path:
    args = ["-f", "lavfi", "-i", f"testsrc2=size=320x240:rate={fps}:duration={seconds}"]
    if audio:
        args += ["-f", "lavfi", "-i", f"sine=frequency=500:sample_rate=48000:duration={seconds}"]
    args += ["-c:v", "libx264", "-preset", "ultrafast"]
    if audio:
        args += ["-c:a", "aac"]
    _run(ffmpeg, *args, str(out))
    return out


def broken(src: Path, out: Path) -> Path:
    raw = src.read_bytes()
    out.write_bytes(raw[:2048] + bytes(4096) + raw[-1024:])
    return out


def audio_only(ffmpeg: str, out: Path) -> Path:
    _run(ffmpeg, "-f", "lavfi", "-i", "sine=frequency=300:duration=3", "-c:a", "aac", str(out))
    return out


def measure_markers(ffmpeg: str, clip: Path, audio: bool = True) -> tuple[list[float], list[float]]:
    """Flash times (frame pts, s) and beep onsets (sample index / 48000, s) in a clip."""
    import array
    import re
    done = subprocess.run([ffmpeg, "-v", "error", "-i", str(clip), "-map", "0:v:0", "-vf",
                           "signalstats,metadata=print:key=lavfi.signalstats.YAVG:file=-", "-f", "null", "-"],
                          capture_output=True, text=True, creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
    flashes, pts = [], None
    for line in done.stdout.splitlines():
        m = re.search(r"pts_time:(-?[0-9.]+)", line)
        if m:
            pts = float(m.group(1))
            continue
        m = re.search(r"YAVG=([0-9.]+)", line)
        if m and pts is not None and float(m.group(1)) > 128:
            if not flashes or pts - flashes[-1] > 0.2:
                flashes.append(round(pts, 6))
    beeps: list[float] = []
    if audio:
        raw = subprocess.run([ffmpeg, "-v", "error", "-i", str(clip), "-map", "0:a:0", "-f", "s16le", "-ac", "1", "-ar", "48000", "-"],
                             capture_output=True, creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0)).stdout
        samples = array.array("h", raw)
        last = -10**9
        for i, v in enumerate(samples):
            if abs(v) > 8000 and i - last > 9600:
                beeps.append(round(i / 48000, 6))
                last = i
            elif abs(v) > 8000:
                last = i
    return flashes, beeps


def build_all(ffmpeg: str, root: Path) -> dict[str, Path]:
    root.mkdir(parents=True, exist_ok=True)
    files = {
        "scenes": scenes(ffmpeg, root / "scenes 场景.mkv"),
        "sync_vfr": sync(ffmpeg, root / "sync_vfr.mkv"),
        "sync_cfr": sync(ffmpeg, root / "sync_cfr.mkv", vfr=False, audio_delay=0.0, offset=0.0),
        "ntsc": plain(ffmpeg, root / "ntsc_29.97.mp4", "30000/1001"),
        "sixty": plain(ffmpeg, root / "sixty.mov", "60"),
        "noaudio": plain(ffmpeg, root / "noaudio.mp4", "30", audio=False),
        "audio_only": audio_only(ffmpeg, root / "audio_only.mkv"),
    }
    files["broken"] = broken(files["scenes"], root / "broken.mkv")
    return files
