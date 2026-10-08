"""python -m vac_worker --config worker.json [--once]"""
from __future__ import annotations

import argparse
import os
import signal
import sys
import threading
import time

from .mcp import Client
from .worker import load_config, run_loop


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(prog="vac_worker")
    parser.add_argument("--config", required=True)
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args(argv)

    def log(msg: str) -> None:
        print(f"[py-worker {time.strftime('%Y-%m-%dT%H:%M:%S')}] {msg}", flush=True)

    stop = threading.Event()
    watcher_done = threading.Event()
    stop_path = os.environ.get("VAC_SESSION_STOP", "")
    if stop_path and os.path.exists(stop_path):
        log("session was stopped before worker startup")
        return 0

    def watch_session_stop() -> None:
        while not watcher_done.wait(0.2):
            if os.path.exists(stop_path):
                log("session stop requested; no further tasks will be claimed")
                stop.set()
                return

    if stop_path:
        threading.Thread(target=watch_session_stop, daemon=True).start()
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: (log("signal received; stopping after current step"), stop.set()))
    try:
        cfg = load_config(args.config)
        timeout = 3.0 if cfg.role == "ingester" else 15.0
        client = Client(cfg.mcp_url, cfg.token, timeout=timeout)
        if cfg.role == "ingester":
            from .ingest import run_ingest_loop
            result = run_ingest_loop(client, cfg, log, stop, once=args.once)
        else:
            result = run_loop(client, cfg, log, stop, once=args.once)
    except ValueError as err:
        log(f"fatal: {err}")
        return 3 if str(err).startswith("config:") else 2
    except Exception as err:  # startup failure; task transport errors stay inside the ingester loop
        log(f"fatal: {err}")
        return 2
    finally:
        watcher_done.set()
    log(f"exit: {result['outcome']}")
    return 1 if result["outcome"] in ("failed", "fail_report_error") else 0


if __name__ == "__main__":
    sys.exit(main())
