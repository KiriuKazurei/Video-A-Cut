param([string]$Label='tauri-full-cleanup')
$ErrorActionPreference='Stop'
$cleanupRepo=[IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$cleanupRun=Join-Path $cleanupRepo '.run-data'
function Assert-CleanupPath([string]$Path) {
  $full=[IO.Path]::GetFullPath($Path)
  if(!$full.StartsWith($cleanupRepo+'\',[StringComparison]::OrdinalIgnoreCase)){throw "Outside repository: $full"}
  for($cursor=$full;$cursor;$cursor=Split-Path -Parent $cursor){
    $entry=Get-Item -LiteralPath $cursor -Force -ErrorAction SilentlyContinue
    if($entry -and ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint)){throw "Reparse point: $cursor"}
  }
  if(Test-Path -LiteralPath $full -PathType Container){
    if(@(Get-ChildItem -LiteralPath $full -Recurse -Force|Where-Object {$_.Attributes -band [IO.FileAttributes]::ReparsePoint}).Count){throw "Child reparse point: $full"}
  }
}
Assert-CleanupPath $cleanupRun
$testRoots=@($cleanupRun)+@(Get-ChildItem -LiteralPath (Join-Path $cleanupRepo 'workers') -Directory -Recurse -Force -Filter '.run-data'|ForEach-Object FullName)
foreach($testRoot in $testRoots){Assert-CleanupPath $testRoot}
$processes=@(Get-CimInstance Win32_Process|Where-Object {
  $candidate=$_
  $candidate.Name -match '^(.*control-plane.*|msedge|msedgewebview2|python|node|ffmpeg|ffprobe|go|cargo|.*[.]test)[.]exe$' -and $candidate.CommandLine -and
  @($testRoots|Where-Object {$candidate.CommandLine.Contains($_)}).Count -gt 0
})
if($processes.Count){throw 'Test processes remain; no cleanup performed'}
$beforeReport=& (Join-Path $PSScriptRoot 'Measure-TestStorage.ps1') -Label ($Label+'-before')|ConvertFrom-Json
$before=[long]$beforeReport.total_bytes
$cacheNames=@('edge-profile','webview-profile','go-cache','npm-cache','node-compile-cache','__pycache__','node_modules')
$cacheDirs=@($testRoots|ForEach-Object {Get-ChildItem -LiteralPath $_ -Directory -Recurse -Force}|Where-Object {$_.Name -in $cacheNames -and $_.FullName -notmatch '[\\/](sync-backup-2026-10-07|fixtures|录像 样本)[\\/]'})
$cacheDirs+=@(Get-ChildItem -LiteralPath (Join-Path $cleanupRepo 'workers/python') -Directory -Recurse -Force|Where-Object Name -eq '__pycache__')
foreach($mode in @('debug','release','app-debug','app')) {
  $runtimePython=Join-Path $cleanupRepo "build/desktop/$mode/runtime/workers/python"
  if(Test-Path -LiteralPath $runtimePython){Assert-CleanupPath $runtimePython;$cacheDirs+=@(Get-ChildItem -LiteralPath $runtimePython -Directory -Recurse -Force|Where-Object Name -eq '__pycache__')}
}
$generatedExtensions=@('.exe','.dll','.mp4','.wav','.mp3','.mkv','.zip','.png','.jpg','.jpeg','.webp','.log','.err','.out','.pyc')
$files=@($testRoots|ForEach-Object {Get-ChildItem -LiteralPath $_ -File -Recurse -Force}|Where-Object {
  $_.FullName -notmatch '[\\/](sync-backup-2026-10-07|fixtures|录像 样本)[\\/]' -and
  ($_.Extension -in $generatedExtensions -or $_.Name -match '[.]db($|-wal$|-shm$|-journal$)')
})
$files+=@(Get-ChildItem -LiteralPath (Join-Path $cleanupRepo 'build/desktop/debug/deps') -File -Filter 'video_auto_cut_desktop-*.exe' -ErrorAction SilentlyContinue)
$targets=@($cacheDirs.FullName)+@($files.FullName)
$targets=@($targets|Sort-Object -Unique)
foreach($target in $targets){Assert-CleanupPath $target}
foreach($target in $targets){if(Test-Path -LiteralPath $target){Assert-CleanupPath $target;Remove-Item -LiteralPath $target -Recurse -Force}}
$afterReport=& (Join-Path $PSScriptRoot 'Measure-TestStorage.ps1') -Label ($Label+'-after')|ConvertFrom-Json
$after=[long]$afterReport.total_bytes
$report=@{before_bytes=$before;after_bytes=$after;released_bytes=$before-$after;targets=$targets;retained='test source, fixtures, acceptance scripts, text reports, JSON results, source backups; original recording outside repository'}
$report|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $cleanupRun ($Label+'.json')) -Encoding utf8
$report|Select-Object before_bytes,after_bytes,released_bytes|ConvertTo-Json -Compress
$afterReport|ConvertTo-Json -Compress
