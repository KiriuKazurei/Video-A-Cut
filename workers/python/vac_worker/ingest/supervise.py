"""Local execution lease, owned-process stop, and cross-process locks.

The supervisor only signals PIDs it registered. It never matches python or
ffmpeg by image name. Waits are interruptible. Retry sleeps do not hold the
in-flight slot, so a heartbeat can run during a submit backoff.
"""
from __future__ import annotations

import json
import os
import random
import subprocess
import threading
import time
import uuid
from pathlib import Path

from ..mcp import TransportError, transport_kind

COOPERATIVE_SECONDS = 5.0
FORCE_WAIT_SECONDS = 10.0
RETRY_BUDGET_SECONDS = 20.0
STOP_MARGIN_SECONDS = COOPERATIVE_SECONDS + FORCE_WAIT_SECONDS
BACKOFF_SECONDS = (0.5, 1.0, 2.0, 4.0, 8.0)
MAX_INFLIGHT = 2
SAFETY_MARGIN_SECONDS = 1.0

LOST_EXECUTION = frozenset({"stale_execution", "lease_expired", "cancelled", "execution_revoked"})


class CleanupBlocked(Exception):
    """Owned processes or files could not be reclaimed. New executions must not start."""


class Halt:
    def __init__(self):
        self.event = threading.Event()
        self.reason = ""

    def stop(self, reason: str) -> None:
        if not self.reason:
            self.reason = reason
        self.event.set()

    def cancelled(self) -> bool:
        return self.event.is_set()


def wait_interruptible(event: threading.Event, seconds: float) -> bool:
    """Sleep up to ``seconds``. Return True when ``event`` is set."""
    if seconds <= 0:
        return event.is_set()
    return event.wait(seconds)


def backoff_delay(attempt: int, rng=None) -> float:
    base = BACKOFF_SECONDS[min(max(attempt, 0), len(BACKOFF_SECONDS) - 1)]
    rng = rng or random
    span = base * 0.1
    return min(8.0, max(0.0, base + rng.uniform(-span, span)))


class LocalLease:
    """Conservative deadline on a monotonic clock, minus RTT and a safety margin."""

    def __init__(self):
        self.deadline: float | None = None
        self.suspended = False

    def observe(self, remaining_seconds: float, rtt_seconds: float) -> None:
        usable = float(remaining_seconds) - max(0.0, float(rtt_seconds)) - SAFETY_MARGIN_SECONDS
        self.deadline = time.monotonic() + max(0.0, usable)

    def observe_expires(self, expires_at: str, rtt_seconds: float) -> None:
        """Turn a server expiry into a monotonic deadline. Wall clock is used only for the duration."""
        from datetime import datetime, timezone
        text = str(expires_at).strip().replace("Z", "+00:00")
        expiry = datetime.fromisoformat(text)
        if expiry.tzinfo is None:
            expiry = expiry.replace(tzinfo=timezone.utc)
        remaining = (expiry - datetime.now(timezone.utc)).total_seconds()
        self.observe(remaining, rtt_seconds)

    def expired(self) -> bool:
        return self.deadline is not None and time.monotonic() >= self.deadline

    def retry_until(self, started: float | None = None) -> float:
        budget = (started if started is not None else time.monotonic()) + RETRY_BUDGET_SECONDS
        if self.deadline is None:
            return budget
        return min(budget, self.deadline - STOP_MARGIN_SECONDS)


def pid_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    if os.name == "nt":
        import ctypes
        handle = ctypes.windll.kernel32.OpenProcess(0x1000, False, int(pid))
        if not handle:
            return False
        try:
            code = ctypes.c_ulong()
            if not ctypes.windll.kernel32.GetExitCodeProcess(handle, ctypes.byref(code)):
                return False
            return code.value == 259  # STILL_ACTIVE
        finally:
            ctypes.windll.kernel32.CloseHandle(handle)
    try:
        os.kill(pid, 0)
    except OSError:
        return False
    else:
        return True


def taskkill_tree(pid: int) -> None:
    """End one registered process tree. The PID is required; image names are not accepted."""
    if not isinstance(pid, int) or pid <= 0:
        raise CleanupBlocked("refusing to signal a process without a registered pid")
    if os.name == "nt":
        subprocess.run(
            ["taskkill", "/T", "/F", "/PID", str(pid)],
            capture_output=True,
            creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
        )
    else:
        try:
            os.kill(pid, 15)
        except OSError:
            pass


from .isolation import ProcessRegistry, PhysicalLock


class FlightGate:
    """Caps concurrent HTTP calls. The slot is not held while backing off."""

    def __init__(self, limit: int = MAX_INFLIGHT):
        self._sem = threading.BoundedSemaphore(limit)

    def acquire(self, halt: Halt, timeout: float) -> bool:
        deadline = time.monotonic() + timeout
        while True:
            if halt.cancelled():
                return False
            if self._sem.acquire(blocking=False):
                return True
            if time.monotonic() >= deadline:
                return False
            wait_interruptible(halt.event, min(0.05, deadline - time.monotonic()))

    def release(self) -> None:
        self._sem.release()


def retry_call(client, tool: str, args: dict, halt: Halt, lease: LocalLease | None, log, gate: FlightGate | None = None):
    """Retry a transient transport error with the same arguments until the budget or the lease margin."""
    started = time.monotonic()
    attempt = 0
    while True:
        if halt.cancelled():
            from .common import Cancelled
            raise Cancelled(halt.reason or "cancelled")
        deadline = lease.retry_until(started) if lease is not None else started + RETRY_BUDGET_SECONDS
        if time.monotonic() >= deadline:
            raise TransportError("retry budget exhausted")
        held = False
        try:
            if gate is not None:
                if not gate.acquire(halt, max(0.1, deadline - time.monotonic())):
                    from .common import Cancelled
                    raise Cancelled(halt.reason or "cancelled")
                held = True
            started_rtt = time.monotonic()
            result = client.call(tool, args)
            return result, time.monotonic() - started_rtt
        except TransportError as err:
            if transport_kind(err) != "transient":
                raise
            delay = backoff_delay(attempt)
            attempt += 1
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise
            log(f"{tool} transient error, retrying same request: {err}")
            if wait_interruptible(halt.event, min(delay, remaining)):
                from .common import Cancelled
                raise Cancelled(halt.reason or "cancelled")
        finally:
            if held and gate is not None:
                gate.release()
