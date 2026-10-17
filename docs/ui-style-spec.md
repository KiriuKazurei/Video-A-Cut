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

Ant Design 的语义色 Tag / Badge（`success`、`warning`、`error`、`processing`，以及预设的 `blue`、`cyan` 等）可以继续用组件预设色。那是状态色，不是第二套强调色。交互和焦点只用 `#4C8DFF`，不要用 Tag 的预设蓝代替主按钮或焦点环。

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
- 唯一例外是顶栏分段滑块，对齐 Lucian UX/UI Prompt #295。豁免按整段选择器精确匹配，不能因为类名里带有 `seg-nav` 或 `dot` 就放行：
  - `12px`：`.app-nav.seg-nav`
  - `9px`：`.app-nav.seg-nav > .ant-menu-item`、`.app-nav.seg-nav > .ant-menu-submenu`、`.app-nav.seg-nav::before`
  - `50%`：`.stage-dot`、`.stage-next-dot`
  - 逗号分组时，每一段都要在名单里。`.seg-nav-card`、单独的 `.seg-nav`、`.stage-dot-extra` 都不豁免。JS/TSX 里没有选择器，不豁免 `9px`、`12px`、`50%`。
- 间距只用 `--vac-page-x`（28px）、`--vac-page-y`（20px）、`--vac-section-gap`（24px）、`--vac-stack-gap`（20px）。侧栏不使用这组变量。
- 字号：正文 `--vac-font` 13px（antd `fontSize`），辅助 `--vac-font-sm` 12px（`fontSizeSM`），区块标题 `--vac-font-md` 14px。
- 描述表、属性表、同类状态行：行高 = 字号 × 1.25，即 `--vac-line: 1.25`。有边框的单元格纵向内边距 4px。不要把行撑成接近两行字的空档。
- 状态监控每行说明（`.status-desc`）保持 `--vac-line-read: 1.65`，列表项保持可读的上下留白，不要按描述表压扁。

## 禁止

- 渐变（`linear-gradient`、`radial-gradient`、`conic-gradient`、`drop-shadow`，以及进度条 `strokeColor` 的 `{ from, to }` 或百分比键）。
- 发光、外阴影、内阴影。`box-shadow` / `boxShadow` 只允许 `none`。`text-shadow` / `textShadow` 同样只允许 `none`（字幕正文投影除外，见下）。焦点用 `outline`。
- 液态玻璃、`backdrop-filter` / `backdropFilter`（含 `-webkit-`）、`blur()`。
- 色板以外的颜色：裸十六进制、`%23` 百分号编码、命名色、`rgb` / `hsl` / `oklch` / `oklab` / `lab` / `lch` / `hwb`。`argb(` 不是 `rgb(`。`#face-btn` 这种选择器不是颜色。
- 自绘控件外观。按钮、输入、选择、滑块、开关、表格、描述表、标签、菜单、弹窗一律 Ant Design 5 标准样式。允许写布局（栅格、轨道高度、省略），不允许另画推子帽、轨道凹槽或玻璃卡片。
- 侧栏必须实色：antd `Layout.Sider`，底色 `--vac-panel`，直角，antd 分隔线。`siderTheme` 保持 `inherit: false`，并且自己带上 `Button: { primaryShadow: 'none', defaultShadow: 'none', dangerShadow: 'none' }`（`inherit: false` 不会继承 `appTheme` 的按钮阴影）。
- antd 写死、token 关不掉的运行时阴影，在 `styles.css` 里强制为 `none`：Slider 把手的 `box-shadow`、Tabs 滚动提示的 inset `box-shadow`、激活页签和 Spin 文案的 `text-shadow`。不给这些值开豁免。

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

`npm run test:unit` 会连带跑它，并跑 `style_lint_test.mjs`（构造样例）和 `style_smoke_test.mjs`（reduced-motion、描述表行高 1.25、`.app-nav.seg-nav` 圆角、侧栏按钮阴影 token）。脚本读取 `:root` 与 `theme.ts` 里声明的色值，并扫描 `webui/src` 的 `.css` / `.ts` / `.tsx`。声明值读到 `;` 或 `}` 为止，一行规则末尾没有分号也按同一条声明判断。

会查出：

- `gradient(`、`drop-shadow(`、`blur(`、进度条 `strokeColor` 的 `from`/`to` 或百分比键
- `backdrop-filter`、`backdropFilter`、`WebkitBackdropFilter` 不是 `none`
- `box-shadow` / `boxShadow`、`text-shadow` / `textShadow` 不是 `none`
- 色板以外的 `#` 色、`%23` 编码色、CSS 命名色、`rgb` / `rgba` / `hsl` / `hsla` / `oklch` / `oklab` / `lab` / `lch` / `hwb`（前面有字母的 `argb(` 不算）
- 超出 2–4px 的圆角，包括字符串 `'12px'` / `'50%'` 和 `border-top-left-radius` 等长手属性。滑块和状态点只按上一节的精确选择器豁免

不扫描、也不要靠它代替人工看的：

- `node_modules` 里 antd 自身的样式。运行时阴影靠本节的 token 和 `styles.css` 覆写关掉
- 注释，以及 `#face-btn` 这类标识符（`#` 后面的十六进制如果还接着字母、数字或 `-`，就不是颜色）
- 字幕内容文件 `assEditorModel.ts`、`AssStyleEditor.tsx` 里的拾色十六进制和正文 `textShadow`

人工清单（脚本看不到的，改 UI 时逐项看）：

1. 交互控件是否仍是 Ant Design 5 默认外观，没有自绘推子、玻璃、发光边。
2. 侧栏是否实色、直角，没有另铺一层背景。
3. 动效是否只留在顶栏滑块上。
4. 强调色是否只有 `#4C8DFF`；片段色有没有被拿去当按钮或链接。
5. 属性表行高是否仍是字号的 1.25 倍，状态监控说明是否仍可读。
6. 有没有顺手改文案、路由或 F1 确认逻辑。
