# Local operator startup (R3, first delivery)

This entry reuses the existing supervisor. It is Windows-only and requires PowerShell
7.2+, Node 22.12+, Python 3.11+ with `sqlite3`, FFmpeg with libx264/AAC and the timeline
filters, ffprobe, and a prebuilt WebUI. All dependencies are preinstalled: startup
does not download tools or models or run provider connectivity tests.

Use an explicit business directory outside the application/version directory:

```powershell
pwsh -NoProfile -File scripts/Start-VideoAutoCut.ps1 -DataDirectory 'E:\VideoAutoCutData' -DevelopmentBuild
pwsh -NoProfile -File scripts/Stop-VideoAutoCut.ps1 -DataDirectory 'E:\VideoAutoCutData'
```

`DevelopmentBuild` requires a local Go toolchain and the repository dependencies.
For a prepared executable, build it explicitly, then supply the resulting executable
and its `.runtime.json` sidecar. The build helper requires dependencies already in
the local Go module cache; it does not fetch missing dependencies:

```powershell
pwsh -NoProfile -File scripts/Build-ControlPlaneRuntime.ps1
pwsh -NoProfile -File scripts/Start-VideoAutoCut.ps1 -DataDirectory 'E:\VideoAutoCutData' -ControlBinary 'E:\Apps\VideoAutoCut\bin\control-plane.exe'
```

The binary path in this example is illustrative. Copy both output files from
`build/runtime/bin` to the chosen version directory. This is **not a complete portable
package**: the WebUI, worker sources, contracts and any production Node dependencies
must also belong to the same release. Packaging is a later R3 delivery.

Diagnostics can run separately with an explicit JSON result destination in an
existing directory. Output contains fixed resolution messages and numeric tool
versions, not environment variable values or raw provider responses:

```powershell
pwsh -NoProfile -File scripts/Get-VideoAutoCutDiagnostics.ps1 -DataDirectory 'E:\VideoAutoCutData' -DevelopmentBuild -ReportFile 'E:\Diagnostics\vac-startup.json'
```

`ready` is an individual observation, not pipeline acceptance. Fonts and voices are
`not_checked` here and are checked through the existing task preflight/font and TTS
selectors. A missing optional subtitle feature does not prevent other tasks from
starting. Database access and ownership are checked under the supervisor's lock.

## Data, restart and stop

- `control.db` and `delivery/` persist in the business directory. Each launch gets a
  separate `sessions/<id>/` directory with logs, generated worker configs and process
  identities. The exclusive `runtime.lock` prevents two supervisors using one store.
- `ProfileFile` bootstraps revision 1 only when it is absent. On subsequent launches
  its profile ID and revision select the **stored** immutable profile. To use changed
  settings, save a new revision in the application and select that revision in the
  launch profile file. File edits do not overwrite a stored revision.
- Restart checks the previous supervisor and recorded worker identities. A live
  process from the prior session blocks startup. PID reuse does not authorize killing
  another process. Abnormal supervisor loss needs identity review/manual recovery.
- The stop entry writes a request marker only after checking the supervisor's PID,
  executable path and start time. It reports **requested**, not **completed**. The live
  supervisor drains workers for up to 15 seconds and stops its verified process handles.
  A control-plane exit ends the session with an error, allowing a deliberate restart
  against the same store rather than leaving workers indefinitely disconnected.
- Node export work receives cancellation. Python workers stop claiming further tasks;
  an existing Python task can exceed the drain interval and be forcibly stopped.
  Descendant cleanup and interruption recovery still require runtime acceptance.
- Logs/configs can contain business paths and selected provider endpoints. Keep the
  business directory private; this delivery does not add a redacted support-log bundle.

The original `Start-LocalPipeline.ps1` still defaults to isolated `.run-data` sessions
when `DataDirectory` is omitted. Do not use production data for test sessions.

## Upgrade and rollback boundary

While holding the data lock, startup refuses a database containing migrations absent
from this build's actual migration inventory, checks SQLite consistency and creates a new consistent SQLite backup
in the session directory before the control plane can migrate it. WAL data is included
through SQLite's backup API, not by copying a live `.db` file. Backups are never overwritten.
Do not run a second program directly against the same database outside this supervisor.

The manifest hash protects against accidentally pairing a binary with a different
manifest. It is not a signature or a guarantee of schema compatibility with older
programs. An old release without this guard must not be pointed at a newer database.

Rollback is manual and offline: stop all verified processes, retain the entire current
business directory (including post-upgrade results and WAL files), restore the prior
database backup to a **new** recovery directory with the matching delivery tree, and
start the matching older version there. Resolve post-upgrade business increments before
choosing a production directory. Never overwrite new business data with an old backup.

This delivery has static code review only. Startup, occupied-port behavior, backup,
worker cancellation, restart/history access and clean-environment acceptance have not
been executed. The full portable package and runtime-capabilities API/UI remain planned.
