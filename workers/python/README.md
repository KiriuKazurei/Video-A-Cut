# Python 流水线 Worker

领取并执行 `recognize / sort / narrate / tts / subtitle / mix` 六类任务的 Worker。媒体处理依赖本机 ffmpeg / ffprobe，中文语音使用 Windows SAPI。识别与解说通过 `ContentProvider` 接入：已实现 `builtin` 元数据/模板模式、`vision` 画面识别和 `narration` 解说适配器，并支持 OpenAI Chat Completions、Anthropic Messages、Google Gemini 请求格式。实际模型兼容性与游戏内容效果仍需验收。

只通过 MCP（`/mcp`）与控制面通信：不打开 SQLite，不自报 agent ID 或角色。

## 运行

```sh
VAC_WORKER_TOKEN=<明文 bearer token> python -m vac_worker --config worker.json [--once]
```

`--once` 最多处理一个任务后退出：0 为成功或空闲，1 为已上报失败，2 为配置或连接错误。不带则轮询到 SIGINT/SIGTERM。

`worker.json`：

```json
{
  "mcp_url": "http://127.0.0.1:8787/mcp",
  "token_env": "VAC_WORKER_TOKEN",
  "delivery_root": "E:\...\root",
  "output_prefix": "stages",
  "poll_interval_ms": 2000,
  "heartbeat_interval_ms": 5000,
  "task_timeout_ms": 300000,
  "ffmpeg": "ffmpeg",
  "ffprobe": "ffprobe",
  "content_provider": "builtin"
}
```

`delivery_root` 必须是控制面 `delivery_root` 的同一目录；`heartbeat_interval_ms` 必须小于 `lease_seconds`。ffmpeg/ffprobe 需在 PATH 上。

`content_provider` 默认 `builtin`，其标签标记 `metadata_only`，旁白为“第 N 段”模板，用于本地闭环与诊断，不执行真实画面识别或 LLM 推理。指定未知 provider 会在启动时失败，模型请求失败也不会暗中改用模板。

实际模型使用 `vision`（识别）或 `narration`（解说），在 `content_provider_config` 中配置 `endpoint`、`model`、凭证环境变量名 `token_env` 和 `allow_external`。可选 `api_format` 为 `openai`、`anthropic` 或 `gemini`；省略时使用原有自定义 `/vision`、`/narration` JSON 契约。凭证由运行环境提供，预设不保存凭证值。

固定预设流程推荐使用仓库根目录的 [Start-LocalPipeline.ps1](../../scripts/Start-LocalPipeline.ps1)，由选定 profile 生成各角色的匹配 Worker 配置，并按具体预设版本记录媒体外发许可。配置示例见 [configs/providers](../../configs/providers)，协议、连接测试与运行说明见 [服务商接入报告](../../docs/服务商通用接入开发报告.md)。取样证据和内容审查契约见 [四阶段开发文档](../../docs/四阶段开发文档.md)。

`tts` 在 Windows 上使用本机中文 SAPI 语音。若语音不可用或合成失败，任务失败，不会用测试音冒充旁白。旁白窗口是最长可用时长；超长语音拒绝交付，实际语音长度回填 EDL，字幕与音乐压低跟随实际长度。单元测试可显式启用测试音；真实交付不启用。

真实录屏的 Premiere 验收样包可用 `scripts/build_real_media_acceptance.py --source <录屏绝对路径> --output <全新目录>` 生成；使用 `scripts/verify_premiere_delivery.py --delivery <输出目录>/premiere-delivery --report <报告路径>` 检查帧、采样、语音位置和压低增益。人工导入验收仍需在 Premiere 中完成。

## 流程

`claim_task` → `report_progress` → `get_asset` / `get_asset_edl` → 阶段计算，产出写到临时目录 → 原子改名为 `<output_prefix>/<asset_id>/<task_id>` → `submit_delivery`。控制面用与人工导入相同的规则复验包内容，替换资产产物表并置对应状态。

子进程带硬超时；中途失约会清理临时目录。阶段失败调用 `fail_task` 上报；租约丢失则放弃不回报。

## 测试

在 `workers/python` 目录执行完整测试，包括导入恢复测试：

```sh
python -m unittest discover -s tests -t . -v
```

兼容不指定顶层目录的入口 `python -m unittest discover -s tests -v`，也可使用 `python -m unittest tests.test_ingest_recovery -v` 单独复验恢复逻辑。
