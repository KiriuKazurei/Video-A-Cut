# Export / Preview / Delivery Variant Worker

核对日期：2026-10-16。通过 MCP 执行 export、preview、delivery_variant，不打开 SQLite，不在请求中冒充其他 agent/role。通过 `node src/main.mjs --config <worker.json> [--once]` 启动，token_env 只引用环境变量名。

配置需包含 mcp_url、delivery_root、cli_path、adapters_path；后三个路径为本机绝对路径。可配置 profile_sha256、output_prefix、心跳/轮询间隔及 task_timeout_ms。delivery_root 与 Go 一致，执行前检验根内路径和重解析边界。`--once` 处理至多一项，退出码为 0 成功/空闲、1 任务失败、2 配置/传输错误。

| 类型 | 入口及约束 |
|---|---|
| export | 固定任务输入、brief 摘要与版本检查 → timeline CLI build → submit_delivery；Go 重验包和归属 |
| preview | 独立 preview_capability，固定输入/预设摘要及执行 scope → FFmpeg 渲染或恢复登记 → submit_preview |
| delivery_variant | 独立 delivery_variant_capability，ass-basic-v1/v2 固定输入 → 派生 ASS 包 → submit_delivery_variant |

预览 720p 是最大画布、不放大小素材。有 SRT 的预览需要本机字体；ASS 样式预览使用冻结的 schema 2 输入。ASS 文件生成本身不要求字体安装，但实际烧录/显示需要字体证据。参见 [MP4 协议](../../../contracts/mp4-preview/v1/README.md) 和 [ASS 协议](../../../contracts/delivery-variant/v1/README.md)。

取消、租约丢失或输入失效时终止拥有的子进程并避免晚提交。产物已发布但登记结果未知时查询状态、有限重试并保留包；不能把未知登记当作失败重跑后覆盖。具体错误、回执和源哈希仍由 Go 独立验证。

Windows 单元/集成测试经 [统一入口](../../../scripts/TESTING.md)，custom 指向本目录并运行 `node --test`；静态检查为 npm run check。52 项导出器测试已通过；brief 哈希负向测试还以移除校验的变异验证其有效性。模型与人工验收不由 Worker 的成功状态替代。
