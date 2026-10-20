#Requires -Version 7.2
[CmdletBinding()]
param(
  [ValidateSet('Prepare','Measure','Clear','Run')][string]$Action = 'Prepare',
  [ValidatePattern('^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$')][string]$Version,
  [ValidateSet('go','python','webui','pipeline','timeline','exporter','guard','workspace','custom')][string]$Suite = 'custom',
  [string]$Executable,
  [string[]]$Arguments = @(),
  [string]$ArgumentsJson,
  [string]$WorkingDirectory,
  [switch]$CacheOnly,
  [string]$StorageRoot
)
$ErrorActionPreference = 'Stop'
if ($CacheOnly -and $Action -ne 'Clear') { throw 'CacheOnly requires -Action Clear' }
$repo = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$allowedRoot = Join-Path $repo '.run-data/tests'
if (!$StorageRoot) { $StorageRoot = $allowedRoot }
$StorageRoot = [IO.Path]::GetFullPath($StorageRoot)
if ($StorageRoot -ne $allowedRoot -and !$StorageRoot.StartsWith($allowedRoot+'\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Test storage must be inside .run-data/tests' }
if (!$Version) { $Version = (Get-Content -LiteralPath (Join-Path $repo 'desktop/tauri.conf.json') -Raw | ConvertFrom-Json).version }
if ($Version -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$') { throw 'Invalid test version' }
$current = Join-Path $StorageRoot $Version
$archives = Join-Path $StorageRoot 'archives'
if ($ArgumentsJson) {
  $parsedArguments=ConvertFrom-Json -InputObject $ArgumentsJson -NoEnumerate
  if ($parsedArguments -isnot [array] -or @($parsedArguments | Where-Object { $_ -isnot [string] }).Count) { throw 'ArgumentsJson must be an array of strings' }
  $Arguments=$parsedArguments
}
function Assert-Path([string]$Path, [switch]$SkipChildren) {
  $full = [IO.Path]::GetFullPath($Path)
  if (!$full.StartsWith($allowedRoot+'\', [StringComparison]::OrdinalIgnoreCase) -and $full -ne $allowedRoot) { throw "Outside test storage: $full" }
  for ($cursor=$full; $cursor; $cursor=Split-Path -Parent $cursor) {
    $item = Get-Item -LiteralPath $cursor -Force -ErrorAction SilentlyContinue
    if ($item -and ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Reparse point: $cursor" }
  }
  if (!$SkipChildren -and (Test-Path -LiteralPath $full -PathType Container)) {
    if (@(Get-ChildItem -LiteralPath $full -Recurse -Force | Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint }).Count) { throw "Child reparse point: $full" }
  }
}
function Assert-Idle {
  $active = @(Get-CimInstance Win32_Process | Where-Object {
    $_.ProcessId -ne $PID -and $_.CommandLine -and $_.Name -match '^(go|cargo|node|python|ffmpeg|ffprobe|compile|link|msedge|chrome|msedgewebview2|.*control-plane.*|video-auto-cut-desktop|.*[.]test)[.]exe$' -and
    ($_.CommandLine.Contains($StorageRoot) -or ($_.ExecutablePath -and $_.ExecutablePath.StartsWith($StorageRoot+'\',[StringComparison]::OrdinalIgnoreCase)))
  })
  if ($active.Count) { throw ('Test processes are still running: '+($active.ProcessId -join ', ')) }
}
function Remove-Owned([string]$Path) {
  Assert-Idle
  Assert-Path $Path
  $relative = [IO.Path]::GetRelativePath($repo, $Path).Replace('\','/')
  $tracked = @(git -C $repo ls-files -- $relative)
  if ($LASTEXITCODE -ne 0 -or $tracked.Count) { throw "Refusing to remove tracked files: $relative" }
  if (Test-Path -LiteralPath $Path) { Remove-Item -LiteralPath $Path -Recurse -Force }
}
function Read-Summary([string]$Directory) {
  $path = Join-Path $Directory 'summary.json'
  if (Test-Path -LiteralPath $path) {
    $summary=Get-Content -LiteralPath $path -Raw | ConvertFrom-Json -AsHashtable
    if ($summary.version -ne [IO.Path]::GetFileName($Directory) -or $summary.tests -isnot [Collections.IDictionary]) { throw 'Invalid test summary; nothing was deleted' }
    return $summary
  }
  return @{ version=([IO.Path]::GetFileName($Directory)); tests=@{} }
}
function Archive-Summary([string]$Directory) {
  $summary = Read-Summary $Directory
  if ($summary.version -ne [IO.Path]::GetFileName($Directory) -or $summary.version -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') { throw 'Invalid summary version' }
  $lines = @("# 测试简档 $($summary.version)", '', '类型 | 最近结论 | 修复后通过', '---|---|---')
  foreach ($type in @($summary.tests.Keys | Sort-Object)) {
    $entry = $summary.tests[$type]
    $fixed = if ($entry.fixed_in) { $entry.fixed_in } else { '未记录' }
    $lines += "$type | $($entry.result) | $fixed"
  }
  if (!$summary.tests.Count) { $lines += '未记录 | 没有测试结果 | 未记录' }
  $lines += @('', ('清空时间：'+[DateTime]::UtcNow.ToString('o')))
  Assert-Path $archives
  New-Item -ItemType Directory -Path $archives -Force | Out-Null
  $path = Join-Path $archives ($summary.version+'.md')
  Assert-Path $path
  $lines | Set-Content -LiteralPath $path -Encoding utf8NoBOM
  # At most one tiny archive per version, and only the latest three versions.
  $old = @(Get-ChildItem -LiteralPath $archives -File -Filter '*.md' | Where-Object BaseName -Match '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$' | Sort-Object LastWriteTimeUtc -Descending | Select-Object -Skip 3)
  foreach ($file in $old) { Remove-Owned $file.FullName }
}
function Test-MissingStorageEntry([Management.Automation.ErrorRecord]$Record) {
  if ($Record.CategoryInfo.Category -eq 'ObjectNotFound') { return $true }
  for ($exception=$Record.Exception; $exception; $exception=$exception.InnerException) {
    if ($exception -is [IO.IOException] -and ($exception.HResult -band 0xffff) -in @(2,3)) { return $true }
    if ($exception -is [ComponentModel.Win32Exception] -and $exception.NativeErrorCode -in @(2,3)) { return $true }
  }
  return $false
}
function Get-StorageEntries([string]$Path) {
  if ([IO.File]::Exists($Path)) { return [IO.FileInfo]::new($Path) }
  return [IO.DirectoryInfo]::new($Path).GetFileSystemInfos()
}
function Measure-Storage {
  $files = @{}
  $legacyPaths=@(Get-ChildItem -LiteralPath (Join-Path $repo '.run-data') -Force -ErrorAction SilentlyContinue | Where-Object { $_.Name -notin @('tests','local-pipeline','build-cache') } | ForEach-Object FullName)
  $paths = @($allowedRoot, (Join-Path $repo 'test-support'), (Join-Path $repo 'workers/python/tests'),
    (Join-Path $repo 'workers/python/scripts'), (Join-Path $repo 'workers/node/timeline-cli/test'),
    (Join-Path $repo 'workers/node/timeline-cli/fixtures'), (Join-Path $repo 'workers/node/timeline-cli/scripts'),
    (Join-Path $repo 'workers/node/export-worker/test'), (Join-Path $repo 'scripts'),
    (Join-Path $repo 'contracts/mp4-preview/v1/fixtures'), (Join-Path $repo 'contracts/delivery-variant/v1/fixtures')) + $legacyPaths
  foreach ($path in $paths) {
    $pending = [Collections.Generic.Stack[string]]::new()
    $pending.Push($path)
    while ($pending.Count) {
      $directory = $pending.Pop()
      if (![IO.Directory]::Exists($directory) -and ![IO.File]::Exists($directory)) { continue }
      # Enumerate one directory at a time: recursive PowerShell enumeration can
      # abort the whole tree when Go removes an unrelated temporary child.
      for ($attempt=0; $attempt -lt 3; $attempt++) {
        try {
          $entries=@(Get-StorageEntries $directory)
          break
        } catch {
          if ((Test-MissingStorageEntry $_) -and ![IO.Directory]::Exists($directory) -and ![IO.File]::Exists($directory)) { $entries=@(); break }
          if (!(Test-MissingStorageEntry $_) -or $attempt -eq 2) {
            Write-Warning ("Storage scan failed at {0}: {1}; HResult={2}; NativeError={3}; ErrorId={4}" -f $directory,$_.Exception.GetType().FullName,$_.Exception.HResult,$_.Exception.NativeErrorCode,$_.FullyQualifiedErrorId)
            throw
          }
          Start-Sleep -Milliseconds 50
        }
      }
      foreach ($entry in $entries) {
        if ($entry -is [IO.DirectoryInfo]) {
          if (!($entry.Attributes -band [IO.FileAttributes]::ReparsePoint)) { $pending.Push($entry.FullName) }
        } else { $files[$entry.FullName]=[long]$entry.Length }
      }
    }
  }
  foreach ($file in Get-ChildItem -LiteralPath (Join-Path $repo 'control-plane') -File -Recurse -Filter '*_test.go') { $files[$file.FullName]=[long]$file.Length }
  foreach ($file in Get-ChildItem -LiteralPath (Join-Path $repo 'webui') -File -Filter '*.mjs') { $files[$file.FullName]=[long]$file.Length }
  foreach ($file in Get-ChildItem -LiteralPath (Join-Path $repo 'workers/python') -File -Recurse -Filter '*.pyc') { $files[$file.FullName]=[long]$file.Length }
  $bytes = [long](($files.Values | Measure-Object -Sum).Sum)
  return [ordered]@{version=$Version; total_bytes=$bytes; total_MiB=[math]::Round($bytes/1MB,3)}
}
function Clear-Current {
  Archive-Summary $current
  $names=if($CacheOnly){@('cache')}else{@('artifacts','cache')}
  foreach ($name in $names) { Remove-Owned (Join-Path $current $name) }
}
Assert-Path $StorageRoot -SkipChildren:($Action -eq 'Measure')
if ($Action -eq 'Measure') { Measure-Storage | ConvertTo-Json -Compress; return }
New-Item -ItemType Directory -Path $StorageRoot -Force | Out-Null
$lock = [IO.File]::Open((Join-Path $StorageRoot '.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
try {
  Assert-Idle
  # Validate every existing directory before deleting any previous version.
  $previousSummary=$null
  foreach ($directory in Get-ChildItem -LiteralPath $StorageRoot -Directory | Sort-Object LastWriteTimeUtc) {
    if ($directory.Name -eq 'archives') { continue }
    if ($directory.Name -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') { throw 'Unrecognized directory in test storage' }
    $marker=Join-Path $directory.FullName '.test-workspace.json'
    if (!(Test-Path -LiteralPath $marker)) {
      if ($directory.Name -ne $Version -or @(Get-ChildItem -LiteralPath $directory.FullName -Force).Count) { throw 'Unmarked test directory; nothing was deleted' }
      continue
    }
    $owner=Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json
    if ($owner.schema -ne 1 -or $owner.version -ne $directory.Name) { throw 'Invalid workspace marker; nothing was deleted' }
    [void](Read-Summary $directory.FullName)
  }
  foreach ($directory in Get-ChildItem -LiteralPath $StorageRoot -Directory) {
    if ($directory.Name -in @($Version,'archives')) { continue }
    if ($directory.Name -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') { throw 'Unrecognized directory in test storage' }
    $marker = Join-Path $directory.FullName '.test-workspace.json'
    if (!(Test-Path -LiteralPath $marker)) { throw "Unmanaged directory: $($directory.FullName)" }
    $owner = Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json
    if ($owner.schema -ne 1 -or $owner.version -ne $directory.Name) { throw 'Invalid test workspace marker' }
    $previousSummary=Read-Summary $directory.FullName
    Archive-Summary $directory.FullName
    Remove-Owned $directory.FullName
  }
  Assert-Path $current
  New-Item -ItemType Directory -Path $current -Force | Out-Null
  $marker = Join-Path $current '.test-workspace.json'
  if (Test-Path -LiteralPath $marker) {
    $owner = Get-Content -LiteralPath $marker -Raw | ConvertFrom-Json
    if ($owner.schema -ne 1 -or $owner.version -ne $Version) { throw 'Invalid current workspace marker' }
  } elseif (@(Get-ChildItem -LiteralPath $current -Force).Count) { throw 'Refusing to adopt an unmarked non-empty directory' }
  @{schema=1;version=$Version} | ConvertTo-Json | Set-Content -LiteralPath $marker -Encoding utf8NoBOM
  if ($previousSummary -and !(Test-Path -LiteralPath (Join-Path $current 'summary.json'))) {
    $baseline=@{version=$Version;tests=@{}}
    foreach($type in $previousSummary.tests.Keys){
      $entry=$previousSummary.tests[$type]
      $priorResult=if($entry.result -eq 'pending' -and $entry.previous_result){$entry.previous_result}else{$entry.result}
      $priorVersion=if($entry.result -eq 'pending' -and $entry.previous_version){$entry.previous_version}else{$previousSummary.version}
      $baseline.tests[$type]=@{result='pending';previous_result=$priorResult;previous_version=$priorVersion;fixed_in=$entry.fixed_in}
    }
    $baseline | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $current 'summary.json') -Encoding utf8NoBOM
  }
  $before = Measure-Storage
  if ($Action -eq 'Clear') {
    Clear-Current
    $after = Measure-Storage
    [ordered]@{before_bytes=$before.total_bytes;after_bytes=$after.total_bytes;released_bytes=($before.total_bytes-$after.total_bytes)} | ConvertTo-Json -Compress
    return
  }
  if ($Action -eq 'Measure') { Measure-Storage | ConvertTo-Json -Compress; return }
  $artifact = Join-Path $current ('artifacts/'+$Suite)
  $cache = Join-Path $current 'cache'
  foreach ($path in @($artifact,$cache)) { New-Item -ItemType Directory -Path $path -Force | Out-Null }
  if ($Action -eq 'Prepare') { Write-Output $artifact; return }
  $command = switch ($Suite) {
    'go' { @{exe='go';args=@('-C','control-plane','test','-p','1','-count=1','./...');cwd=$repo} }
    'python' { @{exe='python';args=@('-B','-m','unittest','discover','-s','tests','-t','.','-v');cwd=(Join-Path $repo 'workers/python')} }
    'webui' { @{exe='node';args=@('--experimental-strip-types','--test','test_suite.mjs','desktop_test.mjs','tts_test.mjs','pipeline_test.mjs','evidence_summary_test.mjs');cwd=(Join-Path $repo 'webui')} }
    'pipeline' { @{exe='node';args=@('--experimental-strip-types','--test','pipeline_test.mjs');cwd=(Join-Path $repo 'webui')} }
    'timeline' { @{exe='node';args=@('--test');cwd=(Join-Path $repo 'workers/node/timeline-cli')} }
    'exporter' { @{exe='node';args=@('--test');cwd=(Join-Path $repo 'workers/node/export-worker')} }
    'guard' { @{exe='pwsh';args=@('-NoProfile','-File',(Join-Path $repo 'scripts/Test-TauriStateGuard.ps1'));cwd=$repo} }
    'workspace' { @{exe='pwsh';args=@('-NoProfile','-File',(Join-Path $repo 'test-support/test-workspace.ps1'));cwd=$repo} }
    'custom' { if (!$Executable) { throw 'Custom tests require -Executable and -Arguments' }; @{exe=$Executable;args=$Arguments;cwd=$(if($WorkingDirectory){[IO.Path]::GetFullPath($WorkingDirectory)}else{$repo})} }
  }
  $start = [Diagnostics.ProcessStartInfo]::new($command.exe)
  $start.WorkingDirectory=$command.cwd; $start.UseShellExecute=$false
  $start.RedirectStandardOutput=$true; $start.RedirectStandardError=$true; $start.CreateNoWindow=$true
  foreach ($argument in $command.args) { $start.ArgumentList.Add($argument) }
  $start.Environment['TEMP']=$artifact; $start.Environment['TMP']=$artifact; $start.Environment['TMPDIR']=$artifact
  $start.Environment['VAC_TEST_ROOT']=$artifact; $start.Environment['VAC_TEST_OUTPUT']=$artifact
  $start.Environment['VAC_TEST_VERSION']=$Version; $start.Environment['PYTHONDONTWRITEBYTECODE']='1'
  $start.Environment['GOCACHE']=(Join-Path $cache 'go'); $start.Environment['GOTMPDIR']=$artifact
  $start.Environment['GOPROXY']='off'; $start.Environment['GOTOOLCHAIN']='local'; $start.Environment['GOFLAGS']='-p=1'
  $start.Environment['npm_config_cache']=(Join-Path $cache 'npm')
  Write-Output ("Testing $Suite / $Version; before: $($before.total_MiB) MiB")
  $process = [Diagnostics.Process]::Start($start)
  $stdout=$process.StandardOutput.ReadToEndAsync(); $stderr=$process.StandardError.ReadToEndAsync()
  $process.WaitForExit()
  $output=$stdout.GetAwaiter().GetResult()+$stderr.GetAwaiter().GetResult()
  $output | Set-Content -LiteralPath (Join-Path $artifact 'output.log') -Encoding utf8NoBOM
  Write-Output $output
  $exitCode=$process.ExitCode
  $result=if($exitCode -eq 0){'passed'}else{'failed'}
  $summary=Read-Summary $current
  $old=$summary.tests[$Suite]
  $head=(& git -C $repo rev-parse --short HEAD).Trim()
  if (@(& git -C $repo status --porcelain).Count) { $head += '+working-tree' }
  $fixed=if($old -and $old.fixed_in){$old.fixed_in}else{''}
  if($result -eq 'passed' -and $old -and $old.result -eq 'failed') { $fixed="$Version@$head" }
  if($result -eq 'passed' -and $old -and $old.previous_result -eq 'failed') { $fixed="$Version@$head ($($old.previous_version) failed→passed)" }
  $summary.tests[$Suite]=@{result=$result;fixed_in=$fixed;head=$head}
  $summary | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $current 'summary.json') -Encoding utf8NoBOM
  $after=Measure-Storage
  $after | ConvertTo-Json -Compress
  if($exitCode -ne 0) { exit $exitCode }
} finally {
  if($process){if(!$process.HasExited){$process.Kill($true);$process.WaitForExit()};$process.Dispose()}
  if($lock){$lock.Dispose()}
}
