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

界面按类似 Premiere 的工作流分成六个页面，顶部页签切换，地址栏用 hash 深链（如 `#/edit`）。所有页面常驻挂载、只用 `hidden` 切换，切页不会丢失未保存的表单与选段。

| 页面 | 内容 | 接口（见接口清单编号） |
| --- | --- | --- |
| 01 导入 `#/import` | 源监视器、素材概览、登记原始录像、导入交付包、资产治理面板 | API-01~06、42，预览用 44/45 |
| 02 粗剪 `#/assembly` | 吸顶源监视器、导入运行、探测与切分参数、源时间线（候选 C / 已选 V1）、候选分页、拆分合并、生成短片 | API-07~16、19 |
| 03 准备 `#/prepare` | 处理预设版本、服务商发现/诊断、运行条件预检、外发授权 | API-17~21、23~26 |
| 04 编辑 `#/edit` | 节目监视器 + 场景证据/解说草稿审查面板 + 序列时间线（V1 场景 / A1 解说）、流程状态与阶段 | API-28~38 |
| 05 导出 `#/export` | 节目监视器预览/试听交付文件、交付文件列表与 ZIP、流程交付 ZIP 与人工验收 | API-39~41、43~46 |
| 06 监控 `#/monitor` | 任务派发与编排控制台、最近审计、审计日志面板 | API-47~49 |

固定区域：左侧「项目 · 资产」面板（API-01），右侧「属性 · 资产治理」面板（API-03），底部状态栏显示 SSE 连接（API-50）。监视器只播放受控文件键或证据地址，不把 artifacts 当下载地址。界面组件：`src/navigation.ts`（页面定义）、`src/pages/Pages.tsx`、`src/components/{Monitor,Timeline,MediaBin,Inspector}.tsx`；样式令牌集中在 `src/styles.css`（深色专业剪辑主题，参考 ui-ux-pro-max 的 Short Video Editor 配色）。

浏览器回归脚本（`browser_acceptance.mjs` / `provider_browser_acceptance.mjs`）会先点击对应页签再操作。

`control-plane/control.json` 示例（路径须换成本机绝对路径）：

```json
{
  "http_addr": "127.0.0.1:8787",
  "delivery_root": "E:/AI/Video Auto Cut/.run-data/phase1-acceptance",
  "web_root": "E:/AI/Video Auto Cut/webui/dist"
}
```

`delivery_root` 省略时文件交付关闭；`web_root` 省略时由 Vite 或同源反向代理托管前端。运行 `npm run build` 后设置 `web_root`，Go 会从同一端口提供 WebUI 和 `/api`。两项路径均须是已存在的绝对目录。当前验收状态见 `docs/二阶段验收记录-2026-09-29.md`。
