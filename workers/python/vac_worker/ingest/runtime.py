"""Local process identities and kernel-owned media trees (no image-name kills)."""
from __future__ import annotations
import ctypes
import os
from ctypes import wintypes as w


def process_identity(pid: int) -> dict | None:
    if os.name != "nt":
        try:
            from pathlib import Path
            fields = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
            return {"pid": pid, "created": fields[19], "path": os.readlink(f"/proc/{pid}/exe")}
        except (FileNotFoundError, ProcessLookupError):
            return None
    k = ctypes.WinDLL("kernel32", use_last_error=True)
    k.OpenProcess.argtypes, k.OpenProcess.restype = [w.DWORD, w.BOOL, w.DWORD], w.HANDLE
    k.CloseHandle.argtypes = [w.HANDLE]
    k.GetProcessTimes.argtypes = [w.HANDLE] + [ctypes.POINTER(w.FILETIME)] * 4
    k.QueryFullProcessImageNameW.argtypes = [w.HANDLE, w.DWORD, w.LPWSTR, ctypes.POINTER(w.DWORD)]
    handle = k.OpenProcess(0x1000, False, pid)
    if not handle:
        error = ctypes.get_last_error()
        if error == 87:  # ERROR_INVALID_PARAMETER: the PID does not exist.
            return None
        raise OSError(error, "cannot verify process identity")
    try:
        created, exited, kernel, user = (w.FILETIME() for _ in range(4))
        if not k.GetProcessTimes(handle, ctypes.byref(created), ctypes.byref(exited), ctypes.byref(kernel), ctypes.byref(user)):
            raise OSError(ctypes.get_last_error(), "cannot verify process creation time")
        if exited.dwLowDateTime or exited.dwHighDateTime:
            return None
        buf, size = ctypes.create_unicode_buffer(32768), w.DWORD(32768)
        if not k.QueryFullProcessImageNameW(handle, 0, buf, ctypes.byref(size)):
            raise OSError(ctypes.get_last_error(), "cannot verify process executable")
        return {"pid": pid, "created": str((created.dwHighDateTime << 32) | created.dwLowDateTime), "path": buf.value}
    finally:
        k.CloseHandle(handle)


def matches_process(identity: dict) -> bool:
    current = process_identity(int(identity.get("pid") or 0))
    return current is not None and current == {k: identity.get(k) for k in ("pid", "created", "path")}


class OwnedJob:
    """The worker's noninheritable handle owns all descendants, including on crash."""
    def __init__(self):
        self.handle = None
        if os.name != "nt":
            return
        class Basic(ctypes.Structure):
            _fields_ = [("process_time", ctypes.c_longlong), ("job_time", ctypes.c_longlong),
                        ("flags", w.DWORD), ("min_ws", ctypes.c_size_t), ("max_ws", ctypes.c_size_t),
                        ("active_limit", w.DWORD), ("affinity", ctypes.c_size_t), ("priority", w.DWORD), ("scheduling", w.DWORD)]
        class Extended(ctypes.Structure):
            _fields_ = [("basic", Basic), ("io", ctypes.c_ulonglong * 6), ("process_memory", ctypes.c_size_t),
                        ("job_memory", ctypes.c_size_t), ("peak_process", ctypes.c_size_t), ("peak_job", ctypes.c_size_t)]
        self.k = ctypes.WinDLL("kernel32", use_last_error=True)
        self.k.CreateJobObjectW.argtypes, self.k.CreateJobObjectW.restype = [ctypes.c_void_p, w.LPCWSTR], w.HANDLE
        self.k.SetInformationJobObject.argtypes = [w.HANDLE, ctypes.c_int, ctypes.c_void_p, w.DWORD]
        self.k.AssignProcessToJobObject.argtypes = [w.HANDLE, w.HANDLE]
        self.k.TerminateJobObject.argtypes = [w.HANDLE, w.UINT]
        self.k.CloseHandle.argtypes = [w.HANDLE]
        self.handle = self.k.CreateJobObjectW(None, None)
        info = Extended()
        info.basic.flags = 0x2000  # JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
        if not self.handle or not self.k.SetInformationJobObject(self.handle, 9, ctypes.byref(info), ctypes.sizeof(info)):
            self.close()
            raise OSError(ctypes.get_last_error(), "cannot establish owned media job")

    def attach_and_resume(self, proc) -> None:
        if not self.handle:
            return
        if not self.k.AssignProcessToJobObject(self.handle, w.HANDLE(int(proc._handle))):
            proc.kill()
            proc.wait(timeout=5)
            raise OSError(ctypes.get_last_error(), "cannot assign suspended media child to owned job")
        class ThreadEntry(ctypes.Structure):
            _fields_ = [("size", w.DWORD), ("usage", w.DWORD), ("tid", w.DWORD), ("pid", w.DWORD),
                        ("base", w.LONG), ("delta", w.LONG), ("flags", w.DWORD)]
        self.k.CreateToolhelp32Snapshot.argtypes, self.k.CreateToolhelp32Snapshot.restype = [w.DWORD, w.DWORD], w.HANDLE
        self.k.Thread32First.argtypes = self.k.Thread32Next.argtypes = [w.HANDLE, ctypes.POINTER(ThreadEntry)]
        self.k.OpenThread.argtypes, self.k.OpenThread.restype = [w.DWORD, w.BOOL, w.DWORD], w.HANDLE
        self.k.ResumeThread.argtypes, self.k.ResumeThread.restype = [w.HANDLE], w.DWORD
        snapshot = self.k.CreateToolhelp32Snapshot(4, 0)
        if snapshot == ctypes.c_void_p(-1).value:
            raise OSError(ctypes.get_last_error(), "cannot enumerate owned primary thread")
        try:
            entry = ThreadEntry()
            entry.size = ctypes.sizeof(entry)
            more = self.k.Thread32First(snapshot, ctypes.byref(entry))
            while more:
                if entry.pid == proc.pid:
                    thread = self.k.OpenThread(2, False, entry.tid)
                    if thread:
                        try:
                            if self.k.ResumeThread(thread) != 0xFFFFFFFF:
                                return
                        finally:
                            self.k.CloseHandle(thread)
                more = self.k.Thread32Next(snapshot, ctypes.byref(entry))
            raise OSError("owned primary thread could not be resumed")
        finally:
            self.k.CloseHandle(snapshot)

    def terminate(self):
        if self.handle and not self.k.TerminateJobObject(self.handle, 1):
            raise OSError(ctypes.get_last_error(), "owned job termination failed")

    def close(self):
        if self.handle:
            import time
            self.k.QueryInformationJobObject.argtypes = [w.HANDLE, ctypes.c_int, ctypes.c_void_p, w.DWORD, ctypes.c_void_p]
            accounting = ctypes.create_string_buffer(48)
            try:
                if not self.k.QueryInformationJobObject(self.handle, 1, accounting, 48, None):
                    raise OSError(ctypes.get_last_error(), "cannot verify owned job drainage")
                if int.from_bytes(accounting.raw[40:44], "little"):
                    self.terminate()
                    deadline = time.monotonic() + 10
                    while True:
                        if not self.k.QueryInformationJobObject(self.handle, 1, accounting, 48, None):
                            raise OSError(ctypes.get_last_error(), "cannot verify owned job drainage")
                        if not int.from_bytes(accounting.raw[40:44], "little"):
                            break
                        if time.monotonic() >= deadline:
                            raise OSError("owned job tree did not drain")
                        time.sleep(0.02)
            finally:
                # KILL_ON_JOB_CLOSE remains the final containment even if the
                # query fails. The error must still reach cleanup_blocked.
                self.k.CloseHandle(self.handle)
                self.handle = None

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        self.close()
