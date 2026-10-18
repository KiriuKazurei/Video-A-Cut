# Python 流水线 Worker

核对日期：2026-10-18（agent/develop）。Worker 通过 MCP 领取导入及 recognize/sort/narrate/tts/subtitle/mix 等任务，不打开 SQLite，不自行指定认证角色。工具和依赖应事先安装；运行不下载模型。

从本目录运行 `python -m vac_worker --config <worker.json> [--once]`。token_env 指定凭证环境变量名，配置不保存令牌值。delivery_root 必须与控制面一致；ffmpeg/ffprobe 在 PATH 或配置中提供，心跳必须早于租约到期。`--once` 最多执行一项，0 表示成功/空闲，1 为已上报失败，2 为配置/传输错误。完整固定预设配置由 [运行入口](../../scripts/RUNTIME.md) 生成。

识别和解说可使用 builtin、vision、narration。builtin 是显式本地元数据/模板回归路径，不是模型推理；实际请求失败不偷偷改用模板。配置固定 endpoint/model/api_format/token_env，支持源码已有的 OpenAI/Anthropic/Gemini 格式；外部端点需要针对固定预设版本的外发许可。具体模型兼容性和内容质量仍需实际验收。

正式 TTS 可绑定固定模板版本与摘要，模板支持本机 SAPI 或已配置的 stepfun 协议端点；无模板时沿用预设中的 SAPI 声音。声音不可用、超时或合成超出旁白窗口会失败，不用测试音冒充正式旁白。实际语音长度回填 EDL，字幕和音乐压低依实际长度。测试桩只用于明确的离线验证。

阶段输出先写 staging 再原子发布，随后提交由 Go 重验。导入任务使用固定执行身份、停止/排空、检查点和恢复协议；租约丢失后不能晚提交覆盖新执行。业务素材和配置不放在测试清理目录。

Windows 日常测试通过 [统一测试入口](../../scripts/TESTING.md)，本轮 custom 的工作目录指向此目录，参数为 `-B -m unittest discover -s tests -t . -v`。兼容 `discover -s tests -v` 和单模块入口。原工作区固定媒体已迁至 timeline-cli/fixtures/media；历史独立脚本与源码恢复副本保留在 test-support/legacy，不作为生成物删除。测试版本来自 desktop/tauri.conf.json，输出经 VAC_TEST_ROOT/VAC_TEST_OUTPUT，500 MiB 与简档规则见根 AGENTS。

## 脚本工作台

`python -B -m vac_worker.script_workbench` 是 Go 显式调用的 JSON stdin/stdout 桥，负责受限抽帧、视觉检查、可选文字查证和实际旁白草稿。只处理控制面冻结的可见素材；不替人定稿。标准协议与凭证要求、范围和修订约束见 [工作台说明](../../docs/script-workbench-syntax.md)。本轮工作台/重定向 8 项和真实跨语言往返通过，不代表真实模型质量。

## 审查补充（历史：2026-10-16）

2026-10-17：CR01 已修复。省略 api_format 的自定义 vision/narration 与标准协议网关、TTS 共用 NoRedirect，拒绝转向其他路径或目的地。20 个重定向子场景和两类正常请求通过，Python 全套 148 项通过；监督脚本测试改用被测脚本要求的 pwsh。测试仅使用本机服务和虚拟令牌，详见 [代码审查与复验](../../docs/代码审查-2026-10-16.md)。
