# 界面样式规范（硬性）

本文件是 Video-A-Cut 界面的硬性样式规范，自中性灰配色确认（`199f4be`）起生效。后续任何 UI 改动——包括页面、弹窗、空状态、引导、自动模式授权和状态监控——都必须遵守。

**违者 Gate 不放行。** 合入前须通过 `webui` 下的 `npm run lint:style`（已纳入 `npm run test:unit`）。脚本扫的是机器能判定的项；下面的人工清单仍要过目。

不在本规范范围内、也不要借改样式之机改动：文案、路由、信息架构、F1 业务逻辑（入库 / Agent 可见、可见集确认、脚本过期、交付阻断）。

## 色板

只允许下列界面色。新颜色先写入本节和 `:root` / `webui/src/theme.ts`，再使用。组件里不写裸十六进制。

| Token | 色值 | 用途 |
| --- | --- | --- |
| `--vac-bg` | `#1E1E1E` | 窗口、顶栏、底栏、页面底 |
| `--vac-panel` | `#252525` | 面板、卡片、侧栏 |
| `--vac-control` | `#2F2F2F` | 控件底、表头、选中滑块 |
| `--vac-border` / `--vac-hairline` | `#3A3A3A` | 边框、分隔线 |
| `--vac-text` | `#E6E6E6` | 主文字 |
| `--vac-text-2` | `#9A9A9A` | 次文字、未选中页签 |
| `--vac-accent` | `#4C8DFF` | **唯一强调色**（主按钮、链接、当前步骤、焦点环） |

片段类型色是低饱和备用，只标媒体种类，不作第二套强调色，也不拿来做渐变：

| Token | 色值 | 用途 |
| --- | --- | --- |
| `--vac-clip-video` | `#3E6FB0` | 视频片段 |
| `--vac-clip-audio` | `#3E8E6B` | 音频片段 |
| `--vac-clip-subtitle` | `#B08A3E` | 字幕片段 |
| `--vac-clip-effect` | `#8A5FB0` | 特效片段 |

状态色只表达成功 / 警告 / 失败，以及文案里已经写明的「青」（解说未批准）。它们不是品牌色，不能用来做装饰、发光或大面积铺底。

| Token | 色值 | 用途 |
| --- | --- | --- |
| `--vac-success` / `--vac-success-text` | `#49AA19` / `#52C41A` | 已确认、已批准、通过 |
| `--vac-warning` / `--vac-warning-text` | `#D89614` / `#FAAD14` | 警告、缺失、进行中需要注意 |
| `--vac-danger` / `--vac-danger-text` / `--vac-danger-soft` | `#DC4446` / `#FF4D4F` / `#FF7875` | 失败、退回、错误 |
| `--vac-status-cyan` | `#13A8A8` | 解说未批准。界面文案仍是「青」，不要改成音频绿 |

媒体井（监视器、缩略图、波形底下）用 `--vac-media: #000000`。叠在画面上的字用 `--vac-on-media: #FFFFFF`。这两色不是窗口底，也不要拿来做面板。

半透明叠层只用 `color-mix(in srgb, <上面的 token> <百分比>, transparent)`，不写 `rgb()` / `rgba()` / `hsl()`。

字幕正文颜色（ASS 预设、用户拾色）是内容数据，不是界面色板。只允许出现在 `webui/src/assEditorModel.ts` 和 `webui/src/components/AssStyleEditor.tsx`。

## 圆角、间距、字号

- 圆角只使用 2–4px：`--vac-radius: 3px`，`--vac-radius-window: 4px`，antd `borderRadius` 3 / `borderRadiusLG` 4 / `borderRadiusSM` 2。
- 唯一例外：顶栏分段滑块。槽 12px、滑块 9px，对齐 Lucian UX/UI Prompt #295 的形态。不要把这个圆角扩散到按钮、卡片、输入框或弹窗。
- 6px 及更小的状态点可以用 `50%`（`.stage-dot`、`.stage-next-dot`）。
- 间距只用 `--vac-page-x`（28px）、`--vac-page-y`（20px）、`--vac-section-gap`（24px）、`--vac-stack-gap`（20px）。侧栏不使用这组变量。
- 字号：正文 `--vac-font` 13px（antd `fontSize`），辅助 `--vac-font-sm` 12px（`fontSizeSM`），区块标题 `--vac-font-md` 14px。
- 描述表、属性表、同类状态行：行高 = 字号 × 1.25，即 `--vac-line: 1.25`。有边框的单元格纵向内边距 4px。不要把行撑成接近两行字的空档。
- 状态监控每行说明（`.status-desc`）保持 `--vac-line-read: 1.65`，列表项保持可读的上下留白，不要按描述表压扁。

## 禁止

- 渐变（`linear-gradient`、`radial-gradient`、`conic-gradient`、进度条 `strokeColor` 的 `{ from, to }`）。
- 发光、外阴影、内阴影。`box-shadow` 只允许 `none`。焦点用 `outline`。
- 液态玻璃、`backdrop-filter`、背景模糊、高光描边。
- 自绘控件外观。按钮、输入、选择、滑块、开关、表格、描述表、标签、菜单、弹窗一律 Ant Design 5 标准样式。允许写布局（栅格、轨道高度、省略），不允许另画推子帽、轨道凹槽或玻璃卡片。
- 侧栏必须实色：antd `Layout.Sider`，底色 `--vac-panel`，直角，antd 分隔线。`siderTheme` 保持 `inherit: false`。

## 动效

位移动效只加在顶栏页签滑块（`.app-nav.seg-nav::before`）：0.35s、`cubic-bezier(.3, 1.3, .5, 1)`，与 Lucian #295 的滑块相同。首次定位不播动画。`prefers-reduced-motion: reduce` 时滑块瞬时到位。

页面淡入、列表进入、按下缩放可以保留，但不得引入新的滑块、弹簧或发光过渡。不要在滑块以外的元素上做位移动效装饰。

## 描述表

属性面板（输入类型、Agent 可见、锁定、人工批准、允许的角色、更新时间、产物记录）和全站其它 `Descriptions` 使用 Ant Design 5 的描述表，紧凑规则写在 `webui/src/styles.css` 的 `.ant-descriptions`。不要换成手写表格，也不要为了收紧行高去改单元格文案。

## 检查

机器检查（Gate）：

```bash
cd webui && npm run lint:style
```

`npm run test:unit` 会连带跑它。脚本读取 `:root` 与 `theme.ts` 里声明的色值，并扫描 `webui/src`：

- `gradient(`、进度条双色 `strokeColor`
- `backdrop-filter` 不是 `none`，以及 `filter: blur`
- `box-shadow` / `boxShadow` 不是 `none`
- 不属于色板的 `#` 色值（字幕内容文件除外；与 token 完全相同的十六进制可以通过）
- `rgb()` / `rgba()` / `hsl()` / `hsla()`
- 超出 2–4px 的圆角（滑块 9px/12px 与状态点 `50%` 除外）

人工清单（脚本看不到的，改 UI 时逐项看）：

1. 交互控件是否仍是 Ant Design 5 默认外观，没有自绘推子、玻璃、发光边。
2. 侧栏是否实色、直角，没有另铺一层背景。
3. 动效是否只留在顶栏滑块上。
4. 强调色是否只有 `#4C8DFF`；片段色有没有被拿去当按钮或链接。
5. 属性表行高是否仍是字号的 1.25 倍，状态监控说明是否仍可读。
6. 有没有顺手改文案、路由或 F1 确认逻辑。
