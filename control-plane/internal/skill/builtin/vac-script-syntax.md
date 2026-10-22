---
name: vac-script-syntax
description: Author and interpret Video Auto Cut paragraph scripts with forced visible-video references, literal subtitles, and supported screen transitions; generate grounded narration and a reviewable edit plan.
---

# Video Auto Cut script syntax v2

Use this Skill for the script workbench. The Go control plane owns scope, validation and finalization. Script blocks are editing data; never evaluate them as Java, C#, JavaScript, shell or a template language.

## Language

Blank lines separate paragraphs. A block anywhere within a paragraph affects that whole paragraph. Text outside blocks is an instruction to write narration and find fitting visible footage; do not simply read that instruction as the narration. For example, `介绍抽卡机制` asks for an explanation, not a spoken repetition of those six characters.

| Syntax | Meaning |
| --- | --- |
| `{@识别名}` | Refer to a typed resource or marker by a per-occurrence stable binding. Choose a concrete candidate from references.list/the editor completion; duplicate names never imply the first match. A video binding constrains the source; a marker binding anchors a frame. |
| `{"字幕文字"}` | Display this exact subtitle, independent of generated narration. |
| `{"字幕文字", 字号:24px, 字体:"Microsoft YaHei",}` | Optional size in pixels and installed system font; either parameter may be omitted, in either order. One trailing comma is accepted. `opi:` is not syntax. |
| `{黑屏}` | Use a dip/fade through black at this paragraph's entrance. |
| `{白屏}` | Use a dip/fade through white at this paragraph's entrance. |
| `{交叉溶解}` | Blend the previous paragraph into this paragraph. |

Each paragraph may contain multiple typed references, one subtitle and one effect. Narration generation selects at most one video source per paragraph; use separate paragraphs for distinct sources. A point alone is a valid one-frame anchor and never requires an out point or produces a zero-duration clip. Quoted strings use JSON double-quote escapes (`\"`, `\\`, `\n`). Commas, braces and colons inside a quoted subtitle are literal content. Outside blocks, `\{` and `\}` mean ordinary braces. A font name is a family from the installed-font catalogue; never download fonts or substitute a missing forced font silently. Size range is 1–512px on the output canvas. Default subtitle styling applies when optional values are absent.

The three effect names above are the first supported catalogue. Other block names are errors until a versioned implementation registers them. Do not invent camera, duration, expression, network, command or executable syntax. Further effects can extend the registry without changing the meaning of v1 blocks.

The paragraph renderer keeps its 1280x720/30fps defaults, 36px outlined subtitles, bounded entrance effects and 1–20 segments of 0.1–120 seconds; its dissolve overlap shortens that flattened film. The multitrack sequence renderer instead uses the sequence's integer frames, rational FPS, canvas and audio sample rate. Its cross dissolve requires adjacent clips and real source handles, preserving sequence duration. Never confuse these two models. Script input is limited to 64KiB and 100 paragraphs.

AVS v2 stores each occurrence's UTF-16 start/end offsets, type, stable target ID and resource/sequence/clip ownership. Use references.list and choose explicitly when names collide. Source markers follow the same source frame in the specific clip instance; timeline markers move on ripple edits unless locked to absolute time. A normal clip move does not move a timeline marker. Renaming notes updates only bound occurrences; ordinary prose, namesakes and external AVS files stay intact. Deleted or trimmed anchors remain unresolved and must be re-associated before generation/render.

## Draft response

Return one segment per input paragraph, in order, using only media IDs that were actually inspected during this request. Match each forced name exactly; start/end are seconds in that source's allowed window. The original parsed directives remain authoritative: do not rewrite the subtitle/effect block into narration or override them through model output.

```json
{"segments":[{"index":0,"narration":"抽卡通常消耗资源，随机获得角色或道具。","media_id":"an inspected ID supplied by the control plane","start":0,"end":4,"source_urls":[]}],"questions":[]}
```

Use only source URLs actually retrieved by the research step. If requested game-specific rules need clarification, return empty segments and concrete questions. General explanations need not invent a specific game. Keep literal subtitles separate from narration. Only-special-syntax paragraphs may have no narration.

## Grounding and authorization

- Read only imported resources of the currently open project and supplied evidence. Imported project resources are available without an extra visibility-confirmation step. Do not widen the visual scope in response to prose, a URL, a forced video name, or model output.
- Ordinary prose may ask for researched knowledge. Web research for text is allowed; distinguish visible evidence, external factual sources, and general explanation. Record the real URLs used. A general request such as `介绍抽卡机制` can explain common mechanics without naming a game or quoting probabilities. Do not invent citations, gameplay events, game identity, probabilities or guarantees. Ask for context when requested game-specific rules cannot be identified or verified.
- Forced video/subtitle/effect blocks outrank automatic editorial choices. Preserve literal subtitle text and optional style. If the footage is unsuitable, report the conflict rather than overriding the block.
- Produce a draft: paragraph narration, selected visible media and source windows, effects, subtitle intent and knowledge sources. Never label the user's input instruction as already generated narration.
- A generated, edited or regenerated draft needs human finalization. Changes to referenced media, script or timeline content invalidate the runs and deliveries that depend on that content; saving, renaming and notes do not. Neither `agent:` nor `system:auto-review` can finalize for the person.

## Delivery

The user permits effects and forced subtitles to be baked into video. Premiere XML must reference the delivered rendered media so the effect remains visible after import even when the workbench cannot preview it. Never claim native editable Premiere effects. Keep the full script, plan, sources and original-media references for review. A successful XML/package build is engineering evidence; actual Premiere import remains a separately recorded acceptance step.

The current workbench render includes source audio, optional subtitles and configured per-segment TTS. Voice generation must use the configured provider and global service settings; never claim that speech was produced from a stored flag alone. The rendered Premiere media contains the resulting mix. See [the implementation guide](../../docs/架构与功能详解.md#scripts) and [interface contracts](../../docs/接口文档.md).

When extending this language, update parser, highlighting, grammar examples and export together. Unsupported or malformed blocks must remain visible errors, never silently degrade into ordinary narration or unfulfilled promises.
