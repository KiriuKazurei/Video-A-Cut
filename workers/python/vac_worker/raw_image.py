"""Local camera RAW inspect/develop. No downloads, source writes or provider calls."""
import json
import math
import os
from pathlib import Path
import sys


def settings(value):
    value = value or {"exposure": 0, "white_balance": "camera", "contrast": 1, "saturation": 1}
    if not isinstance(value, dict):
        raise ValueError("Invalid RAW recipe")
    for key, low, high in [("exposure", -2, 3), ("contrast", .5, 2), ("saturation", 0, 2)]:
        number = value.get(key)
        if isinstance(number, bool) or not isinstance(number, (float, int)) or not math.isfinite(number) or not low <= number <= high:
            raise ValueError("Invalid RAW parameter")
    if value.get("white_balance") not in ("camera", "auto", "daylight"):
        raise ValueError("Invalid RAW white balance")
    return value


def process(request):
    import rawpy
    import numpy as np
    from PIL import Image
    path = Path(request["path"])
    if not path.is_file():
        raise ValueError("Missing RAW source")
    before = path.stat()
    with rawpy.imread(str(path)) as raw:
        sizes = raw.sizes
        width, height = sizes.width, sizes.height
        if sizes.flip in (5, 6):
            width, height = height, width
        if width <= 0 or height <= 0 or width * height > 150_000_000:
            raise ValueError("RAW image exceeds 150 megapixels")
        data = {"width": width, "height": height, "codec_name": "camera_raw", "pix_fmt": "rgb24", "raw_decoder": "rawpy " + rawpy.__version__, "libraw_version": ".".join(map(str, rawpy.libraw_version)), "sensor_width": sizes.raw_width, "sensor_height": sizes.raw_height}
        if request["operation"] == "develop":
            recipe = settings(request.get("settings"))
            output = Path(request["output"])
            if output.resolve() == path.resolve() or output.suffix.lower() != ".png":
                raise ValueError("Invalid derived image target")
            rgb = raw.postprocess(use_camera_wb=recipe["white_balance"] == "camera", use_auto_wb=recipe["white_balance"] == "auto", no_auto_bright=True, exp_shift=2 ** recipe["exposure"], output_color=rawpy.ColorSpace.sRGB, output_bps=8)
            pixels = rgb.astype(np.float32) / 255
            luma = (pixels * np.array([.2126, .7152, .0722], dtype=np.float32)).sum(axis=2, keepdims=True)
            pixels = luma + (pixels - luma) * recipe["saturation"]
            pixels = (pixels - .5) * recipe["contrast"] + .5
            image = Image.fromarray(np.rint(np.clip(pixels, 0, 1) * 255).astype(np.uint8))
            image.save(output, format="PNG")
            data.update(width=image.width, height=image.height)
        elif request["operation"] != "inspect":
            raise ValueError("Unknown RAW operation")
    after = path.stat()
    if (before.st_size, before.st_mtime_ns) != (after.st_size, after.st_mtime_ns):
        raise ValueError("RAW source changed")
    return data


def main():
    try:
        request = json.loads(sys.stdin.buffer.read(1_048_577))
        result = process(request)
        print(json.dumps({"ok": True, "data": result}))
        return 0
    except Exception as error:
        # Paths and proprietary decoder diagnostics are not sent to clients.
        print(json.dumps({"ok": False, "error_type": type(error).__name__}))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
