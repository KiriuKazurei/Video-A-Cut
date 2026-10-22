#Requires -Version 7.2
param([string]$OutputDirectory='')
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Runtime.Common.ps1')
$runtimeRoot = Split-Path -Parent $PSScriptRoot
if (!$OutputDirectory) { $OutputDirectory = Join-Path $runtimeRoot 'build/runtime/bin' }
$OutputDirectory = Assert-VacPlainPath $OutputDirectory
if (Test-Path -LiteralPath $OutputDirectory) { throw 'Select a new output directory; existing builds are never overwritten.' }
$sourceRoot = Join-Path $runtimeRoot 'control-plane'
$null = Assert-VacPlainPath $sourceRoot
$sourceFiles = @(Get-ChildItem -LiteralPath $sourceRoot -Recurse -File | Where-Object { $_.Extension -in @('.go', '.sql', '.json') -or $_.Name -in @('go.mod', 'go.sum') })
$sourceHashes = @{}
foreach ($file in $sourceFiles) { $sourceHashes[$file.FullName] = (Get-FileHash -LiteralPath (Assert-VacPlainPath $file.FullName)).Hash }
$migrations = @(Get-ChildItem -LiteralPath (Join-Path $sourceRoot 'internal/store/migrations') -Filter '*.sql' -File | Sort-Object Name)
$maxSchema = ($migrations | ForEach-Object { [int]$_.Name.Substring(0,4) } | Measure-Object -Maximum).Maximum
if ($maxSchema -lt 1) { throw 'No schema migrations found.' }
New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
$binary = Join-Path $OutputDirectory 'control-plane.exe'
$previousProxy = $env:GOPROXY
$previousCache = $env:GOCACHE
$previousToolchain = $env:GOTOOLCHAIN
try {
    # All build dependencies must already be available; no implicit downloads.
    $env:GOPROXY = 'off'
    $env:GOTOOLCHAIN = 'local'
    $env:GOCACHE = if ($env:VAC_TEST_ROOT) { Join-Path (Assert-VacPlainPath $env:VAC_TEST_ROOT) 'cache/go' } else { Join-Path $runtimeRoot 'build/cache/go' }
    Push-Location $sourceRoot
    try { & go build -o $binary .; if ($LASTEXITCODE) { throw 'Control-plane build failed; inspect the build output.' } }
    finally { Pop-Location }
    foreach ($file in $sourceFiles) {
        if ((Get-FileHash -LiteralPath $file.FullName).Hash -ne $sourceHashes[$file.FullName]) { throw 'Source changed during build; do not use this incomplete output.' }
    }
    $afterFiles = @(Get-ChildItem -LiteralPath $sourceRoot -Recurse -File | Where-Object { $_.Extension -in @('.go', '.sql', '.json') -or $_.Name -in @('go.mod', 'go.sum') })
    if (@(Compare-Object $sourceFiles.FullName $afterFiles.FullName).Count) { throw 'Source inventory changed during build.' }
    Write-VacJson ($binary+'.runtime.json') @{
        schema_version=1;built_at=[DateTime]::UtcNow.ToString('o');max_schema_version=[int]$maxSchema
        schema_versions=@($migrations | ForEach-Object { [int]$_.Name.Substring(0,4) })
        binary_sha256=(Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()
        migrations=@($migrations | ForEach-Object { @{name=$_.Name;sha256=$sourceHashes[$_.FullName].ToLowerInvariant()} })
    }
    Write-Host "Built control plane and matching schema manifest: $OutputDirectory"
} finally {
    $env:GOPROXY = $previousProxy
    $env:GOCACHE = $previousCache
    $env:GOTOOLCHAIN = $previousToolchain
}
