param([ValidatePattern('^[A-Za-z0-9_-]+$')][string]$Label='full-cleanup')
$ErrorActionPreference='Stop'
$cleanupRepo=[IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$cleanupRun=Join-Path $cleanupRepo '.run-data'
$evidence=Join-Path $cleanupRun 'test-cleanup-2026-10-07'
# Reviewed literal inventory only. No recursive extension-based deletion.
$directoryNames=@(
'go-cache','mp4-quality-tmp','minor-fixes-2026-10-05/python-tmp',
'minor-fixes-2026-10-05/browser/edge-profile','minor-fixes-2026-10-05/browser-recheck/edge-profile',
'project-goal-audit-2026-10-05/go-tmp','project-goal-audit-2026-10-05/node-tmp',
'project-goal-audit-2026-10-05/python-tmp','project-goal-audit-2026-10-05/python-recheck-tmp',
'project-goal-audit-2026-10-05/browser/edge-profile','project-goal-audit-2026-10-05/browser-recheck/edge-profile',
'mp4-preview-quality-review-2026-10-07/browser/edge-profile',
'mp4-preview-sync-fixes-2026-10-07/real-chain/browser/edge-profile','preview-context-check/edge-profile'
)
# report.json identifies phase6_synthetic; renderer-audit.mjs creates synthetic-package.
# Preserve all JSON/text evidence before deleting these generated temporary packages.
$generatedEnvironments=@('phase6-e2e-af440266/delivery','mp4-preview-quality-review-2026-10-07/synthetic-package')
$fileGroups=[ordered]@{
'minor-fixes-2026-10-05'=@('browser-recheck.log','browser.log','build.log','frontend-tests.log','legacy-discovery.log','python-tests.log','browser/edge.log','browser/failure.png','browser/preview-context-fixed.png','browser/task-controls-375.png','browser-recheck/edge.log','browser-recheck/preview-context-fixed.png','browser-recheck/task-controls-375.png')
'project-goal-audit-2026-10-05'=@('browser-recheck.log','browser.log','go-vet.log','node-tests.log','prepared-e2e.log','python-tests-recheck.log','python-tests.log','review-fixes-static.log','webui-build.log','browser/edge.log','browser-recheck/edge.log','browser-recheck/preview-context-fixed.png','browser-recheck/task-toolbar-375.png')
'mp4-preview-quality-review-2026-10-07'=@('browser-audit.log','foreign.mp4','go-audit-repro.log','go-tests.log','raw-explicit-rate.mp4','raw-multicut.mp4','renderer-audit.log','timeline-tests.log','web-build.log','web-tests.log','worker-tests.log','browser/edge.log','browser/mp4-invalidated-375.png','experiment-output/final.mp4')
'preview-context-check'=@('build.log','edge.log','failure.png','preview-context-fixed.png','source-before-fix.png','static-checks.log','unit-tests.log')
'mp4-preview-sync-fixes-2026-10-07'=@('go-audit.log','go-first.log','go-regression.log','real-chain-browser.log','real-chain.log','timeline-first.log','web-build.log','web-tests.log','worker-first.log','real-chain/final.mp4','real-chain/browser/edge.log','real-chain/browser/invalidated-375.png')
'mp4-preview-framework-2026-10-06'=@('go-preview.log','node-preview.log','store-blueprint.log','webui-check.log')
'phase6-e2e-af440266'=@('control-plane.exe','control.db','control.db-shm','control.db-wal','control.log','delivery.zip','export.log')
''=@('preview-go-tests.log','preview-go-tests-verified.log')
}
$targets=[Collections.Generic.List[string]]::new()
foreach($relative in ($directoryNames+$generatedEnvironments)){$targets.Add((Join-Path $cleanupRun $relative))}
foreach($group in $fileGroups.GetEnumerator()){foreach($name in $group.Value){$targets.Add((Join-Path $cleanupRun ($group.Key+'/'+$name).TrimStart('/')))}}
$targets.Add((Join-Path $cleanupRepo 'workers/python/scripts/__pycache__'))
$targets=@($targets|Where-Object{Test-Path -LiteralPath $_})
function Assert-Target([string]$target){
$full=[IO.Path]::GetFullPath($target)
if(-not $full.StartsWith($cleanupRepo+'\',[StringComparison]::OrdinalIgnoreCase)){throw "Outside repository: $full"}
$parent=$full
while($parent -and $parent.StartsWith($cleanupRepo,[StringComparison]::OrdinalIgnoreCase)){
if((Get-Item -LiteralPath $parent -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw "Reparse point: $parent"}
$parent=Split-Path -Parent $parent
}
if(Test-Path -LiteralPath $full -PathType Container){
if(@(Get-ChildItem -LiteralPath $full -Recurse -Force|Where-Object{$_.Attributes -band [IO.FileAttributes]::ReparsePoint}).Count){throw "Child reparse point: $full"}
}
}
foreach($target in $targets){Assert-Target $target}
Assert-Target $cleanupRun
$processes=@(Get-CimInstance Win32_Process|Where-Object{
$_.Name -match '^(go|node|ffmpeg|ffprobe|msedge|control-plane|.*[.]test)[.]exe$' -and $_.CommandLine -and
($_.CommandLine.Contains($cleanupRun) -or ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($cleanupRun+'\',[StringComparison]::OrdinalIgnoreCase)))
})
if($processes.Count){throw ('Related tests still running: '+(($processes|ForEach-Object{$_.Name+':'+$_.ProcessId}) -join ', '))}
$before=[long]((Get-ChildItem -LiteralPath $cleanupRun -File -Recurse -Force|Measure-Object Length -Sum).Sum)
$protectedHashes=@{}
foreach($relative in @('workers/node/timeline-cli/fixtures','contracts/mp4-preview/v1/fixtures','.run-data/sync-backup-2026-10-07')){
$root=Join-Path $cleanupRepo $relative
if(Test-Path -LiteralPath $root){foreach($file in Get-ChildItem -LiteralPath $root -File -Recurse -Force){$protectedHashes[$file.FullName]=(Get-FileHash -LiteralPath $file.FullName).Hash}}
}
New-Item -ItemType Directory -Path $evidence -Force|Out-Null
$targets|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $evidence "$Label-targets.json") -Encoding utf8
$verification=@()
foreach($target in $targets|Where-Object{[IO.Path]::GetExtension($_) -eq '.log'}){
$verification+='Report source: '+$target
$verification+=@(Get-Content -LiteralPath $target|Select-String -Pattern '^ok\s|^FAIL|^--- (PASS|FAIL)|^=== RUN|^ℹ|^✔|^✖|passed|failed|succeeded'|ForEach-Object{$_.Line})
}
$verification|Set-Content -LiteralPath (Join-Path $evidence "$Label-verification-report.txt") -Encoding utf8
foreach($relative in $generatedEnvironments){
$root=Join-Path $cleanupRun $relative
if(Test-Path -LiteralPath $root){
foreach($file in Get-ChildItem -LiteralPath $root -File -Recurse -Force|Where-Object{$_.Extension -in @('.json','.txt','.md','.srt','.xml','.otio')}){
$destination=Join-Path $evidence ('preserved-results/'+$file.FullName.Substring($cleanupRun.Length+1))
New-Item -ItemType Directory -Force (Split-Path -Parent $destination)|Out-Null
Copy-Item -LiteralPath $file.FullName -Destination $destination
if((Get-FileHash -LiteralPath $destination).Hash -ne (Get-FileHash -LiteralPath $file.FullName).Hash){throw 'Evidence copy mismatch'}
}
}
}
foreach($target in $targets){Assert-Target $target; Remove-Item -LiteralPath $target -Recurse -Force}
foreach($path in $protectedHashes.Keys){if((Get-FileHash -LiteralPath $path).Hash -ne $protectedHashes[$path]){throw "Protected file changed: $path"}}
$after=[long]((Get-ChildItem -LiteralPath $cleanupRun -File -Recurse -Force|Measure-Object Length -Sum).Sum)
$report=[ordered]@{label=$Label;run_data_before_bytes=$before;run_data_after_bytes=$after;run_data_freed_bytes=$before-$after;target_count=$targets.Count;protected_files_verified=$protectedHashes.Count;process_check='no_related_test_processes';reparse_check='targets_parents_descendants_checked';retained='sources_fixtures_scripts_text_reports_JSON_results_source_backup'}
$report|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $evidence "$Label.json") -Encoding utf8
$report|ConvertTo-Json -Compress
& (Join-Path $PSScriptRoot 'Measure-TestStorage.ps1') -Label "$Label-after"
