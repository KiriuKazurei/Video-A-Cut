# MP4 预览协议与冻结 ASS 扩展

核对日期：2026-10-16。Go、Node 渲染/Worker 和 WebUI 已接入。本文件为当前协议说明，旧实施分工与首轮框架结果保存在历史归档，不再列为待实现任务。

## 类型、规格和输入

[contracts.d.ts](contracts.d.ts) 定义请求、能力、输入与视图；[固定预设](presets/mp4-720p-v1.json) 与 [指纹样本](fixtures/input.json) 定义精确字节。preview-store.sql 是早期结构参考，真实数据库结构以 control-plane/internal/store/migrations 为准。

preview 是独立任务，绑定成功导出的不可变 run/revision/package。输入冻结 EDL、原视频、最终音轨与 SRT/ASS 文件的相对路径、大小和 SHA-256，以及完整 preset 快照和摘要。Go 检查审批、治理、根内路径、重解析点、大小写别名、固定文件清单和当前归属；结构 fixture 不能作为真实媒体。

mp4-720p-v1 使用 H.264/yuv420p、AAC 48 kHz 双声道，最大 1280×720，contain/黑色间隙，不放大小素材；帧率沿用项目。小素材真实输出可低于 720p，不应修改 preset 迎合错误的尺寸断言。

输入指纹按共享代码固定 atom 序列拼接，每项为 UTF-8 字节长度的 ASCII 十进制、冒号、原始 UTF-8 内容，最终 SHA-256 小写 hex。文件按固定路径字节序列排序，散列包含规格和文件绑定，不含每次尝试的 preview/task ID。Node/Go 以独立固定样本验证，不直接改期望哈希使测试通过。

schema 1 使用原 SRT。创建时指定 subtitle_variant_id，则 Go 绑定同流程、同导出修订、成功且未失效的 ASS 派生，冻结 schema 2 的 variant/package/ASS/style/font 信息。扩展指纹在 v1 序列后追加冻结 ASS 身份和哈希；原 v1 指纹不变。Worker 必须支持 ASS 且报告所需字体。渲染直接使用冻结 ASS，不能读取可变草稿替代。

## REST、MCP 和执行身份

| REST | 用途 |
|---|---|
| POST/GET /api/workflows/{run}/previews | 固定版本创建、读取就绪检查与历史列表 |
| GET /api/previews/{id} | 状态、当前性、媒体和已登记产物 |
| POST /api/previews/{id}/cancel | 取消对应预览 |
| GET /api/previews/{id}/files/{key} | 受控下载；MP4 支持 inline/Range，download=1 请求附件 |

新请求携带 expected_version/revision_id/request_id；同请求的传输重试返回原绑定，参数冲突拒绝，明确新尝试使用新 ID。Task 是状态事实来源，invalidated/is_current 分别表示失效和当前版本；历史可读不意味着可作为当前节目源。

MCP 由 claim_task 返回执行 scope；get_task_input、heartbeat、report_progress、get_task_status、fail_task 和 submit_preview 校验 task/runtime_instance/execution/generation。旧执行不得覆盖新租约。独立 preview_capability 表示执行协议、可用预设/字体；它与 processing_profile 指纹不同。

Worker 在取消、失约或输入失效时停止子进程并等退出。已发布但登记未知时查询同一执行状态，有限重试，保留已发布包；registration_pending 不等于可覆盖重跑。新执行可按固定输入和回执核验后恢复登记。

## 产物与独立校验

输出为新目录 previews/<asset>/<revision>/<preview>，包含 final.mp4、render-report.json、preview-receipt.json、preview-manifest.json。manifest 注册 mp4/report/receipt，不散列自身；不能作为原始 delivery-manifest 提交。

report 必含 source_fingerprint、timeline_frames、audio_alignment、subtitles、media_streams，均由实际检查支持。无字幕也要明确通过原因。Go 使用独立 ffprobe 复核编码、帧数、时长和音频呈现区间；区间容差与源码一致，不代表内部声音内容或真人听感验收。receipt 记录实际工具、渲染器和字体哈希，ASS 字体数组不泄露本机绝对路径。

前端缓存和媒体选择绑定 asset/run/revision，只将成功、未失效且匹配当前选择的结果作为自动节目源；同版本刷新保留有效播放，切换归属或离开时停止旧媒体。SSE 通知后仍需 REST，重连不依赖历史重放。

## 已有证据与边界

Timeline 的真实 FFmpeg、中文字幕与冻结 ASS 渲染测试，以及自动模式真实预览链路均已有通过证据；新增链路为 1280×720、8 秒、240 帧，实际 Node worker、服务端提交与 ffprobe 检验，并查看三个时间点的抽帧。模型/TTS 使用本机桩，不能代表业务质量。

当前结论见 [项目状态](../../../docs/当前项目状态.md)，测试入口见 [运行与测试](../../../docs/当前运行与测试.md)，旧报告见 [历史索引](../../../docs/历史文档索引.md)。长素材、真实模型、实际剪辑软件和人工作品验收单独保留，不由本协议或机器报告替代。
