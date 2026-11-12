# 视觉设计语言

适用于每一个工作区：资源、编辑、绘制、模板库、导出、TTS、设置，以及仍挂在顶栏上的脚本页。模板库和设置里的模板目录使用同一套 token、控件和动效，不另做一套外观。本文是可执行约束，不是氛围说明。色板摘要与样式门仍写在 [架构与功能详解 §15](../架构与功能详解.md#style)；两边冲突时以本文为准。

机器门：`webui/scripts/lint-style.mjs`（`npm run lint:style`，并纳入 `test:unit`）。它检查色板、圆角、渐变、模糊、阴影，以及页面/列表入场位移、代码条身份色和旧时间线音频条。控件外观、侧栏、焦点和状态语义仍要人工看实际界面。

## 阶段

| 阶段 | 范围 | 本文件中的地位 |
|---|---|---|
| Phase 1 | 去掉工作区入场动画和全局列表入场动画；代码条与旧音频条不再用状态色当身份色；脚本插入/速查和同类操作控件改为 Ant Design | 已经要遵守的现行规则 |
| Phase 2 | 顶栏去掉「脚本」；脚本在编辑右坞，时间线保持可见可编；右坞 36px 折叠条，代码条在属性里 | 已经要遵守的现行布局 |
| Phase 3 | 导出、设置以及其余工作区使用本文的页边、标题和区块间距 | 已经要遵守的现行疏密 |

后一阶段不得另起一套视觉。新功能进已有主表面里的模块，不新增顶栏项。

## 色

只使用下列 token。组件不写裸颜色。`theme.ts` 与 `styles.css` 的 `:root` 保持同一组色值。透明叠层写 `color-mix(in srgb, token 百分比, transparent)`。ASS 字幕颜色是内容，只允许出现在 `AssStyleEditor` / `assEditorModel`。

| Token | 值 | 用途 |
|---|---|---|
| `--vac-bg` | `#1E1E1E` | 窗口 |
| `--vac-panel` | `#252525` | 面板 |
| `--vac-control` | `#2F2F2F` | 控件底 |
| `--vac-border` / `--vac-hairline` | `#3A3A3A` | 边框、分隔 |
| `--vac-text` | `#E6E6E6` | 主文字 |
| `--vac-text-2` | `#9A9A9A` | 次文字 |
| `--vac-accent` | `#5492FF` | 唯一强调色：播放头、选中、主操作。不是片段或轨道身份色 |
| `--vac-clip-video` | `#3E6FB0` | 视频轨道默认色，也是预设「默认蓝」 |
| `--vac-clip-indigo` | `#4556A3` | 视频片段预设「靛」 |
| `--vac-clip-steel` | `#4A6D88` | 视频片段预设「钢」 |
| `--vac-clip-brown` | `#7A6248` | 视频片段预设「棕」 |
| `--vac-clip-rose` | `#7A5362` | 视频片段预设「玫瑰」 |
| `--vac-clip-mist` | `#65588A` | 视频片段预设「雾紫」 |
| `--vac-clip-audio` | `#3E8E6B` | 音频轨道默认色 |
| `--vac-clip-code` | `#3A7D86` | 代码轨道默认色 |
| `--vac-clip-subtitle` | `#B08A3E` | 字幕轨道默认色 |
| `--vac-clip-effect` | `#8A5FB0` | 效果身份；未单独着色的 Agent 来源片段 |
| `--vac-success` / `--vac-success-text` | `#49AA19` / `#52C41A` | 仅状态：完成、通过 |
| `--vac-warning` / `--vac-warning-text` | `#D89614` / `#FAAD14` | 仅状态：警告、进行中、未就绪 |
| `--vac-danger` / `--vac-danger-text` / `--vac-danger-soft` | `#DC4446` / `#FF4D4F` / `#FF7875` | 仅状态：错误、无效、阻塞 |
| `--vac-status-cyan` | `#13A8A8` | 仅状态：解说未批准 |
| `--vac-media` / `--vac-on-media` | `#000000` / `#FFFFFF` | 媒体井，以及画在媒体上的字 |

做：

- 轨道默认色：视频 `--vac-clip-video`，音频 `--vac-clip-audio`，代码 `--vac-clip-code`，字幕 `--vac-clip-subtitle`。Agent 来源且没有单独颜色时用 `--vac-clip-effect`，标题保留「Agent ·」。
- 代码条整段使用 `--vac-clip-code`，不用强调色，也不再用效果紫做区分。时间线标题仍是「代码 · 名称」，工作台卡片标题仍是名称。
- 视频轨上的不同片段可以不同颜色。未设置时用 `--vac-clip-video`。内置预设只有：默认蓝 `#3E6FB0`、靛 `#4556A3`、钢 `#4A6D88`、棕 `#7A6248`、玫瑰 `#7A5362`、雾紫 `#65588A`。预设里不放红、黄、亮绿或状态青。
- 自定义颜色使用 Ant Design `ColorPicker`。用户可以选任意 `#RRGGBB`，包括预设以外的颜色，不要拦。颜色写在片段的 `color` 字段上，随工程保存，重开后时间线条用这个颜色。空字符串或不写字段表示默认蓝。「恢复默认」清空该字段。
- 色板放在已有的片段属性里，只在选中视频片段时出现。不为此新开页面或顶栏。
- 旧兼容视图 `components/Timeline.tsx` 的音频条使用 `--vac-clip-audio`。
- 成功、警告、危险、青色只表示状态：批准、错误、无效语法、电平过载、解说未批准、阶段完成/进行中/出错。

不做：

- 不用 `--vac-accent` 给代码条、轨道或片段做默认身份色。
- 不用 `--vac-danger*`、`--vac-warning*`、`--vac-success*`、`--vac-status-cyan` 给代码条、轨道或片段做默认身份色。
- 不把音频、字幕、代码、效果条做成可逐条换色，除非沿用同一个 `color` 字段且几乎不用加规则。当前只对视频片段写入 `color`。
- 不把 Ant Design 的语义色当成第二套强调色。
- 不加渐变、发光、`box-shadow`、`text-shadow`、`backdrop-filter`、模糊。侧栏是实色 `Layout.Sider`，不是玻璃。`ColorPicker` 内部的选色面是组件本身，页面上不要再画渐变。

## 形

- 圆角 2–4px。默认 `--vac-radius: 3px`，窗口 `--vac-radius-window: 4px`。
- 唯一豁免，且必须和 `lint-style.mjs` 的整段选择器一致：`.app-nav.seg-nav` 为 12px；其直接子项 `.ant-menu-item`、`.ant-menu-submenu` 和 `::before` 为 9px；`.stage-dot` 与 `.stage-next-dot` 为 50%。类名里碰巧含有这些词不算豁免。JS/TSX 里没有选择器的圆角不豁免。

不做：大圆角、胶囊营销按钮、液态玻璃、ui-ux-pro-max。

## 控件

交互和操作控件使用 Ant Design 5：`Button`、`Slider`、`InputNumber`、`Select`、`Input`、`Checkbox`、`Switch`、`Tabs`、`Card` 等。尺寸用组件自己的 `size="small"`，不另画一套芯片、开关或工具按钮，也不把一排操作画成胶囊 `Segmented`。

允许自绘的只有：

- 时间线片段、波形、增益包络
- 绘制画布的手柄
- 构图时间轴上的图层条
- 脚本补全列表。它可以保持自绘 listbox，但背景、边框、文字和焦点必须用上面的 token

属性表（`Descriptions`）行高 1.25（`--vac-line`），单元格纵向内边距 4px。状态说明可以用 `--vac-line-read`（1.65）。

做：脚本插入、段落模板、无效语法替换、语法速查行，使用 `Button size="small"`，外层用 `Space` 或 `Flex`。速查行可以是通栏小按钮，左边写法、右边说明，但不是自绘 `<button class="script-chip">`。

不做：自绘 `.script-chip`、`.script-cheat-row`、`.run-chip` 按钮。资源视图切换和 TTS 引擎选择如果已经是 Ant Design `Segmented`，保持该组件，不要再包一层自绘胶囊。

## 动效

只有顶栏页签滑块 `.app-nav.seg-nav::before` 可以滑动（`translate`）。时长 0.35s，曲线 `cubic-bezier(.3, 1.3, .5, 1)`，形态沿用 Lucian #295，颜色仍用本文色板。第一次放到某一项时不播滑动（现有 `data-thumb-anim`）。`prefers-reduced-motion: reduce` 时该滑块 `transition: none`。

工作区内容立刻出现。

做：

- 按钮按下 `scale(0.97)`，150–250ms（`--vac-dur-fast` 到 `--vac-dur-slow`）的颜色和边框过渡
- 时间线吸附参考线、焦点 `outline: 2px solid var(--vac-accent)`
- 拖放目标可以用短的透明度变化。拖动进行中不播宽度或高度动画（现有 `.is-dragging-resize` 关掉坞宽过渡）

不做：

- 不给 `.page` 加 `vac-page-in`，也不用带 `translateY` / `translateX` 的入场关键帧
- 不把顶栏滑块和页面进入的位移或淡入绑在一起
- 不给 `.ant-list-item`、`.ant-table-row`、`.ant-alert`、`.ant-empty`、`.ant-result`、`.asset-card` 加 `vac-item-in` 或其它入场动画
- 不给分区内容、面包屑、「下一步」做进入淡入
- 二级 `Tabs` 设置 `animated={false}`，避免再出现一条滑动墨条
- 不加发光

随后（不是 Phase 1）才允许：模块展开和坞宽使用 150–200ms 的高度或宽度过渡，且只在松手之后，不在拖动过程中。

## 信息架构

顶栏六项，等宽，顺序固定：资源、编辑、绘制、模板库、导出、TTS。设置从文件菜单进入，不是顶栏页签。模板库是 0.5.3 已有的主表面，沿用本文，不单独设计。人读日志、Skill、MCP、诊断和模板目录放在设置的模块里。

数字键按这个顺序：1 资源、2 编辑、3 绘制、4 模板库、5 导出、6 TTS。设置和已经取消的审查页没有数字键。

脚本不在顶栏。脚本工作台在编辑右坞里，宽度可拖。打开或展开脚本时，时间线保持可见，并且可以继续编辑。专注脚本是右坞里的按钮，只收起右坞里的其他模块，不把脚本做成顶栏项，也不隐藏时间线。脚本区负责启用 TTS 和挂上段落。声音细调留在 TTS 页。

右坞收起时是 36px 竖条，只显示模块名（脚本、属性、Skill 任务、编辑历史）和进行中徽章。点击模块名展开。展开后是纵向排列的可折叠模块，用 Ant Design `Button size="small"`，圆角 2–4px，没有渐变、发光或阴影。

选中代码条时，代码条工作台出现在属性模块里。这不得卸载或替换脚本工作台。

不做：

- 不加新的顶栏页签。MCP、日志、代码、诊断都不是新的顶层页
- 不把流程阶段条放回顶栏。`PipelineStepper` 可以留在仓库里供既有契约测试引用，但不要为了动效或导航把它挂回 `.app-header`
- 打开脚本时不隐藏时间线
- 不把外部 Agent 和内置 Worker 的现有展示拆开重做

节目监视器：传输控件贴底，多出来的高度给画面。时间线视频条像 Premiere：只在片段起点放一张按比例的缩略图。增益包络在用户编辑之前是直线，不跟着波形走。

## 疏密

编辑和时间线保持紧：间隙大约 4–8px。

资源、绘制、模板库、导出、TTS 和设置用同一套页面疏密，不各做一套：

- 页边：左右 `--vac-page-x`（28px），上下 `--vac-page-y`（20px）
- 页面标题：24px
- 区块间距：`--vac-section-gap`（24px）
- 卡片用 `Card size="small"`，卡片内边距大约 12px，不要再用 20px 以上的内边距把页面撑成说明书

这些页面可以比时间线松，但标题、页边和区块间距要对齐上面的数字。模板库标题也是 24px，设置分类标题保持 24px 的页面标题和更小的分类标题。

## 禁止清单

- 液态玻璃、ui-ux-pro-max、大圆角、胶囊营销界面、装饰性入场动画
- 把流程阶段条放回顶栏
- 编辑脚本时藏起时间线
- 为 MCP、日志、代码或诊断新开顶栏页
- 用强调色或状态红、黄、绿、青给代码条或轨道做默认身份色
- 渐变、发光、阴影、背景模糊
