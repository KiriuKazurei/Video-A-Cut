# Export Worker

Node Worker that turns queued `export` tasks into delivery packages by running
the Phase 1 timeline CLI. It talks to the control plane only through MCP
(`/mcp`); it never opens SQLite and never names its own agent id or role.

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

`claim_task` → `report_progress` → `get_asset` (reads the `edl` artifact, a
root-relative path) → CLI `build` into `<output_prefix>/<asset_id>/<task_id>`
with a hard timeout → `submit_delivery`. The control plane re-validates the
package with the same rules as human import, replaces the asset's artifact map
and marks it `exported` in one transaction.

Any CLI or path failure becomes `fail_task` with the reason. A lost lease
(`lease_expired`) abandons the task without reporting, since the task may
already belong to another worker.

## Test

```sh
npm run check && npm test
```
