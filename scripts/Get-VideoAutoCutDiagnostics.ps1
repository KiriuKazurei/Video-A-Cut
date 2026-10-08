#Requires -Version 7.2
param(
    [Parameter(Mandatory=$true)][string]$DataDirectory,
    [ValidateRange(1024,65535)][int]$Port=8806,
    [string]$ProfileFile='',
    [string]$ControlBinary='',
    [switch]$DevelopmentBuild,
    [string]$ReportFile=''
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Runtime.Common.ps1')
$runtimeRoot = Split-Path -Parent $PSScriptRoot
if (!$ProfileFile) { $ProfileFile = Join-Path $runtimeRoot 'configs/phase6/profile.builtin.example.json' }
if (!$ControlBinary) { $ControlBinary = Join-Path $runtimeRoot 'bin/control-plane.exe' }
$checks = [Collections.Generic.List[object]]::new()
function Add-Check($ID, $Status, $Message, [bool]$StartupBlocking=$true) { $checks.Add(@{id=$ID;status=$Status;message=$Message;startup_blocking=$StartupBlocking}) }
try {
    $null = Assert-VacPlainPath $DataDirectory
    $candidate = $DataDirectory
    while (!(Test-Path -LiteralPath $candidate)) { $candidate = Split-Path -Parent $candidate }
    if (!(Test-Path -LiteralPath $candidate -PathType Container)) { throw 'Data path is not a directory.' }
    # Actual writability is checked when the supervisor opens its exclusive lock.
    Add-Check 'data_directory' 'not_checked' 'Path checked; write access and exclusive ownership are checked at startup.'
} catch { Add-Check 'data_directory' 'blocked' 'Use an absolute directory without reparse points.' }
try {
    $profile = Get-Content -LiteralPath $ProfileFile -Raw | ConvertFrom-Json
    if (!$profile.profile_id -or $profile.revision -lt 1) { throw 'Invalid profile binding.' }
    Add-Check 'profile' 'ready' 'Profile JSON and binding are readable; service validation runs at startup.'
} catch { Add-Check 'profile' 'blocked' 'Select a readable processing profile JSON with a positive revision.' }
if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
    Add-Check 'port' 'blocked' "Port $Port is occupied; choose another port or stop its owning session."
} else { Add-Check 'port' 'ready' "Port $Port is available at observation time." }
if (Test-Path -LiteralPath (Join-Path $runtimeRoot 'webui/dist/index.html') -PathType Leaf) {
    Add-Check 'webui' 'ready' 'WebUI build exists.'
} else { Add-Check 'webui' 'blocked' 'Build WebUI before starting, or use a complete portable directory.' }
if (!$DevelopmentBuild) {
    try {
        $ControlBinary = Assert-VacPlainPath $ControlBinary
        $manifest = Get-Content -LiteralPath (Assert-VacPlainPath ($ControlBinary+'.runtime.json')) -Raw | ConvertFrom-Json
        if ($manifest.schema_version -ne 1 -or $manifest.max_schema_version -lt 1 -or !$manifest.schema_versions.Count -or
            $manifest.binary_sha256 -ne (Get-FileHash -LiteralPath $ControlBinary -Algorithm SHA256).Hash.ToLowerInvariant()) { throw 'Invalid binary manifest.' }
        Add-Check 'control_binary' 'ready' 'Prebuilt control plane matches its schema manifest.'
    } catch { Add-Check 'control_binary' 'blocked' 'Supply the executable and matching runtime manifest from Build-ControlPlaneRuntime.ps1, or explicitly select DevelopmentBuild.' }
} elseif (Get-Command go -CommandType Application -ErrorAction SilentlyContinue) {
    Add-Check 'control_binary' 'not_checked' 'Development build selected; the supervisor will build the control plane.'
} else { Add-Check 'control_binary' 'blocked' 'DevelopmentBuild requires Go.' }
foreach ($tool in 'node', 'python', 'ffmpeg', 'ffprobe') {
    try {
        $command = Get-Command $tool -CommandType Application -ErrorAction Stop
        $arguments = @('--version')
        if ($tool -in @('ffmpeg', 'ffprobe')) { $arguments = @('-version') }
        if ($tool -eq 'python') { $arguments = @('-c', 'import sys,sqlite3; print("Python " + sys.version.split()[0]); sys.exit(0 if sys.version_info >= (3,11) else 1)') }
        $version = Invoke-VacToolProbe $command.Source $arguments
        if ($tool -eq 'node' -and ($version -notmatch '^v(\d+)\.(\d+)\.' -or
            [int]$Matches[1] -lt 22 -or ([int]$Matches[1] -eq 22 -and [int]$Matches[2] -lt 12))) { throw 'Node version too old.' }
        $versionLabel = if ($version -match '(?m)(?:^v|^Python |^(?:ffmpeg|ffprobe) version )(\d+(?:\.\d+)+)') { $Matches[1] } else { 'version not identified' }
        Add-Check $tool 'ready' "Local executable responds ($versionLabel)."
        if ($tool -eq 'ffmpeg') {
            $encoders = Invoke-VacToolProbe $command.Source @('-hide_banner', '-encoders')
            $filters = Invoke-VacToolProbe $command.Source @('-hide_banner', '-filters')
            $ready = $encoders -match '(?m)^\s*V\S*\s+libx264\s' -and $encoders -match '(?m)^\s*A\S*\s+aac\s'
            foreach ($filter in 'scale', 'pad', 'fps', 'trim', 'atrim', 'concat', 'amix') {
                $ready = $ready -and $filters -match "(?m)^\s*\S+\s+$filter\s"
            }
            Add-Check 'ffmpeg_preview_features' $(if ($ready) {'ready'} else {'blocked'}) 'MP4 tasks require libx264, AAC and the core timeline filters; existing worker preflight enforces this.' $false
            Add-Check 'subtitle_filter' $(if ($filters -match '(?m)^\s*\S+\s+subtitles\s') {'ready'} else {'blocked'}) 'Optional subtitle tasks also require the subtitles filter and their selected font.' $false
        }
    } catch { Add-Check $tool 'blocked' "Install or configure a compatible local $tool executable; no dependency was downloaded." }
}
Add-Check 'subtitle_font' 'not_checked' 'The selected subtitle font is checked by the exporter for each subtitle task.'
Add-Check 'tts_voice' 'not_checked' 'Use the existing TTS voice selector and task preflight to check the selected SAPI voice.'
$report = [ordered]@{
    schema_version=1;generated_at=[DateTime]::UtcNow.ToString('o')
    overall_status=$(if (@($checks | Where-Object { $_.status -eq 'blocked' -and $_.startup_blocking }).Count) {'blocked'} else {'not_checked'})
    checks=@($checks.ToArray()); cloud_probes_performed=$false
}
if ($ReportFile) { Write-VacJson (Assert-VacPlainPath $ReportFile) $report }
$report | ConvertTo-Json -Depth 20
