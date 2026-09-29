# WebUI 控制台

二阶段人用控制台。React/TypeScript + Vite；只调用 Go 的 REST/SSE，不持有业务规则。详见 `docs/二阶段开发文档.md`。

开发时先启动 Go 控制面（默认 `127.0.0.1:8787`），再在本目录运行：

```bash
npm install
npm run check
npm run dev
```

打开 `http://127.0.0.1:5173`。Vite 仅监听本机，并把 `/api` 代理到 Go；代理移除开发页的 Origin，Go 本机来源校验仍保持启用。独立端口验收可设置 `VAC_API_TARGET=http://127.0.0.1:8788` 后运行 `npm run dev -- --port 5174`。

当前 UI 可读取资产/审计、修改治理字段、创建并跟踪任务，SSE 连接或重连时重新取 REST 快照。配置 Go 的 `delivery_root` 后，可输入该目录内 CLI 交付包的相对目录，导入 `delivery-manifest.json`，预览视频、试听音频、下载单个产物或保留原目录结构的交付 ZIP。任务列表与取消/重试接口尚未提供，UI 不承诺这些能力。

`control-plane/control.json` 示例（路径须换成本机绝对路径）：

```json
{
  "http_addr": "127.0.0.1:8787",
  "delivery_root": "E:/AI/Video Auto Cut/.run-data/phase1-acceptance",
  "web_root": "E:/AI/Video Auto Cut/webui/dist"
}
```

`delivery_root` 省略时文件交付关闭；`web_root` 省略时由 Vite 或同源反向代理托管前端。运行 `npm run build` 后设置 `web_root`，Go 会从同一端口提供 WebUI 和 `/api`。两项路径均须是已存在的绝对目录。当前验收状态见 `docs/二阶段验收记录-2026-09-29.md`。
