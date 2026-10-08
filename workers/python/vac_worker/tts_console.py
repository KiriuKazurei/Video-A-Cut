"""JSON stdin/stdout bridge for the Go-owned console executor."""
import json
import os
import sys
from pathlib import Path

from .stages import Tools
from .tts import TTSError, capabilities, synthesize


def main():
    try:
        request = json.loads(sys.stdin.read(65537))
        if request.get("operation") == "capabilities":
            result = capabilities()
        else:
            # A process-local, noninheritable job handle owns all descendants.
            # When Go cancels this Python process the kernel also stops FFmpeg/SAPI.
            if os.name == "nt":
                import ctypes
                from ctypes import wintypes
                from .ingest.runtime import OwnedJob
                owned_job = OwnedJob()
                owned_job.k.GetCurrentProcess.restype = wintypes.HANDLE
                if not owned_job.k.AssignProcessToJobObject(owned_job.handle, owned_job.k.GetCurrentProcess()):
                    raise TTSError("无法建立合成进程隔离")
            out = Path(request["output"])
            if not out.is_absolute() or not out.parent.is_dir():
                raise TTSError("无效的受控输出目录")
            result = synthesize(request["text"], out, request["settings"], Tools(timeout=110))
        print(json.dumps({"ok": True, **result}, ensure_ascii=False))
        return 0
    except TTSError as err:
        print(json.dumps({"ok": False, "message": str(err)}, ensure_ascii=False))
    except Exception:
        print(json.dumps({"ok": False, "message": "本地合成或音频处理失败，请检查运行环境"}, ensure_ascii=False))
    return 1


if __name__ == "__main__":
    sys.exit(main())
