#Requires -Version 7.2
param(
  [Parameter(Mandatory)][ValidatePattern('^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$')][string]$Version,
  [Parameter(Mandatory)][string]$Installer,
  [Parameter(Mandatory)][string]$OutputPath,
  [ValidatePattern('^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$')][string]$Repository='KiriuKazurei/Video-A-Cut',
  [string]$Notes='Video Auto Cut 桌面更新'
)
$ErrorActionPreference='Stop'
$installerFile=Get-Item -LiteralPath $Installer
if($installerFile.Extension -ne '.exe'){throw 'Signed NSIS installer is required'}
$signatureFile=$installerFile.FullName+'.sig'
if(!(Test-Path -LiteralPath $signatureFile)){throw 'Updater signature is missing'}
$signature=(Get-Content -LiteralPath $signatureFile -Raw).Trim()
if(!$signature){throw 'Updater signature is empty'}
[void][Convert]::FromBase64String($signature)
$tag=[Uri]::EscapeDataString('v'+$Version)
$filename=[Uri]::EscapeDataString($installerFile.Name)
$manifest=[ordered]@{
  version=$Version;notes=$Notes;pub_date=(Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
  platforms=@{'windows-x86_64'=@{url="https://github.com/$Repository/releases/download/$tag/$filename";signature=$signature}}
}
$manifest|ConvertTo-Json -Depth 8|Set-Content -LiteralPath $OutputPath -Encoding utf8NoBOM
Write-Output "Update manifest: $([IO.Path]::GetFullPath($OutputPath))"
