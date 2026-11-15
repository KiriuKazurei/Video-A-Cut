# 视觉设计语言

本文是全工作区的现行规范：资源、编辑、绘制、模板库、导出、TTS、设置。模板库页面和设置里的模板目录使用同一套 token、控件和动效，不另做一套外观。脚本在编辑右坞，不是顶栏页。

本文是可执行约束。色值、圆角、间距、字号、时长都从 `webui/src/theme.ts`、`webui/src/styles.css` 的 `:root` 和 `webui/scripts/lint-style.mjs` 归纳，不另造一套数。与 [架构与功能详解 §15](../架构与功能详解.md#style) 冲突时，以本文为准。

## 目的与效力

- 所有工作区共用这一套设计语言。新页面、新模块、新颜色先对照本文，再写界面。
- 本文描述已经落地的规则。历史阶段只放在文末附录，不作为「还没做」的清单。
- 不改营销版本号。视觉调整不单独升版本。

## Token 与源文件

两处必须保持同一组色值：`webui/src/theme.ts` 的导出常量，以及 `webui/src/styles.css` 的 `:root`。`lint-style.mjs` 用这两处收集允许的十六进制色。组件不写裸颜色。透明叠层写 `color-mix(in srgb, token 百分比, transparent)`。ASS 字幕颜色是内容，只允许出现在 `AssStyleEditor` / `assEditorModel`。

### 色

| Token | 值 | 用途 |
|---|---|---|
| `--vac-bg` | `#1E1E1E` | 窗口 |
| `--vac-panel` | `#252525` | 面板、侧栏 |
| `--vac-control` | `#2F2F2F` | 控件底 |
| `--vac-border` / `--vac-hairline` | `#3A3A3A` | 边框、分隔 |
| `--vac-text` | `#E6E6E6` | 主文字 |
| `--vac-text-2` | `#9A9A9A` | 次文字 |
| `--vac-accent` | `#5492FF` | 唯一强调色 |
| `--vac-clip-video` | `#3E6FB0` | 视频轨默认色，也是预设「默认蓝」 |
| `--vac-clip-indigo` | `#4556A3` | 视频片段预设「靛」 |
| `--vac-clip-steel` | `#4A6D88` | 视频片段预设「钢」 |
| `--vac-clip-brown` | `#7A6248` | 视频片段预设「棕」 |
| `--vac-clip-rose` | `#7A5362` | 视频片段预设「玫瑰」 |
| `--vac-clip-mist` | `#65588A` | 视频片段预设「雾紫」 |
| `--vac-clip-audio` | `#3E8E6B` | 音频轨 |
| `--vac-clip-code` | `#3A7D86` | 代码轨 |
| `--vac-clip-subtitle` | `#B08A3E` | 字幕轨 |
| `--vac-clip-effect` | `#8A5FB0` | 效果；未单独着色的 Agent 来源片段 |
| `--vac-success` / `--vac-success-text` | `#49AA19` / `#52C41A` | 仅状态：完成、通过 |
| `--vac-warning` / `--vac-warning-text` | `#D89614` / `#FAAD14` | 仅状态：警告、进行中、未就绪 |
| `--vac-danger` / `--vac-danger-text` / `--vac-danger-soft` | `#DC4446` / `#FF4D4F` / `#FF7875` | 仅状态：错误、无效、阻塞 |
| `--vac-status-cyan` | `#13A8A8` | 仅状态：解说未批准 |
| `--vac-media` / `--vac-on-media` | `#000000` / `#FFFFFF` | 媒体井，以及画在媒体上的字 |

`theme.ts` 把上述色映射进 Ant Design：`colorPrimary` / `colorInfo` / `colorLink` 都是强调色；成功、警告、错误只用状态色。`boxShadow`、`boxShadowSecondary`、`boxShadowTertiary` 为 `none`。

### 圆角、间距、字号、行高、时长

| Token | 值 | 用途 |
|---|---|---|
| `--vac-radius` | `3px` | 默认圆角。`theme.ts` 的 `borderRadius` 也是 3 |
| `--vac-radius-window` | `4px` | 窗口与监视器外框。`borderRadiusLG` 为 4 |
| `borderRadiusSM` / `borderRadiusXS` | `2` | 小控件 |
| `--vac-page-x` | `28px` | 文书页左右页边。写在 `.stage-main` |
| `--vac-page-y` | `20px` | 文书页上下页边 |
| `--vac-section-gap` | `24px` | 区块间距 |
| `--vac-stack-gap` | `20px` | 同一区块里的堆叠（`.section-content`、TTS 文本列） |
| `--vac-font` | `13px` | 正文。`theme.ts` 的 `fontSize` |
| `--vac-font-sm` | `12px` | 次文字、标注。`fontSizeSM` |
| `--vac-font-md` | `14px` | 卡片标题。`Card.headerFontSize` |
| `--vac-line` | `1.25` | 属性表行高 |
| `--vac-line-read` | `1.65` | 状态说明、设置分类说明 |
| `--vac-ctxbar-h` | `40px` | 导出 / TTS 位置条高度 |
| `--vac-dur-fast` | `150ms` | 按下、短颜色过渡。`motionDurationFast` 为 `0.15s` |
| `--vac-dur` | `200ms` | 常规颜色 / 边框。`motionDurationMid` 为 `0.2s` |
| `--vac-dur-slow` | `250ms` | 较慢过渡，含侧栏松手后的宽度。`motionDurationSlow` 为 `0.25s` |
| `--vac-ease` | `cubic-bezier(0.645, 0.045, 0.355, 1)` | 颜色与边框 |
| `--vac-ease-out` | `cubic-bezier(0.215, 0.61, 0.355, 1)` | 绘制拖入提示 |
| 顶栏滑块 | `0.35s`，`cubic-bezier(.3, 1.3, .5, 1)` | 只写在 `.app-nav.seg-nav::before` |

控件高度：`controlHeight` 28、`controlHeightSM` 24、`controlHeightLG` 34。顶栏高 48，菜单行在 `ProjectMenuBar` 为 34px。顶栏页签项高 32px，导航槽宽固定 600px。

## 字体

`theme.ts` 不覆盖 `fontFamily`。界面文字用 Ant Design 5.29 的默认无衬线栈：`-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif`，并带系统彩色表情字体。Ant Design 自己的 `fontFamilyCode` 不作为界面正文。

| 层级 | 大小 | 出现在 |
|---|---|---|
| 页面标题 | 24px | 设置的 `h1`（`.settings-page-heading`）、模板库的 `h3`（`.shotcraft-library`） |
| 分类标题 | 18px | 设置当前分类的 `h2` |
| 卡片标题 | 14px | Ant Design `Card` 头 |
| 正文 | 13px | 默认字号、属性表、位置条 |
| 次文字 / 标注 | 12px | `--vac-font-sm`：轨道名、时间码、绘制分区标注、位置条里的资产段 |
| 媒体上的字幕条 | 15px | `.monitor-caption`，只叠在画面上 |
| 时间线空隙字 | 11px | `.timeline-gap` |
| 增益读数 | 11px，行高 16px | `.clip-gain-readout` |

等宽只用于机器文本：位置条资产段、运行编号、设置里的配置代码，字体栈是 `ui-monospace, SFMono-Regular, Menlo, Consolas, monospace`。绘制源码编辑器使用 `monospace`，字号 12px，行高 1.65。字幕字体是成片内容，不在这套 UI 字体里规定。

编辑右坞里的「脚本」是加粗正文，不是 24px 页面标题。样式表里仍留有未用于现行坞的 `h1` 规则（独立脚本标题 24px、旧坞内 18px）；现行 `dock` 分支渲染的是 `Typography.Text strong`。

## 色

银色底只有窗口、面板、控件、边框这一组。强调色只有 `--vac-accent`（`#5492FF`），只给播放头、选中、主操作，以及时间线上的增益线（`gainEnvelope.ts` 的 `GAIN_LINE`，与强调色同值）。强调色不涂片段，也不做轨道默认色。

轨道默认色：

- 视频 `--vac-clip-video`
- 音频 `--vac-clip-audio`
- 代码 `--vac-clip-code`。整段用这一色，标题仍是「代码 · 名称」。不用强调色，也不用效果紫做区分
- 字幕 `--vac-clip-subtitle`
- Agent 来源且没有单独颜色时用 `--vac-clip-effect`，标题保留「Agent ·」

视频轨上的片段可以不同颜色。未设置时用默认蓝。内置预设只有：默认蓝、靛、钢、棕、玫瑰、雾紫。预设里不放红、黄、亮绿或状态青。

自定义颜色使用 Ant Design `ColorPicker`（`disabledAlpha`，`format="hex"`）。用户可以选任意 `#RRGGBB`，包括预设以外的颜色，不要拦。颜色写在片段的 `color` 字段上，随工程保存；空字符串或不写字段表示默认蓝。「恢复默认」清空该字段。色板放在已有的片段属性里，只在选中视频片段时出现。当前只对视频片段写入 `color`。音频、字幕、代码、效果条不逐条换色。

成功、警告、危险、青色只表示状态：批准、错误、无效语法、电平过载、解说未批准、阶段完成 / 进行中 / 出错。旧兼容视图 `components/Timeline.tsx` 的音频条使用 `--vac-clip-audio`。

不做：

- 不用 `--vac-accent` 给代码条、轨道或片段做默认身份色
- 不用 `--vac-danger*`、`--vac-warning*`、`--vac-success*`、`--vac-status-cyan` 给代码条、轨道或片段做默认身份色
- 不把 Ant Design 的语义色当成第二套强调色
- 不加渐变、发光、`box-shadow`、`text-shadow`、`backdrop-filter`、模糊。侧栏是实色 `Layout.Sider`。`ColorPicker` 内部的选色面是组件本身，页面上不要再画渐变

## 形

圆角 2–4px。默认 3px，窗口 4px。

`lint-style.mjs` 的圆角豁免必须整段选择器相等：

| 值 | 选择器 |
|---|---|
| `12px` | `.app-nav.seg-nav` |
| `9px` | `.app-nav.seg-nav > .ant-menu-item`、`.app-nav.seg-nav > .ant-menu-submenu`、`.app-nav.seg-nav::before` |
| `50%` | `.stage-dot`、`.stage-next-dot` |

类名里碰巧含有这些词不算豁免。JS/TSX 里没有选择器的圆角不豁免。

不做：大圆角、胶囊营销按钮、液态玻璃。

## 控件

交互和操作控件使用 Ant Design 5：`Button`、`Slider`、`InputNumber`、`Select`、`Input`、`Checkbox`、`Switch`、`Tabs`、`Card`、`ColorPicker`、`Descriptions`、`Collapse`、`List`、`Menu`。尺寸用组件自己的 `size="small"`。不另画一套芯片、开关或工具按钮，也不把一排操作画成自绘胶囊。

允许自绘的只有：

- 时间线片段、波形、增益包络、播放头、标记和空隙
- 绘制画布的选框和缩放手柄
- 构图时间轴上的图层条
- 脚本补全列表。它可以保持自绘 listbox，但背景、边框、文字和焦点必须用本文 token

属性表（`Descriptions`）行高 1.25，单元格纵向内边距 4px。状态说明可以用行高 1.65。

脚本插入、段落模板、无效语法替换、语法速查行，使用 `Button size="small"`，外层用 `Space` 或 `Flex`。速查行可以是通栏小按钮，左边写法、右边说明。类名可以仍叫 `script-chip-bar`，但控件必须是 Ant Design `Button`，不是自绘 `<button class="script-chip">`。

资源视图切换和 TTS 引擎选择如果已经是 Ant Design `Segmented`，保持该组件。

侧栏主题 `siderTheme` 设置 `inherit: false`，并单独关掉按钮阴影。焦点用 `outline: 2px solid var(--vac-accent)`，不用发光。

时间线工具条为了可点，把小按钮热区收到 30×30px（`.timeline-toolbar .ant-btn`），轨道头按钮为 22×22px。这是命中区域，不是另一套视觉控件。

## 动效

只有顶栏页签滑块 `.app-nav.seg-nav::before` 可以滑动（`translate`）。时长 0.35s，曲线 `cubic-bezier(.3, 1.3, .5, 1)`。第一次放到某一项时不播滑动（`data-thumb-anim` 不为 `1`）。`prefers-reduced-motion: reduce` 时，全局把动画和过渡压到约 0.01ms，顶栏滑块再写一条 `transition: none`。绘制区和设置区在同一媒体查询里把自身过渡清掉。

工作区内容立刻出现。

允许的短过渡：

- 按钮按下 `scale(0.97)`，颜色和边框 150–250ms
- 时间线吸附参考线、焦点描边
- 拖放目标的短透明度变化。拖动进行中不播宽度或高度动画（`.is-dragging-resize` 关掉坞宽过渡）
- 侧栏在松手之后可以用 `--vac-dur-slow`（250ms）过渡宽度。拖动过程中关掉
- 绘制拖入提示只过渡 opacity / transform，时长 `--vac-dur-fast`

不做：

- 不给 `.page` 加 `vac-page-in`，也不用带 `translateY` / `translateX` 的入场关键帧
- 不把顶栏滑块和页面进入绑在一起
- 不给 `.ant-list-item`、`.ant-table-row`、`.ant-alert`、`.ant-empty`、`.ant-result`、`.asset-card` 加入场动画
- 不给分区内容、面包屑、「下一步」做进入淡入
- 二级 `Tabs` 设置 `animated={false}`
- 不加发光

## 信息架构

顶栏六项，等宽，顺序固定：资源管理、编辑、绘制、模板库、导出、TTS。实现是 `navigation.ts` 的 `TOP_NAV`。槽宽 600px，每项 `calc(100% / 6)`，最小宽 96px。设置不是顶栏页签。入口是「文件 → 设置」；「编辑 → 首选项」进入同一页。

数字键按这个顺序：1 资源、2 编辑、3 绘制、4 模板库、5 导出、6 TTS（`WORKSPACE_KEYS`）。设置和已经取消的审查页没有数字键。

路由（hash）：

| 地址 | 行为 |
|---|---|
| `#/resources` | 资源管理 |
| `#/editing`、`#/editing/import` | 编辑时间线 |
| `#/editing/content`、`#/script` | 打开编辑，并展开右坞里的脚本。时间线仍在，仍可编。`#/script` 会写成 `#/editing/content` |
| `#/editing/review`、`#/export/acceptance` | 审查页已取消，进入编辑时间线 |
| `#/draw` | 绘制。查询参数只带 project、run、slot、engine、focus |
| `#/templates` | 模板库 |
| `#/export`、`#/export/delivery`、`#/export/tasks` | 导出的交付 / 任务监控 |
| `#/tts` | TTS。可带 `script`、`version`、`segment` |
| `#/settings` 及 `#/settings/profiles` 等 | 设置分类：处理预设、服务商接入、运行准备、Agent 与 Skill、模板库、日志与诊断、状态监控 |

旧单段地址仍兼容：`#/import`、`#/assembly` 进编辑时间线，`#/edit` 进编辑并展开脚本，`#/prepare` 进设置的运行准备，`#/monitor` 进导出任务监控。

脚本不在顶栏。脚本工作台在编辑右坞，宽度可拖。打开或展开脚本时，时间线保持可见，并且可以继续编辑。专注脚本是右坞里的按钮，只收起右坞里的其他模块，不隐藏时间线。「视图」菜单里的「脚本」走同一条路由，不是顶栏项。脚本区负责启用 TTS 和挂上段落。声音细调留在 TTS 页。

右坞收起时是 36px 竖条，模块名竖排：脚本、属性、Skill 任务、编辑历史。进行中的 Skill 任务用 Ant Design `Badge`。展开后是纵向排列的可折叠模块，条头是 `Button size="small"`。

选中代码条时，代码条工作台出现在属性模块里（`CodeClipWorkbench` 挂在属性区，不替换 `ScriptWorkspace`）。脚本工作台保持挂载。

不做：

- 不加新的顶栏页签。MCP、日志、代码、诊断都不是新的顶层页
- 不把流程阶段条放回顶栏。`PipelineStepper` 留在仓库里供既有契约使用，不要挂回 `.app-header`
- 打开脚本时不隐藏时间线
- 不把外部 Agent 和内置 Worker 的现有展示拆开重做

## 疏密

两档，不要各页再发明第三套。

编辑和时间线保持紧。`.unified-editor` 的内边距是 `4px 8px 8px`，监视器间距 8px，时间线工具条间距 6px，轨道头控件间隙为 0。间隙落在大约 4–8px。

资源、模板库、导出、TTS、设置用文书页疏密：

- 左右页边 `--vac-page-x`（28px），由 `.stage-main` 给出
- 上下页边 `--vac-page-y`（20px），由 `.page-body` 或该页自己的容器给出
- 页面标题 24px（有标题的页：设置、模板库）
- 区块间距 `--vac-section-gap`（24px）
- 卡片用 `Card size="small"`。`theme.ts` 里 small 卡片内容内边距是 16px；设置里的预设表单和提供商卡片把 body 收到 12px。不要再用 20px 以上的内边距把页面撑开

导出和 TTS 的当前位置用 13px 位置条（`.context-bar`），不在位置条下面再叠一个同文案的 24px 标题。设置分类标题是 18px，页面标题仍是 24px。

窄于 960px 时，`.stage-main` 改为 12px 内边距，这是窄窗口例外，不是第二套桌面疏密。

绘制是铺满的工具台，和文书页不是同一档。`.workspace-draw .stage-main` 的 padding 为 0。工具条是加粗正文「绘制」，分区内边距 12px。不要为了凑 28px 页边把画布缩进一圈，也不要再加一个 24px 文书标题。

## 各工作区落地要点

**编辑。** 左栏是资源箱，中栏是时间线，右栏是 36px 可展开坞。节目监视器把画面放在中间行，传输控件在第三行贴底，多出来的高度给画面。视频条只在片段起点放一张按比例的缩略图（`.clip-thumbnails`，`object-fit: contain`，靠左）。没有关键帧时，增益是一条直线：`effectiveKeyframes` 在缺少 `gain_keyframes` 时返回起点和终点同一 dB 的两点；整段拖动且没有已存关键帧时保持恒定增益，不跟着波形走。选中视频片段后，属性里给出六个预设和 `ColorPicker`。

**脚本坞。** 展开时时间线仍可编。专注脚本只收起坞内其他模块。代码条工作台进属性，不卸载脚本。

**资源。** 左栏卡片标题是「资源管理」，右栏是元信息。未选资源时，主区是居中空态，不另加 24px 标题。选中后主列使用上下 20px、区块间距 24px，左右页边仍是舞台的 28px。预览、操作和备注用 `Card size="small"`。

**绘制。** 四区：左构图 / 图层 / 素材，中画布，下构图时间轴，右属性。图层条、选框和缩放手柄允许自绘。拖入提示只用短的透明度变化。设置里的模板目录和这里的素材引用都走本文 token，不另配色。

**模板库。** 标题「Shotcraft 模板库」为 24px。页边 28/20，区块间距 24px。列表和预览是同一页里的浏览器，不是新的顶栏。设置 → 模板库只管理目录，外观仍是设置页的 24px 标题、18px 分类标题和 24px 区块间距。

**导出。** 位置条加「交付 / 任务监控」两个页签，`animated={false}`。页边和 `.page-body` 的区块间距走 28/20/24。卡片 `size="small"`。没有成片时的空监视器是数据状态，不是另一套空态皮肤。

**TTS。** 三列：声音模板、文本与试听、声音控制台，列间距是 `--vac-section-gap`。环境警告用状态色，不改成强调色。引擎选择保持 Ant Design 控件。

**设置。** 页标题 24px，分类标题 18px，分类导航在 992px 以上为 208px。分组是工作流配置、扩展与模板、诊断与维护。返回上一个工作区的按钮留在页头右侧。

## 禁止清单

- 液态玻璃、大圆角、胶囊营销界面、装饰性入场动画
- 渐变、发光、阴影、背景模糊
- 把流程阶段条放回顶栏
- 编辑脚本时藏起时间线
- 为 MCP、日志、代码或诊断新开顶栏页
- 用强调色或状态红、黄、绿、青给代码条或轨道做默认身份色
- 自绘 `.script-chip`、`.script-cheat-row`、`.run-chip` 按钮
- 为了对齐文书页，把绘制画布或编辑时间线改成 28px 页边
- 把外部 Agent 和内置 Worker 的现有展示拆开重做
- 借视觉调整改版本号

## 机器门

`npm run lint:style`（`webui/scripts/lint-style.mjs`，并纳入 `webui` 的 `test:unit`）。它扫描 `webui/src` 的 CSS/TS/TSX：

- 色板以外的十六进制和命名色
- `rgb` / `hsl` 等颜色函数（应改为 token 或 `color-mix`）
- 渐变、`drop-shadow`、模糊、非 `none` 的 `backdrop-filter` 和阴影
- 超出 2–4px 且不在豁免选择器里的圆角
- `vac-page-in` / `vac-item-in`，以及带 `translate` 的关键帧
- 页面、列表、表格、提示、空态、资产卡片上的入场 `animation`
- `codeClips.ts` 必须使用 `--vac-clip-code`，且不得使用强调色或状态色
- `Timeline.tsx` 的音频条必须使用 `--vac-clip-audio`

机器门不替代人工查看：控件是不是 Ant Design、侧栏是不是实色、焦点、状态色有没有被当成身份色、顶栏是不是六项、脚本展开时时间线还能不能编、页边和标题是否符合上面的分档。

## 如何扩展

- 新能力进已有主表面里的模块。不新增顶栏项，不恢复「脚本」顶栏，不把流程阶段条挂回顶栏。
- 新颜色必须同时写进 `theme.ts` 和 `:root`，并说明它是身份色、强调色还是状态色。状态色不能当轨道或片段的默认色。视频片段的新预设不能使用红、黄、亮绿或状态青。
- 新的文书页沿用 28/20、24px 标题、24px 区块间距和 `Card size="small"`。新的时间线或画布类工具沿用紧凑或铺满，不套文书页边。
- 新的操作控件用 Ant Design 5。只有时间线、波形、增益、绘制画布和构图图层条可以自绘。
- 新的位移动效只允许加在顶栏滑块上。其它反馈停留在 150–250ms 的颜色、边框、透明度或按下缩放，并尊重 `prefers-reduced-motion`。

## 提交前检查清单

- `webui` 下 `npm run lint:style` 通过
- 改过界面时，再跑 `npm run test:unit` 和 `npx tsc --noEmit`
- 顶栏仍是六项等宽，没有新页签
- 强调色没有涂到片段默认色上；状态色没有变成轨道身份色
- 圆角仍在 2–4px，或落在样式门的整段豁免里
- 没有新增渐变、阴影、模糊或入场位移
- 编辑仍紧，文书页仍是 28/20/24；绘制仍铺满
- 没有改版本号

## 附录：阶段历史

这些阶段已经做完，只作检索，不再用来组织本文。

| 阶段 | 当时做了什么 |
|---|---|
| Phase 1 | 去掉工作区入场动画和全局列表入场动画；代码条与旧音频条不再用状态色当身份色；脚本插入、速查和同类操作改为 Ant Design |
| Phase 2 | 顶栏去掉「脚本」；脚本留在编辑右坞，时间线保持可编；右坞收起为 36px；代码条工作台放进属性 |
| Phase 3 | 文书页使用 28/20 页边、24px 标题和 24px 区块间距；编辑保持紧；绘制保持铺满工具台 |
