#Requires -Version 7.2
param([Parameter(Mandatory=$true)][string]$DataDirectory)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Runtime.Common.ps1')
$DataDirectory = Assert-VacPlainPath $DataDirectory
$active = Get-Content -LiteralPath (Assert-VacPlainPath (Join-Path $DataDirectory 'active-session.json')) -Raw | ConvertFrom-Json
if ($active.status -ne 'running' -or !(Test-VacProcessIdentity $active.supervisor)) {
    throw 'No verified live supervisor for this data directory. Review the retained session identities before manual recovery.'
}
$session = Resolve-VacSessionPath $DataDirectory $active.session_id
# A cooperative request only: the live supervisor stops its in-memory process handles.
$marker = Assert-VacPlainPath (Join-Path $session 'session.stop')
[IO.File]::WriteAllText($marker, 'operator stop requested')
Write-Host 'Stop requested for the verified session. Check the supervisor result and retained logs for completion.'
