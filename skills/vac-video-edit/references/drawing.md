# 绘制与局部代码条

## 普通绘制任务

用当前提供的 `vac-draw-pipeline` 或其他绘制执行包；`strategy:draw` 可在无视频素材的工程运行。先 `draw.capabilities`，再按实际能力设计；使用该包的资源索引读取引擎说明及工具合同，不要求用户先选引擎。

如果脚本指定 `{#卡号}`，按绘制包声明的 `draw.shot` 工具读取对应配方、源码和图片别名。原样调用可向 `draw.save` 传顶层 `shot_card`，需要定制时保留作者声明并适配返回源码。图片别名绑定当前工程素材。旧运行只以自身声明的工具为准。

1. `draw.list`、`sequences.get` 获取session、project_revision与已有构图。新构图使用稳定ID、revision:0；更新使用已存composition.revision。每次draw.save前重读project_revision，工具日志也可能推进它。
2. `draw.save`保存源码/统一document，再 `draw.check`。统一文档用 `schema_version:1`、canvas `{width,height,fps:{num,den},alpha:"straight",color_space:"srgb"}`、duration_frames、sources和nodes；完整节点schema取实时capabilities，不凭空造字段。
3. `draw.preview`抽查起点/运动中间/尾帧，再 `artifacts.get` 看实际像素。透明素材要检查alpha与合成效果。预览源帧不要求先生成MP4。
4. 保留可编辑源码时 `draw.import source_mode:snapshot`（冻结源）或 `follow`（明确跟随构图）；进入代码轨，无需先渲电影。需要单一视频时 `draw.render` 后以 `rendered` 入视频轨；渲后MP4不能还原成图层。
5. import前重读sequence.revision；`at`为序列帧。默认新建Agent轨组，也可指定兼容target_track_id。显式预留既有轨时，用timeline.edit锁住自己的目标轨 → 渲染/导入 → 显式解锁；不清用户锁。
6. 用 `timeline.edit` 编排已入轨资源，最后结构化 `run.finish {sequence_revision,content_version}`。原生参数作用在时间线，源码不重复实现同一缩放/音量/位移。

## 源码与图片/声音

| 引擎 | 源码约束 |
|---|---|
| `hyperframes` | 完整HTML；根有id、data-composition-id/data-width/data-height/data-duration。GSAP由本地注入，暂停且按帧确定的时间线登记至window.__timelines |
| `remotion` | 默认导出React组件的TSX，仅导入react/remotion；宿主建立Composition/registerRoot，使用useCurrentFrame/useVideoConfig |
| `motion_canvas` | VAC 0.5.1合同支持3.17.2；`makeScene2D(function* (view) {...})`，只导入@motion-canvas/core和@motion-canvas/2d；不另起Vite编辑器 |

以实际draw.capabilities为准。不得通过随机数/真实时钟改变同帧画面；无远程URL、任意磁盘导入、shell或隐式安装。缺Chrome/运行时明确报依赖，不能静默下载。单引擎旧构图支持范围：偶数宽16..3840、高16..2160，FPS 1..120，1..18000帧且最长300秒，源码96KiB，每工程最多16构图；新文档约束按当前schema核对。

图片从当前 `image_resources` / `resources.list` 获取resource_id。`composition.images`绑定实例，勿用 `document.images`；统一文档会规范为image节点。每实例独立id/位置/尺寸/时间/透明度；`mode:layer`自动叠加，`mode:source`只提供给源码，避免两次显示。HTML用 `vac-image:INSTANCE_ID` 和window.vacImages；Remotion组件收 `{images}`，用 `images[INSTANCE_ID].src`。Go绑定SHA，不能传路径替代resource_id。删除/变化后重新关联并保存，不能复用旧像素。

代码声音位于 `DrawDocument.audio_bindings`：`{resource_id,at_frame,duration_frames,source_in,stream_index,gain_db}`，at/duration使用源码FPS，source_in为源秒。仅真实导入音频流；Go解析路径。代码条音量只控制这些声明，不自动控制独立音乐/旁白。

## 只改选中代码条

**仅改参数/字幕**：使用具备timeline.edit的普通任务（不设置code_targets），读取完整code.script并只增改所需指令，通过 `code.script` 携带code_revision保存；不调用源码生成、code.replace或draw.import。只传明确选中的clip_ids。若要交付整片，走结构化序列finish；若只需保存参数，报告参数写入结果，不为了完成状态生成整片。已领取的describe/regenerate局部任务不支持这类参数保存，不能借其提交不匹配动作。

例如，只让选中代码条从第1秒起立即放大至120%，同时显示2秒字幕。先确认代码条至少3秒长，检查原脚本是否已有同一时刻的缩放，然后把下列指令合并进完整作者文本。

```vac-code
{缩放, 时间:条内1s, 比例:120%}
{字幕, 文本:"开始，{原样}", 时间:条内1s, 持续:2s}
```

在 `timeline.edit` 的 `command.operations` 中提交 `type:"code.script"`、`clip_ids:[选中的ID]`、`description:合并后的完整文本`、`code_revision:当前代码修订`，省略mode。用 `timeline.read` 核对目标效果和源资源ID，以及未选条的状态。字幕和缩放都已写入时，只报告本次编辑的实际结果。

**重新生成源码或补空描述**：使用下述局部任务。

先从TimelineView获得每个目标clip的 `code.content_version`、`code.revision` 与完整 `code.script`。有已排队任务先领取，不重复创建。创建时用 `strategy:draw`、支持timeline工具的执行包、`code_targets:{CLIP_ID:CONTENT_VERSION}` 及 `code_action:describe|regenerate`。`skill_runs.get`提供code_sources、捕获目标及失败候选。

此任务只允许目标上的相应代码操作，每次只提交一个目标/一个operation：regenerate仅code.replace；describe仅code.script且mode:describe。禁止draw.save/import、普通时间线改写或code.job。下表也列出普通时间线操作的字段，不能据此认为局部任务允许全部操作：

| timeline.edit operation | 用途 |
|---|---|
| `{type:"code.script",clip_ids:[id],description:完整作者文本,code_revision,mode?}` | 解析直接参数并保存描述；mode:describe只补空描述，保留画面/参数 |
| `{type:"code.replace",clip_ids:[id],code_version,code_segments:[{composition:完整DrawComposition,description}]}` | 一个目标可替换1–32独立段，源码检查/真实首帧后一次替换并在当前组波纹 |
| `{type:"code.job",clip_ids:[id],code_run_id}` | 只关联任务状态，不改变源码/参数 |

`code_version`来自目标内容摘要，`code_revision`是描述修订，二者不能互代。每个目标独立事务/撤销项；某目标失败不回滚其他成功目标。人工修改产生冲突后保留candidate供后续处理，不覆盖用户新文字。复制/切割出的新实例独立，不继承旧任务目标。

描述变化才重新生成对应条；只改参数直接保存，不触发源码再生成；空行不会拆条。至少一个目标写入成功后可 `run.finish {}`，不强制导出整片；报告哪些目标成功/失败，不把部分成功写成全部完成。所有目标都失败时不能finish伪造成功。
