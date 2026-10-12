# WebUI 控制台

二阶段人用控制台。React/TypeScript + Vite；只调用 Go 的 REST/SSE，不持有业务规则。详见 `docs/二阶段开发文档.md`。

接手当前完整界面时，请先阅读 [前端接口清单与重构交接](../docs/前端接口清单与重构交接.md) 和 [机器可读接口清单](../docs/frontend-api-inventory.json)。文档覆盖录屏导入、服务商、预设、内容审查、交付、任务、审计与 SSE，并区分流程级取消/重试与尚未提供的单任务控制接口。

开发时先启动 Go 控制面（默认 `127.0.0.1:8787`），再在本目录运行：

```bash
npm install
npm run check
npm run dev
```

打开 `http://127.0.0.1:5173`。Vite 仅监听本机，并把 `/api` 代理到 Go；代理移除开发页的 Origin，Go 本机来源校验仍保持启用。独立端口验收可设置 `VAC_API_TARGET=http://127.0.0.1:8788` 后运行 `npm run dev -- --port 5174`。

当前 UI 可读取资产/审计、修改治理字段、创建并跟踪任务，SSE 连接或重连时重新取 REST 快照。配置 Go 的 `delivery_root` 后，可输入该目录内 CLI 交付包的相对目录，导入 `delivery-manifest.json`，预览视频、试听音频、下载单个产物或保留原目录结构的交付 ZIP。任务列表与取消/重试接口尚未提供，UI 不承诺这些能力。

## 界面结构（剪辑工作区）

界面按类似 Premiere 的工作流分成五个工作区，顶部 Ant Design `Menu` 切换（窄屏时多余页签自动折叠进「…」子菜单），地址栏用 hash 深链（如 `#/editing/import`）。所有页面常驻挂载、只用 `hidden` 切换，切页不会丢失未保存的表单与选段。

| 工作区 | 分区 / 内容 | 接口（与 `src/navigation.ts` 一致） |
| --- | --- | --- |
| 资源管理 `#/resources` | 资产库、资源预览、属性治理 | `API-01 · 03 · 43~45` |
| 编辑 `#/editing/{import|content|review}` | 导入与粗剪（源监视器 + 源时间线直接选段）· 内容编辑 · 审查 | `API-03~16 · 19 · 28~38 · 42` |
| 导出 `#/export/{delivery|acceptance|tasks}` | 交付 · 人工验收 · 任务监控 | `API-28 · 30 · 39~41 · 43~49` |
| TTS `#/tts` | 文本试听、声音调控、模板 | TTS API |
| 设置 `#/settings/{profiles|providers|preparation|logs}` | 处理预设 · 服务商 · 运行准备 · 日志 | `API-17~21 · 23~26 · 49~50` |

未接入界面的接口：API-02（单资产读取；界面使用列表快照）、API-22（保存预设的别名路由；界面用通用保存 API-20）、API-27（早期基础预检；界面只用固定版本预检 API-25）、API-51/52（旧资产级解说批准/撤销；界面只用流程级解说批准/撤销 API-33/34）。

固定区域：左侧「项目 · 资产」面板（API-01），右侧「属性 · 资产治理」面板（API-03），底部状态栏显示 SSE 连接（API-50，支持 11 种事件含 audit.changed 及断线重连）。监视器只播放受控文件键或证据地址，不把 artifacts 当下载地址。界面组件：`src/navigation.ts`（页面定义）、`src/pages/Pages.tsx`、`src/components/{Monitor,Timeline,MediaBin,Inspector}.tsx`。

视觉：全部操作控件使用 Ant Design 5（`antd` + `@ant-design/icons`，`ConfigProvider` 设 `zhCN` 与 `theme.darkAlgorithm`，主色、圆角、控件尺寸保持默认）。整体模仿 Premiere：布局、容器、页头页脚共用同一底色 `#1c1d21`（`src/theme.ts` 里的 `colorBgBase/colorBgLayout/colorBgContainer`），中间工作区不用色块分隔，只留细线。顶部页签和展示窗口使用液态玻璃（ui-ux-pro-max「Spatial UI (VisionOS)」：半透明填充、`backdrop-filter: blur(40px) saturate(180%)`、内侧高光描边、深度阴影）：五个工作区页签使用玻璃材质（设置在页头右侧按钮，非整页胶囊栏），当前页是蓝色调玻璃高亮（白字），窄屏的「…」折叠弹层同样材质；监视器与短片预览窗口是玻璃边框。左右侧栏是 antd `Layout.Sider`：纯色底（`lightSiderBg` = `colorBgContainer`，无渐变、无透明、无 `backdrop-filter`）、直角、antd 分隔线；侧栏内容包在独立的 `ConfigProvider`（`siderTheme`，`inherit: false`）里，不继承中间工作区的组件级覆写，按钮、搜索框、列表与选中项（`controlItemBgActive`）、卡片、描述表、标签、提示框都是 antd 默认暗色外观，`styles.css` 不对侧栏内部写外观样式。页面不设环境光层，唯一的光晕在页头中央玻璃页签背后，不延伸到侧栏。系统开启「减少透明度」或浏览器不支持 `backdrop-filter` 时退化为不透明面板，「减少动态效果」时关闭过渡。`src/styles.css` 只保留布局、媒体尺寸、时间线轨道和玻璃材质。

浏览器回归脚本（`browser_acceptance.mjs` / `provider_browser_acceptance.mjs`）已对齐五工作区路由：
`#/resources`、`#/editing/{import|content|review}`、`#/export/{delivery|acceptance|tasks}`、`#/settings/{profiles|providers|preparation|logs}`、`#/tts`（仍接受旧六页别名）。
经由顶部工作区页签（设置走页头按钮）+ 二级 Tabs/菜单进入分区；窄屏时先展开折叠子菜单或「项目」抽屉；断言只有目标工作区可见。点击、输入和下拉选择都只作用于可见元素。任意宽度下只要有可见元素越过视口右边缘（`pageOverflow`）即判失败。
本机跑法见 `/workspace/vac-demo/run_browser_tests.py`（八种模式）与 `run_provider_test.py`。

`control-plane/control.json` 示例（路径须换成本机绝对路径）：

```json
{
  "http_addr": "127.0.0.1:8787",
  "delivery_root": "E:/AI/Video Auto Cut/.run-data/phase1-acceptance",
  "web_root": "E:/AI/Video Auto Cut/webui/dist"
}
```

`delivery_root` 省略时文件交付关闭；`web_root` 省略时由 Vite 或同源反向代理托管前端。运行 `npm run build` 后设置 `web_root`，Go 会从同一端口提供 WebUI 和 `/api`。两项路径均须是已存在的绝对目录。当前验收状态见 `docs/二阶段验收记录-2026-09-29.md`。

## 统一导航与「下一步」（Stage 3a）

- `src/pipeline.ts`：纯函数，从资产 / 导入运行 / 内容流程的真实状态推导五个阶段（导入与粗剪 → 准备 → 生成 → 审查 → 交付）的状态、推荐的「下一步」，以及位置面包屑。单元测试需 Node 22 的类型剥离：`node --experimental-strip-types pipeline_test.mjs`（`navigation_smoke.mjs` 同理可用 Playwright）。
- 页头阶段条（`PipelineStepper`）与页内位置条（`ContextBar`：面包屑 + 下一步）共用这份推导；编辑 / 导出 / 设置的二级菜单用同一阶段模型显示状态点。路由与深链不变。
- 导航冒烟：`PLAYWRIGHT_CORE=<playwright-core/index.mjs> CHROME=<chrome> node navigation_smoke.mjs http://127.0.0.1:8816 [截图目录]`（连 `vite` 开发服务器可同时检查 React 的 DOM 嵌套警告）。
