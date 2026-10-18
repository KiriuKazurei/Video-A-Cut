# Timeline CLI

核对日期：2026-10-16。读取 EDL，以 ffprobe/FFmpeg 校验切点、帧与采样边界，生成 XML/OTIO、48 kHz 分轨 WAV、SRT 和自包含声明媒体；也提供 MP4 与 ASS 渲染/派生。工具须预装在 PATH。

从本目录执行以下本地文件入口，输出必须为全新目录；它们不注册 Go 任务：

```text
node src/cli.mjs validate --edl <edl.json>
node src/cli.mjs build --edl <edl.json> --output <new-dir> --adapters adapters/index.mjs
node src/cli.mjs render-preview --package <exported-dir> --output <new-dir> --preset mp4-720p-v1
node src/cli.mjs build-ass --package <exported-dir> --output <new-dir>
```

使用前以 cli.mjs 的 usage 确认选项。项目时间线支持正整数 FPS；VFR/小数时间基按既有校验处理，不许把任意输入默认为已支持。720p 预设的上限为 1280×720，禁止放大小素材；有字幕时记录实际字体摘要。schema 2 预览烧录冻结 ASS，原 SRT 预览语义保留。协议见 [MP4](../../../contracts/mp4-preview/v1/README.md)、[ASS](../../../contracts/delivery-variant/v1/README.md)。

Windows 测试经 [统一入口](../../../scripts/TESTING.md)，custom 工作目录指向本目录并运行 node --test。固定媒体保留在 fixtures/media；原根工作区已完成迁移，当前候选还未合入整个测试目录改造，旧位置的跟踪样本不得删除。generate-media.mjs 生成物也必须在受管理输出目录。npm run test:managed 只适用于已经包含该脚本的工作区，不是当前候选的通用命令。

52 项 Timeline 测试含真实 FFmpeg、字体目录和中文字幕渲染已通过。产物只表示可进入导入验证，Premiere/Resolve 的切点、分轨、字幕、重链接及业务质量仍须人工验收；不把 CLI 成功记为人工通过。
