# 后续模块 v1 契约草案

日期：2026-10-07。**本目录保留早期交接草案和验收模板。ASS 与 delivery_variant 已接入，正式已实现协议见 [delivery-variant v1](../../../contracts/delivery-variant/v1/README.md)。FCPXML 与 runtime-capabilities 仍未实现。完整职责、文件边界与分工见 [方案书](../../后续模块开发框架与Agent交接方案-2026-10-07.md)。

| 文件 | 用途 |
|---|---|
| `source-baseline-ass.json` | ASS 首版完成后的当前源码索引，接收本轮变更使用此文件 |
| `source-baseline-repaired.json` | 稳定性修复后源码/锁文件/配置示例/方案文档的精确字节 SHA-256；不包含源码内容，也不是完整 Git 提交 |
| `source-baseline.json` | 修复前交接快照，保留历史；历史稳定性修复基线；本轮接收代码使用 ASS 索引 |
| `acceptance-run.template.json` | R1 真实业务会话及机器/真人分离的证据记录，null 必须在实际执行后填写 |
| `preview-quality-cases.json` | R2 必测矩阵、条件与通过标准；所有初始状态为 not_run |
| `runtime-capabilities.template.json` | R3 本地能力汇总的计划响应，not_checked 不能算 ready |
| `delivery-variant.request.template.json` | R4/R5 计划请求体；填齐字段且实现路由后才可发送 |

本目录的 null、pending、not_run 和 not_checked 是有意保留的未执行状态。不得把模板字段当成已经存在的运行证据。

## 1. 验收证据契约

版本字段 `schema_version=1`；`session_id` 唯一对应一次隔离运行。candidate 至少有 HEAD、工作区索引文件引用与 SHA-256、实际 build 索引引用与 SHA-256；索引自身参与散列，不把它自身的 hash 填回同一文件造成循环。

profile、source、workflow、preview 均记录身份和 SHA-256。物理文件 SHA-256 覆盖精确文件字节，输入逻辑指纹使用现有协议算法，两者不互换。没有预览、尚未导出或没有模型配置时保留 null 并在 blockers 中说明，不虚造标识。

自动结果使用 `not_run|passed|failed|blocked`。人工结果使用 `pending|passed|failed|blocked`；passed/failed 需要非空 actor、时间、note 和证据引用。这个旁路 JSON 的 pending/blocked 不是现有 Acceptance POST 的新枚举，现有接口仍仅接受 passed/failed。

证据引用为 `{path, sha256}`；相对会话证据根，路径禁止绝对地址、`..`、链接/重解析点逃逸和大小写别名重复。用户原素材只读记录受控 root_id/相对路径及 hash，报告不接收任意浏览器绝对路径。独立 schema/索引校验不能伪造证据文件已存在；模板与可接受执行结果分开验证。

源码发生变化、导出 revision 变化、预览 input/preset/output hash 变化，产生新候选/结果关联；历史记录保留。不沿用旧结论判定新结果。

## 2. 运行诊断契约

`overall_status` 为 `ready|blocked|not_checked`；`checks[].status` 为 `passed|blocked|not_checked`。每个能力独立判定，preview 需要当前 Worker ready、匹配 preset 及有效心跳；特定 profile 的真实模型/TTS 就绪不能由 preview ready 推出。

`observed_at`/`expires_at` 使用 UTC ISO 8601。既有能力 TTL 由 Go 的实际规则决定；读取不延长 Worker 心跳，也不把已过期快照视为可领取。受控动作需重新检查，不信任之前页面的 ready 标志。

普通 GET 仅有界、本地探测与状态聚合。模型连接测试使用现有明确触发的诊断接口，不在此 GET 中外发数据、自动安装或下载模型。响应不包含 token 值、环境变量值或原始外部响应。尚未实现端点时页面显示待支持，不能创建伪成功数据。

## 3. 派生格式任务契约

### 3.1 请求与状态

计划 POST 请求体严格为 `revision_id`、`expected_version`、`request_id`、`spec_id`。每个 spec 只对应一个格式、样式或目标应用矩阵；调用者不能直接指定输出路径或任意转换器。

202 表示创建任务，200 表示原请求或活动同输入任务复用；400 参数错误，403 治理拒绝，404 资源不存在，409 版本/请求绑定冲突。能力不满足沿用现有 control-plane invalid_state 错误语义，handler 不自定义另一套规则。不同请求复用活动任务也保存每个 request 的持久绑定；成功后新 request 产生新尝试。

Task 复用 queued→claimed→running→succeeded/failed/cancelled；view 额外投影 invalidated/is_current，与现有 preview 的治理和版本语义一致。数据库不重复保存 Task 状态。新能力和任务分派需覆盖旧 Worker 无能力时不会领到新任务。

### 3.2 输入逻辑指纹

所有 atom 用 `ASCII(UTF8字节长度) + ':' + UTF8内容` 拼接，然后 SHA-256 小写 hex。下面顺序冻结为草案 v1，实施者须写跨 Go/Node/Python 独立样本后才能接入：

1. `vac.delivery-variant.input.v1`。
2. schema_version 的十进制字符串 `1`。
3. asset_id、run_id、revision_id、parent_package_ref。
4. edl_sha256、parent_manifest_sha256。
5. format、spec_id、spec_sha256。
6. 文件数量的十进制字符串。
7. 按 path 的 UTF-8 字节序排序，对每个文件依次追加 kind、path、sha256、size 的十进制字符串。

variant_id/task_id/request_id 不参与输入指纹；request 的 revision/version/spec 绑定另行保存并核对。expected_version 属于创建请求身份，不加入媒体内容指纹。每个 spec 原始 UTF-8 文件字节有 SHA-256，规范固定 LF，并覆盖目标应用/版本、格式版本及样式等全部影响输出的参数。固定 spec 对象须与注册文件一致，对象 key 传输重排不构成参数变化；hash 仍来自注册原始字节。

输入文件清单覆盖 EDL、父 manifest、新格式使用的全部视频、分轨、字幕和其他媒体；重复路径、大小写别名、无效 hash/size 和未冻结来源拒绝。哈希文件大小、整数与时间使用当前受支持安全范围，不扩展分数项目 FPS。

### 3.3 输出验证与恢复

`variant-manifest.json` 包含版本、variant/task/asset/run/revision、input/spec 指纹及 artifacts `{kind,path,mime,size,sha256}`。`variant-receipt.json` 记录相同身份、转换器/工具/字体版本及恢复绑定。`variant-report.json` 记录目标 spec、独立检查、支持/不支持结构及媒体/时间摘要。

每个实际产物在新包内且唯一；由 Go 独立验证路径、散列、大小、身份、治理、租约和当前 revision。ASS/FCPXML 的必需结构检查不能是空数组。ZIP 从已登记产物和可控媒体构建，自包含且不漏掉原素材引用。母包不可变，派生过程不覆盖 EDL、正式 artifacts、preview 或人工通过记录。

发布先同盘 staging、后原子目录发布，再登记；失败前清理自己的 staging，失败后已发布目录保留用于相同身份恢复。不同身份、过期租约或失效 revision 的恢复提交拒绝。下载历史结果带版本，不能自动替换当前交付。

首个迁移、SSE 事件 `delivery_variant.changed`、API inventory、worker capability 和 UI 缓存键由集成负责人统一冻结；事件后重取 REST 快照，SSE 无历史重放。交付共用框架之前 R5 不另造一套任务或迁移。

## 4. 交接与变更

模板扩展会影响方案版本，先更新本目录及主方案，再由各实现者采用。已上线后任何破坏性字段/哈希变化须新的协议版本；不要为测试通过改写原 fixture 的 expected hash。

原始交接轮只生成文档与索引。本次 ASS 增量已实现任务、固定样式、独立包、Worker、API 和 UI，并执行转换器/Go/真实链路/浏览器验证；详见 [ASS 实现与验收](../../ASS字幕模块实现与验收-2026-10-07.md)。本目录尚未执行的 R1/R2/R3/R5 模板保持 pending/not_run。上方第 3 节为早期草案，ASS 实际字段以正式协议为准。
