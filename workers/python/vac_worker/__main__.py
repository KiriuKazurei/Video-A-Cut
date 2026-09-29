"""python -m vac_worker --config worker.json [--once]"""
from __future__ import annotations

import argparse
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
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: (log("signal received; stopping after current step"), stop.set()))
    try:
        cfg = load_config(args.config)
        client = Client(cfg.mcp_url, cfg.token)
        result = run_loop(client, cfg, log, stop, once=args.once)
    except Exception as err:  # startup/transport failure
        log(f"fatal: {err}")
        return 2
    log(f"exit: {result['outcome']}")
    return 1 if result["outcome"] in ("failed", "fail_report_error") else 0


if __name__ == "__main__":
    sys.exit(main())
