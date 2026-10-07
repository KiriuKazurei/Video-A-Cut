# Export & Preview Worker

Node Worker that turns queued `export` tasks into delivery packages and queued
`preview` tasks into rendered MP4 previews. It talks to the control plane only
through MCP (`/mcp`); it never opens SQLite and never names its own agent id or role.

## Run

```sh
VAC_WORKER_TOKEN=<plain bearer token> node src/main.mjs --config <worker.json> [--once]
```

`--once` processes at most one task and exits (0 on success or idle, 1 on a
reported failure, 2 on configuration/transport error). Without it the worker
polls until SIGINT/SIGTERM.

`worker.json` (absolute paths; the token stays in the environment):

```json
{
  "mcp_url": "http://127.0.0.1:8787/mcp",
  "token_env": "VAC_WORKER_TOKEN",
  "delivery_root": "E:\\...\\root",
  "cli_path": "E:\\...\\workers\\node\\timeline-cli\\src\\cli.mjs",
  "adapters_path": "E:\\...\\workers\\node\\timeline-cli\\adapters\\index.mjs",
  "output_prefix": "deliveries",
  "poll_interval_ms": 2000,
  "heartbeat_interval_ms": 5000,
  "task_timeout_ms": 600000
}
```

`delivery_root` must be the same directory as the control plane's
`delivery_root`. The heartbeat interval must be shorter than `lease_seconds`.

## Flow

### Export Tasks (`task.type === 'export'`)
`claim_task` → `report_progress` → `get_asset` (reads the `edl` artifact, a
root-relative path) → CLI `build` into `<output_prefix>/<asset_id>/<task_id>`
with a hard timeout → `submit_delivery`. The control plane re-validates the
package with the same rules as human import, replaces the asset's artifact map
and marks it `exported` in one transaction.

### Preview Tasks (`task.type === 'preview'`)
When the control plane exposes `submit_preview`, the worker probes host
rendering capability (`ffmpeg`, `ffprobe`, preset `mp4-720p-v1`, subtitle fonts)
and includes `preview_capability` in periodic heartbeats.

Task flow: `claim_task` → `get_task_input` → resolve package & target preview
directory (`previews/<asset_id>/<revision_id>/<preview_id>`) → breakpoint
recovery check (`final.mp4` + `preview-receipt.json` + `preview-manifest.json`)
→ if not recovered: invoke preview renderer with progress streaming →
`submit_preview`.

Any task-level failure becomes `fail_task` with the reason. A lost lease
(`lease_expired`, `conflict`, etc.) aborts execution and abandons the task
without reporting, preventing stale submissions.

## Test

ASS tasks use an independent `delivery_variant_capability` heartbeat for fixed
`ass-basic-v1`. `delivery_variant` reads frozen SRT/style inputs, copies the
declared parent files into `delivery-variants/<asset_id>/<variant_id>`, and
calls `submit_delivery_variant`. Go independently reconstructs the expected
ASS before registration. Generation needs no FFmpeg/font installation; the
real rendering acceptance is a separate check. Original export packages and
the existing MP4 preset are preserved. See `contracts/delivery-variant/v1/README.md`.

```sh
npm run check && npm test
```
