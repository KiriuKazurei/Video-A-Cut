param([int]$Port=8805)
$ErrorActionPreference='Stop'
$taskRoot=Split-Path -Parent $PSScriptRoot
$taskEvidence=Get-ChildItem -LiteralPath (Join-Path $taskRoot '.run-data') -Directory -Filter 'phase5-e2e-*' | Sort-Object LastWriteTime -Descending | Where-Object {
    $taskReport=Join-Path $_.FullName 'report.json'
    (Test-Path -LiteralPath $taskReport) -and ((Get-Content -LiteralPath $taskReport -Raw | ConvertFrom-Json).engineering -eq 'passed')
} | Select-Object -First 1
if (!$taskEvidence) { throw 'No passing phase5 fixture found. Run python workers/python/scripts/verify_phase5_workflow.py first.' }
if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) { throw "Port $Port is already in use; choose -Port." }
$taskPreview=Join-Path $taskRoot ('.run-data/phase45-review/'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $taskPreview -Force | Out-Null
$taskDB=Join-Path $taskPreview 'review.db'
& python -c 'import sqlite3,sys; a=sqlite3.connect(sys.argv[1]); b=sqlite3.connect(sys.argv[2]); a.backup(b); b.close(); a.close()' (Join-Path $taskEvidence.FullName 'control.db') $taskDB
if ($LASTEXITCODE -ne 0) { throw 'Fixture database backup failed.' }
$taskConfig=Get-Content -LiteralPath (Join-Path $taskEvidence.FullName 'control.json') -Raw | ConvertFrom-Json
$taskConfig.http_addr="127.0.0.1:$Port"
$taskConfig.web_root=Join-Path $taskRoot 'webui/dist'
$taskConfig | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $taskPreview 'control.json') -Encoding utf8
$taskBinary=Join-Path $taskPreview 'control-plane.exe'
$taskOldCache=$env:GOCACHE
try { $env:GOCACHE=Join-Path $taskRoot '.run-data/go-cache'; Push-Location (Join-Path $taskRoot 'control-plane'); try { & go build -o $taskBinary . } finally { Pop-Location } } finally { $env:GOCACHE=$taskOldCache }
if ($LASTEXITCODE -ne 0) { throw 'Control plane build failed.' }
Write-Host "Synthetic UI review: http://127.0.0.1:$Port/"
Write-Host 'Select phase5_synthetic. This fixture is mock content; real model acceptance remains pending.'
Write-Host 'This server runs in this window. Press Ctrl+C to stop. It does not start Workers or send media.'
& $taskBinary -config (Join-Path $taskPreview 'control.json') -db $taskDB
