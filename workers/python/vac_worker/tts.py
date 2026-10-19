"""Shared, bounded speech synthesis for the console and reviewed narration.

No SDK dependency, model download, engine fallback, or embedded credentials.
"""
from __future__ import annotations

import base64
import json
import math
import os
import re
import subprocess
import tempfile
import urllib.error
import urllib.request
import wave
from pathlib import Path

from .provider_http import NoRedirect, api_base

MAX_AUDIO_BYTES = 32 * 1024 * 1024
SYSTEM_VOICES = [
    {"id": "vibrant-youth", "name": "元气男声"},
    {"id": "lively-girl", "name": "活泼女声"},
    {"id": "soft-spoken-gentleman", "name": "温柔男声"},
    {"id": "magnetic-voiced-male", "name": "磁性男声"},
    {"id": "zixinnansheng", "name": "自信男声"},
    {"id": "elegantgentle-female", "name": "温婉女声"},
    {"id": "livelybreezy-female", "name": "轻快女声"},
]


class TTSError(Exception):
    pass


def legacy_settings(voice=""):
    return {"engine": "sapi", "voice": voice, "language": "zh", "speed": 1.0,
            "volume": 1.0, "sapi_rate": 5, "pitch_semitones": 0, "instruction": ""}


def validate_settings(s, *, legacy=False):
    if not isinstance(s, dict) or s.get("engine") not in ("sapi", "stepfun"):
        raise TTSError("请选择 SAPI 或 StepFun 引擎")
    voice = s.get("voice", "")
    if not isinstance(voice, str) or len(voice) > 200 or (not voice.strip() and not legacy):
        raise TTSError("音色不能为空且不得超过 200 字符")
    for name, low, high in (("speed", .5, 2), ("volume", .1, 2), ("pitch_semitones", -12, 12), ("sapi_rate", -10, 10)):
        value = s.get(name)
        if type(value) not in (int, float) or not math.isfinite(value) or not low <= value <= high:
            raise TTSError(f"无效参数：{name}")
    if int(s["sapi_rate"]) != s["sapi_rate"]:
        raise TTSError("SAPI 语速必须为整数")
    for name in ("low_gain_db", "mid_gain_db", "high_gain_db"):
        value = s.get(name, 0)
        if type(value) not in (int, float) or not math.isfinite(value) or not -12 <= value <= 12:
            raise TTSError(f"无效频段增益：{name}")
    instruction = s.get("instruction", "")
    if not isinstance(instruction, str) or len(instruction) > 200 or "\x00" in instruction:
        raise TTSError("表达指令不得超过 200 字符")
    if s.get("language") not in ("zh", "en", "ja", "ko", "fr", "es"):
        raise TTSError("不支持的语言")
    if s["engine"] == "sapi":
        if instruction or s["volume"] > 1 or s["speed"] != 1 or any(s.get(k) for k in ("endpoint", "model", "token_env")):
            raise TTSError("SAPI 不支持云端参数或表达指令")
    else:
        if s.get("model") != "stepaudio-2.5-tts":
            raise TTSError("首期支持 stepaudio-2.5-tts 模型")
        api_base(s.get("endpoint", ""), "openai")
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,127}", s.get("token_env", "")):
            raise TTSError("无效的凭证环境变量名称")
    return s


def sapi_script(text: str, out: Path, voice_name="", rate=5, volume=100):
    encode = lambda value: base64.b64encode(str(value).encode("utf-8")).decode()
    return (
        "$ErrorActionPreference='Stop';"
        f"$t=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{encode(text)}'));"
        f"$p=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{encode(out)}'));"
        "$s=New-Object -ComObject SAPI.SpVoice;"
        f"$wanted=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{encode(voice_name)}'));"
        "if($wanted){$found=$false;foreach($v in $s.GetVoices()){if($v.GetAttribute('Name') -eq $wanted){$s.Voice=$v;$found=$true;break}};if(!$found){throw 'Selected SAPI voice unavailable'}}"
        "else{$voices=$s.GetVoices('Language=804','');if($voices.Count -lt 1){throw 'Chinese SAPI voice unavailable'};$s.Voice=$voices.Item(0)};"
        f"$s.Rate={int(rate)};$s.Volume={int(volume)};"
        "$stream=New-Object -ComObject SAPI.SpFileStream;"
        "$fmt=New-Object -ComObject SAPI.SpAudioFormat;$fmt.Type=22;$stream.Format=$fmt;"
        "try{$stream.Open($p,3,$false);$s.AudioOutputStream=$stream;[void]$s.Speak($t)}"
        "finally{if($stream){$stream.Close()}}"
    )


def synthesize_raw(text, out, settings, tools, *, legacy=False, opener=None):
    validate_settings(settings, legacy=legacy)
    if not isinstance(text, str) or not text.strip() or len(text) > 1000 or "\x00" in text:
        raise TTSError("合成文本须为 1–1,000 字符，不会自动截断")
    if settings["engine"] == "sapi":
        if os.name != "nt":
            raise TTSError("sapi unavailable")
        tools.run([tools.powershell, "-NoProfile", "-NonInteractive", "-Command",
                   sapi_script(text, out, settings["voice"], settings["sapi_rate"], round(settings["volume"] * 100))])
        return
    token = os.environ.get(settings["token_env"], "").strip()
    if not token:
        raise TTSError("缺少 TTS 凭证，请在启动环境设置凭证变量")
    if len(token) > 4096 or any(c in token for c in "\r\n\x00"):
        raise TTSError("TTS 凭证格式无效")
    body = {"model": settings["model"], "input": text, "voice": settings["voice"],
            "language": settings["language"], "speed": settings["speed"], "volume": settings["volume"],
            "instruction": settings["instruction"], "response_format": "wav", "sample_rate": 24000,
            "return_url": False}
    request = urllib.request.Request(api_base(settings["endpoint"], "openai") + "/audio/speech",
                                     data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
                                     headers={"Content-Type": "application/json", "Authorization": "Bearer " + token,
                                              "Accept": "audio/wav"}, method="POST")
    open_request = opener or urllib.request.build_opener(NoRedirect()).open
    try:
        with open_request(request, timeout=min(tools.timeout, 120)) as response:
            audio = response.read(MAX_AUDIO_BYTES + 1)
        if len(audio) > MAX_AUDIO_BYTES:
            raise TTSError("TTS 音频超过 32 MiB 限制")
        if len(audio) < 44 or audio[:4] != b"RIFF" or audio[8:12] != b"WAVE":
            raise TTSError("TTS 服务没有返回有效 WAV 音频")
        out.write_bytes(audio)
    except urllib.error.HTTPError as err:
        raise TTSError(f"TTS 服务返回 HTTP {err.code}；请检查凭证、权限或额度后手动重试") from None
    except TTSError:
        raise
    except Exception:
        # Never echo provider bodies, headers or URLs containing credentials.
        raise TTSError("TTS 网络请求失败或超时；未自动重试或切换引擎") from None


def audio_filters(settings, sample_rate=48000):
    pitch = settings.get("pitch_semitones", 0)
    filters = []
    if pitch:
        filters.append(f"rubberband=tempo=1:pitch={2 ** (pitch / 12):.12f}:formant=preserved:pitchq=quality")
    for name, frequency in (("low_gain_db", 120), ("mid_gain_db", 1000), ("high_gain_db", 4000)):
        gain = settings.get(name, 0)
        if gain:
            filters.append(f"equalizer=f={frequency}:t=q:w=1:g={gain}")
    filters.append(f"aresample={sample_rate}")
    return ",".join(filters)


def synthesize(text, out: Path, settings, tools, *, legacy=False):
    with tempfile.TemporaryDirectory(prefix=".tts-", dir=out.parent) as tmp:
        raw = Path(tmp) / "raw.wav"
        synthesize_raw(text, raw, settings, tools, legacy=legacy)
        tools.run([tools.ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-i", str(raw),
                   "-af", audio_filters(settings), "-ar", "48000", "-ac", "2", "-c:a", "pcm_s16le", str(out)])
    with wave.open(str(out), "rb") as audio:
        if audio.getframerate() != 48000 or audio.getnchannels() != 2 or audio.getsampwidth() != 2 or audio.getnframes() <= 0:
            raise TTSError("合成音频格式或时长无效")
        duration = audio.getnframes() / audio.getframerate()
    if out.stat().st_size > MAX_AUDIO_BYTES:
        raise TTSError("试听音频超过 32 MiB 限制")
    return {"duration_seconds": duration, "size_bytes": out.stat().st_size}


def capabilities(ffmpeg="ffmpeg", powershell="powershell"):
    voices, pitch, media = [], False, False
    try:
        result = subprocess.run([ffmpeg, "-hide_banner", "-filters"], capture_output=True, text=True, timeout=5,
                                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
        media = result.returncode == 0
        pitch = media and "rubberband" in result.stdout
    except (OSError, subprocess.TimeoutExpired):
        pass
    if os.name == "nt":
        try:
            script = "$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);$s=New-Object -ComObject SAPI.SpVoice;@($s.GetVoices() | ForEach-Object {$_.GetAttribute('Name')}) | ConvertTo-Json -Compress"
            result = subprocess.run([powershell, "-NoProfile", "-NonInteractive", "-Command", script],
                                    capture_output=True, timeout=5, creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
            if result.returncode == 0:
                data = json.loads(result.stdout.decode("utf-8-sig"))
                voices = data if isinstance(data, list) else [data]
        except (OSError, ValueError, subprocess.TimeoutExpired):
            pass
    return {"sapi_voices": voices, "stepfun_voices": SYSTEM_VOICES, "pitch_available": pitch, "media_ready": media}
