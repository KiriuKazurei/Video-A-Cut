"""RAW image decoder tests and a deterministic Bayer DNG fixture (no user media)."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import struct
import sys
import tempfile
import unittest

from vac_worker.raw_image import process, settings


def write_dng(path):
    width, height = 64, 48
    def shorts(*values): return struct.pack("<" + "H" * len(values), *values)
    def longs(*values): return struct.pack("<" + "I" * len(values), *values)
    def rationals(values, signed=False):
        return b"".join(struct.pack("<ii" if signed else "<II", int(v * 10000), 10000) for v in values)
    tags = [
        (256,4,1,longs(width)), (257,4,1,longs(height)), (258,3,1,shorts(16)),
        (259,3,1,shorts(1)), (262,3,1,shorts(32803)), (271,2,4,b"VAC\0"),
        (272,2,9,b"Test DNG\0"), (273,4,1,longs(0)), (274,3,1,shorts(1)),
        (277,3,1,shorts(1)), (278,4,1,longs(height)), (279,4,1,longs(width*height*2)),
        (284,3,1,shorts(1)), (33421,3,2,shorts(2,2)), (33422,1,4,bytes([0,1,1,2])),
        (50706,1,4,bytes([1,4,0,0])), (50707,1,4,bytes([1,1,0,0])),
        (50708,2,9,b"VAC Test\0"), (50710,1,3,bytes([0,1,2])), (50711,3,1,shorts(1)),
        (50713,3,2,shorts(1,1)), (50714,5,1,rationals([0])), (50717,4,1,longs(16383)),
        (50718,5,2,rationals([1,1])), (50719,4,2,longs(0,0)), (50720,4,2,longs(width,height)),
        (50721,10,9,rationals([1,0,0,0,1,0,0,0,1],True)),
        (50728,5,3,rationals([1,1,1])), (50778,3,1,shorts(21)),
    ]
    tags.sort()
    offset=8+2+12*len(tags)+4
    extra=bytearray()
    entries=[]
    for tag,kind,count,value in tags:
        if len(value)>4:
            entries.append([tag,kind,count,longs(offset+len(extra))]);extra.extend(value)
            if len(extra)%2:extra.append(0)
        else:entries.append([tag,kind,count,value.ljust(4,b"\0")])
    pixel_offset=offset+len(extra)
    for entry in entries:
        if entry[0]==273:entry[3]=longs(pixel_offset)
    data=b"II"+shorts(42)+longs(8)+shorts(len(tags))+b"".join(struct.pack("<HHI",*e[:3])+e[3] for e in entries)+longs(0)+extra
    pixels=[]
    for y in range(height):
        for x in range(width):
            base=[2600,2100,2100,1100][(y%2)*2+x%2]
            pixels.append(base+((x//8)%4)*300)
    Path(path).write_bytes(data+shorts(*pixels))


@unittest.skipUnless(importlib.util.find_spec("rawpy") and importlib.util.find_spec("PIL"), "install requirements-media.txt for local RAW tests")
class RawImageTests(unittest.TestCase):
    def test_decode_adjust_and_preserve_sensor_source(self):
        from PIL import Image
        import numpy as np
        with tempfile.TemporaryDirectory(dir=os.environ.get("VAC_TEST_OUTPUT")) as directory:
            source=Path(directory)/"sensor.dng";write_dng(source)
            digest=hashlib.sha256(source.read_bytes()).hexdigest()
            info=process({"operation":"inspect","path":str(source)})
            self.assertEqual((info["width"],info["height"]),(64,48))
            recipe={"exposure":0,"white_balance":"camera","contrast":1,"saturation":1}
            def develop(name, **change):
                output=Path(directory)/(name+".png")
                process({"operation":"develop","path":str(source),"output":str(output),"settings":{**recipe,**change}})
                return np.asarray(Image.open(output)).astype(float)
            original=develop("default")
            brighter=develop("exposure",exposure=1)
            self.assertGreater(brighter.mean(),original.mean()+10)
            monochrome=develop("gray",saturation=0)
            self.assertLess(np.abs(monochrome[:,:,0]-monochrome[:,:,1]).mean(),1)
            contrast=develop("contrast",contrast=1.5)
            self.assertFalse(np.array_equal(original,contrast))
            self.assertEqual(digest,hashlib.sha256(source.read_bytes()).hexdigest())
            with self.assertRaises(ValueError):process({"operation":"develop","path":str(source),"output":str(source),"settings":recipe})

    @unittest.skipUnless(os.environ.get("VAC_ARW_FIXTURE"), "explicit checked Sony ARW sample required")
    def test_real_sony_arw(self):
        from PIL import Image
        import numpy as np
        source=Path(os.environ["VAC_ARW_FIXTURE"])
        before=hashlib.sha256(source.read_bytes()).hexdigest()
        with tempfile.TemporaryDirectory(dir=os.environ.get("VAC_TEST_OUTPUT")) as directory:
            info=process({"operation":"inspect","path":str(source)})
            output=Path(directory)/"sony.png"
            recipe={"exposure":0,"white_balance":"camera","contrast":1,"saturation":1}
            process({"operation":"develop","path":str(source),"output":str(output),"settings":recipe})
            with Image.open(output) as image:
                self.assertEqual(image.size,(info["width"],info["height"]))
                self.assertGreater(image.width,5000)
                mean=np.asarray(image).mean()
            recipe["exposure"]=1
            process({"operation":"develop","path":str(source),"output":str(output),"settings":recipe})
            with Image.open(output) as image:self.assertGreater(np.asarray(image).mean(),mean)
            record={"source_sha256":before,"metadata":info,"adjusted":True,"original_unchanged":before==hashlib.sha256(source.read_bytes()).hexdigest()}
            if os.environ.get("VAC_TEST_OUTPUT"):(Path(os.environ["VAC_TEST_OUTPUT"])/"sony-arw-verification.json").write_text(json.dumps(record,indent=2),encoding="utf-8")
        self.assertEqual(before,hashlib.sha256(source.read_bytes()).hexdigest())

    def test_corrupt_raw_and_invalid_recipes_are_rejected(self):
        recipe={"exposure":0,"white_balance":"camera","contrast":1,"saturation":1}
        for key,value in [("exposure",4),("contrast",0),("saturation",float("nan")),("white_balance","invalid")]:
            with self.assertRaises(ValueError):settings({**recipe,key:value})
        with tempfile.TemporaryDirectory(dir=os.environ.get("VAC_TEST_OUTPUT")) as directory:
            bad=Path(directory)/"invalid.arw";bad.write_bytes(b"not a RAW file")
            with self.assertRaises(Exception):process({"operation":"inspect","path":str(bad)})


if __name__ == "__main__":
    if len(sys.argv)==3 and sys.argv[1]=="--fixture":write_dng(sys.argv[2])
    else:unittest.main()
