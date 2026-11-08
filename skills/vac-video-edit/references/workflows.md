# 编辑、局部修改与交付

## 素材成片路线

1. 当前工程发现 → 读取脚本资源与序列 → 选择服务端执行包/flow并创建或领取任务。先读该包的 `vac/tool-contracts.md`。
2. `media.list` 后按目标获取词级转录或定向抽帧。`media.get_frames` 返回的是元数据，要用 `artifacts.get {id:frame_id}` 实际查看图片；转录artifact的base64按JSON解码。不要用标题/文件名推断画面。口播切点按词级边界，保留自然停顿。
3. 视觉大意路线先 `summaries.submit`，保存引用真实frame_ids的源时间证据。`scripts.propose` 的body段落与segments数量/顺序一致，start/end为允许窗口内的源秒，旁白与镜头匹配，显式字幕/转场和引用仍权威。
4. 提案由本任务Agent确认并继续，不要求另加人工定稿；只有旧任务仍待定时用 `skill_runs.update action:finalize`。配置配音时使用当前不可变模板/配置版本和实际声音合同；enabled不证明已有音频。缺少提供方或许可时说明依赖，不填造模板、密钥或accept_external。
5. `renders.create` → `renders.inspect` → 对inspection每个frame_id依次 `artifacts.get` → `renders.review`（精确plan_version及完整有序frame_ids）→ `run.finish {}`。需要修改则新提案后重新渲染/检查，不复用旧版本检查。

MP4和Premiere ZIP为精确计划版本的产物；完成后整片可作为一个视频片段加入独立Agent轨组，原剪辑保留。不能把成片单片段当作内部镜头可单独调整的时间线。XML内可能引用烘焙效果/字幕/配音，生成成功不证明Premiere实机导入或原生可编辑效果通过。

## 原生时间线路线

使用具备timeline工具的执行包，先读当前序列，核对 `session/revision/content_version`、轨道锁、媒体流索引、链接组和当前作用范围。

下面只演示移动已有片段的封装；使用实际ID、最新版本/修订。`at:90` 是30fps序列的第3秒，不能当90秒；其他FPS需重新换算。

```json
{"run_id":"RUN_ID","request":{"name":"timeline.edit","expected_version":7,"idempotency_key":"move-clip-01","arguments":{"sequence_id":"SEQUENCE_ID","command":{"session":"SESSION_ID","expected_revision":12,"idempotency_key":"move-command-01","label":"调整片段位置","operations":[{"type":"clip.move","clip_ids":["CLIP_ID"],"target_track_id":"TARGET_TRACK_ID","at":90}]}}}}
```

外层run版本和内层sequence修订各自校验，内外幂等键各自标识动作。当前片段是否可按此位置移动，以实际源长度、重叠模式、锁和链接规则为准。添加轨道后先读生成的track_id再插片段，不能预猜ID。

- `video` 接普通/已渲视频；`audio` 接音频；`code` 接draw_source。只在兼容类型间移动，不以跨类型拖放代替抽音。
- 音频可跨兼容音轨/轨组；链接音轨随视频进入匹配轨，insert波纹仅目标组。轨组是对照范围，不是锁。
- 保留用户手动锁和其他任务锁。只有显式 `track.update {track_id,locked:true/false}` 改锁；自己的预留可以写入并在插入后释放。
- 使用真实stream_index；独立音乐、旁白、源声不重复混入。图层隐藏与声音静音分别控制。
- 复杂操作按服务端schema填写，不猜新增效果字段。所有operations原子成功或失败，错误不允许偷偷提交部分命令。

编辑后使用返回的当前序列信息。没有扁平计划时 `run.finish {sequence_revision,content_version}` 对该精确序列共享合成、验证并交付MP4；不要为了finish额外提出扁平计划，重复添加整片。检查实际产物和技术警告，不把预览代理或单帧就绪当成正式导出。

## 成片局部修改

用户指定某段时，使用 `skill_runs.edit action:target`，arguments包含当前 `plan_version,prompt,artifact_id,artifact_sha256,idempotency_key` 及 `segment_index`、`at_seconds` 或 `timeline_start/timeline_end` 定位。时间为当前成片时间，不能直接当源时间。先从 `get` 读取 `edit_request.base_plan/windows` 的明确映射。

只修改选中段，保持全局字幕开关、段数、未选段的正文/媒体/旁白/配音设置不变；选中段正文也不能拆成更多段。`scripts.propose` 与 `skill_runs.edit action:plan` 都受此范围约束，不能用整计划保存绕过。完成新提案后重新进行同版本渲染与检查。画面内容的模型修补/生成接口仍not_implemented，不能因此调用外部图像提供方。

## 失败、保存与结果表述

运行状态名 `waiting_human` 不一定要求人工点击：可能是依赖或Worker失败，Agent修复原因后可resume。参数错误/过期上下文通常仍queued，刷新并修正即可。`run.steps`读取本任务诊断，不读取人工操作历史。预算耗尽或无法修复时报告具体缺口，不重建任务消除消耗。

保存脚本不启动生成；复制交接文字不证明已派发；任务完成不证明无路径工程已跨重启保存。当前MCP表没有通用AVP另存/素材导入/全局设置写入入口：如需这类操作，说明VAC界面中的对应步骤，不发明 `projects.save` 或 `resources.import`。已有工程资源无需重复导入。

交付说明包含产物ID/名称（有真实可用链接才给链接）、对应计划或序列版本、时长/规格及实际检查结果。技术黑屏/静音警告仅按事实提示，不能为了显示passed改掉用户要求的黑场/静音；人工验收未做写not_checked。保存、连接或HTTP成功都不能替代真实成片。
