# Delivery variant protocol v1/v2 — ASS

核对日期：2026-10-16。当前运行和验收边界见 [当前状态](../../../docs/当前项目状态.md)。

ASS is a separate `delivery_variant` Task. The original export package, approved text, asset artifacts, workflow revision and existing MP4 preset stay immutable. `ass-basic-v1` remains compatible; `ass-basic-v2` adds editable style/time snapshots. FCPXML is a later extension.

## Editable style extension (2026-10-07)

Shared types: `contracts.ts`; browser/Node defaults, inheritance, canonical JSON and validation: `style.mjs`. GET/PUT `/api/workflows/{run}/subtitle-style` return/save `SubtitleStyleDraft`. PUT carries `revision_id`, `expected_draft_version`, and `document`; stale versions/source bindings return 409. GET `/api/subtitle-fonts` returns active exporter family/hash metadata, never absolute paths.

`ass-basic-v2` creation also carries `draft_version` and `style_sha256`. Go freezes `style_document`/`style_sha256` in input `schema_version=2`. `spec`/`spec_sha256` still identify the fixed v2 contract, not the custom style. Worker heartbeat protocol remains 1; the `specs` array advertises supported v1/v2 contracts and optional `fonts:[{family,sha256}]`. Tasks are matched to their own spec.

The document contains source SRT hash, script dimensions, global style, and cue overrides. Timing is integer milliseconds, quantized half-up to centiseconds; fades must fit the quantized duration. Cue IDs are 1-based source SRT ordinals. Body text is immutable. `play_res_y=720`; width follows the existing preview canvas aspect ratio. Outline/shadow accept at most two decimal places.

SHA-256 over `canonicalJSON(document)` is the style identity: sorted object keys, literal Unicode/HTML, array order retained, no final newline. `overrides` is an array, never null; absent optional override fields are omitted. `ass-style.json` uses those exact canonical bytes. V2 manifest/receipt/report binding uses schema version 2; converter version is `vac.ass-basic-v2/1`. V1 output bytes stay unchanged.

The v2 input fingerprint retains the v1 atom algorithm and appends `ass-basic-v2`, `style_sha256` after the source files. The style hash includes source binding, resolution, timing and all custom styles. Go independently regenerates and checks ASS from the approved source and frozen document.

Current implementation: Go service validates frozen inputs, Node generates the package, and WebUI edits immutable style revisions. Font/rendering and human acceptance remain separate.

## Request and capability

`POST /api/workflows/{run}/delivery-variants` accepts `revision_id`, `expected_version`, `request_id`, `spec_id`; v2 additionally requires `draft_version` and `style_sha256`. A new task returns 202; a durable request or active identical input returns 200. New requests after a completed attempt create a new attempt. Reusing a request with a different revision/version/spec returns 409.

`GET /api/workflows/{run}/delivery-variants` returns readiness, spec hashes and task views. `GET /api/delivery-variants/{id}`, `POST .../{id}/cancel`, `GET .../{id}/files/{key}` and `GET .../{id}/delivery.zip` expose status, cancellation and registered downloads. Errors use the existing Go error mapping. Capability requires exporter heartbeat `delivery_variant_capability={protocol_version:1,ready:true,specs:[{spec_id,sha256}]}`; the Go expiry is 30 seconds. MP4 capability alone cannot claim ASS.

MCP `get_task_input` returns `input_kind=delivery_variant` and `delivery_variant` frozen input. `submit_delivery_variant({task_id,variant_dir})` is listed on the shared task surface but execution requires the exporter role and ownership of the task. Generic create/result/delivery paths cannot complete this task.

## Frozen input

Input contains schema/variant/task/asset/run/revision IDs, parent_package_ref, edl_sha256, parent_manifest_sha256, format, spec_id, spec_sha256, complete spec object, source_files and input_sha256. Source entries contain kind/path/sha256/size, relative to the parent package. Exactly one declared SRT is required. Empty SRT is valid. UTF-8 is required; metadata and subtitles are bounded to 1 MiB.

SHA-256 hashes concatenate these atoms, each encoded as `ASCII(UTF8 byte length) + ':' + UTF8 bytes`:

1. `vac.delivery-variant.input.v1`, `1`.
2. asset_id, run_id, revision_id, parent_package_ref.
3. edl_sha256, parent_manifest_sha256, format, spec_id, spec_sha256.
4. Decimal source file count.
5. Each source file sorted by UTF-8 path bytes: kind, path, sha256, decimal size.

Attempt IDs and request IDs are excluded. Request bindings are stored separately. Hashes are lowercase hex; paths reject traversal, redirection and case aliases. Sizes stay within JavaScript safe integer range. Spec SHA is over the exact UTF-8 LF file bytes, while the transported JSON object must equal the registered spec. The independent Python-generated [golden input](fixtures/input.json) is checked by both Go and Node; the embedded Go spec must match the contract bytes.

## `ass-basic-v1`

UTF-8 ASS v4.00+, 1280×720, Microsoft YaHei 36, white text, black outline 2, shadow 1, bottom center, left/right margins 64 and bottom margin 36, WrapStyle 0. Generation needs no installed font or FFmpeg; playback requires a renderer with the specified font or an explicit fallback. Fonts are never downloaded or embedded automatically.

Content comes from the exported SRT. Preserve original SRT bytes, including BOM and CRLF. Cue start/end are rounded to the nearest centisecond, ties upward, with at most 5 ms quantization. Empty text, collapsed duration, control characters, malformed indexes/times and cues outside the frozen video timeline are rejected. This version supports the project's existing positive integer FPS timeline contract.

Layout inserts deterministic hard breaks with a 56-unit line budget: each visible scalar counts 2, tab counts 4; U+0300–036F accents, variation selectors and ZWJ count 0. Original explicit line breaks are preserved, and no characters are removed. More than 8 resulting lines per cue or ASS/manifest over 1 MiB is rejected. This conservative layout does not require renderer Unicode automatic wrapping, which native ASS does not enable by default in FFmpeg ([official implementation](https://www.ffmpeg.org/doxygen/8.0/vf__subtitles_8c_source.html)). Complex-script shaping and other fonts/renderers still need visual acceptance.

Real line breaks become `\N`. Commas and Unicode remain literal. Braces become `\{` and `\}`. Each literal backslash is followed by U+2060 WORD JOINER, preventing the following `N/n/h` or tag text from becoming an ASS escape while retaining its visible spelling. Raw SRT remains available for exact text interchange. This policy is validated with FFmpeg/libass; compatibility with other renderers has not been accepted. ASS does not have a general double-backslash escape in libass.

See [Aegisub ASS tags](https://aegisub.org/docs/latest/ass_tags/), [libass parser](https://github.com/libass/libass/blob/master/libass/ass_parse.c) and [FFmpeg subtitles filters](https://ffmpeg.org/ffmpeg-filters.html#ass).

## Output and validation

`delivery-variants/{asset_id}/{variant_id}/` copies all frozen declared parent files, EDL and parent manifest; adds `subtitles.ass`, exact `ass-style.json`, `variant-report.json`, `variant-receipt.json`, `variant-manifest.json`. Publish uses a sibling staging directory and atomic new-directory rename. Existing output is never overwritten. Cancellation before publication removes staging; output published before registration can recover only with the same full identity and fingerprint.

Manifest, report and receipt bind schema/variant/task/asset/run/revision and input/spec hashes. Receipt converter_version is `vac.ass-basic-v1/1` for v1 and `vac.ass-basic-v2/1` for v2; schema/style binding follows the selected version. Manifest artifact entries have key/kind/path/mime/size/sha256; paths are relative to the new package. Required keys are ass, srt, style, report, receipt. Go verifies all copied source hashes, exact spec bytes, expected ASS bytes reconstructed independently from SRT, report duration/count, receipt binding, current revision, governance and lease. Required report checks are source_fingerprint, subtitle_text, subtitle_timing, ass_style and self_contained; a passed report cannot override failed validation.

ZIP streams registered artifacts plus the hashed variant manifest. Source copied into the variant is self-contained to the extent declared by the original export manifest; the ASS task does not re-author the EDL or audit unrelated parent format semantics. Historical downloads retain their revision and cannot become the current result. Locked/hidden exporter assets block downloads and generation.

CLI local file conversion: `node workers/node/timeline-cli/src/cli.mjs build-ass --package <export-package> --output <new-directory>`. It does not register a Go task. WebUI users generate and download in Export → Delivery → ASS style subtitles. Schema 1 preview input retains SRT rendering. Schema 2 preview input freezes the completed ASS variant/style and burns that exact ASS using the existing preview pipeline; this path is implemented. Both retain the 1280×720 maximum and no-upscale policy.
