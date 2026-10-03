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

界面按类似 Premiere 的工作流分成六个页面，顶部 Ant Design `Menu` 切换（窄屏时多余页签自动折叠进「…」子菜单），地址栏用 hash 深链（如 `#/edit`）。所有页面常驻挂载、只用 `hidden` 切换，切页不会丢失未保存的表单与选段。

| 页面 | 内容 | 接口（页头标签，与 `src/navigation.ts` 一致） |
| --- | --- | --- |
| 01 导入 `#/import` | 源监视器、素材概览、登记原始录像、导入交付包、资产治理面板 | `API-01 · 03~06 · 42 · 44/45 · 48/49` |
| 02 粗剪 `#/assembly` | 吸顶源监视器、导入运行、探测与切分参数、源时间线（候选 C / 已选 V1）、候选分页、拆分合并、生成短片 | `API-03 · 06~16 · 19` |
| 03 准备 `#/prepare` | 处理预设版本、服务商发现/诊断、运行条件预检、外发授权；启动固定预设流程后自动切到「编辑」页并选中新流程 | `API-17~21 · 23~26` |
| 04 编辑 `#/edit` | 节目监视器 + 场景证据/解说草稿审查（卡片页签）+ 序列时间线（V1 场景 / A1 解说）、流程状态与阶段 | `API-28~38` |
| 05 导出 `#/export` | 节目监视器预览/试听交付文件、交付文件列表与 ZIP、流程交付 ZIP 与人工验收 | `API-28 · 30 · 39~41 · 43~46` |
| 06 监控 `#/monitor` | 任务派发与编排控制台、最近审计、审计日志面板、事件流 | `API-47~50` |

未接入界面的接口：API-02（单资产读取；界面使用列表快照）、API-22（保存预设的别名路由；界面用通用保存 API-20）、API-27（早期基础预检；界面只用固定版本预检 API-25）、API-51/52（旧资产级解说批准/撤销；界面只用流程级解说批准/撤销 API-33/34）。

固定区域：左侧「项目 · 资产」面板（API-01），右侧「属性 · 资产治理」面板（API-03），底部状态栏显示 SSE 连接（API-50）。监视器只播放受控文件键或证据地址，不把 artifacts 当下载地址。界面组件：`src/navigation.ts`（页面定义）、`src/pages/Pages.tsx`、`src/components/{Monitor,Timeline,MediaBin,Inspector}.tsx`。

视觉：全部操作控件使用 Ant Design 5（`antd` + `@ant-design/icons`，`ConfigProvider` 设 `zhCN` 与 `theme.darkAlgorithm`，主色、圆角、控件尺寸保持默认）。整体模仿 Premiere：布局、容器、页头页脚共用同一底色 `#1c1d21`（`src/main.tsx` 里的 `colorBgBase/colorBgLayout/colorBgContainer`），中间工作区不用色块分隔，只留细线。顶部页签和展示窗口使用液态玻璃（ui-ux-pro-max「Spatial UI (VisionOS)」：半透明填充、`backdrop-filter: blur(40px) saturate(180%)`、内侧高光描边、深度阴影）：六个页签放在玻璃胶囊里，当前页是蓝色调玻璃高亮（白字），窄屏的「…」折叠弹层同样材质；监视器与短片预览窗口是玻璃边框。左右侧栏是 antd `Layout.Sider` 标准外观（直角、统一底色、antd 分隔线），不加玻璃。系统开启「减少透明度」或浏览器不支持 `backdrop-filter` 时退化为不透明面板，「减少动态效果」时关闭过渡。`src/styles.css` 只保留布局、媒体尺寸、时间线轨道和玻璃材质。

浏览器回归脚本（`browser_acceptance.mjs` / `provider_browser_acceptance.mjs`）经由顶部菜单进入页面（窄屏时先展开折叠子菜单），并断言只有目标页可见；点击、输入和下拉选择都只作用于可见元素，隐藏页里的同名控件不会被点到。任意宽度下只要有可见元素越过视口右边缘（`pageOverflow`）即判失败。

`control-plane/control.json` 示例（路径须换成本机绝对路径）：

```json
{
  "http_addr": "127.0.0.1:8787",
  "delivery_root": "E:/AI/Video Auto Cut/.run-data/phase1-acceptance",
  "web_root": "E:/AI/Video Auto Cut/webui/dist"
}
```

`delivery_root` 省略时文件交付关闭；`web_root` 省略时由 Vite 或同源反向代理托管前端。运行 `npm run build` 后设置 `web_root`，Go 会从同一端口提供 WebUI 和 `/api`。两项路径均须是已存在的绝对目录。当前验收状态见 `docs/二阶段验收记录-2026-09-29.md`。
