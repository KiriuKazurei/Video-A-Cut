#Requires -Version 7.2
param(
    [Parameter(Mandatory=$true)][string]$DataDirectory,
    [string]$ProfileFile='',
    [ValidateRange(1024,65535)][int]$Port=8806,
    [string]$IngestRootsFile='',
    [string]$ControlBinary='',
    [switch]$DevelopmentBuild
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Runtime.Common.ps1')
$runtimeRoot = [IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$DataDirectory = Assert-VacPlainPath $DataDirectory
if ($DataDirectory -eq $runtimeRoot -or $DataDirectory.StartsWith($runtimeRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Operator data must be outside the version directory. Use Start-LocalPipeline.ps1 for repository test sessions.'
}
$diagnosticArgs = @{DataDirectory=$DataDirectory;Port=$Port;ProfileFile=$ProfileFile;ControlBinary=$ControlBinary;DevelopmentBuild=$DevelopmentBuild}
$diagnostic = & (Join-Path $PSScriptRoot 'Get-VideoAutoCutDiagnostics.ps1') @diagnosticArgs | ConvertFrom-Json
foreach ($check in $diagnostic.checks) { Write-Host "[$($check.status)] $($check.id): $($check.message)" }
if ($diagnostic.overall_status -eq 'blocked') { throw 'Startup diagnostics are blocked; resolve the items above before starting.' }
$supervisorArgs = @{DataDirectory=$DataDirectory;Port=$Port;ProfileFile=$ProfileFile;IngestRootsFile=$IngestRootsFile}
if (!$DevelopmentBuild) {
    if (!$ControlBinary) { $ControlBinary = Join-Path $runtimeRoot 'bin/control-plane.exe' }
    $supervisorArgs.ControlBinary = Assert-VacPlainPath $ControlBinary
}
& (Join-Path $PSScriptRoot 'Start-LocalPipeline.ps1') @supervisorArgs
