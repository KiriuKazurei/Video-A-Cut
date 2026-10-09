param([Parameter(Mandatory=$true)][ValidatePattern('^[A-Za-z0-9_-]+$')][string]$Label)
$ErrorActionPreference = 'Stop'
$testRepoRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$testFiles = @{}
function Add-TestStoragePath([string]$Path) {
    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        $entry = Get-Item -LiteralPath $Path -Force
        $script:testFiles[$entry.FullName] = [long]$entry.Length
    } elseif (Test-Path -LiteralPath $Path -PathType Container) {
        foreach ($entry in Get-ChildItem -LiteralPath $Path -File -Recurse -Force -ErrorAction Stop) {
            $script:testFiles[$entry.FullName] = [long]$entry.Length
        }
    }
}
Add-TestStoragePath (Join-Path $testRepoRoot '.run-data')
foreach ($entry in Get-ChildItem -LiteralPath (Join-Path $testRepoRoot 'workers') -Directory -Recurse -Force -Filter '.run-data') { Add-TestStoragePath $entry.FullName }
$testRunBytes = [long](($testFiles.Values | Measure-Object -Sum).Sum)
foreach ($testRelative in @(
    'workers/python/tests', 'workers/python/scripts', 'workers/node/timeline-cli/test',
    'workers/node/timeline-cli/fixtures', 'workers/node/timeline-cli/scripts',
    'workers/node/export-worker/test', 'scripts', 'contracts/mp4-preview/v1/fixtures',
    'contracts/delivery-variant/v1/fixtures'
)) { Add-TestStoragePath (Join-Path $testRepoRoot $testRelative) }
foreach ($entry in Get-ChildItem -LiteralPath (Join-Path $testRepoRoot 'control-plane') -File -Recurse -Filter '*_test.go') { Add-TestStoragePath $entry.FullName }
foreach ($entry in Get-ChildItem -LiteralPath (Join-Path $testRepoRoot 'webui') -File -Filter '*.mjs') { Add-TestStoragePath $entry.FullName }
foreach ($entry in Get-ChildItem -LiteralPath (Join-Path $testRepoRoot 'workers/python') -File -Recurse -Filter '*.pyc') { Add-TestStoragePath $entry.FullName }
if (Test-Path -LiteralPath (Join-Path $testRepoRoot 'desktop/src')) {
    Add-TestStoragePath (Join-Path $testRepoRoot 'desktop/src')
    foreach ($entry in Get-ChildItem -LiteralPath (Join-Path $testRepoRoot 'build/desktop/debug/deps') -File -Filter 'video_auto_cut_desktop-*.exe' -ErrorAction SilentlyContinue) { Add-TestStoragePath $entry.FullName }
}
foreach ($mode in @('debug','release','app-debug','app')) {
    $runtimePython = Join-Path $testRepoRoot "build/desktop/$mode/runtime/workers/python"
    if (Test-Path -LiteralPath $runtimePython) {
        foreach ($entry in Get-ChildItem -LiteralPath $runtimePython -File -Recurse -Filter '*.pyc') { Add-TestStoragePath $entry.FullName }
    }
}
$testTotal = [long](($testFiles.Values | Measure-Object -Sum).Sum)
$testReport = [ordered]@{
    label=$Label; time=(Get-Date -Format o); limit_bytes=524288000
    run_data_bytes=$testRunBytes
    retained_test_source_fixture_runner_and_python_cache_bytes=($testTotal-$testRunBytes)
    total_bytes=$testTotal; total_MiB=[math]::Round($testTotal/1MB,3)
    within_limit=($testTotal -le 524288000)
}
$testReportRoot = Join-Path $testRepoRoot '.run-data/test-storage'
New-Item -ItemType Directory -Path $testReportRoot -Force | Out-Null
$testReport | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $testReportRoot ('storage-'+$Label+'.json')) -Encoding utf8
$testReport | ConvertTo-Json -Compress
if ($testTotal -gt 524288000) { throw 'Test storage exceeds 500 MiB. Stop tests and perform the full safe cleanup required by AGENTS.md.' }
