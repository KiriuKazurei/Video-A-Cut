#Requires -Version 7.2
$ErrorActionPreference='Stop'
$repo=Split-Path -Parent $PSScriptRoot
$manager=Join-Path $repo 'scripts/TestWorkspace.ps1'
$root=Join-Path $env:VAC_TEST_ROOT 'workspace-checks'
if(!$env:VAC_TEST_ROOT -or !$root.StartsWith((Join-Path $repo '.run-data/tests')+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Managed test root required'}
New-Item -ItemType Directory -Path $root -Force | Out-Null
$checks=0
$tokens=$null; $parseErrors=$null
$managerAst=[Management.Automation.Language.Parser]::ParseFile($manager,[ref]$tokens,[ref]$parseErrors)
if($parseErrors.Count){throw 'Test manager does not parse'}
$missingFunction=$managerAst.Find({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Test-MissingStorageEntry'},$true)
if(!$missingFunction){throw 'Missing storage error classifier'}
. ([scriptblock]::Create($missingFunction.Extent.Text))
foreach($case in @(
  @{exception=[IO.IOException]::new('File vanished',[int]0x80070002);missing=$true},
  @{exception=[IO.DirectoryNotFoundException]::new('Directory vanished');missing=$true},
  @{exception=[ComponentModel.Win32Exception]::new(3);missing=$true},
  @{exception=[IO.IOException]::new('Access denied',[int]0x80070005);missing=$false},
  @{exception=[UnauthorizedAccessException]::new('Denied');missing=$false},
  @{exception=[IO.IOException]::new('Other I/O failure',[int]0x80070020);missing=$false}
)){
  $record=[Management.Automation.ErrorRecord]::new($case.exception,'StorageEnumeration',[Management.Automation.ErrorCategory]::ReadError,$root)
  if((Test-MissingStorageEntry $record) -ne $case.missing){throw ('Incorrect storage error classification: '+$case.exception.Message)}
  $checks++
}
$measureFunction=$managerAst.Find({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Measure-Storage'},$true)
$readerFunction=$managerAst.Find({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-StorageEntries'},$true)
. ([scriptblock]::Create($readerFunction.Extent.Text))
$reader=(Get-Command Get-StorageEntries).ScriptBlock
. ([scriptblock]::Create($measureFunction.Extent.Text))
& {
  $allowedRoot=Join-Path $env:VAC_TEST_ROOT 'enumeration-probe'
  New-Item -ItemType Directory -Path $allowedRoot -Force | Out-Null
  Set-Content -LiteralPath (Join-Path $allowedRoot 'retained.txt') -Value 'count me'
  $state=@{calls=0;failures=1;exception=[IO.IOException]::new('File vanished',[int]0x80070002)}
  function Get-StorageEntries([string]$Path) {
    if($Path -eq $allowedRoot){
      $state.calls++
      if($state.calls -le $state.failures){throw $state.exception}
    }
    if($state.vanish -and $Path -eq (Join-Path $allowedRoot 'ephemeral')){
      Remove-Item -LiteralPath $Path -Force
      throw [IO.DirectoryNotFoundException]::new('Temporary directory removed after discovery')
    }
    & $reader $Path
  }
  $measurement=Measure-Storage
  if($state.calls -ne 2 -or $measurement.total_bytes -lt 8){throw 'Disappearing directory was not retried and counted'}
  $script:checks++
  foreach($case in @(
    @{failures=9;exception=[IO.IOException]::new('Still missing',[int]0x80070002);calls=3},
    @{failures=9;exception=[UnauthorizedAccessException]::new('Denied');calls=1}
  )){
    $state.calls=0;$state.failures=$case.failures;$state.exception=$case.exception
    $failed=$false
    try{[void](Measure-Storage)}catch{$failed=$true}
    if(!$failed -or $state.calls -ne $case.calls){throw 'Storage enumeration hid an error or retried without a bound'}
    $script:checks++
  }
  New-Item -ItemType Directory -Path (Join-Path $allowedRoot 'ephemeral') -Force | Out-Null
  $state.calls=0;$state.failures=0;$state.vanish=$true
  $measurement=Measure-Storage
  if($measurement.total_bytes -lt 8){throw 'A vanished child prevented retained files from being counted'}
  $script:checks++
}
function Invoke-Manager([string]$action,[string]$version,[string[]]$extra=@(),[switch]$Fail) {
  $parameters=@('-NoProfile','-File',$manager,'-Action',$action,'-Version',$version,'-StorageRoot',$root)+$extra
  $messages=& pwsh @parameters 2>&1
  $code=$LASTEXITCODE
  $script:lastManagerExitCode=$code
  $script:lastManagerMessages=$messages
  if($Fail -and $code -eq 0){throw 'Expected refusal, but operation succeeded'}
  if(!$Fail -and $code -ne 0){throw ($messages -join "`n")}
  $script:checks++
}
function Remove-Fixture([string]$path) {
  $full=[IO.Path]::GetFullPath($path)
  if(!$full.StartsWith($root+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Fixture cleanup outside owned test root'}
  # Link fixtures are removed without recursion; their target is preserved.
  Remove-Item -LiteralPath $full -Force
}
Invoke-Manager Prepare '0.0.1'
Set-Content -LiteralPath (Join-Path $root '0.0.1/artifacts/custom/generated.txt') -Value 'old'
@{version='0.0.1';tests=@{custom=@{result='failed';fixed_in=''}}}|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $root '0.0.1/summary.json')
Invoke-Manager Measure '0.0.2'
if(!(Test-Path -LiteralPath (Join-Path $root '0.0.1/artifacts/custom/generated.txt'))){throw 'Measure unexpectedly deleted old version'}
Invoke-Manager Prepare '0.0.2'
if(Test-Path -LiteralPath (Join-Path $root '0.0.1')){throw 'Old version survived rotation'}
if((Get-Content -LiteralPath (Join-Path $root 'archives/0.0.1.md') -Raw) -notmatch 'custom \| failed'){throw 'Failed test missing from simple archive'}
$baseline=Get-Content -LiteralPath (Join-Path $root '0.0.2/summary.json') -Raw|ConvertFrom-Json
if($baseline.tests.custom.result -ne 'pending'){throw 'Old result was presented as already passed in the new version'}
Invoke-Manager Run '0.0.2' @('-Suite','custom','-Executable','node','-ArgumentsJson','["-e","process.exit(0)"]')
$fixed=Get-Content -LiteralPath (Join-Path $root '0.0.2/summary.json') -Raw|ConvertFrom-Json
if($fixed.tests.custom.fixed_in -notmatch '0.0.2@.*0.0.1 failed→passed'){throw 'Cross-version repair was not recorded'}
# Sparse logical size crosses the removed cap without allocating a large fixture.
# The child remains alive beyond the former five-second quota sampling interval.
$largeFixture=Join-Path $root '0.0.2/artifacts/custom/oversized.bin'
[IO.File]::WriteAllBytes($largeFixture,[byte[]]@())
& fsutil sparse setflag $largeFixture | Out-Null
if($LASTEXITCODE -ne 0){throw 'Unable to mark the owned size-regression fixture sparse'}
$cacheSentinel=Join-Path $root '0.0.2/cache/retained.txt'
Set-Content -LiteralPath $cacheSentinel -Value 'retain cache'
$growthCheck='const fs=require("node:fs"),p=require("node:path"); fs.truncateSync(p.join(process.env.VAC_TEST_ROOT,"oversized.bin"),501*1024*1024); setTimeout(()=>{fs.writeFileSync(p.join(process.env.VAC_TEST_ROOT,"completed.txt"),"completed"); console.log("size regression completed")},6000)'
$growthArgs=ConvertTo-Json -InputObject @('-e',$growthCheck) -Compress
Invoke-Manager Run '0.0.2' @('-Suite','custom','-Executable','node','-ArgumentsJson',$growthArgs)
if((Get-Item -LiteralPath $largeFixture).Length -ne 501MB -or !(Test-Path -LiteralPath (Join-Path $root '0.0.2/artifacts/custom/completed.txt'))){throw 'Size growth interrupted the child or deleted its artifacts'}
$checks++
Invoke-Manager Measure '0.0.2'
$usage=($lastManagerMessages -join "`n") | ConvertFrom-Json
if($usage.total_bytes -lt 501MB -or $usage.PSObject.Properties.Name -contains 'limit_bytes' -or $usage.PSObject.Properties.Name -contains 'within_limit'){throw 'Measurement still reports a quota or omitted the large fixture'}
$checks++
Invoke-Manager Prepare '0.0.2'
if(!(Test-Path -LiteralPath $largeFixture) -or !(Test-Path -LiteralPath $cacheSentinel)){throw 'Prepare deleted output or cache based on size'}
$checks++
$failureCheck='const fs=require("node:fs"),p=require("node:path"); fs.writeFileSync(p.join(process.env.VAC_TEST_ROOT,"failure.txt"),"failure evidence"); console.error("intentional failure evidence"); process.exit(7)'
$failureArgs=ConvertTo-Json -InputObject @('-e',$failureCheck) -Compress
Invoke-Manager Run '0.0.2' @('-Suite','custom','-Executable','node','-ArgumentsJson',$failureArgs) -Fail
$failureSummary=Get-Content -LiteralPath (Join-Path $root '0.0.2/summary.json') -Raw | ConvertFrom-Json
if($lastManagerExitCode -ne 7 -or $failureSummary.tests.custom.result -ne 'failed' -or
  !(Test-Path -LiteralPath $largeFixture) -or !(Test-Path -LiteralPath $cacheSentinel) -or
  (Get-Content -LiteralPath (Join-Path $root '0.0.2/artifacts/custom/failure.txt') -Raw).Trim() -ne 'failure evidence' -or
  (Get-Content -LiteralPath (Join-Path $root '0.0.2/artifacts/custom/output.log') -Raw) -notmatch 'intentional failure evidence'){
  throw 'Size changed the failure exit code or removed its summary, cache or evidence'
}
$checks++
Remove-Fixture $largeFixture
for($number=3;$number -le 5;$number++){Invoke-Manager Prepare "0.0.$number"}
if(@(Get-ChildItem -LiteralPath (Join-Path $root 'archives') -File -Filter '*.md').Count -ne 3){throw 'Archive version limit not enforced'}
$unmanaged=Join-Path $root 'unmanaged'
New-Item -ItemType Directory -Path $unmanaged | Out-Null
Set-Content -LiteralPath (Join-Path $unmanaged 'retain.txt') -Value 'retain'
Invoke-Manager Prepare '0.0.6' -Fail
if(!(Test-Path -LiteralPath (Join-Path $root '0.0.5'))){throw 'Validation failure deleted a managed version'}
Remove-Fixture (Join-Path $unmanaged 'retain.txt'); Remove-Fixture $unmanaged
$target=Join-Path $root '0.0.5/artifacts/custom/protected'
New-Item -ItemType Directory -Path $target | Out-Null
Set-Content -LiteralPath (Join-Path $target 'retain.txt') -Value 'retain'
$junction=Join-Path $root '0.0.5/artifacts/custom/link'
New-Item -ItemType Junction -Path $junction -Target $target | Out-Null
Invoke-Manager Clear '0.0.5' -Fail
Remove-Fixture $junction
if(!(Test-Path -LiteralPath (Join-Path $target 'retain.txt'))){throw 'Junction target was deleted'}
$start=[Diagnostics.ProcessStartInfo]::new('node')
$start.UseShellExecute=$false;$start.CreateNoWindow=$true
$start.ArgumentList.Add('-e');$start.ArgumentList.Add('setTimeout(()=>{},60000)');$start.ArgumentList.Add($root)
$ownedProcess=[Diagnostics.Process]::Start($start)
try{Start-Sleep -Milliseconds 300; Invoke-Manager Clear '0.0.5' -Fail}
finally{if(!$ownedProcess.HasExited){$ownedProcess.Kill($true);$ownedProcess.WaitForExit()};$ownedProcess.Dispose()}
Invoke-Manager Run '0.0.5' @('-Suite','custom','-Executable','node','-ArgumentsJson','["-e","process.exit(2)"]') -Fail
Invoke-Manager Run '0.0.5' @('-Suite','custom','-Executable','node','-ArgumentsJson','["-e","process.exit(0)"]')
$summary=Get-Content -LiteralPath (Join-Path $root '0.0.5/summary.json') -Raw|ConvertFrom-Json
if($summary.tests.custom.result -ne 'passed' -or $summary.tests.custom.fixed_in -notmatch '^0.0.5@'){throw 'Failure-to-pass version was not recorded'}
$cacheFixture=Join-Path $root '0.0.5/cache'
New-Item -ItemType Directory -Path $cacheFixture -Force | Out-Null
Set-Content -LiteralPath (Join-Path $cacheFixture 'compiler-cache.txt') -Value 'generated cache'
$cacheLink=Join-Path $cacheFixture 'link'
New-Item -ItemType Junction -Path $cacheLink -Target $target | Out-Null
Invoke-Manager Clear '0.0.5' @('-CacheOnly') -Fail
Remove-Fixture $cacheLink
Invoke-Manager Clear '0.0.5' @('-CacheOnly')
if(Test-Path -LiteralPath $cacheFixture){throw 'Compiler cache survived CacheOnly'}
if(!(Test-Path -LiteralPath (Join-Path $target 'retain.txt'))){throw 'CacheOnly deleted retained artifacts'}
if((Get-Content -LiteralPath (Join-Path $root 'archives/0.0.5.md') -Raw) -notmatch 'custom \| passed'){throw 'CacheOnly did not archive the test result'}
Invoke-Manager Measure '0.0.5' @('-CacheOnly') -Fail
Invoke-Manager Clear '0.0.5'
if(Test-Path -LiteralPath (Join-Path $root '0.0.5/artifacts')){throw 'Generated output survived Clear'}
if((Get-Content -LiteralPath (Join-Path $root 'archives/0.0.5.md') -Raw) -notmatch 'custom \| passed \| 0.0.5@'){throw 'Fixed result missing from archive'}
& node --input-type=module -e 'import assert from "node:assert/strict"; import { testDirectory } from "./scripts/test-paths.mjs"; assert.match(testDirectory("node-path"), /tests/); assert.throws(()=>testDirectory("../../../../../../"));'
if($LASTEXITCODE -ne 0){throw 'Node test path helper failed'}
$pythonCheck=@'
import os,sys
from pathlib import Path
sys.path.insert(0,str(Path.cwd()/'workers/python/scripts'))
from test_paths import test_directory,go_cache
assert test_directory('python-path').is_relative_to(Path(os.environ['VAC_TEST_ROOT']))
assert go_cache()==os.environ['GOCACHE']
try:
    test_directory('../../../../')
except ValueError:
    pass
else:
    raise AssertionError('Traversal accepted')
'@
& python -B -c $pythonCheck
if($LASTEXITCODE -ne 0){throw 'Python test path helper failed'}
$checks+=2
Write-Output "PASS: $checks lifecycle checks; version rotation, archives, read-only measurement, unknown directory, junction, running process, fail-to-pass history, uncapped size growth, failure evidence retention and full Clear."
