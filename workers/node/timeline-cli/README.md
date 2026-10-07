# Timeline CLI

The Phase 1 CLI reads `edl.json`, probes local sources with FFprobe, validates
frame and audio boundaries, and exports populated OTIO/XML timelines, separate
48 kHz WAV stems, SRT, and copied source video. FFmpeg and FFprobe must be on
`PATH`. Integer project frame rates are supported; VFR and decimal project
frame rates fail explicitly.

Run `npm test` and `npm run check`. For a reproducible synthetic acceptance
sample, run `node scripts/generate-media.mjs <new-media-dir>`, then:

```sh
node src/cli.mjs build --edl <new-media-dir>/edl.json --output <new-delivery-dir> --adapters adapters/index.mjs
```

To render an MP4 preview from an exported package:

```sh
node src/cli.mjs render-preview --package <new-delivery-dir> --output <preview-dir> [--preset mp4-720p-v1]
```

For a separate ASS bundle retaining the exact original SRT and declared media:
`node src/cli.mjs build-ass --package <exported-dir> --output <new-ass-dir>`.
This uses the fixed `ass-basic-v1` style and does not register a Go task;
use Export → Delivery in WebUI for the managed workflow.

The output is ready for import **validation**, not human acceptance. Import
`edit.xml` in Premiere or `timeline.otio` in Resolve and check the cut points,
audio stems, subtitle file, and media relinking. The implementation brief is
`docs/一阶段开发文档.md` at the repository root.
