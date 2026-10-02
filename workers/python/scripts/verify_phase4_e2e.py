"""Formal phase-4 acceptance entry.

This script does not substitute the built-in provider, a mock HTTP server, or
a test tone for the content acceptance. It runs only when the operator has
named a vision endpoint, a narration endpoint, and the recording to use.
Premiere judgment stays a human step and is not marked passed here.
"""
from __future__ import annotations

import os
import sys


REQUIRED = ("VAC_VISION_ENDPOINT", "VAC_NARRATION_ENDPOINT", "VAC_PHASE4_RECORDING")


def main() -> int:
    missing = [name for name in REQUIRED if not os.environ.get(name)]
    if missing:
        print("第四阶段正式验收未执行。缺少操作员提供的配置: " + ", ".join(missing))
        print("不要用 builtin、mock 或第三阶段 Premiere 记录代替本次内容验收。")
        return 2
    if os.environ.get("VAC_VISION_ALLOW_EXTERNAL") != "true" or os.environ.get("VAC_NARRATION_ALLOW_EXTERNAL") != "true":
        print("外发仍关闭。把 VAC_VISION_ALLOW_EXTERNAL 和 VAC_NARRATION_ALLOW_EXTERNAL 设为 true 才会发送画面。")
        return 2
    print("端点已配置，但本脚本不会自行上传录屏，也不会代替人工判断标签、排序和解说。")
    print("请用正式 Worker 的领取/心跳/提交跑完全链，再由人在 Premiere 核对交付包。")
    return 2


if __name__ == "__main__":
    sys.exit(main())
